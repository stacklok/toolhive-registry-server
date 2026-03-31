package database

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/stacklok/toolhive-registry-server/internal/auth"
	"github.com/stacklok/toolhive-registry-server/internal/db"
	"github.com/stacklok/toolhive-registry-server/internal/service"
)

// ---------------------------------------------------------------------------
// Write-path validation (gate checks, publish/delete authorization)
// ---------------------------------------------------------------------------

// validateClaimsSubset checks that callerClaims covers resourceClaims.
// Returns nil if:
//   - callerClaims is nil (anonymous mode — no auth enforcement)
//   - resourceClaims is nil/empty (open resource — no restriction)
//   - the caller is a super-admin (bypasses all claim checks)
//   - callerClaims is a superset of resourceClaims
//
// Returns ErrClaimsInsufficient otherwise.
func validateClaimsSubset(ctx context.Context, callerClaims, resourceClaims map[string]any) error {
	if callerClaims == nil || len(resourceClaims) == 0 {
		return nil
	}
	if auth.IsSuperAdmin(ctx) {
		return nil
	}
	if !claimsContain(callerClaims, resourceClaims) {
		return fmt.Errorf("%w: caller claims do not cover resource claims", service.ErrClaimsInsufficient)
	}
	return nil
}

// validateClaimsSubsetBytes is like validateClaimsSubset but accepts raw JSON
// for resourceClaims. Nil or empty JSON is treated as an open resource.
func validateClaimsSubsetBytes(ctx context.Context, callerClaims map[string]any, resourceClaimsJSON []byte) error {
	if callerClaims == nil || len(resourceClaimsJSON) == 0 {
		return nil
	}
	resourceClaims := db.DeserializeClaims(resourceClaimsJSON)
	return validateClaimsSubset(ctx, callerClaims, resourceClaims)
}

// claimsFromCtx extracts JWT claims from the context as map[string]any.
// Returns nil in anonymous mode (no claims present).
func claimsFromCtx(ctx context.Context) map[string]any {
	jwtClaims := auth.ClaimsFromContext(ctx)
	if jwtClaims == nil {
		return nil
	}
	return map[string]any(jwtClaims)
}

// ---------------------------------------------------------------------------
// Read-path filtering (per-user entry visibility)
// ---------------------------------------------------------------------------

// newClaimsFilterWith builds a RecordFilter that keeps a record only when the
// caller's claims are non-empty, the record has stored claims, and they match.
// extract retrieves the raw claims JSON from a record; returning ok=false
// causes the filter to reject the record with a type error.
// Returns nil when callerClaims is nil or empty so the caller can skip
// filtering entirely. Also returns nil for super-admin users (they see all
// entries regardless of claims).
func newClaimsFilterWith(
	ctx context.Context,
	callerClaims map[string]any,
	extract func(record any) (claims []byte, ok bool),
) service.RecordFilter {
	callerJSON := marshalClaims(callerClaims)
	if callerJSON == nil {
		return nil
	}
	if auth.IsSuperAdmin(ctx) {
		return nil
	}
	return func(_ context.Context, record any) (bool, error) {
		recordJSON, ok := extract(record)
		if !ok {
			return false, fmt.Errorf("unexpected record type: %T", record)
		}
		return checkClaims(callerJSON, recordJSON), nil
	}
}

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

// marshalClaims serializes callerClaims to JSON. Returns nil if the map is nil
// or empty, or if serialization fails (treated as "no claims").
func marshalClaims(callerClaims map[string]any) []byte {
	if len(callerClaims) == 0 {
		return nil
	}
	b, err := json.Marshal(callerClaims)
	if err != nil {
		return nil
	}
	return b
}

// checkClaims returns true only when callerJSON satisfies every claim in recordJSON.
// The caller's claims must be a superset of the record's claims (containment, not equality).
func checkClaims(callerJSON, recordJSON []byte) bool {
	if len(callerJSON) == 0 || len(recordJSON) == 0 {
		return false
	}
	var caller, record map[string]any
	if err := json.Unmarshal(callerJSON, &caller); err != nil {
		return false
	}
	if err := json.Unmarshal(recordJSON, &record); err != nil {
		return false
	}
	return claimsContain(caller, record)
}
