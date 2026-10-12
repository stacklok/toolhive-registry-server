// Package operator is the optional Kubernetes adapter. It exports ToolHive CRDs
// without interpreting identity annotations. The standalone legacy controller is
// an exclusive alternative owner; never run both against the same source.
package operator

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	mcpv1beta1 "github.com/stacklok/toolhive/cmd/thv-operator/api/v1beta1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"

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

// Fetcher borrows an authoritative reader. Do not supply an incompletely synced
// or narrower cache: use an API reader, or guarantee the complete scope yourself.
type Fetcher struct {
	reader     client.Reader
	definition persistence.SourceDefinition
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

// Convert shares the standalone metadata conversion without parsing claims or
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
// Malformed export metadata is skipped as in the standalone operator adapter.
func (f *Fetcher) Fetch(ctx context.Context, definition persistence.SourceDefinition) (ingest.Result, error) {
	if !f.matches(definition) {
		return ingest.Result{}, persistence.ErrInvalid
	}
	var snapshot model.Snapshot
	namespaces := f.definition.Kubernetes.Namespaces
	if len(namespaces) == 0 {
		namespaces = []string{""}
	}
	for _, namespace := range namespaces {
		for _, list := range resourceLists() {
			if err := f.reader.List(ctx, list, client.InNamespace(namespace)); err != nil {
				if ctx.Err() != nil {
					return ingest.Result{}, ctx.Err()
				}
				return ingest.Result{}, ingest.ErrFetch
			}
			if list.GetContinue() != "" {
				return ingest.Result{}, ingest.ErrFetch
			}
			err := meta.EachListItem(list, func(obj runtime.Object) error {
				object, ok := obj.(client.Object)
				if !ok {
					return persistence.ErrInvalid
				}
				if server := exportedServer(object); server != nil {
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
	// Colliding names across CRD kinds must fail, not silently pick one resource.
	if err := persistence.ValidateSnapshot(&snapshot); err != nil {
		return ingest.Result{}, err
	}
	data, err := json.Marshal(snapshot.Data)
	if err != nil {
		return ingest.Result{}, persistence.ErrInvalid
	}
	return ingest.Result{Snapshot: snapshot, Hash: fmt.Sprintf("%x", sha256.Sum256(data))}, nil
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

func exportedServer(object client.Object) *model.Server {
	annotations := object.GetAnnotations()
	if annotations[defaultRegistryExportAnnotation] != "true" {
		return nil
	}
	if _, ok := annotations[defaultRegistryURLAnnotation]; !ok {
		return nil
	}
	if _, ok := annotations[defaultRegistryDescriptionAnnotation]; !ok {
		return nil
	}
	server, err := Convert(object)
	if err != nil {
		return nil
	}
	// Legacy conversion omits network package URLs. The fenced schema requires
	// them: use the explicit export URL, never a cluster status address.
	for i := range server.Packages {
		transport := &server.Packages[i].Transport
		if transport.Type != "stdio" && transport.URL == "" {
			transport.URL = annotations[defaultRegistryURLAnnotation]
		}
	}
	single := model.Snapshot{}
	single.Data.Servers = []model.Server{*server}
	if persistence.ValidateSnapshot(&single) != nil {
		return nil
	}
	return server
}

func resourceLists() []client.ObjectList {
	return []client.ObjectList{&mcpv1beta1.MCPServerList{}, &mcpv1beta1.VirtualMCPServerList{}, &mcpv1beta1.MCPRemoteProxyList{}}
}
