package database

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/stacklok/toolhive-registry-server/internal/service"
)

// newClaimsFilterWith builds a RecordFilter that keeps a record only when the
// caller's claims are non-empty, the record has stored claims, and they match.
// extract retrieves the raw claims JSON from a record; returning ok=false
// causes the filter to reject the record with a type error.
// Returns nil when callerClaims is nil or empty so the caller can skip
// filtering entirely.
func newClaimsFilterWith(
	callerClaims map[string]any,
	extract func(record any) (claims []byte, ok bool),
) service.RecordFilter {
	callerJSON := marshalClaims(callerClaims)
	if callerJSON == nil {
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

// checkClaims returns true only when both callerJSON and recordJSON are
// non-empty and represent the same JSON object.
func checkClaims(callerJSON, recordJSON []byte) bool {
	if len(callerJSON) == 0 || len(recordJSON) == 0 {
		return false
	}
	return claimsMatch(callerJSON, recordJSON)
}
