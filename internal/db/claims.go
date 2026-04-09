// Package db provides shared database utilities.
package db

import (
	"encoding/json"
	"fmt"
)

// SerializeClaims serializes a claims map to JSON bytes for database storage.
// Returns nil for empty/nil claims maps or on marshal error.
func SerializeClaims(claims map[string]any) []byte {
	if len(claims) == 0 {
		return nil
	}
	data, err := json.Marshal(claims)
	if err != nil {
		return nil
	}
	return data
}

// DeserializeClaims deserializes claims from JSON bytes to a map.
// Returns nil for empty/nil data or on unmarshal error.
func DeserializeClaims(data []byte) map[string]any {
	if len(data) == 0 {
		return nil
	}
	var claims map[string]any
	if err := json.Unmarshal(data, &claims); err != nil {
		return nil
	}
	return claims
}

// ValidateClaimValues checks that all claim values are strings or arrays of strings.
// This is the format required by the authorization system's JSONB containment queries.
func ValidateClaimValues(claims map[string]any) error {
	for key, val := range claims {
		switch v := val.(type) {
		case string:
			// OK
		case []any:
			for i, elem := range v {
				if _, ok := elem.(string); !ok {
					return fmt.Errorf("claim %q: array element [%d] must be a string, got %T", key, i, elem)
				}
			}
		case []string:
			// OK — may come from YAML deserialization
		default:
			return fmt.Errorf("claim %q: value must be a string or array of strings, got %T", key, val)
		}
	}
	return nil
}
