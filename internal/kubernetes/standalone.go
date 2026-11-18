package kubernetes

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	mcp "github.com/stacklok/toolhive/cmd/thv-operator/api/v1beta1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/stacklok/toolhive-registry-server/internal/db"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/ingest"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/operator"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/reconcile"
)

const (
	exportAnnotation      = "toolhive.stacklok.dev/registry-export"
	urlAnnotation         = "toolhive.stacklok.dev/registry-url"
	descriptionAnnotation = "toolhive.stacklok.dev/registry-description"
	claimsAnnotation      = "toolhive.stacklok.dev/authz-claims"
)

// AnnotationClaims records, per Kubernetes source, the claims each exported
// server's authz-claims annotation sets. The claims hook reads them when the
// source's snapshot is committed, so an annotation replaces the source's
// claims for that entry, as in v1.5.4. It is safe for concurrent use.
type AnnotationClaims struct {
	mu sync.Mutex
	// bySource maps source name → list scope (namespace and kind) → server
	// name → claims JSON. Each list covers one namespace and one kind.
	bySource map[string]map[listScope]map[string][]byte
}

type listScope struct{ namespace, kind string }

// NewAnnotationClaims returns an empty store.
func NewAnnotationClaims() *AnnotationClaims {
	return &AnnotationClaims{bySource: map[string]map[listScope]map[string][]byte{}}
}

// EntryClaims returns the recorded per-server claims of a source. Scopes are
// merged in a fixed order, so a name annotated in two scopes resolves the same
// way every time.
func (a *AnnotationClaims) EntryClaims(sourceName string) map[string][]byte {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	scopes := slices.SortedFunc(maps.Keys(a.bySource[sourceName]), func(x, y listScope) int {
		return cmp.Or(cmp.Compare(x.namespace, y.namespace), cmp.Compare(x.kind, y.kind))
	})
	out := map[string][]byte{}
	for _, scope := range scopes {
		maps.Copy(out, a.bySource[sourceName][scope])
	}
	return out
}

// replace swaps the recorded claims of one namespace and kind for a source.
// Each such list is complete, so a removed annotation drops out.
func (a *AnnotationClaims) replace(sourceName string, scope listScope, byName map[string][]byte) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.bySource[sourceName] == nil {
		a.bySource[sourceName] = map[listScope]map[string][]byte{}
	}
	a.bySource[sourceName][scope] = byName
}

// reset forgets a source's recorded claims. A rebuilt worker may watch other
// namespaces, and scopes it no longer lists must not keep applying.
func (a *AnnotationClaims) reset(sourceName string) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.bySource, sourceName)
}

// digest summarizes a source's recorded claims.
func (a *AnnotationClaims) digest(sourceName string) string {
	claims := a.EntryClaims(sourceName)
	h := sha256.New()
	for _, name := range slices.Sorted(maps.Keys(claims)) {
		h.Write([]byte(name))
		h.Write([]byte{0})
		h.Write(claims[name])
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// claimsDigestSeparator joins the library's filter hash and the annotation
// claims digest in the stored sync result.
const claimsDigestSeparator = "|claims="

// annotationJobs makes a sync commit its full snapshot, and so run the claims
// hook, whenever the source's annotation claims differ from the ones last
// written. The snapshot hash covers server data only, so otherwise a change
// to an annotation alone would complete as an unchanged sync.
//
// The digest of the written claims is stored with the sync result, appended
// to the filter hash, so every replica compares against the database rather
// than its own memory. Status strips it again and reports the baseline as
// invalid when it differs; the coordinator then calls CommitSnapshot.
type annotationJobs struct {
	persistence.Jobs
	source string
	claims *AnnotationClaims
}

func (j *annotationJobs) Status(ctx context.Context, sourceName string) (persistence.JobStatus, error) {
	status, err := j.Jobs.Status(ctx, sourceName)
	if err != nil {
		return status, err
	}
	filterHash, digest, _ := strings.Cut(status.LastFilterHash, claimsDigestSeparator)
	status.LastFilterHash = filterHash
	if digest != j.claims.digest(j.source) {
		status.BaselineValid = false
	}
	return status, nil
}

func (j *annotationJobs) Complete(ctx context.Context, job persistence.Job, result persistence.SyncResult) error {
	result.FilterHash += claimsDigestSeparator + j.claims.digest(j.source)
	return j.Jobs.Complete(ctx, job, result)
}

func (j *annotationJobs) CommitSnapshot(
	ctx context.Context, job persistence.Job, result persistence.SyncResult, snapshot model.Snapshot,
) error {
	result.FilterHash += claimsDigestSeparator + j.claims.digest(j.source)
	return j.Jobs.CommitSnapshot(ctx, job, result, snapshot)
}

// syncedJobs records that the source has synced at least once.
type syncedJobs struct {
	persistence.Jobs
	synced *atomic.Bool
}

func (j *syncedJobs) Complete(ctx context.Context, job persistence.Job, result persistence.SyncResult) error {
	err := j.Jobs.Complete(ctx, job, result)
	if err == nil {
		j.synced.Store(true)
	}
	return err
}

func (j *syncedJobs) CommitSnapshot(
	ctx context.Context, job persistence.Job, result persistence.SyncResult, snapshot model.Snapshot,
) error {
	err := j.Jobs.CommitSnapshot(ctx, job, result, snapshot)
	if err == nil {
		j.synced.Store(true)
	}
	return err
}

// NewStandaloneOperator constructs the leased operator worker for one
// Kubernetes source; it restarts the runner when a watch ends. The
// authoritative client remains caller-owned. claims may be nil, which disables
// annotation claims.
func NewStandaloneOperator(
	api client.WithWatch, jobs persistence.Jobs, definition persistence.SourceDefinition,
	options reconcile.Options, claims *AnnotationClaims,
) (*OperatorWorker, error) {
	if api == nil {
		return nil, persistence.ErrInvalid
	}
	if claims != nil {
		// The runtime stops the previous worker before building this one.
		claims.reset(definition.Name)
		jobs = &annotationJobs{Jobs: jobs, source: definition.Name, claims: claims}
	}
	worker := &OperatorWorker{source: definition.Name}
	jobs = &syncedJobs{Jobs: jobs, synced: &worker.synced}
	runner, err := operator.NewRunner(&standaloneExportClient{WithWatch: api, source: definition.Name, claims: claims},
		jobs, definition, options)
	if err != nil {
		return nil, err
	}
	worker.runner = runner
	return worker, nil
}

// PreflightStandaloneOperator lists all persisted effective namespaces/kinds.
// It does not watch, lease or write.
func PreflightStandaloneOperator(ctx context.Context, api client.WithWatch, definition persistence.SourceDefinition) error {
	if api == nil {
		return persistence.ErrInvalid
	}
	if definition.ID == "" {
		definition.ID = "preflight"
	}
	checked := &standaloneExportClient{WithWatch: api}
	if _, err := operator.NewFetcher(checked, definition); err != nil {
		return err
	}
	namespaces := definition.Kubernetes.Namespaces
	if len(namespaces) == 0 {
		namespaces = []string{""}
	}
	for _, namespace := range namespaces {
		for _, list := range []client.ObjectList{&mcp.MCPServerList{}, &mcp.VirtualMCPServerList{}, &mcp.MCPRemoteProxyList{}} {
			if err := checked.List(ctx, list, client.InNamespace(namespace)); err != nil {
				return err
			}
		}
	}
	return nil
}

// standaloneExportClient delegates to the API client. On each complete list
// of an exported kind it records the authz-claims annotations and drops
// objects whose annotation is invalid, so they are not synced at all.
type standaloneExportClient struct {
	client.WithWatch
	source string
	claims *AnnotationClaims
}

func (c *standaloneExportClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	if err := c.WithWatch.List(ctx, list, opts...); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ingest.ErrFetch
	}
	if list.GetContinue() != "" {
		return ingest.ErrFetch
	}
	var kind string
	switch list.(type) {
	case *mcp.MCPServerList:
		kind = "MCPServer"
	case *mcp.VirtualMCPServerList:
		kind = "VirtualMCPServer"
	case *mcp.MCPRemoteProxyList:
		kind = "MCPRemoteProxy"
	default:
		return nil
	}
	items, err := meta.ExtractList(list)
	if err != nil {
		return persistence.ErrInvalid
	}
	kept, byName, err := exportedClaims(ctx, kind, items)
	if err != nil {
		return err
	}
	if len(kept) != len(items) {
		if err := meta.SetList(list, kept); err != nil {
			return persistence.ErrInvalid
		}
	}
	listOptions := &client.ListOptions{}
	listOptions.ApplyOptions(opts)
	c.claims.replace(c.source, listScope{namespace: listOptions.Namespace, kind: kind}, byName)
	return nil
}

// exportedClaims records the authz-claims annotation of each exported item,
// keyed by server name, and drops items whose annotation is invalid.
func exportedClaims(ctx context.Context, kind string, items []runtime.Object) ([]runtime.Object, map[string][]byte, error) {
	kept := make([]runtime.Object, 0, len(items))
	byName := map[string][]byte{}
	for _, obj := range items {
		object, ok := obj.(client.Object)
		if !ok {
			return nil, nil, persistence.ErrInvalid
		}
		annotations := object.GetAnnotations()
		_, hasURL := annotations[urlAnnotation]
		_, hasDescription := annotations[descriptionAnnotation]
		if annotations[exportAnnotation] != "true" || !hasURL || !hasDescription {
			// The fetcher does not export it, so its claims must not apply to
			// another resource of the same name.
			kept = append(kept, obj)
			continue
		}
		entryClaims, err := parseAnnotationClaims(annotations)
		if err != nil {
			slog.WarnContext(ctx, "Invalid authz-claims annotation, skipping entry",
				"type", kind, "namespace", object.GetNamespace(), "name", object.GetName(), "error", err)
			continue
		}
		if entryClaims != nil {
			if server, err := operator.Convert(object); err == nil && server != nil {
				byName[server.Name] = entryClaims
			}
		}
		kept = append(kept, obj)
	}
	return kept, byName, nil
}

// parseAnnotationClaims returns the marshaled annotation claims, nil when the
// annotation is absent or empty, and an error when it is not a valid claim
// map; v1.5.4 skipped such entries rather than sync them without claims.
func parseAnnotationClaims(annotations map[string]string) ([]byte, error) {
	raw, ok := annotations[claimsAnnotation]
	if !ok || raw == "" {
		return nil, nil
	}
	var claims map[string]any
	if err := json.Unmarshal([]byte(raw), &claims); err != nil {
		return nil, fmt.Errorf("failed to parse %s annotation: %w", claimsAnnotation, err)
	}
	if err := db.ValidateClaimValues(claims); err != nil {
		return nil, fmt.Errorf("invalid claim value in %s annotation: %w", claimsAnnotation, err)
	}
	return json.Marshal(claims)
}
