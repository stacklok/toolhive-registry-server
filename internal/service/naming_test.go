package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPrefixServerName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		registryName string
		serverName   string
		expected     string
	}{
		{
			name:         "standard prefixing",
			registryName: "partner-a",
			serverName:   "io.github.user/server",
			expected:     "partner-a.io.github.user/server",
		},
		{
			name:         "simple names",
			registryName: "registry",
			serverName:   "server",
			expected:     "registry.server",
		},
		{
			name:         "empty registry name",
			registryName: "",
			serverName:   "server",
			expected:     ".server",
		},
		{
			name:         "empty server name",
			registryName: "registry",
			serverName:   "",
			expected:     "registry.",
		},
		{
			name:         "both empty",
			registryName: "",
			serverName:   "",
			expected:     ".",
		},
		{
			name:         "single character registry name",
			registryName: "a",
			serverName:   "server",
			expected:     "a.server",
		},
		{
			name:         "single character server name",
			registryName: "registry",
			serverName:   "s",
			expected:     "registry.s",
		},
		{
			name:         "single character both",
			registryName: "a",
			serverName:   "b",
			expected:     "a.b",
		},
		{
			name:         "registry name with hyphens",
			registryName: "my-partner-registry",
			serverName:   "server",
			expected:     "my-partner-registry.server",
		},
		{
			name:         "registry name with underscores",
			registryName: "my_partner_registry",
			serverName:   "server",
			expected:     "my_partner_registry.server",
		},
		{
			name:         "server name with dots",
			registryName: "registry",
			serverName:   "io.github.user.project",
			expected:     "registry.io.github.user.project",
		},
		{
			name:         "server name with slashes",
			registryName: "registry",
			serverName:   "org/repo/server",
			expected:     "registry.org/repo/server",
		},
		{
			name:         "server name with mixed special characters",
			registryName: "registry",
			serverName:   "io.github.user/my-server_v1",
			expected:     "registry.io.github.user/my-server_v1",
		},
		{
			name:         "unicode registry name",
			registryName: "registre-francais",
			serverName:   "server",
			expected:     "registre-francais.server",
		},
		{
			name:         "unicode server name",
			registryName: "registry",
			serverName:   "serveur-donnees",
			expected:     "registry.serveur-donnees",
		},
		{
			name:         "numeric registry name",
			registryName: "registry123",
			serverName:   "server",
			expected:     "registry123.server",
		},
		{
			name:         "numeric server name",
			registryName: "registry",
			serverName:   "server456",
			expected:     "registry.server456",
		},
		{
			name:         "registry name with dots",
			registryName: "com.example.registry",
			serverName:   "server",
			expected:     "com.example.registry.server",
		},
		{
			name:         "whitespace in registry name",
			registryName: "registry name",
			serverName:   "server",
			expected:     "registry name.server",
		},
		{
			name:         "whitespace in server name",
			registryName: "registry",
			serverName:   "server name",
			expected:     "registry.server name",
		},
		{
			name:         "long registry name",
			registryName: "this-is-a-very-long-registry-name-for-testing-purposes",
			serverName:   "server",
			expected:     "this-is-a-very-long-registry-name-for-testing-purposes.server",
		},
		{
			name:         "long server name",
			registryName: "registry",
			serverName:   "io.github.organization.project.subproject.server-name-v1",
			expected:     "registry.io.github.organization.project.subproject.server-name-v1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := PrefixServerName(tt.registryName, tt.serverName)

			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestShouldPrefixNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		registryName *string
		expected     bool
	}{
		{
			name:         "nil registry name - aggregated query",
			registryName: nil,
			expected:     true,
		},
		{
			name:         "non-nil registry name - specific registry query",
			registryName: ptr("my-registry"),
			expected:     false,
		},
		{
			name:         "empty string registry name",
			registryName: ptr(""),
			expected:     false,
		},
		{
			name:         "whitespace registry name",
			registryName: ptr("   "),
			expected:     false,
		},
		{
			name:         "single character registry name",
			registryName: ptr("a"),
			expected:     false,
		},
		{
			name:         "registry name with special characters",
			registryName: ptr("registry-name_v1"),
			expected:     false,
		},
		{
			name:         "long registry name",
			registryName: ptr("this-is-a-very-long-registry-name-for-testing"),
			expected:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := ShouldPrefixNames(tt.registryName)

			assert.Equal(t, tt.expected, result)
		})
	}
}

// ptr is a helper function to create a pointer to a string value.
func ptr(s string) *string {
	return &s
}
