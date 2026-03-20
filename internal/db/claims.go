// Package db provides shared database utilities.
package db

import "encoding/json"

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
