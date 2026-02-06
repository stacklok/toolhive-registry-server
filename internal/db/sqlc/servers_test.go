package sqlc

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/aws/smithy-go/ptr"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/database"
)

//nolint:thelper // We want to see these lines in the test output
func setupRegistry(t *testing.T, queries *Queries) uuid.UUID {
	regID, err := queries.InsertConfigRegistry(
		context.Background(),
		InsertConfigRegistryParams{
			Name:     "test-registry",
			RegType:  RegistryTypeREMOTE,
			Syncable: true,
		},
	)
	require.NoError(t, err)
	return regID
}

func TestInsertServerVersion(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		setupFunc    func(t *testing.T, queries *Queries, regID uuid.UUID)
		scenarioFunc func(t *testing.T, queries *Queries, regID uuid.UUID)
	}{
		{
			name: "insert server version with minimal fields",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries, _ uuid.UUID) {},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) {
				createdAt := time.Now().UTC()

				entryID, err := queries.InsertRegistryEntry(
					context.Background(),
					InsertRegistryEntryParams{
						Name:      "test-server",
						Version:   "1.0.0",
						RegID:     regID,
						EntryType: EntryTypeMCP,
						CreatedAt: &createdAt,
					},
				)
				require.NoError(t, err)

				_, err = queries.InsertServerVersion(
					context.Background(),
					InsertServerVersionParams{
						EntryID: entryID,
					},
				)
				require.NoError(t, err)
			},
		},
		{
			name: "insert server version with all fields",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries, _ uuid.UUID) {},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) {
				createdAt := time.Now().UTC()
				entryID, err := queries.InsertRegistryEntry(
					context.Background(),
					InsertRegistryEntryParams{
						Name:        "test-server",
						Version:     "1.0.0",
						RegID:       regID,
						Description: ptr.String("Test description"),
						Title:       ptr.String("Test Title"),
						EntryType:   EntryTypeMCP,
						CreatedAt:   &createdAt,
						UpdatedAt:   &createdAt,
					},
				)
				require.NoError(t, err)

				_, err = queries.InsertServerVersion(
					context.Background(),
					InsertServerVersionParams{
						EntryID:             entryID,
						Website:             ptr.String("https://example.com"),
						UpstreamMeta:        []byte(`{"key": "value"}`),
						ServerMeta:          []byte(`{"meta": "data"}`),
						RepositoryUrl:       ptr.String("https://github.com/test/repo"),
						RepositoryID:        ptr.String("repo-id"),
						RepositorySubfolder: ptr.String("subfolder"),
						RepositoryType:      ptr.String("git"),
					},
				)
				require.NoError(t, err)
			},
		},
		{
			name: "insert duplicate server version fails",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) {
				createdAt := time.Now().UTC()
				entryID, err := queries.InsertRegistryEntry(
					context.Background(),
					InsertRegistryEntryParams{
						Name:      "test-server",
						Version:   "1.0.0",
						RegID:     regID,
						EntryType: EntryTypeMCP,
						CreatedAt: &createdAt,
						UpdatedAt: &createdAt,
					},
				)
				require.NoError(t, err)

				_, err = queries.InsertServerVersion(
					context.Background(),
					InsertServerVersionParams{
						EntryID: entryID,
					},
				)
				require.NoError(t, err)
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) {
				updatedAt := time.Now().UTC()
				entryID, err := queries.InsertRegistryEntry(
					context.Background(),
					InsertRegistryEntryParams{
						Name:        "test-server",
						Version:     "1.0.0",
						RegID:       regID,
						Description: ptr.String("Updated description"),
						Title:       ptr.String("Updated Title"),
						EntryType:   EntryTypeMCP,
						UpdatedAt:   &updatedAt,
					},
				)
				require.Error(t, err)

				_, err = queries.InsertServerVersion(
					context.Background(),
					InsertServerVersionParams{
						EntryID: entryID,
					},
				)
				require.Error(t, err) // Should fail with unique constraint violation
			},
		},
		{
			name: "insert server version with invalid reg_id",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries, _ uuid.UUID) {},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, _ uuid.UUID) {
				createdAt := time.Now().UTC()
				regID := uuid.New()
				_, err := queries.InsertRegistryEntry(
					context.Background(),
					InsertRegistryEntryParams{
						Name:      "test-server",
						Version:   "1.0.0",
						RegID:     regID,
						EntryType: EntryTypeMCP,
						CreatedAt: &createdAt,
						UpdatedAt: &createdAt,
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
		setupFunc    func(t *testing.T, queries *Queries, regID uuid.UUID) string
		scenarioFunc func(t *testing.T, queries *Queries, serverName string)
	}{
		{
			name: "no server versions",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries, _ uuid.UUID) string {
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
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) string {
				createdAt := time.Now().UTC()
				entryID, err := queries.InsertRegistryEntry(
					context.Background(),
					InsertRegistryEntryParams{
						Name:      "test-server",
						Version:   "1.0.0",
						RegID:     regID,
						EntryType: EntryTypeMCP,
						CreatedAt: &createdAt,
						UpdatedAt: &createdAt,
					},
				)
				require.NoError(t, err)

				_, err = queries.InsertServerVersion(
					context.Background(),
					InsertServerVersionParams{
						EntryID: entryID,
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
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) string {
				createdAt := time.Now().UTC().Add(-1 * time.Minute)
				for _, version := range []string{"1.0.0", "2.0.0", "3.0.0"} {
					createdAt = createdAt.Add(1 * time.Second)
					entryID, err := queries.InsertRegistryEntry(
						context.Background(),
						InsertRegistryEntryParams{
							Name:      "test-server",
							Version:   version,
							RegID:     regID,
							EntryType: EntryTypeMCP,
							CreatedAt: &createdAt,
							UpdatedAt: &createdAt,
						},
					)
					require.NoError(t, err)

					_, err = queries.InsertServerVersion(
						context.Background(),
						InsertServerVersionParams{
							EntryID: entryID,
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
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) string {
				createdAt := time.Now().UTC().Add(-1 * time.Minute)
				for _, version := range []string{"1.0.0", "2.0.0", "3.0.0"} {
					createdAt = createdAt.Add(1 * time.Second)
					entryID, err := queries.InsertRegistryEntry(
						context.Background(),
						InsertRegistryEntryParams{
							Name:      "test-server",
							Version:   version,
							RegID:     regID,
							EntryType: EntryTypeMCP,
							CreatedAt: &createdAt,
							UpdatedAt: &createdAt,
						},
					)
					require.NoError(t, err)

					_, err = queries.InsertServerVersion(
						context.Background(),
						InsertServerVersionParams{
							EntryID: entryID,
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
				assert.Equal(t, "1.0.0", versions[0].Version)
				assert.Equal(t, "2.0.0", versions[1].Version)
				assert.Equal(t, "3.0.0", versions[2].Version)

				// Get next page
				nextTime := versions[1].CreatedAt.UTC()

				nextVersions, err := queries.ListServerVersions(
					context.Background(),
					ListServerVersionsParams{
						Name: serverName,
						Next: &nextTime,
						Size: 10,
					},
				)
				require.NoError(t, err)
				assert.Len(t, nextVersions, 1)
				assert.Equal(t, "3.0.0", nextVersions[0].Version)
			},
		},
		{
			name: "list server versions with limit",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) string {
				createdAt := time.Now().UTC().Add(-1 * time.Minute)
				for _, version := range []string{"1.0.0", "2.0.0", "3.0.0"} {
					createdAt = createdAt.Add(1 * time.Second)
					entryID, err := queries.InsertRegistryEntry(
						context.Background(),
						InsertRegistryEntryParams{
							Name:      "test-server",
							Version:   version,
							RegID:     regID,
							EntryType: EntryTypeMCP,
							CreatedAt: &createdAt,
							UpdatedAt: &createdAt,
						},
					)
					require.NoError(t, err)

					_, err = queries.InsertServerVersion(
						context.Background(),
						InsertServerVersionParams{
							EntryID: entryID,
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
		setupFunc    func(t *testing.T, queries *Queries, regID uuid.UUID)
		scenarioFunc func(t *testing.T, queries *Queries)
	}{
		{
			name: "no servers",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries, _ uuid.UUID) {},
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
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) {
				createdAt := time.Now().UTC()
				entryID, err := queries.InsertRegistryEntry(
					context.Background(),
					InsertRegistryEntryParams{
						Name:      "test-server",
						Version:   "1.0.0",
						RegID:     regID,
						EntryType: EntryTypeMCP,
						CreatedAt: &createdAt,
						UpdatedAt: &createdAt,
					},
				)
				require.NoError(t, err)

				_, err = queries.InsertServerVersion(
					context.Background(),
					InsertServerVersionParams{
						EntryID: entryID,
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
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) {
				createdAt := time.Now().UTC()
				for _, version := range []string{"1.0.0", "2.0.0"} {
					createdAt = createdAt.Add(1 * time.Second)
					entryID, err := queries.InsertRegistryEntry(
						context.Background(),
						InsertRegistryEntryParams{
							Name:      "test-server",
							Version:   version,
							RegID:     regID,
							EntryType: EntryTypeMCP,
							CreatedAt: &createdAt,
							UpdatedAt: &createdAt,
						},
					)
					require.NoError(t, err)

					_, err = queries.InsertServerVersion(
						context.Background(),
						InsertServerVersionParams{
							EntryID: entryID,
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
			name: "list servers with cursor pagination",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) {
				createdAt := time.Now().UTC().Add(-1 * time.Minute)
				for _, version := range []string{"1.0.0", "2.0.0"} {
					createdAt = createdAt.Add(1 * time.Second)
					entryID, err := queries.InsertRegistryEntry(
						context.Background(),
						InsertRegistryEntryParams{
							Name:      "test-server",
							Version:   version,
							RegID:     regID,
							EntryType: EntryTypeMCP,
							CreatedAt: &createdAt,
							UpdatedAt: &createdAt,
						},
					)
					require.NoError(t, err)

					_, err = queries.InsertServerVersion(
						context.Background(),
						InsertServerVersionParams{
							EntryID: entryID,
						},
					)
					require.NoError(t, err)
				}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries) {
				// First get all servers without cursor
				allServers, err := queries.ListServers(
					context.Background(),
					ListServersParams{
						Size: 10,
					},
				)
				require.NoError(t, err)
				require.Len(t, allServers, 2)
				// Verify ordering by name ASC, version ASC
				require.Equal(t, "test-server", allServers[0].Name)
				require.Equal(t, "1.0.0", allServers[0].Version)
				require.Equal(t, "test-server", allServers[1].Name)
				require.Equal(t, "2.0.0", allServers[1].Version)

				// Now use cursor to skip past first server
				cursorName := allServers[0].Name
				cursorVersion := allServers[0].Version
				servers, err := queries.ListServers(
					context.Background(),
					ListServersParams{
						CursorName:    &cursorName,
						CursorVersion: &cursorVersion,
						Size:          10,
					},
				)
				require.NoError(t, err)
				require.Len(t, servers, 1)
				// Should only return the second server (after cursor)
				require.Equal(t, "test-server", servers[0].Name)
				require.Equal(t, "2.0.0", servers[0].Version)
			},
		},
		{
			name: "list servers with is_latest flag",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) {
				createdAt := time.Now().UTC()
				entryID, err := queries.InsertRegistryEntry(
					context.Background(),
					InsertRegistryEntryParams{
						Name:      "test-server",
						Version:   "1.0.0",
						RegID:     regID,
						EntryType: EntryTypeMCP,
						CreatedAt: &createdAt,
						UpdatedAt: &createdAt,
					},
				)
				require.NoError(t, err)

				_, err = queries.InsertServerVersion(
					context.Background(),
					InsertServerVersionParams{
						EntryID: entryID,
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
						RegID:   regID,
						Name:    "test-server",
						Version: "1.0.0",
						EntryID: servers[0].ID,
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
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) {
				createdAt := time.Now().UTC().Add(-1 * time.Minute)
				for _, version := range []string{"1.0.0", "2.0.0"} {
					createdAt = createdAt.Add(1 * time.Second)
					entryID, err := queries.InsertRegistryEntry(
						context.Background(),
						InsertRegistryEntryParams{
							Name:      "test-server",
							Version:   version,
							RegID:     regID,
							EntryType: EntryTypeMCP,
							CreatedAt: &createdAt,
							UpdatedAt: &createdAt,
						},
					)
					require.NoError(t, err)

					_, err = queries.InsertServerVersion(
						context.Background(),
						InsertServerVersionParams{
							EntryID: entryID,
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
		setupFunc    func(t *testing.T, queries *Queries, regID uuid.UUID) []uuid.UUID
		scenarioFunc func(t *testing.T, queries *Queries, regID uuid.UUID, ids []uuid.UUID)
	}{
		{
			name: "insert latest server version",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) []uuid.UUID {
				createdAt := time.Now().UTC()
				entryID, err := queries.InsertRegistryEntry(
					context.Background(),
					InsertRegistryEntryParams{
						Name:      "test-server",
						Version:   "1.0.0",
						RegID:     regID,
						EntryType: EntryTypeMCP,
						CreatedAt: &createdAt,
						UpdatedAt: &createdAt,
					},
				)
				require.NoError(t, err)

				serverID, err := queries.InsertServerVersion(
					context.Background(),
					InsertServerVersionParams{
						EntryID: entryID,
					},
				)
				require.NoError(t, err)
				require.NotNil(t, serverID)
				return []uuid.UUID{serverID}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, regID uuid.UUID, ids []uuid.UUID) {
				serverID, err := queries.UpsertLatestServerVersion(
					context.Background(),
					UpsertLatestServerVersionParams{
						RegID:   regID,
						Name:    "test-server",
						Version: "1.0.0",
						EntryID: ids[0],
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
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) []uuid.UUID {
				var serverIDs []uuid.UUID
				createdAt := time.Now().UTC().Add(-1 * time.Minute)
				for _, version := range []string{"1.0.0", "2.0.0"} {
					createdAt = createdAt.Add(1 * time.Second)
					entryID, err := queries.InsertRegistryEntry(
						context.Background(),
						InsertRegistryEntryParams{
							Name:      "test-server",
							Version:   version,
							RegID:     regID,
							EntryType: EntryTypeMCP,
							CreatedAt: &createdAt,
							UpdatedAt: &createdAt,
						},
					)
					require.NoError(t, err)

					serverID, err := queries.InsertServerVersion(
						context.Background(),
						InsertServerVersionParams{
							EntryID: entryID,
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
						RegID:   regID,
						Name:    "test-server",
						Version: "1.0.0",
						EntryID: serverIDs[0],
					},
				)
				require.NoError(t, err)
				require.NotNil(t, latestServerID)
				require.Equal(t, serverIDs[0], latestServerID)

				return serverIDs
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, regID uuid.UUID, ids []uuid.UUID) {
				latestServerID, err := queries.UpsertLatestServerVersion(
					context.Background(),
					UpsertLatestServerVersionParams{
						RegID:   regID,
						Name:    "test-server",
						Version: "2.0.0",
						EntryID: ids[0],
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
			setupFunc: func(_ *testing.T, _ *Queries, _ uuid.UUID) []uuid.UUID {
				return []uuid.UUID{uuid.New()}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, _ uuid.UUID, ids []uuid.UUID) {
				regID := uuid.New()
				_, err := queries.UpsertLatestServerVersion(
					context.Background(),
					UpsertLatestServerVersionParams{
						RegID:   regID,
						Name:    "test-server",
						Version: "1.0.0",
						EntryID: ids[0],
					},
				)
				require.Error(t, err)
			},
		},
		{
			name: "upsert latest server version with invalid server_id",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries, _ uuid.UUID) []uuid.UUID {
				return []uuid.UUID{uuid.New()}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, regID uuid.UUID, ids []uuid.UUID) {
				_, err := queries.UpsertLatestServerVersion(
					context.Background(),
					UpsertLatestServerVersionParams{
						RegID:   regID,
						Name:    "test-server",
						Version: "1.0.0",
						EntryID: ids[0],
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

func TestInsertServerIcon(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		setupFunc    func(t *testing.T, queries *Queries, regID uuid.UUID) uuid.UUID
		scenarioFunc func(t *testing.T, queries *Queries, entryID uuid.UUID)
	}{
		{
			name: "insert server icon",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) uuid.UUID {
				createdAt := time.Now().UTC()
				entryID, err := queries.InsertRegistryEntry(
					context.Background(),
					InsertRegistryEntryParams{
						Name:      "test-server",
						Version:   "1.0.0",
						RegID:     regID,
						EntryType: EntryTypeMCP,
						CreatedAt: &createdAt,
						UpdatedAt: &createdAt,
					},
				)
				require.NoError(t, err)

				serverID, err := queries.InsertServerVersion(
					context.Background(),
					InsertServerVersionParams{
						EntryID: entryID,
					},
				)
				require.NoError(t, err)
				require.NotNil(t, serverID)
				return serverID
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, entryID uuid.UUID) {
				err := queries.InsertServerIcon(
					context.Background(),
					InsertServerIconParams{
						EntryID:   entryID,
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
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) uuid.UUID {
				createdAt := time.Now().UTC()
				entryID, err := queries.InsertRegistryEntry(
					context.Background(),
					InsertRegistryEntryParams{
						Name:      "test-server",
						Version:   "1.0.0",
						RegID:     regID,
						EntryType: EntryTypeMCP,
						CreatedAt: &createdAt,
						UpdatedAt: &createdAt,
					},
				)
				require.NoError(t, err)

				serverID, err := queries.InsertServerVersion(
					context.Background(),
					InsertServerVersionParams{
						EntryID: entryID,
					},
				)
				require.NoError(t, err)
				require.NotNil(t, serverID)
				return serverID
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, entryID uuid.UUID) {
				err := queries.InsertServerIcon(
					context.Background(),
					InsertServerIconParams{
						EntryID:   entryID,
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
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) uuid.UUID {
				createdAt := time.Now().UTC()
				entryID, err := queries.InsertRegistryEntry(
					context.Background(),
					InsertRegistryEntryParams{
						Name:      "test-server",
						Version:   "1.0.0",
						RegID:     regID,
						EntryType: EntryTypeMCP,
						CreatedAt: &createdAt,
						UpdatedAt: &createdAt,
					},
				)
				require.NoError(t, err)
				require.NotNil(t, entryID)

				serverID, err := queries.InsertServerVersion(
					context.Background(),
					InsertServerVersionParams{
						EntryID: entryID,
					},
				)
				require.NoError(t, err)
				require.Equal(t, entryID, serverID)

				// Insert initial icon
				err = queries.InsertServerIcon(
					context.Background(),
					InsertServerIconParams{
						EntryID:   entryID,
						SourceUri: "https://example.com/icon.png",
						MimeType:  "image/png",
						Theme:     IconThemeLIGHT,
					},
				)
				require.NoError(t, err)

				return entryID
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, entryID uuid.UUID) {
				err := queries.InsertServerIcon(
					context.Background(),
					InsertServerIconParams{
						EntryID:   entryID,
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
			setupFunc: func(_ *testing.T, _ *Queries, _ uuid.UUID) uuid.UUID {
				return uuid.New()
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, entryID uuid.UUID) {
				err := queries.InsertServerIcon(
					context.Background(),
					InsertServerIconParams{
						EntryID:   entryID,
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

func TestInsertServerPackage(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		setupFunc    func(t *testing.T, queries *Queries, regID uuid.UUID) uuid.UUID
		scenarioFunc func(t *testing.T, queries *Queries, serverID uuid.UUID)
	}{
		{
			name: "insert server package with minimal fields",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) uuid.UUID {
				createdAt := time.Now().UTC()
				entryID, err := queries.InsertRegistryEntry(
					context.Background(),
					InsertRegistryEntryParams{
						Name:      "test-server",
						Version:   "1.0.0",
						RegID:     regID,
						EntryType: EntryTypeMCP,
						CreatedAt: &createdAt,
						UpdatedAt: &createdAt,
					},
				)
				require.NoError(t, err)
				require.NotNil(t, entryID)

				serverID, err := queries.InsertServerVersion(
					context.Background(),
					InsertServerVersionParams{
						EntryID: entryID,
					},
				)
				require.NoError(t, err)
				require.Equal(t, entryID, serverID)
				return entryID
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, entryID uuid.UUID) {
				err := queries.InsertServerPackage(
					context.Background(),
					InsertServerPackageParams{
						EntryID:        entryID,
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
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) uuid.UUID {
				createdAt := time.Now().UTC()
				entryID, err := queries.InsertRegistryEntry(
					context.Background(),
					InsertRegistryEntryParams{
						Name:      "test-server",
						Version:   "1.0.0",
						RegID:     regID,
						EntryType: EntryTypeMCP,
						CreatedAt: &createdAt,
						UpdatedAt: &createdAt,
					},
				)
				require.NoError(t, err)
				require.NotNil(t, entryID)

				serverID, err := queries.InsertServerVersion(
					context.Background(),
					InsertServerVersionParams{
						EntryID: entryID,
					},
				)
				require.NoError(t, err)
				require.Equal(t, entryID, serverID)
				return entryID
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, entryID uuid.UUID) {
				err := queries.InsertServerPackage(
					context.Background(),
					InsertServerPackageParams{
						EntryID:          entryID,
						RegistryType:     "npm",
						PkgRegistryUrl:   "https://registry.npmjs.org",
						PkgIdentifier:    "@test/package",
						PkgVersion:       "1.0.0",
						RuntimeHint:      ptr.String("npx"),
						RuntimeArguments: []string{"--yes"},
						PackageArguments: []string{"--arg", "value"},
						EnvVars:          []byte(`[{"name":"NODE_ENV"},{"name":"API_KEY"}]`),
						Sha256Hash:       ptr.String("abc123"),
						Transport:        "stdio",
						TransportUrl:     ptr.String("https://example.com"),
						TransportHeaders: []byte(`[{"name":"Authorization: Bearer token"}]`),
					},
				)
				require.NoError(t, err)
			},
		},
		{
			name: "insert server package with invalid server_id",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries, _ uuid.UUID) uuid.UUID {
				return uuid.New()
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, entryID uuid.UUID) {
				err := queries.InsertServerPackage(
					context.Background(),
					InsertServerPackageParams{
						EntryID:        entryID,
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

func TestInsertServerRemote(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		setupFunc    func(t *testing.T, queries *Queries, regID uuid.UUID) uuid.UUID
		scenarioFunc func(t *testing.T, queries *Queries, serverID uuid.UUID)
	}{
		{
			name: "insert server remote with minimal fields",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) uuid.UUID {
				createdAt := time.Now().UTC()
				entryID, err := queries.InsertRegistryEntry(
					context.Background(),
					InsertRegistryEntryParams{
						Name:      "test-server",
						Version:   "1.0.0",
						RegID:     regID,
						EntryType: EntryTypeMCP,
						CreatedAt: &createdAt,
						UpdatedAt: &createdAt,
					},
				)
				require.NoError(t, err)
				require.NotNil(t, entryID)

				serverID, err := queries.InsertServerVersion(
					context.Background(),
					InsertServerVersionParams{
						EntryID: entryID,
					},
				)
				require.NoError(t, err)
				require.Equal(t, entryID, serverID)
				return entryID
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, entryID uuid.UUID) {
				err := queries.InsertServerRemote(
					context.Background(),
					InsertServerRemoteParams{
						EntryID:      entryID,
						Transport:    "sse",
						TransportUrl: "https://example.com/sse",
					},
				)
				require.NoError(t, err)
			},
		},
		{
			name: "insert server remote with all fields",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) uuid.UUID {
				createdAt := time.Now().UTC()
				entryID, err := queries.InsertRegistryEntry(
					context.Background(),
					InsertRegistryEntryParams{
						Name:      "test-server",
						Version:   "1.0.0",
						RegID:     regID,
						EntryType: EntryTypeMCP,
						CreatedAt: &createdAt,
						UpdatedAt: &createdAt,
					},
				)
				require.NoError(t, err)
				require.NotNil(t, entryID)

				serverID, err := queries.InsertServerVersion(
					context.Background(),
					InsertServerVersionParams{
						EntryID: entryID,
					},
				)
				require.NoError(t, err)
				require.Equal(t, entryID, serverID)
				return entryID
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, entryID uuid.UUID) {
				err := queries.InsertServerRemote(
					context.Background(),
					InsertServerRemoteParams{
						EntryID:          entryID,
						Transport:        "sse",
						TransportUrl:     "https://example.com/sse",
						TransportHeaders: []byte(`[{"name":"Authorization: Bearer token"},{"name":"X-Custom: value"}]`),
					},
				)
				require.NoError(t, err)
			},
		},
		{
			name: "insert duplicate server remote fails",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) uuid.UUID {
				createdAt := time.Now().UTC()
				entryID, err := queries.InsertRegistryEntry(
					context.Background(),
					InsertRegistryEntryParams{
						Name:      "test-server",
						Version:   "1.0.0",
						RegID:     regID,
						EntryType: EntryTypeMCP,
						CreatedAt: &createdAt,
						UpdatedAt: &createdAt,
					},
				)
				require.NoError(t, err)
				require.NotNil(t, entryID)

				serverID, err := queries.InsertServerVersion(
					context.Background(),
					InsertServerVersionParams{
						EntryID: entryID,
					},
				)
				require.NoError(t, err)
				require.Equal(t, entryID, serverID)

				// Insert initial remote
				err = queries.InsertServerRemote(
					context.Background(),
					InsertServerRemoteParams{
						EntryID:          entryID,
						Transport:        "sse",
						TransportUrl:     "https://example.com/sse",
						TransportHeaders: []byte(`[{"name":"Old-Header: old"}]`),
					},
				)
				require.NoError(t, err)

				return entryID
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, entryID uuid.UUID) {
				// Attempt to insert duplicate remote with same primary key (server_id, transport, transport_url) - should fail
				err := queries.InsertServerRemote(
					context.Background(),
					InsertServerRemoteParams{
						EntryID:          entryID,
						Transport:        "sse",
						TransportUrl:     "https://example.com/sse",
						TransportHeaders: []byte(`[{"name":"New-Header: new"}]`),
					},
				)
				require.Error(t, err)
			},
		},
		{
			name: "insert server remote with invalid server_id",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries, _ uuid.UUID) uuid.UUID {
				return uuid.New()
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, entryID uuid.UUID) {
				err := queries.InsertServerRemote(
					context.Background(),
					InsertServerRemoteParams{
						EntryID:      entryID,
						Transport:    "sse",
						TransportUrl: "https://example.com/sse",
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

func TestListServerPackages(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		setupFunc    func(t *testing.T, queries *Queries, regID uuid.UUID) []uuid.UUID
		scenarioFunc func(t *testing.T, queries *Queries, serverIDs []uuid.UUID)
	}{
		{
			name: "no server packages",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) []uuid.UUID {
				createdAt := time.Now().UTC()
				entryID, err := queries.InsertRegistryEntry(
					context.Background(),
					InsertRegistryEntryParams{
						Name:      "test-server",
						Version:   "1.0.0",
						RegID:     regID,
						EntryType: EntryTypeMCP,
						CreatedAt: &createdAt,
						UpdatedAt: &createdAt,
					},
				)
				require.NoError(t, err)
				require.NotNil(t, entryID)

				serverID, err := queries.InsertServerVersion(
					context.Background(),
					InsertServerVersionParams{
						EntryID: entryID,
					},
				)
				require.NoError(t, err)
				require.Equal(t, entryID, serverID)
				return []uuid.UUID{entryID}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, entryIDs []uuid.UUID) {
				packages, err := queries.ListServerPackages(
					context.Background(),
					entryIDs,
				)
				require.NoError(t, err)
				require.Empty(t, packages)
			},
		},
		{
			name: "list single server package",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) []uuid.UUID {
				createdAt := time.Now().UTC()
				entryID, err := queries.InsertRegistryEntry(
					context.Background(),
					InsertRegistryEntryParams{
						Name:      "test-server",
						Version:   "1.0.0",
						RegID:     regID,
						EntryType: EntryTypeMCP,
						CreatedAt: &createdAt,
						UpdatedAt: &createdAt,
					},
				)
				require.NoError(t, err)
				require.NotNil(t, entryID)

				serverID, err := queries.InsertServerVersion(
					context.Background(),
					InsertServerVersionParams{
						EntryID: entryID,
					},
				)
				require.NoError(t, err)
				require.Equal(t, entryID, serverID)

				err = queries.InsertServerPackage(
					context.Background(),
					InsertServerPackageParams{
						EntryID:        entryID,
						RegistryType:   "npm",
						PkgRegistryUrl: "https://registry.npmjs.org",
						PkgIdentifier:  "@test/package",
						PkgVersion:     "1.0.0",
						Transport:      "stdio",
					},
				)
				require.NoError(t, err)

				return []uuid.UUID{entryID}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, entryIDs []uuid.UUID) {
				packages, err := queries.ListServerPackages(
					context.Background(),
					entryIDs,
				)
				require.NoError(t, err)
				require.Len(t, packages, 1)
				require.Equal(t, entryIDs[0], packages[0].EntryID)
				require.Equal(t, "npm", packages[0].RegistryType)
				require.Equal(t, "@test/package", packages[0].PkgIdentifier)
				require.Equal(t, "1.0.0", packages[0].PkgVersion)
			},
		},
		{
			name: "list multiple server packages for multiple servers",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) []uuid.UUID {
				var entryIDs []uuid.UUID
				createdAt := time.Now().UTC().Add(-1 * time.Minute)
				for i, version := range []string{"1.0.0", "2.0.0", "3.0.0"} {
					createdAt = createdAt.Add(1 * time.Second)
					entryID, err := queries.InsertRegistryEntry(
						context.Background(),
						InsertRegistryEntryParams{
							Name:      fmt.Sprintf("test-server-%d", i+1),
							Version:   version,
							RegID:     regID,
							EntryType: EntryTypeMCP,
							CreatedAt: &createdAt,
							UpdatedAt: &createdAt,
						},
					)
					require.NoError(t, err)
					require.NotNil(t, entryID)

					serverID, err := queries.InsertServerVersion(
						context.Background(),
						InsertServerVersionParams{
							EntryID: entryID,
						},
					)
					require.NoError(t, err)
					require.Equal(t, entryID, serverID)
					entryIDs = append(entryIDs, entryID)

					err = queries.InsertServerPackage(
						context.Background(),
						InsertServerPackageParams{
							EntryID:        entryID,
							RegistryType:   "npm",
							PkgRegistryUrl: "https://registry.npmjs.org",
							PkgIdentifier:  "@test/package",
							PkgVersion:     version,
							Transport:      "stdio",
						},
					)
					require.NoError(t, err)
				}

				return entryIDs
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, serverIDs []uuid.UUID) {
				packages, err := queries.ListServerPackages(
					context.Background(),
					serverIDs,
				)
				require.NoError(t, err)
				require.Len(t, packages, 3)
				// Verify ordering by version DESC
				require.Equal(t, "3.0.0", packages[0].PkgVersion)
				require.Equal(t, "2.0.0", packages[1].PkgVersion)
				require.Equal(t, "1.0.0", packages[2].PkgVersion)
			},
		},
		{
			name: "list server packages for multiple servers",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) []uuid.UUID {
				var entryIDs []uuid.UUID
				createdAt := time.Now().UTC().Add(-1 * time.Minute)
				for i, name := range []string{"server-1", "server-2"} {
					createdAt = createdAt.Add(1 * time.Second)
					entryID, err := queries.InsertRegistryEntry(
						context.Background(),
						InsertRegistryEntryParams{
							Name:      name,
							Version:   "1.0.0",
							RegID:     regID,
							EntryType: EntryTypeMCP,
							CreatedAt: &createdAt,
							UpdatedAt: &createdAt,
						},
					)
					require.NoError(t, err)
					require.NotNil(t, entryID)

					serverID, err := queries.InsertServerVersion(
						context.Background(),
						InsertServerVersionParams{
							EntryID: entryID,
						},
					)
					require.NoError(t, err)
					require.Equal(t, entryID, serverID)
					entryIDs = append(entryIDs, entryID)

					err = queries.InsertServerPackage(
						context.Background(),
						InsertServerPackageParams{
							EntryID:        entryID,
							RegistryType:   "npm",
							PkgRegistryUrl: "https://registry.npmjs.org",
							PkgIdentifier:  fmt.Sprintf("@test/package-%d", i+1),
							PkgVersion:     "1.0.0",
							Transport:      "stdio",
						},
					)
					require.NoError(t, err)
				}

				return entryIDs
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, entryIDs []uuid.UUID) {
				packages, err := queries.ListServerPackages(
					context.Background(),
					entryIDs,
				)
				require.NoError(t, err)
				require.Len(t, packages, 2)
				// Verify both servers are included
				entryIDMap := make(map[uuid.UUID]bool)
				for _, pkg := range packages {
					entryIDMap[pkg.EntryID] = true
				}
				require.True(t, entryIDMap[entryIDs[0]])
				require.True(t, entryIDMap[entryIDs[1]])
			},
		},
		{
			name: "list server packages with non-existent server IDs",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries, _ uuid.UUID) []uuid.UUID {
				return []uuid.UUID{
					uuid.New(),
					uuid.New(),
				}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, serverIDs []uuid.UUID) {
				packages, err := queries.ListServerPackages(
					context.Background(),
					serverIDs,
				)
				require.NoError(t, err)
				require.Empty(t, packages)
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

			serverIDs := tc.setupFunc(t, queries, regID)
			tc.scenarioFunc(t, queries, serverIDs)
		})
	}
}

func TestListServerRemotes(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		setupFunc    func(t *testing.T, queries *Queries, regID uuid.UUID) []uuid.UUID
		scenarioFunc func(t *testing.T, queries *Queries, serverIDs []uuid.UUID)
	}{
		{
			name: "no server remotes",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) []uuid.UUID {
				createdAt := time.Now().UTC()
				entryID, err := queries.InsertRegistryEntry(
					context.Background(),
					InsertRegistryEntryParams{
						Name:      "test-server",
						Version:   "1.0.0",
						RegID:     regID,
						EntryType: EntryTypeMCP,
						CreatedAt: &createdAt,
						UpdatedAt: &createdAt,
					},
				)
				require.NoError(t, err)
				require.NotNil(t, entryID)

				serverID, err := queries.InsertServerVersion(
					context.Background(),
					InsertServerVersionParams{
						EntryID: entryID,
					},
				)
				require.NoError(t, err)
				require.Equal(t, entryID, serverID)
				return []uuid.UUID{entryID}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, entryIDs []uuid.UUID) {
				remotes, err := queries.ListServerRemotes(
					context.Background(),
					entryIDs,
				)
				require.NoError(t, err)
				require.Empty(t, remotes)
			},
		},
		{
			name: "list single server remote",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) []uuid.UUID {
				createdAt := time.Now().UTC()
				entryID, err := queries.InsertRegistryEntry(
					context.Background(),
					InsertRegistryEntryParams{
						Name:      "test-server",
						Version:   "1.0.0",
						RegID:     regID,
						EntryType: EntryTypeMCP,
						CreatedAt: &createdAt,
						UpdatedAt: &createdAt,
					},
				)
				require.NoError(t, err)
				require.NotNil(t, entryID)

				serverID, err := queries.InsertServerVersion(
					context.Background(),
					InsertServerVersionParams{
						EntryID: entryID,
					},
				)
				require.NoError(t, err)
				require.Equal(t, entryID, serverID)

				err = queries.InsertServerRemote(
					context.Background(),
					InsertServerRemoteParams{
						EntryID:      entryID,
						Transport:    "sse",
						TransportUrl: "https://example.com/sse",
					},
				)
				require.NoError(t, err)

				return []uuid.UUID{entryID}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, entryIDs []uuid.UUID) {
				remotes, err := queries.ListServerRemotes(
					context.Background(),
					entryIDs,
				)
				require.NoError(t, err)
				require.Len(t, remotes, 1)
				require.Equal(t, entryIDs[0], remotes[0].EntryID)
				require.Equal(t, "sse", remotes[0].Transport)
				require.Equal(t, "https://example.com/sse", remotes[0].TransportUrl)
			},
		},
		{
			name: "list multiple server remotes for single server",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) []uuid.UUID {
				createdAt := time.Now().UTC()
				entryID, err := queries.InsertRegistryEntry(
					context.Background(),
					InsertRegistryEntryParams{
						Name:      "test-server",
						Version:   "1.0.0",
						RegID:     regID,
						EntryType: EntryTypeMCP,
						CreatedAt: &createdAt,
						UpdatedAt: &createdAt,
					},
				)
				require.NoError(t, err)
				require.NotNil(t, entryID)

				serverID, err := queries.InsertServerVersion(
					context.Background(),
					InsertServerVersionParams{
						EntryID: entryID,
					},
				)
				require.NoError(t, err)
				require.Equal(t, entryID, serverID)

				remotes := []struct {
					transport string
					url       string
				}{
					{"sse", "https://example.com/sse1"},
					{"sse", "https://example.com/sse2"},
					{"http", "https://example.com/http"},
				}

				for _, remote := range remotes {
					err = queries.InsertServerRemote(
						context.Background(),
						InsertServerRemoteParams{
							EntryID:      entryID,
							Transport:    remote.transport,
							TransportUrl: remote.url,
						},
					)
					require.NoError(t, err)
				}

				return []uuid.UUID{entryID}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, entryIDs []uuid.UUID) {
				remotes, err := queries.ListServerRemotes(
					context.Background(),
					entryIDs,
				)
				require.NoError(t, err)
				require.Len(t, remotes, 3)
				require.Equal(t, entryIDs[0], remotes[0].EntryID)
				// Verify ordering by transport, then transport_url
				require.Equal(t, "http", remotes[0].Transport)
				require.Equal(t, "sse", remotes[1].Transport)
				require.Equal(t, "sse", remotes[2].Transport)
				require.Equal(t, "https://example.com/http", remotes[0].TransportUrl)
				require.Equal(t, "https://example.com/sse1", remotes[1].TransportUrl)
				require.Equal(t, "https://example.com/sse2", remotes[2].TransportUrl)
			},
		},
		{
			name: "list server remotes for multiple servers",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) []uuid.UUID {
				var entryIDs []uuid.UUID
				createdAt := time.Now().UTC().Add(-1 * time.Minute)
				for i, name := range []string{"server-1", "server-2"} {
					createdAt = createdAt.Add(1 * time.Second)
					entryID, err := queries.InsertRegistryEntry(
						context.Background(),
						InsertRegistryEntryParams{
							Name:      name,
							Version:   "1.0.0",
							RegID:     regID,
							EntryType: EntryTypeMCP,
							CreatedAt: &createdAt,
							UpdatedAt: &createdAt,
						},
					)
					require.NoError(t, err)
					require.NotNil(t, entryID)

					serverID, err := queries.InsertServerVersion(
						context.Background(),
						InsertServerVersionParams{
							EntryID: entryID,
						},
					)
					require.NoError(t, err)
					require.Equal(t, entryID, serverID)
					entryIDs = append(entryIDs, entryID)

					err = queries.InsertServerRemote(
						context.Background(),
						InsertServerRemoteParams{
							EntryID:      entryID,
							Transport:    "sse",
							TransportUrl: fmt.Sprintf("https://example.com/sse-%d", i+1),
						},
					)
					require.NoError(t, err)
				}

				return entryIDs
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, entryIDs []uuid.UUID) {
				remotes, err := queries.ListServerRemotes(
					context.Background(),
					entryIDs,
				)
				require.NoError(t, err)
				require.Len(t, remotes, 2)
				// Verify both servers are included
				entryIDMap := make(map[uuid.UUID]bool)
				for _, remote := range remotes {
					entryIDMap[remote.EntryID] = true
				}
				require.True(t, entryIDMap[entryIDs[0]])
				require.True(t, entryIDMap[entryIDs[1]])
			},
		},
		{
			name: "list server remotes with non-existent server IDs",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries, _ uuid.UUID) []uuid.UUID {
				return []uuid.UUID{
					uuid.New(),
					uuid.New(),
				}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, entryIDs []uuid.UUID) {
				remotes, err := queries.ListServerRemotes(
					context.Background(),
					entryIDs,
				)
				require.NoError(t, err)
				require.Empty(t, remotes)
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

			serverIDs := tc.setupFunc(t, queries, regID)
			tc.scenarioFunc(t, queries, serverIDs)
		})
	}
}

func TestGetServerVersion(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		setupFunc    func(t *testing.T, queries *Queries, regID uuid.UUID) (string, string)
		scenarioFunc func(t *testing.T, queries *Queries, serverName, version string)
	}{
		{
			name: "get server version with minimal fields",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) (string, string) {
				createdAt := time.Now().UTC()
				entryID, err := queries.InsertRegistryEntry(
					context.Background(),
					InsertRegistryEntryParams{
						Name:      "test-server",
						Version:   "1.0.0",
						RegID:     regID,
						EntryType: EntryTypeMCP,
						CreatedAt: &createdAt,
						UpdatedAt: &createdAt,
					},
				)
				require.NoError(t, err)
				require.NotNil(t, entryID)

				serverID, err := queries.InsertServerVersion(
					context.Background(),
					InsertServerVersionParams{
						EntryID: entryID,
					},
				)
				require.NoError(t, err)
				require.Equal(t, entryID, serverID)

				//nolint:goconst
				return "test-server", "1.0.0"
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, serverName, version string) {
				server, err := queries.GetServerVersion(
					context.Background(),
					GetServerVersionParams{
						Name:    serverName,
						Version: version,
					},
				)
				require.NoError(t, err)
				require.Equal(t, serverName, server.Name)
				require.Equal(t, version, server.Version)
				require.Equal(t, RegistryTypeREMOTE, server.RegistryType)
				require.False(t, server.IsLatest)
				require.NotNil(t, server.ID)
				require.NotNil(t, server.CreatedAt)
			},
		},
		{
			name: "get server version with all fields",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) (string, string) {
				createdAt := time.Now().UTC()
				entryID, err := queries.InsertRegistryEntry(
					context.Background(),
					InsertRegistryEntryParams{
						Name:        "test-server",
						Version:     "1.0.0",
						RegID:       regID,
						Description: ptr.String("Test description"),
						Title:       ptr.String("Test Title"),
						EntryType:   EntryTypeMCP,
						CreatedAt:   &createdAt,
						UpdatedAt:   &createdAt,
					},
				)
				require.NoError(t, err)
				require.NotNil(t, entryID)

				serverID, err := queries.InsertServerVersion(
					context.Background(),
					InsertServerVersionParams{
						EntryID:             entryID,
						Website:             ptr.String("https://example.com"),
						UpstreamMeta:        []byte(`{"key": "value"}`),
						ServerMeta:          []byte(`{"meta": "data"}`),
						RepositoryUrl:       ptr.String("https://github.com/test/repo"),
						RepositoryID:        ptr.String("repo-id"),
						RepositorySubfolder: ptr.String("subfolder"),
						RepositoryType:      ptr.String("git"),
					},
				)
				require.NoError(t, err)
				require.Equal(t, entryID, serverID)
				return "test-server", "1.0.0"
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, serverName, version string) {
				server, err := queries.GetServerVersion(
					context.Background(),
					GetServerVersionParams{
						Name:    serverName,
						Version: version,
					},
				)
				require.NoError(t, err)
				require.Equal(t, serverName, server.Name)
				require.Equal(t, version, server.Version)
				require.Equal(t, RegistryTypeREMOTE, server.RegistryType)
				require.NotNil(t, server.Description)
				require.Equal(t, "Test description", *server.Description)
				require.NotNil(t, server.Title)
				require.Equal(t, "Test Title", *server.Title)
				require.NotNil(t, server.Website)
				require.Equal(t, "https://example.com", *server.Website)
				require.Equal(t, []byte(`{"key": "value"}`), server.UpstreamMeta)
				require.Equal(t, []byte(`{"meta": "data"}`), server.ServerMeta)
				require.NotNil(t, server.RepositoryUrl)
				require.Equal(t, "https://github.com/test/repo", *server.RepositoryUrl)
				require.NotNil(t, server.RepositoryID)
				require.Equal(t, "repo-id", *server.RepositoryID)
				require.NotNil(t, server.RepositorySubfolder)
				require.Equal(t, "subfolder", *server.RepositorySubfolder)
				require.NotNil(t, server.RepositoryType)
				require.Equal(t, "git", *server.RepositoryType)
			},
		},
		{
			name: "get server version marked as latest",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) (string, string) {
				createdAt := time.Now().UTC()
				entryID, err := queries.InsertRegistryEntry(
					context.Background(),
					InsertRegistryEntryParams{
						Name:      "test-server",
						Version:   "1.0.0",
						RegID:     regID,
						EntryType: EntryTypeMCP,
						CreatedAt: &createdAt,
						UpdatedAt: &createdAt,
					},
				)
				require.NoError(t, err)
				require.NotNil(t, entryID)

				serverID, err := queries.InsertServerVersion(
					context.Background(),
					InsertServerVersionParams{
						EntryID: entryID,
					},
				)
				require.NoError(t, err)
				require.Equal(t, entryID, serverID)

				_, err = queries.UpsertLatestServerVersion(
					context.Background(),
					UpsertLatestServerVersionParams{
						RegID:   regID,
						Name:    "test-server",
						Version: "1.0.0",
						EntryID: entryID,
					},
				)
				require.NoError(t, err)
				return "test-server", "1.0.0"
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, serverName, version string) {
				server, err := queries.GetServerVersion(
					context.Background(),
					GetServerVersionParams{
						Name:    serverName,
						Version: version,
					},
				)
				require.NoError(t, err)
				require.Equal(t, serverName, server.Name)
				require.Equal(t, version, server.Version)
				require.True(t, server.IsLatest)
			},
		},
		{
			name: "get server version not marked as latest",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) (string, string) {
				createdAt := time.Now().UTC().Add(-1 * time.Minute)
				entryID1, err := queries.InsertRegistryEntry(
					context.Background(),
					InsertRegistryEntryParams{
						Name:      "test-server",
						Version:   "1.0.0",
						RegID:     regID,
						EntryType: EntryTypeMCP,
						CreatedAt: &createdAt,
						UpdatedAt: &createdAt,
					},
				)
				require.NoError(t, err)
				require.NotNil(t, entryID1)

				// Create first version and mark it as latest
				serverID1, err := queries.InsertServerVersion(
					context.Background(),
					InsertServerVersionParams{
						EntryID: entryID1,
					},
				)
				require.NoError(t, err)
				require.Equal(t, entryID1, serverID1)

				_, err = queries.UpsertLatestServerVersion(
					context.Background(),
					UpsertLatestServerVersionParams{
						RegID:   regID,
						Name:    "test-server",
						Version: "1.0.0",
						EntryID: entryID1,
					},
				)
				require.NoError(t, err)

				// Create second version (not marked as latest)
				createdAt2 := createdAt.Add(1 * time.Second)
				entryID2, err := queries.InsertRegistryEntry(
					context.Background(),
					InsertRegistryEntryParams{
						Name:      "test-server",
						Version:   "2.0.0",
						RegID:     regID,
						EntryType: EntryTypeMCP,
						CreatedAt: &createdAt2,
						UpdatedAt: &createdAt2,
					},
				)
				require.NoError(t, err)
				require.NotNil(t, entryID2)

				serverID2, err := queries.InsertServerVersion(
					context.Background(),
					InsertServerVersionParams{
						EntryID: entryID2,
					},
				)
				require.NoError(t, err)
				require.Equal(t, entryID2, serverID2)
				return "test-server", "2.0.0"
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, serverName, version string) {
				server, err := queries.GetServerVersion(
					context.Background(),
					GetServerVersionParams{
						Name:    serverName,
						Version: version,
					},
				)
				require.NoError(t, err)
				require.Equal(t, serverName, server.Name)
				require.Equal(t, version, server.Version)
				require.False(t, server.IsLatest)
			},
		},
		{
			name: "get non-existent server version",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries, _ uuid.UUID) (string, string) {
				return "non-existent-server", "1.0.0"
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, serverName, version string) {
				_, err := queries.GetServerVersion(
					context.Background(),
					GetServerVersionParams{
						Name:    serverName,
						Version: version,
					},
				)
				require.Error(t, err)
			},
		},
		{
			name: "get server version with wrong version",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) (string, string) {
				createdAt := time.Now().UTC()
				entryID, err := queries.InsertRegistryEntry(
					context.Background(),
					InsertRegistryEntryParams{
						Name:      "test-server",
						Version:   "1.0.0",
						RegID:     regID,
						EntryType: EntryTypeMCP,
						CreatedAt: &createdAt,
						UpdatedAt: &createdAt,
					},
				)
				require.NoError(t, err)
				require.NotNil(t, entryID)

				serverID, err := queries.InsertServerVersion(
					context.Background(),
					InsertServerVersionParams{
						EntryID: entryID,
					},
				)
				require.NoError(t, err)
				require.Equal(t, entryID, serverID)
				return "test-server", "2.0.0"
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, serverName, version string) {
				_, err := queries.GetServerVersion(
					context.Background(),
					GetServerVersionParams{
						Name:    serverName,
						Version: version,
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

			serverName, version := tc.setupFunc(t, queries, regID)
			tc.scenarioFunc(t, queries, serverName, version)
		})
	}
}
