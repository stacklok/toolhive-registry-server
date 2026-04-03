package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"time"

	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	toolhivetypes "github.com/stacklok/toolhive-core/registry/types"
	mcpv1alpha1 "github.com/stacklok/toolhive/cmd/thv-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/stacklok/toolhive-registry-server/internal/sync/writer"
)

// reconcileResult holds the output of getMCPServerList: the upstream registry
// data plus optional per-entry claims derived from CRD annotations.
type reconcileResult struct {
	Registry       *toolhivetypes.UpstreamRegistry
	PerEntryClaims map[string][]byte // server name → claims JSON (nil if no per-entry claims)
}

// MCPServerReconciler reconciles MCPServer objects
type MCPServerReconciler struct {
	client       client.Client
	scheme       *runtime.Scheme
	requeueAfter time.Duration
	syncWriter   writer.SyncWriter
	registryName string
	baseClaims   map[string]any // source-level claims merged into per-entry claims
}

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *MCPServerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// Fetch the MCPServer instance
	result, err := getMCPServerList(ctx, r.client, req.Namespace, r.baseClaims)
	if err != nil {
		slog.Error("Failed to get MCPServer list", "error", err)
		return ctrl.Result{}, err
	}

	slog.Info("MCP servers list fetched successfully",
		"registry", r.registryName,
		"count", len(result.Registry.Data.Servers),
	)

	var opts []writer.StoreOption
	if len(result.PerEntryClaims) > 0 {
		opts = append(opts, writer.WithPerEntryClaims(result.PerEntryClaims))
	}

	if err := r.syncWriter.Store(ctx, r.registryName, result.Registry, opts...); err != nil {
		slog.Error("Failed to store MCPServer list", "error", err)
		return ctrl.Result{
			Requeue:      true,
			RequeueAfter: r.requeueAfter,
		}, err
	}

	slog.Info("MCP servers stored successfully",
		"registry", r.registryName,
		"count", len(result.Registry.Data.Servers),
	)

	return ctrl.Result{}, nil
}

func checkAnnotation(annotations map[string]string, annotation string) bool {
	if annotations == nil {
		return false
	}
	value, ok := annotations[annotation]
	if !ok {
		return false
	}
	if value == "true" {
		return true
	}
	return false
}

func makeNewObjectPredicate[T client.Object](
	annotation string,
) func(event.TypedCreateEvent[T]) bool {
	return func(event event.TypedCreateEvent[T]) bool {
		annotations := event.Object.GetAnnotations()
		return checkAnnotation(annotations, annotation)
	}
}

func makeUpdateObjectPredicate[T client.Object](
	annotation string,
) func(event.TypedUpdateEvent[T]) bool {
	return func(event event.TypedUpdateEvent[T]) bool {
		newAnnotations := event.ObjectNew.GetAnnotations()
		oldAnnotations := event.ObjectOld.GetAnnotations()

		newEnabled := checkAnnotation(newAnnotations, annotation)
		oldEnabled := checkAnnotation(oldAnnotations, annotation)

		// For the update case, we need to trigger a reconcile
		// even if the annotations haven't changed. This is because
		// the server might have been modified in other ways that
		// are relevant to the sync.
		//
		// Below is the truth table for the update case:
		// new-enabled  | old-enabled  | enqueue
		// new-enabled  | old-disabled | enqueue
		// new-disabled | old-enabled  | enqueue
		// new-disabled | old-disabled | ignore
		return newEnabled || oldEnabled
	}
}

func makeDeleteObjectPredicate[T client.Object](
	annotation string,
) func(event.TypedDeleteEvent[T]) bool {
	return func(event event.TypedDeleteEvent[T]) bool {
		annotations := event.Object.GetAnnotations()
		return checkAnnotation(annotations, annotation)
	}
}

// enqueueMCPServerRequests is an event handler that enqueues a reconcile request
// for the namespace where the watched object resides. This allows VirtualMCPServer and
// MCPRemoteProxy changes to trigger the same reconciliation logic as MCPServer changes.
func enqueueMCPServerRequests() handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(
		func(_ context.Context, obj client.Object) []reconcile.Request {
			return []reconcile.Request{
				{
					NamespacedName: types.NamespacedName{
						Name:      obj.GetName(),
						Namespace: obj.GetNamespace(),
					},
				},
			}
		},
	)
}

// SetupWithManager sets up the controller with the Manager.
func (r *MCPServerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Predicate to filter only registry export ConfigMaps
	annotationPredicate := predicate.Funcs{
		CreateFunc: makeNewObjectPredicate[client.Object](defaultRegistryExportAnnotation),
		UpdateFunc: makeUpdateObjectPredicate[client.Object](defaultRegistryExportAnnotation),
		DeleteFunc: makeDeleteObjectPredicate[client.Object](defaultRegistryExportAnnotation),
	}

	r.client = mgr.GetClient()
	r.scheme = mgr.GetScheme()

	return ctrl.NewControllerManagedBy(mgr).
		For(&mcpv1alpha1.MCPServer{}, builder.WithPredicates(annotationPredicate)).
		Watches(&mcpv1alpha1.VirtualMCPServer{}, enqueueMCPServerRequests(), builder.WithPredicates(annotationPredicate)).
		Watches(&mcpv1alpha1.MCPRemoteProxy{}, enqueueMCPServerRequests(), builder.WithPredicates(annotationPredicate)).
		Complete(r)
}

// extractorFunc converts a K8s resource into a ServerJSON.
type extractorFunc func(client.Object) (*upstreamv0.ServerJSON, error)

// processResources filters, extracts, and builds per-entry claims for a list of K8s resources.
func processResources(
	items []client.Object,
	typeName string,
	extractor extractorFunc,
	baseClaims map[string]any,
	serverJSONs *[]upstreamv0.ServerJSON,
	perEntryClaims map[string][]byte,
) {
	for _, obj := range items {
		if !hasRequiredRegistryAnnotations(obj.GetAnnotations()) {
			continue
		}
		serverJSON, err := extractor(obj)
		if err != nil {
			slog.Warn("Failed to extract ServerJSON from K8s resource, skipping",
				"type", typeName,
				"namespace", obj.GetNamespace(),
				"name", obj.GetName(),
				"error", err)
			continue
		}
		claims, err := buildEntryClaims(baseClaims, obj.GetAnnotations())
		if err != nil {
			slog.Warn("Invalid authz-claims annotation, falling back to source-level claims",
				"type", typeName,
				"namespace", obj.GetNamespace(),
				"name", obj.GetName(),
				"error", err)
		} else if claims != nil {
			perEntryClaims[serverJSON.Name] = claims
		}
		*serverJSONs = append(*serverJSONs, *serverJSON)
	}
}

// getMCPServerList retrieves all MCPServer objects, extracts ServerJSON objects,
// and builds per-entry claims from the authz-claims annotation merged with base claims.
func getMCPServerList(
	ctx context.Context, c client.Client, namespace string, baseClaims map[string]any,
) (*reconcileResult, error) {
	listOptions := []client.ListOption{
		client.InNamespace(namespace),
	}

	var mcpServerList mcpv1alpha1.MCPServerList
	if err := c.List(ctx, &mcpServerList, listOptions...); err != nil {
		return nil, fmt.Errorf("failed to list MCPServers: %w", err)
	}
	var vmcpServerList mcpv1alpha1.VirtualMCPServerList
	if err := c.List(ctx, &vmcpServerList, listOptions...); err != nil {
		return nil, fmt.Errorf("failed to list VirtualMCPServers: %w", err)
	}
	var mcpRemoteProxyList mcpv1alpha1.MCPRemoteProxyList
	if err := c.List(ctx, &mcpRemoteProxyList, listOptions...); err != nil {
		return nil, fmt.Errorf("failed to list MCPRemoteProxies: %w", err)
	}

	var serverJSONs []upstreamv0.ServerJSON
	perEntryClaims := make(map[string][]byte)

	// Convert typed slices to []client.Object for the shared helper
	mcpObjects := make([]client.Object, len(mcpServerList.Items))
	for i := range mcpServerList.Items {
		mcpObjects[i] = &mcpServerList.Items[i]
	}
	processResources(mcpObjects, "MCPServer", func(obj client.Object) (*upstreamv0.ServerJSON, error) {
		return extractServer(obj.(*mcpv1alpha1.MCPServer))
	}, baseClaims, &serverJSONs, perEntryClaims)

	vmcpObjects := make([]client.Object, len(vmcpServerList.Items))
	for i := range vmcpServerList.Items {
		vmcpObjects[i] = &vmcpServerList.Items[i]
	}
	processResources(vmcpObjects, "VirtualMCPServer", func(obj client.Object) (*upstreamv0.ServerJSON, error) {
		return extractVirtualMCPServer(obj.(*mcpv1alpha1.VirtualMCPServer))
	}, baseClaims, &serverJSONs, perEntryClaims)

	mcpProxyObjects := make([]client.Object, len(mcpRemoteProxyList.Items))
	for i := range mcpRemoteProxyList.Items {
		mcpProxyObjects[i] = &mcpRemoteProxyList.Items[i]
	}
	processResources(mcpProxyObjects, "MCPRemoteProxy", func(obj client.Object) (*upstreamv0.ServerJSON, error) {
		return extractMCPRemoteProxy(obj.(*mcpv1alpha1.MCPRemoteProxy))
	}, baseClaims, &serverJSONs, perEntryClaims)

	// Return nil instead of empty map when no per-entry claims exist
	var resultClaims map[string][]byte
	if len(perEntryClaims) > 0 {
		resultClaims = perEntryClaims
	}

	return &reconcileResult{
		Registry: &toolhivetypes.UpstreamRegistry{
			Data: toolhivetypes.UpstreamData{
				Servers: serverJSONs,
			},
		},
		PerEntryClaims: resultClaims,
	}, nil
}

// buildEntryClaims reads the authz-claims annotation, parses it as JSON,
// merges with baseClaims, and returns the serialized result.
// Returns (nil, nil) if no annotation is present (entry will use source-level claims).
// Returns (nil, error) if the annotation contains invalid JSON or unsupported claim value types.
func buildEntryClaims(baseClaims map[string]any, annotations map[string]string) ([]byte, error) {
	raw, ok := annotations[defaultAuthzClaimsAnnotation]
	if !ok || raw == "" {
		return nil, nil
	}

	var annotationClaims map[string]any
	if err := json.Unmarshal([]byte(raw), &annotationClaims); err != nil {
		return nil, fmt.Errorf("failed to parse %s annotation: %w", defaultAuthzClaimsAnnotation, err)
	}

	if err := validateClaimValues(annotationClaims); err != nil {
		return nil, fmt.Errorf("invalid claim value in %s annotation: %w", defaultAuthzClaimsAnnotation, err)
	}

	// Merge: clone base claims, then overlay annotation claims (annotation wins on key collision).
	// maps.Clone produces a new map so baseClaims is never mutated.
	merged := maps.Clone(baseClaims)
	if merged == nil {
		merged = make(map[string]any, len(annotationClaims))
	}
	maps.Copy(merged, annotationClaims)

	return json.Marshal(merged)
}

// validateClaimValues ensures all claim values are strings or arrays of strings,
// which is the format the authorization system's JSONB containment queries expect.
func validateClaimValues(claims map[string]any) error {
	for k, v := range claims {
		switch val := v.(type) {
		case string:
			// ok
		case []any:
			for i, elem := range val {
				if _, ok := elem.(string); !ok {
					return fmt.Errorf("claim %q: array element [%d] must be a string, got %T", k, i, elem)
				}
			}
		default:
			return fmt.Errorf("claim %q: value must be a string or array of strings, got %T", k, v)
		}
	}
	return nil
}
