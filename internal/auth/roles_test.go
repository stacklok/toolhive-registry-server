package auth

import (
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/config"
)

func TestResolveRoles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		claims   jwt.MapClaims
		authzCfg *config.AuthzConfig
		expected []Role
	}{
		{
			name:     "nil authzCfg returns nil",
			claims:   jwt.MapClaims{"sub": "user-1"},
			authzCfg: nil,
			expected: nil,
		},
		{
			name:     "nil claims returns nil",
			claims:   nil,
			authzCfg: &config.AuthzConfig{},
			expected: nil,
		},
		{
			name:   "AND logic across keys in a single claim map",
			claims: jwt.MapClaims{"org": "acme", "role": "admin"},
			authzCfg: &config.AuthzConfig{
				Roles: config.RolesConfig{
					ManageSources: []map[string]any{
						{"org": "acme", "role": "admin"},
					},
				},
			},
			expected: []Role{RoleManageSources},
		},
		{
			name:   "AND logic fails when one key does not match",
			claims: jwt.MapClaims{"org": "acme", "role": "viewer"},
			authzCfg: &config.AuthzConfig{
				Roles: config.RolesConfig{
					ManageSources: []map[string]any{
						{"org": "acme", "role": "admin"},
					},
				},
			},
			expected: nil,
		},
		{
			name:   "AND logic fails when key is missing from claims",
			claims: jwt.MapClaims{"org": "acme"},
			authzCfg: &config.AuthzConfig{
				Roles: config.RolesConfig{
					ManageSources: []map[string]any{
						{"org": "acme", "role": "admin"},
					},
				},
			},
			expected: nil,
		},
		{
			name:   "OR logic across maps - first map matches",
			claims: jwt.MapClaims{"org": "acme"},
			authzCfg: &config.AuthzConfig{
				Roles: config.RolesConfig{
					ManageSources: []map[string]any{
						{"org": "acme"},
						{"org": "globex"},
					},
				},
			},
			expected: []Role{RoleManageSources},
		},
		{
			name:   "OR logic across maps - second map matches",
			claims: jwt.MapClaims{"org": "globex"},
			authzCfg: &config.AuthzConfig{
				Roles: config.RolesConfig{
					ManageSources: []map[string]any{
						{"org": "acme"},
						{"org": "globex"},
					},
				},
			},
			expected: []Role{RoleManageSources},
		},
		{
			name:   "OR logic within array values - JWT has array claim",
			claims: jwt.MapClaims{"roles": []any{"editor", "admin"}},
			authzCfg: &config.AuthzConfig{
				Roles: config.RolesConfig{
					ManageEntries: []map[string]any{
						{"roles": "admin"},
					},
				},
			},
			expected: []Role{RoleManageEntries},
		},
		{
			name:   "OR logic within array values - required is array",
			claims: jwt.MapClaims{"role": "editor"},
			authzCfg: &config.AuthzConfig{
				Roles: config.RolesConfig{
					ManageEntries: []map[string]any{
						{"role": []any{"admin", "editor"}},
					},
				},
			},
			expected: []Role{RoleManageEntries},
		},
		{
			name:   "superAdmin role granted when config matches",
			claims: jwt.MapClaims{"email": "root@example.com"},
			authzCfg: &config.AuthzConfig{
				Roles: config.RolesConfig{
					SuperAdmin: []map[string]any{
						{"email": "root@example.com"},
					},
				},
			},
			expected: []Role{RoleSuperAdmin},
		},
		{
			name:   "multiple roles granted at once",
			claims: jwt.MapClaims{"org": "acme", "role": "admin"},
			authzCfg: &config.AuthzConfig{
				Roles: config.RolesConfig{
					SuperAdmin:       []map[string]any{{"org": "acme", "role": "admin"}},
					ManageSources:    []map[string]any{{"org": "acme"}},
					ManageRegistries: []map[string]any{{"role": "admin"}},
					ManageEntries:    []map[string]any{{"org": "other"}},
				},
			},
			expected: []Role{RoleSuperAdmin, RoleManageSources, RoleManageRegistries},
		},
		{
			name:   "no matching roles returns empty slice",
			claims: jwt.MapClaims{"org": "unknown"},
			authzCfg: &config.AuthzConfig{
				Roles: config.RolesConfig{
					ManageSources: []map[string]any{
						{"org": "acme"},
					},
				},
			},
			expected: nil,
		},
		{
			name:   "empty rules returns no roles",
			claims: jwt.MapClaims{"org": "acme"},
			authzCfg: &config.AuthzConfig{
				Roles: config.RolesConfig{},
			},
			expected: nil,
		},
		{
			name:   "empty claim map in rules does not match",
			claims: jwt.MapClaims{"org": "acme"},
			authzCfg: &config.AuthzConfig{
				Roles: config.RolesConfig{
					ManageSources: []map[string]any{
						{},
					},
				},
			},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := ResolveRoles(tt.claims, tt.authzCfg)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestHasRole(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		roles    []Role
		required Role
		expected bool
	}{
		{
			name:     "returns true for exact match",
			roles:    []Role{RoleManageSources, RoleManageEntries},
			required: RoleManageSources,
			expected: true,
		},
		{
			name:     "returns false for non-matching role",
			roles:    []Role{RoleManageSources},
			required: RoleManageRegistries,
			expected: false,
		},
		{
			name:     "superAdmin grants any role",
			roles:    []Role{RoleSuperAdmin},
			required: RoleManageEntries,
			expected: true,
		},
		{
			name:     "superAdmin grants another arbitrary role",
			roles:    []Role{RoleSuperAdmin},
			required: RoleManageRegistries,
			expected: true,
		},
		{
			name:     "empty roles returns false",
			roles:    []Role{},
			required: RoleManageSources,
			expected: false,
		},
		{
			name:     "nil roles returns false",
			roles:    nil,
			required: RoleManageSources,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := HasRole(tt.roles, tt.required)
			assert.Equal(t, tt.expected, got)
		})
	}
}

// TestMatchesClaimValue exercises the matchesClaimValue logic through
// ResolveRoles, covering the various type combinations for JWT and required values.
func TestMatchesClaimValue(t *testing.T) {
	t.Parallel()

	// Helper that builds a minimal AuthzConfig with one ManageSources rule.
	cfg := func(claimKey string, requiredValue any) *config.AuthzConfig {
		return &config.AuthzConfig{
			Roles: config.RolesConfig{
				ManageSources: []map[string]any{
					{claimKey: requiredValue},
				},
			},
		}
	}

	tests := []struct {
		name      string
		claims    jwt.MapClaims
		authzCfg  *config.AuthzConfig
		wantMatch bool
	}{
		{
			name:      "JWT string matches required string",
			claims:    jwt.MapClaims{"org": "acme"},
			authzCfg:  cfg("org", "acme"),
			wantMatch: true,
		},
		{
			name:      "JWT string does not match required string",
			claims:    jwt.MapClaims{"org": "globex"},
			authzCfg:  cfg("org", "acme"),
			wantMatch: false,
		},
		{
			name:      "JWT []any matches required string",
			claims:    jwt.MapClaims{"groups": []any{"devs", "admins"}},
			authzCfg:  cfg("groups", "admins"),
			wantMatch: true,
		},
		{
			name:      "JWT []any does not match required string",
			claims:    jwt.MapClaims{"groups": []any{"devs", "viewers"}},
			authzCfg:  cfg("groups", "admins"),
			wantMatch: false,
		},
		{
			name:      "JWT string matches required []any",
			claims:    jwt.MapClaims{"role": "editor"},
			authzCfg:  cfg("role", []any{"admin", "editor"}),
			wantMatch: true,
		},
		{
			name:      "JWT string does not match required []any",
			claims:    jwt.MapClaims{"role": "viewer"},
			authzCfg:  cfg("role", []any{"admin", "editor"}),
			wantMatch: false,
		},
		{
			name:      "JWT []any matches required []any",
			claims:    jwt.MapClaims{"scopes": []any{"read", "write"}},
			authzCfg:  cfg("scopes", []any{"write", "delete"}),
			wantMatch: true,
		},
		{
			name:      "JWT []any does not match required []any",
			claims:    jwt.MapClaims{"scopes": []any{"read"}},
			authzCfg:  cfg("scopes", []any{"write", "delete"}),
			wantMatch: false,
		},
		{
			name:      "non-string values in JWT []any are ignored",
			claims:    jwt.MapClaims{"ids": []any{123, true}},
			authzCfg:  cfg("ids", "123"),
			wantMatch: false,
		},
		{
			name:      "non-string required value returns no match",
			claims:    jwt.MapClaims{"count": "5"},
			authzCfg:  cfg("count", 5),
			wantMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			roles := ResolveRoles(tt.claims, tt.authzCfg)
			if tt.wantMatch {
				require.Len(t, roles, 1)
				assert.Equal(t, RoleManageSources, roles[0])
			} else {
				assert.Empty(t, roles)
			}
		})
	}
}
