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

// checkClaimConsistency verifies that incoming claims match the existing entry's claims.
// Both-nil is OK (no claims on either side). Both-non-nil are compared for equality.
// One-nil-one-non-nil is a mismatch — the publisher must be explicit and consistent.
func checkClaimConsistency(incomingJSON, existingJSON []byte) error {
	incomingEmpty := len(incomingJSON) == 0
	existingEmpty := len(existingJSON) == 0

	if incomingEmpty && existingEmpty {
		return nil
	}
	if incomingEmpty != existingEmpty {
		return fmt.Errorf("%w: claims do not match existing entry", service.ErrClaimsMismatch)
	}

	var incoming, existing map[string]any
	if err := json.Unmarshal(incomingJSON, &incoming); err != nil {
		return fmt.Errorf("failed to unmarshal incoming claims: %w", err)
	}
	if err := json.Unmarshal(existingJSON, &existing); err != nil {
		return fmt.Errorf("failed to unmarshal existing claims: %w", err)
	}
	if !claimsEqual(incoming, existing) {
		return fmt.Errorf("%w: claims do not match existing entry", service.ErrClaimsMismatch)
	}
	return nil
}

// validateClaimsSubset checks that callerClaims cover resourceClaims using the
// write-path containment rule (claimsContain — AND within arrays). Use it when
// the caller supplies NEW claims for a resource (create/update/publish/
// update-claims): the caller must hold every value, or they could stamp a
// resource with broader visibility than their own identity allows (auth.md §5).
//
// Returns nil if:
//   - callerClaims is nil (caller has opted out of the gate — callers pass
//     nil when skipAuthz is enabled or in anonymous mode)
//   - the caller is a super-admin (bypasses all claim checks)
//   - callerClaims is a superset of resourceClaims
//
// Returns ErrClaimsInsufficient otherwise, including when resourceClaims
// is nil/empty (default-deny on unlabeled resources — see auth.md §4).
func validateClaimsSubset(ctx context.Context, callerClaims, resourceClaims map[string]any) error {
	return validateClaimsWith(ctx, callerClaims, resourceClaims, claimsContain)
}

// validateClaimsVisible checks whether callerClaims may see or access a resource
// whose claims are resourceClaims, using the read-path visibility rule
// (claimsVisible — OR within arrays). Use it for access gates and any check
// against an EXISTING resource's stored claims (read/list/delete, the registry
// access gate, referencing a source) so the single-resource gate agrees with
// the list filter (auth.md §4). Same nil-caller / super-admin / default-deny
// short-circuits as validateClaimsSubset.
func validateClaimsVisible(ctx context.Context, callerClaims, resourceClaims map[string]any) error {
	return validateClaimsWith(ctx, callerClaims, resourceClaims, claimsVisible)
}

// validateClaimsWith is the shared claim gate. It applies the uniform
// short-circuits — nil caller (authz off / anonymous) and super-admin bypass,
// empty resource claims are default-deny (auth.md §4) — then defers the claim
// comparison to match: claimsContain for write-path subset, claimsVisible for
// read-path visibility.
func validateClaimsWith(
	ctx context.Context,
	callerClaims, resourceClaims map[string]any,
	match func(caller, record map[string]any) bool,
) error {
	if callerClaims == nil {
		return nil
	}
	if auth.IsSuperAdmin(ctx) {
		return nil
	}
	if len(resourceClaims) == 0 {
		return fmt.Errorf("%w: resource has no claims", service.ErrClaimsInsufficient)
	}
	if !match(callerClaims, resourceClaims) {
		return fmt.Errorf("%w: caller claims do not cover resource claims", service.ErrClaimsInsufficient)
	}
	return nil
}

// validateClaimsVisibleBytes is like validateClaimsVisible but accepts raw JSON
// for resourceClaims. An empty resource JSON is default-deny when callerClaims
// is non-nil (unlabeled resources are invisible to claim-bearing callers).
func validateClaimsVisibleBytes(ctx context.Context, callerClaims map[string]any, resourceClaimsJSON []byte) error {
	if callerClaims == nil {
		return nil
	}
	resourceClaims := db.DeserializeClaims(resourceClaimsJSON)
	return validateClaimsVisible(ctx, callerClaims, resourceClaims)
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
// Returns nil (no filter applied — every record visible) when:
//   - callerClaims is nil/empty (callers pass nil when skipAuthz is enabled
//     or in anonymous mode)
//   - the caller is a super-admin (uniform bypass)
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

// checkClaims reports whether a caller with callerJSON may see a record whose
// claims are recordJSON. It applies the read-path visibility rule via
// claimsVisible (AND across keys, OR within array values) — not the write-path
// containment rule (claimsContain).
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
	return claimsVisible(caller, record)
}

// claimsContain reports whether callerClaims satisfies every claim in recordClaims
// using the write-path / subset rule: AND across keys, AND within arrays
// (containment). It backs validateClaimsSubset and claimsEqual, where the caller
// must cover *all* of a resource's values or they could create resources with
// broader visibility than their own identity (auth.md §5). Read-path visibility
// uses claimsVisible (OR within arrays) — do not swap one for the other.
func claimsContain(caller, record map[string]any) bool {
	return claimsMatch(caller, record, subsetOf)
}

// claimsVisible reports whether a caller may see a record whose claims are
// recordClaims using the read-path / visibility rule: AND across keys, OR within
// arrays. A record tagged team:[eng,data] is an allow-list — visible to a caller
// in *either* team (auth.md §3). This is the deliberate mirror of claimsContain's
// within-array direction; keep them separate.
func claimsVisible(caller, record map[string]any) bool {
	return claimsMatch(caller, record, overlaps)
}

// claimsMatch runs the key-level contract shared by both claim rules and defers
// the within-array decision to matchValues (subsetOf for containment, overlaps
// for visibility). The shared contract, identical for both rules, is:
//
//   - AND across keys: every key in the record must be satisfied.
//   - The caller must hold each record key (absent key on the caller → fail).
//   - An empty-array record value (e.g. "teams": []) is vacuously satisfied by
//     any caller value for that key — presence of the key is the meaningful
//     signal, matching what ValidateClaimValues accepts.
//   - Fails closed (returns false) on unsupported record value types (nil,
//     number, nested object, mixed array). ValidateClaimValues blocks these at
//     the API edge, but rows persisted via direct DB writes or future sync paths
//     could carry them — the gate must not treat them as vacuously satisfied.
//
// Keeping the contract in one place is deliberate: the two rules must differ
// only in the within-array test, never in the key-level handling (auth.md §3).
func claimsMatch(caller, record map[string]any, matchValues func(required, have map[string]struct{}) bool) bool {
	for k, rv := range record {
		if !isValidClaimValue(rv) {
			return false
		}
		cv, ok := caller[k]
		if !ok {
			return false
		}
		required := toStringSet(rv)
		if len(required) == 0 {
			continue // empty array: presence of the key is enough
		}
		have := toStringSet(cv)
		if !matchValues(required, have) {
			return false
		}
	}
	return true
}

// subsetOf reports whether every element of required is present in have
// (containment — the write-path within-array rule).
func subsetOf(required, have map[string]struct{}) bool {
	for v := range required {
		if _, found := have[v]; !found {
			return false
		}
	}
	return true
}

// overlaps reports whether two string sets share at least one element
// (the read-path within-array rule).
func overlaps(a, b map[string]struct{}) bool {
	// Iterate the smaller set for fewer lookups.
	if len(b) < len(a) {
		a, b = b, a
	}
	for v := range a {
		if _, found := b[v]; found {
			return true
		}
	}
	return false
}

// isValidClaimValue reports whether v is a supported claim value (string,
// []string, or []any of strings). Used by claimsContain to fail closed on
// rows whose values bypassed ValidateClaimValues.
func isValidClaimValue(v any) bool {
	switch val := v.(type) {
	case string:
		return true
	case []string:
		return true
	case []any:
		for _, elem := range val {
			if _, ok := elem.(string); !ok {
				return false
			}
		}
		return true
	default:
		return false
	}
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
