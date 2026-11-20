// Package operator is the optional Kubernetes adapter. It exports ToolHive CRDs
// without interpreting identity annotations. It must exclusively own its source;
// never run an old unfenced binary against the same source.
package operator

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"sync"

	mcpv1beta1 "github.com/stacklok/toolhive/cmd/thv-operator/api/v1beta1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/stacklok/toolhive-registry-server/internal/shutdown"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/ingest"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

const (
	defaultRegistryExportAnnotation          = "toolhive.stacklok.dev/registry-export"
	defaultRegistryURLAnnotation             = "toolhive.stacklok.dev/registry-url"
	defaultRegistryDescriptionAnnotation     = "toolhive.stacklok.dev/registry-description"
	defaultRegistryTitleAnnotation           = "toolhive.stacklok.dev/registry-title"
	defaultRegistryIconAnnotation            = "toolhive.stacklok.dev/registry-icon"
	defaultRegistryCategoryAnnotation        = "toolhive.stacklok.dev/registry-category"
	defaultRegistryToolDefinitionsAnnotation = "toolhive.stacklok.dev/tool-definitions"
	defaultRegistryToolsAnnotation           = "toolhive.stacklok.dev/tools"
	defaultRegistryVersionAnnotation         = "toolhive.stacklok.dev/registry-version"
)

// errDefinitionChanged is persistence.ErrStale for a leased definition other
// than the fetcher's captured incarnation and scope.
var errDefinitionChanged = fmt.Errorf("%w: the source definition changed; construct a new fetcher", persistence.ErrStale)

// Fetcher borrows an authoritative reader. Do not supply an incompletely synced
// or narrower cache: use an API reader, or guarantee the complete scope yourself.
type Fetcher struct {
	reader     client.Reader
	definition persistence.SourceDefinition

	mu      sync.Mutex
	fetched bool
	// exported holds, for the last complete list, every object annotated for
	// export: what Relevant compares watch events with.
	exported map[objectKey]objectState
}

// NewFetcher captures one source incarnation and fixed namespace scope. After a
// scope change construct a new fetcher/runner; stale jobs cannot commit across the
// definition generation. An empty namespace list means all namespaces.
func NewFetcher(reader client.Reader, definition persistence.SourceDefinition) (*Fetcher, error) {
	kind, err := definition.Kind()
	if err != nil || kind != "kubernetes" || definition.ID == "" || reader == nil {
		return nil, persistence.ErrInvalid
	}
	namespaces := slices.Clone(definition.Kubernetes.Namespaces)
	slices.Sort(namespaces)
	for i, namespace := range namespaces {
		if len(validation.IsDNS1123Label(namespace)) != 0 || (i > 0 && namespaces[i-1] == namespace) {
			return nil, persistence.ErrInvalid
		}
	}
	definition.Kubernetes = &persistence.KubernetesSpec{Namespaces: namespaces}
	return &Fetcher{reader: reader, definition: definition}, nil
}

// Convert maps one supported CRD object to a server without parsing claims or
// deciding export eligibility. Fetch applies exact export opt-in separately.
func Convert(object client.Object) (*model.Server, error) {
	switch obj := object.(type) {
	case *mcpv1beta1.MCPServer:
		return extractServer(obj)
	case *mcpv1beta1.VirtualMCPServer:
		return extractVirtualMCPServer(obj)
	case *mcpv1beta1.MCPRemoteProxy:
		return extractMCPRemoteProxy(obj)
	default:
		return nil, persistence.ErrInvalid
	}
}

// Fetch lists every configured namespace and every supported CRD kind before
// returning a snapshot. Empty is valid. Any list failure preserves old data.
//
// Every object annotated registry-export=true with the registry-url and
// registry-description annotations is exported, as v1.5.4 stored them: no
// schema validation applies. An object that cannot be converted is skipped
// with a warning naming it. Storage safety (including duplicate name/version
// exports, which fail the source rather than silently pick a resource) is
// checked by the coordinator on the returned snapshot. A definition other than
// the captured incarnation and scope is persistence.ErrStale.
//
//nolint:gocyclo // Validate the complete scope before returning any snapshot, including cancellation races.
func (f *Fetcher) Fetch(ctx context.Context, definition persistence.SourceDefinition) (ingest.Result, error) {
	if !f.matches(definition) {
		return ingest.Result{}, errDefinitionChanged
	}
	var snapshot model.Snapshot
	exported := map[objectKey]objectState{}
	namespaces := f.definition.Kubernetes.Namespaces
	if len(namespaces) == 0 {
		namespaces = []string{""}
	}
	for _, namespace := range namespaces {
		for _, list := range resourceLists() {
			if err := f.reader.List(ctx, list, client.InNamespace(namespace)); err != nil {
				if ctx.Err() != nil && shutdown.Result(err) == nil {
					return ingest.Result{}, ctx.Err()
				}
				return ingest.Result{}, errors.Join(ctx.Err(), ingest.WithCause(ingest.ErrFetch,
					fmt.Errorf("listing %s in %s: %w", listKind(list), namespaceLabel(namespace), err)))
			}
			if list.GetContinue() != "" {
				return ingest.Result{}, ingest.WithCause(ingest.ErrFetch,
					fmt.Errorf("listing %s in %s returned an incomplete page", listKind(list), namespaceLabel(namespace)))
			}
			err := meta.EachListItem(list, func(obj runtime.Object) error {
				object, ok := obj.(client.Object)
				if !ok {
					return persistence.ErrInvalid
				}
				if !isExported(object) {
					return nil
				}
				exported[keyOf(object)] = stateOf(object)
				if server := exportedServer(ctx, object); server != nil {
					snapshot.Data.Servers = append(snapshot.Data.Servers, *server)
				}
				return nil
			})
			if err != nil {
				return ingest.Result{}, err
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return ingest.Result{}, err
	}
	slices.SortFunc(snapshot.Data.Servers, func(a, b model.Server) int {
		if n := strings.Compare(a.Name, b.Name); n != 0 {
			return n
		}
		return strings.Compare(a.Version, b.Version)
	})
	data, err := json.Marshal(snapshot.Data)
	if err != nil {
		return ingest.Result{}, ingest.WithCause(persistence.ErrInvalid, err)
	}
	f.mu.Lock()
	f.fetched, f.exported = true, exported
	f.mu.Unlock()
	return ingest.Result{Snapshot: snapshot, Hash: fmt.Sprintf("%x", sha256.Sum256(data))}, nil
}

// Relevant reports whether a watch event can change what Fetch would export,
// compared with the last complete Fetch. It is false for objects that are not
// annotated for export now and were not at the last fetch, and for updates
// that change neither metadata.generation nor annotations (such as status
// updates). Deleting a previously exported object is relevant. Before the
// first Fetch every event is relevant.
func (f *Fetcher) Relevant(event watch.Event) bool {
	switch event.Type {
	case watch.Added, watch.Modified, watch.Deleted:
	case watch.Bookmark, watch.Error:
		return false
	default:
		return false
	}
	object, ok := event.Object.(client.Object)
	if !ok {
		return true
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.fetched {
		return true
	}
	seen, wasExported := f.exported[keyOf(object)]
	if event.Type == watch.Deleted {
		return wasExported
	}
	if !isExported(object) {
		return wasExported
	}
	return !wasExported || seen.generation != object.GetGeneration() || !maps.Equal(seen.annotations, object.GetAnnotations())
}

func (f *Fetcher) matches(definition persistence.SourceDefinition) bool {
	kind, err := definition.Kind()
	if err != nil || kind != "kubernetes" || definition.ID != f.definition.ID || definition.Name != f.definition.Name {
		return false
	}
	namespaces := slices.Clone(definition.Kubernetes.Namespaces)
	slices.Sort(namespaces)
	return slices.Equal(namespaces, f.definition.Kubernetes.Namespaces)
}

type objectKey struct{ kind, namespace, name string }

type objectState struct {
	generation  int64
	annotations map[string]string
}

func keyOf(object client.Object) objectKey {
	return objectKey{kind: objectKind(object), namespace: object.GetNamespace(), name: object.GetName()}
}

func stateOf(object client.Object) objectState {
	return objectState{generation: object.GetGeneration(), annotations: maps.Clone(object.GetAnnotations())}
}

func isExported(object client.Object) bool {
	return object.GetAnnotations()[defaultRegistryExportAnnotation] == "true"
}

func exportedServer(ctx context.Context, object client.Object) *model.Server {
	annotations := object.GetAnnotations()
	if _, ok := annotations[defaultRegistryURLAnnotation]; !ok {
		return nil
	}
	if _, ok := annotations[defaultRegistryDescriptionAnnotation]; !ok {
		return nil
	}
	server, err := Convert(object)
	if err != nil {
		slog.WarnContext(ctx, "Failed to extract ServerJSON from K8s resource, skipping",
			"type", objectKind(object), "namespace", object.GetNamespace(), "name", object.GetName(), "error", err)
		return nil
	}
	// Legacy conversion omits network package URLs: use the explicit export
	// URL, never a cluster status address.
	for i := range server.Packages {
		transport := &server.Packages[i].Transport
		if transport.Type != "stdio" && transport.URL == "" {
			transport.URL = annotations[defaultRegistryURLAnnotation]
		}
	}
	return server
}

func objectKind(object client.Object) string {
	switch object.(type) {
	case *mcpv1beta1.MCPServer:
		return "MCPServer"
	case *mcpv1beta1.VirtualMCPServer:
		return "VirtualMCPServer"
	case *mcpv1beta1.MCPRemoteProxy:
		return "MCPRemoteProxy"
	default:
		return fmt.Sprintf("%T", object)
	}
}

func listKind(list client.ObjectList) string {
	switch list.(type) {
	case *mcpv1beta1.MCPServerList:
		return "MCPServers"
	case *mcpv1beta1.VirtualMCPServerList:
		return "VirtualMCPServers"
	case *mcpv1beta1.MCPRemoteProxyList:
		return "MCPRemoteProxies"
	default:
		return fmt.Sprintf("%T", list)
	}
}

func namespaceLabel(namespace string) string {
	if namespace == "" {
		return "all namespaces"
	}
	return fmt.Sprintf("namespace %q", namespace)
}

func resourceLists() []client.ObjectList {
	return []client.ObjectList{&mcpv1beta1.MCPServerList{}, &mcpv1beta1.VirtualMCPServerList{}, &mcpv1beta1.MCPRemoteProxyList{}}
}
