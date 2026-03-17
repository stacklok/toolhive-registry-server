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

	"github.com/stacklok/toolhive-registry-server/database"
	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/db/sqlc"
	"github.com/stacklok/toolhive-registry-server/internal/service"
)

// setupTestService creates a test database service with a migrated database
func setupTestService(t *testing.T) (*dbService, func()) {
	t.Helper()

	ctx := context.Background()
	db, cleanupFunc := database.SetupTestDB(t)
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

	return svc, serviceCleanup
}

// setupTestData creates a registry and server versions for testing
//
//nolint:thelper // We want to see these lines in the test output
func setupTestData(t *testing.T, pool *pgxpool.Pool) {
	ctx := context.Background()
	queries := sqlc.New(pool)

	// Create a source
	regID, err := queries.UpsertSource(
		ctx,
		sqlc.UpsertSourceParams{
			Name:         "test-registry",
			CreationType: sqlc.CreationTypeCONFIG,
			SourceType:   "git",
			Syncable:     true,
		},
	)
	require.NoError(t, err)

	// Create a registry and link the source to it
	now := time.Now().UTC()
	registry, err := queries.UpsertRegistry(ctx, sqlc.UpsertRegistryParams{
		Name:         "test-registry",
		CreationType: sqlc.CreationTypeCONFIG,
		CreatedAt:    &now,
		UpdatedAt:    &now,
	})
	require.NoError(t, err)

	err = queries.LinkRegistrySource(ctx, sqlc.LinkRegistrySourceParams{
		RegistryID: registry.ID,
		SourceID:   regID,
		Position:   0,
	})
	require.NoError(t, err)

	// Create server versions

	// Server 1: one registry entry, multiple versions
	entryID1, err := queries.InsertRegistryEntry(
		ctx,
		sqlc.InsertRegistryEntryParams{
			SourceID:  regID,
			EntryType: sqlc.EntryTypeMCP,
			Name:      "com.example/test-server-1",
			CreatedAt: &now,
			UpdatedAt: &now,
		},
	)
	require.NoError(t, err)

	for i, version := range []string{"1.0.0", "1.1.0", "2.0.0"} {
		createdAt := now.Add(time.Duration(i) * time.Hour)
		versionID, err := queries.InsertEntryVersion(
			ctx,
			sqlc.InsertEntryVersionParams{
				EntryID:     entryID1,
				Version:     version,
				Title:       ptr.String("Test Server 1"),
				Description: ptr.String("Test server 1 description"),
				CreatedAt:   &createdAt,
				UpdatedAt:   &createdAt,
			},
		)
		require.NoError(t, err)

		serverID, err := queries.InsertServerVersion(
			ctx,
			sqlc.InsertServerVersionParams{
				VersionID:           versionID,
				Website:             ptr.String("https://example.com/server1"),
				UpstreamMeta:        []byte(`{"key": "value1"}`),
				ServerMeta:          []byte(`{"meta": "data1"}`),
				RepositoryUrl:       ptr.String("https://github.com/test/server1"),
				RepositoryID:        ptr.String("repo-1"),
				RepositorySubfolder: ptr.String("subfolder1"),
				RepositoryType:      ptr.String("git"),
			},
		)
		require.NoError(t, err)
		require.Equal(t, versionID, serverID)

		if version == "2.0.0" {
			_, err := queries.UpsertLatestServerVersion(
				ctx,
				sqlc.UpsertLatestServerVersionParams{
					SourceID:  regID,
					Name:      "com.example/test-server-1",
					Version:   "2.0.0",
					VersionID: versionID,
				},
			)
			require.NoError(t, err)
		}
	}

	// Server 2 with single version
	createdAt := now.Add(2 * time.Hour)
	entryID2, err := queries.InsertRegistryEntry(
		ctx,
		sqlc.InsertRegistryEntryParams{
			SourceID:  regID,
			EntryType: sqlc.EntryTypeMCP,
			Name:      "com.example/test-server-2",
			CreatedAt: &createdAt,
			UpdatedAt: &createdAt,
		},
	)
	require.NoError(t, err)

	versionID2, err := queries.InsertEntryVersion(
		ctx,
		sqlc.InsertEntryVersionParams{
			EntryID:     entryID2,
			Version:     "1.0.0",
			Title:       ptr.String("Test Server 2"),
			Description: ptr.String("Test server 2 description"),
			CreatedAt:   &createdAt,
			UpdatedAt:   &createdAt,
		},
	)
	require.NoError(t, err)

	serverID2, err := queries.InsertServerVersion(
		ctx,
		sqlc.InsertServerVersionParams{
			VersionID:           versionID2,
			Website:             ptr.String("https://example.com/server2"),
			UpstreamMeta:        []byte(`{"key": "value2"}`),
			ServerMeta:          []byte(`{"meta": "data2"}`),
			RepositoryUrl:       ptr.String("https://github.com/test/server2"),
			RepositoryID:        ptr.String("repo-2"),
			RepositorySubfolder: ptr.String("subfolder2"),
			RepositoryType:      ptr.String("git"),
		},
	)
	require.NoError(t, err)
	require.Equal(t, versionID2, serverID2)
}

func TestListServers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		setupFunc     func(*testing.T, *pgxpool.Pool)
		options       []service.Option
		expectedCount int
		validateFunc  func(*testing.T, *service.ListServersResult)
	}{
		{
			name: "list all servers with valid cursor",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupTestData(t, pool)
			},
			options: []service.Option{
				service.WithLimit(10),
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, result *service.ListServersResult) {
				require.Len(t, result.Servers, 4)
				// Verify server structure
				for _, server := range result.Servers {
					require.NotEmpty(t, server.Name)
					require.NotEmpty(t, server.Version)
				}
			},
		},
		{
			name: "list servers with limit",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupTestData(t, pool)
			},
			options: []service.Option{
				service.WithLimit(2),
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, result *service.ListServersResult) {
				require.Len(t, result.Servers, 2)
			},
		},
		{
			name: "invalid cursor format",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupTestData(t, pool)
			},
			options: []service.Option{
				service.WithCursor("invalid-base64"),
				service.WithLimit(10),
			},
		},
		{
			name: "cursor without comma separator returns error",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupTestData(t, pool)
			},
			options: []service.Option{
				// "YWJj" is base64("abc"), which has no comma separator
				service.WithCursor("YWJj"),
				service.WithLimit(10),
			},
		},
		{
			name: "empty database",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *pgxpool.Pool) {
				// Don't set up any data
			},
			options: []service.Option{
				service.WithLimit(10),
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, result *service.ListServersResult) {
				require.Len(t, result.Servers, 0)
			},
		},
		{
			name: "list servers with registry name",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupTestData(t, pool)
			},
			options: []service.Option{
				service.WithRegistryName("test-registry"),
				service.WithLimit(10),
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, result *service.ListServersResult) {
				require.Len(t, result.Servers, 4)
				// Verify server structure
				for _, server := range result.Servers {
					require.NotEmpty(t, server.Name)
					require.NotEmpty(t, server.Version)
				}
			},
		},
		{
			name: "list servers with non-existent registry name",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupTestData(t, pool)
			},
			options: []service.Option{
				service.WithRegistryName("non-existent-registry"),
				service.WithLimit(10),
			},
		},
		{
			name: "list servers with search by name",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupTestData(t, pool)
			},
			options: []service.Option{
				service.WithSearch("server-1"),
				service.WithLimit(10),
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, result *service.ListServersResult) {
				require.Len(t, result.Servers, 3) // Should find all 3 versions of com.example/test-server-1
				for _, server := range result.Servers {
					require.Equal(t, "com.example/test-server-1", server.Name)
				}
			},
		},
		{
			name: "list servers with search by title",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupTestData(t, pool)
			},
			options: []service.Option{
				service.WithSearch("Test Server 2"),
				service.WithLimit(10),
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, result *service.ListServersResult) {
				require.Len(t, result.Servers, 1) // Should find only com.example/test-server-2
				require.Equal(t, "com.example/test-server-2", result.Servers[0].Name)
			},
		},
		{
			name: "list servers with search by description",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupTestData(t, pool)
			},
			options: []service.Option{
				service.WithSearch("server 2 description"),
				service.WithLimit(10),
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, result *service.ListServersResult) {
				require.Len(t, result.Servers, 1) // Should find only com.example/test-server-2
				require.Equal(t, "com.example/test-server-2", result.Servers[0].Name)
			},
		},
		{
			name: "list servers with case-insensitive search",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupTestData(t, pool)
			},
			options: []service.Option{
				service.WithSearch("SERVER-1"), // Uppercase
				service.WithLimit(10),
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, result *service.ListServersResult) {
				require.Len(t, result.Servers, 3) // Should still find com.example/test-server-1 versions
				for _, server := range result.Servers {
					require.Equal(t, "com.example/test-server-1", server.Name)
				}
			},
		},
		{
			name: "list servers with partial search match",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupTestData(t, pool)
			},
			options: []service.Option{
				service.WithSearch("server"), // Partial match
				service.WithLimit(10),
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, result *service.ListServersResult) {
				require.Len(t, result.Servers, 4) // Should find all servers (both com.example/test-server-1 and com.example/test-server-2)
			},
		},
		{
			name: "list servers with search no matches",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupTestData(t, pool)
			},
			options: []service.Option{
				service.WithSearch("nonexistent"),
				service.WithLimit(10),
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, result *service.ListServersResult) {
				require.Len(t, result.Servers, 0) // Should find no servers
			},
		},
		{
			name: "list servers with search and registry filter",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupTestData(t, pool)
			},
			options: []service.Option{
				service.WithSearch("server-1"),
				service.WithRegistryName("test-registry"),
				service.WithLimit(10),
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, result *service.ListServersResult) {
				require.Len(t, result.Servers, 3) // Should find com.example/test-server-1 versions in test-registry
				for _, server := range result.Servers {
					require.Equal(t, "com.example/test-server-1", server.Name)
				}
			},
		},
		{
			name: "list servers filtered by version latest",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupTestData(t, pool)
			},
			options: []service.Option{
				service.WithVersion("latest"),
				service.WithLimit(10),
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, result *service.ListServersResult) {
				// Only com.example/test-server-1 v2.0.0 has a latest_entry_version record
				require.Len(t, result.Servers, 1)
				require.Equal(t, "com.example/test-server-1", result.Servers[0].Name)
				require.Equal(t, "2.0.0", result.Servers[0].Version)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc, cleanup := setupTestService(t)
			defer cleanup()

			tt.setupFunc(t, svc.pool)

			result, err := svc.ListServers(context.Background(), tt.options...)

			if tt.validateFunc == nil {
				require.Error(t, err)
				require.Nil(t, result)
				return
			}

			require.NoError(t, err)
			tt.validateFunc(t, result)
		})
	}
}

func TestListServerVersions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		setupFunc    func(*testing.T, *pgxpool.Pool)
		options      []service.Option
		validateFunc func(*testing.T, []*upstreamv0.ServerJSON)
	}{
		{
			name: "list versions for existing server",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupTestData(t, pool)
			},
			options: []service.Option{
				service.WithName("com.example/test-server-1"),
				service.WithLimit(10),
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, servers []*upstreamv0.ServerJSON) {
				require.Len(t, servers, 3)
				// Verify all are the same server name
				for _, server := range servers {
					require.Equal(t, "com.example/test-server-1", server.Name)
				}
				// Verify versions are present
				versions := make([]string, len(servers))
				for i, s := range servers {
					versions[i] = s.Version
				}
				require.Contains(t, versions, "1.0.0")
				require.Contains(t, versions, "1.1.0")
				require.Contains(t, versions, "2.0.0")
			},
		},
		{
			name: "list versions with limit",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupTestData(t, pool)
			},
			options: []service.Option{
				service.WithName("com.example/test-server-1"),
				service.WithLimit(2),
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, servers []*upstreamv0.ServerJSON) {
				require.Len(t, servers, 2)
			},
		},
		{
			name: "list versions for non-existent server",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupTestData(t, pool)
			},
			options: []service.Option{
				service.WithName("com.example/non-existent-server"),
				service.WithLimit(10),
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, servers []*upstreamv0.ServerJSON) {
				require.Len(t, servers, 0)
			},
		},
		{
			name: "list versions with next cursor",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupTestData(t, pool)
			},
			options: []service.Option{
				service.WithName("com.example/test-server-1"),
				func(opts any) error {
					// Set nextTime to 30 minutes from now, so only versions created at +1h and +2h are returned
					nextTime := time.Now().Add(30 * time.Minute).UTC()

					castOpts := opts.(*service.ListServerVersionsOptions)
					castOpts.Next = &nextTime
					castOpts.Limit = 10

					return nil
				},
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, servers []*upstreamv0.ServerJSON) {
				require.Len(t, servers, 2)
			},
		},
		{
			name: "list versions with prev cursor",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupTestData(t, pool)
			},
			options: []service.Option{
				service.WithName("com.example/test-server-1"),
				func(opts any) error {
					prevTime := time.Now().Add(1 * time.Hour).UTC()

					castOpts := opts.(*service.ListServerVersionsOptions)
					castOpts.Prev = &prevTime
					castOpts.Limit = 10

					return nil
				},
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, servers []*upstreamv0.ServerJSON) {
				require.Len(t, servers, 2)
			},
		},
		{
			name: "invalid name option",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupTestData(t, pool)
			},
			options: []service.Option{
				service.WithName(""), // Empty name should error
				service.WithLimit(10),
			},
		},
		{
			name: "list versions with registry name",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupTestData(t, pool)
			},
			options: []service.Option{
				service.WithName("com.example/test-server-1"),
				service.WithRegistryName("test-registry"),
				service.WithLimit(10),
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, servers []*upstreamv0.ServerJSON) {
				require.Len(t, servers, 3)
				// Verify all are the same server name
				for _, server := range servers {
					require.Equal(t, "com.example/test-server-1", server.Name)
				}
				// Verify versions are present
				versions := make([]string, len(servers))
				for i, s := range servers {
					versions[i] = s.Version
				}
				require.Contains(t, versions, "1.0.0")
				require.Contains(t, versions, "1.1.0")
				require.Contains(t, versions, "2.0.0")
			},
		},
		{
			name: "list versions with non-existent registry name",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupTestData(t, pool)
			},
			options: []service.Option{
				service.WithName("com.example/test-server-1"),
				service.WithRegistryName("non-existent-registry"),
				service.WithLimit(10),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc, cleanup := setupTestService(t)
			defer cleanup()

			tt.setupFunc(t, svc.pool)

			servers, err := svc.ListServerVersions(context.Background(), tt.options...)

			if tt.validateFunc == nil {
				require.Error(t, err)
				require.Nil(t, servers)
				return
			}

			require.NoError(t, err)
			tt.validateFunc(t, servers)
		})
	}
}

func TestGetServerVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		setupFunc    func(*testing.T, *pgxpool.Pool)
		options      []service.Option
		validateFunc func(*testing.T, *upstreamv0.ServerJSON)
	}{
		{
			name: "get existing server version",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupTestData(t, pool)
			},
			options: []service.Option{
				service.WithName("com.example/test-server-1"),
				service.WithVersion("1.0.0"),
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, server *upstreamv0.ServerJSON) {
				require.NotNil(t, server)
				require.Equal(t, "com.example/test-server-1", server.Name)
				require.Equal(t, "1.0.0", server.Version)
				require.Equal(t, "Test server 1 description", server.Description)
				require.Equal(t, "Test Server 1", server.Title)
				require.NotNil(t, server.Repository)
				require.Equal(t, "https://github.com/test/server1", server.Repository.URL)
			},
		},
		{
			name: "get different version of same server",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupTestData(t, pool)
			},
			options: []service.Option{
				service.WithName("com.example/test-server-1"),
				service.WithVersion("2.0.0"),
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, server *upstreamv0.ServerJSON) {
				require.NotNil(t, server)
				require.Equal(t, "com.example/test-server-1", server.Name)
				require.Equal(t, "2.0.0", server.Version)
			},
		},
		{
			name: "get non-existent server",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupTestData(t, pool)
			},
			options: []service.Option{
				service.WithName("com.example/non-existent-server"),
				service.WithVersion("1.0.0"),
			},
		},
		{
			name: "get non-existent version",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupTestData(t, pool)
			},
			options: []service.Option{
				service.WithName("com.example/test-server-1"),
				service.WithVersion("999.999.999"),
			},
		},
		{
			name: "invalid name option",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupTestData(t, pool)
			},
			options: []service.Option{
				service.WithName(""), // Empty name should error
				service.WithVersion("1.0.0"),
			},
		},
		{
			name: "invalid version option",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupTestData(t, pool)
			},
			options: []service.Option{
				service.WithName("com.example/test-server-1"),
				service.WithVersion(""), // Empty version should error
			},
		},
		{
			name: "get server from different registry",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupTestData(t, pool)
			},
			options: []service.Option{
				service.WithName("com.example/test-server-2"),
				service.WithVersion("1.0.0"),
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, server *upstreamv0.ServerJSON) {
				require.NotNil(t, server)
				require.Equal(t, "com.example/test-server-2", server.Name)
				require.Equal(t, "1.0.0", server.Version)
				require.Equal(t, "Test server 2 description", server.Description)
			},
		},
		{
			name: "get server version with packages and remotes",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				ctx := context.Background()
				queries := sqlc.New(pool)

				// Create a source
				regID, err := queries.UpsertSource(
					ctx,
					sqlc.UpsertSourceParams{
						Name:         "test-registry-with-packages",
						CreationType: sqlc.CreationTypeCONFIG,
						SourceType:   "git",
						Syncable:     true,
					},
				)
				require.NoError(t, err)

				// Create a server version
				now := time.Now().UTC()
				entryID, err := queries.InsertRegistryEntry(
					ctx,
					sqlc.InsertRegistryEntryParams{
						SourceID:  regID,
						EntryType: sqlc.EntryTypeMCP,
						Name:      "com.test/server-with-packages",
						CreatedAt: &now,
						UpdatedAt: &now,
					},
				)
				require.NoError(t, err)

				versionID, err := queries.InsertEntryVersion(
					ctx,
					sqlc.InsertEntryVersionParams{
						EntryID:     entryID,
						Version:     "1.0.0",
						Title:       ptr.String("Test Server With Packages"),
						Description: ptr.String("Test server with packages and remotes"),
						CreatedAt:   &now,
						UpdatedAt:   &now,
					},
				)
				require.NoError(t, err)

				serverID, err := queries.InsertServerVersion(
					ctx,
					sqlc.InsertServerVersionParams{
						VersionID:           versionID,
						Website:             ptr.String("https://example.com/server-with-packages"),
						UpstreamMeta:        []byte(`{"key": "value"}`),
						ServerMeta:          []byte(`{"meta": "data"}`),
						RepositoryUrl:       ptr.String("https://github.com/test/server-with-packages"),
						RepositoryID:        ptr.String("repo-with-packages"),
						RepositorySubfolder: ptr.String("subfolder"),
						RepositoryType:      ptr.String("git"),
					},
				)
				require.NoError(t, err)
				require.Equal(t, versionID, serverID)

				// Add a package
				err = queries.InsertServerPackage(
					ctx,
					sqlc.InsertServerPackageParams{
						ServerID:         versionID,
						RegistryType:     "npm",
						PkgRegistryUrl:   "https://registry.npmjs.org",
						PkgIdentifier:    "@test/package",
						PkgVersion:       "1.0.0",
						RuntimeHint:      ptr.String("npx"),
						RuntimeArguments: []string{"--yes"},
						PackageArguments: []string{"--arg", "value"},
						EnvVars:          []byte(`[{"name":"NODE_ENV"}]`),
						Sha256Hash:       ptr.String("abc123def456"),
						Transport:        "stdio",
						TransportUrl:     ptr.String("https://example.com/transport"),
						TransportHeaders: []byte(`[{"name":"X-Custom: header"}]`),
					},
				)
				require.NoError(t, err)

				// Add a remote
				err = queries.InsertServerRemote(
					ctx,
					sqlc.InsertServerRemoteParams{
						ServerID:         versionID,
						Transport:        "sse",
						TransportUrl:     "https://example.com/sse",
						TransportHeaders: []byte(`[{"name":"Authorization: Bearer token"}]`),
					},
				)
				require.NoError(t, err)
			},
			options: []service.Option{
				service.WithName("com.test/server-with-packages"),
				service.WithVersion("1.0.0"),
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, server *upstreamv0.ServerJSON) {
				require.NotNil(t, server)
				require.Equal(t, "com.test/server-with-packages", server.Name)
				require.Equal(t, "1.0.0", server.Version)
				require.Equal(t, "Test server with packages and remotes", server.Description)
				require.Equal(t, "Test Server With Packages", server.Title)
				require.NotNil(t, server.Repository)
				require.Equal(t, "https://github.com/test/server-with-packages", server.Repository.URL)

				// Validate packages
				require.Len(t, server.Packages, 1)
				require.Equal(t, "npm", server.Packages[0].RegistryType)
				require.Equal(t, "https://registry.npmjs.org", server.Packages[0].RegistryBaseURL)
				require.Equal(t, "@test/package", server.Packages[0].Identifier)
				require.Equal(t, "1.0.0", server.Packages[0].Version)
				require.Equal(t, "abc123def456", server.Packages[0].FileSHA256)
				require.Equal(t, "npx", server.Packages[0].RunTimeHint)
				require.Equal(t, "stdio", server.Packages[0].Transport.Type)

				// Validate remotes
				require.Len(t, server.Remotes, 1)
				require.Equal(t, "sse", server.Remotes[0].Type)
				require.Equal(t, "https://example.com/sse", server.Remotes[0].URL)
			},
		},
		{
			name: "get server version with registry name",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupTestData(t, pool)
			},
			options: []service.Option{
				service.WithName("com.example/test-server-1"),
				service.WithVersion("1.0.0"),
				service.WithRegistryName("test-registry"),
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, server *upstreamv0.ServerJSON) {
				require.NotNil(t, server)
				require.Equal(t, "com.example/test-server-1", server.Name)
				require.Equal(t, "1.0.0", server.Version)
				require.Equal(t, "Test server 1 description", server.Description)
				require.Equal(t, "Test Server 1", server.Title)
			},
		},
		{
			name: "get server version with non-existent registry name",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupTestData(t, pool)
			},
			options: []service.Option{
				service.WithName("com.example/test-server-1"),
				service.WithVersion("1.0.0"),
				service.WithRegistryName("non-existent-registry"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc, cleanup := setupTestService(t)
			defer cleanup()

			tt.setupFunc(t, svc.pool)

			server, err := svc.GetServerVersion(context.Background(), tt.options...)

			if tt.validateFunc == nil {
				require.Error(t, err)
				require.Nil(t, server)
				return
			}

			require.NoError(t, err)
			tt.validateFunc(t, server)
		})
	}
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
				db, cleanupFunc := database.SetupTestDB(t)
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
				db, cleanupFunc := database.SetupTestDB(t)
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
				db, cleanupFunc := database.SetupTestDB(t)
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
						FileSHA256:      "abc123",
						Transport: model.Transport{
							Type: "stdio",
							URL:  "https://example.com/transport",
						},
						RuntimeArguments:     []model.Argument{{Name: "--yes"}},
						PackageArguments:     []model.Argument{{Name: "--verbose"}},
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
				format := config.SourceFormatUpstream

				_, err := queries.InsertSource(ctx, sqlc.InsertSourceParams{
					Name:         "delete-src-happy",
					CreationType: sqlc.CreationTypeAPI,
					SourceType:   "managed",
					Format:       &format,
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
				format := config.SourceFormatUpstream

				// Create an API source
				src, err := queries.InsertSource(ctx, sqlc.InsertSourceParams{
					Name:         "delete-src-in-use",
					CreationType: sqlc.CreationTypeAPI,
					SourceType:   "managed",
					Format:       &format,
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
				format := config.SourceFormatUpstream
				now := time.Now()
				sourceConfig := []byte(`{"repository":"https://github.com/example/repo.git","branch":"main"}`)

				_, err := queries.InsertSource(ctx, sqlc.InsertSourceParams{
					Name:         "update-test-registry",
					CreationType: sqlc.CreationTypeAPI,
					SourceType:   sourceType,
					Format:       &format,
					SourceConfig: sourceConfig,
					Syncable:     true,
					CreatedAt:    &now,
					UpdatedAt:    &now,
				})
				require.NoError(t, err)

				return "update-test-registry"
			},
			updateReq: &service.SourceCreateRequest{
				Format: "toolhive",
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
				require.Equal(t, "toolhive", result.Format)
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
				format := config.SourceFormatUpstream
				now := time.Now()
				sourceConfig := []byte(`{"endpoint":"https://api.example.com/v1"}`)

				_, err := queries.InsertSource(ctx, sqlc.InsertSourceParams{
					Name:         "same-type-test-registry",
					CreationType: sqlc.CreationTypeAPI,
					SourceType:   sourceType,
					Format:       &format,
					SourceConfig: sourceConfig,
					Syncable:     true,
					CreatedAt:    &now,
					UpdatedAt:    &now,
				})
				require.NoError(t, err)

				return "same-type-test-registry"
			},
			updateReq: &service.SourceCreateRequest{
				Format: config.SourceFormatUpstream,
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
				Format: config.SourceFormatUpstream,
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
				Format: config.SourceFormatUpstream,
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
				format := config.SourceFormatUpstream
				now := time.Now()
				sourceConfig := []byte(`{"repository":"https://github.com/example/repo.git","branch":"main"}`)

				_, err := queries.InsertSource(ctx, sqlc.InsertSourceParams{
					Name:         "source-type-change-test",
					CreationType: sqlc.CreationTypeAPI,
					SourceType:   sourceType,
					Format:       &format,
					SourceConfig: sourceConfig,
					Syncable:     true,
					CreatedAt:    &now,
					UpdatedAt:    &now,
				})
				require.NoError(t, err)

				return "source-type-change-test"
			},
			updateReq: &service.SourceCreateRequest{
				Format: config.SourceFormatUpstream,
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
				format := config.SourceFormatUpstream
				now := time.Now()
				sourceConfig := []byte(`{"repository":"https://github.com/example/repo.git"}`)

				_, err := queries.InsertSource(ctx, sqlc.InsertSourceParams{
					Name:         "invalid-config-test",
					CreationType: sqlc.CreationTypeAPI,
					SourceType:   sourceType,
					Format:       &format,
					SourceConfig: sourceConfig,
					Syncable:     true,
					CreatedAt:    &now,
					UpdatedAt:    &now,
				})
				require.NoError(t, err)

				return "invalid-config-test"
			},
			updateReq: &service.SourceCreateRequest{
				Format: config.SourceFormatUpstream,
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
				format := config.SourceFormatUpstream
				now := time.Now()
				sourceConfig := []byte(`{"repository":"https://github.com/example/repo.git"}`)

				_, err := queries.InsertSource(ctx, sqlc.InsertSourceParams{
					Name:         "no-source-type-test",
					CreationType: sqlc.CreationTypeAPI,
					SourceType:   sourceType,
					Format:       &format,
					SourceConfig: sourceConfig,
					Syncable:     true,
					CreatedAt:    &now,
					UpdatedAt:    &now,
				})
				require.NoError(t, err)

				return "no-source-type-test"
			},
			updateReq: &service.SourceCreateRequest{
				Format: config.SourceFormatUpstream,
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
				format := config.SourceFormatUpstream
				now := time.Now()
				sourceConfig := []byte(`{"endpoint":"https://api.example.com/v1"}`)

				_, err := queries.InsertSource(ctx, sqlc.InsertSourceParams{
					Name:         "missing-sync-policy-test",
					CreationType: sqlc.CreationTypeAPI,
					SourceType:   sourceType,
					Format:       &format,
					SourceConfig: sourceConfig,
					Syncable:     true,
					CreatedAt:    &now,
					UpdatedAt:    &now,
				})
				require.NoError(t, err)

				return "missing-sync-policy-test"
			},
			updateReq: &service.SourceCreateRequest{
				Format: config.SourceFormatUpstream,
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
				format := config.SourceFormatUpstream
				now := time.Now()
				sourceConfig := []byte(`{}`)

				_, err := queries.InsertSource(ctx, sqlc.InsertSourceParams{
					Name:         "managed-registry-update-test",
					CreationType: sqlc.CreationTypeAPI,
					SourceType:   sourceType,
					Format:       &format,
					SourceConfig: sourceConfig,
					Syncable:     false,
					CreatedAt:    &now,
					UpdatedAt:    &now,
				})
				require.NoError(t, err)

				return "managed-registry-update-test"
			},
			updateReq: &service.SourceCreateRequest{
				Format:  "toolhive",
				Managed: &config.ManagedConfig{},
				// No SyncPolicy needed for managed type
			},
			validateFunc: func(t *testing.T, result *service.SourceInfo, err error) {
				t.Helper()
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Equal(t, "managed-registry-update-test", result.Name)
				require.Equal(t, config.SourceTypeManaged, result.SourceType)
				require.Equal(t, "toolhive", result.Format)
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
				format := config.SourceFormatUpstream

				// Create two API sources
				srcA, err := queries.InsertSource(ctx, sqlc.InsertSourceParams{
					Name:         "list-reg-source-a",
					CreationType: sqlc.CreationTypeAPI,
					SourceType:   "managed",
					Format:       &format,
					SourceConfig: []byte(`{}`),
					Syncable:     false,
					CreatedAt:    &now,
					UpdatedAt:    &now,
				})
				require.NoError(t, err)

				srcB, err := queries.InsertSource(ctx, sqlc.InsertSourceParams{
					Name:         "list-reg-source-b",
					CreationType: sqlc.CreationTypeAPI,
					SourceType:   "managed",
					Format:       &format,
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
				format := config.SourceFormatUpstream

				srcA, err := queries.InsertSource(ctx, sqlc.InsertSourceParams{
					Name:         "get-reg-source-a",
					CreationType: sqlc.CreationTypeAPI,
					SourceType:   "managed",
					Format:       &format,
					SourceConfig: []byte(`{}`),
					Syncable:     false,
					CreatedAt:    &now,
					UpdatedAt:    &now,
				})
				require.NoError(t, err)

				srcB, err := queries.InsertSource(ctx, sqlc.InsertSourceParams{
					Name:         "get-reg-source-b",
					CreationType: sqlc.CreationTypeAPI,
					SourceType:   "managed",
					Format:       &format,
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
				format := config.SourceFormatUpstream

				_, err := queries.InsertSource(ctx, sqlc.InsertSourceParams{
					Name:         "create-reg-source-a",
					CreationType: sqlc.CreationTypeAPI,
					SourceType:   "managed",
					Format:       &format,
					SourceConfig: []byte(`{}`),
					Syncable:     false,
					CreatedAt:    &now,
					UpdatedAt:    &now,
				})
				require.NoError(t, err)

				_, err = queries.InsertSource(ctx, sqlc.InsertSourceParams{
					Name:         "create-reg-source-b",
					CreationType: sqlc.CreationTypeAPI,
					SourceType:   "managed",
					Format:       &format,
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
				format := config.SourceFormatUpstream

				src, err := queries.InsertSource(ctx, sqlc.InsertSourceParams{
					Name:         "create-dup-source",
					CreationType: sqlc.CreationTypeAPI,
					SourceType:   "managed",
					Format:       &format,
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
				format := config.SourceFormatUpstream

				_, err := queries.InsertSource(ctx, sqlc.InsertSourceParams{
					Name:         "create-dup-src-source",
					CreationType: sqlc.CreationTypeAPI,
					SourceType:   "managed",
					Format:       &format,
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
				format := config.SourceFormatUpstream

				srcA, err := queries.InsertSource(ctx, sqlc.InsertSourceParams{
					Name:         "update-reg-source-a",
					CreationType: sqlc.CreationTypeAPI,
					SourceType:   "managed",
					Format:       &format,
					SourceConfig: []byte(`{}`),
					Syncable:     false,
					CreatedAt:    &now,
					UpdatedAt:    &now,
				})
				require.NoError(t, err)

				_, err = queries.InsertSource(ctx, sqlc.InsertSourceParams{
					Name:         "update-reg-source-b",
					CreationType: sqlc.CreationTypeAPI,
					SourceType:   "managed",
					Format:       &format,
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
				format := config.SourceFormatUpstream

				srcA, err := queries.InsertSource(ctx, sqlc.InsertSourceParams{
					Name:         "reorder-reg-source-a",
					CreationType: sqlc.CreationTypeAPI,
					SourceType:   "managed",
					Format:       &format,
					SourceConfig: []byte(`{}`),
					Syncable:     false,
					CreatedAt:    &now,
					UpdatedAt:    &now,
				})
				require.NoError(t, err)

				srcB, err := queries.InsertSource(ctx, sqlc.InsertSourceParams{
					Name:         "reorder-reg-source-b",
					CreationType: sqlc.CreationTypeAPI,
					SourceType:   "managed",
					Format:       &format,
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

func TestDeleteRegistry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		setupFunc    func(t *testing.T, pool *pgxpool.Pool) string
		validateFunc func(t *testing.T, svc *dbService, registryName string, err error)
	}{
		{
			name: "happy path",
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) string {
				t.Helper()
				ctx := context.Background()
				queries := sqlc.New(pool)

				now := time.Now()
				format := config.SourceFormatUpstream

				src, err := queries.InsertSource(ctx, sqlc.InsertSourceParams{
					Name:         "delete-reg-source",
					CreationType: sqlc.CreationTypeAPI,
					SourceType:   "managed",
					Format:       &format,
					SourceConfig: []byte(`{}`),
					Syncable:     false,
					CreatedAt:    &now,
					UpdatedAt:    &now,
				})
				require.NoError(t, err)

				reg, err := queries.UpsertRegistry(ctx, sqlc.UpsertRegistryParams{
					Name:         "delete-reg-happy",
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

				return "delete-reg-happy"
			},
			validateFunc: func(t *testing.T, svc *dbService, registryName string, err error) {
				t.Helper()
				require.NoError(t, err)

				// Verify the registry is gone
				_, getErr := svc.GetRegistryByName(context.Background(), registryName)
				require.Error(t, getErr)
				require.ErrorIs(t, getErr, service.ErrRegistryNotFound)
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
					Name:         "delete-reg-config-source",
					CreationType: sqlc.CreationTypeCONFIG,
					SourceType:   "git",
					Syncable:     true,
				})
				require.NoError(t, err)

				reg, err := queries.UpsertRegistry(ctx, sqlc.UpsertRegistryParams{
					Name:         "delete-reg-config",
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

				return "delete-reg-config"
			},
			validateFunc: func(t *testing.T, _ *dbService, _ string, err error) {
				t.Helper()
				require.Error(t, err)
				require.ErrorIs(t, err, service.ErrConfigRegistry)
			},
		},
		{
			name: "not found",
			setupFunc: func(_ *testing.T, _ *pgxpool.Pool) string {
				return "delete-reg-nonexistent"
			},
			validateFunc: func(t *testing.T, _ *dbService, _ string, err error) {
				t.Helper()
				require.Error(t, err)
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

			err := svc.DeleteRegistry(context.Background(), registryName)

			tt.validateFunc(t, svc, registryName, err)
		})
	}
}
