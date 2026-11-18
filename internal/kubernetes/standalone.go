package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	mcp "github.com/stacklok/toolhive/cmd/thv-operator/api/v1beta1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/stacklok/toolhive-registry-server/internal/db"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/ingest"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/operator"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/reconcile"
)

const (
	exportAnnotation = "toolhive.stacklok.dev/registry-export"
	claimsAnnotation = "toolhive.stacklok.dev/authz-claims"
)

// AnnotationClaims records, per Kubernetes source, the claims each exported
// server's authz-claims annotation sets. The claims hook reads them when the
// source's snapshot is committed, so an annotation replaces the source's
// claims for that entry, as in v1.5.4. It is safe for concurrent use.
type AnnotationClaims struct {
	mu sync.Mutex
	// bySource maps source name → resource kind → server name → claims JSON.
	bySource map[string]map[string]map[string][]byte
}

// NewAnnotationClaims returns an empty store.
func NewAnnotationClaims() *AnnotationClaims {
	return &AnnotationClaims{bySource: map[string]map[string]map[string][]byte{}}
}

// EntryClaims returns the recorded per-server claims of a source.
func (a *AnnotationClaims) EntryClaims(sourceName string) map[string][]byte {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	out := map[string][]byte{}
	for _, byName := range a.bySource[sourceName] {
		for name, claims := range byName {
			out[name] = claims
		}
	}
	return out
}

// replace swaps one resource kind's recorded claims for a source. Each list
// of a kind is complete, so a removed annotation drops out.
func (a *AnnotationClaims) replace(sourceName, kind string, byName map[string][]byte) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.bySource[sourceName] == nil {
		a.bySource[sourceName] = map[string]map[string][]byte{}
	}
	a.bySource[sourceName][kind] = byName
}

// NewStandaloneOperator constructs the leased operator runner for one
// Kubernetes source. The authoritative client remains caller-owned. claims
// may be nil when authorization is off.
func NewStandaloneOperator(
	api client.WithWatch, jobs persistence.Jobs, definition persistence.SourceDefinition,
	options reconcile.Options, claims *AnnotationClaims,
) (*operator.Runner, error) {
	if api == nil {
		return nil, persistence.ErrInvalid
	}
	return operator.NewRunner(&standaloneExportClient{WithWatch: api, source: definition.Name, claims: claims},
		jobs, definition, options)
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
	c.claims.replace(c.source, kind, byName)
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
		if annotations[exportAnnotation] != "true" {
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
