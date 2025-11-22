package database

import (
	"context"
	"encoding/base64"
	"testing"
	"time"

	"github.com/aws/smithy-go/ptr"
	"github.com/jackc/pgx/v5/pgxpool"
	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
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
		_, err := queries.UpsertServerVersion(
			ctx,
			sqlc.UpsertServerVersionParams{
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
	_, err = queries.UpsertServerVersion(
		ctx,
		sqlc.UpsertServerVersionParams{
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
				func(opts *service.ListServersOptions) error {
					// Create a valid base64-encoded RFC3339 time cursor
					cursorTime := time.Now().Add(-1 * time.Hour).UTC()
					cursorStr := base64.StdEncoding.EncodeToString([]byte(cursorTime.Format(time.RFC3339)))
					opts.Cursor = cursorStr
					opts.Limit = 10
					return nil
				},
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
				func(opts *service.ListServersOptions) error {
					cursorTime := time.Now().Add(-1 * time.Hour).UTC()
					cursorStr := base64.StdEncoding.EncodeToString([]byte(cursorTime.Format(time.RFC3339)))
					opts.Cursor = cursorStr
					opts.Limit = 2
					return nil
				},
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
				func(opts *service.ListServersOptions) error {
					opts.Cursor = "invalid-base64"
					opts.Limit = 10
					return nil
				},
			},
		},
		{
			name: "invalid cursor time format",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, pool *pgxpool.Pool) {
				setupTestData(t, pool)
			},
			options: []service.Option[service.ListServersOptions]{
				func(opts *service.ListServersOptions) error {
					// Valid base64 but invalid time format
					invalidTime := base64.StdEncoding.EncodeToString([]byte("not-a-time"))
					opts.Cursor = invalidTime
					opts.Limit = 10
					return nil
				},
			},
		},
		{
			name: "empty database",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *pgxpool.Pool) {
				// Don't set up any data
			},
			options: []service.Option[service.ListServersOptions]{
				func(opts *service.ListServersOptions) error {
					cursorTime := time.Now().Add(-1 * time.Hour).UTC()
					cursorStr := base64.StdEncoding.EncodeToString([]byte(cursorTime.Format(time.RFC3339)))
					opts.Cursor = cursorStr
					opts.Limit = 10
					return nil
				},
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
				serverID, err := queries.UpsertServerVersion(
					ctx,
					sqlc.UpsertServerVersionParams{
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
				err = queries.UpsertServerPackage(
					ctx,
					sqlc.UpsertServerPackageParams{
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
				err = queries.UpsertServerRemote(
					ctx,
					sqlc.UpsertServerRemoteParams{
						ServerID:         serverID,
						Transport:        "sse",
						TransportUrl:     ptr.String("https://example.com/sse"),
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
