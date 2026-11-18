package claims

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/stacklok/toolhive-registry-server/internal/auth"
	"github.com/stacklok/toolhive-registry-server/internal/db/sqlc"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

// ErrRegistryForbidden reports that the caller may not read a registry. The
// consumer API maps it to 403 with this exact message, as v1.5.4 did.
var ErrRegistryForbidden = errors.New("forbidden: insufficient claims for registry")

// RegistryLookup reads a registry's stored claims. *sqlc.Queries satisfies it.
type RegistryLookup interface {
	GetRegistryByName(ctx context.Context, name string) (sqlc.Registry, error)
}

// Reader is the claims-mode persistence.ViewEntryReader. For a caller with
// claims it first checks the registry gate, then reads through the library
// with claims filtering on, so an entry the caller cannot see neither appears
// nor hides a lower-priority source's entry of the same name. Anonymous
// callers and superadmins read unfiltered, as in v1.5.4. Use it only when
// authorization is configured; without authz, use the library reader directly.
type Reader struct {
	next       persistence.ViewEntryReader
	registries RegistryLookup
}

var _ persistence.ViewEntryReader = (*Reader)(nil)

// NewReader wraps the library's view reader.
func NewReader(next persistence.ViewEntryReader, registries RegistryLookup) (*Reader, error) {
	if next == nil || registries == nil {
		return nil, fmt.Errorf("%w: claims reader needs a view reader and a registry lookup", persistence.ErrInvalid)
	}
	return &Reader{next: next, registries: registries}, nil
}

// ListViewEntries lists one page of the registry the caller may see.
func (r *Reader) ListViewEntries(ctx context.Context, view string, query persistence.ViewQuery) (persistence.EntryPage, error) {
	ctx, err := r.scope(ctx, view)
	if err != nil {
		return persistence.EntryPage{}, err
	}
	return r.next.ListViewEntries(ctx, view, query)
}

// GetViewEntry reads one entry the caller may see; a hidden higher-priority
// entry falls through to the next visible source.
func (r *Reader) GetViewEntry(
	ctx context.Context, view string, name persistence.EntryName, selector persistence.VersionSelector,
) (persistence.Entry, error) {
	ctx, err := r.scope(ctx, view)
	if err != nil {
		return persistence.Entry{}, err
	}
	return r.next.GetViewEntry(ctx, view, name, selector)
}

// Caller returns the caller's JWT claims when claim checks apply to the
// request, and nil when they do not: anonymous callers and superadmins.
func Caller(ctx context.Context) map[string]any {
	jwt := auth.ClaimsFromContext(ctx)
	if jwt == nil || auth.IsSuperAdmin(ctx) {
		return nil
	}
	return map[string]any(jwt)
}

// GateVisible applies the gate for an existing resource's stored claims: a
// nil caller passes, a resource without claims is denied, and otherwise the
// read-path rule decides.
func GateVisible(caller map[string]any, resourceJSON []byte) bool {
	if caller == nil {
		return true
	}
	resource := Deserialize(resourceJSON)
	return len(resource) > 0 && Visible(caller, resource)
}

func (r *Reader) scope(ctx context.Context, view string) (context.Context, error) {
	caller := Caller(ctx)
	if caller == nil {
		return ctx, nil
	}
	registry, err := r.registries.GetRegistryByName(ctx, view)
	if errors.Is(err, pgx.ErrNoRows) {
		return ctx, nil // the library reports the missing registry as not found
	}
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return ctx, err
		}
		return ctx, persistence.ErrUnavailable
	}
	if !GateVisible(caller, registry.Claims) {
		return ctx, ErrRegistryForbidden
	}
	if len(caller) == 0 {
		return ctx, nil // v1.5.4 applied no entry filter for an empty claim set
	}
	callerJSON, err := json.Marshal(caller)
	if err != nil {
		return ctx, persistence.ErrUnavailable
	}
	return WithViewCaller(ctx, callerJSON), nil
}
