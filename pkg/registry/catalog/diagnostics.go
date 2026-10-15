package catalog

import (
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

//go:generate mockgen -destination=mocks/administration.go -package=mocks github.com/stacklok/toolhive-registry-server/pkg/registry/persistence ManagedPublisher,EntryDiagnostics,SourceStatusReader

// Diagnostics exposes complete raw versions and sanitized job status, not
// consumer selection, source configuration, readiness or job mutation. Hosts own
// authorization. Status is authoritative for its own SourceID; separate calls
// do not promise an atomic definition/status observation.
type Diagnostics interface {
	persistence.EntryDiagnostics
	persistence.SourceStatusReader
}

type diagnostics struct {
	persistence.EntryDiagnostics
	persistence.SourceStatusReader
}

// NewDiagnostics accepts independently initialized non-nil narrow capabilities.
// It performs no IO, starts no work and owns/closes neither dependency. Typed nil
// implementations are outside the contract. No separate definition lookup is
// needed: status resolves its own incarnation, and listings resolve existence.
func NewDiagnostics(entries persistence.EntryDiagnostics, status persistence.SourceStatusReader) (Diagnostics, error) {
	if entries == nil || status == nil {
		return nil, persistence.ErrInvalid
	}
	return &diagnostics{EntryDiagnostics: entries, SourceStatusReader: status}, nil
}
