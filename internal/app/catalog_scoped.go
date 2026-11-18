package app

import (
	"context"

	"github.com/stacklok/toolhive-registry-server/internal/app/storage"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

// scopedSources applies the server's source rules to definitions written
// through the library's admin API (Cedar mode): a Kubernetes source gets the
// same effective namespace scope and preflight as a config source, and every
// definition passes the same checks as config sources.
type scopedSources struct {
	persistence.Sources
	watch     string
	preflight func(context.Context, persistence.SourceDefinition) error
}

// CreateSource validates and scopes d before creating it.
func (s *scopedSources) CreateSource(ctx context.Context, d persistence.SourceDefinition) (persistence.SourceDefinition, error) {
	d, err := s.prepare(ctx, d)
	if err != nil {
		return d, err
	}
	return s.Sources.CreateSource(ctx, d)
}

// UpdateSource validates and scopes d before updating source name.
func (s *scopedSources) UpdateSource(
	ctx context.Context, name string, d persistence.SourceDefinition,
) (persistence.SourceDefinition, error) {
	d, err := s.prepare(ctx, d)
	if err != nil {
		return d, err
	}
	return s.Sources.UpdateSource(ctx, name, d)
}

func (s *scopedSources) prepare(ctx context.Context, d persistence.SourceDefinition) (persistence.SourceDefinition, error) {
	if d.Kubernetes != nil {
		namespaces, err := storage.EffectiveNamespaces(d.Kubernetes.Namespaces, s.watch)
		if err != nil {
			return d, err
		}
		spec := *d.Kubernetes
		spec.Namespaces = namespaces
		d.Kubernetes = &spec
	}
	return d, s.preflight(ctx, d)
}
