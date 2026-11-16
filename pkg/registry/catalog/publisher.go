package catalog

import (
	"context"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

// Publisher adds/removes exact versions in the singleton managed source. A host
// owns authorization; there is no source/view destination or implicit creation.
type Publisher interface {
	persistence.ManagedPublisher
}

type publisher struct{ backend persistence.ManagedPublisher }

// NewPublisher borrows an initialized non-nil backend, performs no IO, starts
// no work and owns no resources. Typed nil implementations are outside the contract.
func NewPublisher(backend persistence.ManagedPublisher) (Publisher, error) {
	if backend == nil {
		return nil, persistence.ErrInvalid
	}
	return &publisher{backend: backend}, nil
}

func (p *publisher) Publish(ctx context.Context, entry persistence.Entry) (persistence.Entry, error) {
	if err := entry.Validate(); err != nil {
		return persistence.Entry{}, err
	}
	return p.backend.Publish(ctx, entry)
}

// DeleteManaged interprets every version, including "latest", literally.
func (p *publisher) DeleteManaged(ctx context.Context, kind persistence.EntryKind, name, version string) error {
	if err := kind.Validate(); err != nil {
		return err
	}
	if name == "" || version == "" {
		return persistence.ErrInvalid
	}
	return p.backend.DeleteManaged(ctx, kind, name, version)
}
