package sqlc

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	_ "github.com/lib/pq" // Register postgres driver
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/database"
)

//nolint:thelper // We want to see these lines in the test output
func setupRegistry(t *testing.T, queries *Queries) pgtype.UUID {
	regID, err := queries.InsertRegistry(
		context.Background(),
		InsertRegistryParams{
			Name:    "test-registry",
			RegType: RegistryTypeREMOTE,
		},
	)
	require.NoError(t, err)
	return regID
}

func TestUpsertServerVersion(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		setupFunc    func(t *testing.T, queries *Queries, regID pgtype.UUID)
		scenarioFunc func(t *testing.T, queries *Queries, regID pgtype.UUID)
	}{
		{
			name: "insert server version with minimal fields",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries, _ pgtype.UUID) {},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, regID pgtype.UUID) {
				_, err := queries.UpsertServerVersion(
					context.Background(),
					UpsertServerVersionParams{
						Name:    "test-server",
						Version: "1.0.0",
						RegID:   regID,
					},
				)
				require.NoError(t, err)
			},
		},
		{
			name: "insert server version with all fields",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries, _ pgtype.UUID) {},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, regID pgtype.UUID) {
				_, err := queries.UpsertServerVersion(
					context.Background(),
					UpsertServerVersionParams{
						Name:                "test-server",
						Version:             "1.0.0",
						RegID:               regID,
						Description:         pgtype.Text{String: "Test description", Valid: true},
						Title:               pgtype.Text{String: "Test Title", Valid: true},
						Website:             pgtype.Text{String: "https://example.com", Valid: true},
						UpstreamMeta:        []byte(`{"key": "value"}`),
						ServerMeta:          []byte(`{"meta": "data"}`),
						RepositoryUrl:       pgtype.Text{String: "https://github.com/test/repo", Valid: true},
						RepositoryID:        pgtype.Text{String: "repo-id", Valid: true},
						RepositorySubfolder: pgtype.Text{String: "subfolder", Valid: true},
						RepositoryType:      pgtype.Text{String: "git", Valid: true},
					},
				)
				require.NoError(t, err)
			},
		},
		{
			name: "update existing server version",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID pgtype.UUID) {
				_, err := queries.UpsertServerVersion(
					context.Background(),
					UpsertServerVersionParams{
						Name:    "test-server",
						Version: "1.0.0",
						RegID:   regID,
					},
				)
				require.NoError(t, err)
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, regID pgtype.UUID) {
				_, err := queries.UpsertServerVersion(
					context.Background(),
					UpsertServerVersionParams{
						Name:        "test-server",
						Version:     "1.0.0",
						RegID:       regID,
						Description: pgtype.Text{String: "Updated description", Valid: true},
						Title:       pgtype.Text{String: "Updated Title", Valid: true},
					},
				)
				require.NoError(t, err)
			},
		},
		{
			name: "insert server version with invalid reg_id",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries, _ pgtype.UUID) {},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, _ pgtype.UUID) {
				regID := pgtype.UUID{Bytes: uuid.New(), Valid: true}
				_, err := queries.UpsertServerVersion(
					context.Background(),
					UpsertServerVersionParams{
						Name:    "test-server",
						Version: "1.0.0",
						RegID:   regID,
					},
				)
				require.Error(t, err)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			db, cleanupFunc := database.SetupTestDB(t)
			t.Cleanup(cleanupFunc)
			queries := New(db)
			require.NotNil(t, queries)

			regID := setupRegistry(t, queries)
			require.NotNil(t, regID)

			tc.setupFunc(t, queries, regID)
			tc.scenarioFunc(t, queries, regID)
		})
	}
}

func TestListServerVersions(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		setupFunc    func(t *testing.T, queries *Queries, regID pgtype.UUID) string
		scenarioFunc func(t *testing.T, queries *Queries, serverName string)
	}{
		{
			name: "no server versions",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries, _ pgtype.UUID) string {
				return "non-existent-server"
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, serverName string) {
				versions, err := queries.ListServerVersions(
					context.Background(),
					ListServerVersionsParams{
						Name: serverName,
						Size: 10,
					},
				)
				require.NoError(t, err)
				require.Empty(t, versions)
			},
		},
		{
			name: "list single server version",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID pgtype.UUID) string {
				_, err := queries.UpsertServerVersion(
					context.Background(),
					UpsertServerVersionParams{
						Name:    "test-server",
						Version: "1.0.0",
						RegID:   regID,
					},
				)
				require.NoError(t, err)
				//nolint:goconst
				return "test-server"
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, serverName string) {
				versions, err := queries.ListServerVersions(
					context.Background(),
					ListServerVersionsParams{
						Name: serverName,
						Size: 10,
					},
				)
				require.NoError(t, err)
				require.Len(t, versions, 1)
				require.Equal(t, serverName, versions[0].Name)
				require.Equal(t, "1.0.0", versions[0].Version)
			},
		},
		{
			name: "list multiple server versions",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID pgtype.UUID) string {
				for _, version := range []string{"1.0.0", "2.0.0", "3.0.0"} {
					_, err := queries.UpsertServerVersion(
						context.Background(),
						UpsertServerVersionParams{
							Name:    "test-server",
							Version: version,
							RegID:   regID,
						},
					)
					require.NoError(t, err)
				}
				return "test-server"
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, serverName string) {
				versions, err := queries.ListServerVersions(
					context.Background(),
					ListServerVersionsParams{
						Name: serverName,
						Size: 10,
					},
				)
				require.NoError(t, err)
				require.Len(t, versions, 3)
				require.Equal(t, serverName, versions[0].Name)
			},
		},
		{
			name: "list server versions with pagination",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID pgtype.UUID) string {
				for _, version := range []string{"1.0.0", "2.0.0", "3.0.0"} {
					_, err := queries.UpsertServerVersion(
						context.Background(),
						UpsertServerVersionParams{
							Name:    "test-server",
							Version: version,
							RegID:   regID,
						},
					)
					require.NoError(t, err)
				}
				return "test-server"
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, serverName string) {
				// Get first page
				versions, err := queries.ListServerVersions(
					context.Background(),
					ListServerVersionsParams{
						Name: serverName,
						Size: 10,
					},
				)
				require.NoError(t, err)
				require.Len(t, versions, 3)

				// Get next page
				nextTime := versions[0].CreatedAt.Time.UTC()
				for _, version := range versions {
					if nextTime.Before(version.CreatedAt.Time.UTC()) {
						nextTime = version.CreatedAt.Time.UTC()
					}
				}
				nextTime = nextTime.Add(-100 * time.Microsecond)

				nextVersions, err := queries.ListServerVersions(
					context.Background(),
					ListServerVersionsParams{
						Name: serverName,
						Next: pgtype.Timestamptz{
							Time:  nextTime,
							Valid: true,
						},
						Size: 10,
					},
				)
				require.NoError(t, err)
				assert.Len(t, nextVersions, 1)
			},
		},
		{
			name: "list server versions with limit",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID pgtype.UUID) string {
				for _, version := range []string{"1.0.0", "2.0.0", "3.0.0"} {
					_, err := queries.UpsertServerVersion(
						context.Background(),
						UpsertServerVersionParams{
							Name:    "test-server",
							Version: version,
							RegID:   regID,
						},
					)
					require.NoError(t, err)
				}
				return "test-server"
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, serverName string) {
				versions, err := queries.ListServerVersions(
					context.Background(),
					ListServerVersionsParams{
						Name: serverName,
						Size: 2,
					},
				)
				require.NoError(t, err)
				require.Len(t, versions, 2)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			db, cleanupFunc := database.SetupTestDB(t)
			t.Cleanup(cleanupFunc)
			queries := New(db)
			require.NotNil(t, queries)

			regID := setupRegistry(t, queries)
			require.NotNil(t, regID)

			serverName := tc.setupFunc(t, queries, regID)
			tc.scenarioFunc(t, queries, serverName)
		})
	}
}

func TestListServers(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		setupFunc    func(t *testing.T, queries *Queries, regID pgtype.UUID)
		scenarioFunc func(t *testing.T, queries *Queries)
	}{
		{
			name: "no servers",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries, _ pgtype.UUID) {},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries) {
				servers, err := queries.ListServers(
					context.Background(),
					ListServersParams{
						Size: 10,
					},
				)
				require.NoError(t, err)
				require.Empty(t, servers)
			},
		},
		{
			name: "list single server",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID pgtype.UUID) {
				_, err := queries.UpsertServerVersion(
					context.Background(),
					UpsertServerVersionParams{
						Name:    "test-server",
						Version: "1.0.0",
						RegID:   regID,
					},
				)
				require.NoError(t, err)
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries) {
				servers, err := queries.ListServers(
					context.Background(),
					ListServersParams{
						Size: 10,
					},
				)
				require.NoError(t, err)
				require.Len(t, servers, 1)
				require.Equal(t, "test-server", servers[0].Name)
				require.Equal(t, "1.0.0", servers[0].Version)
				require.Equal(t, RegistryTypeREMOTE, servers[0].RegistryType)
			},
		},
		{
			name: "list multiple servers",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID pgtype.UUID) {
				for _, version := range []string{"1.0.0", "2.0.0"} {
					_, err := queries.UpsertServerVersion(
						context.Background(),
						UpsertServerVersionParams{
							Name:    "test-server-%d",
							Version: version,
							RegID:   regID,
						},
					)
					require.NoError(t, err)
				}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries) {
				servers, err := queries.ListServers(
					context.Background(),
					ListServersParams{
						Size: 10,
					},
				)
				require.NoError(t, err)
				require.Len(t, servers, 2)
			},
		},
		{
			name: "list servers with pagination next",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID pgtype.UUID) {
				for _, version := range []string{"1.0.0", "2.0.0"} {
					_, err := queries.UpsertServerVersion(
						context.Background(),
						UpsertServerVersionParams{
							Name:    "test-server",
							Version: version,
							RegID:   regID,
						},
					)
					require.NoError(t, err)
				}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries) {
				nextTime := time.Now().UTC().Add(-10 * time.Minute)
				servers, err := queries.ListServers(
					context.Background(),
					ListServersParams{
						Next: pgtype.Timestamptz{
							Time:  nextTime,
							Valid: true,
						},
						Size: 10,
					},
				)
				require.NoError(t, err)
				require.NotEmpty(t, servers)
				for _, server := range servers {
					require.True(t, server.CreatedAt.Time.After(nextTime))
				}
			},
		},
		{
			name: "list servers with pagination prev",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID pgtype.UUID) {
				for _, version := range []string{"1.0.0", "2.0.0"} {
					_, err := queries.UpsertServerVersion(
						context.Background(),
						UpsertServerVersionParams{
							Name:    "test-server",
							Version: version,
							RegID:   regID,
						},
					)
					require.NoError(t, err)
				}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries) {
				prevTime := time.Now().UTC().Add(10 * time.Minute)
				servers, err := queries.ListServers(
					context.Background(),
					ListServersParams{
						Prev: pgtype.Timestamptz{
							Time:  prevTime,
							Valid: true,
						},
						Size: 10,
					},
				)
				require.NoError(t, err)
				require.NotEmpty(t, servers)
				for _, server := range servers {
					require.True(t, server.CreatedAt.Time.Before(prevTime))
				}
			},
		},
		{
			name: "list servers with is_latest flag",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID pgtype.UUID) {
				_, err := queries.UpsertServerVersion(
					context.Background(),
					UpsertServerVersionParams{
						Name:    "test-server",
						Version: "1.0.0",
						RegID:   regID,
					},
				)
				require.NoError(t, err)

				// Get the server ID by listing
				servers, err := queries.ListServers(
					context.Background(),
					ListServersParams{Size: 10},
				)
				require.NoError(t, err)
				require.Len(t, servers, 1)

				_, err = queries.UpsertLatestServerVersion(
					context.Background(),
					UpsertLatestServerVersionParams{
						RegID:    regID,
						Name:     "test-server",
						Version:  "1.0.0",
						ServerID: servers[0].ID,
					},
				)
				require.NoError(t, err)
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries) {
				servers, err := queries.ListServers(
					context.Background(),
					ListServersParams{
						Size: 10,
					},
				)
				require.NoError(t, err)
				require.Len(t, servers, 1)
			},
		},
		{
			name: "list servers with limit",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID pgtype.UUID) {
				for _, version := range []string{"1.0.0", "2.0.0"} {
					_, err := queries.UpsertServerVersion(
						context.Background(),
						UpsertServerVersionParams{
							Name:    "test-server",
							Version: version,
							RegID:   regID,
						},
					)
					require.NoError(t, err)
				}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries) {
				servers, err := queries.ListServers(
					context.Background(),
					ListServersParams{
						Size: 1,
					},
				)
				require.NoError(t, err)
				require.Len(t, servers, 1)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			db, cleanupFunc := database.SetupTestDB(t)
			t.Cleanup(cleanupFunc)
			queries := New(db)
			require.NotNil(t, queries)

			regID := setupRegistry(t, queries)
			require.NotNil(t, regID)

			tc.setupFunc(t, queries, regID)
			tc.scenarioFunc(t, queries)
		})
	}
}

func TestUpsertLatestServerVersion(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		setupFunc    func(t *testing.T, queries *Queries, regID pgtype.UUID) []pgtype.UUID
		scenarioFunc func(t *testing.T, queries *Queries, regID pgtype.UUID, ids []pgtype.UUID)
	}{
		{
			name: "insert latest server version",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID pgtype.UUID) []pgtype.UUID {
				serverID, err := queries.UpsertServerVersion(
					context.Background(),
					UpsertServerVersionParams{
						Name:    "test-server",
						Version: "1.0.0",
						RegID:   regID,
					},
				)
				require.NoError(t, err)
				require.NotNil(t, serverID)
				return []pgtype.UUID{serverID}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, regID pgtype.UUID, ids []pgtype.UUID) {
				serverID, err := queries.UpsertLatestServerVersion(
					context.Background(),
					UpsertLatestServerVersionParams{
						RegID:    regID,
						Name:     "test-server",
						Version:  "1.0.0",
						ServerID: ids[0],
					},
				)
				require.NoError(t, err)
				require.NotNil(t, serverID)
				require.Equal(t, ids[0], serverID)
			},
		},
		{
			name: "update existing latest server version",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID pgtype.UUID) []pgtype.UUID {
				var serverIDs []pgtype.UUID
				for _, version := range []string{"1.0.0", "2.0.0"} {
					serverID, err := queries.UpsertServerVersion(
						context.Background(),
						UpsertServerVersionParams{
							Name:    "test-server",
							Version: version,
							RegID:   regID,
						},
					)
					require.NoError(t, err)
					require.NotNil(t, serverID)
					serverIDs = append(serverIDs, serverID)
				}

				// Set initial latest version
				latestServerID, err := queries.UpsertLatestServerVersion(
					context.Background(),
					UpsertLatestServerVersionParams{
						RegID:    regID,
						Name:     "test-server",
						Version:  "1.0.0",
						ServerID: serverIDs[0],
					},
				)
				require.NoError(t, err)
				require.NotNil(t, latestServerID)
				require.Equal(t, serverIDs[0], latestServerID)

				return serverIDs
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, regID pgtype.UUID, ids []pgtype.UUID) {
				latestServerID, err := queries.UpsertLatestServerVersion(
					context.Background(),
					UpsertLatestServerVersionParams{
						RegID:    regID,
						Name:     "test-server",
						Version:  "2.0.0",
						ServerID: ids[0],
					},
				)
				require.NoError(t, err)
				require.NotNil(t, latestServerID)
				require.Equal(t, ids[0], latestServerID)
			},
		},
		{
			name: "upsert latest server version with invalid reg_id",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries, _ pgtype.UUID) []pgtype.UUID {
				return []pgtype.UUID{{Bytes: uuid.New(), Valid: true}}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, _ pgtype.UUID, ids []pgtype.UUID) {
				regID := pgtype.UUID{Bytes: uuid.New(), Valid: true}
				_, err := queries.UpsertLatestServerVersion(
					context.Background(),
					UpsertLatestServerVersionParams{
						RegID:    regID,
						Name:     "test-server",
						Version:  "1.0.0",
						ServerID: ids[0],
					},
				)
				require.Error(t, err)
			},
		},
		{
			name: "upsert latest server version with invalid server_id",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries, _ pgtype.UUID) []pgtype.UUID {
				return []pgtype.UUID{{Bytes: uuid.New(), Valid: true}}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, regID pgtype.UUID, ids []pgtype.UUID) {
				_, err := queries.UpsertLatestServerVersion(
					context.Background(),
					UpsertLatestServerVersionParams{
						RegID:    regID,
						Name:     "test-server",
						Version:  "1.0.0",
						ServerID: ids[0],
					},
				)
				require.Error(t, err)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			db, cleanupFunc := database.SetupTestDB(t)
			t.Cleanup(cleanupFunc)
			queries := New(db)
			require.NotNil(t, queries)

			regID := setupRegistry(t, queries)
			require.NotNil(t, regID)

			ids := tc.setupFunc(t, queries, regID)
			tc.scenarioFunc(t, queries, regID, ids)
		})
	}
}

func TestUpsertServerIcon(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		setupFunc    func(t *testing.T, queries *Queries, regID pgtype.UUID) pgtype.UUID
		scenarioFunc func(t *testing.T, queries *Queries, serverID pgtype.UUID)
	}{
		{
			name: "insert server icon",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID pgtype.UUID) pgtype.UUID {
				serverID, err := queries.UpsertServerVersion(
					context.Background(),
					UpsertServerVersionParams{
						Name:    "test-server",
						Version: "1.0.0",
						RegID:   regID,
					},
				)
				require.NoError(t, err)
				require.NotNil(t, serverID)
				return serverID
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, serverID pgtype.UUID) {
				err := queries.UpsertServerIcon(
					context.Background(),
					UpsertServerIconParams{
						ServerID:  serverID,
						SourceUri: "https://example.com/icon.png",
						MimeType:  "image/png",
						Theme:     IconThemeLIGHT,
					},
				)
				require.NoError(t, err)
			},
		},
		{
			name: "insert server icon with dark theme",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID pgtype.UUID) pgtype.UUID {
				serverID, err := queries.UpsertServerVersion(
					context.Background(),
					UpsertServerVersionParams{
						Name:    "test-server",
						Version: "1.0.0",
						RegID:   regID,
					},
				)
				require.NoError(t, err)
				require.NotNil(t, serverID)
				return serverID
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, serverID pgtype.UUID) {
				err := queries.UpsertServerIcon(
					context.Background(),
					UpsertServerIconParams{
						ServerID:  serverID,
						SourceUri: "https://example.com/icon.png",
						MimeType:  "image/png",
						Theme:     IconThemeDARK,
					},
				)
				require.NoError(t, err)
			},
		},
		{
			name: "update existing server icon",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID pgtype.UUID) pgtype.UUID {
				serverID, err := queries.UpsertServerVersion(
					context.Background(),
					UpsertServerVersionParams{
						Name:    "test-server",
						Version: "1.0.0",
						RegID:   regID,
					},
				)
				require.NoError(t, err)
				require.NotNil(t, serverID)

				// Insert initial icon
				err = queries.UpsertServerIcon(
					context.Background(),
					UpsertServerIconParams{
						ServerID:  serverID,
						SourceUri: "https://example.com/icon.png",
						MimeType:  "image/png",
						Theme:     IconThemeLIGHT,
					},
				)
				require.NoError(t, err)

				return serverID
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, serverID pgtype.UUID) {
				err := queries.UpsertServerIcon(
					context.Background(),
					UpsertServerIconParams{
						ServerID:  serverID,
						SourceUri: "https://example.com/icon.png",
						MimeType:  "image/png",
						Theme:     IconThemeDARK,
					},
				)
				require.NoError(t, err)
			},
		},
		{
			name: "upsert server icon with invalid server_id",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries, _ pgtype.UUID) pgtype.UUID {
				return pgtype.UUID{Bytes: uuid.New(), Valid: true}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, serverID pgtype.UUID) {
				err := queries.UpsertServerIcon(
					context.Background(),
					UpsertServerIconParams{
						ServerID:  serverID,
						SourceUri: "https://example.com/icon.png",
						MimeType:  "image/png",
						Theme:     IconThemeLIGHT,
					},
				)
				require.Error(t, err)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			db, cleanupFunc := database.SetupTestDB(t)
			t.Cleanup(cleanupFunc)
			queries := New(db)
			require.NotNil(t, queries)

			regID := setupRegistry(t, queries)
			require.NotNil(t, regID)

			serverID := tc.setupFunc(t, queries, regID)
			tc.scenarioFunc(t, queries, serverID)
		})
	}
}

func TestUpsertServerPackage(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		setupFunc    func(t *testing.T, queries *Queries, regID pgtype.UUID) pgtype.UUID
		scenarioFunc func(t *testing.T, queries *Queries, serverID pgtype.UUID)
	}{
		{
			name: "insert server package with minimal fields",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID pgtype.UUID) pgtype.UUID {
				serverID, err := queries.UpsertServerVersion(
					context.Background(),
					UpsertServerVersionParams{
						Name:    "test-server",
						Version: "1.0.0",
						RegID:   regID,
					},
				)
				require.NoError(t, err)
				require.NotNil(t, serverID)
				return serverID
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, serverID pgtype.UUID) {
				err := queries.UpsertServerPackage(
					context.Background(),
					UpsertServerPackageParams{
						ServerID:       serverID,
						RegistryType:   "npm",
						PkgRegistryUrl: "https://registry.npmjs.org",
						PkgIdentifier:  "@test/package",
						PkgVersion:     "1.0.0",
						Transport:      "stdio",
					},
				)
				require.NoError(t, err)
			},
		},
		{
			name: "insert server package with all fields",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID pgtype.UUID) pgtype.UUID {
				serverID, err := queries.UpsertServerVersion(
					context.Background(),
					UpsertServerVersionParams{
						Name:    "test-server",
						Version: "1.0.0",
						RegID:   regID,
					},
				)
				require.NoError(t, err)
				require.NotNil(t, serverID)
				return serverID
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, serverID pgtype.UUID) {
				err := queries.UpsertServerPackage(
					context.Background(),
					UpsertServerPackageParams{
						ServerID:         serverID,
						RegistryType:     "npm",
						PkgRegistryUrl:   "https://registry.npmjs.org",
						PkgIdentifier:    "@test/package",
						PkgVersion:       "1.0.0",
						RuntimeHint:      pgtype.Text{String: "npx", Valid: true},
						RuntimeArguments: []string{"--yes"},
						PackageArguments: []string{"--arg", "value"},
						EnvVars:          []string{"NODE_ENV", "API_KEY"},
						Sha256Hash:       pgtype.Text{String: "abc123", Valid: true},
						Transport:        "stdio",
						TransportUrl:     pgtype.Text{String: "https://example.com", Valid: true},
						TransportHeaders: []string{"Authorization: Bearer token"},
					},
				)
				require.NoError(t, err)
			},
		},
		{
			name: "insert server package with invalid server_id",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries, _ pgtype.UUID) pgtype.UUID {
				return pgtype.UUID{Bytes: uuid.New(), Valid: true}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, serverID pgtype.UUID) {
				err := queries.UpsertServerPackage(
					context.Background(),
					UpsertServerPackageParams{
						ServerID:       serverID,
						RegistryType:   "npm",
						PkgRegistryUrl: "https://registry.npmjs.org",
						PkgIdentifier:  "@test/package",
						PkgVersion:     "1.0.0",
						Transport:      "stdio",
					},
				)
				require.Error(t, err)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			db, cleanupFunc := database.SetupTestDB(t)
			t.Cleanup(cleanupFunc)
			queries := New(db)
			require.NotNil(t, queries)

			regID := setupRegistry(t, queries)
			require.NotNil(t, regID)

			serverID := tc.setupFunc(t, queries, regID)
			tc.scenarioFunc(t, queries, serverID)
		})
	}
}

func TestUpsertServerRemote(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		setupFunc    func(t *testing.T, queries *Queries, regID pgtype.UUID) pgtype.UUID
		scenarioFunc func(t *testing.T, queries *Queries, serverID pgtype.UUID)
	}{
		{
			name: "insert server remote with minimal fields",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID pgtype.UUID) pgtype.UUID {
				serverID, err := queries.UpsertServerVersion(
					context.Background(),
					UpsertServerVersionParams{
						Name:    "test-server",
						Version: "1.0.0",
						RegID:   regID,
					},
				)
				require.NoError(t, err)
				require.NotNil(t, serverID)
				return serverID
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, serverID pgtype.UUID) {
				err := queries.UpsertServerRemote(
					context.Background(),
					UpsertServerRemoteParams{
						ServerID:     serverID,
						Transport:    "sse",
						TransportUrl: pgtype.Text{String: "https://example.com/sse", Valid: true},
					},
				)
				require.NoError(t, err)
			},
		},
		{
			name: "insert server remote with all fields",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID pgtype.UUID) pgtype.UUID {
				serverID, err := queries.UpsertServerVersion(
					context.Background(),
					UpsertServerVersionParams{
						Name:    "test-server",
						Version: "1.0.0",
						RegID:   regID,
					},
				)
				require.NoError(t, err)
				require.NotNil(t, serverID)
				return serverID
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, serverID pgtype.UUID) {
				err := queries.UpsertServerRemote(
					context.Background(),
					UpsertServerRemoteParams{
						ServerID:         serverID,
						Transport:        "sse",
						TransportUrl:     pgtype.Text{String: "https://example.com/sse", Valid: true},
						TransportHeaders: []string{"Authorization: Bearer token", "X-Custom: value"},
					},
				)
				require.NoError(t, err)
			},
		},
		{
			name: "update existing server remote",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID pgtype.UUID) pgtype.UUID {
				serverID, err := queries.UpsertServerVersion(
					context.Background(),
					UpsertServerVersionParams{
						Name:    "test-server",
						Version: "1.0.0",
						RegID:   regID,
					},
				)
				require.NoError(t, err)
				require.NotNil(t, serverID)

				// Insert initial remote
				err = queries.UpsertServerRemote(
					context.Background(),
					UpsertServerRemoteParams{
						ServerID:         serverID,
						Transport:        "sse",
						TransportUrl:     pgtype.Text{String: "https://example.com/sse", Valid: true},
						TransportHeaders: []string{"Old-Header: old"},
					},
				)
				require.NoError(t, err)

				return serverID
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, serverID pgtype.UUID) {
				err := queries.UpsertServerRemote(
					context.Background(),
					UpsertServerRemoteParams{
						ServerID:         serverID,
						Transport:        "sse",
						TransportUrl:     pgtype.Text{String: "https://example.com/sse", Valid: true},
						TransportHeaders: []string{"New-Header: new"},
					},
				)
				require.NoError(t, err)
			},
		},
		{
			name: "upsert server remote with invalid server_id",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries, _ pgtype.UUID) pgtype.UUID {
				return pgtype.UUID{Bytes: uuid.New(), Valid: true}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, serverID pgtype.UUID) {
				err := queries.UpsertServerRemote(
					context.Background(),
					UpsertServerRemoteParams{
						ServerID:     serverID,
						Transport:    "sse",
						TransportUrl: pgtype.Text{String: "https://example.com/sse", Valid: true},
					},
				)
				require.Error(t, err)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			db, cleanupFunc := database.SetupTestDB(t)
			t.Cleanup(cleanupFunc)
			queries := New(db)
			require.NotNil(t, queries)

			regID := setupRegistry(t, queries)
			require.NotNil(t, regID)

			serverID := tc.setupFunc(t, queries, regID)
			tc.scenarioFunc(t, queries, serverID)
		})
	}
}
