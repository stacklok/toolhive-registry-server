package db_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/stacklok/toolhive-registry-server/internal/db"
)

func TestSerializeClaims(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		claims   map[string]any
		wantNil  bool
		wantJSON string
	}{
		{
			name:    "nil map returns nil",
			claims:  nil,
			wantNil: true,
		},
		{
			name:    "empty map returns nil",
			claims:  map[string]any{},
			wantNil: true,
		},
		{
			name:     "single string claim",
			claims:   map[string]any{"issuer": "https://example.com"},
			wantJSON: `{"issuer":"https://example.com"}`,
		},
		{
			name: "multiple claims",
			claims: map[string]any{
				"issuer":   "https://example.com",
				"audience": "my-app",
			},
		},
		{
			name: "claim with array value",
			claims: map[string]any{
				"scopes": []string{"read", "write", "admin"},
			},
		},
		{
			name: "mixed value types",
			claims: map[string]any{
				"issuer":   "https://example.com",
				"verified": true,
				"count":    float64(42),
				"tags":     []string{"a", "b"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := db.SerializeClaims(tt.claims)

			if tt.wantNil {
				assert.Nil(t, result)
				return
			}

			assert.NotNil(t, result)

			// For single-key cases where we know exact JSON, verify it
			if tt.wantJSON != "" {
				assert.JSONEq(t, tt.wantJSON, string(result))
			}

			// Verify the result is valid JSON by round-tripping
			deserialized := db.DeserializeClaims(result)
			assert.NotNil(t, deserialized)
			assert.Equal(t, len(tt.claims), len(deserialized))
		})
	}
}

func TestDeserializeClaims(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		data     []byte
		expected map[string]any
	}{
		{
			name:     "nil input returns nil",
			data:     nil,
			expected: nil,
		},
		{
			name:     "empty slice returns nil",
			data:     []byte{},
			expected: nil,
		},
		{
			name:     "invalid JSON returns nil",
			data:     []byte(`{not valid json`),
			expected: nil,
		},
		{
			name:     "empty JSON object",
			data:     []byte(`{}`),
			expected: map[string]any{},
		},
		{
			name: "single string claim",
			data: []byte(`{"issuer":"https://example.com"}`),
			expected: map[string]any{
				"issuer": "https://example.com",
			},
		},
		{
			name: "multiple claims",
			data: []byte(`{"audience":"my-app","issuer":"https://example.com"}`),
			expected: map[string]any{
				"issuer":   "https://example.com",
				"audience": "my-app",
			},
		},
		{
			name: "claim with array value",
			data: []byte(`{"scopes":["read","write"]}`),
			expected: map[string]any{
				"scopes": []any{"read", "write"},
			},
		},
		{
			name: "claim with nested object",
			data: []byte(`{"metadata":{"key":"value"}}`),
			expected: map[string]any{
				"metadata": map[string]any{"key": "value"},
			},
		},
		{
			name: "claim with numeric value",
			data: []byte(`{"count":42}`),
			expected: map[string]any{
				"count": float64(42),
			},
		},
		{
			name: "claim with boolean value",
			data: []byte(`{"verified":true}`),
			expected: map[string]any{
				"verified": true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := db.DeserializeClaims(tt.data)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSerializeDeserializeRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		claims map[string]any
	}{
		{
			name:   "nil map round-trips to nil",
			claims: nil,
		},
		{
			name:   "empty map round-trips to nil",
			claims: map[string]any{},
		},
		{
			name: "single claim round-trips",
			claims: map[string]any{
				"issuer": "https://example.com",
			},
		},
		{
			name: "multiple string claims round-trip",
			claims: map[string]any{
				"issuer":   "https://example.com",
				"audience": "my-app",
				"subject":  "user-123",
			},
		},
		{
			name: "claims with array values round-trip",
			claims: map[string]any{
				"scopes": []any{"read", "write", "admin"},
				"roles":  []any{"viewer"},
			},
		},
		{
			name: "mixed types round-trip",
			claims: map[string]any{
				"issuer":   "https://example.com",
				"verified": true,
				"count":    float64(7),
				"tags":     []any{"a", "b"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			serialized := db.SerializeClaims(tt.claims)
			deserialized := db.DeserializeClaims(serialized)

			if len(tt.claims) == 0 {
				assert.Nil(t, deserialized)
				return
			}

			assert.Equal(t, tt.claims, deserialized)
		})
	}
}
