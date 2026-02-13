package authz

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/stacklok/toolhive-registry-server/internal/config"
)

func TestExtractScopes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		claims map[string]any
		want   []string
	}{
		{
			name: "scope claim as space-separated string",
			claims: map[string]any{
				"scope": "mcp-registry:read mcp-registry:write",
			},
			want: []string{"mcp-registry:read", "mcp-registry:write"},
		},
		{
			name: "scope claim with single scope",
			claims: map[string]any{
				"scope": "mcp-registry:read",
			},
			want: []string{"mcp-registry:read"},
		},
		{
			name: "scope claim with multiple spaces between scopes",
			claims: map[string]any{
				"scope": "mcp-registry:read   mcp-registry:write   mcp-registry:admin",
			},
			want: []string{"mcp-registry:read", "mcp-registry:write", "mcp-registry:admin"},
		},
		{
			name: "scp claim as array of strings",
			claims: map[string]any{
				"scp": []any{"mcp-registry:read", "mcp-registry:write"},
			},
			want: []string{"mcp-registry:read", "mcp-registry:write"},
		},
		{
			name: "scp claim with single element",
			claims: map[string]any{
				"scp": []any{"mcp-registry:admin"},
			},
			want: []string{"mcp-registry:admin"},
		},
		{
			name: "scope claim takes precedence over scp",
			claims: map[string]any{
				"scope": "mcp-registry:read",
				"scp":   []any{"mcp-registry:admin"},
			},
			want: []string{"mcp-registry:read"},
		},
		{
			name:   "neither claim returns nil",
			claims: map[string]any{},
			want:   nil,
		},
		{
			name:   "nil claims returns nil",
			claims: nil,
			want:   nil,
		},
		{
			name: "empty scope string returns nil",
			claims: map[string]any{
				"scope": "",
			},
			want: nil,
		},
		{
			name: "scope claim is not a string (ignored, falls through to scp)",
			claims: map[string]any{
				"scope": 12345,
				"scp":   []any{"mcp-registry:read"},
			},
			want: []string{"mcp-registry:read"},
		},
		{
			name: "scp claim with non-string elements skips them",
			claims: map[string]any{
				"scp": []any{"mcp-registry:read", 42, "mcp-registry:write", nil},
			},
			want: []string{"mcp-registry:read", "mcp-registry:write"},
		},
		{
			name: "scp claim is not an array (returns nil)",
			claims: map[string]any{
				"scp": "not-an-array",
			},
			want: nil,
		},
		{
			name: "empty scp array returns empty slice",
			claims: map[string]any{
				"scp": []any{},
			},
			want: []string{},
		},
		{
			name: "scope with whitespace only returns empty slice",
			claims: map[string]any{
				"scope": "   ",
			},
			want: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := ExtractScopes(tt.claims)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMapScopesToActions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		scopes  []string
		mapping []config.ScopeMappingEntry
		want    []string
	}{
		{
			name:    "read scope maps to read action",
			scopes:  []string{"mcp-registry:read"},
			mapping: config.DefaultScopeMapping,
			want:    []string{"read"},
		},
		{
			name:    "write scope maps to read and write actions",
			scopes:  []string{"mcp-registry:write"},
			mapping: config.DefaultScopeMapping,
			want:    []string{"read", "write"},
		},
		{
			name:    "admin scope maps to read write and admin actions",
			scopes:  []string{"mcp-registry:admin"},
			mapping: config.DefaultScopeMapping,
			want:    []string{"admin", "read", "write"},
		},
		{
			name:    "unknown scope maps to empty actions",
			scopes:  []string{"unknown:scope"},
			mapping: config.DefaultScopeMapping,
			want:    []string{},
		},
		{
			name:    "empty scopes maps to empty actions",
			scopes:  []string{},
			mapping: config.DefaultScopeMapping,
			want:    []string{},
		},
		{
			name:    "nil scopes maps to empty actions",
			scopes:  nil,
			mapping: config.DefaultScopeMapping,
			want:    []string{},
		},
		{
			name:    "multiple scopes deduplicates actions",
			scopes:  []string{"mcp-registry:read", "mcp-registry:write"},
			mapping: config.DefaultScopeMapping,
			want:    []string{"read", "write"},
		},
		{
			name:    "all scopes deduplicates actions",
			scopes:  []string{"mcp-registry:read", "mcp-registry:write", "mcp-registry:admin"},
			mapping: config.DefaultScopeMapping,
			want:    []string{"admin", "read", "write"},
		},
		{
			name:   "custom mapping works correctly",
			scopes: []string{"custom:viewer"},
			mapping: []config.ScopeMappingEntry{
				{Scope: "custom:viewer", Actions: []string{"read"}},
				{Scope: "custom:editor", Actions: []string{"read", "write"}},
			},
			want: []string{"read"},
		},
		{
			name:   "custom mapping with multiple matching scopes",
			scopes: []string{"custom:viewer", "custom:editor"},
			mapping: []config.ScopeMappingEntry{
				{Scope: "custom:viewer", Actions: []string{"read"}},
				{Scope: "custom:editor", Actions: []string{"read", "write"}},
			},
			want: []string{"read", "write"},
		},
		{
			name:    "empty mapping returns empty actions",
			scopes:  []string{"mcp-registry:read"},
			mapping: []config.ScopeMappingEntry{},
			want:    []string{},
		},
		{
			name:    "nil mapping returns empty actions",
			scopes:  []string{"mcp-registry:read"},
			mapping: nil,
			want:    []string{},
		},
		{
			name:    "mixed known and unknown scopes",
			scopes:  []string{"mcp-registry:read", "unknown:scope", "mcp-registry:write"},
			mapping: config.DefaultScopeMapping,
			want:    []string{"read", "write"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := MapScopesToActions(tt.scopes, tt.mapping)
			// Sort for deterministic comparison since map iteration order is non-deterministic
			sort.Strings(got)
			sort.Strings(tt.want)
			assert.Equal(t, tt.want, got)
		})
	}
}
