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

// claimsContain reports whether callerClaims satisfies every claim in recordClaims.
// For each key K in recordClaims the caller must have K, and every value required
// by the record must appear in the caller's value(s) for K.
// Both plain strings and []string values are supported.
//
// An empty-array value (e.g. "teams": []) is vacuously satisfied by any caller
// value for that key — this is intentional since ValidateClaimValues accepts
// empty arrays, and presence of the key is the meaningful signal.
func claimsContain(caller, record map[string]any) bool {
	for k, rv := range record {
		cv, ok := caller[k]
		if !ok {
			return false
		}
		required := toStringSet(rv)
		have := toStringSet(cv)
		for v := range required {
			if _, found := have[v]; !found {
				return false
			}
		}
	}
	return true
}

// claimsEqual returns true when a and b have exactly the same keys and values.
// Used to enforce strict claim consistency on subsequent publishes of the same entry name.
func claimsEqual(a, b map[string]any) bool {
	return claimsContain(a, b) && claimsContain(b, a)
}

// toStringSet normalises a claim value (string, []any of strings, or []string) to a set.
func toStringSet(v any) map[string]struct{} {
	switch val := v.(type) {
	case string:
		return map[string]struct{}{val: {}}
	case []any:
		s := make(map[string]struct{}, len(val))
		for _, elem := range val {
			if str, ok := elem.(string); ok {
				s[str] = struct{}{}
			}
		}
		return s
	case []string:
		s := make(map[string]struct{}, len(val))
		for _, str := range val {
			s[str] = struct{}{}
		}
		return s
	default:
		return map[string]struct{}{}
	}
}
