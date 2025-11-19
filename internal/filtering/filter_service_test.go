package filtering

import (
	"context"
	"strings"
	"testing"

	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
	toolhivetypes "github.com/stacklok/toolhive/pkg/registry/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/config"
)

// Helper to create upstream server with ToolHive tags
func newTestServer(name string, tags []string, packageType string, identifier string) upstreamv0.ServerJSON {
	tagInterfaces := make([]interface{}, len(tags))
	for i, tag := range tags {
		tagInterfaces[i] = tag
	}

	return upstreamv0.ServerJSON{
		Schema:      "https://static.modelcontextprotocol.io/schemas/2025-10-17/server.schema.json",
		Name:        "io.test/" + name,
		Description: name + " server",
		Version:     "1.0.0",
		Packages: []model.Package{
			{
				RegistryType: packageType,
				Identifier:   identifier,
				Transport:    model.Transport{Type: "stdio"},
			},
		},
		Meta: &upstreamv0.ServerMeta{
			PublisherProvided: map[string]any{
				"provider": map[string]any{
					"metadata": map[string]any{
						"tags": tagInterfaces,
					},
				},
			},
		},
	}
}

// Helper to assert servers contain expected names
func assertContainsServerNames(t *testing.T, servers []upstreamv0.ServerJSON, expectedNames []string) {
	t.Helper()
	assert.Len(t, servers, len(expectedNames), "Server count mismatch")
	for _, expectedName := range expectedNames {
		found := false
		for _, server := range servers {
			if strings.Contains(server.Name, expectedName) {
				found = true
				break
			}
		}
		assert.True(t, found, "Expected server %s in results", expectedName)
	}
}

func TestNewDefaultFilterService(t *testing.T) {
	t.Parallel()

	service := NewDefaultFilterService()
	assert.NotNil(t, service)
	assert.NotNil(t, service.nameFilter)
	assert.NotNil(t, service.tagFilter)
}

func TestNewFilterService(t *testing.T) {
	t.Parallel()

	nameFilter := NewDefaultNameFilter()
	tagFilter := NewDefaultTagFilter()

	service := NewFilterService(nameFilter, tagFilter)
	assert.NotNil(t, service)
	assert.Equal(t, nameFilter, service.nameFilter)
	assert.Equal(t, tagFilter, service.tagFilter)
}

func TestDefaultFilterService_ApplyFilters_NoFilter(t *testing.T) {
	t.Parallel()

	service := NewDefaultFilterService()
	ctx := context.Background()

	// Create test registry with both container and remote servers
	originalRegistry := &toolhivetypes.UpstreamRegistry{
		Version:     "1.0.0",
		LastUpdated: "2023-01-01T00:00:00Z",
		Servers: []upstreamv0.ServerJSON{
			newTestServer("postgres", []string{"database", "sql"}, "oci", "postgres:latest"),
			newTestServer("web-api", []string{"web", "api"}, "http", "https://example.com"),
		},
	}

	// Apply no filter
	result, err := service.ApplyFilters(ctx, originalRegistry, nil)
	require.NoError(t, err)
	assert.Equal(t, originalRegistry, result, "No filter should return original registry")
}

func TestDefaultFilterService_ApplyFilters_NameFiltering(t *testing.T) {
	t.Parallel()

	service := NewDefaultFilterService()
	ctx := context.Background()

	tests := []struct {
		name          string
		nameInclude   []string
		nameExclude   []string
		expectedNames []string // Unified: all server names that should be in result
	}{
		{
			name:          "include postgres pattern",
			nameInclude:   []string{"*/postgres-*"},
			nameExclude:   []string{},
			expectedNames: []string{"postgres-server"},
		},
		{
			name:          "exclude experimental pattern",
			nameInclude:   []string{},
			nameExclude:   []string{"*/*-experimental"},
			expectedNames: []string{"postgres-server", "mysql-server", "web-api"},
		},
		{
			name:          "include with exclude precedence",
			nameInclude:   []string{"*/*-server"},
			nameExclude:   []string{"*/*-experimental"},
			expectedNames: []string{"postgres-server", "mysql-server"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create test registry with both container and remote servers
			originalRegistry := &toolhivetypes.UpstreamRegistry{
				Version:     "1.0.0",
				LastUpdated: "2023-01-01T00:00:00Z",
				Servers: []upstreamv0.ServerJSON{
					newTestServer("postgres-server", []string{"database"}, "oci", "postgres:latest"),
					newTestServer("mysql-server", []string{"database"}, "oci", "mysql:latest"),
					newTestServer("redis-experimental", []string{"cache"}, "oci", "redis:latest"),
					newTestServer("web-api", []string{"web"}, "http", "https://example.com"),
					newTestServer("admin-experimental", []string{"admin"}, "http", "https://admin.example.com"),
				},
			}

			filter := &config.FilterConfig{
				Names: &config.NameFilterConfig{
					Include: tt.nameInclude,
					Exclude: tt.nameExclude,
				},
			}

			result, err := service.ApplyFilters(ctx, originalRegistry, filter)

			require.NoError(t, err)
			assert.Equal(t, originalRegistry.Version, result.Version)
			assert.Equal(t, originalRegistry.LastUpdated, result.LastUpdated)

			// Check filtered servers using helper
			assertContainsServerNames(t, result.Servers, tt.expectedNames)
		})
	}
}

func TestDefaultFilterService_ApplyFilters_TagFiltering(t *testing.T) {
	t.Parallel()

	service := NewDefaultFilterService()
	ctx := context.Background()

	tests := []struct {
		name          string
		tagInclude    []string
		tagExclude    []string
		expectedNames []string // Unified: all server names that should be in result
	}{
		{
			name:          "include database tags",
			tagInclude:    []string{"database"},
			tagExclude:    []string{},
			expectedNames: []string{"postgres-server", "mysql-server"},
		},
		{
			name:          "exclude deprecated tags",
			tagInclude:    []string{},
			tagExclude:    []string{"deprecated"},
			expectedNames: []string{"postgres-server", "redis-server", "web-api"},
		},
		{
			name:          "include with exclude precedence",
			tagInclude:    []string{"database"},
			tagExclude:    []string{"deprecated"},
			expectedNames: []string{"postgres-server"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create test registry with servers having different tags
			originalRegistry := &toolhivetypes.UpstreamRegistry{
				Version:     "1.0.0",
				LastUpdated: "2023-01-01T00:00:00Z",
				Servers: []upstreamv0.ServerJSON{
					newTestServer("postgres-server", []string{"database", "sql"}, "oci", "postgres:latest"),
					newTestServer("mysql-server", []string{"database", "deprecated"}, "oci", "mysql:latest"),
					newTestServer("redis-server", []string{"cache"}, "oci", "redis:latest"),
					newTestServer("web-api", []string{"web", "api"}, "http", "https://example.com"),
					newTestServer("legacy-api", []string{"web", "deprecated"}, "http", "https://legacy.example.com"),
				},
			}

			filter := &config.FilterConfig{
				Tags: &config.TagFilterConfig{
					Include: tt.tagInclude,
					Exclude: tt.tagExclude,
				},
			}

			result, err := service.ApplyFilters(ctx, originalRegistry, filter)

			require.NoError(t, err)

			// Check filtered servers using helper
			assertContainsServerNames(t, result.Servers, tt.expectedNames)
		})
	}
}

func TestDefaultFilterService_ApplyFilters_CombinedFiltering(t *testing.T) {
	t.Parallel()

	service := NewDefaultFilterService()
	ctx := context.Background()

	// Create test registry
	originalRegistry := &toolhivetypes.UpstreamRegistry{
		Version:     "1.0.0",
		LastUpdated: "2023-01-01T00:00:00Z",
		Servers: []upstreamv0.ServerJSON{
			newTestServer("postgres-server", []string{"database", "sql"}, "oci", "postgres:latest"),
			newTestServer("postgres-experimental", []string{"database", "experimental"}, "oci", "postgres:experimental"),
			newTestServer("web-server", []string{"web", "api"}, "oci", "nginx:latest"),
			newTestServer("database-api", []string{"database", "api"}, "http", "https://db.example.com"),
		},
	}

	filter := &config.FilterConfig{
		Names: &config.NameFilterConfig{
			Include: []string{"*/postgres-*", "*/*-api"},
			Exclude: []string{"*/*-experimental"},
		},
		Tags: &config.TagFilterConfig{
			Include: []string{"database"},
			Exclude: []string{},
		},
	}

	result, err := service.ApplyFilters(ctx, originalRegistry, filter)

	require.NoError(t, err)

	// Should include:
	// - postgres-server: matches postgres-* pattern AND has database tag
	// - database-api: matches *-api pattern AND has database tag
	// Should exclude:
	// - postgres-experimental: excluded by *-experimental pattern (exclude takes precedence)
	// - web-server: matches no name patterns
	assertContainsServerNames(t, result.Servers, []string{"postgres-server", "database-api"})
}

func TestDefaultFilterService_ApplyFilters_EmptyRegistry(t *testing.T) {
	t.Parallel()

	service := NewDefaultFilterService()
	ctx := context.Background()

	// Create empty registry
	originalRegistry := &toolhivetypes.UpstreamRegistry{
		Version:     "1.0.0",
		LastUpdated: "2023-01-01T00:00:00Z",
		Servers:     []upstreamv0.ServerJSON{},
	}

	filter := &config.FilterConfig{
		Names: &config.NameFilterConfig{
			Include: []string{"*/postgres-*"},
		},
	}

	result, err := service.ApplyFilters(ctx, originalRegistry, filter)

	require.NoError(t, err)
	assert.Len(t, result.Servers, 0)
	assert.Equal(t, originalRegistry.Version, result.Version)
	assert.Equal(t, originalRegistry.LastUpdated, result.LastUpdated)
}

func TestDefaultFilterService_ApplyFilters_PreservesMetadata(t *testing.T) {
	t.Parallel()

	service := NewDefaultFilterService()
	ctx := context.Background()

	// Create registry with specific metadata values
	originalRegistry := &toolhivetypes.UpstreamRegistry{
		Version:     "2.5.3",
		LastUpdated: "2023-12-15T14:30:00Z",
		Servers: []upstreamv0.ServerJSON{
			newTestServer("test-server", []string{"test"}, "oci", "test:latest"),
		},
	}

	filter := &config.FilterConfig{
		Names: &config.NameFilterConfig{
			Include: []string{"*/*"},
		},
	}

	result, err := service.ApplyFilters(ctx, originalRegistry, filter)

	require.NoError(t, err)
	// Verify metadata is preserved exactly
	assert.Equal(t, originalRegistry.Version, result.Version)
	assert.Equal(t, originalRegistry.LastUpdated, result.LastUpdated)
	// Verify server is included (wildcard match)
	assert.Len(t, result.Servers, 1)
}
