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

const (
	latestVersion = "latest"
)

//nolint:thelper // We want to see these lines in the test output
func setupRegistry(t *testing.T, queries *Queries) uuid.UUID {
	regID, err := queries.UpsertSource(
		context.Background(),
		UpsertSourceParams{CreationType: CreationTypeCONFIG,
			Name:       "test-registry",
			SourceType: "git",
			Syncable:   true,
		},
	)
	require.NoError(t, err)

	// Create a registry and link the source to it (needed for registry_name subquery)
	now := time.Now().UTC()
	reg, err := queries.UpsertRegistry(context.Background(), UpsertRegistryParams{CreationType: CreationTypeCONFIG,
		Name:      "test-registry",
		CreatedAt: &now,
		UpdatedAt: &now,
	})
	require.NoError(t, err)
	err = queries.LinkRegistrySource(context.Background(), LinkRegistrySourceParams{
		RegistryID: reg.ID,
		SourceID:   regID,
		Position:   0,
	})
	require.NoError(t, err)

	return regID
}

//nolint:thelper // We want to see these lines in the test output
func createEntry(
	t *testing.T,
	queries *Queries,
	regID uuid.UUID,
	name string,
	createdAt *time.Time,
) uuid.UUID {
	entryID, err := queries.InsertRegistryEntry(
		context.Background(),
		InsertRegistryEntryParams{
			Name:      name,
			SourceID:  regID,
			EntryType: EntryTypeMCP,
			CreatedAt: createdAt,
			UpdatedAt: createdAt,
		},
	)
	require.NoError(t, err)
	return entryID
}

//nolint:thelper // We want to see these lines in the test output
func createVersion(
	t *testing.T,
	queries *Queries,
	entryID uuid.UUID,
	name string,
	version string,
	description, title *string,
	createdAt *time.Time,
) uuid.UUID {
	versionID, err := queries.InsertEntryVersion(
		context.Background(),
		InsertEntryVersionParams{
			EntryID:     entryID,
			Name:        name,
			Version:     version,
			Title:       title,
			Description: description,
			CreatedAt:   createdAt,
			UpdatedAt:   createdAt,
		},
	)
	require.NoError(t, err)
	return versionID
}

//nolint:thelper // We want to see these lines in the test output
func createServer(
	t *testing.T,
	queries *Queries,
	versionID uuid.UUID,
) uuid.UUID {
	serverID, err := queries.InsertServerVersion(
		context.Background(),
		InsertServerVersionParams{
			VersionID: versionID,
		},
	)
	require.NoError(t, err)
	return serverID
}

func TestInsertServerVersion(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		setupFunc    func(t *testing.T, queries *Queries, regID uuid.UUID) uuid.UUID
		scenarioFunc func(t *testing.T, queries *Queries, regID uuid.UUID, entryID uuid.UUID)
	}{
		{
			name: "insert server version with minimal fields",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries, _ uuid.UUID) uuid.UUID {
				return uuid.Nil
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, regID uuid.UUID, _ uuid.UUID) {
				createdAt := time.Now().UTC()
				entryID := createEntry(t, queries, regID, "test-server", &createdAt)
				versionID := createVersion(t, queries, entryID, "test-server", "1.0.0", nil, nil, &createdAt)
				createServer(t, queries, versionID)
			},
		},
		{
			name: "insert server version with all fields",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) uuid.UUID {
				createdAt := time.Now().UTC()
				entryID := createEntry(t, queries, regID, "test-server", &createdAt)
				return entryID
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, _ uuid.UUID, entryID uuid.UUID) {
				createdAt := time.Now().UTC()
				versionID := createVersion(
					t,
					queries,
					entryID,
					"test-server",
					"1.0.0",
					ptr.String("Test description"),
					ptr.String("Test Title"),
					&createdAt,
				)
				_, err := queries.InsertServerVersion(
					context.Background(),
					InsertServerVersionParams{
						VersionID:           versionID,
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
			name: "insert duplicate entry version fails",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) uuid.UUID {
				createdAt := time.Now().UTC()
				entryID := createEntry(t, queries, regID, "test-server", &createdAt)
				createVersion(t, queries, entryID, "test-server", "1.0.0", nil, nil, &createdAt)
				return entryID
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, _ uuid.UUID, entryID uuid.UUID) {
				// Inserting a duplicate entry_version (same entry_id+version) should fail
				createdAt := time.Now().UTC()
				_, err := queries.InsertEntryVersion(
					context.Background(),
					InsertEntryVersionParams{
						EntryID:   entryID,
						Name:      "test-server",
						Version:   "1.0.0",
						CreatedAt: &createdAt,
						UpdatedAt: &createdAt,
					},
				)
				require.Error(t, err)
			},
		},
		{
			name: "insert server version with invalid source_id",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries, _ uuid.UUID) uuid.UUID {
				return uuid.Nil
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, _ uuid.UUID, _ uuid.UUID) {
				createdAt := time.Now().UTC()
				invalidRegID := uuid.New()
				_, err := queries.InsertRegistryEntry(
					context.Background(),
					InsertRegistryEntryParams{
						Name:      "test-server",
						SourceID:  invalidRegID,
						EntryType: EntryTypeMCP,
						CreatedAt: &createdAt,
						UpdatedAt: &createdAt,
					},
				)
				require.Error(t, err)
			},
		},
		{
			name: "insert server version with invalid entry_id",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries, _ uuid.UUID) uuid.UUID {
				return uuid.Nil
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, regID uuid.UUID, _ uuid.UUID) {
				createdAt := time.Now().UTC()
				_, err := queries.InsertRegistryEntry(
					context.Background(),
					InsertRegistryEntryParams{
						Name:      "test-server",
						SourceID:  regID,
						EntryType: EntryTypeMCP,
						CreatedAt: &createdAt,
						UpdatedAt: &createdAt,
					},
				)
				require.NoError(t, err)

				invalidEntryID := uuid.New()
				_, err = queries.InsertEntryVersion(
					context.Background(),
					InsertEntryVersionParams{
						EntryID:   invalidEntryID,
						Version:   "1.0.0",
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

			entryID := tc.setupFunc(t, queries, regID)
			tc.scenarioFunc(t, queries, regID, entryID)
		})
	}
}

func TestListServersByName(t *testing.T) {
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
				versions, err := queries.ListServers(
					context.Background(),
					ListServersParams{
						Name: &serverName,
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
				entryID := createEntry(t, queries, regID, "test-server", &createdAt)
				versionID := createVersion(t, queries, entryID, "test-server", "1.0.0", nil, nil, &createdAt)
				createServer(t, queries, versionID)
				//nolint:goconst
				return "test-server"
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, serverName string) {
				versions, err := queries.ListServers(
					context.Background(),
					ListServersParams{
						Name: &serverName,
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
				entryID := createEntry(t, queries, regID, "test-server", &createdAt)
				for _, version := range []string{"1.0.0", "2.0.0", "3.0.0"} {
					createdAt = createdAt.Add(1 * time.Second)
					versionID := createVersion(t, queries, entryID, "test-server", version, nil, nil, &createdAt)
					createServer(t, queries, versionID)
				}
				return "test-server"
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, serverName string) {
				versions, err := queries.ListServers(
					context.Background(),
					ListServersParams{
						Name: &serverName,
						Size: 10,
					},
				)
				require.NoError(t, err)
				require.Len(t, versions, 3)
				require.Equal(t, serverName, versions[0].Name)
			},
		},
		{
			name: "list server versions with cursor pagination",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) string {
				createdAt := time.Now().UTC().Add(-1 * time.Minute)
				entryID := createEntry(t, queries, regID, "test-server", &createdAt)
				for _, version := range []string{"1.0.0", "2.0.0", "3.0.0"} {
					createdAt = createdAt.Add(1 * time.Second)
					versionID := createVersion(t, queries, entryID, "test-server", version, nil, nil, &createdAt)
					createServer(t, queries, versionID)
				}
				return "test-server"
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, serverName string) {
				// Get all versions
				allVersions, err := queries.ListServers(
					context.Background(),
					ListServersParams{
						Name: &serverName,
						Size: 10,
					},
				)
				require.NoError(t, err)
				require.Len(t, allVersions, 3)
				assert.Equal(t, "1.0.0", allVersions[0].Version)
				assert.Equal(t, "2.0.0", allVersions[1].Version)
				assert.Equal(t, "3.0.0", allVersions[2].Version)

				// Use cursor to skip past first version
				cursorName := allVersions[0].Name
				cursorVersion := allVersions[0].Version
				nextVersions, err := queries.ListServers(
					context.Background(),
					ListServersParams{
						Name:          &serverName,
						CursorName:    &cursorName,
						CursorVersion: &cursorVersion,
						Size:          10,
					},
				)
				require.NoError(t, err)
				assert.Len(t, nextVersions, 2)
				assert.Equal(t, "2.0.0", nextVersions[0].Version)
				assert.Equal(t, "3.0.0", nextVersions[1].Version)
			},
		},
		{
			name: "list server versions with limit",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) string {
				createdAt := time.Now().UTC().Add(-1 * time.Minute)
				entryID := createEntry(t, queries, regID, "test-server", &createdAt)
				for _, version := range []string{"1.0.0", "2.0.0", "3.0.0"} {
					createdAt = createdAt.Add(1 * time.Second)
					versionID := createVersion(t, queries, entryID, "test-server", version, nil, nil, &createdAt)
					createServer(t, queries, versionID)
				}
				return "test-server"
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, serverName string) {
				versions, err := queries.ListServers(
					context.Background(),
					ListServersParams{
						Name: &serverName,
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
				entryID := createEntry(t, queries, regID, "test-server", &createdAt)
				versionID := createVersion(t, queries, entryID, "test-server", "1.0.0", nil, nil, &createdAt)
				createServer(t, queries, versionID)
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
				require.Equal(t, "git", servers[0].RegistryType)
			},
		},
		{
			name: "list multiple servers",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) {
				createdAt := time.Now().UTC()
				entryID := createEntry(t, queries, regID, "test-server", &createdAt)
				for _, version := range []string{"1.0.0", "2.0.0"} {
					createdAt = createdAt.Add(1 * time.Second)
					versionID := createVersion(t, queries, entryID, "test-server", version, nil, nil, &createdAt)
					createServer(t, queries, versionID)
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
				entryID := createEntry(t, queries, regID, "test-server", &createdAt)
				for _, version := range []string{"1.0.0", "2.0.0"} {
					createdAt = createdAt.Add(1 * time.Second)
					versionID := createVersion(t, queries, entryID, "test-server", version, nil, nil, &createdAt)
					createServer(t, queries, versionID)
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
				entryID := createEntry(t, queries, regID, "test-server", &createdAt)
				versionID := createVersion(t, queries, entryID, "test-server", "1.0.0", nil, nil, &createdAt)
				createServer(t, queries, versionID)

				_, err := queries.UpsertLatestServerVersion(
					context.Background(),
					UpsertLatestServerVersionParams{
						SourceID:  regID,
						Name:      "test-server",
						Version:   "1.0.0",
						VersionID: versionID,
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
				entryID := createEntry(t, queries, regID, "test-server", &createdAt)
				for _, version := range []string{"1.0.0", "2.0.0"} {
					createdAt = createdAt.Add(1 * time.Second)
					versionID := createVersion(t, queries, entryID, "test-server", version, nil, nil, &createdAt)
					createServer(t, queries, versionID)
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
		{
			name: "list servers filtered by specific version",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) {
				createdAt := time.Now().UTC().Add(-1 * time.Minute)
				entryID := createEntry(t, queries, regID, "test-server", &createdAt)
				for _, version := range []string{"1.0.0", "2.0.0", "3.0.0"} {
					createdAt = createdAt.Add(1 * time.Second)
					versionID := createVersion(t, queries, entryID, "test-server", version, nil, nil, &createdAt)
					createServer(t, queries, versionID)
				}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries) {
				//nolint:goconst
				version := "2.0.0"
				servers, err := queries.ListServers(
					context.Background(),
					ListServersParams{
						Version: &version,
						Size:    10,
					},
				)
				require.NoError(t, err)
				require.Len(t, servers, 1)
				assert.Equal(t, "test-server", servers[0].Name)
				assert.Equal(t, "2.0.0", servers[0].Version)
			},
		},
		{
			name: "list servers filtered by latest version",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) {
				createdAt := time.Now().UTC().Add(-1 * time.Minute)
				entryID := createEntry(t, queries, regID, "test-server", &createdAt)
				var versionIDs []uuid.UUID
				for _, version := range []string{"1.0.0", "2.0.0"} {
					createdAt = createdAt.Add(1 * time.Second)
					versionID := createVersion(t, queries, entryID, "test-server", version, nil, nil, &createdAt)
					createServer(t, queries, versionID)
					versionIDs = append(versionIDs, versionID)
				}

				// Mark 2.0.0 as latest
				_, err := queries.UpsertLatestServerVersion(
					context.Background(),
					UpsertLatestServerVersionParams{
						SourceID:  regID,
						Name:      "test-server",
						Version:   "2.0.0",
						VersionID: versionIDs[1],
					},
				)
				require.NoError(t, err)
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries) {
				version := latestVersion
				servers, err := queries.ListServers(
					context.Background(),
					ListServersParams{
						Version: &version,
						Size:    10,
					},
				)
				require.NoError(t, err)
				require.Len(t, servers, 1)
				assert.Equal(t, "test-server", servers[0].Name)
				assert.Equal(t, "2.0.0", servers[0].Version)
				assert.True(t, servers[0].IsLatest)
			},
		},
		{
			name: "list servers filtered by version returns empty when no match",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) {
				createdAt := time.Now().UTC()
				entryID := createEntry(t, queries, regID, "test-server", &createdAt)
				versionID := createVersion(t, queries, entryID, "test-server", "1.0.0", nil, nil, &createdAt)
				createServer(t, queries, versionID)
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries) {
				version := "9.9.9"
				servers, err := queries.ListServers(
					context.Background(),
					ListServersParams{
						Version: &version,
						Size:    10,
					},
				)
				require.NoError(t, err)
				require.Empty(t, servers)
			},
		},
		{
			name: "list servers filtered by latest returns empty when no latest set",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) {
				createdAt := time.Now().UTC().Add(-1 * time.Minute)
				entryID := createEntry(t, queries, regID, "test-server", &createdAt)
				for _, version := range []string{"1.0.0", "2.0.0"} {
					createdAt = createdAt.Add(1 * time.Second)
					versionID := createVersion(t, queries, entryID, "test-server", version, nil, nil, &createdAt)
					createServer(t, queries, versionID)
				}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries) {
				version := latestVersion
				servers, err := queries.ListServers(
					context.Background(),
					ListServersParams{
						Version: &version,
						Size:    10,
					},
				)
				require.NoError(t, err)
				require.Empty(t, servers)
			},
		},
		{
			name: "list servers with nil version returns all versions",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) {
				createdAt := time.Now().UTC().Add(-1 * time.Minute)
				entryID := createEntry(t, queries, regID, "test-server", &createdAt)
				for _, version := range []string{"1.0.0", "2.0.0", "3.0.0"} {
					createdAt = createdAt.Add(1 * time.Second)
					versionID := createVersion(t, queries, entryID, "test-server", version, nil, nil, &createdAt)
					createServer(t, queries, versionID)
				}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries) {
				servers, err := queries.ListServers(
					context.Background(),
					ListServersParams{
						Version: nil,
						Size:    10,
					},
				)
				require.NoError(t, err)
				require.Len(t, servers, 3)
				assert.Equal(t, "1.0.0", servers[0].Version)
				assert.Equal(t, "2.0.0", servers[1].Version)
				assert.Equal(t, "3.0.0", servers[2].Version)
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
				entryID := createEntry(t, queries, regID, "test-server", &createdAt)
				versionID := createVersion(t, queries, entryID, "test-server", "1.0.0", nil, nil, &createdAt)
				serverID := createServer(t, queries, versionID)
				return []uuid.UUID{serverID}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, regID uuid.UUID, ids []uuid.UUID) {
				serverID, err := queries.UpsertLatestServerVersion(
					context.Background(),
					UpsertLatestServerVersionParams{
						SourceID:  regID,
						Name:      "test-server",
						Version:   "1.0.0",
						VersionID: ids[0],
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
				entryID := createEntry(t, queries, regID, "test-server", &createdAt)
				for _, version := range []string{"1.0.0", "2.0.0"} {
					createdAt = createdAt.Add(1 * time.Second)
					versionID := createVersion(t, queries, entryID, "test-server", version, nil, nil, &createdAt)
					serverID := createServer(t, queries, versionID)
					serverIDs = append(serverIDs, serverID)
				}

				// Set initial latest version
				latestServerID, err := queries.UpsertLatestServerVersion(
					context.Background(),
					UpsertLatestServerVersionParams{
						SourceID:  regID,
						Name:      "test-server",
						Version:   "1.0.0",
						VersionID: serverIDs[0],
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
						SourceID:  regID,
						Name:      "test-server",
						Version:   "2.0.0",
						VersionID: ids[0],
					},
				)
				require.NoError(t, err)
				require.NotNil(t, latestServerID)
				require.Equal(t, ids[0], latestServerID)
			},
		},
		{
			name: "upsert latest server version with invalid source_id",
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
						SourceID:  regID,
						Name:      "test-server",
						Version:   "1.0.0",
						VersionID: ids[0],
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
						SourceID:  regID,
						Name:      "test-server",
						Version:   "1.0.0",
						VersionID: ids[0],
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
				entryID := createEntry(t, queries, regID, "test-server", &createdAt)
				versionID := createVersion(t, queries, entryID, "test-server", "1.0.0", nil, nil, &createdAt)
				serverID := createServer(t, queries, versionID)
				return serverID
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, entryID uuid.UUID) {
				err := queries.InsertServerIcon(
					context.Background(),
					InsertServerIconParams{
						ServerID:  entryID,
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
				entryID := createEntry(t, queries, regID, "test-server", &createdAt)
				versionID := createVersion(t, queries, entryID, "test-server", "1.0.0", nil, nil, &createdAt)
				serverID := createServer(t, queries, versionID)
				return serverID
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, entryID uuid.UUID) {
				err := queries.InsertServerIcon(
					context.Background(),
					InsertServerIconParams{
						ServerID:  entryID,
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
				entryID := createEntry(t, queries, regID, "test-server", &createdAt)
				versionID := createVersion(t, queries, entryID, "test-server", "1.0.0", nil, nil, &createdAt)
				createServer(t, queries, versionID)

				// Insert initial icon
				err := queries.InsertServerIcon(
					context.Background(),
					InsertServerIconParams{
						ServerID:  versionID,
						SourceUri: "https://example.com/icon.png",
						MimeType:  "image/png",
						Theme:     IconThemeLIGHT,
					},
				)
				require.NoError(t, err)

				return versionID
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, entryID uuid.UUID) {
				err := queries.InsertServerIcon(
					context.Background(),
					InsertServerIconParams{
						ServerID:  entryID,
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
						ServerID:  entryID,
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
				entryID := createEntry(t, queries, regID, "test-server", &createdAt)
				versionID := createVersion(t, queries, entryID, "test-server", "1.0.0", nil, nil, &createdAt)
				createServer(t, queries, versionID)
				return versionID
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, entryID uuid.UUID) {
				err := queries.InsertServerPackage(
					context.Background(),
					InsertServerPackageParams{
						ServerID:       entryID,
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
				entryID := createEntry(t, queries, regID, "test-server", &createdAt)
				versionID := createVersion(t, queries, entryID, "test-server", "1.0.0", nil, nil, &createdAt)
				createServer(t, queries, versionID)
				return versionID
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, entryID uuid.UUID) {
				err := queries.InsertServerPackage(
					context.Background(),
					InsertServerPackageParams{
						ServerID:         entryID,
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
						ServerID:       entryID,
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
				entryID := createEntry(t, queries, regID, "test-server", &createdAt)
				versionID := createVersion(t, queries, entryID, "test-server", "1.0.0", nil, nil, &createdAt)
				createServer(t, queries, versionID)
				return versionID
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, entryID uuid.UUID) {
				err := queries.InsertServerRemote(
					context.Background(),
					InsertServerRemoteParams{
						ServerID:     entryID,
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
				entryID := createEntry(t, queries, regID, "test-server", &createdAt)
				versionID := createVersion(t, queries, entryID, "test-server", "1.0.0", nil, nil, &createdAt)
				createServer(t, queries, versionID)
				return versionID
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, entryID uuid.UUID) {
				err := queries.InsertServerRemote(
					context.Background(),
					InsertServerRemoteParams{
						ServerID:         entryID,
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
				entryID := createEntry(t, queries, regID, "test-server", &createdAt)
				versionID := createVersion(t, queries, entryID, "test-server", "1.0.0", nil, nil, &createdAt)
				createServer(t, queries, versionID)

				// Insert initial remote
				err := queries.InsertServerRemote(
					context.Background(),
					InsertServerRemoteParams{
						ServerID:         versionID,
						Transport:        "sse",
						TransportUrl:     "https://example.com/sse",
						TransportHeaders: []byte(`[{"name":"Old-Header: old"}]`),
					},
				)
				require.NoError(t, err)

				return versionID
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, entryID uuid.UUID) {
				// Attempt to insert duplicate remote with same primary key (server_id, transport, transport_url) - should fail
				err := queries.InsertServerRemote(
					context.Background(),
					InsertServerRemoteParams{
						ServerID:         entryID,
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
						ServerID:     entryID,
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
				entryID := createEntry(t, queries, regID, "test-server", &createdAt)
				versionID := createVersion(t, queries, entryID, "test-server", "1.0.0", nil, nil, &createdAt)
				createServer(t, queries, versionID)
				return []uuid.UUID{versionID}
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
				entryID := createEntry(t, queries, regID, "test-server", &createdAt)
				versionID := createVersion(t, queries, entryID, "test-server", "1.0.0", nil, nil, &createdAt)
				createServer(t, queries, versionID)

				err := queries.InsertServerPackage(
					context.Background(),
					InsertServerPackageParams{
						ServerID:       versionID,
						RegistryType:   "npm",
						PkgRegistryUrl: "https://registry.npmjs.org",
						PkgIdentifier:  "@test/package",
						PkgVersion:     "1.0.0",
						Transport:      "stdio",
					},
				)
				require.NoError(t, err)

				return []uuid.UUID{versionID}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, entryIDs []uuid.UUID) {
				packages, err := queries.ListServerPackages(
					context.Background(),
					entryIDs,
				)
				require.NoError(t, err)
				require.Len(t, packages, 1)
				require.Equal(t, entryIDs[0], packages[0].ServerID)
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
					eID := createEntry(t, queries, regID, fmt.Sprintf("test-server-%d", i+1), &createdAt)
					versionID := createVersion(t, queries, eID, fmt.Sprintf("test-server-%d", i+1), version, nil, nil, &createdAt)
					createServer(t, queries, versionID)
					entryIDs = append(entryIDs, versionID)

					vErr := queries.InsertServerPackage(
						context.Background(),
						InsertServerPackageParams{
							ServerID:       versionID,
							RegistryType:   "npm",
							PkgRegistryUrl: "https://registry.npmjs.org",
							PkgIdentifier:  "@test/package",
							PkgVersion:     version,
							Transport:      "stdio",
						},
					)
					require.NoError(t, vErr)
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
					eID := createEntry(t, queries, regID, name, &createdAt)
					versionID := createVersion(t, queries, eID, name, "1.0.0", nil, nil, &createdAt)
					createServer(t, queries, versionID)
					entryIDs = append(entryIDs, versionID)

					vErr := queries.InsertServerPackage(
						context.Background(),
						InsertServerPackageParams{
							ServerID:       versionID,
							RegistryType:   "npm",
							PkgRegistryUrl: "https://registry.npmjs.org",
							PkgIdentifier:  fmt.Sprintf("@test/package-%d", i+1),
							PkgVersion:     "1.0.0",
							Transport:      "stdio",
						},
					)
					require.NoError(t, vErr)
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
					entryIDMap[pkg.ServerID] = true
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
				entryID := createEntry(t, queries, regID, "test-server", &createdAt)
				versionID := createVersion(t, queries, entryID, "test-server", "1.0.0", nil, nil, &createdAt)
				createServer(t, queries, versionID)
				return []uuid.UUID{versionID}
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
				entryID := createEntry(t, queries, regID, "test-server", &createdAt)
				versionID := createVersion(t, queries, entryID, "test-server", "1.0.0", nil, nil, &createdAt)
				createServer(t, queries, versionID)

				err := queries.InsertServerRemote(
					context.Background(),
					InsertServerRemoteParams{
						ServerID:     versionID,
						Transport:    "sse",
						TransportUrl: "https://example.com/sse",
					},
				)
				require.NoError(t, err)

				return []uuid.UUID{versionID}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, entryIDs []uuid.UUID) {
				remotes, err := queries.ListServerRemotes(
					context.Background(),
					entryIDs,
				)
				require.NoError(t, err)
				require.Len(t, remotes, 1)
				require.Equal(t, entryIDs[0], remotes[0].ServerID)
				require.Equal(t, "sse", remotes[0].Transport)
				require.Equal(t, "https://example.com/sse", remotes[0].TransportUrl)
			},
		},
		{
			name: "list multiple server remotes for single server",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) []uuid.UUID {
				createdAt := time.Now().UTC()
				entryID := createEntry(t, queries, regID, "test-server", &createdAt)
				versionID := createVersion(t, queries, entryID, "test-server", "1.0.0", nil, nil, &createdAt)
				createServer(t, queries, versionID)

				remotes := []struct {
					transport string
					url       string
				}{
					{"sse", "https://example.com/sse1"},
					{"sse", "https://example.com/sse2"},
					{"http", "https://example.com/http"},
				}

				for _, remote := range remotes {
					rErr := queries.InsertServerRemote(
						context.Background(),
						InsertServerRemoteParams{
							ServerID:     versionID,
							Transport:    remote.transport,
							TransportUrl: remote.url,
						},
					)
					require.NoError(t, rErr)
				}

				return []uuid.UUID{versionID}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, entryIDs []uuid.UUID) {
				remotes, err := queries.ListServerRemotes(
					context.Background(),
					entryIDs,
				)
				require.NoError(t, err)
				require.Len(t, remotes, 3)
				require.Equal(t, entryIDs[0], remotes[0].ServerID)
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
					eID := createEntry(t, queries, regID, name, &createdAt)
					versionID := createVersion(t, queries, eID, name, "1.0.0", nil, nil, &createdAt)
					createServer(t, queries, versionID)
					entryIDs = append(entryIDs, versionID)

					vErr := queries.InsertServerRemote(
						context.Background(),
						InsertServerRemoteParams{
							ServerID:     versionID,
							Transport:    "sse",
							TransportUrl: fmt.Sprintf("https://example.com/sse-%d", i+1),
						},
					)
					require.NoError(t, vErr)
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
					entryIDMap[remote.ServerID] = true
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
				entryID := createEntry(t, queries, regID, "test-server", &createdAt)
				versionID := createVersion(t, queries, entryID, "test-server", "1.0.0", nil, nil, &createdAt)
				createServer(t, queries, versionID)

				//nolint:goconst
				return "test-server", "1.0.0"
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, serverName, version string) {
				serverRows, err := queries.GetServerVersion(
					context.Background(),
					GetServerVersionParams{
						Name:    serverName,
						Version: version,
					},
				)
				require.NoError(t, err)
				require.NotEmpty(t, serverRows)
				server := serverRows[0]
				require.Equal(t, serverName, server.Name)
				require.Equal(t, version, server.Version)
				require.Equal(t, "git", server.RegistryType)
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
				entryID := createEntry(t, queries, regID, "test-server", &createdAt)
				versionID := createVersion(t, queries, entryID, "test-server", "1.0.0", ptr.String("Test description"), ptr.String("Test Title"), &createdAt)
				serverID, err := queries.InsertServerVersion(
					context.Background(),
					InsertServerVersionParams{
						VersionID:           versionID,
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
				require.Equal(t, versionID, serverID)
				return "test-server", "1.0.0"
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, serverName, version string) {
				serverRows, err := queries.GetServerVersion(
					context.Background(),
					GetServerVersionParams{
						Name:    serverName,
						Version: version,
					},
				)
				require.NoError(t, err)
				require.NotEmpty(t, serverRows)
				server := serverRows[0]
				require.Equal(t, serverName, server.Name)
				require.Equal(t, version, server.Version)
				require.Equal(t, "git", server.RegistryType)
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
				entryID := createEntry(t, queries, regID, "test-server", &createdAt)
				versionID := createVersion(t, queries, entryID, "test-server", "1.0.0", nil, nil, &createdAt)
				createServer(t, queries, versionID)

				_, err := queries.UpsertLatestServerVersion(
					context.Background(),
					UpsertLatestServerVersionParams{
						SourceID:  regID,
						Name:      "test-server",
						Version:   "1.0.0",
						VersionID: versionID,
					},
				)
				require.NoError(t, err)
				return "test-server", "1.0.0"
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, serverName, version string) {
				serverRows, err := queries.GetServerVersion(
					context.Background(),
					GetServerVersionParams{
						Name:    serverName,
						Version: version,
					},
				)
				require.NoError(t, err)
				require.NotEmpty(t, serverRows)
				server := serverRows[0]
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
				entryID := createEntry(t, queries, regID, "test-server", &createdAt)

				// Create first version and mark it as latest
				versionID1 := createVersion(t, queries, entryID, "test-server", "1.0.0", nil, nil, &createdAt)
				createServer(t, queries, versionID1)

				_, err := queries.UpsertLatestServerVersion(
					context.Background(),
					UpsertLatestServerVersionParams{
						SourceID:  regID,
						Name:      "test-server",
						Version:   "1.0.0",
						VersionID: versionID1,
					},
				)
				require.NoError(t, err)

				// Create second version (not marked as latest)
				createdAt2 := createdAt.Add(1 * time.Second)
				versionID2 := createVersion(t, queries, entryID, "test-server", "2.0.0", nil, nil, &createdAt2)
				createServer(t, queries, versionID2)
				return "test-server", "2.0.0"
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, serverName, version string) {
				serverRows, err := queries.GetServerVersion(
					context.Background(),
					GetServerVersionParams{
						Name:    serverName,
						Version: version,
					},
				)
				require.NoError(t, err)
				require.NotEmpty(t, serverRows)
				server := serverRows[0]
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
				serverRows, err := queries.GetServerVersion(
					context.Background(),
					GetServerVersionParams{
						Name:    serverName,
						Version: version,
					},
				)
				require.NoError(t, err)
				require.Empty(t, serverRows)
			},
		},
		{
			name: "get server version with wrong version",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) (string, string) {
				createdAt := time.Now().UTC()
				entryID := createEntry(t, queries, regID, "test-server", &createdAt)
				versionID := createVersion(t, queries, entryID, "test-server", "1.0.0", nil, nil, &createdAt)
				createServer(t, queries, versionID)
				return "test-server", "2.0.0"
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, serverName, version string) {
				serverRows, err := queries.GetServerVersion(
					context.Background(),
					GetServerVersionParams{
						Name:    serverName,
						Version: version,
					},
				)
				require.NoError(t, err)
				require.Empty(t, serverRows)
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
