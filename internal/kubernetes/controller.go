package kubernetes

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	mcpv1alpha1 "github.com/stacklok/toolhive/cmd/thv-operator/api/v1alpha1"
	toolhivetypes "github.com/stacklok/toolhive/pkg/registry/registry"
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

// MCPServerReconciler reconciles MCPServer objects
type MCPServerReconciler struct {
	client       client.Client
	scheme       *runtime.Scheme
	requeueAfter time.Duration
	syncWriter   writer.SyncWriter
	registryName string
}

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *MCPServerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// Fetch the MCPServer instance
	registry, err := getMCPServerList(ctx, r.client, req.Namespace)
	if err != nil {
		slog.Error("Failed to get MCPServer list", "error", err)
		return ctrl.Result{}, err
	}

	slog.Info("MCP servers list fetched successfully",
		"registry", r.registryName,
		"count", len(registry.Data.Servers),
	)

	if err := r.syncWriter.Store(ctx, r.registryName, registry); err != nil {
		slog.Error("Failed to store MCPServer list", "error", err)
		return ctrl.Result{
			Requeue:      true,
			RequeueAfter: r.requeueAfter,
		}, err
	}

	slog.Info("MCP servers stored successfully",
		"registry", r.registryName,
		"count", len(registry.Data.Servers),
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

// getMCPServerList retrieves all MCPServer objects and extracts ServerJSON objects
func getMCPServerList(ctx context.Context, c client.Client, namespace string) (*toolhivetypes.UpstreamRegistry, error) {
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
	for _, mcpServer := range mcpServerList.Items {
		if !hasRequiredRegistryAnnotations(mcpServer.GetAnnotations()) {
			continue
		}
		serverJSON, err := extractServer(&mcpServer)
		if err != nil {
			slog.Warn("Failed to extract ServerJSON from K8s resource, skipping",
				"type", "MCPServer",
				"namespace", mcpServer.Namespace,
				"name", mcpServer.Name,
				"error", err)
			continue
		}
		serverJSONs = append(serverJSONs, *serverJSON)
	}

	for _, vmcpServer := range vmcpServerList.Items {
		if !hasRequiredRegistryAnnotations(vmcpServer.GetAnnotations()) {
			continue
		}
		serverJSON, err := extractVirtualMCPServer(&vmcpServer)
		if err != nil {
			slog.Warn("Failed to extract ServerJSON from K8s resource, skipping",
				"type", "VirtualMCPServer",
				"namespace", vmcpServer.Namespace,
				"name", vmcpServer.Name,
				"error", err)
			continue
		}
		serverJSONs = append(serverJSONs, *serverJSON)
	}

	for _, mcpRemoteProxy := range mcpRemoteProxyList.Items {
		if !hasRequiredRegistryAnnotations(mcpRemoteProxy.GetAnnotations()) {
			continue
		}
		serverJSON, err := extractMCPRemoteProxy(&mcpRemoteProxy)
		if err != nil {
			slog.Warn("Failed to extract ServerJSON from K8s resource, skipping",
				"type", "MCPRemoteProxy",
				"namespace", mcpRemoteProxy.Namespace,
				"name", mcpRemoteProxy.Name,
				"error", err)
			continue
		}
		serverJSONs = append(serverJSONs, *serverJSON)
	}

	return &toolhivetypes.UpstreamRegistry{
		Data: toolhivetypes.UpstreamData{
			Servers: serverJSONs,
		},
	}, nil
}
