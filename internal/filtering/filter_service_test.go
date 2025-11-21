package filtering

import (
	"context"
	"strings"
	"testing"

	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/registry"
)

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
	// Cast to concrete type to access fields in tests (same package)
	concreteService := service.(*defaultFilterService)
	assert.NotNil(t, concreteService.nameFilter)
	assert.NotNil(t, concreteService.tagFilter)
}

func TestNewFilterService(t *testing.T) {
	t.Parallel()

	nameFilter := NewDefaultNameFilter()
	tagFilter := NewDefaultTagFilter()

	service := NewFilterService(nameFilter, tagFilter)
	assert.NotNil(t, service)
	// Cast to concrete type to access fields in tests (same package)
	concreteService := service.(*defaultFilterService)
	assert.Equal(t, nameFilter, concreteService.nameFilter)
	assert.Equal(t, tagFilter, concreteService.tagFilter)
}

func TestDefaultFilterService_ApplyFilters_NoFilter(t *testing.T) {
	t.Parallel()

	service := NewDefaultFilterService()
	ctx := context.Background()

	// Create test registry with both container and remote servers
	originalRegistry := registry.NewTestUpstreamRegistry(
		registry.WithServers(
			registry.NewTestServer("postgres",
				registry.WithNamespace("io.test/"),
				registry.WithTags("database", "sql"),
				registry.WithOCIPackage("postgres:latest"),
				registry.WithServerVersion("1.0.0"),
			),
			registry.NewTestServer("web-api",
				registry.WithNamespace("io.test/"),
				registry.WithTags("web", "api"),
				registry.WithHTTPPackage("https://example.com"),
				registry.WithMetadata("test", "test"),
				registry.WithToolHiveMetadata("tier", "Official"),
			),
		),
	)

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
			originalRegistry := registry.NewTestUpstreamRegistry(
				registry.WithServers(
					registry.NewTestServer("postgres-server",
						registry.WithNamespace("io.test/"),
						registry.WithTags("database"),
						registry.WithOCIPackage("postgres:latest"),
					),
					registry.NewTestServer("mysql-server",
						registry.WithNamespace("io.test/"),
						registry.WithTags("database"),
						registry.WithOCIPackage("mysql:latest"),
					),
					registry.NewTestServer("redis-experimental",
						registry.WithNamespace("io.test/"),
						registry.WithTags("cache"),
						registry.WithOCIPackage("redis:latest"),
					),
					registry.NewTestServer("web-api",
						registry.WithNamespace("io.test/"),
						registry.WithTags("web"),
						registry.WithHTTPPackage("https://example.com"),
					),
					registry.NewTestServer("admin-experimental",
						registry.WithNamespace("io.test/"),
						registry.WithTags("admin"),
						registry.WithHTTPPackage("https://admin.example.com"),
					),
				),
			)

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
			originalRegistry := registry.NewTestUpstreamRegistry(
				registry.WithServers(
					registry.NewTestServer("postgres-server",
						registry.WithNamespace("io.test/"),
						registry.WithTags("database", "sql"),
						registry.WithOCIPackage("postgres:latest"),
					),
					registry.NewTestServer("mysql-server",
						registry.WithNamespace("io.test/"),
						registry.WithTags("database", "deprecated"),
						registry.WithOCIPackage("mysql:latest"),
					),
					registry.NewTestServer("redis-server",
						registry.WithNamespace("io.test/"),
						registry.WithTags("cache"),
						registry.WithOCIPackage("redis:latest"),
					),
					registry.NewTestServer("web-api",
						registry.WithNamespace("io.test/"),
						registry.WithTags("web", "api"),
						registry.WithHTTPPackage("https://example.com"),
					),
					registry.NewTestServer("legacy-api",
						registry.WithNamespace("io.test/"),
						registry.WithTags("web", "deprecated"),
						registry.WithHTTPPackage("https://legacy.example.com"),
					),
				),
			)

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
	originalRegistry := registry.NewTestUpstreamRegistry(
		registry.WithServers(
			registry.NewTestServer("postgres-server",
				registry.WithNamespace("io.test/"),
				registry.WithTags("database", "sql"),
				registry.WithOCIPackage("postgres:latest"),
			),
			registry.NewTestServer("postgres-experimental",
				registry.WithNamespace("io.test/"),
				registry.WithTags("database", "experimental"),
				registry.WithOCIPackage("postgres:experimental"),
			),
			registry.NewTestServer("web-server",
				registry.WithNamespace("io.test/"),
				registry.WithTags("web", "api"),
				registry.WithOCIPackage("nginx:latest"),
			),
			registry.NewTestServer("database-api",
				registry.WithNamespace("io.test/"),
				registry.WithTags("database", "api"),
				registry.WithHTTPPackage("https://db.example.com"),
			),
		),
	)

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
	originalRegistry := registry.NewTestUpstreamRegistry()

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
	originalRegistry := registry.NewTestUpstreamRegistry(
		registry.WithVersion("2.5.3"),
		registry.WithLastUpdated("2023-12-15T14:30:00Z"),
		registry.WithServers(
			registry.NewTestServer("test-server",
				registry.WithNamespace("io.test/"),
				registry.WithTags("test"),
				registry.WithOCIPackage("test:latest"),
			),
		),
	)

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
