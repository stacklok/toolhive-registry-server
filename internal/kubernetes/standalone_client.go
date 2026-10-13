package kubernetes

import (
	"context"
	"errors"
	"sync"

	mcp "github.com/stacklok/toolhive/cmd/thv-operator/api/v1beta1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlconfig "sigs.k8s.io/controller-runtime/pkg/client/config"

	"github.com/stacklok/toolhive-registry-server/internal/sync/standalone"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/reconcile"
)

// StandaloneFactory borrows an injected API client or lazily constructs a direct
// client on first preflight. It owns no manager or background work.
type StandaloneFactory struct {
	API client.WithWatch
	mu  sync.Mutex
}

func (f *StandaloneFactory) api() (client.WithWatch, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.API == nil {
		api, err := NewStandaloneClient()
		if err != nil {
			return nil, err
		}
		f.API = api
	}
	return f.API, nil
}

// Preflight checks authoritative lists without starting work or mutating storage.
func (f *StandaloneFactory) Preflight(ctx context.Context, d persistence.SourceDefinition) error {
	api, err := f.api()
	if err != nil {
		return err
	}
	return PreflightStandaloneOperator(ctx, api, d)
}

// Worker constructs an unstarted leased runner borrowing the authoritative client.
func (f *StandaloneFactory) Worker(
	jobs persistence.Jobs, d persistence.SourceDefinition, opts reconcile.Options,
) (standalone.Worker, error) {
	api, err := f.api()
	if err != nil {
		return nil, err
	}
	return NewStandaloneOperator(api, jobs, d, opts, nil)
}

// NewStandaloneClient creates an authoritative direct list/watch client without
// starting a manager, cache, leader election or background reconciliation.
func NewStandaloneClient() (client.WithWatch, error) {
	cfg, err := ctrlconfig.GetConfig()
	if err != nil {
		return nil, errors.New("operator client configuration unavailable")
	}
	scheme := runtime.NewScheme()
	if err := mcp.AddToScheme(scheme); err != nil {
		return nil, err
	}
	api, err := client.NewWithWatch(cfg, client.Options{Scheme: scheme})
	if err != nil {
		return nil, errors.New("operator client unavailable")
	}
	return api, nil
}
