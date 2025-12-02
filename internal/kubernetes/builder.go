package kubernetes

import (
	"context"
	"fmt"
	"time"

	"github.com/stacklok/toolhive-registry-server/internal/sync/writer"
	mcpv1alpha1 "github.com/stacklok/toolhive/cmd/thv-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	defaultRegistryExportAnnotation      = "toolhive.stacklok.dev/registry-export"
	defaultRegistryURLAnnotation         = "toolhive.stacklok.dev/registry-url"
	defaultRegistryDescriptionAnnotation = "toolhive.stacklok.dev/registry-description"
	defaultRegistryTierAnnotation        = "toolhive.stacklok.dev/registry-tier"

	defaultRequeueAfter = 10 * time.Second

	leaderElectionID = "toolhive-registry-server-leader-election"
)

type mcpServerReconcilerOptions struct {
	namespaces   []string
	requeueAfter time.Duration
	syncWriter   writer.SyncWriter
}

type Option func(*mcpServerReconcilerOptions) error

func WithNamespaces(namespaces ...string) Option {
	return func(o *mcpServerReconcilerOptions) error {
		if o.namespaces == nil {
			o.namespaces = make([]string, 0)
		}
		o.namespaces = append(o.namespaces, namespaces...)
		return nil
	}
}

func WithRequeueAfter(requeueAfter time.Duration) Option {
	return func(o *mcpServerReconcilerOptions) error {
		if requeueAfter <= 0 {
			return fmt.Errorf("requeueAfter must be greater than 0")
		}
		o.requeueAfter = requeueAfter
		return nil
	}
}

func WithSyncWriter(sw writer.SyncWriter) Option {
	return func(o *mcpServerReconcilerOptions) error {
		if sw == nil {
			return fmt.Errorf("sync writer is required")
		}
		o.syncWriter = sw
		return nil
	}
}

// NewMCPServerReconciler creates a new MCPServerReconciler.
func NewMCPServerReconciler(
	ctx context.Context,
	opts ...Option,
) (ctrl.Manager, error) {
	log := log.FromContext(ctx)

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
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), options)
	if err != nil {
		return nil, fmt.Errorf("failed to create manager: %w", err)
	}

	controller := &MCPServerReconciler{
		requeueAfter: o.requeueAfter,
		syncWriter:   o.syncWriter,
	}

	if err := controller.SetupWithManager(mgr); err != nil {
		return nil, fmt.Errorf("failed to setup controller with manager: %w", err)
	}

	go func() {
		if err := mgr.Start(ctx); err != nil {
			log.Error(err, "failed to start manager")
		}
	}()

	return mgr, nil
}
