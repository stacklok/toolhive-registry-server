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

const testPluginVersion = "1.0.0"

//nolint:thelper // We want to see these lines in the test output
func createPluginEntry(
	t *testing.T,
	queries *Queries,
	regID uuid.UUID,
	name, version string,
	description, title *string,
) uuid.UUID {
	createdAt := time.Now().UTC()
	entryID, err := queries.InsertRegistryEntry(
		context.Background(),
		InsertRegistryEntryParams{
			Name:      name,
			SourceID:  regID,
			EntryType: EntryTypePLUGIN,
			CreatedAt: &createdAt,
			UpdatedAt: &createdAt,
		},
	)
	require.NoError(t, err)

	versionID, err := queries.InsertEntryVersion(
		context.Background(),
		InsertEntryVersionParams{
			EntryID:     entryID,
			Name:        name,
			Version:     version,
			Title:       title,
			Description: description,
			CreatedAt:   &createdAt,
			UpdatedAt:   &createdAt,
		},
	)
	require.NoError(t, err)
	return versionID
}

//nolint:thelper // We want to see these lines in the test output
func insertPlugin(
	t *testing.T,
	queries *Queries,
	versionID uuid.UUID,
	namespace string,
) uuid.UUID {
	pluginEntryID, err := queries.InsertPluginVersion(
		context.Background(),
		InsertPluginVersionParams{
			VersionID:     versionID,
			Namespace:     namespace,
			Status:        NullPluginStatus{PluginStatus: PluginStatusACTIVE, Valid: true},
			Repository:    []byte(`{}`),
			Icons:         []byte(`[]`),
			Metadata:      []byte(`{}`),
			ExtensionMeta: []byte(`{}`),
		},
	)
	require.NoError(t, err)
	return pluginEntryID
}

func TestInsertPluginVersion(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		setupFunc    func(t *testing.T, queries *Queries, regID uuid.UUID)
		scenarioFunc func(t *testing.T, queries *Queries, regID uuid.UUID)
	}{
		{
			name: "insert plugin version with minimal fields",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries, _ uuid.UUID) {},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) {
				versionID := createPluginEntry(t, queries, regID, "test-plugin", testPluginVersion, nil, nil)

				pluginEntryID, err := queries.InsertPluginVersion(
					context.Background(),
					InsertPluginVersionParams{
						VersionID:     versionID,
						Namespace:     "test-namespace",
						Repository:    []byte(`{}`),
						Icons:         []byte(`[]`),
						Metadata:      []byte(`{}`),
						ExtensionMeta: []byte(`{}`),
					},
				)
				require.NoError(t, err)
				require.Equal(t, versionID, pluginEntryID)
			},
		},
		{
			name: "insert plugin version with all fields",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries, _ uuid.UUID) {},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) {
				versionID := createPluginEntry(t, queries, regID, "test-plugin", testPluginVersion,
					ptr.String("A test plugin"), ptr.String("Test Plugin"))

				pluginEntryID, err := queries.InsertPluginVersion(
					context.Background(),
					InsertPluginVersionParams{
						VersionID:     versionID,
						Namespace:     "test-namespace",
						Status:        NullPluginStatus{PluginStatus: PluginStatusACTIVE, Valid: true},
						License:       ptr.String("MIT"),
						Repository:    []byte(`{"url":"https://github.com/test/repo"}`),
						Icons:         []byte(`[{"src":"icon.png"}]`),
						Metadata:      []byte(`{"key":"value"}`),
						ExtensionMeta: []byte(`{"ext":"meta"}`),
					},
				)
				require.NoError(t, err)
				require.Equal(t, versionID, pluginEntryID)
			},
		},
		{
			name: "insert duplicate plugin version fails",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) {
				versionID := createPluginEntry(t, queries, regID, "test-plugin", testPluginVersion, nil, nil)
				insertPlugin(t, queries, versionID, "test-namespace")
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) {
				// Look up the registry_entry to get its ID for the duplicate version insert
				createdAt := time.Now().UTC()
				entryID, err := queries.InsertRegistryEntry(
					context.Background(),
					InsertRegistryEntryParams{
						Name:      "test-plugin-dup",
						SourceID:  regID,
						EntryType: EntryTypePLUGIN,
						CreatedAt: &createdAt,
						UpdatedAt: &createdAt,
					},
				)
				require.NoError(t, err)

				_, err = queries.InsertEntryVersion(
					context.Background(),
					InsertEntryVersionParams{
						EntryID:   entryID,
						Name:      "test-plugin-dup",
						Version:   testPluginVersion,
						CreatedAt: &createdAt,
						UpdatedAt: &createdAt,
					},
				)
				require.NoError(t, err)

				// Inserting a duplicate entry_version (same entry_id+version) should fail
				_, err = queries.InsertEntryVersion(
					context.Background(),
					InsertEntryVersionParams{
						EntryID:   entryID,
						Name:      "test-plugin-dup",
						Version:   testPluginVersion,
						CreatedAt: &createdAt,
						UpdatedAt: &createdAt,
					},
				)
				require.Error(t, err)
			},
		},
		{
			name: "insert plugin version with invalid entry_id",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries, _ uuid.UUID) {},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, _ uuid.UUID) {
				_, err := queries.InsertPluginVersion(
					context.Background(),
					InsertPluginVersionParams{
						VersionID:     uuid.New(),
						Namespace:     "test-namespace",
						Repository:    []byte(`{}`),
						Icons:         []byte(`[]`),
						Metadata:      []byte(`{}`),
						ExtensionMeta: []byte(`{}`),
					},
				)
				require.Error(t, err)
			},
		},
		{
			name: "insert plugin version defaults status to ACTIVE when not provided",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries, _ uuid.UUID) {},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) {
				versionID := createPluginEntry(t, queries, regID, "test-plugin", testPluginVersion, nil, nil)

				pluginEntryID, err := queries.InsertPluginVersion(
					context.Background(),
					InsertPluginVersionParams{
						VersionID:     versionID,
						Namespace:     "test-namespace",
						Status:        NullPluginStatus{Valid: false},
						Repository:    []byte(`{}`),
						Icons:         []byte(`[]`),
						Metadata:      []byte(`{}`),
						ExtensionMeta: []byte(`{}`),
					},
				)
				require.NoError(t, err)

				// Verify the status defaulted to ACTIVE by fetching
				pluginRows, err := queries.GetPluginVersion(
					context.Background(),
					GetPluginVersionParams{
						RegistryID: getRegistryID(t, queries, "test-registry"),
						Name:       "test-plugin",
						Version:    testPluginVersion,
						Size:       10,
					},
				)
				require.NoError(t, err)
				require.NotEmpty(t, pluginRows)
				require.Equal(t, pluginEntryID, pluginRows[0].PluginVersionID)
				require.Equal(t, PluginStatusACTIVE, pluginRows[0].Status)
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

func TestInsertPluginVersionForSync(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		setupFunc    func(t *testing.T, queries *Queries, regID uuid.UUID)
		scenarioFunc func(t *testing.T, queries *Queries, regID uuid.UUID)
	}{
		{
			name: "insert plugin version for sync with minimal fields",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries, _ uuid.UUID) {},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) {
				versionID := createPluginEntry(t, queries, regID, "test-plugin", testPluginVersion, nil, nil)

				pluginEntryID, err := queries.InsertPluginVersionForSync(
					context.Background(),
					InsertPluginVersionForSyncParams{
						VersionID:     versionID,
						Namespace:     "test-namespace",
						Repository:    []byte(`{}`),
						Icons:         []byte(`[]`),
						Metadata:      []byte(`{}`),
						ExtensionMeta: []byte(`{}`),
					},
				)
				require.NoError(t, err)
				require.Equal(t, versionID, pluginEntryID)
			},
		},
		{
			name: "insert plugin version for sync with all fields",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries, _ uuid.UUID) {},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) {
				versionID := createPluginEntry(t, queries, regID, "test-plugin", testPluginVersion,
					ptr.String("Sync plugin"), ptr.String("Sync Plugin Title"))

				pluginEntryID, err := queries.InsertPluginVersionForSync(
					context.Background(),
					InsertPluginVersionForSyncParams{
						VersionID:     versionID,
						Namespace:     "sync-namespace",
						Status:        NullPluginStatus{PluginStatus: PluginStatusDEPRECATED, Valid: true},
						License:       ptr.String("Apache-2.0"),
						Repository:    []byte(`{"url":"https://github.com/sync/repo"}`),
						Icons:         []byte(`[{"src":"sync-icon.png"}]`),
						Metadata:      []byte(`{"sync":"true"}`),
						ExtensionMeta: []byte(`{"ext":"sync"}`),
					},
				)
				require.NoError(t, err)
				require.Equal(t, versionID, pluginEntryID)
			},
		},
		{
			name: "insert plugin version for sync with invalid entry_id",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries, _ uuid.UUID) {},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, _ uuid.UUID) {
				_, err := queries.InsertPluginVersionForSync(
					context.Background(),
					InsertPluginVersionForSyncParams{
						VersionID:     uuid.New(),
						Namespace:     "test-namespace",
						Repository:    []byte(`{}`),
						Icons:         []byte(`[]`),
						Metadata:      []byte(`{}`),
						ExtensionMeta: []byte(`{}`),
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

func TestUpsertPluginVersionForSync(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		setupFunc    func(t *testing.T, queries *Queries, regID uuid.UUID)
		scenarioFunc func(t *testing.T, queries *Queries, regID uuid.UUID)
	}{
		{
			name: "insert new plugin version via upsert",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries, _ uuid.UUID) {},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) {
				versionID := createPluginEntry(t, queries, regID, "test-plugin", testPluginVersion, nil, nil)

				pluginEntryID, err := queries.UpsertPluginVersionForSync(
					context.Background(),
					UpsertPluginVersionForSyncParams{
						VersionID:     versionID,
						Namespace:     "test-namespace",
						Status:        NullPluginStatus{PluginStatus: PluginStatusACTIVE, Valid: true},
						License:       ptr.String("MIT"),
						Repository:    []byte(`{}`),
						Icons:         []byte(`[]`),
						Metadata:      []byte(`{}`),
						ExtensionMeta: []byte(`{}`),
					},
				)
				require.NoError(t, err)
				require.Equal(t, versionID, pluginEntryID)
			},
		},
		{
			name: "update existing plugin version via upsert",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) {
				versionID := createPluginEntry(t, queries, regID, "test-plugin", testPluginVersion, nil, nil)
				insertPlugin(t, queries, versionID, "test-namespace")
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, _ uuid.UUID) {
				// Look up the entry_id for the existing plugin
				existingRows, err := queries.GetPluginVersion(
					context.Background(),
					GetPluginVersionParams{
						RegistryID: getRegistryID(t, queries, "test-registry"),
						Name:       "test-plugin",
						Version:    testPluginVersion,
						Size:       10,
					},
				)
				require.NoError(t, err)
				require.NotEmpty(t, existingRows)
				existing := existingRows[0]

				// Upsert should update the existing row
				pluginVersionID, err := queries.UpsertPluginVersionForSync(
					context.Background(),
					UpsertPluginVersionForSyncParams{
						VersionID:     existing.PluginVersionID,
						Namespace:     "test-namespace",
						Status:        NullPluginStatus{PluginStatus: PluginStatusDEPRECATED, Valid: true},
						License:       ptr.String("Apache-2.0"),
						Repository:    []byte(`{"url":"https://github.com/updated/repo"}`),
						Icons:         []byte(`[{"src":"updated-icon.png"}]`),
						Metadata:      []byte(`{"updated":"true"}`),
						ExtensionMeta: []byte(`{"ext":"updated"}`),
					},
				)
				require.NoError(t, err)
				require.Equal(t, existing.PluginVersionID, pluginVersionID)

				// Verify the update
				pluginRows, err := queries.GetPluginVersion(
					context.Background(),
					GetPluginVersionParams{
						RegistryID: getRegistryID(t, queries, "test-registry"),
						Name:       "test-plugin",
						Version:    testPluginVersion,
						Size:       10,
					},
				)
				require.NoError(t, err)
				require.NotEmpty(t, pluginRows)
				require.Equal(t, PluginStatusDEPRECATED, pluginRows[0].Status)
				require.NotNil(t, pluginRows[0].License)
				require.Equal(t, "Apache-2.0", *pluginRows[0].License)
			},
		},
		{
			name: "upsert plugin version with invalid entry_id",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries, _ uuid.UUID) {},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, _ uuid.UUID) {
				_, err := queries.UpsertPluginVersionForSync(
					context.Background(),
					UpsertPluginVersionForSyncParams{
						VersionID:     uuid.New(),
						Namespace:     "test-namespace",
						Repository:    []byte(`{}`),
						Icons:         []byte(`[]`),
						Metadata:      []byte(`{}`),
						ExtensionMeta: []byte(`{}`),
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

func TestGetPluginVersion(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		setupFunc    func(t *testing.T, queries *Queries, regID uuid.UUID) (string, string)
		scenarioFunc func(t *testing.T, queries *Queries, regID uuid.UUID, pluginName, version string)
	}{
		{
			name: "get plugin version with minimal fields",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) (string, string) {
				entryID := createPluginEntry(t, queries, regID, "test-plugin", testPluginVersion, nil, nil)
				insertPlugin(t, queries, entryID, "test-namespace")
				//nolint:goconst
				return "test-plugin", testPluginVersion
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, _ uuid.UUID, pluginName, version string) {
				pluginRows, err := queries.GetPluginVersion(
					context.Background(),
					GetPluginVersionParams{
						RegistryID: getRegistryID(t, queries, "test-registry"),
						Name:       pluginName,
						Version:    version,
						Size:       10,
					},
				)
				require.NoError(t, err)
				require.NotEmpty(t, pluginRows)
				plugin := pluginRows[0]
				require.Equal(t, pluginName, plugin.Name)
				require.Equal(t, version, plugin.Version)
				require.Equal(t, "git", plugin.RegistryType)
				require.Equal(t, "test-namespace", plugin.Namespace)
				require.Equal(t, PluginStatusACTIVE, plugin.Status)
				require.False(t, plugin.IsLatest)
				require.NotNil(t, plugin.ID)
				require.NotNil(t, plugin.CreatedAt)
			},
		},
		{
			name: "get plugin version with all fields",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) (string, string) {
				versionID := createPluginEntry(t, queries, regID, "test-plugin", testPluginVersion,
					ptr.String("A test plugin"), ptr.String("Test Plugin"))

				_, err := queries.InsertPluginVersion(
					context.Background(),
					InsertPluginVersionParams{
						VersionID:     versionID,
						Namespace:     "full-namespace",
						Status:        NullPluginStatus{PluginStatus: PluginStatusACTIVE, Valid: true},
						License:       ptr.String("MIT"),
						Repository:    []byte(`{"url":"https://github.com/test/repo"}`),
						Icons:         []byte(`[{"src":"icon.png"}]`),
						Metadata:      []byte(`{"key":"value"}`),
						ExtensionMeta: []byte(`{"ext":"meta"}`),
					},
				)
				require.NoError(t, err)
				return "test-plugin", testPluginVersion
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, _ uuid.UUID, pluginName, version string) {
				pluginRows, err := queries.GetPluginVersion(
					context.Background(),
					GetPluginVersionParams{
						RegistryID: getRegistryID(t, queries, "test-registry"),
						Name:       pluginName,
						Version:    version,
						Size:       10,
					},
				)
				require.NoError(t, err)
				require.NotEmpty(t, pluginRows)
				plugin := pluginRows[0]
				require.Equal(t, pluginName, plugin.Name)
				require.Equal(t, version, plugin.Version)
				require.NotNil(t, plugin.Description)
				require.Equal(t, "A test plugin", *plugin.Description)
				require.NotNil(t, plugin.Title)
				require.Equal(t, "Test Plugin", *plugin.Title)
				require.Equal(t, "full-namespace", plugin.Namespace)
				require.Equal(t, PluginStatusACTIVE, plugin.Status)
				require.NotNil(t, plugin.License)
				require.Equal(t, "MIT", *plugin.License)
				assert.JSONEq(t, `{"url":"https://github.com/test/repo"}`, string(plugin.Repository))
				assert.JSONEq(t, `[{"src":"icon.png"}]`, string(plugin.Icons))
				assert.JSONEq(t, `{"key":"value"}`, string(plugin.Metadata))
				assert.JSONEq(t, `{"ext":"meta"}`, string(plugin.ExtensionMeta))
			},
		},
		{
			name: "get plugin version marked as latest",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) (string, string) {
				versionID := createPluginEntry(t, queries, regID, "test-plugin", testPluginVersion, nil, nil)
				insertPlugin(t, queries, versionID, "test-namespace")

				_, err := queries.UpsertLatestPluginVersion(
					context.Background(),
					UpsertLatestPluginVersionParams{
						SourceID:  regID,
						Name:      "test-plugin",
						Version:   testPluginVersion,
						VersionID: versionID,
					},
				)
				require.NoError(t, err)
				return "test-plugin", testPluginVersion
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, _ uuid.UUID, pluginName, version string) {
				pluginRows, err := queries.GetPluginVersion(
					context.Background(),
					GetPluginVersionParams{
						RegistryID: getRegistryID(t, queries, "test-registry"),
						Name:       pluginName,
						Version:    version,
						Size:       10,
					},
				)
				require.NoError(t, err)
				require.NotEmpty(t, pluginRows)
				plugin := pluginRows[0]
				require.Equal(t, pluginName, plugin.Name)
				require.Equal(t, version, plugin.Version)
				require.True(t, plugin.IsLatest)
			},
		},
		{
			name: "get plugin version using latest alias",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) (string, string) {
				versionID := createPluginEntry(t, queries, regID, "test-plugin", "2.0.0", nil, nil)
				insertPlugin(t, queries, versionID, "test-namespace")

				_, err := queries.UpsertLatestPluginVersion(
					context.Background(),
					UpsertLatestPluginVersionParams{
						SourceID:  regID,
						Name:      "test-plugin",
						Version:   "2.0.0",
						VersionID: versionID,
					},
				)
				require.NoError(t, err)
				//nolint:goconst
				return "test-plugin", "latest"
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, _ uuid.UUID, pluginName, version string) {
				pluginRows, err := queries.GetPluginVersion(
					context.Background(),
					GetPluginVersionParams{
						RegistryID: getRegistryID(t, queries, "test-registry"),
						Name:       pluginName,
						Version:    version,
						Size:       10,
					},
				)
				require.NoError(t, err)
				require.NotEmpty(t, pluginRows)
				plugin := pluginRows[0]
				require.Equal(t, pluginName, plugin.Name)
				require.Equal(t, "2.0.0", plugin.Version)
				require.True(t, plugin.IsLatest)
			},
		},
		{
			name: "get plugin version filtered by source IDs",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) (string, string) {
				versionID := createPluginEntry(t, queries, regID, "test-plugin", testPluginVersion, nil, nil)
				insertPlugin(t, queries, versionID, "test-namespace")
				return "test-plugin", testPluginVersion
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, _ uuid.UUID, pluginName, version string) {
				regID := getRegistryID(t, queries, "test-registry")
				pluginRows, err := queries.GetPluginVersion(
					context.Background(),
					GetPluginVersionParams{
						Name:       pluginName,
						Version:    version,
						RegistryID: regID,
						Size:       10,
					},
				)
				require.NoError(t, err)
				require.NotEmpty(t, pluginRows)
				require.Equal(t, pluginName, pluginRows[0].Name)
				require.Equal(t, version, pluginRows[0].Version)

				// Wrong registry ID returns empty results
				wrongID := uuid.New()
				wrongRows, err := queries.GetPluginVersion(
					context.Background(),
					GetPluginVersionParams{
						Name:       pluginName,
						Version:    version,
						RegistryID: wrongID,
						Size:       10,
					},
				)
				require.NoError(t, err)
				require.Empty(t, wrongRows)
			},
		},
		{
			name: "get non-existent plugin version",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries, _ uuid.UUID) (string, string) {
				return "non-existent-plugin", testPluginVersion
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, _ uuid.UUID, pluginName, version string) {
				pluginRows, err := queries.GetPluginVersion(
					context.Background(),
					GetPluginVersionParams{
						RegistryID: getRegistryID(t, queries, "test-registry"),
						Name:       pluginName,
						Version:    version,
						Size:       10,
					},
				)
				require.NoError(t, err)
				require.Empty(t, pluginRows)
			},
		},
		{
			name: "get plugin version with wrong version",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) (string, string) {
				entryID := createPluginEntry(t, queries, regID, "test-plugin", testPluginVersion, nil, nil)
				insertPlugin(t, queries, entryID, "test-namespace")
				return "test-plugin", "9.9.9"
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, _ uuid.UUID, pluginName, version string) {
				pluginRows, err := queries.GetPluginVersion(
					context.Background(),
					GetPluginVersionParams{
						RegistryID: getRegistryID(t, queries, "test-registry"),
						Name:       pluginName,
						Version:    version,
						Size:       10,
					},
				)
				require.NoError(t, err)
				require.Empty(t, pluginRows)
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

			pluginName, version := tc.setupFunc(t, queries, regID)
			tc.scenarioFunc(t, queries, regID, pluginName, version)
		})
	}
}

func TestListPlugins(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		setupFunc    func(t *testing.T, queries *Queries, regID uuid.UUID)
		scenarioFunc func(t *testing.T, queries *Queries, regID uuid.UUID)
	}{
		{
			name: "no plugins",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries, _ uuid.UUID) {},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, _ uuid.UUID) {
				plugins, err := queries.ListPlugins(
					context.Background(),
					ListPluginsParams{
						RegistryID: getRegistryID(t, queries, "test-registry"),
						Size:       10,
					},
				)
				require.NoError(t, err)
				require.Empty(t, plugins)
			},
		},
		{
			name: "list single plugin",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) {
				entryID := createPluginEntry(t, queries, regID, "test-plugin", testPluginVersion, nil, nil)
				insertPlugin(t, queries, entryID, "test-namespace")
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, _ uuid.UUID) {
				plugins, err := queries.ListPlugins(
					context.Background(),
					ListPluginsParams{
						RegistryID: getRegistryID(t, queries, "test-registry"),
						Size:       10,
					},
				)
				require.NoError(t, err)
				require.Len(t, plugins, 1)
				require.Equal(t, "test-plugin", plugins[0].Name)
				require.Equal(t, testPluginVersion, plugins[0].Version)
				require.Equal(t, "test-namespace", plugins[0].Namespace)
				require.Equal(t, "git", plugins[0].RegistryType)
			},
		},
		{
			name: "list multiple plugins",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) {
				for _, name := range []string{"alpha-plugin", "beta-plugin", "gamma-plugin"} {
					entryID := createPluginEntry(t, queries, regID, name, testPluginVersion, nil, nil)
					insertPlugin(t, queries, entryID, "test-namespace")
				}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, _ uuid.UUID) {
				plugins, err := queries.ListPlugins(
					context.Background(),
					ListPluginsParams{
						RegistryID: getRegistryID(t, queries, "test-registry"),
						Size:       10,
					},
				)
				require.NoError(t, err)
				require.Len(t, plugins, 3)
				// Verify ordering by name ASC
				assert.Equal(t, "alpha-plugin", plugins[0].Name)
				assert.Equal(t, "beta-plugin", plugins[1].Name)
				assert.Equal(t, "gamma-plugin", plugins[2].Name)
			},
		},
		{
			name: "list plugins with cursor pagination",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) {
				for _, name := range []string{"alpha-plugin", "beta-plugin", "gamma-plugin"} {
					entryID := createPluginEntry(t, queries, regID, name, testPluginVersion, nil, nil)
					insertPlugin(t, queries, entryID, "test-namespace")
				}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, _ uuid.UUID) {
				regID := getRegistryID(t, queries, "test-registry")
				// Get first page
				allPlugins, err := queries.ListPlugins(
					context.Background(),
					ListPluginsParams{
						RegistryID: regID,
						Size:       10,
					},
				)
				require.NoError(t, err)
				require.Len(t, allPlugins, 3)

				// Use cursor to skip past first plugin
				cursorName := allPlugins[0].Name
				cursorVersion := allPlugins[0].Version
				plugins, err := queries.ListPlugins(
					context.Background(),
					ListPluginsParams{
						RegistryID:    regID,
						CursorName:    &cursorName,
						CursorVersion: &cursorVersion,
						Size:          10,
					},
				)
				require.NoError(t, err)
				require.Len(t, plugins, 2)
				assert.Equal(t, "beta-plugin", plugins[0].Name)
				assert.Equal(t, "gamma-plugin", plugins[1].Name)
			},
		},
		{
			name: "list plugins with limit",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) {
				for _, name := range []string{"alpha-plugin", "beta-plugin", "gamma-plugin"} {
					entryID := createPluginEntry(t, queries, regID, name, testPluginVersion, nil, nil)
					insertPlugin(t, queries, entryID, "test-namespace")
				}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, _ uuid.UUID) {
				plugins, err := queries.ListPlugins(
					context.Background(),
					ListPluginsParams{
						RegistryID: getRegistryID(t, queries, "test-registry"),
						Size:       2,
					},
				)
				require.NoError(t, err)
				require.Len(t, plugins, 2)
			},
		},
		{
			name: "list plugins filtered by namespace",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) {
				entryID1 := createPluginEntry(t, queries, regID, "plugin-a", testPluginVersion, nil, nil)
				insertPlugin(t, queries, entryID1, "namespace-1")

				entryID2 := createPluginEntry(t, queries, regID, "plugin-b", testPluginVersion, nil, nil)
				insertPlugin(t, queries, entryID2, "namespace-2")
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, _ uuid.UUID) {
				//nolint:goconst
				ns := "namespace-1"
				plugins, err := queries.ListPlugins(
					context.Background(),
					ListPluginsParams{
						RegistryID: getRegistryID(t, queries, "test-registry"),
						Namespace:  &ns,
						Size:       10,
					},
				)
				require.NoError(t, err)
				require.Len(t, plugins, 1)
				assert.Equal(t, "plugin-a", plugins[0].Name)
				assert.Equal(t, "namespace-1", plugins[0].Namespace)
			},
		},
		{
			name: "list plugins filtered by name",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) {
				for _, name := range []string{"plugin-a", "plugin-b", "plugin-c"} {
					entryID := createPluginEntry(t, queries, regID, name, testPluginVersion, nil, nil)
					insertPlugin(t, queries, entryID, "test-namespace")
				}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, _ uuid.UUID) {
				name := "plugin-b"
				plugins, err := queries.ListPlugins(
					context.Background(),
					ListPluginsParams{
						RegistryID: getRegistryID(t, queries, "test-registry"),
						Name:       &name,
						Size:       10,
					},
				)
				require.NoError(t, err)
				require.Len(t, plugins, 1)
				assert.Equal(t, "plugin-b", plugins[0].Name)
			},
		},
		{
			name: "list plugins with search",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) {
				entryID1 := createPluginEntry(t, queries, regID, "code-review", testPluginVersion,
					ptr.String("Automated code review tool"), ptr.String("Code Review"))
				insertPlugin(t, queries, entryID1, "test-namespace")

				entryID2 := createPluginEntry(t, queries, regID, "test-runner", testPluginVersion,
					ptr.String("Run unit tests"), ptr.String("Test Runner"))
				insertPlugin(t, queries, entryID2, "test-namespace")
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, _ uuid.UUID) {
				search := "code"
				plugins, err := queries.ListPlugins(
					context.Background(),
					ListPluginsParams{
						RegistryID: getRegistryID(t, queries, "test-registry"),
						Search:     &search,
						Size:       10,
					},
				)
				require.NoError(t, err)
				require.Len(t, plugins, 1)
				assert.Equal(t, "code-review", plugins[0].Name)
			},
		},
		{
			name: "list plugins with updated_since filter",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) {
				oldTime := time.Now().UTC().Add(-1 * time.Hour)
				recentTime := time.Now().UTC()

				oldEntryID, err := queries.InsertRegistryEntry(
					context.Background(),
					InsertRegistryEntryParams{
						Name:      "old-plugin",
						SourceID:  regID,
						EntryType: EntryTypePLUGIN,
						CreatedAt: &oldTime,
						UpdatedAt: &oldTime,
					},
				)
				require.NoError(t, err)
				versionID1, err := queries.InsertEntryVersion(
					context.Background(),
					InsertEntryVersionParams{
						EntryID:   oldEntryID,
						Name:      "old-plugin",
						Version:   testPluginVersion,
						CreatedAt: &oldTime,
						UpdatedAt: &oldTime,
					},
				)
				require.NoError(t, err)
				insertPlugin(t, queries, versionID1, "test-namespace")

				recentEntryID, err := queries.InsertRegistryEntry(
					context.Background(),
					InsertRegistryEntryParams{
						Name:      "recent-plugin",
						SourceID:  regID,
						EntryType: EntryTypePLUGIN,
						CreatedAt: &recentTime,
						UpdatedAt: &recentTime,
					},
				)
				require.NoError(t, err)
				versionID2, err := queries.InsertEntryVersion(
					context.Background(),
					InsertEntryVersionParams{
						EntryID:   recentEntryID,
						Name:      "recent-plugin",
						Version:   testPluginVersion,
						CreatedAt: &recentTime,
						UpdatedAt: &recentTime,
					},
				)
				require.NoError(t, err)
				insertPlugin(t, queries, versionID2, "test-namespace")
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, _ uuid.UUID) {
				since := time.Now().UTC().Add(-30 * time.Minute)
				plugins, err := queries.ListPlugins(
					context.Background(),
					ListPluginsParams{
						RegistryID:   getRegistryID(t, queries, "test-registry"),
						UpdatedSince: &since,
						Size:         10,
					},
				)
				require.NoError(t, err)
				require.Len(t, plugins, 1)
				assert.Equal(t, "recent-plugin", plugins[0].Name)
			},
		},
		{
			name: "list plugins filtered by registry name",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) {
				entryID := createPluginEntry(t, queries, regID, "test-plugin", testPluginVersion, nil, nil)
				insertPlugin(t, queries, entryID, "test-namespace")
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, _ uuid.UUID) {
				regID := getRegistryID(t, queries, "test-registry")
				plugins, err := queries.ListPlugins(
					context.Background(),
					ListPluginsParams{
						RegistryID: regID,
						Size:       10,
					},
				)
				require.NoError(t, err)
				require.Len(t, plugins, 1)

				// Wrong registry ID returns empty
				wrongID := uuid.New()
				plugins, err = queries.ListPlugins(
					context.Background(),
					ListPluginsParams{
						RegistryID: wrongID,
						Size:       10,
					},
				)
				require.NoError(t, err)
				require.Empty(t, plugins)
			},
		},
		{
			name: "list plugins with multiple versions ordered correctly",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) {
				createdAt := time.Now().UTC()
				entryID, err := queries.InsertRegistryEntry(
					context.Background(),
					InsertRegistryEntryParams{
						Name:      "test-plugin",
						SourceID:  regID,
						EntryType: EntryTypePLUGIN,
						CreatedAt: &createdAt,
						UpdatedAt: &createdAt,
					},
				)
				require.NoError(t, err)
				for _, version := range []string{testPluginVersion, "2.0.0", "3.0.0"} {
					versionID, vErr := queries.InsertEntryVersion(
						context.Background(),
						InsertEntryVersionParams{
							EntryID:   entryID,
							Name:      "test-plugin",
							Version:   version,
							CreatedAt: &createdAt,
							UpdatedAt: &createdAt,
						},
					)
					require.NoError(t, vErr)
					insertPlugin(t, queries, versionID, "test-namespace")
				}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, _ uuid.UUID) {
				plugins, err := queries.ListPlugins(
					context.Background(),
					ListPluginsParams{
						RegistryID: getRegistryID(t, queries, "test-registry"),
						Size:       10,
					},
				)
				require.NoError(t, err)
				require.Len(t, plugins, 3)
				// ORDER BY name ASC, version ASC
				assert.Equal(t, testPluginVersion, plugins[0].Version)
				assert.Equal(t, "2.0.0", plugins[1].Version)
				assert.Equal(t, "3.0.0", plugins[2].Version)
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

func TestUpsertLatestPluginVersion(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		setupFunc    func(t *testing.T, queries *Queries, regID uuid.UUID) []uuid.UUID
		scenarioFunc func(t *testing.T, queries *Queries, regID uuid.UUID, ids []uuid.UUID)
	}{
		{
			name: "insert latest plugin version",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) []uuid.UUID {
				entryID := createPluginEntry(t, queries, regID, "test-plugin", testPluginVersion, nil, nil)
				pluginID := insertPlugin(t, queries, entryID, "test-namespace")
				return []uuid.UUID{pluginID}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, regID uuid.UUID, ids []uuid.UUID) {
				latestID, err := queries.UpsertLatestPluginVersion(
					context.Background(),
					UpsertLatestPluginVersionParams{
						SourceID:  regID,
						Name:      "test-plugin",
						Version:   testPluginVersion,
						VersionID: ids[0],
					},
				)
				require.NoError(t, err)
				require.Equal(t, ids[0], latestID)
			},
		},
		{
			name: "update existing latest plugin version",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) []uuid.UUID {
				createdAt := time.Now().UTC()
				entryID, err := queries.InsertRegistryEntry(
					context.Background(),
					InsertRegistryEntryParams{
						Name:      "test-plugin",
						SourceID:  regID,
						EntryType: EntryTypePLUGIN,
						CreatedAt: &createdAt,
						UpdatedAt: &createdAt,
					},
				)
				require.NoError(t, err)

				var versionIDs []uuid.UUID
				for _, version := range []string{testPluginVersion, "2.0.0"} {
					versionID, vErr := queries.InsertEntryVersion(
						context.Background(),
						InsertEntryVersionParams{
							EntryID:   entryID,
							Name:      "test-plugin",
							Version:   version,
							CreatedAt: &createdAt,
							UpdatedAt: &createdAt,
						},
					)
					require.NoError(t, vErr)
					insertPlugin(t, queries, versionID, "test-namespace")
					versionIDs = append(versionIDs, versionID)
				}

				// Set initial latest version
				latestID, err := queries.UpsertLatestPluginVersion(
					context.Background(),
					UpsertLatestPluginVersionParams{
						SourceID:  regID,
						Name:      "test-plugin",
						Version:   testPluginVersion,
						VersionID: versionIDs[0],
					},
				)
				require.NoError(t, err)
				require.Equal(t, versionIDs[0], latestID)

				return versionIDs
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, regID uuid.UUID, ids []uuid.UUID) {
				// Update latest to version 2.0.0
				latestID, err := queries.UpsertLatestPluginVersion(
					context.Background(),
					UpsertLatestPluginVersionParams{
						SourceID:  regID,
						Name:      "test-plugin",
						Version:   "2.0.0",
						VersionID: ids[1],
					},
				)
				require.NoError(t, err)
				require.Equal(t, ids[1], latestID)
			},
		},
		{
			name: "upsert latest plugin version with invalid reg_id",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries, _ uuid.UUID) []uuid.UUID {
				return []uuid.UUID{uuid.New()}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, _ uuid.UUID, ids []uuid.UUID) {
				_, err := queries.UpsertLatestPluginVersion(
					context.Background(),
					UpsertLatestPluginVersionParams{
						SourceID:  uuid.New(),
						Name:      "test-plugin",
						Version:   testPluginVersion,
						VersionID: ids[0],
					},
				)
				require.Error(t, err)
			},
		},
		{
			name: "upsert latest plugin version with invalid entry_id",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries, _ uuid.UUID) []uuid.UUID {
				return []uuid.UUID{uuid.New()}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, regID uuid.UUID, ids []uuid.UUID) {
				_, err := queries.UpsertLatestPluginVersion(
					context.Background(),
					UpsertLatestPluginVersionParams{
						SourceID:  regID,
						Name:      "test-plugin",
						Version:   testPluginVersion,
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

func TestDeleteOrphanedPlugins(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		setupFunc    func(t *testing.T, queries *Queries, regID uuid.UUID) []uuid.UUID
		scenarioFunc func(t *testing.T, queries *Queries, regID uuid.UUID, entryIDs []uuid.UUID)
	}{
		{
			name: "delete orphaned plugins keeping specified IDs",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) []uuid.UUID {
				var entryIDs []uuid.UUID
				for _, name := range []string{"keep-plugin", "orphan-plugin-1", "orphan-plugin-2"} {
					entryID := createPluginEntry(t, queries, regID, name, testPluginVersion, nil, nil)
					insertPlugin(t, queries, entryID, "test-namespace")
					entryIDs = append(entryIDs, entryID)
				}
				return entryIDs
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, regID uuid.UUID, entryIDs []uuid.UUID) {
				// Keep only the first plugin
				err := queries.DeleteOrphanedEntryVersions(
					context.Background(),
					DeleteOrphanedEntryVersionsParams{
						SourceID:  regID,
						EntryType: EntryTypePLUGIN,
						KeepIds:   []uuid.UUID{entryIDs[0]},
					},
				)
				require.NoError(t, err)

				// Verify only the kept plugin remains
				plugins, err := queries.ListPlugins(
					context.Background(),
					ListPluginsParams{
						RegistryID: getRegistryID(t, queries, "test-registry"),
						Size:       10,
					},
				)
				require.NoError(t, err)
				require.Len(t, plugins, 1)
				assert.Equal(t, "keep-plugin", plugins[0].Name)
			},
		},
		{
			name: "delete orphaned plugins with empty keep list deletes all",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) []uuid.UUID {
				var entryIDs []uuid.UUID
				for _, name := range []string{"plugin-a", "plugin-b"} {
					entryID := createPluginEntry(t, queries, regID, name, testPluginVersion, nil, nil)
					insertPlugin(t, queries, entryID, "test-namespace")
					entryIDs = append(entryIDs, entryID)
				}
				return entryIDs
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, regID uuid.UUID, _ []uuid.UUID) {
				err := queries.DeleteOrphanedEntryVersions(
					context.Background(),
					DeleteOrphanedEntryVersionsParams{
						SourceID:  regID,
						EntryType: EntryTypePLUGIN,
						KeepIds:   []uuid.UUID{},
					},
				)
				require.NoError(t, err)

				plugins, err := queries.ListPlugins(
					context.Background(),
					ListPluginsParams{
						RegistryID: getRegistryID(t, queries, "test-registry"),
						Size:       10,
					},
				)
				require.NoError(t, err)
				require.Empty(t, plugins)
			},
		},
		{
			name: "delete orphaned plugins when none exist",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries, _ uuid.UUID) []uuid.UUID {
				return nil
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, regID uuid.UUID, _ []uuid.UUID) {
				err := queries.DeleteOrphanedEntryVersions(
					context.Background(),
					DeleteOrphanedEntryVersionsParams{
						SourceID:  regID,
						EntryType: EntryTypePLUGIN,
						KeepIds:   []uuid.UUID{},
					},
				)
				require.NoError(t, err)
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

			entryIDs := tc.setupFunc(t, queries, regID)
			tc.scenarioFunc(t, queries, regID, entryIDs)
		})
	}
}

func TestInsertPluginGitPackage(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		setupFunc    func(t *testing.T, queries *Queries, regID uuid.UUID) uuid.UUID
		scenarioFunc func(t *testing.T, queries *Queries, pluginID uuid.UUID)
	}{
		{
			name: "insert git package with minimal fields",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) uuid.UUID {
				entryID := createPluginEntry(t, queries, regID, "test-plugin", testPluginVersion, nil, nil)
				pluginVersionID := insertPlugin(t, queries, entryID, "test-namespace")
				return pluginVersionID
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, pluginVersionID uuid.UUID) {
				err := queries.InsertPluginGitPackage(
					context.Background(),
					InsertPluginGitPackageParams{
						PluginID: pluginVersionID,
						Url:      "https://github.com/test/plugin-repo",
					},
				)
				require.NoError(t, err)
			},
		},
		{
			name: "insert git package with all fields",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) uuid.UUID {
				entryID := createPluginEntry(t, queries, regID, "test-plugin", testPluginVersion, nil, nil)
				pluginVersionID := insertPlugin(t, queries, entryID, "test-namespace")
				return pluginVersionID
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, pluginVersionID uuid.UUID) {
				err := queries.InsertPluginGitPackage(
					context.Background(),
					InsertPluginGitPackageParams{
						PluginID:  pluginVersionID,
						Url:       "https://github.com/test/plugin-repo",
						Ref:       ptr.String("refs/tags/v1.0.0"),
						CommitSha: ptr.String("abc123def456"),
						Subfolder: ptr.String("plugins/my-plugin"),
					},
				)
				require.NoError(t, err)
			},
		},
		{
			name: "insert git package with invalid plugin_entry_id",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries, _ uuid.UUID) uuid.UUID {
				return uuid.New()
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, pluginVersionID uuid.UUID) {
				err := queries.InsertPluginGitPackage(
					context.Background(),
					InsertPluginGitPackageParams{
						PluginID: pluginVersionID,
						Url:      "https://github.com/test/plugin-repo",
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
			tc.scenarioFunc(t, queries, entryID)
		})
	}
}

func TestInsertPluginOciPackage(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		setupFunc    func(t *testing.T, queries *Queries, regID uuid.UUID) uuid.UUID
		scenarioFunc func(t *testing.T, queries *Queries, pluginVersionID uuid.UUID)
	}{
		{
			name: "insert oci package with minimal fields",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) uuid.UUID {
				entryID := createPluginEntry(t, queries, regID, "test-plugin", testPluginVersion, nil, nil)
				pluginVersionID := insertPlugin(t, queries, entryID, "test-namespace")
				return pluginVersionID
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, pluginVersionID uuid.UUID) {
				err := queries.InsertPluginOciPackage(
					context.Background(),
					InsertPluginOciPackageParams{
						PluginID:   pluginVersionID,
						Identifier: "ghcr.io/test/plugin:v1.0.0",
					},
				)
				require.NoError(t, err)
			},
		},
		{
			name: "insert oci package with all fields",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) uuid.UUID {
				entryID := createPluginEntry(t, queries, regID, "test-plugin", testPluginVersion, nil, nil)
				pluginVersionID := insertPlugin(t, queries, entryID, "test-namespace")
				return pluginVersionID
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, pluginVersionID uuid.UUID) {
				err := queries.InsertPluginOciPackage(
					context.Background(),
					InsertPluginOciPackageParams{
						PluginID:   pluginVersionID,
						Identifier: "ghcr.io/test/plugin:v1.0.0",
						Digest:     ptr.String("sha256:abcdef1234567890"),
						MediaType:  ptr.String("application/vnd.oci.image.manifest.v1+json"),
					},
				)
				require.NoError(t, err)
			},
		},
		{
			name: "insert oci package with invalid plugin_entry_id",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries, _ uuid.UUID) uuid.UUID {
				return uuid.New()
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, pluginVersionID uuid.UUID) {
				err := queries.InsertPluginOciPackage(
					context.Background(),
					InsertPluginOciPackageParams{
						PluginID:   pluginVersionID,
						Identifier: "ghcr.io/test/plugin:v1.0.0",
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
			tc.scenarioFunc(t, queries, entryID)
		})
	}
}

func TestListPluginGitPackages(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		setupFunc    func(t *testing.T, queries *Queries, regID uuid.UUID) []uuid.UUID
		scenarioFunc func(t *testing.T, queries *Queries, entryIDs []uuid.UUID)
	}{
		{
			name: "no git packages",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) []uuid.UUID {
				entryID := createPluginEntry(t, queries, regID, "test-plugin", testPluginVersion, nil, nil)
				insertPlugin(t, queries, entryID, "test-namespace")
				return []uuid.UUID{entryID}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, entryIDs []uuid.UUID) {
				packages, err := queries.ListPluginGitPackages(context.Background(), entryIDs)
				require.NoError(t, err)
				require.Empty(t, packages)
			},
		},
		{
			name: "list single git package",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) []uuid.UUID {
				entryID := createPluginEntry(t, queries, regID, "test-plugin", testPluginVersion, nil, nil)
				pluginVersionID := insertPlugin(t, queries, entryID, "test-namespace")

				err := queries.InsertPluginGitPackage(
					context.Background(),
					InsertPluginGitPackageParams{
						PluginID:  pluginVersionID,
						Url:       "https://github.com/test/plugin-repo",
						Ref:       ptr.String("refs/tags/v1.0.0"),
						CommitSha: ptr.String("abc123"),
						Subfolder: ptr.String("plugins/my-plugin"),
					},
				)
				require.NoError(t, err)
				return []uuid.UUID{entryID}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, entryIDs []uuid.UUID) {
				packages, err := queries.ListPluginGitPackages(context.Background(), entryIDs)
				require.NoError(t, err)
				require.Len(t, packages, 1)
				assert.Equal(t, entryIDs[0], packages[0].PluginID)
				assert.Equal(t, "https://github.com/test/plugin-repo", packages[0].Url)
				require.NotNil(t, packages[0].Ref)
				assert.Equal(t, "refs/tags/v1.0.0", *packages[0].Ref)
				require.NotNil(t, packages[0].CommitSha)
				assert.Equal(t, "abc123", *packages[0].CommitSha)
				require.NotNil(t, packages[0].Subfolder)
				assert.Equal(t, "plugins/my-plugin", *packages[0].Subfolder)
			},
		},
		{
			name: "list git packages for multiple plugins",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) []uuid.UUID {
				var entryIDs []uuid.UUID
				for i, name := range []string{"plugin-1", "plugin-2"} {
					entryID := createPluginEntry(t, queries, regID, name, testPluginVersion, nil, nil)
					pluginVersionID := insertPlugin(t, queries, entryID, "test-namespace")

					err := queries.InsertPluginGitPackage(
						context.Background(),
						InsertPluginGitPackageParams{
							PluginID: pluginVersionID,
							Url:      fmt.Sprintf("https://github.com/test/plugin-repo-%d", i+1),
						},
					)
					require.NoError(t, err)
					entryIDs = append(entryIDs, entryID)
				}
				return entryIDs
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, entryIDs []uuid.UUID) {
				packages, err := queries.ListPluginGitPackages(context.Background(), entryIDs)
				require.NoError(t, err)
				require.Len(t, packages, 2)

				entryIDMap := make(map[uuid.UUID]bool)
				for _, pkg := range packages {
					entryIDMap[pkg.PluginID] = true
				}
				require.True(t, entryIDMap[entryIDs[0]])
				require.True(t, entryIDMap[entryIDs[1]])
			},
		},
		{
			name: "list git packages with non-existent entry IDs",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries, _ uuid.UUID) []uuid.UUID {
				return []uuid.UUID{uuid.New(), uuid.New()}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, entryIDs []uuid.UUID) {
				packages, err := queries.ListPluginGitPackages(context.Background(), entryIDs)
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

			entryIDs := tc.setupFunc(t, queries, regID)
			tc.scenarioFunc(t, queries, entryIDs)
		})
	}
}

func TestListPluginOciPackages(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		setupFunc    func(t *testing.T, queries *Queries, regID uuid.UUID) []uuid.UUID
		scenarioFunc func(t *testing.T, queries *Queries, entryIDs []uuid.UUID)
	}{
		{
			name: "no oci packages",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) []uuid.UUID {
				entryID := createPluginEntry(t, queries, regID, "test-plugin", testPluginVersion, nil, nil)
				insertPlugin(t, queries, entryID, "test-namespace")
				return []uuid.UUID{entryID}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, entryIDs []uuid.UUID) {
				packages, err := queries.ListPluginOciPackages(context.Background(), entryIDs)
				require.NoError(t, err)
				require.Empty(t, packages)
			},
		},
		{
			name: "list single oci package",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) []uuid.UUID {
				entryID := createPluginEntry(t, queries, regID, "test-plugin", testPluginVersion, nil, nil)
				pluginVersionID := insertPlugin(t, queries, entryID, "test-namespace")

				err := queries.InsertPluginOciPackage(
					context.Background(),
					InsertPluginOciPackageParams{
						PluginID:   pluginVersionID,
						Identifier: "ghcr.io/test/plugin:v1.0.0",
						Digest:     ptr.String("sha256:abcdef1234567890"),
						MediaType:  ptr.String("application/vnd.oci.image.manifest.v1+json"),
					},
				)
				require.NoError(t, err)
				return []uuid.UUID{entryID}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, entryIDs []uuid.UUID) {
				packages, err := queries.ListPluginOciPackages(context.Background(), entryIDs)
				require.NoError(t, err)
				require.Len(t, packages, 1)
				assert.Equal(t, entryIDs[0], packages[0].PluginID)
				assert.Equal(t, "ghcr.io/test/plugin:v1.0.0", packages[0].Identifier)
				require.NotNil(t, packages[0].Digest)
				assert.Equal(t, "sha256:abcdef1234567890", *packages[0].Digest)
				require.NotNil(t, packages[0].MediaType)
				assert.Equal(t, "application/vnd.oci.image.manifest.v1+json", *packages[0].MediaType)
			},
		},
		{
			name: "list oci packages for multiple plugins",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) []uuid.UUID {
				var entryIDs []uuid.UUID
				for i, name := range []string{"plugin-1", "plugin-2"} {
					entryID := createPluginEntry(t, queries, regID, name, testPluginVersion, nil, nil)
					pluginVersionID := insertPlugin(t, queries, entryID, "test-namespace")

					err := queries.InsertPluginOciPackage(
						context.Background(),
						InsertPluginOciPackageParams{
							PluginID:   pluginVersionID,
							Identifier: fmt.Sprintf("ghcr.io/test/plugin-%d:v1.0.0", i+1),
						},
					)
					require.NoError(t, err)
					entryIDs = append(entryIDs, entryID)
				}
				return entryIDs
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, entryIDs []uuid.UUID) {
				packages, err := queries.ListPluginOciPackages(context.Background(), entryIDs)
				require.NoError(t, err)
				require.Len(t, packages, 2)

				entryIDMap := make(map[uuid.UUID]bool)
				for _, pkg := range packages {
					entryIDMap[pkg.PluginID] = true
				}
				require.True(t, entryIDMap[entryIDs[0]])
				require.True(t, entryIDMap[entryIDs[1]])
			},
		},
		{
			name: "list oci packages with non-existent entry IDs",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries, _ uuid.UUID) []uuid.UUID {
				return []uuid.UUID{uuid.New(), uuid.New()}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, entryIDs []uuid.UUID) {
				packages, err := queries.ListPluginOciPackages(context.Background(), entryIDs)
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

			entryIDs := tc.setupFunc(t, queries, regID)
			tc.scenarioFunc(t, queries, entryIDs)
		})
	}
}
