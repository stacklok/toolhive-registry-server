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
	// Claims receives the authz-claims annotations the workers observe; set in
	// claims mode (with or without auth.authz), nil in Cedar mode.
	Claims *AnnotationClaims
	// IgnoreClaimAnnotations leaves authz-claims annotations unread, for
	// Cedar mode: claims carry no meaning there, so a malformed annotation must
	// not keep a resource out of the registry. It excludes Claims.
	IgnoreClaimAnnotations bool
	mu                     sync.Mutex
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
	return preflightStandaloneOperator(ctx, api, d, f.IgnoreClaimAnnotations)
}

// Worker constructs an unstarted leased runner borrowing the authoritative client.
func (f *StandaloneFactory) Worker(
	jobs persistence.Jobs, d persistence.SourceDefinition, opts reconcile.Options,
) (standalone.Worker, error) {
	api, err := f.api()
	if err != nil {
		return nil, err
	}
	return newStandaloneOperator(api, jobs, d, opts, f.Claims, f.IgnoreClaimAnnotations)
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
