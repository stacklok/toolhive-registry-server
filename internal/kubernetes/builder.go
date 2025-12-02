package kubernetes

import (
	"context"
	"fmt"
	"time"

	mcpv1alpha1 "github.com/stacklok/toolhive/cmd/thv-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/log"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/stacklok/toolhive-registry-server/internal/sync/writer"
)

const (
	defaultRegistryExportAnnotation      = "toolhive.stacklok.dev/registry-export"
	defaultRegistryURLAnnotation         = "toolhive.stacklok.dev/registry-url"
	defaultRegistryDescriptionAnnotation = "toolhive.stacklok.dev/registry-description"

	defaultRequeueAfter = 10 * time.Second

	leaderElectionID = "toolhive-registry-server-leader-election"
)

// hasRequiredRegistryAnnotations checks if the given annotations map contains
// all required annotations for registry export (description and URL).
func hasRequiredRegistryAnnotations(annotations map[string]string) bool {
	if annotations == nil {
		return false
	}
	_, hasDesc := annotations[defaultRegistryDescriptionAnnotation]
	_, hasURL := annotations[defaultRegistryURLAnnotation]
	return hasDesc && hasURL
}

type mcpServerReconcilerOptions struct {
	namespaces   []string
	requeueAfter time.Duration
	syncWriter   writer.SyncWriter
	registryName string
}

// Option is a function that sets an option for the MCPServerReconciler.
type Option func(*mcpServerReconcilerOptions) error

// WithNamespaces sets the namespaces to watch.
func WithNamespaces(namespaces ...string) Option {
	return func(o *mcpServerReconcilerOptions) error {
		if o.namespaces == nil {
			o.namespaces = make([]string, 0)
		}
		o.namespaces = append(o.namespaces, namespaces...)
		return nil
	}
}

// WithRequeueAfter sets the requeue after duration.
func WithRequeueAfter(requeueAfter time.Duration) Option {
	return func(o *mcpServerReconcilerOptions) error {
		if requeueAfter <= 0 {
			return fmt.Errorf("requeueAfter must be greater than 0")
		}
		o.requeueAfter = requeueAfter
		return nil
	}
}

// WithSyncWriter sets the sync writer.
func WithSyncWriter(sw writer.SyncWriter) Option {
	return func(o *mcpServerReconcilerOptions) error {
		if sw == nil {
			return fmt.Errorf("sync writer is required")
		}
		o.syncWriter = sw
		return nil
	}
}

// WithRegistryName sets the registry name. This is used to identify the
// registry in the sync writer.
func WithRegistryName(name string) Option {
	return func(o *mcpServerReconcilerOptions) error {
		if name == "" {
			return fmt.Errorf("registry name is required")
		}
		o.registryName = name
		return nil
	}
}

// NewMCPServerReconciler creates a new MCPServerReconciler.
func NewMCPServerReconciler(
	ctx context.Context,
	opts ...Option,
) (ctrl.Manager, error) {
	logger := log.FromContext(ctx)

	o := &mcpServerReconcilerOptions{
		namespaces:   []string{},
		requeueAfter: defaultRequeueAfter,
	}

	for _, opt := range opts {
		if err := opt(o); err != nil {
			return nil, err
		}
	}

	if o.syncWriter == nil {
		return nil, fmt.Errorf("sync writer is required")
	}
	if o.registryName == "" {
		return nil, fmt.Errorf("registry name is required")
	}

	defaultNamespaces := map[string]cache.Config{}
	for _, namespace := range o.namespaces {
		defaultNamespaces[namespace] = cache.Config{}
	}

	scheme := runtime.NewScheme()
	if err := mcpv1alpha1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("failed to add MCPv1alpha1 scheme: %w", err)
	}

	options := ctrl.Options{
		Scheme:           scheme,
		LeaderElection:   true,
		LeaderElectionID: leaderElectionID,
		Cache: cache.Options{
			// if nil, defaults to all namespaces
			DefaultNamespaces: defaultNamespaces,
		},
		// disable metrics server
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), options)
	if err != nil {
		return nil, fmt.Errorf("failed to create manager: %w", err)
	}

	controller := &MCPServerReconciler{
		requeueAfter: o.requeueAfter,
		syncWriter:   o.syncWriter,
		registryName: o.registryName,
	}

	if err := controller.SetupWithManager(mgr); err != nil {
		return nil, fmt.Errorf("failed to setup controller with manager: %w", err)
	}

	go func() {
		if err := mgr.Start(ctx); err != nil {
			logger.Error(err, "failed to start manager")
		}
	}()

	return mgr, nil
}
