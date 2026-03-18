package auth

import (
	"context"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaimsContext(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		claims jwt.MapClaims
		verify func(t *testing.T, got jwt.MapClaims)
	}{
		{
			name: "round-trip with string claims",
			claims: jwt.MapClaims{
				"sub": "user-123",
				"iss": "https://auth.example.com",
			},
			verify: func(t *testing.T, got jwt.MapClaims) {
				t.Helper()
				require.NotNil(t, got)
				assert.Equal(t, "user-123", got["sub"])
				assert.Equal(t, "https://auth.example.com", got["iss"])
			},
		},
		{
			name: "claims with various types",
			claims: jwt.MapClaims{
				"sub":   "user-456",
				"exp":   float64(1700000000),
				"roles": []interface{}{"admin", "editor"},
				"metadata": map[string]interface{}{
					"org": "acme",
				},
			},
			verify: func(t *testing.T, got jwt.MapClaims) {
				t.Helper()
				require.NotNil(t, got)
				assert.Equal(t, "user-456", got["sub"])
				assert.Equal(t, float64(1700000000), got["exp"])
				assert.Equal(t, []interface{}{"admin", "editor"}, got["roles"])
				metadata, ok := got["metadata"].(map[string]interface{})
				require.True(t, ok)
				assert.Equal(t, "acme", metadata["org"])
			},
		},
		{
			name:   "empty claims map",
			claims: jwt.MapClaims{},
			verify: func(t *testing.T, got jwt.MapClaims) {
				t.Helper()
				require.NotNil(t, got)
				assert.Empty(t, got)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := ContextWithClaims(context.Background(), tt.claims)
			got := ClaimsFromContext(ctx)
			tt.verify(t, got)
		})
	}
}

func TestClaimsFromContext_NoClaims(t *testing.T) {
	t.Parallel()

	got := ClaimsFromContext(context.Background())
	assert.Nil(t, got)
}

func TestContextWithClaims_LastOneWins(t *testing.T) {
	t.Parallel()

	first := jwt.MapClaims{"sub": "user-1"}
	second := jwt.MapClaims{"sub": "user-2"}

	ctx := ContextWithClaims(context.Background(), first)
	ctx = ContextWithClaims(ctx, second)

	got := ClaimsFromContext(ctx)
	require.NotNil(t, got)
	assert.Equal(t, "user-2", got["sub"])
}
