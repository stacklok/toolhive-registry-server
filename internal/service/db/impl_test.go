package database

import (
	"context"
	"testing"
	"time"

	"github.com/aws/smithy-go/ptr"
	"github.com/jackc/pgx/v5/pgxpool"
	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/database/dbtest"
	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/db/sqlc"
	"github.com/stacklok/toolhive-registry-server/internal/service"
)

// setupTestService creates a test database service with a migrated database
func setupTestService(t *testing.T) (*dbService, func()) {
	t.Helper()

	ctx := context.Background()
	db, cleanupFunc := dbtest.SetupTestDB(t)
	t.Cleanup(cleanupFunc)

	// Get connection string from the db connection
	connStr := db.Config().ConnString()

	// Create a pgxpool from the connection string
	pool, err := pgxpool.New(ctx, connStr)
	require.NoError(t, err)

	serviceCleanup := func() {
		pool.Close()
		cleanupFunc()
	}

	svc := &dbService{
		pool:        pool,
		maxMetaSize: config.DefaultMaxMetaSize,
	}
	withLibraryPublisher(t, svc)

	return svc, serviceCleanup
}

func TestCheckReadiness(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		setupFunc    func(*testing.T, *dbService, func())
		validateFunc func(*testing.T, error)
	}{
		{
			name: "success when database is available",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *dbService, _ func()) {
				// Service is already set up, no additional setup needed
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, err error) {
				require.NoError(t, err)
			},
		},
		{
			name: "failure when pool is closed",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, svc *dbService, _ func()) {
				// Close the pool before checking readiness
				svc.pool.Close()
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, err error) {
				require.Error(t, err)
				require.Contains(t, err.Error(), "failed to ping database")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc, cleanup := setupTestService(t)
			defer cleanup()

			tt.setupFunc(t, svc, cleanup)

			ctx := context.Background()
			err := svc.CheckReadiness(ctx)

			tt.validateFunc(t, err)
		})
	}
}

func TestWithConnectionPool(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		setupFunc    func(*testing.T) *pgxpool.Pool
		validateFunc func(*testing.T, error, *options)
	}{
		{
			name: "success with valid pool",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T) *pgxpool.Pool {
				ctx := context.Background()
				db, cleanupFunc := dbtest.SetupTestDB(t)
				t.Cleanup(cleanupFunc)

				connStr := db.Config().ConnString()
				pool, err := pgxpool.New(ctx, connStr)
				require.NoError(t, err)
				t.Cleanup(func() { pool.Close() })

				return pool
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, err error, o *options) {
				require.NoError(t, err)
				require.NotNil(t, o.pool)
			},
		},
		{
			name: "failure with nil pool",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T) *pgxpool.Pool {
				return nil
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, err error, _ *options) {
				require.Error(t, err)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pool := tt.setupFunc(t)
			opt := WithConnectionPool(pool)
			o := &options{}
			err := opt(o)

			tt.validateFunc(t, err, o)
		})
	}
}

func TestNew(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		setupFunc    func(*testing.T) []Option
		validateFunc func(*testing.T, service.RegistryService, error)
	}{
		{
			name: "success with connection pool",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T) []Option {
				ctx := context.Background()
				db, cleanupFunc := dbtest.SetupTestDB(t)
				t.Cleanup(cleanupFunc)

				connStr := db.Config().ConnString()
				pool, err := pgxpool.New(ctx, connStr)
				require.NoError(t, err)
				t.Cleanup(func() { pool.Close() })

				return []Option{WithConnectionPool(pool)}
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, svc service.RegistryService, err error) {
				require.NoError(t, err)
				require.NotNil(t, svc)
			},
		},
		{
			name: "success with connection string",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T) []Option {
				ctx := context.Background()
				db, cleanupFunc := dbtest.SetupTestDB(t)
				t.Cleanup(cleanupFunc)

				connStr := db.Config().ConnString()
				pool, err := pgxpool.New(ctx, connStr)
				require.NoError(t, err)
				t.Cleanup(func() { pool.Close() })

				return []Option{WithConnectionPool(pool)}
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, svc service.RegistryService, err error) {
				require.NoError(t, err)
				require.NotNil(t, svc)
			},
		},
		{
			name: "failure with invalid connection string",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T) []Option {
				return []Option{WithConnectionPool(nil)}
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, svc service.RegistryService, err error) {
				require.Error(t, err)
				require.Nil(t, svc)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opts := tt.setupFunc(t)
			svc, err := New(opts...)

			tt.validateFunc(t, svc, err)
		})
	}
}

func TestPublishServerVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		setupFunc    func(*testing.T, *pgxpool.Pool)
		serverData   *upstreamv0.ServerJSON
		validateFunc func(*testing.T, *upstreamv0.ServerJSON, error)
	}{
		{
			name: "success - publish new server version",
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				t.Helper()
				ctx := context.Background()
				queries := sqlc.New(pool)

				// Create a MANAGED source
				_, err := queries.UpsertSource(ctx, sqlc.UpsertSourceParams{
					Name:         "test-registry",
					CreationType: sqlc.CreationTypeCONFIG,
					SourceType:   "managed",
					Syncable:     false,
				})
				require.NoError(t, err)
			},
			serverData: &upstreamv0.ServerJSON{
				Name:        "com.example/test-server",
				Version:     "1.0.0",
				Description: "Test server description",
				Title:       "Test Server",
			},
			validateFunc: func(t *testing.T, result *upstreamv0.ServerJSON, err error) {
				t.Helper()
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Equal(t, "com.example/test-server", result.Name)
				require.Equal(t, "1.0.0", result.Version)
				require.Equal(t, "Test server description", result.Description)
				require.Equal(t, "Test Server", result.Title)
			},
		},
		{
			name: "success - publish with metadata and repository",
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				t.Helper()
				ctx := context.Background()
				queries := sqlc.New(pool)

				_, err := queries.UpsertSource(ctx, sqlc.UpsertSourceParams{
					Name:         "test-registry-meta",
					CreationType: sqlc.CreationTypeCONFIG,
					SourceType:   "managed",
					Syncable:     false,
				})
				require.NoError(t, err)
			},
			serverData: &upstreamv0.ServerJSON{
				Name:        "com.test/server-with-meta",
				Version:     "2.0.0",
				Description: "Server with metadata",
				Title:       "Meta Server",
				Repository: &model.Repository{
					URL:       "https://github.com/example/server",
					Source:    "github",
					ID:        "example/server",
					Subfolder: "src",
				},
				Meta: &upstreamv0.ServerMeta{
					PublisherProvided: map[string]any{
						"custom_field": "custom_value",
					},
				},
			},
			validateFunc: func(t *testing.T, result *upstreamv0.ServerJSON, err error) {
				t.Helper()
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Equal(t, "com.test/server-with-meta", result.Name)
				require.Equal(t, "2.0.0", result.Version)
				require.NotNil(t, result.Repository)
				require.Equal(t, "https://github.com/example/server", result.Repository.URL)
			},
		},
		{
			name: "success - publish with packages and remotes",
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				t.Helper()
				ctx := context.Background()
				queries := sqlc.New(pool)

				_, err := queries.UpsertSource(ctx, sqlc.UpsertSourceParams{
					Name:         "test-registry-full",
					CreationType: sqlc.CreationTypeCONFIG,
					SourceType:   "managed",
					Syncable:     false,
				})
				require.NoError(t, err)
			},
			serverData: &upstreamv0.ServerJSON{
				Name:        "org.example/server-with-packages-remotes",
				Version:     "3.0.0",
				Description: "Server with packages and remotes",
				Title:       "Full Server",
				Packages: []model.Package{
					{
						RegistryType:    "npm",
						RegistryBaseURL: "https://registry.npmjs.org",
						Identifier:      "@test/package",
						Version:         "1.0.0",
						RunTimeHint:     "npx",
						FileSHA256:      "abc1230000000000000000000000000000000000000000000000000000000000",
						Transport: model.Transport{
							Type: "stdio",
							URL:  "https://example.com/transport",
						},
						RuntimeArguments:     []model.Argument{{Type: model.ArgumentTypeNamed, Name: "--yes"}},
						PackageArguments:     []model.Argument{{Type: model.ArgumentTypeNamed, Name: "--verbose"}},
						EnvironmentVariables: []model.KeyValueInput{{Name: "NODE_ENV"}},
					},
				},
				Remotes: []model.Transport{
					{
						Type:    "sse",
						URL:     "https://example.com/sse",
						Headers: []model.KeyValueInput{{Name: "Authorization"}},
					},
				},
			},
			validateFunc: func(t *testing.T, result *upstreamv0.ServerJSON, err error) {
				t.Helper()
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Equal(t, "org.example/server-with-packages-remotes", result.Name)
				require.Equal(t, "3.0.0", result.Version)
				require.Len(t, result.Packages, 1)
				require.Equal(t, "npm", result.Packages[0].RegistryType)
				require.Equal(t, "@test/package", result.Packages[0].Identifier)
				require.Len(t, result.Remotes, 1)
				require.Equal(t, "sse", result.Remotes[0].Type)
				require.Equal(t, "https://example.com/sse", result.Remotes[0].URL)
			},
		},
		{
			name: "failure - no managed source",
			setupFunc: func(t *testing.T, _ *pgxpool.Pool) {
				t.Helper()
			},
			serverData: &upstreamv0.ServerJSON{
				Name:        "com.example/test-server",
				Version:     "1.0.0",
				Description: "Test",
			},
			validateFunc: func(t *testing.T, result *upstreamv0.ServerJSON, err error) {
				t.Helper()
				require.Error(t, err)
				require.Nil(t, result)
				require.ErrorIs(t, err, service.ErrNoManagedSource)
			},
		},
		{
			name: "failure - no managed source with only remote sources",
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				t.Helper()
				ctx := context.Background()
				queries := sqlc.New(pool)

				// Create a REMOTE (non-managed) source — getManagedSource should still fail
				_, err := queries.UpsertSource(ctx, sqlc.UpsertSourceParams{
					Name:         "remote-registry",
					CreationType: sqlc.CreationTypeCONFIG,
					SourceType:   "git",
					Syncable:     true,
				})
				require.NoError(t, err)
			},
			serverData: &upstreamv0.ServerJSON{
				Name:        "com.example/test-server",
				Version:     "1.0.0",
				Description: "Test",
			},
			validateFunc: func(t *testing.T, result *upstreamv0.ServerJSON, err error) {
				t.Helper()
				require.Error(t, err)
				require.Nil(t, result)
				require.ErrorIs(t, err, service.ErrNoManagedSource)
			},
		},
		{
			name: "failure - version already exists",
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				t.Helper()
				ctx := context.Background()
				queries := sqlc.New(pool)

				// Create a MANAGED source
				regID, err := queries.UpsertSource(ctx, sqlc.UpsertSourceParams{
					Name:         "test-registry-dup",
					CreationType: sqlc.CreationTypeCONFIG,
					SourceType:   "managed",
					Syncable:     false,
				})
				require.NoError(t, err)

				now := time.Now()
				entryID, err := queries.InsertRegistryEntry(ctx, sqlc.InsertRegistryEntryParams{
					SourceID:  regID,
					EntryType: sqlc.EntryTypeMCP,
					Name:      "com.example/existing-server",
					CreatedAt: &now,
					UpdatedAt: &now,
				})
				require.NoError(t, err)

				versionID, err := queries.InsertEntryVersion(ctx, sqlc.InsertEntryVersionParams{
					EntryID:     entryID,
					Name:        "com.example/existing-server",
					Version:     "1.0.0",
					Description: ptr.String("Existing"),
					CreatedAt:   &now,
					UpdatedAt:   &now,
				})
				require.NoError(t, err)

				_, err = queries.InsertServerVersion(ctx, sqlc.InsertServerVersionParams{
					VersionID: versionID,
				})
				require.NoError(t, err)
			},
			serverData: &upstreamv0.ServerJSON{
				Name:        "com.example/existing-server",
				Version:     "1.0.0",
				Description: "Duplicate",
			},
			validateFunc: func(t *testing.T, result *upstreamv0.ServerJSON, err error) {
				t.Helper()
				require.Error(t, err)
				require.Nil(t, result)
				require.ErrorIs(t, err, service.ErrVersionAlreadyExists)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc, cleanup := setupTestService(t)
			defer cleanup()

			// Setup registry if needed
			if tt.setupFunc != nil {
				tt.setupFunc(t, svc.pool)
			}

			// Call PublishServerVersion
			result, err := svc.PublishServerVersion(
				context.Background(),
				service.WithServerData(tt.serverData),
			)

			tt.validateFunc(t, result, err)
		})
	}
}

func TestDeleteSource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		setupFunc    func(*testing.T, *pgxpool.Pool) string
		validateFunc func(*testing.T, *dbService, string, error)
	}{
		{
			name: "happy path - API source not linked to any registry",
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) string {
				t.Helper()
				ctx := context.Background()
				queries := sqlc.New(pool)

				now := time.Now()
				_, err := queries.InsertSource(ctx, sqlc.InsertSourceParams{
					Name:         "delete-src-happy",
					CreationType: sqlc.CreationTypeAPI,
					SourceType:   "managed",
					SourceConfig: []byte(`{}`),
					Syncable:     false,
					CreatedAt:    &now,
					UpdatedAt:    &now,
				})
				require.NoError(t, err)

				return "delete-src-happy"
			},
			validateFunc: func(t *testing.T, svc *dbService, name string, err error) {
				t.Helper()
				require.NoError(t, err)

				// Verify the source is gone
				_, getErr := svc.GetSourceByName(context.Background(), name)
				require.Error(t, getErr)
				require.ErrorIs(t, getErr, service.ErrSourceNotFound)
			},
		},
		{
			name: "failure - source not found",
			setupFunc: func(_ *testing.T, _ *pgxpool.Pool) string {
				return "delete-src-nonexistent"
			},
			validateFunc: func(t *testing.T, _ *dbService, _ string, err error) {
				t.Helper()
				require.Error(t, err)
				require.ErrorIs(t, err, service.ErrSourceNotFound)
			},
		},
		{
			name: "failure - cannot delete CONFIG source",
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) string {
				t.Helper()
				ctx := context.Background()
				queries := sqlc.New(pool)

				_, err := queries.UpsertSource(ctx, sqlc.UpsertSourceParams{
					Name:         "delete-src-config",
					CreationType: sqlc.CreationTypeCONFIG,
					SourceType:   "git",
					Syncable:     true,
				})
				require.NoError(t, err)

				return "delete-src-config"
			},
			validateFunc: func(t *testing.T, _ *dbService, _ string, err error) {
				t.Helper()
				require.Error(t, err)
				require.ErrorIs(t, err, service.ErrConfigSource)
			},
		},
		{
			name: "failure - source in use by a registry",
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) string {
				t.Helper()
				ctx := context.Background()
				queries := sqlc.New(pool)

				now := time.Now()

				// Create an API source
				src, err := queries.InsertSource(ctx, sqlc.InsertSourceParams{
					Name:         "delete-src-in-use",
					CreationType: sqlc.CreationTypeAPI,
					SourceType:   "managed",

					SourceConfig: []byte(`{}`),
					Syncable:     false,
					CreatedAt:    &now,
					UpdatedAt:    &now,
				})
				require.NoError(t, err)

				// Create a registry that references this source
				reg, err := queries.UpsertRegistry(ctx, sqlc.UpsertRegistryParams{
					Name:         "delete-src-in-use-registry",
					CreationType: sqlc.CreationTypeAPI,
					CreatedAt:    &now,
					UpdatedAt:    &now,
				})
				require.NoError(t, err)

				err = queries.LinkRegistrySource(ctx, sqlc.LinkRegistrySourceParams{
					RegistryID: reg.ID,
					SourceID:   src.ID,
					Position:   0,
				})
				require.NoError(t, err)

				return "delete-src-in-use"
			},
			validateFunc: func(t *testing.T, svc *dbService, name string, err error) {
				t.Helper()
				require.Error(t, err)
				require.ErrorIs(t, err, service.ErrSourceInUse)

				// Verify the source still exists
				src, getErr := svc.GetSourceByName(context.Background(), name)
				require.NoError(t, getErr)
				require.Equal(t, name, src.Name)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc, cleanup := setupTestService(t)
			defer cleanup()

			sourceName := tt.setupFunc(t, svc.pool)

			err := svc.DeleteSource(context.Background(), sourceName)

			tt.validateFunc(t, svc, sourceName, err)
		})
	}
}

func TestUpdateSource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		setupFunc    func(*testing.T, *pgxpool.Pool) string // returns registry name
		updateReq    *service.SourceCreateRequest
		validateFunc func(*testing.T, *service.SourceInfo, error)
	}{
		{
			name: "success - update existing API registry",
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) string {
				t.Helper()
				ctx := context.Background()
				queries := sqlc.New(pool)

				sourceType := string(config.SourceTypeGit)

				now := time.Now()
				sourceConfig := []byte(`{"repository":"https://github.com/example/repo.git","branch":"main"}`)

				_, err := queries.InsertSource(ctx, sqlc.InsertSourceParams{
					Name:         "update-test-registry",
					CreationType: sqlc.CreationTypeAPI,
					SourceType:   sourceType,

					SourceConfig: sourceConfig,
					Syncable:     true,
					CreatedAt:    &now,
					UpdatedAt:    &now,
				})
				require.NoError(t, err)

				return "update-test-registry"
			},
			updateReq: &service.SourceCreateRequest{

				Git: &config.GitConfig{
					Repository: "https://github.com/example/updated-repo.git",
					Branch:     "develop",
				},
				SyncPolicy: &config.SyncPolicyConfig{
					Interval: "30m",
				},
			},
			validateFunc: func(t *testing.T, result *service.SourceInfo, err error) {
				t.Helper()
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Equal(t, "update-test-registry", result.Name)

				require.Equal(t, config.SourceTypeGit, result.SourceType)
				// Verify the source config was updated
				gitConfig, ok := result.SourceConfig.(*config.GitConfig)
				require.True(t, ok)
				require.Equal(t, "https://github.com/example/updated-repo.git", gitConfig.Repository)
				require.Equal(t, "develop", gitConfig.Branch)
			},
		},
		{
			name: "success - update with same source type",
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) string {
				t.Helper()
				ctx := context.Background()
				queries := sqlc.New(pool)

				sourceType := string(config.SourceTypeAPI)

				now := time.Now()
				sourceConfig := []byte(`{"endpoint":"https://api.example.com/v1"}`)

				_, err := queries.InsertSource(ctx, sqlc.InsertSourceParams{
					Name:         "same-type-test-registry",
					CreationType: sqlc.CreationTypeAPI,
					SourceType:   sourceType,

					SourceConfig: sourceConfig,
					Syncable:     true,
					CreatedAt:    &now,
					UpdatedAt:    &now,
				})
				require.NoError(t, err)

				return "same-type-test-registry"
			},
			updateReq: &service.SourceCreateRequest{

				API: &config.APIConfig{
					Endpoint: "https://api.example.com/v2",
				},
				SyncPolicy: &config.SyncPolicyConfig{
					Interval: "1h",
				},
			},
			validateFunc: func(t *testing.T, result *service.SourceInfo, err error) {
				t.Helper()
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Equal(t, "same-type-test-registry", result.Name)
				require.Equal(t, config.SourceTypeAPI, result.SourceType)
				// Verify the endpoint was updated
				apiConfig, ok := result.SourceConfig.(*config.APIConfig)
				require.True(t, ok)
				require.Equal(t, "https://api.example.com/v2", apiConfig.Endpoint)
			},
		},
		{
			name: "failure - registry not found",
			setupFunc: func(_ *testing.T, _ *pgxpool.Pool) string {
				// No setup needed - registry doesn't exist
				return "nonexistent-registry"
			},
			updateReq: &service.SourceCreateRequest{

				Git: &config.GitConfig{
					Repository: "https://github.com/example/repo.git",
				},
				SyncPolicy: &config.SyncPolicyConfig{
					Interval: "1h",
				},
			},
			validateFunc: func(t *testing.T, result *service.SourceInfo, err error) {
				t.Helper()
				require.Error(t, err)
				require.Nil(t, result)
				require.ErrorIs(t, err, service.ErrSourceNotFound)
			},
		},
		{
			name: "failure - cannot modify CONFIG registry",
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) string {
				t.Helper()
				ctx := context.Background()
				queries := sqlc.New(pool)

				// Create a CONFIG source (created via config file, not API)
				_, err := queries.UpsertSource(ctx, sqlc.UpsertSourceParams{
					Name:         "config-registry-test",
					CreationType: sqlc.CreationTypeCONFIG,
					SourceType:   "git",
					Syncable:     true,
				})
				require.NoError(t, err)

				return "config-registry-test"
			},
			updateReq: &service.SourceCreateRequest{

				Git: &config.GitConfig{
					Repository: "https://github.com/example/repo.git",
				},
				SyncPolicy: &config.SyncPolicyConfig{
					Interval: "1h",
				},
			},
			validateFunc: func(t *testing.T, result *service.SourceInfo, err error) {
				t.Helper()
				require.Error(t, err)
				require.Nil(t, result)
				require.ErrorIs(t, err, service.ErrConfigSource)
			},
		},
		{
			name: "failure - source type change not allowed",
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) string {
				t.Helper()
				ctx := context.Background()
				queries := sqlc.New(pool)

				sourceType := string(config.SourceTypeGit)

				now := time.Now()
				sourceConfig := []byte(`{"repository":"https://github.com/example/repo.git","branch":"main"}`)

				_, err := queries.InsertSource(ctx, sqlc.InsertSourceParams{
					Name:         "source-type-change-test",
					CreationType: sqlc.CreationTypeAPI,
					SourceType:   sourceType,

					SourceConfig: sourceConfig,
					Syncable:     true,
					CreatedAt:    &now,
					UpdatedAt:    &now,
				})
				require.NoError(t, err)

				return "source-type-change-test"
			},
			updateReq: &service.SourceCreateRequest{

				File: &config.FileConfig{
					Path: "/data/registry.json",
				},
				SyncPolicy: &config.SyncPolicyConfig{
					Interval: "1h",
				},
			},
			validateFunc: func(t *testing.T, result *service.SourceInfo, err error) {
				t.Helper()
				require.Error(t, err)
				require.Nil(t, result)
				require.ErrorIs(t, err, service.ErrSourceTypeChangeNotAllowed)
			},
		},
		{
			name: "failure - invalid configuration - missing required fields",
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) string {
				t.Helper()
				ctx := context.Background()
				queries := sqlc.New(pool)

				sourceType := string(config.SourceTypeGit)

				now := time.Now()
				sourceConfig := []byte(`{"repository":"https://github.com/example/repo.git"}`)

				_, err := queries.InsertSource(ctx, sqlc.InsertSourceParams{
					Name:         "invalid-config-test",
					CreationType: sqlc.CreationTypeAPI,
					SourceType:   sourceType,

					SourceConfig: sourceConfig,
					Syncable:     true,
					CreatedAt:    &now,
					UpdatedAt:    &now,
				})
				require.NoError(t, err)

				return "invalid-config-test"
			},
			updateReq: &service.SourceCreateRequest{

				Git: &config.GitConfig{
					// Missing required Repository field
					Branch: "main",
				},
				SyncPolicy: &config.SyncPolicyConfig{
					Interval: "1h",
				},
			},
			validateFunc: func(t *testing.T, result *service.SourceInfo, err error) {
				t.Helper()
				require.Error(t, err)
				require.Nil(t, result)
				require.ErrorIs(t, err, service.ErrInvalidSourceConfig)
			},
		},
		{
			name: "failure - invalid configuration - no source type specified",
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) string {
				t.Helper()
				ctx := context.Background()
				queries := sqlc.New(pool)

				sourceType := string(config.SourceTypeGit)

				now := time.Now()
				sourceConfig := []byte(`{"repository":"https://github.com/example/repo.git"}`)

				_, err := queries.InsertSource(ctx, sqlc.InsertSourceParams{
					Name:         "no-source-type-test",
					CreationType: sqlc.CreationTypeAPI,
					SourceType:   sourceType,

					SourceConfig: sourceConfig,
					Syncable:     true,
					CreatedAt:    &now,
					UpdatedAt:    &now,
				})
				require.NoError(t, err)

				return "no-source-type-test"
			},
			updateReq: &service.SourceCreateRequest{

				// No source type specified (Git, API, File, Managed, or Kubernetes)
				SyncPolicy: &config.SyncPolicyConfig{
					Interval: "1h",
				},
			},
			validateFunc: func(t *testing.T, result *service.SourceInfo, err error) {
				t.Helper()
				require.Error(t, err)
				require.Nil(t, result)
				require.ErrorIs(t, err, service.ErrInvalidSourceConfig)
			},
		},
		{
			name: "failure - invalid configuration - missing sync policy for synced type",
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) string {
				t.Helper()
				ctx := context.Background()
				queries := sqlc.New(pool)

				sourceType := string(config.SourceTypeAPI)

				now := time.Now()
				sourceConfig := []byte(`{"endpoint":"https://api.example.com/v1"}`)

				_, err := queries.InsertSource(ctx, sqlc.InsertSourceParams{
					Name:         "missing-sync-policy-test",
					CreationType: sqlc.CreationTypeAPI,
					SourceType:   sourceType,

					SourceConfig: sourceConfig,
					Syncable:     true,
					CreatedAt:    &now,
					UpdatedAt:    &now,
				})
				require.NoError(t, err)

				return "missing-sync-policy-test"
			},
			updateReq: &service.SourceCreateRequest{

				API: &config.APIConfig{
					Endpoint: "https://api.example.com/v2",
				},
				// Missing required SyncPolicy for API (synced) type
			},
			validateFunc: func(t *testing.T, result *service.SourceInfo, err error) {
				t.Helper()
				require.Error(t, err)
				require.Nil(t, result)
				require.ErrorIs(t, err, service.ErrInvalidSourceConfig)
			},
		},
		{
			name: "success - update managed registry without sync policy",
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) string {
				t.Helper()
				ctx := context.Background()
				queries := sqlc.New(pool)

				sourceType := string(config.SourceTypeManaged)

				now := time.Now()
				sourceConfig := []byte(`{}`)

				_, err := queries.InsertSource(ctx, sqlc.InsertSourceParams{
					Name:         "managed-registry-update-test",
					CreationType: sqlc.CreationTypeAPI,
					SourceType:   sourceType,

					SourceConfig: sourceConfig,
					Syncable:     false,
					CreatedAt:    &now,
					UpdatedAt:    &now,
				})
				require.NoError(t, err)

				return "managed-registry-update-test"
			},
			updateReq: &service.SourceCreateRequest{

				Managed: &config.ManagedConfig{},
				// No SyncPolicy needed for managed type
			},
			validateFunc: func(t *testing.T, result *service.SourceInfo, err error) {
				t.Helper()
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Equal(t, "managed-registry-update-test", result.Name)
				require.Equal(t, config.SourceTypeManaged, result.SourceType)

			},
		},
		{
			// A source created before source-name DNS-subdomain validation
			// shipped can have a name that fails the rule today. There is no
			// rename endpoint, so UpdateSource must not re-gate the name —
			// doing so would permanently strand this row: every future PUT
			// would 400 on the name before ever reaching the point where an
			// update would otherwise apply.
			name: "success - update source whose stored name predates DNS-subdomain validation",
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) string {
				t.Helper()
				ctx := context.Background()
				queries := sqlc.New(pool)

				sourceType := string(config.SourceTypeGit)

				now := time.Now()
				sourceConfig := []byte(`{"repository":"https://github.com/example/repo.git","branch":"main"}`)

				_, err := queries.InsertSource(ctx, sqlc.InsertSourceParams{
					Name:         "Legacy_Source.Name",
					CreationType: sqlc.CreationTypeAPI,
					SourceType:   sourceType,

					SourceConfig: sourceConfig,
					Syncable:     true,
					CreatedAt:    &now,
					UpdatedAt:    &now,
				})
				require.NoError(t, err)

				return "Legacy_Source.Name"
			},
			updateReq: &service.SourceCreateRequest{

				Git: &config.GitConfig{
					Repository: "https://github.com/example/updated-repo.git",
					Branch:     "develop",
				},
				SyncPolicy: &config.SyncPolicyConfig{
					Interval: "30m",
				},
			},
			validateFunc: func(t *testing.T, result *service.SourceInfo, err error) {
				t.Helper()
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Equal(t, "Legacy_Source.Name", result.Name)

				gitConfig, ok := result.SourceConfig.(*config.GitConfig)
				require.True(t, ok)
				require.Equal(t, "https://github.com/example/updated-repo.git", gitConfig.Repository)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc, cleanup := setupTestService(t)
			defer cleanup()

			// Setup registry if needed
			registryName := tt.setupFunc(t, svc.pool)

			// Call UpdateSource
			result, err := svc.UpdateSource(
				context.Background(),
				registryName,
				tt.updateReq,
			)

			tt.validateFunc(t, result, err)
		})
	}
}

// TestCreateSource_LegacyNameFallsThroughToUpdate covers the create-then-update
// flow that PUT /v1/sources/{name} actually takes: upsertSource calls
// CreateSource first and only reaches UpdateSource when it gets back
// ErrSourceAlreadyExists.
//
// A source whose stored name predates DNS-subdomain validation must therefore
// get ErrSourceAlreadyExists out of CreateSource, not ErrInvalidSourceConfig.
// If the name gate sits above the existence check, the handler sees a 400 and
// the row becomes permanently un-updatable — there is no rename endpoint.
func TestCreateSource_LegacyNameFallsThroughToUpdate(t *testing.T) {
	t.Parallel()

	const legacyName = "Legacy_Source.Name"

	svc, cleanup := setupTestService(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	_, err := sqlc.New(svc.pool).InsertSource(ctx, sqlc.InsertSourceParams{
		Name:         legacyName,
		CreationType: sqlc.CreationTypeAPI,
		SourceType:   string(config.SourceTypeGit),
		SourceConfig: []byte(`{"repository":"https://github.com/example/repo.git","branch":"main"}`),
		Syncable:     true,
		CreatedAt:    &now,
		UpdatedAt:    &now,
	})
	require.NoError(t, err)

	req := &service.SourceCreateRequest{
		Git: &config.GitConfig{
			Repository: "https://github.com/example/updated-repo.git",
			Branch:     "develop",
		},
		SyncPolicy: &config.SyncPolicyConfig{Interval: "30m"},
	}

	_, err = svc.CreateSource(ctx, legacyName, req)
	require.Error(t, err)
	require.ErrorIs(t, err, service.ErrSourceAlreadyExists,
		"CreateSource must surface ErrSourceAlreadyExists so upsertSource falls through to UpdateSource")
	require.NotErrorIs(t, err, service.ErrInvalidSourceConfig,
		"the name gate must not fire before the existence check, or the handler writes 400 instead of updating")

	// The fall-through the handler would then take must succeed.
	result, err := svc.UpdateSource(ctx, legacyName, req)
	require.NoError(t, err)
	require.Equal(t, legacyName, result.Name)

	// A genuinely new source with an invalid name is still rejected.
	_, err = svc.CreateSource(ctx, "Another_Invalid.Name", req)
	require.ErrorIs(t, err, service.ErrInvalidSourceConfig)
	require.Contains(t, err.Error(), "must be a valid DNS subdomain")
}

func TestListSources(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		setupFunc    func(t *testing.T, pool *pgxpool.Pool)
		validateFunc func(t *testing.T, result []service.SourceInfo, err error)
	}{
		{
			name: "empty database",
			setupFunc: func(_ *testing.T, _ *pgxpool.Pool) {
				// No setup needed - database is empty
			},
			validateFunc: func(t *testing.T, result []service.SourceInfo, err error) {
				t.Helper()
				require.NoError(t, err)
				require.Empty(t, result)
			},
		},
		{
			name: "with sources",
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				t.Helper()
				ctx := context.Background()
				queries := sqlc.New(pool)

				now := time.Now()

				_, err := queries.InsertSource(ctx, sqlc.InsertSourceParams{
					Name:         "list-src-alpha",
					CreationType: sqlc.CreationTypeAPI,
					SourceType:   "managed",

					SourceConfig: []byte(`{}`),
					Syncable:     false,
					CreatedAt:    &now,
					UpdatedAt:    &now,
				})
				require.NoError(t, err)

				_, err = queries.InsertSource(ctx, sqlc.InsertSourceParams{
					Name:         "list-src-beta",
					CreationType: sqlc.CreationTypeAPI,
					SourceType:   "kubernetes",

					SourceConfig: []byte(`{}`),
					Syncable:     false,
					CreatedAt:    &now,
					UpdatedAt:    &now,
				})
				require.NoError(t, err)
			},
			validateFunc: func(t *testing.T, result []service.SourceInfo, err error) {
				t.Helper()
				require.NoError(t, err)
				require.Len(t, result, 2)

				// Build a map by name for order-independent assertions
				byName := make(map[string]service.SourceInfo)
				for _, s := range result {
					byName[s.Name] = s
				}

				_, ok := byName["list-src-alpha"]
				require.True(t, ok, "list-src-alpha should be present")

				_, ok = byName["list-src-beta"]
				require.True(t, ok, "list-src-beta should be present")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc, cleanup := setupTestService(t)
			defer cleanup()

			tt.setupFunc(t, svc.pool)

			result, err := svc.ListSources(context.Background())

			tt.validateFunc(t, result, err)
		})
	}
}

func TestListRegistries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		setupFunc    func(t *testing.T, pool *pgxpool.Pool)
		validateFunc func(t *testing.T, result []service.RegistryInfo, err error)
	}{
		{
			name: "empty database",
			setupFunc: func(_ *testing.T, _ *pgxpool.Pool) {
				// No setup needed - database is empty
			},
			validateFunc: func(t *testing.T, result []service.RegistryInfo, err error) {
				t.Helper()
				require.NoError(t, err)
				require.Empty(t, result)
			},
		},
		{
			name: "with registries",
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				t.Helper()
				ctx := context.Background()
				queries := sqlc.New(pool)

				now := time.Now()

				// Create two API sources
				srcA, err := queries.InsertSource(ctx, sqlc.InsertSourceParams{
					Name:         "list-reg-source-a",
					CreationType: sqlc.CreationTypeAPI,
					SourceType:   "managed",

					SourceConfig: []byte(`{}`),
					Syncable:     false,
					CreatedAt:    &now,
					UpdatedAt:    &now,
				})
				require.NoError(t, err)

				srcB, err := queries.InsertSource(ctx, sqlc.InsertSourceParams{
					Name:         "list-reg-source-b",
					CreationType: sqlc.CreationTypeAPI,
					SourceType:   "kubernetes",

					SourceConfig: []byte(`{}`),
					Syncable:     false,
					CreatedAt:    &now,
					UpdatedAt:    &now,
				})
				require.NoError(t, err)

				// Create first registry with source-a
				reg1, err := queries.UpsertRegistry(ctx, sqlc.UpsertRegistryParams{
					Name:         "list-reg-1",
					CreationType: sqlc.CreationTypeAPI,
					CreatedAt:    &now,
					UpdatedAt:    &now,
				})
				require.NoError(t, err)
				err = queries.LinkRegistrySource(ctx, sqlc.LinkRegistrySourceParams{
					RegistryID: reg1.ID,
					SourceID:   srcA.ID,
					Position:   0,
				})
				require.NoError(t, err)

				// Create second registry with source-b then source-a
				reg2, err := queries.UpsertRegistry(ctx, sqlc.UpsertRegistryParams{
					Name:         "list-reg-2",
					CreationType: sqlc.CreationTypeAPI,
					CreatedAt:    &now,
					UpdatedAt:    &now,
				})
				require.NoError(t, err)
				err = queries.LinkRegistrySource(ctx, sqlc.LinkRegistrySourceParams{
					RegistryID: reg2.ID,
					SourceID:   srcB.ID,
					Position:   0,
				})
				require.NoError(t, err)
				err = queries.LinkRegistrySource(ctx, sqlc.LinkRegistrySourceParams{
					RegistryID: reg2.ID,
					SourceID:   srcA.ID,
					Position:   1,
				})
				require.NoError(t, err)
			},
			validateFunc: func(t *testing.T, result []service.RegistryInfo, err error) {
				t.Helper()
				require.NoError(t, err)
				require.Len(t, result, 2)

				// Build a map by name for order-independent assertions
				byName := make(map[string]service.RegistryInfo)
				for _, r := range result {
					byName[r.Name] = r
				}

				reg1, ok := byName["list-reg-1"]
				require.True(t, ok, "list-reg-1 should be present")
				require.Equal(t, []string{"list-reg-source-a"}, reg1.Sources)

				reg2, ok := byName["list-reg-2"]
				require.True(t, ok, "list-reg-2 should be present")
				require.Equal(t, []string{"list-reg-source-b", "list-reg-source-a"}, reg2.Sources)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc, cleanup := setupTestService(t)
			defer cleanup()

			tt.setupFunc(t, svc.pool)

			result, err := svc.ListRegistries(context.Background())

			tt.validateFunc(t, result, err)
		})
	}
}

func TestGetRegistryByName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		setupFunc    func(t *testing.T, pool *pgxpool.Pool) string
		validateFunc func(t *testing.T, result *service.RegistryInfo, err error)
	}{
		{
			name: "found",
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) string {
				t.Helper()
				ctx := context.Background()
				queries := sqlc.New(pool)

				now := time.Now()

				srcA, err := queries.InsertSource(ctx, sqlc.InsertSourceParams{
					Name:         "get-reg-source-a",
					CreationType: sqlc.CreationTypeAPI,
					SourceType:   "managed",

					SourceConfig: []byte(`{}`),
					Syncable:     false,
					CreatedAt:    &now,
					UpdatedAt:    &now,
				})
				require.NoError(t, err)

				srcB, err := queries.InsertSource(ctx, sqlc.InsertSourceParams{
					Name:         "get-reg-source-b",
					CreationType: sqlc.CreationTypeAPI,
					SourceType:   "kubernetes",

					SourceConfig: []byte(`{}`),
					Syncable:     false,
					CreatedAt:    &now,
					UpdatedAt:    &now,
				})
				require.NoError(t, err)

				reg, err := queries.UpsertRegistry(ctx, sqlc.UpsertRegistryParams{
					Name:         "get-reg-test",
					CreationType: sqlc.CreationTypeAPI,
					CreatedAt:    &now,
					UpdatedAt:    &now,
				})
				require.NoError(t, err)

				err = queries.LinkRegistrySource(ctx, sqlc.LinkRegistrySourceParams{
					RegistryID: reg.ID,
					SourceID:   srcA.ID,
					Position:   0,
				})
				require.NoError(t, err)
				err = queries.LinkRegistrySource(ctx, sqlc.LinkRegistrySourceParams{
					RegistryID: reg.ID,
					SourceID:   srcB.ID,
					Position:   1,
				})
				require.NoError(t, err)

				return "get-reg-test"
			},
			validateFunc: func(t *testing.T, result *service.RegistryInfo, err error) {
				t.Helper()
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Equal(t, "get-reg-test", result.Name)
				require.Equal(t, []string{"get-reg-source-a", "get-reg-source-b"}, result.Sources)
				require.False(t, result.CreatedAt.IsZero())
				require.False(t, result.UpdatedAt.IsZero())
			},
		},
		{
			name: "not found",
			setupFunc: func(_ *testing.T, _ *pgxpool.Pool) string {
				return "nonexistent-registry"
			},
			validateFunc: func(t *testing.T, result *service.RegistryInfo, err error) {
				t.Helper()
				require.Error(t, err)
				require.Nil(t, result)
				require.ErrorIs(t, err, service.ErrRegistryNotFound)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc, cleanup := setupTestService(t)
			defer cleanup()

			registryName := tt.setupFunc(t, svc.pool)

			result, err := svc.GetRegistryByName(context.Background(), registryName)

			tt.validateFunc(t, result, err)
		})
	}
}

func TestCreateRegistry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		setupFunc    func(t *testing.T, pool *pgxpool.Pool) string
		createReq    *service.RegistryCreateRequest
		validateFunc func(t *testing.T, result *service.RegistryInfo, err error)
	}{
		{
			name: "happy path",
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) string {
				t.Helper()
				ctx := context.Background()
				queries := sqlc.New(pool)

				now := time.Now()

				_, err := queries.InsertSource(ctx, sqlc.InsertSourceParams{
					Name:         "create-reg-source-a",
					CreationType: sqlc.CreationTypeAPI,
					SourceType:   "managed",

					SourceConfig: []byte(`{}`),
					Syncable:     false,
					CreatedAt:    &now,
					UpdatedAt:    &now,
				})
				require.NoError(t, err)

				_, err = queries.InsertSource(ctx, sqlc.InsertSourceParams{
					Name:         "create-reg-source-b",
					CreationType: sqlc.CreationTypeAPI,
					SourceType:   "kubernetes",

					SourceConfig: []byte(`{}`),
					Syncable:     false,
					CreatedAt:    &now,
					UpdatedAt:    &now,
				})
				require.NoError(t, err)

				return "create-reg-happy"
			},
			createReq: &service.RegistryCreateRequest{
				Sources: []string{"create-reg-source-a", "create-reg-source-b"},
			},
			validateFunc: func(t *testing.T, result *service.RegistryInfo, err error) {
				t.Helper()
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Equal(t, "create-reg-happy", result.Name)
				require.Equal(t, service.CreationTypeAPI, result.CreationType)
				require.Equal(t, []string{"create-reg-source-a", "create-reg-source-b"}, result.Sources)
				require.False(t, result.CreatedAt.IsZero())
				require.False(t, result.UpdatedAt.IsZero())
			},
		},
		{
			name: "already exists",
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) string {
				t.Helper()
				ctx := context.Background()
				queries := sqlc.New(pool)

				now := time.Now()

				src, err := queries.InsertSource(ctx, sqlc.InsertSourceParams{
					Name:         "create-dup-source",
					CreationType: sqlc.CreationTypeAPI,
					SourceType:   "managed",

					SourceConfig: []byte(`{}`),
					Syncable:     false,
					CreatedAt:    &now,
					UpdatedAt:    &now,
				})
				require.NoError(t, err)

				reg, err := queries.UpsertRegistry(ctx, sqlc.UpsertRegistryParams{
					Name:         "create-reg-dup",
					CreationType: sqlc.CreationTypeAPI,
					CreatedAt:    &now,
					UpdatedAt:    &now,
				})
				require.NoError(t, err)

				err = queries.LinkRegistrySource(ctx, sqlc.LinkRegistrySourceParams{
					RegistryID: reg.ID,
					SourceID:   src.ID,
					Position:   0,
				})
				require.NoError(t, err)

				return "create-reg-dup"
			},
			createReq: &service.RegistryCreateRequest{
				Sources: []string{"create-dup-source"},
			},
			validateFunc: func(t *testing.T, result *service.RegistryInfo, err error) {
				t.Helper()
				require.Error(t, err)
				require.Nil(t, result)
				require.ErrorIs(t, err, service.ErrRegistryAlreadyExists)
			},
		},
		{
			name: "invalid source name",
			setupFunc: func(_ *testing.T, _ *pgxpool.Pool) string {
				return "create-reg-bad-source"
			},
			createReq: &service.RegistryCreateRequest{
				Sources: []string{"nonexistent-source"},
			},
			validateFunc: func(t *testing.T, result *service.RegistryInfo, err error) {
				t.Helper()
				require.Error(t, err)
				require.Nil(t, result)
				require.ErrorIs(t, err, service.ErrSourceNotFound)
			},
		},
		{
			name: "nil request",
			setupFunc: func(_ *testing.T, _ *pgxpool.Pool) string {
				return "create-reg-nil"
			},
			createReq: nil,
			validateFunc: func(t *testing.T, result *service.RegistryInfo, err error) {
				t.Helper()
				require.Error(t, err)
				require.Nil(t, result)
				require.ErrorIs(t, err, service.ErrInvalidRegistryConfig)
			},
		},
		{
			name: "empty sources",
			setupFunc: func(_ *testing.T, _ *pgxpool.Pool) string {
				return "create-reg-empty"
			},
			createReq: &service.RegistryCreateRequest{
				Sources: []string{},
			},
			validateFunc: func(t *testing.T, result *service.RegistryInfo, err error) {
				t.Helper()
				require.Error(t, err)
				require.Nil(t, result)
				require.ErrorIs(t, err, service.ErrInvalidRegistryConfig)
			},
		},
		{
			name: "duplicate sources",
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) string {
				t.Helper()
				ctx := context.Background()
				queries := sqlc.New(pool)

				now := time.Now()

				_, err := queries.InsertSource(ctx, sqlc.InsertSourceParams{
					Name:         "create-dup-src-source",
					CreationType: sqlc.CreationTypeAPI,
					SourceType:   "managed",

					SourceConfig: []byte(`{}`),
					Syncable:     false,
					CreatedAt:    &now,
					UpdatedAt:    &now,
				})
				require.NoError(t, err)

				return "create-reg-dup-src"
			},
			createReq: &service.RegistryCreateRequest{
				Sources: []string{"create-dup-src-source", "create-dup-src-source"},
			},
			validateFunc: func(t *testing.T, result *service.RegistryInfo, err error) {
				t.Helper()
				require.Error(t, err)
				require.Nil(t, result)
				require.ErrorIs(t, err, service.ErrInvalidRegistryConfig)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc, cleanup := setupTestService(t)
			defer cleanup()

			registryName := tt.setupFunc(t, svc.pool)

			result, err := svc.CreateRegistry(
				context.Background(),
				registryName,
				tt.createReq,
			)

			tt.validateFunc(t, result, err)
		})
	}
}

func TestUpdateRegistry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		setupFunc    func(t *testing.T, pool *pgxpool.Pool) string
		updateReq    *service.RegistryCreateRequest
		validateFunc func(t *testing.T, result *service.RegistryInfo, err error)
	}{
		{
			name: "happy path",
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) string {
				t.Helper()
				ctx := context.Background()
				queries := sqlc.New(pool)

				now := time.Now()

				srcA, err := queries.InsertSource(ctx, sqlc.InsertSourceParams{
					Name:         "update-reg-source-a",
					CreationType: sqlc.CreationTypeAPI,
					SourceType:   "managed",

					SourceConfig: []byte(`{}`),
					Syncable:     false,
					CreatedAt:    &now,
					UpdatedAt:    &now,
				})
				require.NoError(t, err)

				_, err = queries.InsertSource(ctx, sqlc.InsertSourceParams{
					Name:         "update-reg-source-b",
					CreationType: sqlc.CreationTypeAPI,
					SourceType:   "kubernetes",

					SourceConfig: []byte(`{}`),
					Syncable:     false,
					CreatedAt:    &now,
					UpdatedAt:    &now,
				})
				require.NoError(t, err)

				reg, err := queries.UpsertRegistry(ctx, sqlc.UpsertRegistryParams{
					Name:         "update-reg-happy",
					CreationType: sqlc.CreationTypeAPI,
					CreatedAt:    &now,
					UpdatedAt:    &now,
				})
				require.NoError(t, err)

				err = queries.LinkRegistrySource(ctx, sqlc.LinkRegistrySourceParams{
					RegistryID: reg.ID,
					SourceID:   srcA.ID,
					Position:   0,
				})
				require.NoError(t, err)

				return "update-reg-happy"
			},
			updateReq: &service.RegistryCreateRequest{
				Sources: []string{"update-reg-source-b"},
			},
			validateFunc: func(t *testing.T, result *service.RegistryInfo, err error) {
				t.Helper()
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Equal(t, "update-reg-happy", result.Name)
				require.Equal(t, service.CreationTypeAPI, result.CreationType)
				require.Equal(t, []string{"update-reg-source-b"}, result.Sources)
			},
		},
		{
			name: "CONFIG protection",
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) string {
				t.Helper()
				ctx := context.Background()
				queries := sqlc.New(pool)

				now := time.Now()

				sourceID, err := queries.UpsertSource(ctx, sqlc.UpsertSourceParams{
					Name:         "update-reg-config-source",
					CreationType: sqlc.CreationTypeCONFIG,
					SourceType:   "git",
					Syncable:     true,
				})
				require.NoError(t, err)

				reg, err := queries.UpsertRegistry(ctx, sqlc.UpsertRegistryParams{
					Name:         "update-reg-config",
					CreationType: sqlc.CreationTypeCONFIG,
					CreatedAt:    &now,
					UpdatedAt:    &now,
				})
				require.NoError(t, err)

				err = queries.LinkRegistrySource(ctx, sqlc.LinkRegistrySourceParams{
					RegistryID: reg.ID,
					SourceID:   sourceID,
					Position:   0,
				})
				require.NoError(t, err)

				return "update-reg-config"
			},
			updateReq: &service.RegistryCreateRequest{
				Sources: []string{"update-reg-config-source"},
			},
			validateFunc: func(t *testing.T, result *service.RegistryInfo, err error) {
				t.Helper()
				require.Error(t, err)
				require.Nil(t, result)
				require.ErrorIs(t, err, service.ErrConfigRegistry)
			},
		},
		{
			name: "not found",
			setupFunc: func(_ *testing.T, _ *pgxpool.Pool) string {
				return "update-reg-nonexistent"
			},
			updateReq: &service.RegistryCreateRequest{
				Sources: []string{"some-source"},
			},
			validateFunc: func(t *testing.T, result *service.RegistryInfo, err error) {
				t.Helper()
				require.Error(t, err)
				require.Nil(t, result)
				require.ErrorIs(t, err, service.ErrRegistryNotFound)
			},
		},
		{
			name: "re-order sources",
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) string {
				t.Helper()
				ctx := context.Background()
				queries := sqlc.New(pool)

				now := time.Now()

				srcA, err := queries.InsertSource(ctx, sqlc.InsertSourceParams{
					Name:         "reorder-reg-source-a",
					CreationType: sqlc.CreationTypeAPI,
					SourceType:   "managed",

					SourceConfig: []byte(`{}`),
					Syncable:     false,
					CreatedAt:    &now,
					UpdatedAt:    &now,
				})
				require.NoError(t, err)

				srcB, err := queries.InsertSource(ctx, sqlc.InsertSourceParams{
					Name:         "reorder-reg-source-b",
					CreationType: sqlc.CreationTypeAPI,
					SourceType:   "kubernetes",

					SourceConfig: []byte(`{}`),
					Syncable:     false,
					CreatedAt:    &now,
					UpdatedAt:    &now,
				})
				require.NoError(t, err)

				reg, err := queries.UpsertRegistry(ctx, sqlc.UpsertRegistryParams{
					Name:         "reorder-reg-test",
					CreationType: sqlc.CreationTypeAPI,
					CreatedAt:    &now,
					UpdatedAt:    &now,
				})
				require.NoError(t, err)

				err = queries.LinkRegistrySource(ctx, sqlc.LinkRegistrySourceParams{
					RegistryID: reg.ID,
					SourceID:   srcA.ID,
					Position:   0,
				})
				require.NoError(t, err)
				err = queries.LinkRegistrySource(ctx, sqlc.LinkRegistrySourceParams{
					RegistryID: reg.ID,
					SourceID:   srcB.ID,
					Position:   1,
				})
				require.NoError(t, err)

				return "reorder-reg-test"
			},
			updateReq: &service.RegistryCreateRequest{
				Sources: []string{"reorder-reg-source-b", "reorder-reg-source-a"},
			},
			validateFunc: func(t *testing.T, result *service.RegistryInfo, err error) {
				t.Helper()
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Equal(t, "reorder-reg-test", result.Name)
				require.Equal(t, []string{"reorder-reg-source-b", "reorder-reg-source-a"}, result.Sources)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc, cleanup := setupTestService(t)
			defer cleanup()

			registryName := tt.setupFunc(t, svc.pool)

			result, err := svc.UpdateRegistry(
				context.Background(),
				registryName,
				tt.updateReq,
			)

			tt.validateFunc(t, result, err)
		})
	}
}

func TestDeleteServerVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		registryName string
		serverName   string
		// versions to publish in order before the delete
		publishVersions []string
		// version to delete
		deleteVersion string
		// if non-empty, the version we expect "latest" to resolve to after deletion
		expectLatestVersion string
		// if true, GET latest should return ErrNotFound after deletion
		expectLatestNotFound bool
		// if true, the delete call itself should return an error
		expectDeleteErr bool
		// expected error from the delete call (wrapped with ErrorIs)
		expectDeleteErrIs error
		// if true, use a non-managed (REMOTE) registry
		useRemoteRegistry bool
	}{
		{
			name:                "delete latest version re-points to next highest",
			registryName:        "del-srv-repoint",
			serverName:          "com.example/test-server",
			publishVersions:     []string{"1.0.0", "1.1.0", "2.0.0"},
			deleteVersion:       "2.0.0",
			expectLatestVersion: "1.1.0",
		},
		{
			name:                "delete non-latest version does not change pointer",
			registryName:        "del-srv-nonlatest",
			serverName:          "com.example/test-server",
			publishVersions:     []string{"1.0.0", "2.0.0"},
			deleteVersion:       "1.0.0",
			expectLatestVersion: "2.0.0",
		},
		{
			name:                 "delete the only version removes entry entirely",
			registryName:         "del-srv-only",
			serverName:           "com.example/test-server",
			publishVersions:      []string{"1.0.0"},
			deleteVersion:        "1.0.0",
			expectLatestNotFound: true,
		},
		{
			name:              "delete non-existent version returns error",
			registryName:      "del-srv-noexist",
			serverName:        "com.example/test-server",
			publishVersions:   []string{"1.0.0"},
			deleteVersion:     "9.9.9",
			expectDeleteErr:   true,
			expectDeleteErrIs: service.ErrNotFound,
		},
		{
			name:              "delete on non-managed registry returns error",
			registryName:      "del-srv-remote",
			serverName:        "com.example/test-server",
			publishVersions:   nil,
			deleteVersion:     "1.0.0",
			useRemoteRegistry: true,
			expectDeleteErr:   true,
			expectDeleteErrIs: service.ErrNoManagedSource,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc, cleanup := setupTestService(t)
			defer cleanup()

			ctx := context.Background()
			queries := sqlc.New(svc.pool)

			// Create the registry.
			if tt.useRemoteRegistry {
				_, err := queries.InsertSource(ctx, sqlc.InsertSourceParams{
					Name:         tt.registryName,
					SourceType:   "git",
					CreationType: sqlc.CreationTypeAPI,
					Syncable:     true,
				})
				require.NoError(t, err)
			} else {
				src, err := queries.InsertSource(ctx, sqlc.InsertSourceParams{
					Name:         tt.registryName,
					SourceType:   "managed",
					CreationType: sqlc.CreationTypeCONFIG,
					Syncable:     false,
				})
				require.NoError(t, err)

				now := time.Now()
				reg, err := queries.UpsertRegistry(ctx, sqlc.UpsertRegistryParams{
					Name:         tt.registryName,
					CreationType: sqlc.CreationTypeCONFIG,
					CreatedAt:    &now,
					UpdatedAt:    &now,
				})
				require.NoError(t, err)

				err = queries.LinkRegistrySource(ctx, sqlc.LinkRegistrySourceParams{
					RegistryID: reg.ID,
					SourceID:   src.ID,
					Position:   0,
				})
				require.NoError(t, err)
			}

			// Publish the requested versions.
			for _, ver := range tt.publishVersions {
				_, err := svc.PublishServerVersion(
					ctx,
					service.WithServerData(&upstreamv0.ServerJSON{
						Name:        tt.serverName,
						Version:     ver,
						Description: "test description",
					}),
				)
				require.NoError(t, err)
			}

			// Perform the delete.
			deleteErr := svc.DeleteServerVersion(
				ctx,
				service.WithName(tt.serverName),
				service.WithVersion(tt.deleteVersion),
			)

			if tt.expectDeleteErr {
				require.Error(t, deleteErr)
				if tt.expectDeleteErrIs != nil {
					require.ErrorIs(t, deleteErr, tt.expectDeleteErrIs)
				}
				return
			}
			require.NoError(t, deleteErr)

			// Verify the latest pointer.
			latest, found := latestManagedVersion(t, svc, sqlc.EntryTypeMCP, tt.serverName)
			if tt.expectLatestNotFound {
				require.False(t, found, "latest pointer should be gone")
				return
			}
			require.True(t, found)
			require.Equal(t, tt.expectLatestVersion, latest)
		})
	}
}

func TestDeleteSkillVersion(t *testing.T) {
	t.Parallel()

	const namespace = "com.example"

	tests := []struct {
		name         string
		registryName string
		skillName    string
		// versions to publish in order before the delete
		publishVersions []string
		// version to delete
		deleteVersion string
		// if non-empty, the version we expect "latest" to resolve to after deletion
		expectLatestVersion string
		// if true, GET latest should return ErrNotFound after deletion
		expectLatestNotFound bool
		// if true, the delete call itself should return an error
		expectDeleteErr bool
		// expected error from the delete call (wrapped with ErrorIs)
		expectDeleteErrIs error
		// if true, use a non-managed (REMOTE) registry
		useRemoteRegistry bool
	}{
		{
			name:                "delete latest version re-points to next highest",
			registryName:        "del-skill-repoint",
			skillName:           "test-skill",
			publishVersions:     []string{"1.0.0", "1.1.0", "2.0.0"},
			deleteVersion:       "2.0.0",
			expectLatestVersion: "1.1.0",
		},
		{
			name:                "delete non-latest version does not change pointer",
			registryName:        "del-skill-nonlatest",
			skillName:           "test-skill",
			publishVersions:     []string{"1.0.0", "2.0.0"},
			deleteVersion:       "1.0.0",
			expectLatestVersion: "2.0.0",
		},
		{
			name:                 "delete the only version removes entry entirely",
			registryName:         "del-skill-only",
			skillName:            "test-skill",
			publishVersions:      []string{"1.0.0"},
			deleteVersion:        "1.0.0",
			expectLatestNotFound: true,
		},
		{
			name:              "delete non-existent version returns error",
			registryName:      "del-skill-noexist",
			skillName:         "test-skill",
			publishVersions:   []string{"1.0.0"},
			deleteVersion:     "9.9.9",
			expectDeleteErr:   true,
			expectDeleteErrIs: service.ErrNotFound,
		},
		{
			name:              "delete on non-managed registry returns error",
			registryName:      "del-skill-remote",
			skillName:         "test-skill",
			publishVersions:   nil,
			deleteVersion:     "1.0.0",
			useRemoteRegistry: true,
			expectDeleteErr:   true,
			expectDeleteErrIs: service.ErrNoManagedSource,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc, cleanup := setupTestService(t)
			defer cleanup()

			ctx := context.Background()
			queries := sqlc.New(svc.pool)

			// Create the registry.
			if tt.useRemoteRegistry {
				_, err := queries.InsertSource(ctx, sqlc.InsertSourceParams{
					Name:         tt.registryName,
					SourceType:   "git",
					CreationType: sqlc.CreationTypeAPI,
					Syncable:     true,
				})
				require.NoError(t, err)
			} else {
				src, err := queries.InsertSource(ctx, sqlc.InsertSourceParams{
					Name:         tt.registryName,
					SourceType:   "managed",
					CreationType: sqlc.CreationTypeCONFIG,
					Syncable:     false,
				})
				require.NoError(t, err)

				now := time.Now()
				reg, err := queries.UpsertRegistry(ctx, sqlc.UpsertRegistryParams{
					Name:         tt.registryName,
					CreationType: sqlc.CreationTypeCONFIG,
					CreatedAt:    &now,
					UpdatedAt:    &now,
				})
				require.NoError(t, err)

				err = queries.LinkRegistrySource(ctx, sqlc.LinkRegistrySourceParams{
					RegistryID: reg.ID,
					SourceID:   src.ID,
					Position:   0,
				})
				require.NoError(t, err)
			}

			// Publish the requested versions.
			for _, ver := range tt.publishVersions {
				skill := &service.Skill{
					Name:        tt.skillName,
					Namespace:   namespace,
					Version:     ver,
					Title:       "Test Skill",
					Description: "test description",
				}
				_, err := svc.PublishSkill(
					ctx,
					skill,
				)
				require.NoError(t, err)
			}

			// Perform the delete.
			deleteErr := svc.DeleteSkillVersion(
				ctx,
				service.WithName(tt.skillName),
				service.WithVersion(tt.deleteVersion),
			)

			if tt.expectDeleteErr {
				require.Error(t, deleteErr)
				if tt.expectDeleteErrIs != nil {
					require.ErrorIs(t, deleteErr, tt.expectDeleteErrIs)
				}
				return
			}
			require.NoError(t, deleteErr)

			// Verify the latest pointer.
			latest, found := latestManagedVersion(t, svc, sqlc.EntryTypeSKILL, tt.skillName)
			if tt.expectLatestNotFound {
				require.False(t, found, "latest pointer should be gone")
				return
			}
			require.True(t, found)
			require.Equal(t, tt.expectLatestVersion, latest)
		})
	}
}
