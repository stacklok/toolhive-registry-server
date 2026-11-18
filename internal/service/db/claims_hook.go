package database

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/stacklok/toolhive-registry-server/internal/db/sqlc"
	"github.com/stacklok/toolhive-registry-server/internal/service"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/postgres"
)

// EntryClaimsProvider supplies per-entry claims that replace a synced
// source's claims for individual server entries: the Kubernetes
// toolhive.stacklok.dev/authz-claims annotation. It returns marshaled claims
// keyed by server name; a nil map means no overrides.
type EntryClaimsProvider interface {
	EntryClaims(sourceName string) map[string][]byte
}

// ClaimsHook keeps registry_entry.claims, which the claims-free library
// never writes, in step with library entry writes. It runs inside the
// library's write transaction:
//   - Snapshot: every entry of the source takes the source's claims, except
//     server entries with a per-entry override (v1.5.4 sync semantics).
//   - Publish: the caller must see the managed source's claims, an existing
//     name must already carry the same claims, and the new version's entry
//     stores the request's claims.
//   - Delete: the caller must see the entry's claims.
//
// Publish and delete read the caller and request claims that the service
// attached to the context; a write without them (for example, from a library
// host) stores no claims and skips the caller gates.
type ClaimsHook struct {
	overrides EntryClaimsProvider
}

var _ postgres.EntryWriteHook = (*ClaimsHook)(nil)

// NewClaimsHook builds the hook. overrides may be nil.
func NewClaimsHook(overrides EntryClaimsProvider) *ClaimsHook {
	return &ClaimsHook{overrides: overrides}
}

type entryWriteKey struct{}

// entryWriteRequest carries the claims-mode inputs of one publish or delete.
// gate is the caller's JWT claims for gate checks, nil when authorization is
// off or the caller is anonymous; stored is the marshaled claims to store.
type entryWriteRequest struct {
	gate   map[string]any
	stored []byte
}

func withEntryWriteRequest(ctx context.Context, req entryWriteRequest) context.Context {
	return context.WithValue(ctx, entryWriteKey{}, req)
}

func entryWriteRequestFrom(ctx context.Context) (entryWriteRequest, bool) {
	req, ok := ctx.Value(entryWriteKey{}).(entryWriteRequest)
	return req, ok
}

// BeforeEntryWrite applies the caller gates of publish and delete.
func (*ClaimsHook) BeforeEntryWrite(ctx context.Context, tx pgx.Tx, w postgres.EntryWrite) error {
	req, ok := entryWriteRequestFrom(ctx)
	if !ok {
		return nil
	}
	q := sqlc.New(tx)
	sourceID, err := uuid.Parse(w.SourceID)
	if err != nil {
		return err
	}
	switch w.Op {
	case postgres.EntryWritePublish:
		source, err := q.GetSourceByName(ctx, w.SourceName)
		if err != nil {
			return fmt.Errorf("failed to get managed source: %w", err)
		}
		if err := validateClaimsVisibleBytes(ctx, req.gate, source.Claims); err != nil {
			return err
		}
		existing, err := q.GetRegistryEntryByName(ctx, sqlc.GetRegistryEntryByNameParams{
			SourceID: sourceID, EntryType: sqlc.EntryType(w.Kind), Name: w.Name,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("failed to get or create registry entry: %w", err)
		}
		return checkClaimConsistency(req.stored, existing.Claims)
	case postgres.EntryWriteDelete:
		if req.gate == nil {
			return nil
		}
		existing, err := q.GetRegistryEntryByName(ctx, sqlc.GetRegistryEntryByNameParams{
			SourceID: sourceID, EntryType: sqlc.EntryType(w.Kind), Name: w.Name,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: %s@%s", service.ErrNotFound, w.Name, w.Version)
		}
		if err != nil {
			return fmt.Errorf("failed to look up registry entry: %w", err)
		}
		return validateClaimsVisibleBytes(ctx, req.gate, existing.Claims)
	case postgres.EntryWriteSnapshot:
		return nil // synced snapshots have no caller gate
	default:
		return nil
	}
}

// AfterEntryWrite stores entry claims for snapshots and publishes.
func (h *ClaimsHook) AfterEntryWrite(ctx context.Context, tx pgx.Tx, w postgres.EntryWrite) error {
	q := sqlc.New(tx)
	sourceID, err := uuid.Parse(w.SourceID)
	if err != nil {
		return err
	}
	switch w.Op {
	case postgres.EntryWriteSnapshot:
		if err := q.SetSourceEntryClaimsFromSource(ctx, sourceID); err != nil {
			return fmt.Errorf("failed to store entry claims: %w", err)
		}
		if h.overrides == nil {
			return nil
		}
		for name, entryClaims := range h.overrides.EntryClaims(w.SourceName) {
			if err := q.SetEntryClaims(ctx, sqlc.SetEntryClaimsParams{
				SourceID: sourceID, EntryType: sqlc.EntryTypeMCP, Name: name, Claims: entryClaims,
			}); err != nil {
				return fmt.Errorf("failed to store entry claims: %w", err)
			}
		}
		return nil
	case postgres.EntryWritePublish:
		req, _ := entryWriteRequestFrom(ctx)
		if err := q.SetEntryClaims(ctx, sqlc.SetEntryClaimsParams{
			SourceID: sourceID, EntryType: sqlc.EntryType(w.Kind), Name: w.Name, Claims: req.stored,
		}); err != nil {
			return fmt.Errorf("failed to store entry claims: %w", err)
		}
		return nil
	case postgres.EntryWriteDelete:
		return nil // the deleted version's entry keeps its claims
	default:
		return nil
	}
}
