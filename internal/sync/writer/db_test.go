// Package writer contains the SyncWriter interface and implementations
package writer

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
	toolhivetypes "github.com/stacklok/toolhive/pkg/registry/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/database"
	"github.com/stacklok/toolhive-registry-server/internal/db/sqlc"
)

// Test constants for icon themes and MIME types
const (
	testThemeLight  = "light"
	testThemeDark   = "dark"
	testMimeTypePNG = "image/png"
)

// setupTestDB creates a test database connection and returns cleanup function
func setupTestDB(t *testing.T) (*pgxpool.Pool, func()) {
	t.Helper()

	ctx := context.Background()
	db, cleanupFunc := database.SetupTestDB(t)
	t.Cleanup(cleanupFunc)

	// Get connection string from the db connection
	connStr := db.Config().ConnString()

	// Create a pgxpool from the connection string
	pool, err := pgxpool.New(ctx, connStr)
	require.NoError(t, err)

	poolCleanup := func() {
		pool.Close()
		cleanupFunc()
	}

	return pool, poolCleanup
}

// createTestRegistry creates a test registry in the database and returns its ID
// Uses REMOTE registry type by default, which is the typical type for synced registries
func createTestRegistry(t *testing.T, pool *pgxpool.Pool, name string) uuid.UUID {
	t.Helper()

	ctx := context.Background()
	queries := sqlc.New(pool)

	regID, err := queries.InsertConfigRegistry(ctx, sqlc.InsertConfigRegistryParams{
		Name:     name,
		RegType:  sqlc.RegistryTypeREMOTE,
		Syncable: true,
	})
	require.NoError(t, err)

	return regID
}

// createTestUpstreamRegistry creates a test UpstreamRegistry with the given servers
func createTestUpstreamRegistry(servers []upstreamv0.ServerJSON) *toolhivetypes.UpstreamRegistry {
	return &toolhivetypes.UpstreamRegistry{
		Schema:  "https://example.com/schema.json",
		Version: "1.0.0",
		Data: toolhivetypes.UpstreamData{
			Servers: servers,
		},
	}
}

// createTestServer creates a test ServerJSON with the given name and version
func createTestServer(name, version string) upstreamv0.ServerJSON {
	return upstreamv0.ServerJSON{
		Name:        name,
		Version:     version,
		Description: "Test server description",
		Title:       "Test Server",
		WebsiteURL:  "https://example.com",
	}
}

// createTestServerWithPackages creates a test ServerJSON with packages
func createTestServerWithPackages(name, version string) upstreamv0.ServerJSON {
	server := createTestServer(name, version)
	server.Packages = []model.Package{
		{
			RegistryType:    "npm",
			RegistryBaseURL: "https://registry.npmjs.org",
			Identifier:      "@test/package",
			Version:         "1.0.0",
			RunTimeHint:     "npx",
			FileSHA256:      "abc123def456789",
			Transport: model.Transport{
				Type: "stdio",
				URL:  "https://example.com/transport",
			},
			RuntimeArguments: []model.Argument{
				{Name: "--yes"},
			},
			PackageArguments: []model.Argument{
				{Name: "--verbose"},
			},
			EnvironmentVariables: []model.KeyValueInput{
				{Name: "NODE_ENV"},
			},
		},
	}
	return server
}

// createTestServerWithRemotes creates a test ServerJSON with remotes
func createTestServerWithRemotes(name, version string) upstreamv0.ServerJSON {
	server := createTestServer(name, version)
	server.Remotes = []model.Transport{
		{
			Type: "sse",
			URL:  "https://example.com/sse",
			Headers: []model.KeyValueInput{
				{Name: "Authorization"},
			},
		},
	}
	return server
}

// createTestServerWithIcons creates a test ServerJSON with icons
func createTestServerWithIcons(name, version string) upstreamv0.ServerJSON {
	server := createTestServer(name, version)
	lightTheme := testThemeLight
	darkTheme := testThemeDark
	mimeType := testMimeTypePNG
	server.Icons = []model.Icon{
		{
			Src:      "https://example.com/icon-light.png",
			MimeType: &mimeType,
			Theme:    &lightTheme,
		},
		{
			Src:      "https://example.com/icon-dark.png",
			MimeType: &mimeType,
			Theme:    &darkTheme,
		},
	}
	return server
}

// createTestServerWithRepository creates a test ServerJSON with repository info
func createTestServerWithRepository(name, version string) upstreamv0.ServerJSON {
	server := createTestServer(name, version)
	server.Repository = &model.Repository{
		URL:       "https://github.com/test/repo",
		Source:    "github",
		ID:        "repo-123",
		Subfolder: "src",
	}
	return server
}

// createTestServerWithMeta creates a test ServerJSON with metadata
func createTestServerWithMeta(name, version string) upstreamv0.ServerJSON {
	server := createTestServer(name, version)
	server.Meta = &upstreamv0.ServerMeta{
		PublisherProvided: map[string]interface{}{
			"custom_field": "custom_value",
			"nested": map[string]interface{}{
				"key": "value",
			},
		},
	}
	return server
}

// createFullTestServer creates a test ServerJSON with all fields populated
func createFullTestServer(name, version string) upstreamv0.ServerJSON {
	lightTheme := testThemeLight
	mimeType := testMimeTypePNG

	return upstreamv0.ServerJSON{
		Name:        name,
		Version:     version,
		Description: "Full test server description",
		Title:       "Full Test Server",
		WebsiteURL:  "https://example.com",
		Repository: &model.Repository{
			URL:       "https://github.com/test/repo",
			Source:    "github",
			ID:        "repo-123",
			Subfolder: "src",
		},
		Icons: []model.Icon{
			{
				Src:      "https://example.com/icon.png",
				MimeType: &mimeType,
				Theme:    &lightTheme,
			},
		},
		Packages: []model.Package{
			{
				RegistryType:    "npm",
				RegistryBaseURL: "https://registry.npmjs.org",
				Identifier:      "@test/package",
				Version:         "1.0.0",
				RunTimeHint:     "npx",
				Transport: model.Transport{
					Type: "stdio",
				},
				RuntimeArguments: []model.Argument{
					{Name: "--yes"},
				},
				PackageArguments: []model.Argument{
					{Name: "--verbose"},
				},
				EnvironmentVariables: []model.KeyValueInput{
					{Name: "NODE_ENV"},
				},
			},
		},
		Remotes: []model.Transport{
			{
				Type: "sse",
				URL:  "https://example.com/sse",
				Headers: []model.KeyValueInput{
					{Name: "Authorization"},
				},
			},
		},
		Meta: &upstreamv0.ServerMeta{
			PublisherProvided: map[string]interface{}{
				"custom": "value",
			},
		},
	}
}

// TestNewDBSyncWriter tests the constructor
func TestNewDBSyncWriter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		pool          *pgxpool.Pool
		expectError   bool
		errorContains string
	}{
		{
			name:        "valid pool",
			pool:        &pgxpool.Pool{}, // Non-nil pool
			expectError: false,
		},
		{
			name:          "nil pool",
			pool:          nil,
			expectError:   true,
			errorContains: "pgx pool is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			writer, err := NewDBSyncWriter(tt.pool)

			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorContains)
				assert.Nil(t, writer)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, writer)
			}
		})
	}
}

// TestDbSyncWriter_Store tests the Store method
func TestDbSyncWriter_Store(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		registryName  string
		setupFunc     func(t *testing.T, pool *pgxpool.Pool)
		registry      *toolhivetypes.UpstreamRegistry
		expectError   bool
		errorContains string
		validateFunc  func(t *testing.T, pool *pgxpool.Pool, registryName string)
	}{
		{
			name:         "successful sync with single server",
			registryName: "test-registry",
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				t.Helper()
				createTestRegistry(t, pool, "test-registry")
			},
			registry: createTestUpstreamRegistry([]upstreamv0.ServerJSON{
				createTestServer("test.org/server", "1.0.0"),
			}),
			expectError: false,
			validateFunc: func(t *testing.T, pool *pgxpool.Pool, _ string) {
				t.Helper()
				ctx := context.Background()
				queries := sqlc.New(pool)

				servers, err := queries.ListServers(ctx, sqlc.ListServersParams{Size: 100})
				require.NoError(t, err)
				require.Len(t, servers, 1)
				assert.Equal(t, "test.org/server", servers[0].Name)
				assert.Equal(t, "1.0.0", servers[0].Version)
			},
		},
		{
			name:         "successful sync with multiple servers",
			registryName: "test-registry",
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				t.Helper()
				createTestRegistry(t, pool, "test-registry")
			},
			registry: createTestUpstreamRegistry([]upstreamv0.ServerJSON{
				createTestServer("test.org/server1", "1.0.0"),
				createTestServer("test.org/server2", "2.0.0"),
				createTestServer("test.org/server3", "3.0.0"),
			}),
			expectError: false,
			validateFunc: func(t *testing.T, pool *pgxpool.Pool, _ string) {
				t.Helper()
				ctx := context.Background()
				queries := sqlc.New(pool)

				servers, err := queries.ListServers(ctx, sqlc.ListServersParams{Size: 100})
				require.NoError(t, err)
				require.Len(t, servers, 3)
			},
		},
		{
			name:         "successful sync with server packages",
			registryName: "test-registry",
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				t.Helper()
				createTestRegistry(t, pool, "test-registry")
			},
			registry: createTestUpstreamRegistry([]upstreamv0.ServerJSON{
				createTestServerWithPackages("test.org/server", "1.0.0"),
			}),
			expectError: false,
			validateFunc: func(t *testing.T, pool *pgxpool.Pool, _ string) {
				t.Helper()
				ctx := context.Background()
				queries := sqlc.New(pool)

				servers, err := queries.ListServers(ctx, sqlc.ListServersParams{Size: 100})
				require.NoError(t, err)
				require.Len(t, servers, 1)

				packages, err := queries.ListServerPackages(ctx, []uuid.UUID{servers[0].ID})
				require.NoError(t, err)
				require.Len(t, packages, 1)
				assert.Equal(t, "npm", packages[0].RegistryType)
				assert.Equal(t, "@test/package", packages[0].PkgIdentifier)
			},
		},
		{
			name:         "successful sync with server remotes",
			registryName: "test-registry",
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				t.Helper()
				createTestRegistry(t, pool, "test-registry")
			},
			registry: createTestUpstreamRegistry([]upstreamv0.ServerJSON{
				createTestServerWithRemotes("test.org/server", "1.0.0"),
			}),
			expectError: false,
			validateFunc: func(t *testing.T, pool *pgxpool.Pool, _ string) {
				t.Helper()
				ctx := context.Background()
				queries := sqlc.New(pool)

				servers, err := queries.ListServers(ctx, sqlc.ListServersParams{Size: 100})
				require.NoError(t, err)
				require.Len(t, servers, 1)

				remotes, err := queries.ListServerRemotes(ctx, []uuid.UUID{servers[0].ID})
				require.NoError(t, err)
				require.Len(t, remotes, 1)
				assert.Equal(t, "sse", remotes[0].Transport)
				assert.Equal(t, "https://example.com/sse", remotes[0].TransportUrl)
			},
		},
		{
			name:         "successful sync with server icons",
			registryName: "test-registry",
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				t.Helper()
				createTestRegistry(t, pool, "test-registry")
			},
			registry: createTestUpstreamRegistry([]upstreamv0.ServerJSON{
				createTestServerWithIcons("test.org/server", "1.0.0"),
			}),
			expectError: false,
			validateFunc: func(t *testing.T, pool *pgxpool.Pool, _ string) {
				t.Helper()
				ctx := context.Background()
				queries := sqlc.New(pool)

				servers, err := queries.ListServers(ctx, sqlc.ListServersParams{Size: 100})
				require.NoError(t, err)
				require.Len(t, servers, 1)
				// Icons are stored but not retrieved by ListServers
			},
		},
		{
			name:         "successful sync with repository info",
			registryName: "test-registry",
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				t.Helper()
				createTestRegistry(t, pool, "test-registry")
			},
			registry: createTestUpstreamRegistry([]upstreamv0.ServerJSON{
				createTestServerWithRepository("test.org/server", "1.0.0"),
			}),
			expectError: false,
			validateFunc: func(t *testing.T, pool *pgxpool.Pool, _ string) {
				t.Helper()
				ctx := context.Background()
				queries := sqlc.New(pool)

				server, err := queries.GetServerVersion(ctx, sqlc.GetServerVersionParams{
					Name:    "test.org/server",
					Version: "1.0.0",
				})
				require.NoError(t, err)
				require.NotNil(t, server.RepositoryUrl)
				assert.Equal(t, "https://github.com/test/repo", *server.RepositoryUrl)
			},
		},
		{
			name:         "successful sync with metadata",
			registryName: "test-registry",
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				t.Helper()
				createTestRegistry(t, pool, "test-registry")
			},
			registry: createTestUpstreamRegistry([]upstreamv0.ServerJSON{
				createTestServerWithMeta("test.org/server", "1.0.0"),
			}),
			expectError: false,
			validateFunc: func(t *testing.T, pool *pgxpool.Pool, _ string) {
				t.Helper()
				ctx := context.Background()
				queries := sqlc.New(pool)

				server, err := queries.GetServerVersion(ctx, sqlc.GetServerVersionParams{
					Name:    "test.org/server",
					Version: "1.0.0",
				})
				require.NoError(t, err)
				require.NotNil(t, server.ServerMeta)
			},
		},
		{
			name:         "successful sync with full server",
			registryName: "test-registry",
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				t.Helper()
				createTestRegistry(t, pool, "test-registry")
			},
			registry: createTestUpstreamRegistry([]upstreamv0.ServerJSON{
				createFullTestServer("test.org/server", "1.0.0"),
			}),
			expectError: false,
			validateFunc: func(t *testing.T, pool *pgxpool.Pool, _ string) {
				t.Helper()
				ctx := context.Background()
				queries := sqlc.New(pool)

				servers, err := queries.ListServers(ctx, sqlc.ListServersParams{Size: 100})
				require.NoError(t, err)
				require.Len(t, servers, 1)

				packages, err := queries.ListServerPackages(ctx, []uuid.UUID{servers[0].ID})
				require.NoError(t, err)
				require.Len(t, packages, 1)

				remotes, err := queries.ListServerRemotes(ctx, []uuid.UUID{servers[0].ID})
				require.NoError(t, err)
				require.Len(t, remotes, 1)
			},
		},
		{
			name:         "successful sync with empty servers list",
			registryName: "test-registry",
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				t.Helper()
				createTestRegistry(t, pool, "test-registry")
			},
			registry:    createTestUpstreamRegistry([]upstreamv0.ServerJSON{}),
			expectError: false,
			validateFunc: func(t *testing.T, pool *pgxpool.Pool, _ string) {
				t.Helper()
				ctx := context.Background()
				queries := sqlc.New(pool)

				servers, err := queries.ListServers(ctx, sqlc.ListServersParams{Size: 100})
				require.NoError(t, err)
				require.Len(t, servers, 0)
			},
		},
		{
			name:         "successful sync replaces existing servers",
			registryName: "test-registry",
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				t.Helper()
				ctx := context.Background()
				regID := createTestRegistry(t, pool, "test-registry")

				// Insert existing server that should be deleted
				queries := sqlc.New(pool)
				_, err := queries.InsertServerVersion(ctx, sqlc.InsertServerVersionParams{
					Name:    "test.org/old-server",
					Version: "0.1.0",
					RegID:   regID,
				})
				require.NoError(t, err)
			},
			registry: createTestUpstreamRegistry([]upstreamv0.ServerJSON{
				createTestServer("test.org/new-server", "1.0.0"),
			}),
			expectError: false,
			validateFunc: func(t *testing.T, pool *pgxpool.Pool, _ string) {
				t.Helper()
				ctx := context.Background()
				queries := sqlc.New(pool)

				servers, err := queries.ListServers(ctx, sqlc.ListServersParams{Size: 100})
				require.NoError(t, err)
				require.Len(t, servers, 1)
				assert.Equal(t, "test.org/new-server", servers[0].Name)
			},
		},
		{
			name:         "successful sync updates latest version - single version",
			registryName: "test-registry",
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				t.Helper()
				createTestRegistry(t, pool, "test-registry")
			},
			registry: createTestUpstreamRegistry([]upstreamv0.ServerJSON{
				createTestServer("test.org/server", "1.0.0"),
			}),
			expectError: false,
			validateFunc: func(t *testing.T, pool *pgxpool.Pool, _ string) {
				t.Helper()
				ctx := context.Background()
				queries := sqlc.New(pool)

				server, err := queries.GetServerVersion(ctx, sqlc.GetServerVersionParams{
					Name:    "test.org/server",
					Version: "1.0.0",
				})
				require.NoError(t, err)
				// Single version should be marked as latest
				assert.True(t, server.IsLatest)
			},
		},
		{
			name:         "successful sync updates latest version - multiple versions",
			registryName: "test-registry",
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				t.Helper()
				createTestRegistry(t, pool, "test-registry")
			},
			registry: createTestUpstreamRegistry([]upstreamv0.ServerJSON{
				createTestServer("test.org/server", "1.0.0"),
				createTestServer("test.org/server", "2.0.0"),
				createTestServer("test.org/server", "1.5.0"),
			}),
			expectError: false,
			validateFunc: func(t *testing.T, pool *pgxpool.Pool, _ string) {
				t.Helper()
				ctx := context.Background()
				queries := sqlc.New(pool)

				// Version 2.0.0 should be latest
				server, err := queries.GetServerVersion(ctx, sqlc.GetServerVersionParams{
					Name:    "test.org/server",
					Version: "2.0.0",
				})
				require.NoError(t, err)
				assert.True(t, server.IsLatest)

				// Version 1.0.0 should not be latest
				server, err = queries.GetServerVersion(ctx, sqlc.GetServerVersionParams{
					Name:    "test.org/server",
					Version: "1.0.0",
				})
				require.NoError(t, err)
				assert.False(t, server.IsLatest)

				// Version 1.5.0 should not be latest
				server, err = queries.GetServerVersion(ctx, sqlc.GetServerVersionParams{
					Name:    "test.org/server",
					Version: "1.5.0",
				})
				require.NoError(t, err)
				assert.False(t, server.IsLatest)
			},
		},
		{
			name:          "registry not found error",
			registryName:  "non-existent-registry",
			setupFunc:     func(_ *testing.T, _ *pgxpool.Pool) {},
			registry:      createTestUpstreamRegistry([]upstreamv0.ServerJSON{}),
			expectError:   true,
			errorContains: "registry not found",
		},
		{
			name:          "nil registry error",
			registryName:  "test-registry",
			setupFunc:     func(_ *testing.T, _ *pgxpool.Pool) {},
			registry:      nil,
			expectError:   true,
			errorContains: "registry data is required",
		},
		{
			name:         "multiple servers same name different versions",
			registryName: "test-registry",
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				t.Helper()
				createTestRegistry(t, pool, "test-registry")
			},
			registry: createTestUpstreamRegistry([]upstreamv0.ServerJSON{
				createTestServer("test.org/server", "1.0.0"),
				createTestServer("test.org/server", "1.1.0"),
				createTestServer("test.org/server", "2.0.0"),
			}),
			expectError: false,
			validateFunc: func(t *testing.T, pool *pgxpool.Pool, _ string) {
				t.Helper()
				ctx := context.Background()
				queries := sqlc.New(pool)

				versions, err := queries.ListServerVersions(ctx, sqlc.ListServerVersionsParams{
					Name: "test.org/server",
					Size: 100,
				})
				require.NoError(t, err)
				require.Len(t, versions, 3)
			},
		},
		{
			name:         "server with empty optional fields",
			registryName: "test-registry",
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				t.Helper()
				createTestRegistry(t, pool, "test-registry")
			},
			registry: createTestUpstreamRegistry([]upstreamv0.ServerJSON{
				{
					Name:        "test.org/minimal",
					Version:     "1.0.0",
					Description: "",
					Title:       "",
					WebsiteURL:  "",
				},
			}),
			expectError: false,
			validateFunc: func(t *testing.T, pool *pgxpool.Pool, _ string) {
				t.Helper()
				ctx := context.Background()
				queries := sqlc.New(pool)

				server, err := queries.GetServerVersion(ctx, sqlc.GetServerVersionParams{
					Name:    "test.org/minimal",
					Version: "1.0.0",
				})
				require.NoError(t, err)
				assert.Nil(t, server.Description)
				assert.Nil(t, server.Title)
				assert.Nil(t, server.Website)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pool, cleanup := setupTestDB(t)
			defer cleanup()

			tt.setupFunc(t, pool)

			writer, err := NewDBSyncWriter(pool)
			require.NoError(t, err)

			err = writer.Store(context.Background(), tt.registryName, tt.registry)

			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				require.NoError(t, err)
				if tt.validateFunc != nil {
					tt.validateFunc(t, pool, tt.registryName)
				}
			}
		})
	}
}

// TestIsNewerVersion tests the semver comparison function
func TestIsNewerVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		newVersion string
		oldVersion string
		expected   bool
	}{
		// Valid semver comparisons
		{
			name:       "newer major version",
			newVersion: "2.0.0",
			oldVersion: "1.0.0",
			expected:   true,
		},
		{
			name:       "newer minor version",
			newVersion: "1.2.0",
			oldVersion: "1.1.0",
			expected:   true,
		},
		{
			name:       "newer patch version",
			newVersion: "1.0.2",
			oldVersion: "1.0.1",
			expected:   true,
		},
		{
			name:       "older major version",
			newVersion: "1.0.0",
			oldVersion: "2.0.0",
			expected:   false,
		},
		{
			name:       "older minor version",
			newVersion: "1.1.0",
			oldVersion: "1.2.0",
			expected:   false,
		},
		{
			name:       "older patch version",
			newVersion: "1.0.1",
			oldVersion: "1.0.2",
			expected:   false,
		},
		{
			name:       "equal versions",
			newVersion: "1.0.0",
			oldVersion: "1.0.0",
			expected:   false,
		},
		{
			name:       "prerelease vs release",
			newVersion: "1.0.0",
			oldVersion: "1.0.0-alpha",
			expected:   true,
		},
		{
			name:       "release vs prerelease",
			newVersion: "1.0.0-alpha",
			oldVersion: "1.0.0",
			expected:   false,
		},
		{
			name:       "newer prerelease",
			newVersion: "1.0.0-beta",
			oldVersion: "1.0.0-alpha",
			expected:   true,
		},
		// Fallback to string comparison for non-semver
		{
			name:       "non-semver string comparison newer",
			newVersion: "version-b",
			oldVersion: "version-a",
			expected:   true,
		},
		{
			name:       "non-semver string comparison older",
			newVersion: "version-a",
			oldVersion: "version-b",
			expected:   false,
		},
		{
			name:       "non-semver equal",
			newVersion: "custom-v1",
			oldVersion: "custom-v1",
			expected:   false,
		},
		{
			name:       "mixed semver and non-semver - semver first",
			newVersion: "1.0.0",
			oldVersion: "invalid-version",
			expected:   false, // Falls back to string comparison
		},
		{
			name:       "mixed semver and non-semver - non-semver first",
			newVersion: "invalid-version",
			oldVersion: "1.0.0",
			expected:   true, // Falls back to string comparison, "i" > "1"
		},
		{
			name:       "empty new version",
			newVersion: "",
			oldVersion: "1.0.0",
			expected:   false,
		},
		{
			name:       "empty old version",
			newVersion: "1.0.0",
			oldVersion: "",
			expected:   true,
		},
		{
			name:       "both empty",
			newVersion: "",
			oldVersion: "",
			expected:   false,
		},
		// Edge cases with v prefix
		{
			name:       "v prefix newer",
			newVersion: "v2.0.0",
			oldVersion: "v1.0.0",
			expected:   true,
		},
		{
			name:       "v prefix older",
			newVersion: "v1.0.0",
			oldVersion: "v2.0.0",
			expected:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := isNewerVersion(tt.newVersion, tt.oldVersion)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestServerKey tests the server key generation function
func TestServerKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		server   string
		version  string
		expected string
	}{
		{
			name:     "simple name and version",
			server:   "test-server",
			version:  "1.0.0",
			expected: "test-server@1.0.0",
		},
		{
			name:     "namespaced server",
			server:   "test.org/my-server",
			version:  "2.0.0",
			expected: "test.org/my-server@2.0.0",
		},
		{
			name:     "complex version",
			server:   "server",
			version:  "1.0.0-alpha+build.123",
			expected: "server@1.0.0-alpha+build.123",
		},
		{
			name:     "empty name",
			server:   "",
			version:  "1.0.0",
			expected: "@1.0.0",
		},
		{
			name:     "empty version",
			server:   "server",
			version:  "",
			expected: "server@",
		},
		{
			name:     "both empty",
			server:   "",
			version:  "",
			expected: "@",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := serverKey(tt.server, tt.version)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestNilIfEmpty tests the nilIfEmpty helper function
func TestNilIfEmpty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected *string
	}{
		{
			name:     "non-empty string",
			input:    "test",
			expected: strPtr("test"),
		},
		{
			name:     "empty string",
			input:    "",
			expected: nil,
		},
		{
			name:     "whitespace only",
			input:    "   ",
			expected: strPtr("   "),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := nilIfEmpty(tt.input)
			if tt.expected == nil {
				assert.Nil(t, result)
			} else {
				require.NotNil(t, result)
				assert.Equal(t, *tt.expected, *result)
			}
		})
	}
}

// strPtr returns a pointer to the given string
func strPtr(s string) *string {
	return &s
}

// TestExtractArgumentValues tests the extractArgumentValues helper function
func TestExtractArgumentValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		arguments []model.Argument
		expected  []string
	}{
		{
			name:      "empty arguments",
			arguments: []model.Argument{},
			expected:  []string{},
		},
		{
			name: "single argument",
			arguments: []model.Argument{
				{Name: "--verbose"},
			},
			expected: []string{"--verbose"},
		},
		{
			name: "multiple arguments",
			arguments: []model.Argument{
				{Name: "--verbose"},
				{Name: "--output"},
				{Name: "-f"},
			},
			expected: []string{"--verbose", "--output", "-f"},
		},
		{
			name:      "nil arguments",
			arguments: nil,
			expected:  []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := extractArgumentValues(tt.arguments)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestSerializeKeyValueInputs tests the serializeKeyValueInputs helper function
func TestSerializeKeyValueInputs(t *testing.T) {
	t.Parallel()

	// Create test inputs with full metadata using proper nested struct syntax
	inputWithMetadata := model.KeyValueInput{
		InputWithVariables: model.InputWithVariables{
			Input: model.Input{
				Default:     "default_value",
				Description: "API key for authentication",
				IsSecret:    true,
				IsRequired:  true,
			},
		},
		Name: "API_KEY",
	}

	inputWithSecret := model.KeyValueInput{Name: "API_KEY"}
	inputWithSecret.IsSecret = true

	inputWithDefault := model.KeyValueInput{Name: "DEBUG"}
	inputWithDefault.Default = "false"

	tests := []struct {
		name     string
		kvInputs []model.KeyValueInput
		expected string
	}{
		{
			name:     "empty inputs",
			kvInputs: []model.KeyValueInput{},
			expected: "[]",
		},
		{
			name: "single input with name only",
			kvInputs: []model.KeyValueInput{
				{Name: "NODE_ENV"},
			},
			expected: `[{"name":"NODE_ENV"}]`,
		},
		{
			name:     "input with full metadata",
			kvInputs: []model.KeyValueInput{inputWithMetadata},
			expected: `[{"name":"API_KEY","default":"default_value","description":"API key for authentication","isSecret":true,"isRequired":true}]`,
		},
		{
			name:     "multiple inputs",
			kvInputs: []model.KeyValueInput{{Name: "NODE_ENV"}, inputWithSecret, inputWithDefault},
			expected: `[{"name":"NODE_ENV"},{"name":"API_KEY","isSecret":true},{"name":"DEBUG","default":"false"}]`,
		},
		{
			name:     "nil inputs",
			kvInputs: nil,
			expected: "[]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := serializeKeyValueInputs(tt.kvInputs)
			assert.JSONEq(t, tt.expected, string(result))
		})
	}
}

// TestSerializeServerMeta tests the serializeServerMeta helper function
func TestSerializeServerMeta(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		meta        *upstreamv0.ServerMeta
		expectNil   bool
		expectError bool
	}{
		{
			name:      "nil meta",
			meta:      nil,
			expectNil: true,
		},
		{
			name: "nil publisher provided",
			meta: &upstreamv0.ServerMeta{
				PublisherProvided: nil,
			},
			expectNil: true,
		},
		{
			name: "empty publisher provided",
			meta: &upstreamv0.ServerMeta{
				PublisherProvided: map[string]interface{}{},
			},
			expectNil: true,
		},
		{
			name: "with publisher provided data",
			meta: &upstreamv0.ServerMeta{
				PublisherProvided: map[string]interface{}{
					"key": "value",
				},
			},
			expectNil: false,
		},
		{
			name: "with nested data",
			meta: &upstreamv0.ServerMeta{
				PublisherProvided: map[string]interface{}{
					"nested": map[string]interface{}{
						"key": "value",
					},
				},
			},
			expectNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := serializeServerMeta(tt.meta)

			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				if tt.expectNil {
					assert.Nil(t, result)
				} else {
					assert.NotNil(t, result)
					assert.Greater(t, len(result), 0)
				}
			}
		})
	}
}

// TestDbSyncWriter_Store_ContextCancellation tests context cancellation behavior
func TestDbSyncWriter_Store_ContextCancellation(t *testing.T) {
	t.Parallel()

	pool, cleanup := setupTestDB(t)
	defer cleanup()

	createTestRegistry(t, pool, "test-registry")

	writer, err := NewDBSyncWriter(pool)
	require.NoError(t, err)

	// Create a cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	registry := createTestUpstreamRegistry([]upstreamv0.ServerJSON{
		createTestServer("test.org/server", "1.0.0"),
	})

	err = writer.Store(ctx, "test-registry", registry)
	require.Error(t, err)
}

// TestDbSyncWriter_Store_IconThemes tests icon theme handling
func TestDbSyncWriter_Store_IconThemes(t *testing.T) {
	t.Parallel()

	pool, cleanup := setupTestDB(t)
	defer cleanup()

	createTestRegistry(t, pool, "test-registry")

	writer, err := NewDBSyncWriter(pool)
	require.NoError(t, err)

	// Create server with various icon themes
	mimeType := testMimeTypePNG
	lightTheme := testThemeLight
	darkTheme := testThemeDark
	unknownTheme := "custom"

	server := createTestServer("test.org/server", "1.0.0")
	server.Icons = []model.Icon{
		{
			Src:      "https://example.com/light.png",
			MimeType: &mimeType,
			Theme:    &lightTheme,
		},
		{
			Src:      "https://example.com/dark.png",
			MimeType: &mimeType,
			Theme:    &darkTheme,
		},
		{
			Src:      "https://example.com/unknown.png",
			MimeType: &mimeType,
			Theme:    &unknownTheme, // Should default to light
		},
		{
			Src:      "https://example.com/nil-theme.png",
			MimeType: &mimeType,
			Theme:    nil, // Should default to light
		},
	}

	registry := createTestUpstreamRegistry([]upstreamv0.ServerJSON{server})

	err = writer.Store(context.Background(), "test-registry", registry)
	require.NoError(t, err)

	// Verify servers were created
	ctx := context.Background()
	queries := sqlc.New(pool)
	servers, err := queries.ListServers(ctx, sqlc.ListServersParams{Size: 100})
	require.NoError(t, err)
	require.Len(t, servers, 1)
}

// TestDbSyncWriter_Store_PackageAcrossServers tests packages across multiple server versions
// Note: The database schema only allows one package per server version (server_id is PRIMARY KEY)
func TestDbSyncWriter_Store_PackageAcrossServers(t *testing.T) {
	t.Parallel()

	pool, cleanup := setupTestDB(t)
	defer cleanup()

	createTestRegistry(t, pool, "test-registry")

	writer, err := NewDBSyncWriter(pool)
	require.NoError(t, err)

	// Create multiple server versions, each with their own package
	servers := []upstreamv0.ServerJSON{
		{
			Name:        "test.org/server",
			Version:     "1.0.0",
			Description: "Test server v1",
			Packages: []model.Package{
				{
					RegistryType:    "npm",
					RegistryBaseURL: "https://registry.npmjs.org",
					Identifier:      "@test/package",
					Version:         "1.0.0",
					Transport:       model.Transport{Type: "stdio"},
				},
			},
		},
		{
			Name:        "test.org/server",
			Version:     "2.0.0",
			Description: "Test server v2",
			Packages: []model.Package{
				{
					RegistryType:    "pypi",
					RegistryBaseURL: "https://pypi.org/simple",
					Identifier:      "test-package",
					Version:         "2.0.0",
					Transport:       model.Transport{Type: "stdio"},
				},
			},
		},
		{
			Name:        "test.org/other-server",
			Version:     "1.0.0",
			Description: "Other test server",
			Packages: []model.Package{
				{
					RegistryType: "oci",
					Identifier:   "ghcr.io/test/package:v1.0.0",
					Transport:    model.Transport{Type: "stdio"},
				},
			},
		},
	}

	registry := createTestUpstreamRegistry(servers)

	err = writer.Store(context.Background(), "test-registry", registry)
	require.NoError(t, err)

	ctx := context.Background()
	queries := sqlc.New(pool)

	serverRows, err := queries.ListServers(ctx, sqlc.ListServersParams{Size: 100})
	require.NoError(t, err)
	require.Len(t, serverRows, 3)

	// Extract server IDs
	serverIDs := make([]uuid.UUID, len(serverRows))
	for i, s := range serverRows {
		serverIDs[i] = s.ID
	}

	packages, err := queries.ListServerPackages(ctx, serverIDs)
	require.NoError(t, err)
	require.Len(t, packages, 3)
}

// TestDbSyncWriter_Store_MultipleRemotes tests multiple remotes per server
func TestDbSyncWriter_Store_MultipleRemotes(t *testing.T) {
	t.Parallel()

	pool, cleanup := setupTestDB(t)
	defer cleanup()

	createTestRegistry(t, pool, "test-registry")

	writer, err := NewDBSyncWriter(pool)
	require.NoError(t, err)

	server := createTestServer("test.org/server", "1.0.0")
	server.Remotes = []model.Transport{
		{
			Type: "sse",
			URL:  "https://example.com/sse1",
		},
		{
			Type: "sse",
			URL:  "https://example.com/sse2",
		},
		{
			Type: "streamable-http",
			URL:  "https://example.com/http",
		},
	}

	registry := createTestUpstreamRegistry([]upstreamv0.ServerJSON{server})

	err = writer.Store(context.Background(), "test-registry", registry)
	require.NoError(t, err)

	ctx := context.Background()
	queries := sqlc.New(pool)

	servers, err := queries.ListServers(ctx, sqlc.ListServersParams{Size: 100})
	require.NoError(t, err)
	require.Len(t, servers, 1)

	remotes, err := queries.ListServerRemotes(ctx, []uuid.UUID{servers[0].ID})
	require.NoError(t, err)
	require.Len(t, remotes, 3)
}

// TestDbSyncWriter_Store_LatestVersionDetermination tests latest version logic
func TestDbSyncWriter_Store_LatestVersionDetermination(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		versions       []string
		expectedLatest string
		serverName     string
		registryName   string
	}{
		{
			name:           "standard semver versions",
			versions:       []string{"1.0.0", "2.0.0", "1.5.0"},
			expectedLatest: "2.0.0",
			serverName:     "test.org/server1",
			registryName:   "test-registry-1",
		},
		{
			name:           "with prerelease versions",
			versions:       []string{"1.0.0", "1.0.1-alpha", "1.0.1"},
			expectedLatest: "1.0.1",
			serverName:     "test.org/server2",
			registryName:   "test-registry-2",
		},
		{
			name:           "all prerelease versions",
			versions:       []string{"1.0.0-alpha", "1.0.0-beta", "1.0.0-rc"},
			expectedLatest: "1.0.0-rc",
			serverName:     "test.org/server3",
			registryName:   "test-registry-3",
		},
		{
			name:           "single version",
			versions:       []string{"1.0.0"},
			expectedLatest: "1.0.0",
			serverName:     "test.org/server4",
			registryName:   "test-registry-4",
		},
		{
			name:           "with v prefix",
			versions:       []string{"v1.0.0", "v2.0.0", "v1.5.0"},
			expectedLatest: "v2.0.0",
			serverName:     "test.org/server5",
			registryName:   "test-registry-5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pool, cleanup := setupTestDB(t)
			defer cleanup()

			createTestRegistry(t, pool, tt.registryName)

			writer, err := NewDBSyncWriter(pool)
			require.NoError(t, err)

			servers := make([]upstreamv0.ServerJSON, len(tt.versions))
			for i, version := range tt.versions {
				servers[i] = createTestServer(tt.serverName, version)
			}

			registry := createTestUpstreamRegistry(servers)
			err = writer.Store(context.Background(), tt.registryName, registry)
			require.NoError(t, err)

			ctx := context.Background()
			queries := sqlc.New(pool)

			for _, version := range tt.versions {
				server, err := queries.GetServerVersion(ctx, sqlc.GetServerVersionParams{
					Name:    tt.serverName,
					Version: version,
				})
				require.NoError(t, err)

				if version == tt.expectedLatest {
					assert.True(t, server.IsLatest, "Expected %s to be latest", version)
				} else {
					assert.False(t, server.IsLatest, "Expected %s to not be latest", version)
				}
			}
		})
	}
}

// TestDbSyncWriter_Store_UUIDStability verifies that when the same server is synced twice
// without changes, it keeps the same UUID.
func TestDbSyncWriter_Store_UUIDStability(t *testing.T) {
	t.Parallel()

	pool, cleanup := setupTestDB(t)
	defer cleanup()

	createTestRegistry(t, pool, "test-registry")

	writer, err := NewDBSyncWriter(pool)
	require.NoError(t, err)

	ctx := context.Background()
	queries := sqlc.New(pool)

	// Create UpstreamRegistry with 2 servers
	registry := createTestUpstreamRegistry([]upstreamv0.ServerJSON{
		createTestServer("test.org/server-a", "1.0.0"),
		createTestServer("test.org/server-b", "2.0.0"),
	})

	// First sync
	err = writer.Store(ctx, "test-registry", registry)
	require.NoError(t, err)

	// Query DB and record server UUIDs and created_at timestamps
	serverA1, err := queries.GetServerVersion(ctx, sqlc.GetServerVersionParams{
		Name:    "test.org/server-a",
		Version: "1.0.0",
	})
	require.NoError(t, err)

	serverB1, err := queries.GetServerVersion(ctx, sqlc.GetServerVersionParams{
		Name:    "test.org/server-b",
		Version: "2.0.0",
	})
	require.NoError(t, err)

	// Store original UUIDs and timestamps
	originalUUIDA := serverA1.ID
	originalUUIDB := serverB1.ID
	originalCreatedAtA := serverA1.CreatedAt
	originalCreatedAtB := serverB1.CreatedAt

	// Second sync with exact same data
	err = writer.Store(ctx, "test-registry", registry)
	require.NoError(t, err)

	// Query DB again and verify UUIDs are identical
	serverA2, err := queries.GetServerVersion(ctx, sqlc.GetServerVersionParams{
		Name:    "test.org/server-a",
		Version: "1.0.0",
	})
	require.NoError(t, err)

	serverB2, err := queries.GetServerVersion(ctx, sqlc.GetServerVersionParams{
		Name:    "test.org/server-b",
		Version: "2.0.0",
	})
	require.NoError(t, err)

	// Verify UUIDs are the same (not new UUIDs)
	assert.Equal(t, originalUUIDA, serverA2.ID, "Server A UUID should be preserved after re-sync")
	assert.Equal(t, originalUUIDB, serverB2.ID, "Server B UUID should be preserved after re-sync")

	// Verify created_at is preserved
	assert.Equal(t, originalCreatedAtA, serverA2.CreatedAt, "Server A created_at should be preserved")
	assert.Equal(t, originalCreatedAtB, serverB2.CreatedAt, "Server B created_at should be preserved")

	// Verify updated_at changed (or at least is not nil)
	assert.NotNil(t, serverA2.UpdatedAt, "Server A updated_at should be set")
	assert.NotNil(t, serverB2.UpdatedAt, "Server B updated_at should be set")
}

// TestDbSyncWriter_Store_UpdatePreservesUUID verifies that when a server's fields change,
// the UUID stays the same but fields are updated.
func TestDbSyncWriter_Store_UpdatePreservesUUID(t *testing.T) {
	t.Parallel()

	pool, cleanup := setupTestDB(t)
	defer cleanup()

	createTestRegistry(t, pool, "test-registry")

	writer, err := NewDBSyncWriter(pool)
	require.NoError(t, err)

	ctx := context.Background()
	queries := sqlc.New(pool)

	// First sync with original description
	server := createTestServer("test.org/server", "1.0.0")
	server.Description = "Old description"
	server.Title = "Old Title"
	registry := createTestUpstreamRegistry([]upstreamv0.ServerJSON{server})

	err = writer.Store(ctx, "test-registry", registry)
	require.NoError(t, err)

	// Query and record original UUID and description
	serverV1, err := queries.GetServerVersion(ctx, sqlc.GetServerVersionParams{
		Name:    "test.org/server",
		Version: "1.0.0",
	})
	require.NoError(t, err)

	originalUUID := serverV1.ID
	originalCreatedAt := serverV1.CreatedAt
	require.NotNil(t, serverV1.Description)
	assert.Equal(t, "Old description", *serverV1.Description)
	require.NotNil(t, serverV1.Title)
	assert.Equal(t, "Old Title", *serverV1.Title)

	// Second sync with updated description
	serverUpdated := createTestServer("test.org/server", "1.0.0")
	serverUpdated.Description = "New description"
	serverUpdated.Title = "New Title"
	registryUpdated := createTestUpstreamRegistry([]upstreamv0.ServerJSON{serverUpdated})

	err = writer.Store(ctx, "test-registry", registryUpdated)
	require.NoError(t, err)

	// Query and verify UUID is the same but description is updated
	serverV2, err := queries.GetServerVersion(ctx, sqlc.GetServerVersionParams{
		Name:    "test.org/server",
		Version: "1.0.0",
	})
	require.NoError(t, err)

	// Verify UUID is preserved
	assert.Equal(t, originalUUID, serverV2.ID, "Server UUID should be preserved after update")

	// Verify created_at is preserved
	assert.Equal(t, originalCreatedAt, serverV2.CreatedAt, "Server created_at should be preserved")

	// Verify fields are updated
	require.NotNil(t, serverV2.Description)
	assert.Equal(t, "New description", *serverV2.Description, "Description should be updated")
	require.NotNil(t, serverV2.Title)
	assert.Equal(t, "New Title", *serverV2.Title, "Title should be updated")

	// Verify updated_at changed
	assert.NotNil(t, serverV2.UpdatedAt, "updated_at should be set")
}

// TestDbSyncWriter_Store_OrphanedServerCleanup verifies that servers removed from upstream
// are deleted from the database.
func TestDbSyncWriter_Store_OrphanedServerCleanup(t *testing.T) {
	t.Parallel()

	pool, cleanup := setupTestDB(t)
	defer cleanup()

	createTestRegistry(t, pool, "test-registry")

	writer, err := NewDBSyncWriter(pool)
	require.NoError(t, err)

	ctx := context.Background()
	queries := sqlc.New(pool)

	// First sync with 3 servers
	registry := createTestUpstreamRegistry([]upstreamv0.ServerJSON{
		createTestServer("test.org/server-a", "1.0.0"),
		createTestServer("test.org/server-b", "1.0.0"),
		createTestServer("test.org/server-c", "1.0.0"),
	})

	err = writer.Store(ctx, "test-registry", registry)
	require.NoError(t, err)

	// Verify all 3 servers exist
	servers, err := queries.ListServers(ctx, sqlc.ListServersParams{Size: 100})
	require.NoError(t, err)
	require.Len(t, servers, 3, "Should have 3 servers after first sync")

	// Record UUID for server-a to verify it persists
	serverA, err := queries.GetServerVersion(ctx, sqlc.GetServerVersionParams{
		Name:    "test.org/server-a",
		Version: "1.0.0",
	})
	require.NoError(t, err)
	originalUUIDA := serverA.ID

	// Second sync with only 2 servers (server-b removed)
	registryUpdated := createTestUpstreamRegistry([]upstreamv0.ServerJSON{
		createTestServer("test.org/server-a", "1.0.0"),
		createTestServer("test.org/server-c", "1.0.0"),
	})

	err = writer.Store(ctx, "test-registry", registryUpdated)
	require.NoError(t, err)

	// Verify only server-a and server-c exist
	servers, err = queries.ListServers(ctx, sqlc.ListServersParams{Size: 100})
	require.NoError(t, err)
	require.Len(t, servers, 2, "Should have 2 servers after second sync")

	// Verify server-a still exists with same UUID
	serverAUpdated, err := queries.GetServerVersion(ctx, sqlc.GetServerVersionParams{
		Name:    "test.org/server-a",
		Version: "1.0.0",
	})
	require.NoError(t, err)
	assert.Equal(t, originalUUIDA, serverAUpdated.ID, "Server A UUID should be preserved")

	// Verify server-c exists
	_, err = queries.GetServerVersion(ctx, sqlc.GetServerVersionParams{
		Name:    "test.org/server-c",
		Version: "1.0.0",
	})
	require.NoError(t, err)

	// Verify server-b was deleted (should return error)
	_, err = queries.GetServerVersion(ctx, sqlc.GetServerVersionParams{
		Name:    "test.org/server-b",
		Version: "1.0.0",
	})
	require.Error(t, err, "Server B should have been deleted")
}

// TestDbSyncWriter_Store_PackageCleanup verifies that when a package is changed or removed
// from a server, it is properly updated.
// Note: The database schema only allows ONE package per server version (server_id is PRIMARY KEY).
func TestDbSyncWriter_Store_PackageCleanup(t *testing.T) {
	t.Parallel()

	pool, cleanup := setupTestDB(t)
	defer cleanup()

	createTestRegistry(t, pool, "test-registry")

	writer, err := NewDBSyncWriter(pool)
	require.NoError(t, err)

	ctx := context.Background()
	queries := sqlc.New(pool)

	// Create server with a package
	server := createTestServer("test.org/server", "1.0.0")
	server.Packages = []model.Package{
		{
			RegistryType:    "npm",
			RegistryBaseURL: "https://registry.npmjs.org",
			Identifier:      "@test/package-1",
			Version:         "1.0.0",
			Transport:       model.Transport{Type: "stdio"},
		},
	}
	registry := createTestUpstreamRegistry([]upstreamv0.ServerJSON{server})

	// First sync
	err = writer.Store(ctx, "test-registry", registry)
	require.NoError(t, err)

	// Verify package exists
	servers, err := queries.ListServers(ctx, sqlc.ListServersParams{Size: 100})
	require.NoError(t, err)
	require.Len(t, servers, 1)

	packages, err := queries.ListServerPackages(ctx, []uuid.UUID{servers[0].ID})
	require.NoError(t, err)
	require.Len(t, packages, 1, "Should have 1 package after first sync")
	assert.Equal(t, "@test/package-1", packages[0].PkgIdentifier)

	originalUUID := servers[0].ID

	// Second sync with different package (package changed)
	serverUpdated := createTestServer("test.org/server", "1.0.0")
	serverUpdated.Packages = []model.Package{
		{
			RegistryType:    "pypi",
			RegistryBaseURL: "https://pypi.org/simple",
			Identifier:      "new-test-package",
			Version:         "2.0.0",
			Transport:       model.Transport{Type: "stdio"},
		},
	}
	registryUpdated := createTestUpstreamRegistry([]upstreamv0.ServerJSON{serverUpdated})

	err = writer.Store(ctx, "test-registry", registryUpdated)
	require.NoError(t, err)

	// Verify server UUID is preserved
	serverAfterUpdate, err := queries.GetServerVersion(ctx, sqlc.GetServerVersionParams{
		Name:    "test.org/server",
		Version: "1.0.0",
	})
	require.NoError(t, err)
	assert.Equal(t, originalUUID, serverAfterUpdate.ID, "Server UUID should be preserved")

	// Verify package was replaced with new package
	packages, err = queries.ListServerPackages(ctx, []uuid.UUID{originalUUID})
	require.NoError(t, err)
	require.Len(t, packages, 1, "Should have 1 package after second sync")
	assert.Equal(t, "new-test-package", packages[0].PkgIdentifier, "Package identifier should be updated")
	assert.Equal(t, "pypi", packages[0].RegistryType, "Package registry type should be updated")

	// Third sync with no packages (package removed)
	serverNoPackages := createTestServer("test.org/server", "1.0.0")
	serverNoPackages.Packages = nil
	registryNoPackages := createTestUpstreamRegistry([]upstreamv0.ServerJSON{serverNoPackages})

	err = writer.Store(ctx, "test-registry", registryNoPackages)
	require.NoError(t, err)

	// Verify server UUID is still preserved
	serverAfterRemoval, err := queries.GetServerVersion(ctx, sqlc.GetServerVersionParams{
		Name:    "test.org/server",
		Version: "1.0.0",
	})
	require.NoError(t, err)
	assert.Equal(t, originalUUID, serverAfterRemoval.ID, "Server UUID should be preserved after package removal")

	// Verify no packages exist
	packages, err = queries.ListServerPackages(ctx, []uuid.UUID{originalUUID})
	require.NoError(t, err)
	require.Len(t, packages, 0, "Should have 0 packages after third sync")
}

// TestDbSyncWriter_Store_RemoteCleanup verifies that when remotes are removed from a server,
// they are cleaned up.
func TestDbSyncWriter_Store_RemoteCleanup(t *testing.T) {
	t.Parallel()

	pool, cleanup := setupTestDB(t)
	defer cleanup()

	createTestRegistry(t, pool, "test-registry")

	writer, err := NewDBSyncWriter(pool)
	require.NoError(t, err)

	ctx := context.Background()
	queries := sqlc.New(pool)

	// Create server with 2 remotes
	server := createTestServer("test.org/server", "1.0.0")
	server.Remotes = []model.Transport{
		{
			Type: "sse",
			URL:  "https://example.com/sse1",
		},
		{
			Type: "sse",
			URL:  "https://example.com/sse2",
		},
	}
	registry := createTestUpstreamRegistry([]upstreamv0.ServerJSON{server})

	// First sync
	err = writer.Store(ctx, "test-registry", registry)
	require.NoError(t, err)

	// Verify 2 remotes exist
	servers, err := queries.ListServers(ctx, sqlc.ListServersParams{Size: 100})
	require.NoError(t, err)
	require.Len(t, servers, 1)

	remotes, err := queries.ListServerRemotes(ctx, []uuid.UUID{servers[0].ID})
	require.NoError(t, err)
	require.Len(t, remotes, 2, "Should have 2 remotes after first sync")

	originalUUID := servers[0].ID

	// Second sync with only 1 remote
	serverUpdated := createTestServer("test.org/server", "1.0.0")
	serverUpdated.Remotes = []model.Transport{
		{
			Type: "sse",
			URL:  "https://example.com/sse1",
		},
	}
	registryUpdated := createTestUpstreamRegistry([]upstreamv0.ServerJSON{serverUpdated})

	err = writer.Store(ctx, "test-registry", registryUpdated)
	require.NoError(t, err)

	// Verify server UUID is preserved
	serverAfterUpdate, err := queries.GetServerVersion(ctx, sqlc.GetServerVersionParams{
		Name:    "test.org/server",
		Version: "1.0.0",
	})
	require.NoError(t, err)
	assert.Equal(t, originalUUID, serverAfterUpdate.ID, "Server UUID should be preserved")

	// Verify only 1 remote exists
	remotes, err = queries.ListServerRemotes(ctx, []uuid.UUID{originalUUID})
	require.NoError(t, err)
	require.Len(t, remotes, 1, "Should have 1 remote after second sync")
	assert.Equal(t, "https://example.com/sse1", remotes[0].TransportUrl)
}

// TestDbSyncWriter_Store_IconCleanup verifies that when icons are removed from a server,
// they are cleaned up.
func TestDbSyncWriter_Store_IconCleanup(t *testing.T) {
	t.Parallel()

	pool, cleanup := setupTestDB(t)
	defer cleanup()

	createTestRegistry(t, pool, "test-registry")

	writer, err := NewDBSyncWriter(pool)
	require.NoError(t, err)

	ctx := context.Background()
	queries := sqlc.New(pool)

	// Create server with 2 icons
	lightTheme := testThemeLight
	darkTheme := testThemeDark
	mimeType := testMimeTypePNG

	server := createTestServer("test.org/server", "1.0.0")
	server.Icons = []model.Icon{
		{
			Src:      "https://example.com/icon-light.png",
			MimeType: &mimeType,
			Theme:    &lightTheme,
		},
		{
			Src:      "https://example.com/icon-dark.png",
			MimeType: &mimeType,
			Theme:    &darkTheme,
		},
	}
	registry := createTestUpstreamRegistry([]upstreamv0.ServerJSON{server})

	// First sync
	err = writer.Store(ctx, "test-registry", registry)
	require.NoError(t, err)

	// Verify server created
	servers, err := queries.ListServers(ctx, sqlc.ListServersParams{Size: 100})
	require.NoError(t, err)
	require.Len(t, servers, 1)

	originalUUID := servers[0].ID

	// Count icons using raw query
	var iconCount int
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM mcp_server_icon WHERE server_id = $1", originalUUID).Scan(&iconCount)
	require.NoError(t, err)
	require.Equal(t, 2, iconCount, "Should have 2 icons after first sync")

	// Second sync with only 1 icon
	serverUpdated := createTestServer("test.org/server", "1.0.0")
	serverUpdated.Icons = []model.Icon{
		{
			Src:      "https://example.com/icon-light.png",
			MimeType: &mimeType,
			Theme:    &lightTheme,
		},
	}
	registryUpdated := createTestUpstreamRegistry([]upstreamv0.ServerJSON{serverUpdated})

	err = writer.Store(ctx, "test-registry", registryUpdated)
	require.NoError(t, err)

	// Verify server UUID is preserved
	serverAfterUpdate, err := queries.GetServerVersion(ctx, sqlc.GetServerVersionParams{
		Name:    "test.org/server",
		Version: "1.0.0",
	})
	require.NoError(t, err)
	assert.Equal(t, originalUUID, serverAfterUpdate.ID, "Server UUID should be preserved")

	// Verify only 1 icon exists
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM mcp_server_icon WHERE server_id = $1", originalUUID).Scan(&iconCount)
	require.NoError(t, err)
	require.Equal(t, 1, iconCount, "Should have 1 icon after second sync")
}

// TestDbSyncWriter_Store_RegistryIsolation verifies that orphan cleanup for one registry
// does NOT affect servers from other registries.
func TestDbSyncWriter_Store_RegistryIsolation(t *testing.T) {
	t.Parallel()

	pool, cleanup := setupTestDB(t)
	defer cleanup()

	// Step 1: Create two test registries
	registryAID := createTestRegistry(t, pool, "registry-A")
	registryBID := createTestRegistry(t, pool, "registry-B")

	writer, err := NewDBSyncWriter(pool)
	require.NoError(t, err)

	ctx := context.Background()
	queries := sqlc.New(pool)

	// Step 2-3: Create UpstreamRegistry for registry-A with 2 servers
	registryA := createTestUpstreamRegistry([]upstreamv0.ServerJSON{
		createTestServer("test.org/server-1", "1.0.0"),
		createTestServer("test.org/server-2", "1.0.0"),
	})

	// Create UpstreamRegistry for registry-B with 2 servers
	registryB := createTestUpstreamRegistry([]upstreamv0.ServerJSON{
		createTestServer("test.org/server-3", "1.0.0"),
		createTestServer("test.org/server-4", "1.0.0"),
	})

	// Step 4-5: Call Store() for both registries
	err = writer.Store(ctx, "registry-A", registryA)
	require.NoError(t, err)

	err = writer.Store(ctx, "registry-B", registryB)
	require.NoError(t, err)

	// Step 6: Query DB and verify all 4 servers exist (2 per registry)
	servers, err := queries.ListServers(ctx, sqlc.ListServersParams{Size: 100})
	require.NoError(t, err)
	require.Len(t, servers, 4, "Should have 4 servers total after initial sync")

	// Count servers per registry
	var countA, countB int
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM mcp_server WHERE reg_id = $1", registryAID).Scan(&countA)
	require.NoError(t, err)
	assert.Equal(t, 2, countA, "Registry A should have 2 servers")

	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM mcp_server WHERE reg_id = $1", registryBID).Scan(&countB)
	require.NoError(t, err)
	assert.Equal(t, 2, countB, "Registry B should have 2 servers")

	// Step 7: Record server UUIDs for all 4 servers
	server1, err := queries.GetServerVersion(ctx, sqlc.GetServerVersionParams{
		Name:    "test.org/server-1",
		Version: "1.0.0",
	})
	require.NoError(t, err)
	uuidServer1 := server1.ID

	server2, err := queries.GetServerVersion(ctx, sqlc.GetServerVersionParams{
		Name:    "test.org/server-2",
		Version: "1.0.0",
	})
	require.NoError(t, err)
	uuidServer2 := server2.ID

	server3, err := queries.GetServerVersion(ctx, sqlc.GetServerVersionParams{
		Name:    "test.org/server-3",
		Version: "1.0.0",
	})
	require.NoError(t, err)
	uuidServer3 := server3.ID

	server4, err := queries.GetServerVersion(ctx, sqlc.GetServerVersionParams{
		Name:    "test.org/server-4",
		Version: "1.0.0",
	})
	require.NoError(t, err)
	uuidServer4 := server4.ID

	// Step 8: Create new UpstreamRegistry for registry-A with only 1 server (server-2 removed)
	registryAUpdated := createTestUpstreamRegistry([]upstreamv0.ServerJSON{
		createTestServer("test.org/server-1", "1.0.0"),
	})

	// Step 9: Call Store() for registry-A with the new data
	err = writer.Store(ctx, "registry-A", registryAUpdated)
	require.NoError(t, err)

	// Step 10: Query DB and verify registry isolation
	// 10a: registry-A: only "server-1" exists (server-2 deleted)
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM mcp_server WHERE reg_id = $1", registryAID).Scan(&countA)
	require.NoError(t, err)
	assert.Equal(t, 1, countA, "Registry A should have 1 server after update")

	// Verify server-1 still exists with same UUID
	server1After, err := queries.GetServerVersion(ctx, sqlc.GetServerVersionParams{
		Name:    "test.org/server-1",
		Version: "1.0.0",
	})
	require.NoError(t, err)
	assert.Equal(t, uuidServer1, server1After.ID, "Server 1 UUID should be preserved")

	// Verify server-2 was deleted
	_, err = queries.GetServerVersion(ctx, sqlc.GetServerVersionParams{
		Name:    "test.org/server-2",
		Version: "1.0.0",
	})
	require.Error(t, err, "Server 2 should have been deleted from registry A")
	_ = uuidServer2 // Server 2 was deleted, UUID no longer in DB

	// 10b: registry-B: both "server-3" and "server-4" still exist (unchanged!)
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM mcp_server WHERE reg_id = $1", registryBID).Scan(&countB)
	require.NoError(t, err)
	assert.Equal(t, 2, countB, "Registry B should still have 2 servers (unaffected by registry A changes)")

	// Verify server-3 still exists with same UUID
	server3After, err := queries.GetServerVersion(ctx, sqlc.GetServerVersionParams{
		Name:    "test.org/server-3",
		Version: "1.0.0",
	})
	require.NoError(t, err)
	assert.Equal(t, uuidServer3, server3After.ID, "Server 3 UUID should be preserved (registry B unaffected)")

	// Verify server-4 still exists with same UUID
	server4After, err := queries.GetServerVersion(ctx, sqlc.GetServerVersionParams{
		Name:    "test.org/server-4",
		Version: "1.0.0",
	})
	require.NoError(t, err)
	assert.Equal(t, uuidServer4, server4After.ID, "Server 4 UUID should be preserved (registry B unaffected)")

	// 10c: Verify total server count is 3
	servers, err = queries.ListServers(ctx, sqlc.ListServersParams{Size: 100})
	require.NoError(t, err)
	require.Len(t, servers, 3, "Should have 3 servers total (1 in registry A, 2 in registry B)")

	// Step 11: Do the reverse - remove a server from registry-B and verify registry-A is unaffected
	registryBUpdated := createTestUpstreamRegistry([]upstreamv0.ServerJSON{
		createTestServer("test.org/server-3", "1.0.0"),
		// server-4 removed
	})

	err = writer.Store(ctx, "registry-B", registryBUpdated)
	require.NoError(t, err)

	// Verify registry-A is unaffected
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM mcp_server WHERE reg_id = $1", registryAID).Scan(&countA)
	require.NoError(t, err)
	assert.Equal(t, 1, countA, "Registry A should still have 1 server (unaffected by registry B changes)")

	// Verify server-1 in registry-A still has same UUID
	server1Final, err := queries.GetServerVersion(ctx, sqlc.GetServerVersionParams{
		Name:    "test.org/server-1",
		Version: "1.0.0",
	})
	require.NoError(t, err)
	assert.Equal(t, uuidServer1, server1Final.ID, "Server 1 UUID should still be preserved")

	// Verify registry-B now has 1 server
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM mcp_server WHERE reg_id = $1", registryBID).Scan(&countB)
	require.NoError(t, err)
	assert.Equal(t, 1, countB, "Registry B should have 1 server after update")

	// Verify server-3 still exists with same UUID
	server3Final, err := queries.GetServerVersion(ctx, sqlc.GetServerVersionParams{
		Name:    "test.org/server-3",
		Version: "1.0.0",
	})
	require.NoError(t, err)
	assert.Equal(t, uuidServer3, server3Final.ID, "Server 3 UUID should be preserved")

	// Verify server-4 was deleted
	_, err = queries.GetServerVersion(ctx, sqlc.GetServerVersionParams{
		Name:    "test.org/server-4",
		Version: "1.0.0",
	})
	require.Error(t, err, "Server 4 should have been deleted from registry B")

	// Final verification: total server count is 2
	servers, err = queries.ListServers(ctx, sqlc.ListServersParams{Size: 100})
	require.NoError(t, err)
	require.Len(t, servers, 2, "Should have 2 servers total (1 in each registry)")
}

// TestDbSyncWriter_Store_ServerWithMultiplePackages tests that a server can have multiple packages.
func TestDbSyncWriter_Store_ServerWithMultiplePackages(t *testing.T) {
	t.Parallel()

	pool, cleanup := setupTestDB(t)
	defer cleanup()

	createTestRegistry(t, pool, "test-registry")

	writer, err := NewDBSyncWriter(pool)
	require.NoError(t, err)

	ctx := context.Background()
	queries := sqlc.New(pool)

	// Create a server with 3 different packages (different registry types and identifiers)
	server := createTestServer("test.org/server", "1.0.0")
	server.Packages = []model.Package{
		{
			RegistryType:    "npm",
			RegistryBaseURL: "https://registry.npmjs.org",
			Identifier:      "@test/npm-package",
			Version:         "1.0.0",
			Transport:       model.Transport{Type: "stdio"},
		},
		{
			RegistryType:    "pypi",
			RegistryBaseURL: "https://pypi.org/simple",
			Identifier:      "test-pypi-package",
			Version:         "2.0.0",
			Transport:       model.Transport{Type: "stdio"},
		},
		{
			RegistryType:    "oci",
			RegistryBaseURL: "",
			Identifier:      "ghcr.io/test/oci-package:v1.0.0",
			Version:         "",
			Transport:       model.Transport{Type: "stdio"},
		},
	}

	registry := createTestUpstreamRegistry([]upstreamv0.ServerJSON{server})

	// First sync
	err = writer.Store(ctx, "test-registry", registry)
	require.NoError(t, err)

	// Verify all 3 packages exist
	servers, err := queries.ListServers(ctx, sqlc.ListServersParams{Size: 100})
	require.NoError(t, err)
	require.Len(t, servers, 1)

	packages, err := queries.ListServerPackages(ctx, []uuid.UUID{servers[0].ID})
	require.NoError(t, err)
	require.Len(t, packages, 3, "Server should have 3 packages")

	// Verify each package
	pkgIdentifiers := make(map[string]bool)
	for _, pkg := range packages {
		pkgIdentifiers[pkg.PkgIdentifier] = true
	}
	assert.True(t, pkgIdentifiers["@test/npm-package"], "NPM package should exist")
	assert.True(t, pkgIdentifiers["test-pypi-package"], "PyPI package should exist")
	assert.True(t, pkgIdentifiers["ghcr.io/test/oci-package:v1.0.0"], "OCI package should exist")
}

// TestDbSyncWriter_Store_MultiplePackagesOrphanedCleanup tests that when packages are added/removed,
// orphaned packages are properly deleted.
func TestDbSyncWriter_Store_MultiplePackagesOrphanedCleanup(t *testing.T) {
	t.Parallel()

	pool, cleanup := setupTestDB(t)
	defer cleanup()

	createTestRegistry(t, pool, "test-registry")

	writer, err := NewDBSyncWriter(pool)
	require.NoError(t, err)

	ctx := context.Background()
	queries := sqlc.New(pool)

	// First sync: server with 3 packages
	server := createTestServer("test.org/server", "1.0.0")
	server.Packages = []model.Package{
		{
			RegistryType:    "npm",
			RegistryBaseURL: "https://registry.npmjs.org",
			Identifier:      "@test/package-1",
			Version:         "1.0.0",
			Transport:       model.Transport{Type: "stdio"},
		},
		{
			RegistryType:    "npm",
			RegistryBaseURL: "https://registry.npmjs.org",
			Identifier:      "@test/package-2",
			Version:         "1.0.0",
			Transport:       model.Transport{Type: "stdio"},
		},
		{
			RegistryType:    "pypi",
			RegistryBaseURL: "https://pypi.org/simple",
			Identifier:      "test-package-3",
			Version:         "1.0.0",
			Transport:       model.Transport{Type: "stdio"},
		},
	}
	registry := createTestUpstreamRegistry([]upstreamv0.ServerJSON{server})

	err = writer.Store(ctx, "test-registry", registry)
	require.NoError(t, err)

	// Verify all 3 packages exist
	servers, err := queries.ListServers(ctx, sqlc.ListServersParams{Size: 100})
	require.NoError(t, err)
	require.Len(t, servers, 1)
	serverID := servers[0].ID

	packages, err := queries.ListServerPackages(ctx, []uuid.UUID{serverID})
	require.NoError(t, err)
	require.Len(t, packages, 3, "Should have 3 packages after first sync")

	// Second sync: Drop package-2, keep package-1 and package-3, add package-4
	serverUpdated := createTestServer("test.org/server", "1.0.0")
	serverUpdated.Packages = []model.Package{
		{
			RegistryType:    "npm",
			RegistryBaseURL: "https://registry.npmjs.org",
			Identifier:      "@test/package-1",
			Version:         "1.0.0",
			Transport:       model.Transport{Type: "stdio"},
		},
		{
			RegistryType:    "pypi",
			RegistryBaseURL: "https://pypi.org/simple",
			Identifier:      "test-package-3",
			Version:         "1.0.0",
			Transport:       model.Transport{Type: "stdio"},
		},
		{
			RegistryType:    "npm",
			RegistryBaseURL: "https://registry.npmjs.org",
			Identifier:      "@test/package-4",
			Version:         "1.0.0",
			Transport:       model.Transport{Type: "stdio"},
		},
	}
	registryUpdated := createTestUpstreamRegistry([]upstreamv0.ServerJSON{serverUpdated})

	err = writer.Store(ctx, "test-registry", registryUpdated)
	require.NoError(t, err)

	// Verify server UUID is preserved
	serverAfterUpdate, err := queries.GetServerVersion(ctx, sqlc.GetServerVersionParams{
		Name:    "test.org/server",
		Version: "1.0.0",
	})
	require.NoError(t, err)
	assert.Equal(t, serverID, serverAfterUpdate.ID, "Server UUID should be preserved")

	// Verify only 3 packages exist (package-1, package-3, package-4)
	packages, err = queries.ListServerPackages(ctx, []uuid.UUID{serverID})
	require.NoError(t, err)
	require.Len(t, packages, 3, "Should have 3 packages after second sync")

	// Verify package identifiers
	pkgIdentifiers := make(map[string]bool)
	for _, pkg := range packages {
		pkgIdentifiers[pkg.PkgIdentifier] = true
	}
	assert.True(t, pkgIdentifiers["@test/package-1"], "Package-1 should exist")
	assert.False(t, pkgIdentifiers["@test/package-2"], "Package-2 should have been deleted")
	assert.True(t, pkgIdentifiers["test-package-3"], "Package-3 should exist")
	assert.True(t, pkgIdentifiers["@test/package-4"], "Package-4 should exist")

	// Third sync: Remove all packages
	serverNoPackages := createTestServer("test.org/server", "1.0.0")
	serverNoPackages.Packages = nil
	registryNoPackages := createTestUpstreamRegistry([]upstreamv0.ServerJSON{serverNoPackages})

	err = writer.Store(ctx, "test-registry", registryNoPackages)
	require.NoError(t, err)

	// Verify no packages exist
	packages, err = queries.ListServerPackages(ctx, []uuid.UUID{serverID})
	require.NoError(t, err)
	require.Len(t, packages, 0, "Should have 0 packages after third sync")
}

// TestDbSyncWriter_Store_PackageUpdate tests that when a package is updated (same identifier but different attributes),
// it is properly updated in place.
func TestDbSyncWriter_Store_PackageUpdate(t *testing.T) {
	t.Parallel()

	pool, cleanup := setupTestDB(t)
	defer cleanup()

	createTestRegistry(t, pool, "test-registry")

	writer, err := NewDBSyncWriter(pool)
	require.NoError(t, err)

	ctx := context.Background()
	queries := sqlc.New(pool)

	// First sync: server with 1 package
	server := createTestServer("test.org/server", "1.0.0")
	server.Packages = []model.Package{
		{
			RegistryType:    "npm",
			RegistryBaseURL: "https://registry.npmjs.org",
			Identifier:      "@test/my-package",
			Version:         "1.0.0",
			RunTimeHint:     "npx",
			Transport:       model.Transport{Type: "stdio"},
		},
	}
	registry := createTestUpstreamRegistry([]upstreamv0.ServerJSON{server})

	err = writer.Store(ctx, "test-registry", registry)
	require.NoError(t, err)

	// Verify package exists with version 1.0.0
	servers, err := queries.ListServers(ctx, sqlc.ListServersParams{Size: 100})
	require.NoError(t, err)
	require.Len(t, servers, 1)
	serverID := servers[0].ID

	packages, err := queries.ListServerPackages(ctx, []uuid.UUID{serverID})
	require.NoError(t, err)
	require.Len(t, packages, 1)
	assert.Equal(t, "@test/my-package", packages[0].PkgIdentifier)
	assert.Equal(t, "1.0.0", packages[0].PkgVersion)
	assert.Equal(t, "npx", *packages[0].RuntimeHint)

	// Second sync: Update the package version and runtime hint
	serverUpdated := createTestServer("test.org/server", "1.0.0")
	serverUpdated.Packages = []model.Package{
		{
			RegistryType:    "npm",
			RegistryBaseURL: "https://registry.npmjs.org",
			Identifier:      "@test/my-package",
			Version:         "2.0.0", // Updated version
			RunTimeHint:     "node",  // Updated hint
			Transport:       model.Transport{Type: "stdio"},
		},
	}
	registryUpdated := createTestUpstreamRegistry([]upstreamv0.ServerJSON{serverUpdated})

	err = writer.Store(ctx, "test-registry", registryUpdated)
	require.NoError(t, err)

	// Verify package was updated in place
	packages, err = queries.ListServerPackages(ctx, []uuid.UUID{serverID})
	require.NoError(t, err)
	require.Len(t, packages, 1, "Should still have 1 package")
	assert.Equal(t, "@test/my-package", packages[0].PkgIdentifier)
	assert.Equal(t, "2.0.0", packages[0].PkgVersion, "Package version should be updated")
	assert.Equal(t, "node", *packages[0].RuntimeHint, "Runtime hint should be updated")
}

// TestDbSyncWriter_Store_ComplexSyncScenario tests a realistic scenario with multiple changes at once.
func TestDbSyncWriter_Store_ComplexSyncScenario(t *testing.T) {
	t.Parallel()

	pool, cleanup := setupTestDB(t)
	defer cleanup()

	createTestRegistry(t, pool, "test-registry")

	writer, err := NewDBSyncWriter(pool)
	require.NoError(t, err)

	ctx := context.Background()
	queries := sqlc.New(pool)

	// First sync: 3 servers (A v1.0, B v1.0, C v1.0) each with packages/remotes
	serverA := createTestServer("test.org/server-a", "1.0.0")
	serverA.Description = "Server A original"
	serverA.Packages = []model.Package{
		{
			RegistryType:    "npm",
			RegistryBaseURL: "https://registry.npmjs.org",
			Identifier:      "@test/a-package",
			Version:         "1.0.0",
			Transport:       model.Transport{Type: "stdio"},
		},
	}
	serverA.Remotes = []model.Transport{
		{
			Type: "sse",
			URL:  "https://example.com/a-sse",
		},
	}

	serverB := createTestServer("test.org/server-b", "1.0.0")
	serverB.Description = "Server B (will be removed)"
	serverB.Packages = []model.Package{
		{
			RegistryType:    "npm",
			RegistryBaseURL: "https://registry.npmjs.org",
			Identifier:      "@test/b-package",
			Version:         "1.0.0",
			Transport:       model.Transport{Type: "stdio"},
		},
	}

	serverC := createTestServer("test.org/server-c", "1.0.0")
	serverC.Description = "Server C unchanged"

	registry := createTestUpstreamRegistry([]upstreamv0.ServerJSON{serverA, serverB, serverC})

	err = writer.Store(ctx, "test-registry", registry)
	require.NoError(t, err)

	// Record all UUIDs
	serverAV1, err := queries.GetServerVersion(ctx, sqlc.GetServerVersionParams{
		Name:    "test.org/server-a",
		Version: "1.0.0",
	})
	require.NoError(t, err)
	uuidA := serverAV1.ID

	serverBV1, err := queries.GetServerVersion(ctx, sqlc.GetServerVersionParams{
		Name:    "test.org/server-b",
		Version: "1.0.0",
	})
	require.NoError(t, err)
	_ = serverBV1.ID // Server B will be deleted

	serverCV1, err := queries.GetServerVersion(ctx, sqlc.GetServerVersionParams{
		Name:    "test.org/server-c",
		Version: "1.0.0",
	})
	require.NoError(t, err)
	uuidC := serverCV1.ID

	// Second sync:
	// - Server A v1.0: updated description
	// - Server B v1.0: removed
	// - Server C v1.0: no changes
	// - Server D v1.0: new server added
	serverAUpdated := createTestServer("test.org/server-a", "1.0.0")
	serverAUpdated.Description = "Server A updated description"
	serverAUpdated.Packages = []model.Package{
		{
			RegistryType:    "npm",
			RegistryBaseURL: "https://registry.npmjs.org",
			Identifier:      "@test/a-package",
			Version:         "1.0.0",
			Transport:       model.Transport{Type: "stdio"},
		},
	}
	serverAUpdated.Remotes = []model.Transport{
		{
			Type: "sse",
			URL:  "https://example.com/a-sse",
		},
	}

	serverCUnchanged := createTestServer("test.org/server-c", "1.0.0")
	serverCUnchanged.Description = "Server C unchanged"

	serverD := createTestServer("test.org/server-d", "1.0.0")
	serverD.Description = "Server D new"

	registryUpdated := createTestUpstreamRegistry([]upstreamv0.ServerJSON{serverAUpdated, serverCUnchanged, serverD})

	err = writer.Store(ctx, "test-registry", registryUpdated)
	require.NoError(t, err)

	// Verify results
	// Server A: same UUID, updated description
	serverAV2, err := queries.GetServerVersion(ctx, sqlc.GetServerVersionParams{
		Name:    "test.org/server-a",
		Version: "1.0.0",
	})
	require.NoError(t, err)
	assert.Equal(t, uuidA, serverAV2.ID, "Server A UUID should be preserved")
	require.NotNil(t, serverAV2.Description)
	assert.Equal(t, "Server A updated description", *serverAV2.Description)

	// Verify Server A still has its package and remote
	packagesA, err := queries.ListServerPackages(ctx, []uuid.UUID{uuidA})
	require.NoError(t, err)
	require.Len(t, packagesA, 1, "Server A should still have 1 package")

	remotesA, err := queries.ListServerRemotes(ctx, []uuid.UUID{uuidA})
	require.NoError(t, err)
	require.Len(t, remotesA, 1, "Server A should still have 1 remote")

	// Server B: deleted
	_, err = queries.GetServerVersion(ctx, sqlc.GetServerVersionParams{
		Name:    "test.org/server-b",
		Version: "1.0.0",
	})
	require.Error(t, err, "Server B should have been deleted")

	// Server C: same UUID, no changes
	serverCV2, err := queries.GetServerVersion(ctx, sqlc.GetServerVersionParams{
		Name:    "test.org/server-c",
		Version: "1.0.0",
	})
	require.NoError(t, err)
	assert.Equal(t, uuidC, serverCV2.ID, "Server C UUID should be preserved")
	require.NotNil(t, serverCV2.Description)
	assert.Equal(t, "Server C unchanged", *serverCV2.Description)

	// Server D: new UUID, inserted
	serverDV1, err := queries.GetServerVersion(ctx, sqlc.GetServerVersionParams{
		Name:    "test.org/server-d",
		Version: "1.0.0",
	})
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, serverDV1.ID, "Server D should have a valid UUID")
	require.NotNil(t, serverDV1.Description)
	assert.Equal(t, "Server D new", *serverDV1.Description)

	// Verify total server count
	servers, err := queries.ListServers(ctx, sqlc.ListServersParams{Size: 100})
	require.NoError(t, err)
	require.Len(t, servers, 3, "Should have 3 servers (A, C, D)")
}
