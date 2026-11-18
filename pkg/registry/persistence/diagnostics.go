package persistence

import (
	"context"
	"time"
)

// DiagnosticVersion is a lightweight raw version projection, with no payload or
// claims. CreatedAt is the published timestamp; historical NULLs read as zero.
// Timestamps are UTC storage metadata, not revisions. Reads do not touch them.
type DiagnosticVersion struct {
	Kind        EntryKind
	Name        string
	Version     string
	Title       string
	Description string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// ViewDiagnosticVersion retains source provenance for shadowed duplicates.
type ViewDiagnosticVersion struct {
	DiagnosticVersion
	SourceName string
	SourceID   string
	Position   int32
}

// EntryDiagnostics exposes COMPLETE raw source/view listings: all three kinds,
// all versions, including shadowed duplicates. It is not a consumer reader or an
// authorization boundary. Hosts authorize calls and control diagnostic exposure.
// Each call resolves existence, links and rows in one coherent storage snapshot.
// Missing definitions return ErrNotFound; existing empty ones return non-nil [].
// Ordering is ascending byte-lexical (name, version, kind), then source position
// for view ties, then source name for equal historical positions. There is no
// implicit cap, cursor, dedup or semantic version sort.
// Returned slices do not alias storage. Backend errors are sanitized; context
// cancellation/deadlines propagate. Coherence is per call, not across calls.
type EntryDiagnostics interface {
	ListSourceVersions(ctx context.Context, name string) ([]DiagnosticVersion, error)
	ListViewVersions(ctx context.Context, name string) ([]ViewDiagnosticVersion, error)
}

// SourceStatusReader inspects the authoritative source incarnation and sanitized
// attempt/baseline metadata in one call. Jobs satisfies it without exposing job
// mutation, lease identities or source configuration through this capability.
type SourceStatusReader interface {
	Status(ctx context.Context, name string) (JobStatus, error)
}
