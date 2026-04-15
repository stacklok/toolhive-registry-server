package database

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/auth"
	"github.com/stacklok/toolhive-registry-server/internal/service"
)

func TestValidateClaimsSubset(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		callerClaims   map[string]any
		resourceClaims map[string]any
		superAdmin     bool
		wantErr        error
	}{
		{
			name:           "nil caller claims (anonymous) returns nil",
			callerClaims:   nil,
			resourceClaims: map[string]any{"sub": "user1"},
			wantErr:        nil,
		},
		{
			name:           "nil resource claims returns nil",
			callerClaims:   map[string]any{"sub": "user1"},
			resourceClaims: nil,
			wantErr:        nil,
		},
		{
			name:           "empty resource claims returns nil",
			callerClaims:   map[string]any{"sub": "user1"},
			resourceClaims: map[string]any{},
			wantErr:        nil,
		},
		{
			name:           "caller covers resource with exact match",
			callerClaims:   map[string]any{"sub": "user1"},
			resourceClaims: map[string]any{"sub": "user1"},
			wantErr:        nil,
		},
		{
			name:           "caller is superset of resource",
			callerClaims:   map[string]any{"sub": "user1", "org": "acme"},
			resourceClaims: map[string]any{"sub": "user1"},
			wantErr:        nil,
		},
		{
			name:           "caller missing required key returns ErrClaimsInsufficient",
			callerClaims:   map[string]any{"org": "acme"},
			resourceClaims: map[string]any{"sub": "user1"},
			wantErr:        service.ErrClaimsInsufficient,
		},
		{
			name:           "caller has wrong value returns ErrClaimsInsufficient",
			callerClaims:   map[string]any{"sub": "user2"},
			resourceClaims: map[string]any{"sub": "user1"},
			wantErr:        service.ErrClaimsInsufficient,
		},
		{
			name:           "super-admin bypasses claim checks",
			callerClaims:   map[string]any{"sub": "admin"},
			resourceClaims: map[string]any{"sub": "user1"},
			superAdmin:     true,
			wantErr:        nil,
		},
		{
			name:           "array claim values caller covers resource",
			callerClaims:   map[string]any{"roles": []any{"a", "b", "c"}},
			resourceClaims: map[string]any{"roles": []any{"a", "b"}},
			wantErr:        nil,
		},
		{
			name:           "array claim values caller missing value returns ErrClaimsInsufficient",
			callerClaims:   map[string]any{"roles": []any{"a"}},
			resourceClaims: map[string]any{"roles": []any{"a", "b"}},
			wantErr:        service.ErrClaimsInsufficient,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()
			if tt.superAdmin {
				ctx = auth.ContextWithRoles(ctx, []auth.Role{auth.RoleSuperAdmin})
			}

			err := validateClaimsSubset(ctx, tt.callerClaims, tt.resourceClaims)

			if tt.wantErr != nil {
				require.Error(t, err)
				assert.True(t, errors.Is(err, tt.wantErr), "expected %v, got %v", tt.wantErr, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateClaimsSubsetBytes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		callerClaims map[string]any
		resourceJSON []byte
		wantErr      error
	}{
		{
			name:         "nil caller claims returns nil",
			callerClaims: nil,
			resourceJSON: []byte(`{"sub":"user1"}`),
			wantErr:      nil,
		},
		{
			name:         "nil resource JSON returns nil",
			callerClaims: map[string]any{"sub": "user1"},
			resourceJSON: nil,
			wantErr:      nil,
		},
		{
			name:         "empty resource JSON returns nil",
			callerClaims: map[string]any{"sub": "user1"},
			resourceJSON: []byte{},
			wantErr:      nil,
		},
		{
			name:         "matching JSON returns nil",
			callerClaims: map[string]any{"sub": "user1"},
			resourceJSON: mustMarshalAuthz(t, map[string]any{"sub": "user1"}),
			wantErr:      nil,
		},
		{
			name:         "non-matching JSON returns ErrClaimsInsufficient",
			callerClaims: map[string]any{"sub": "user1"},
			resourceJSON: mustMarshalAuthz(t, map[string]any{"sub": "user2"}),
			wantErr:      service.ErrClaimsInsufficient,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()
			err := validateClaimsSubsetBytes(ctx, tt.callerClaims, tt.resourceJSON)

			if tt.wantErr != nil {
				require.Error(t, err)
				assert.True(t, errors.Is(err, tt.wantErr), "expected %v, got %v", tt.wantErr, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestClaimsFromCtx(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		claims jwt.MapClaims
		want   map[string]any
	}{
		{
			name:   "no claims in context returns nil",
			claims: nil,
			want:   nil,
		},
		{
			name:   "claims in context returns map",
			claims: jwt.MapClaims{"sub": "user1", "org": "acme"},
			want:   map[string]any{"sub": "user1", "org": "acme"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()
			if tt.claims != nil {
				ctx = auth.ContextWithClaims(ctx, tt.claims)
			}

			got := claimsFromCtx(ctx)
			assert.Equal(t, tt.want, got)
		})
	}
}

// mustMarshalAuthz is a test helper that marshals v to JSON or fails the test.
func mustMarshalAuthz(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}
