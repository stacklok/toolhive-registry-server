package filtering

import (
	"context"
	"testing"

	toolhivetypes "github.com/stacklok/toolhive/pkg/registry/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/config"
)

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

	// Create test config
	// Create test registry
	originalRegistry := &toolhivetypes.Registry{
		Version:     "1.0.0",
		LastUpdated: "2023-01-01T00:00:00Z",
		Servers: map[string]*toolhivetypes.ImageMetadata{
			"postgres": {
				BaseServerMetadata: toolhivetypes.BaseServerMetadata{
					Name: "postgres",
					Tags: []string{"database", "sql"},
				},
				Image: "postgres:latest",
			},
		},
		RemoteServers: map[string]*toolhivetypes.RemoteServerMetadata{
			"web-api": {
				BaseServerMetadata: toolhivetypes.BaseServerMetadata{
					Name: "web-api",
					Tags: []string{"web", "api"},
				},
				URL: "https://example.com",
			},
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
		name                      string
		nameInclude               []string
		nameExclude               []string
		expectedServerNames       []string
		expectedRemoteServerNames []string
	}{
		{
			name:                      "include postgres pattern",
			nameInclude:               []string{"postgres-*"},
			nameExclude:               []string{},
			expectedServerNames:       []string{"postgres-server"},
			expectedRemoteServerNames: []string{},
		},
		{
			name:                      "exclude experimental pattern",
			nameInclude:               []string{},
			nameExclude:               []string{"*-experimental"},
			expectedServerNames:       []string{"postgres-server", "mysql-server"},
			expectedRemoteServerNames: []string{"web-api"},
		},
		{
			name:                      "include with exclude precedence",
			nameInclude:               []string{"*-server"},
			nameExclude:               []string{"*-experimental"},
			expectedServerNames:       []string{"postgres-server", "mysql-server"},
			expectedRemoteServerNames: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create test registry
			originalRegistry := &toolhivetypes.Registry{
				Version:     "1.0.0",
				LastUpdated: "2023-01-01T00:00:00Z",
				Servers: map[string]*toolhivetypes.ImageMetadata{
					"postgres-server": {
						BaseServerMetadata: toolhivetypes.BaseServerMetadata{
							Name: "postgres-server",
							Tags: []string{"database"},
						},
						Image: "postgres:latest",
					},
					"mysql-server": {
						BaseServerMetadata: toolhivetypes.BaseServerMetadata{
							Name: "mysql-server",
							Tags: []string{"database"},
						},
						Image: "mysql:latest",
					},
					"redis-experimental": {
						BaseServerMetadata: toolhivetypes.BaseServerMetadata{
							Name: "redis-experimental",
							Tags: []string{"cache"},
						},
						Image: "redis:latest",
					},
				},
				RemoteServers: map[string]*toolhivetypes.RemoteServerMetadata{
					"web-api": {
						BaseServerMetadata: toolhivetypes.BaseServerMetadata{
							Name: "web-api",
							Tags: []string{"web"},
						},
						URL: "https://example.com",
					},
					"admin-experimental": {
						BaseServerMetadata: toolhivetypes.BaseServerMetadata{
							Name: "admin-experimental",
							Tags: []string{"admin"},
						},
						URL: "https://admin.example.com",
					},
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

			// Check filtered servers
			assert.Len(t, result.Servers, len(tt.expectedServerNames))
			for _, expectedName := range tt.expectedServerNames {
				assert.Contains(t, result.Servers, expectedName)
			}

			// Check filtered remote servers
			assert.Len(t, result.RemoteServers, len(tt.expectedRemoteServerNames))
			for _, expectedName := range tt.expectedRemoteServerNames {
				assert.Contains(t, result.RemoteServers, expectedName)
			}
		})
	}
}

func TestDefaultFilterService_ApplyFilters_TagFiltering(t *testing.T) {
	t.Parallel()

	service := NewDefaultFilterService()
	ctx := context.Background()

	tests := []struct {
		name                      string
		tagInclude                []string
		tagExclude                []string
		expectedServerNames       []string
		expectedRemoteServerNames []string
	}{
		{
			name:                      "include database tags",
			tagInclude:                []string{"database"},
			tagExclude:                []string{},
			expectedServerNames:       []string{"postgres-server", "mysql-server"},
			expectedRemoteServerNames: []string{},
		},
		{
			name:                      "exclude deprecated tags",
			tagInclude:                []string{},
			tagExclude:                []string{"deprecated"},
			expectedServerNames:       []string{"postgres-server", "redis-server"},
			expectedRemoteServerNames: []string{"web-api"},
		},
		{
			name:                      "include with exclude precedence",
			tagInclude:                []string{"database"},
			tagExclude:                []string{"deprecated"},
			expectedServerNames:       []string{"postgres-server"},
			expectedRemoteServerNames: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create test registry
			originalRegistry := &toolhivetypes.Registry{
				Version:     "1.0.0",
				LastUpdated: "2023-01-01T00:00:00Z",
				Servers: map[string]*toolhivetypes.ImageMetadata{
					"postgres-server": {
						BaseServerMetadata: toolhivetypes.BaseServerMetadata{
							Name: "postgres-server",
							Tags: []string{"database", "sql"},
						},
						Image: "postgres:latest",
					},
					"mysql-server": {
						BaseServerMetadata: toolhivetypes.BaseServerMetadata{
							Name: "mysql-server",
							Tags: []string{"database", "deprecated"},
						},
						Image: "mysql:latest",
					},
					"redis-server": {
						BaseServerMetadata: toolhivetypes.BaseServerMetadata{
							Name: "redis-server",
							Tags: []string{"cache"},
						},
						Image: "redis:latest",
					},
				},
				RemoteServers: map[string]*toolhivetypes.RemoteServerMetadata{
					"web-api": {
						BaseServerMetadata: toolhivetypes.BaseServerMetadata{
							Name: "web-api",
							Tags: []string{"web", "api"},
						},
						URL: "https://example.com",
					},
					"legacy-api": {
						BaseServerMetadata: toolhivetypes.BaseServerMetadata{
							Name: "legacy-api",
							Tags: []string{"web", "deprecated"},
						},
						URL: "https://legacy.example.com",
					},
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

			// Check filtered servers
			assert.Len(t, result.Servers, len(tt.expectedServerNames))
			for _, expectedName := range tt.expectedServerNames {
				assert.Contains(t, result.Servers, expectedName)
			}

			// Check filtered remote servers
			assert.Len(t, result.RemoteServers, len(tt.expectedRemoteServerNames))
			for _, expectedName := range tt.expectedRemoteServerNames {
				assert.Contains(t, result.RemoteServers, expectedName)
			}
		})
	}
}

func TestDefaultFilterService_ApplyFilters_CombinedFiltering(t *testing.T) {
	t.Parallel()

	service := NewDefaultFilterService()
	ctx := context.Background()

	// Create test registry
	originalRegistry := &toolhivetypes.Registry{
		Version:     "1.0.0",
		LastUpdated: "2023-01-01T00:00:00Z",
		Servers: map[string]*toolhivetypes.ImageMetadata{
			"postgres-server": {
				BaseServerMetadata: toolhivetypes.BaseServerMetadata{
					Name: "postgres-server",
					Tags: []string{"database", "sql"},
				},
				Image: "postgres:latest",
			},
			"postgres-experimental": {
				BaseServerMetadata: toolhivetypes.BaseServerMetadata{
					Name: "postgres-experimental",
					Tags: []string{"database", "experimental"},
				},
				Image: "postgres:experimental",
			},
			"web-server": {
				BaseServerMetadata: toolhivetypes.BaseServerMetadata{
					Name: "web-server",
					Tags: []string{"web", "api"},
				},
				Image: "nginx:latest",
			},
		},
		RemoteServers: map[string]*toolhivetypes.RemoteServerMetadata{
			"database-api": {
				BaseServerMetadata: toolhivetypes.BaseServerMetadata{
					Name: "database-api",
					Tags: []string{"database", "api"},
				},
				URL: "https://db.example.com",
			},
		},
	}

	filter := &config.FilterConfig{
		Names: &config.NameFilterConfig{
			Include: []string{"postgres-*", "*-api"},
			Exclude: []string{"*-experimental"},
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
	assert.Len(t, result.Servers, 1)
	assert.Contains(t, result.Servers, "postgres-server")

	assert.Len(t, result.RemoteServers, 1)
	assert.Contains(t, result.RemoteServers, "database-api")
}

func TestDefaultFilterService_ApplyFilters_EmptyRegistry(t *testing.T) {
	t.Parallel()

	service := NewDefaultFilterService()
	ctx := context.Background()

	// Create empty registry
	originalRegistry := &toolhivetypes.Registry{
		Version:       "1.0.0",
		LastUpdated:   "2023-01-01T00:00:00Z",
		Servers:       make(map[string]*toolhivetypes.ImageMetadata),
		RemoteServers: make(map[string]*toolhivetypes.RemoteServerMetadata),
	}

	filter := &config.FilterConfig{
		Names: &config.NameFilterConfig{
			Include: []string{"postgres-*"},
		},
	}

	result, err := service.ApplyFilters(ctx, originalRegistry, filter)

	require.NoError(t, err)
	assert.Len(t, result.Servers, 0)
	assert.Len(t, result.RemoteServers, 0)
	assert.Equal(t, originalRegistry.Version, result.Version)
	assert.Equal(t, originalRegistry.LastUpdated, result.LastUpdated)
}

func TestDefaultFilterService_ApplyFilters_PreservesMetadata(t *testing.T) {
	t.Parallel()

	service := NewDefaultFilterService()
	ctx := context.Background()

	// Create registry with groups
	groups := []*toolhivetypes.Group{
		{
			Name:        "test-group",
			Description: "Test group",
		},
	}

	originalRegistry := &toolhivetypes.Registry{
		Version:       "1.0.0",
		LastUpdated:   "2023-01-01T00:00:00Z",
		Servers:       make(map[string]*toolhivetypes.ImageMetadata),
		RemoteServers: make(map[string]*toolhivetypes.RemoteServerMetadata),
		Groups:        groups,
	}

	filter := &config.FilterConfig{
		Names: &config.NameFilterConfig{
			Include: []string{"*"},
		},
	}

	result, err := service.ApplyFilters(ctx, originalRegistry, filter)

	require.NoError(t, err)
	assert.Equal(t, originalRegistry.Version, result.Version)
	assert.Equal(t, originalRegistry.LastUpdated, result.LastUpdated)
	assert.Equal(t, originalRegistry.Groups, result.Groups)
}
