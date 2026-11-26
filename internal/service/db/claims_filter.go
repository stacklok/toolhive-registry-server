package database

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/stacklok/toolhive-registry-server/internal/auth"
	"github.com/stacklok/toolhive-registry-server/internal/claims"
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

// The matching rules themselves live in internal/claims, shared with the
// claims-mode list reader and its SQL counterpart (auth.md §3).
var (
	claimsContain = claims.Contain
	claimsVisible = claims.Visible
	claimsEqual   = claims.Equal
)
