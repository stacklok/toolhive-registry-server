package database

import (
	"context"
	"encoding/base64"
	"testing"
	"time"

	"github.com/aws/smithy-go/ptr"
	"github.com/jackc/pgx/v5/pgxpool"
	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/database"
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
		pool: pool,
	}

	return svc, serviceCleanup
}

// createCursor creates a valid base64-encoded RFC3339 time cursor
//
// nolint:unparam
func createCursor(offset time.Duration) string {
	cursorTime := time.Now().Add(offset).UTC()
	return base64.StdEncoding.EncodeToString([]byte(cursorTime.Format(time.RFC3339)))
}

// setupTestData creates a registry and server versions for testing
//
//nolint:thelper // We want to see these lines in the test output
func setupTestData(t *testing.T, pool *pgxpool.Pool) {
	ctx := context.Background()
	queries := sqlc.New(pool)

	// Create a registry
	regID, err := queries.InsertRegistry(
		ctx,
		sqlc.InsertRegistryParams{
			Name:    "test-registry",
			RegType: sqlc.RegistryTypeREMOTE,
		},
	)
	require.NoError(t, err)

	// Create server versions
	now := time.Now().UTC()

	// Server 1 with multiple versions
	for i, version := range []string{"1.0.0", "1.1.0", "2.0.0"} {
		createdAt := now.Add(time.Duration(i) * time.Hour)
		_, err := queries.InsertServerVersion(
			ctx,
			sqlc.InsertServerVersionParams{
				Name:                "test-server-1",
				Version:             version,
				RegID:               regID,
				Description:         ptr.String("Test server 1 description"),
				Title:               ptr.String("Test Server 1"),
				Website:             ptr.String("https://example.com/server1"),
				UpstreamMeta:        []byte(`{"key": "value1"}`),
				ServerMeta:          []byte(`{"meta": "data1"}`),
				RepositoryUrl:       ptr.String("https://github.com/test/server1"),
				RepositoryID:        ptr.String("repo-1"),
				RepositorySubfolder: ptr.String("subfolder1"),
				RepositoryType:      ptr.String("git"),
				CreatedAt:           &createdAt,
				UpdatedAt:           &createdAt,
			},
		)
		require.NoError(t, err)
	}

	// Server 2 with single version
	createdAt := now.Add(2 * time.Hour)
	_, err = queries.InsertServerVersion(
		ctx,
		sqlc.InsertServerVersionParams{
			Name:                "test-server-2",
			Version:             "1.0.0",
			RegID:               regID,
			Description:         ptr.String("Test server 2 description"),
			Title:               ptr.String("Test Server 2"),
			Website:             ptr.String("https://example.com/server2"),
			UpstreamMeta:        []byte(`{"key": "value2"}`),
			ServerMeta:          []byte(`{"meta": "data2"}`),
			RepositoryUrl:       ptr.String("https://github.com/test/server2"),
			RepositoryID:        ptr.String("repo-2"),
			RepositorySubfolder: ptr.String("subfolder2"),
			RepositoryType:      ptr.String("git"),
			CreatedAt:           &createdAt,
			UpdatedAt:           &createdAt,
		},
	)
	require.NoError(t, err)
}

func TestListServers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		setupFunc     func(*testing.T, *pgxpool.Pool)
		options       []service.Option[service.ListServersOptions]
		expectedCount int
		validateFunc  func(*testing.T, []*upstreamv0.ServerJSON)
	}{
		{
			name: "list all servers with valid cursor",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupTestData(t, pool)
			},
			options: []service.Option[service.ListServersOptions]{
				service.WithCursor(createCursor(-1 * time.Hour)),
				service.WithLimit[service.ListServersOptions](10),
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, servers []*upstreamv0.ServerJSON) {
				require.Len(t, servers, 4)
				// Verify server structure
				for _, server := range servers {
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
			options: []service.Option[service.ListServersOptions]{
				service.WithCursor(createCursor(-1 * time.Hour)),
				service.WithLimit[service.ListServersOptions](2),
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, servers []*upstreamv0.ServerJSON) {
				require.Len(t, servers, 2)
			},
		},
		{
			name: "invalid cursor format",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupTestData(t, pool)
			},
			options: []service.Option[service.ListServersOptions]{
				service.WithCursor("invalid-base64"),
				service.WithLimit[service.ListServersOptions](10),
			},
		},
		{
			name: "invalid cursor time format",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupTestData(t, pool)
			},
			options: []service.Option[service.ListServersOptions]{
				service.WithCursor(base64.StdEncoding.EncodeToString([]byte("not-a-time"))),
				service.WithLimit[service.ListServersOptions](10),
			},
		},
		{
			name: "empty database",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *pgxpool.Pool) {
				// Don't set up any data
			},
			options: []service.Option[service.ListServersOptions]{
				service.WithCursor(createCursor(-1 * time.Hour)),
				service.WithLimit[service.ListServersOptions](10),
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, servers []*upstreamv0.ServerJSON) {
				require.Len(t, servers, 0)
			},
		},
		{
			name: "list servers with registry name",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupTestData(t, pool)
			},
			options: []service.Option[service.ListServersOptions]{
				service.WithRegistryName[service.ListServersOptions]("test-registry"),
				service.WithCursor(createCursor(-1 * time.Hour)),
				service.WithLimit[service.ListServersOptions](10),
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, servers []*upstreamv0.ServerJSON) {
				require.Len(t, servers, 4)
				// Verify server structure
				for _, server := range servers {
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
			options: []service.Option[service.ListServersOptions]{
				service.WithRegistryName[service.ListServersOptions]("non-existent-registry"),
				service.WithCursor(createCursor(-1 * time.Hour)),
				service.WithLimit[service.ListServersOptions](10),
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, servers []*upstreamv0.ServerJSON) {
				require.Len(t, servers, 0)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc, cleanup := setupTestService(t)
			defer cleanup()

			tt.setupFunc(t, svc.pool)

			servers, err := svc.ListServers(context.Background(), tt.options...)

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

func TestListServerVersions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		setupFunc    func(*testing.T, *pgxpool.Pool)
		options      []service.Option[service.ListServerVersionsOptions]
		validateFunc func(*testing.T, []*upstreamv0.ServerJSON)
	}{
		{
			name: "list versions for existing server",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupTestData(t, pool)
			},
			options: []service.Option[service.ListServerVersionsOptions]{
				service.WithName[service.ListServerVersionsOptions]("test-server-1"),
				service.WithLimit[service.ListServerVersionsOptions](10),
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, servers []*upstreamv0.ServerJSON) {
				require.Len(t, servers, 3)
				// Verify all are the same server name
				for _, server := range servers {
					require.Equal(t, "test-server-1", server.Name)
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
			options: []service.Option[service.ListServerVersionsOptions]{
				service.WithName[service.ListServerVersionsOptions]("test-server-1"),
				service.WithLimit[service.ListServerVersionsOptions](2),
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
			options: []service.Option[service.ListServerVersionsOptions]{
				service.WithName[service.ListServerVersionsOptions]("non-existent-server"),
				service.WithLimit[service.ListServerVersionsOptions](10),
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
			options: []service.Option[service.ListServerVersionsOptions]{
				service.WithName[service.ListServerVersionsOptions]("test-server-1"),
				func(opts *service.ListServerVersionsOptions) error {
					// Set nextTime to 30 minutes from now, so only versions created at +1h and +2h are returned
					nextTime := time.Now().Add(30 * time.Minute).UTC()
					opts.Next = &nextTime
					opts.Limit = 10
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
			options: []service.Option[service.ListServerVersionsOptions]{
				service.WithName[service.ListServerVersionsOptions]("test-server-1"),
				func(opts *service.ListServerVersionsOptions) error {
					prevTime := time.Now().Add(1 * time.Hour).UTC()
					opts.Prev = &prevTime
					opts.Limit = 10
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
			options: []service.Option[service.ListServerVersionsOptions]{
				service.WithName[service.ListServerVersionsOptions](""), // Empty name should error
				service.WithLimit[service.ListServerVersionsOptions](10),
			},
		},
		{
			name: "list versions with registry name",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupTestData(t, pool)
			},
			options: []service.Option[service.ListServerVersionsOptions]{
				service.WithName[service.ListServerVersionsOptions]("test-server-1"),
				service.WithRegistryName[service.ListServerVersionsOptions]("test-registry"),
				service.WithLimit[service.ListServerVersionsOptions](10),
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, servers []*upstreamv0.ServerJSON) {
				require.Len(t, servers, 3)
				// Verify all are the same server name
				for _, server := range servers {
					require.Equal(t, "test-server-1", server.Name)
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
			options: []service.Option[service.ListServerVersionsOptions]{
				service.WithName[service.ListServerVersionsOptions]("test-server-1"),
				service.WithRegistryName[service.ListServerVersionsOptions]("non-existent-registry"),
				service.WithLimit[service.ListServerVersionsOptions](10),
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, servers []*upstreamv0.ServerJSON) {
				require.Len(t, servers, 0)
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
		options      []service.Option[service.GetServerVersionOptions]
		validateFunc func(*testing.T, *upstreamv0.ServerJSON)
	}{
		{
			name: "get existing server version",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupTestData(t, pool)
			},
			options: []service.Option[service.GetServerVersionOptions]{
				service.WithName[service.GetServerVersionOptions]("test-server-1"),
				service.WithVersion[service.GetServerVersionOptions]("1.0.0"),
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, server *upstreamv0.ServerJSON) {
				require.NotNil(t, server)
				require.Equal(t, "test-server-1", server.Name)
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
			options: []service.Option[service.GetServerVersionOptions]{
				service.WithName[service.GetServerVersionOptions]("test-server-1"),
				service.WithVersion[service.GetServerVersionOptions]("2.0.0"),
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, server *upstreamv0.ServerJSON) {
				require.NotNil(t, server)
				require.Equal(t, "test-server-1", server.Name)
				require.Equal(t, "2.0.0", server.Version)
			},
		},
		{
			name: "get non-existent server",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupTestData(t, pool)
			},
			options: []service.Option[service.GetServerVersionOptions]{
				service.WithName[service.GetServerVersionOptions]("non-existent-server"),
				service.WithVersion[service.GetServerVersionOptions]("1.0.0"),
			},
		},
		{
			name: "get non-existent version",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupTestData(t, pool)
			},
			options: []service.Option[service.GetServerVersionOptions]{
				service.WithName[service.GetServerVersionOptions]("test-server-1"),
				service.WithVersion[service.GetServerVersionOptions]("999.999.999"),
			},
		},
		{
			name: "invalid name option",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupTestData(t, pool)
			},
			options: []service.Option[service.GetServerVersionOptions]{
				service.WithName[service.GetServerVersionOptions](""), // Empty name should error
				service.WithVersion[service.GetServerVersionOptions]("1.0.0"),
			},
		},
		{
			name: "invalid version option",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupTestData(t, pool)
			},
			options: []service.Option[service.GetServerVersionOptions]{
				service.WithName[service.GetServerVersionOptions]("test-server-1"),
				service.WithVersion[service.GetServerVersionOptions](""), // Empty version should error
			},
		},
		{
			name: "get server from different registry",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupTestData(t, pool)
			},
			options: []service.Option[service.GetServerVersionOptions]{
				service.WithName[service.GetServerVersionOptions]("test-server-2"),
				service.WithVersion[service.GetServerVersionOptions]("1.0.0"),
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, server *upstreamv0.ServerJSON) {
				require.NotNil(t, server)
				require.Equal(t, "test-server-2", server.Name)
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

				// Create a registry
				regID, err := queries.InsertRegistry(
					ctx,
					sqlc.InsertRegistryParams{
						Name:    "test-registry-with-packages",
						RegType: sqlc.RegistryTypeREMOTE,
					},
				)
				require.NoError(t, err)

				// Create a server version
				now := time.Now().UTC()
				serverID, err := queries.InsertServerVersion(
					ctx,
					sqlc.InsertServerVersionParams{
						Name:                "test-server-with-packages",
						Version:             "1.0.0",
						RegID:               regID,
						Description:         ptr.String("Test server with packages and remotes"),
						Title:               ptr.String("Test Server With Packages"),
						Website:             ptr.String("https://example.com/server-with-packages"),
						UpstreamMeta:        []byte(`{"key": "value"}`),
						ServerMeta:          []byte(`{"meta": "data"}`),
						RepositoryUrl:       ptr.String("https://github.com/test/server-with-packages"),
						RepositoryID:        ptr.String("repo-with-packages"),
						RepositorySubfolder: ptr.String("subfolder"),
						RepositoryType:      ptr.String("git"),
						CreatedAt:           &now,
						UpdatedAt:           &now,
					},
				)
				require.NoError(t, err)

				// Add a package
				err = queries.InsertServerPackage(
					ctx,
					sqlc.InsertServerPackageParams{
						ServerID:         serverID,
						RegistryType:     "npm",
						PkgRegistryUrl:   "https://registry.npmjs.org",
						PkgIdentifier:    "@test/package",
						PkgVersion:       "1.0.0",
						RuntimeHint:      ptr.String("npx"),
						RuntimeArguments: []string{"--yes"},
						PackageArguments: []string{"--arg", "value"},
						EnvVars:          []string{"NODE_ENV"},
						Sha256Hash:       ptr.String("abc123def456"),
						Transport:        "stdio",
						TransportUrl:     ptr.String("https://example.com/transport"),
						TransportHeaders: []string{"X-Custom: header"},
					},
				)
				require.NoError(t, err)

				// Add a remote
				err = queries.InsertServerRemote(
					ctx,
					sqlc.InsertServerRemoteParams{
						ServerID:         serverID,
						Transport:        "sse",
						TransportUrl:     "https://example.com/sse",
						TransportHeaders: []string{"Authorization: Bearer token"},
					},
				)
				require.NoError(t, err)
			},
			options: []service.Option[service.GetServerVersionOptions]{
				service.WithName[service.GetServerVersionOptions]("test-server-with-packages"),
				service.WithVersion[service.GetServerVersionOptions]("1.0.0"),
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, server *upstreamv0.ServerJSON) {
				require.NotNil(t, server)
				require.Equal(t, "test-server-with-packages", server.Name)
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
			options: []service.Option[service.GetServerVersionOptions]{
				service.WithName[service.GetServerVersionOptions]("test-server-1"),
				service.WithVersion[service.GetServerVersionOptions]("1.0.0"),
				service.WithRegistryName[service.GetServerVersionOptions]("test-registry"),
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, server *upstreamv0.ServerJSON) {
				require.NotNil(t, server)
				require.Equal(t, "test-server-1", server.Name)
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
			options: []service.Option[service.GetServerVersionOptions]{
				service.WithName[service.GetServerVersionOptions]("test-server-1"),
				service.WithVersion[service.GetServerVersionOptions]("1.0.0"),
				service.WithRegistryName[service.GetServerVersionOptions]("non-existent-registry"),
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

// setupMultiRegistryTestData creates multiple registries with servers for testing prefix behavior
//
//nolint:thelper // We want to see these lines in the test output
func setupMultiRegistryTestData(t *testing.T, pool *pgxpool.Pool) {
	ctx := context.Background()
	queries := sqlc.New(pool)

	now := time.Now().UTC()

	// Create first registry "registry-alpha"
	alphaRegID, err := queries.InsertRegistry(
		ctx,
		sqlc.InsertRegistryParams{
			Name:    "registry-alpha",
			RegType: sqlc.RegistryTypeREMOTE,
		},
	)
	require.NoError(t, err)

	// Create servers in registry-alpha
	for i, version := range []string{"1.0.0", "2.0.0"} {
		createdAt := now.Add(time.Duration(i) * time.Hour)
		_, err := queries.InsertServerVersion(
			ctx,
			sqlc.InsertServerVersionParams{
				Name:        "shared-server",
				Version:     version,
				RegID:       alphaRegID,
				Description: ptr.String("Server in registry-alpha"),
				Title:       ptr.String("Shared Server Alpha"),
				CreatedAt:   &createdAt,
				UpdatedAt:   &createdAt,
			},
		)
		require.NoError(t, err)
	}

	// Create unique server in registry-alpha
	createdAt := now.Add(3 * time.Hour)
	_, err = queries.InsertServerVersion(
		ctx,
		sqlc.InsertServerVersionParams{
			Name:        "unique-alpha-server",
			Version:     "1.0.0",
			RegID:       alphaRegID,
			Description: ptr.String("Unique server in registry-alpha"),
			Title:       ptr.String("Unique Alpha Server"),
			CreatedAt:   &createdAt,
			UpdatedAt:   &createdAt,
		},
	)
	require.NoError(t, err)

	// Create second registry "registry-beta"
	betaRegID, err := queries.InsertRegistry(
		ctx,
		sqlc.InsertRegistryParams{
			Name:    "registry-beta",
			RegType: sqlc.RegistryTypeREMOTE,
		},
	)
	require.NoError(t, err)

	// Create server with same name in registry-beta
	createdAt = now.Add(4 * time.Hour)
	_, err = queries.InsertServerVersion(
		ctx,
		sqlc.InsertServerVersionParams{
			Name:        "shared-server",
			Version:     "1.0.0",
			RegID:       betaRegID,
			Description: ptr.String("Server in registry-beta"),
			Title:       ptr.String("Shared Server Beta"),
			CreatedAt:   &createdAt,
			UpdatedAt:   &createdAt,
		},
	)
	require.NoError(t, err)

	// Create unique server in registry-beta
	createdAt = now.Add(5 * time.Hour)
	_, err = queries.InsertServerVersion(
		ctx,
		sqlc.InsertServerVersionParams{
			Name:        "unique-beta-server",
			Version:     "1.0.0",
			RegID:       betaRegID,
			Description: ptr.String("Unique server in registry-beta"),
			Title:       ptr.String("Unique Beta Server"),
			CreatedAt:   &createdAt,
			UpdatedAt:   &createdAt,
		},
	)
	require.NoError(t, err)
}

func TestListServers_WithPrefixNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		setupFunc    func(*testing.T, *pgxpool.Pool)
		options      []service.Option[service.ListServersOptions]
		validateFunc func(*testing.T, []*upstreamv0.ServerJSON, error)
	}{
		{
			name: "aggregated query prefixes server names with registry name",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupMultiRegistryTestData(t, pool)
			},
			options: []service.Option[service.ListServersOptions]{
				service.WithPrefixNames[service.ListServersOptions](true),
				service.WithCursor(createCursor(-1 * time.Hour)),
				service.WithLimit[service.ListServersOptions](20),
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, servers []*upstreamv0.ServerJSON, err error) {
				require.NoError(t, err)
				require.NotEmpty(t, servers)

				// All server names should be prefixed with their registry name
				for _, server := range servers {
					require.NotEmpty(t, server.Name)
					// Server names should contain a dot (registry prefix separator)
					require.Contains(t, server.Name, ".",
						"server name should be prefixed: %s", server.Name)
				}

				// Check specific prefixed names exist
				serverNames := make([]string, len(servers))
				for i, s := range servers {
					serverNames[i] = s.Name
				}
				require.Contains(t, serverNames, "registry-alpha.shared-server")
				require.Contains(t, serverNames, "registry-alpha.unique-alpha-server")
				require.Contains(t, serverNames, "registry-beta.shared-server")
				require.Contains(t, serverNames, "registry-beta.unique-beta-server")
			},
		},
		{
			name: "prefix format is registryName.serverName",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupMultiRegistryTestData(t, pool)
			},
			options: []service.Option[service.ListServersOptions]{
				service.WithPrefixNames[service.ListServersOptions](true),
				service.WithCursor(createCursor(-1 * time.Hour)),
				service.WithLimit[service.ListServersOptions](20),
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, servers []*upstreamv0.ServerJSON, err error) {
				require.NoError(t, err)
				require.NotEmpty(t, servers)

				// Verify prefix format for each server
				for _, server := range servers {
					// The format should be registryName.serverName
					// The first dot separates registry name from server name
					dotIndex := -1
					for i, c := range server.Name {
						if c == '.' {
							dotIndex = i
							break
						}
					}
					require.Greater(t, dotIndex, 0,
						"server name should have prefix with dot separator: %s", server.Name)

					prefix := server.Name[:dotIndex]
					require.True(t, prefix == "registry-alpha" || prefix == "registry-beta",
						"prefix should be a valid registry name, got: %s", prefix)
				}
			},
		},
		{
			name: "aggregated query with empty database returns empty list",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *pgxpool.Pool) {
				// No data setup - empty database
			},
			options: []service.Option[service.ListServersOptions]{
				service.WithPrefixNames[service.ListServersOptions](true),
				service.WithCursor(createCursor(-1 * time.Hour)),
				service.WithLimit[service.ListServersOptions](20),
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, servers []*upstreamv0.ServerJSON, err error) {
				require.NoError(t, err)
				require.Empty(t, servers)
			},
		},
		{
			name: "single registry with prefix names enabled",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupTestData(t, pool) // Uses single "test-registry"
			},
			options: []service.Option[service.ListServersOptions]{
				service.WithPrefixNames[service.ListServersOptions](true),
				service.WithCursor(createCursor(-1 * time.Hour)),
				service.WithLimit[service.ListServersOptions](20),
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, servers []*upstreamv0.ServerJSON, err error) {
				require.NoError(t, err)
				require.NotEmpty(t, servers)

				// All server names should be prefixed with "test-registry"
				for _, server := range servers {
					require.True(t,
						len(server.Name) > len("test-registry.") &&
							server.Name[:14] == "test-registry.",
						"server name should be prefixed with 'test-registry.': %s", server.Name)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc, cleanup := setupTestService(t)
			defer cleanup()

			tt.setupFunc(t, svc.pool)

			servers, err := svc.ListServers(context.Background(), tt.options...)
			tt.validateFunc(t, servers, err)
		})
	}
}

func TestListServers_WithoutPrefixNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		setupFunc    func(*testing.T, *pgxpool.Pool)
		options      []service.Option[service.ListServersOptions]
		validateFunc func(*testing.T, []*upstreamv0.ServerJSON, error)
	}{
		{
			name: "registry-specific query returns unprefixed server names",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupMultiRegistryTestData(t, pool)
			},
			options: []service.Option[service.ListServersOptions]{
				service.WithRegistryName[service.ListServersOptions]("registry-alpha"),
				service.WithPrefixNames[service.ListServersOptions](false),
				service.WithCursor(createCursor(-1 * time.Hour)),
				service.WithLimit[service.ListServersOptions](20),
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, servers []*upstreamv0.ServerJSON, err error) {
				require.NoError(t, err)
				require.NotEmpty(t, servers)

				// Server names should NOT have registry prefix
				serverNames := make([]string, len(servers))
				for i, s := range servers {
					serverNames[i] = s.Name
				}

				// Should contain original names without prefix
				require.Contains(t, serverNames, "shared-server")
				require.Contains(t, serverNames, "unique-alpha-server")

				// Should NOT contain prefixed names
				for _, name := range serverNames {
					require.NotContains(t, name, "registry-alpha.",
						"server name should not be prefixed: %s", name)
					require.NotContains(t, name, "registry-beta.",
						"server name should not be prefixed: %s", name)
				}
			},
		},
		{
			name: "default behavior without PrefixNames option returns unprefixed names",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupTestData(t, pool)
			},
			options: []service.Option[service.ListServersOptions]{
				service.WithRegistryName[service.ListServersOptions]("test-registry"),
				// Note: PrefixNames defaults to false
				service.WithCursor(createCursor(-1 * time.Hour)),
				service.WithLimit[service.ListServersOptions](20),
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, servers []*upstreamv0.ServerJSON, err error) {
				require.NoError(t, err)
				require.NotEmpty(t, servers)

				// Server names should be unprefixed
				serverNames := make([]string, len(servers))
				for i, s := range servers {
					serverNames[i] = s.Name
				}
				require.Contains(t, serverNames, "test-server-1")
				require.Contains(t, serverNames, "test-server-2")
			},
		},
		{
			name: "explicit PrefixNames=false returns unprefixed names",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupTestData(t, pool)
			},
			options: []service.Option[service.ListServersOptions]{
				service.WithRegistryName[service.ListServersOptions]("test-registry"),
				service.WithPrefixNames[service.ListServersOptions](false),
				service.WithCursor(createCursor(-1 * time.Hour)),
				service.WithLimit[service.ListServersOptions](20),
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, servers []*upstreamv0.ServerJSON, err error) {
				require.NoError(t, err)
				require.NotEmpty(t, servers)

				// Server names should be unprefixed (original names)
				for _, server := range servers {
					require.NotContains(t, server.Name, "test-registry.",
						"server name should not be prefixed: %s", server.Name)
				}
			},
		},
		{
			name: "non-existent registry returns empty list",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupMultiRegistryTestData(t, pool)
			},
			options: []service.Option[service.ListServersOptions]{
				service.WithRegistryName[service.ListServersOptions]("nonexistent-registry"),
				service.WithPrefixNames[service.ListServersOptions](false),
				service.WithCursor(createCursor(-1 * time.Hour)),
				service.WithLimit[service.ListServersOptions](20),
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, servers []*upstreamv0.ServerJSON, err error) {
				require.NoError(t, err)
				require.Empty(t, servers)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc, cleanup := setupTestService(t)
			defer cleanup()

			tt.setupFunc(t, svc.pool)

			servers, err := svc.ListServers(context.Background(), tt.options...)
			tt.validateFunc(t, servers, err)
		})
	}
}

func TestListServerVersions_WithPrefixNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		setupFunc    func(*testing.T, *pgxpool.Pool)
		options      []service.Option[service.ListServerVersionsOptions]
		validateFunc func(*testing.T, []*upstreamv0.ServerJSON, error)
	}{
		{
			name: "aggregated query with prefixed name searches and returns prefixed results",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupMultiRegistryTestData(t, pool)
			},
			options: []service.Option[service.ListServerVersionsOptions]{
				// Use prefixed name for aggregated query
				service.WithName[service.ListServerVersionsOptions]("registry-alpha.shared-server"),
				service.WithPrefixNames[service.ListServerVersionsOptions](true),
				service.WithLimit[service.ListServerVersionsOptions](20),
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, servers []*upstreamv0.ServerJSON, err error) {
				require.NoError(t, err)
				require.NotEmpty(t, servers)

				// All returned server names should be prefixed
				for _, server := range servers {
					require.Equal(t, "registry-alpha.shared-server", server.Name,
						"server name should match prefixed query name")
				}

				// Should return both versions from registry-alpha
				versions := make([]string, len(servers))
				for i, s := range servers {
					versions[i] = s.Version
				}
				require.Contains(t, versions, "1.0.0")
				require.Contains(t, versions, "2.0.0")
			},
		},
		{
			name: "prefixed query for unique server returns prefixed name",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupMultiRegistryTestData(t, pool)
			},
			options: []service.Option[service.ListServerVersionsOptions]{
				service.WithName[service.ListServerVersionsOptions]("registry-beta.unique-beta-server"),
				service.WithPrefixNames[service.ListServerVersionsOptions](true),
				service.WithLimit[service.ListServerVersionsOptions](20),
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, servers []*upstreamv0.ServerJSON, err error) {
				require.NoError(t, err)
				require.Len(t, servers, 1)
				require.Equal(t, "registry-beta.unique-beta-server", servers[0].Name)
				require.Equal(t, "1.0.0", servers[0].Version)
			},
		},
		{
			name: "prefixed query for non-existent server returns empty",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupMultiRegistryTestData(t, pool)
			},
			options: []service.Option[service.ListServerVersionsOptions]{
				service.WithName[service.ListServerVersionsOptions]("registry-alpha.nonexistent-server"),
				service.WithPrefixNames[service.ListServerVersionsOptions](true),
				service.WithLimit[service.ListServerVersionsOptions](20),
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, servers []*upstreamv0.ServerJSON, err error) {
				require.NoError(t, err)
				require.Empty(t, servers)
			},
		},
		{
			name: "prefixed query with wrong registry prefix returns empty",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupMultiRegistryTestData(t, pool)
			},
			options: []service.Option[service.ListServerVersionsOptions]{
				// unique-alpha-server only exists in registry-alpha, not registry-beta
				service.WithName[service.ListServerVersionsOptions]("registry-beta.unique-alpha-server"),
				service.WithPrefixNames[service.ListServerVersionsOptions](true),
				service.WithLimit[service.ListServerVersionsOptions](20),
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, servers []*upstreamv0.ServerJSON, err error) {
				require.NoError(t, err)
				require.Empty(t, servers)
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
			tt.validateFunc(t, servers, err)
		})
	}
}

func TestListServerVersions_WithoutPrefixNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		setupFunc    func(*testing.T, *pgxpool.Pool)
		options      []service.Option[service.ListServerVersionsOptions]
		validateFunc func(*testing.T, []*upstreamv0.ServerJSON, error)
	}{
		{
			name: "registry-specific query uses name and registry_name params",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupMultiRegistryTestData(t, pool)
			},
			options: []service.Option[service.ListServerVersionsOptions]{
				service.WithName[service.ListServerVersionsOptions]("shared-server"),
				service.WithRegistryName[service.ListServerVersionsOptions]("registry-alpha"),
				service.WithPrefixNames[service.ListServerVersionsOptions](false),
				service.WithLimit[service.ListServerVersionsOptions](20),
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, servers []*upstreamv0.ServerJSON, err error) {
				require.NoError(t, err)
				require.NotEmpty(t, servers)

				// Server names should NOT be prefixed
				for _, server := range servers {
					require.Equal(t, "shared-server", server.Name,
						"server name should remain unprefixed")
				}

				// Should return versions from registry-alpha only
				versions := make([]string, len(servers))
				for i, s := range servers {
					versions[i] = s.Version
				}
				require.Contains(t, versions, "1.0.0")
				require.Contains(t, versions, "2.0.0")
				require.Len(t, servers, 2)
			},
		},
		{
			name: "registry-specific query returns only servers from specified registry",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupMultiRegistryTestData(t, pool)
			},
			options: []service.Option[service.ListServerVersionsOptions]{
				service.WithName[service.ListServerVersionsOptions]("shared-server"),
				service.WithRegistryName[service.ListServerVersionsOptions]("registry-beta"),
				service.WithPrefixNames[service.ListServerVersionsOptions](false),
				service.WithLimit[service.ListServerVersionsOptions](20),
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, servers []*upstreamv0.ServerJSON, err error) {
				require.NoError(t, err)
				require.Len(t, servers, 1)

				// Server name should NOT be prefixed
				require.Equal(t, "shared-server", servers[0].Name)
				require.Equal(t, "1.0.0", servers[0].Version)
			},
		},
		{
			name: "default PrefixNames (false) returns unprefixed names",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupTestData(t, pool)
			},
			options: []service.Option[service.ListServerVersionsOptions]{
				service.WithName[service.ListServerVersionsOptions]("test-server-1"),
				// No PrefixNames option - defaults to false
				service.WithLimit[service.ListServerVersionsOptions](20),
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, servers []*upstreamv0.ServerJSON, err error) {
				require.NoError(t, err)
				require.Len(t, servers, 3)

				// All should have unprefixed name
				for _, server := range servers {
					require.Equal(t, "test-server-1", server.Name)
				}
			},
		},
		{
			name: "non-existent server in specific registry returns empty",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupMultiRegistryTestData(t, pool)
			},
			options: []service.Option[service.ListServerVersionsOptions]{
				// unique-alpha-server doesn't exist in registry-beta
				service.WithName[service.ListServerVersionsOptions]("unique-alpha-server"),
				service.WithRegistryName[service.ListServerVersionsOptions]("registry-beta"),
				service.WithPrefixNames[service.ListServerVersionsOptions](false),
				service.WithLimit[service.ListServerVersionsOptions](20),
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, servers []*upstreamv0.ServerJSON, err error) {
				require.NoError(t, err)
				require.Empty(t, servers)
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
			tt.validateFunc(t, servers, err)
		})
	}
}

func TestGetServerVersion_WithPrefixNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		setupFunc    func(*testing.T, *pgxpool.Pool)
		options      []service.Option[service.GetServerVersionOptions]
		validateFunc func(*testing.T, *upstreamv0.ServerJSON, error)
	}{
		{
			name: "aggregated query with prefixed name returns prefixed result",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupMultiRegistryTestData(t, pool)
			},
			options: []service.Option[service.GetServerVersionOptions]{
				service.WithName[service.GetServerVersionOptions]("registry-alpha.shared-server"),
				service.WithVersion[service.GetServerVersionOptions]("1.0.0"),
				service.WithPrefixNames[service.GetServerVersionOptions](true),
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, server *upstreamv0.ServerJSON, err error) {
				require.NoError(t, err)
				require.NotNil(t, server)
				require.Equal(t, "registry-alpha.shared-server", server.Name)
				require.Equal(t, "1.0.0", server.Version)
				require.Equal(t, "Server in registry-alpha", server.Description)
			},
		},
		{
			name: "aggregated query distinguishes servers with same name in different registries",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupMultiRegistryTestData(t, pool)
			},
			options: []service.Option[service.GetServerVersionOptions]{
				service.WithName[service.GetServerVersionOptions]("registry-beta.shared-server"),
				service.WithVersion[service.GetServerVersionOptions]("1.0.0"),
				service.WithPrefixNames[service.GetServerVersionOptions](true),
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, server *upstreamv0.ServerJSON, err error) {
				require.NoError(t, err)
				require.NotNil(t, server)
				require.Equal(t, "registry-beta.shared-server", server.Name)
				require.Equal(t, "1.0.0", server.Version)
				// This should be from registry-beta, not registry-alpha
				require.Equal(t, "Server in registry-beta", server.Description)
			},
		},
		{
			name: "prefixed query for unique server returns correct result",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupMultiRegistryTestData(t, pool)
			},
			options: []service.Option[service.GetServerVersionOptions]{
				service.WithName[service.GetServerVersionOptions]("registry-alpha.unique-alpha-server"),
				service.WithVersion[service.GetServerVersionOptions]("1.0.0"),
				service.WithPrefixNames[service.GetServerVersionOptions](true),
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, server *upstreamv0.ServerJSON, err error) {
				require.NoError(t, err)
				require.NotNil(t, server)
				require.Equal(t, "registry-alpha.unique-alpha-server", server.Name)
				require.Equal(t, "1.0.0", server.Version)
			},
		},
		{
			name: "prefixed query for non-existent server returns error",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupMultiRegistryTestData(t, pool)
			},
			options: []service.Option[service.GetServerVersionOptions]{
				service.WithName[service.GetServerVersionOptions]("registry-alpha.nonexistent"),
				service.WithVersion[service.GetServerVersionOptions]("1.0.0"),
				service.WithPrefixNames[service.GetServerVersionOptions](true),
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, server *upstreamv0.ServerJSON, err error) {
				require.Error(t, err)
				require.Nil(t, server)
			},
		},
		{
			name: "prefixed query with wrong registry prefix returns error",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupMultiRegistryTestData(t, pool)
			},
			options: []service.Option[service.GetServerVersionOptions]{
				// unique-alpha-server only exists in registry-alpha
				service.WithName[service.GetServerVersionOptions]("registry-beta.unique-alpha-server"),
				service.WithVersion[service.GetServerVersionOptions]("1.0.0"),
				service.WithPrefixNames[service.GetServerVersionOptions](true),
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, server *upstreamv0.ServerJSON, err error) {
				require.Error(t, err)
				require.Nil(t, server)
			},
		},
		{
			name: "prefixed query for non-existent version returns error",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupMultiRegistryTestData(t, pool)
			},
			options: []service.Option[service.GetServerVersionOptions]{
				service.WithName[service.GetServerVersionOptions]("registry-alpha.shared-server"),
				service.WithVersion[service.GetServerVersionOptions]("999.999.999"),
				service.WithPrefixNames[service.GetServerVersionOptions](true),
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, server *upstreamv0.ServerJSON, err error) {
				require.Error(t, err)
				require.Nil(t, server)
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
			tt.validateFunc(t, server, err)
		})
	}
}

func TestGetServerVersion_WithoutPrefixNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		setupFunc    func(*testing.T, *pgxpool.Pool)
		options      []service.Option[service.GetServerVersionOptions]
		validateFunc func(*testing.T, *upstreamv0.ServerJSON, error)
	}{
		{
			name: "registry-specific query uses name and registry_name params",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupMultiRegistryTestData(t, pool)
			},
			options: []service.Option[service.GetServerVersionOptions]{
				service.WithName[service.GetServerVersionOptions]("shared-server"),
				service.WithVersion[service.GetServerVersionOptions]("1.0.0"),
				service.WithRegistryName[service.GetServerVersionOptions]("registry-alpha"),
				service.WithPrefixNames[service.GetServerVersionOptions](false),
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, server *upstreamv0.ServerJSON, err error) {
				require.NoError(t, err)
				require.NotNil(t, server)
				// Server name should NOT be prefixed
				require.Equal(t, "shared-server", server.Name)
				require.Equal(t, "1.0.0", server.Version)
				require.Equal(t, "Server in registry-alpha", server.Description)
			},
		},
		{
			name: "registry-specific query returns server from correct registry",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupMultiRegistryTestData(t, pool)
			},
			options: []service.Option[service.GetServerVersionOptions]{
				service.WithName[service.GetServerVersionOptions]("shared-server"),
				service.WithVersion[service.GetServerVersionOptions]("1.0.0"),
				service.WithRegistryName[service.GetServerVersionOptions]("registry-beta"),
				service.WithPrefixNames[service.GetServerVersionOptions](false),
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, server *upstreamv0.ServerJSON, err error) {
				require.NoError(t, err)
				require.NotNil(t, server)
				// Server name should NOT be prefixed
				require.Equal(t, "shared-server", server.Name)
				require.Equal(t, "1.0.0", server.Version)
				// Should be from registry-beta
				require.Equal(t, "Server in registry-beta", server.Description)
			},
		},
		{
			name: "default PrefixNames (false) returns unprefixed name",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupTestData(t, pool)
			},
			options: []service.Option[service.GetServerVersionOptions]{
				service.WithName[service.GetServerVersionOptions]("test-server-1"),
				service.WithVersion[service.GetServerVersionOptions]("1.0.0"),
				// No PrefixNames option - defaults to false
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, server *upstreamv0.ServerJSON, err error) {
				require.NoError(t, err)
				require.NotNil(t, server)
				require.Equal(t, "test-server-1", server.Name)
				require.Equal(t, "1.0.0", server.Version)
			},
		},
		{
			name: "server not found in specific registry returns error",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupMultiRegistryTestData(t, pool)
			},
			options: []service.Option[service.GetServerVersionOptions]{
				// unique-alpha-server doesn't exist in registry-beta
				service.WithName[service.GetServerVersionOptions]("unique-alpha-server"),
				service.WithVersion[service.GetServerVersionOptions]("1.0.0"),
				service.WithRegistryName[service.GetServerVersionOptions]("registry-beta"),
				service.WithPrefixNames[service.GetServerVersionOptions](false),
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, server *upstreamv0.ServerJSON, err error) {
				require.Error(t, err)
				require.Nil(t, server)
			},
		},
		{
			name: "version not found in specific registry returns error",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupMultiRegistryTestData(t, pool)
			},
			options: []service.Option[service.GetServerVersionOptions]{
				service.WithName[service.GetServerVersionOptions]("shared-server"),
				service.WithVersion[service.GetServerVersionOptions]("999.999.999"),
				service.WithRegistryName[service.GetServerVersionOptions]("registry-alpha"),
				service.WithPrefixNames[service.GetServerVersionOptions](false),
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, server *upstreamv0.ServerJSON, err error) {
				require.Error(t, err)
				require.Nil(t, server)
			},
		},
		{
			name: "non-existent registry returns error",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupMultiRegistryTestData(t, pool)
			},
			options: []service.Option[service.GetServerVersionOptions]{
				service.WithName[service.GetServerVersionOptions]("shared-server"),
				service.WithVersion[service.GetServerVersionOptions]("1.0.0"),
				service.WithRegistryName[service.GetServerVersionOptions]("nonexistent-registry"),
				service.WithPrefixNames[service.GetServerVersionOptions](false),
			},
			//nolint:thelper // We want to see these lines in the test output
			validateFunc: func(t *testing.T, server *upstreamv0.ServerJSON, err error) {
				require.Error(t, err)
				require.Nil(t, server)
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
			tt.validateFunc(t, server, err)
		})
	}
}

func TestPublishServerVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		setupFunc    func(*testing.T, *pgxpool.Pool) *sqlc.Registry
		serverData   *upstreamv0.ServerJSON
		registryName string
		validateFunc func(*testing.T, *upstreamv0.ServerJSON, error)
	}{
		{
			name: "success - publish new server version",
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) *sqlc.Registry {
				t.Helper()
				ctx := context.Background()
				queries := sqlc.New(pool)

				// Create a MANAGED registry
				regID, err := queries.InsertRegistry(ctx, sqlc.InsertRegistryParams{
					Name:    "test-registry",
					RegType: sqlc.RegistryTypeMANAGED,
				})
				require.NoError(t, err)

				reg, err := queries.GetRegistry(ctx, regID)
				require.NoError(t, err)
				return &reg
			},
			serverData: &upstreamv0.ServerJSON{
				Name:        "test-server",
				Version:     "1.0.0",
				Description: "Test server description",
				Title:       "Test Server",
			},
			registryName: "test-registry",
			validateFunc: func(t *testing.T, result *upstreamv0.ServerJSON, err error) {
				t.Helper()
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Equal(t, "test-server", result.Name)
				require.Equal(t, "1.0.0", result.Version)
				require.Equal(t, "Test server description", result.Description)
				require.Equal(t, "Test Server", result.Title)
			},
		},
		{
			name: "success - publish with metadata and repository",
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) *sqlc.Registry {
				t.Helper()
				ctx := context.Background()
				queries := sqlc.New(pool)

				regID, err := queries.InsertRegistry(ctx, sqlc.InsertRegistryParams{
					Name:    "test-registry-meta",
					RegType: sqlc.RegistryTypeMANAGED,
				})
				require.NoError(t, err)

				reg, err := queries.GetRegistry(ctx, regID)
				require.NoError(t, err)
				return &reg
			},
			serverData: &upstreamv0.ServerJSON{
				Name:        "server-with-meta",
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
					PublisherProvided: map[string]interface{}{
						"custom_field": "custom_value",
					},
				},
			},
			registryName: "test-registry-meta",
			validateFunc: func(t *testing.T, result *upstreamv0.ServerJSON, err error) {
				t.Helper()
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Equal(t, "server-with-meta", result.Name)
				require.Equal(t, "2.0.0", result.Version)
				require.NotNil(t, result.Repository)
				require.Equal(t, "https://github.com/example/server", result.Repository.URL)
			},
		},
		{
			name: "success - publish with packages and remotes",
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) *sqlc.Registry {
				t.Helper()
				ctx := context.Background()
				queries := sqlc.New(pool)

				regID, err := queries.InsertRegistry(ctx, sqlc.InsertRegistryParams{
					Name:    "test-registry-full",
					RegType: sqlc.RegistryTypeMANAGED,
				})
				require.NoError(t, err)

				reg, err := queries.GetRegistry(ctx, regID)
				require.NoError(t, err)
				return &reg
			},
			serverData: &upstreamv0.ServerJSON{
				Name:        "server-with-packages-remotes",
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
			registryName: "test-registry-full",
			validateFunc: func(t *testing.T, result *upstreamv0.ServerJSON, err error) {
				t.Helper()
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Equal(t, "server-with-packages-remotes", result.Name)
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
			name: "failure - registry not found",
			setupFunc: func(t *testing.T, _ *pgxpool.Pool) *sqlc.Registry {
				t.Helper()
				return nil
			},
			serverData: &upstreamv0.ServerJSON{
				Name:        "test-server",
				Version:     "1.0.0",
				Description: "Test",
			},
			registryName: "nonexistent-registry",
			validateFunc: func(t *testing.T, result *upstreamv0.ServerJSON, err error) {
				t.Helper()
				require.Error(t, err)
				require.Nil(t, result)
				require.ErrorIs(t, err, service.ErrRegistryNotFound)
			},
		},
		{
			name: "failure - not a managed registry",
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) *sqlc.Registry {
				t.Helper()
				ctx := context.Background()
				queries := sqlc.New(pool)

				// Create a REMOTE (non-managed) registry
				regID, err := queries.InsertRegistry(ctx, sqlc.InsertRegistryParams{
					Name:    "remote-registry",
					RegType: sqlc.RegistryTypeREMOTE,
				})
				require.NoError(t, err)

				reg, err := queries.GetRegistry(ctx, regID)
				require.NoError(t, err)
				return &reg
			},
			serverData: &upstreamv0.ServerJSON{
				Name:        "test-server",
				Version:     "1.0.0",
				Description: "Test",
			},
			registryName: "remote-registry",
			validateFunc: func(t *testing.T, result *upstreamv0.ServerJSON, err error) {
				t.Helper()
				require.Error(t, err)
				require.Nil(t, result)
				require.ErrorIs(t, err, service.ErrNotManagedRegistry)
			},
		},
		{
			name: "failure - version already exists",
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) *sqlc.Registry {
				t.Helper()
				ctx := context.Background()
				queries := sqlc.New(pool)

				// Create a MANAGED registry
				regID, err := queries.InsertRegistry(ctx, sqlc.InsertRegistryParams{
					Name:    "test-registry-dup",
					RegType: sqlc.RegistryTypeMANAGED,
				})
				require.NoError(t, err)

				// Insert a server version
				now := time.Now()
				_, err = queries.InsertServerVersion(ctx, sqlc.InsertServerVersionParams{
					Name:        "existing-server",
					Version:     "1.0.0",
					RegID:       regID,
					CreatedAt:   &now,
					UpdatedAt:   &now,
					Description: ptr.String("Existing"),
				})
				require.NoError(t, err)

				reg, err := queries.GetRegistry(ctx, regID)
				require.NoError(t, err)
				return &reg
			},
			serverData: &upstreamv0.ServerJSON{
				Name:        "existing-server",
				Version:     "1.0.0",
				Description: "Duplicate",
			},
			registryName: "test-registry-dup",
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
				service.WithRegistryName[service.PublishServerVersionOptions](tt.registryName),
				service.WithServerData(tt.serverData),
			)

			tt.validateFunc(t, result, err)
		})
	}
}
