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

const testSkillVersion = "1.0.0"

//nolint:thelper // We want to see these lines in the test output
func createSkillEntry(
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
			EntryType: EntryTypeSKILL,
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
func insertSkill(
	t *testing.T,
	queries *Queries,
	versionID uuid.UUID,
	namespace string,
) uuid.UUID {
	skillEntryID, err := queries.InsertSkillVersion(
		context.Background(),
		InsertSkillVersionParams{
			VersionID:     versionID,
			Namespace:     namespace,
			Status:        NullSkillStatus{SkillStatus: SkillStatusACTIVE, Valid: true},
			Repository:    []byte(`{}`),
			Icons:         []byte(`[]`),
			Metadata:      []byte(`{}`),
			ExtensionMeta: []byte(`{}`),
		},
	)
	require.NoError(t, err)
	return skillEntryID
}

//nolint:thelper // We want to see these lines in the test output
func getRegistryID(t *testing.T, queries *Queries, name string) uuid.UUID {
	reg, err := queries.GetRegistryByName(context.Background(), name)
	require.NoError(t, err)
	return reg.ID
}

func TestInsertSkillVersion(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		setupFunc    func(t *testing.T, queries *Queries, regID uuid.UUID)
		scenarioFunc func(t *testing.T, queries *Queries, regID uuid.UUID)
	}{
		{
			name: "insert skill version with minimal fields",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries, _ uuid.UUID) {},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) {
				versionID := createSkillEntry(t, queries, regID, "test-skill", testSkillVersion, nil, nil)

				skillEntryID, err := queries.InsertSkillVersion(
					context.Background(),
					InsertSkillVersionParams{
						VersionID:     versionID,
						Namespace:     "test-namespace",
						Repository:    []byte(`{}`),
						Icons:         []byte(`[]`),
						Metadata:      []byte(`{}`),
						ExtensionMeta: []byte(`{}`),
					},
				)
				require.NoError(t, err)
				require.Equal(t, versionID, skillEntryID)
			},
		},
		{
			name: "insert skill version with all fields",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries, _ uuid.UUID) {},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) {
				versionID := createSkillEntry(t, queries, regID, "test-skill", testSkillVersion,
					ptr.String("A test skill"), ptr.String("Test Skill"))

				skillEntryID, err := queries.InsertSkillVersion(
					context.Background(),
					InsertSkillVersionParams{
						VersionID:     versionID,
						Namespace:     "test-namespace",
						Status:        NullSkillStatus{SkillStatus: SkillStatusACTIVE, Valid: true},
						License:       ptr.String("MIT"),
						Compatibility: ptr.String("cursor,vscode"),
						AllowedTools:  []string{"tool-a", "tool-b"},
						Repository:    []byte(`{"url":"https://github.com/test/repo"}`),
						Icons:         []byte(`[{"src":"icon.png"}]`),
						Metadata:      []byte(`{"key":"value"}`),
						ExtensionMeta: []byte(`{"ext":"meta"}`),
					},
				)
				require.NoError(t, err)
				require.Equal(t, versionID, skillEntryID)
			},
		},
		{
			name: "insert duplicate skill version fails",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) {
				versionID := createSkillEntry(t, queries, regID, "test-skill", testSkillVersion, nil, nil)
				insertSkill(t, queries, versionID, "test-namespace")
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) {
				// Look up the registry_entry to get its ID for the duplicate version insert
				createdAt := time.Now().UTC()
				entryID, err := queries.InsertRegistryEntry(
					context.Background(),
					InsertRegistryEntryParams{
						Name:      "test-skill-dup",
						SourceID:  regID,
						EntryType: EntryTypeSKILL,
						CreatedAt: &createdAt,
						UpdatedAt: &createdAt,
					},
				)
				require.NoError(t, err)

				_, err = queries.InsertEntryVersion(
					context.Background(),
					InsertEntryVersionParams{
						EntryID:   entryID,
						Version:   testSkillVersion,
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
						Version:   testSkillVersion,
						CreatedAt: &createdAt,
						UpdatedAt: &createdAt,
					},
				)
				require.Error(t, err)
			},
		},
		{
			name: "insert skill version with invalid entry_id",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries, _ uuid.UUID) {},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, _ uuid.UUID) {
				_, err := queries.InsertSkillVersion(
					context.Background(),
					InsertSkillVersionParams{
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
			name: "insert skill version defaults status to ACTIVE when not provided",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries, _ uuid.UUID) {},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) {
				versionID := createSkillEntry(t, queries, regID, "test-skill", testSkillVersion, nil, nil)

				skillEntryID, err := queries.InsertSkillVersion(
					context.Background(),
					InsertSkillVersionParams{
						VersionID:     versionID,
						Namespace:     "test-namespace",
						Status:        NullSkillStatus{Valid: false},
						Repository:    []byte(`{}`),
						Icons:         []byte(`[]`),
						Metadata:      []byte(`{}`),
						ExtensionMeta: []byte(`{}`),
					},
				)
				require.NoError(t, err)

				// Verify the status defaulted to ACTIVE by fetching
				skillRows, err := queries.GetSkillVersion(
					context.Background(),
					GetSkillVersionParams{
						Name:    "test-skill",
						Version: testSkillVersion,
					},
				)
				require.NoError(t, err)
				require.NotEmpty(t, skillRows)
				require.Equal(t, skillEntryID, skillRows[0].SkillVersionID)
				require.Equal(t, SkillStatusACTIVE, skillRows[0].Status)
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

func TestInsertSkillVersionForSync(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		setupFunc    func(t *testing.T, queries *Queries, regID uuid.UUID)
		scenarioFunc func(t *testing.T, queries *Queries, regID uuid.UUID)
	}{
		{
			name: "insert skill version for sync with minimal fields",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries, _ uuid.UUID) {},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) {
				versionID := createSkillEntry(t, queries, regID, "test-skill", testSkillVersion, nil, nil)

				skillEntryID, err := queries.InsertSkillVersionForSync(
					context.Background(),
					InsertSkillVersionForSyncParams{
						VersionID:     versionID,
						Namespace:     "test-namespace",
						Repository:    []byte(`{}`),
						Icons:         []byte(`[]`),
						Metadata:      []byte(`{}`),
						ExtensionMeta: []byte(`{}`),
					},
				)
				require.NoError(t, err)
				require.Equal(t, versionID, skillEntryID)
			},
		},
		{
			name: "insert skill version for sync with all fields",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries, _ uuid.UUID) {},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) {
				versionID := createSkillEntry(t, queries, regID, "test-skill", testSkillVersion,
					ptr.String("Sync skill"), ptr.String("Sync Skill Title"))

				skillEntryID, err := queries.InsertSkillVersionForSync(
					context.Background(),
					InsertSkillVersionForSyncParams{
						VersionID:     versionID,
						Namespace:     "sync-namespace",
						Status:        NullSkillStatus{SkillStatus: SkillStatusDEPRECATED, Valid: true},
						License:       ptr.String("Apache-2.0"),
						Compatibility: ptr.String("cursor"),
						AllowedTools:  []string{"tool-x"},
						Repository:    []byte(`{"url":"https://github.com/sync/repo"}`),
						Icons:         []byte(`[{"src":"sync-icon.png"}]`),
						Metadata:      []byte(`{"sync":"true"}`),
						ExtensionMeta: []byte(`{"ext":"sync"}`),
					},
				)
				require.NoError(t, err)
				require.Equal(t, versionID, skillEntryID)
			},
		},
		{
			name: "insert skill version for sync with invalid entry_id",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries, _ uuid.UUID) {},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, _ uuid.UUID) {
				_, err := queries.InsertSkillVersionForSync(
					context.Background(),
					InsertSkillVersionForSyncParams{
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

func TestUpsertSkillVersionForSync(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		setupFunc    func(t *testing.T, queries *Queries, regID uuid.UUID)
		scenarioFunc func(t *testing.T, queries *Queries, regID uuid.UUID)
	}{
		{
			name: "insert new skill version via upsert",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries, _ uuid.UUID) {},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) {
				versionID := createSkillEntry(t, queries, regID, "test-skill", testSkillVersion, nil, nil)

				skillEntryID, err := queries.UpsertSkillVersionForSync(
					context.Background(),
					UpsertSkillVersionForSyncParams{
						VersionID:     versionID,
						Namespace:     "test-namespace",
						Status:        NullSkillStatus{SkillStatus: SkillStatusACTIVE, Valid: true},
						License:       ptr.String("MIT"),
						Repository:    []byte(`{}`),
						Icons:         []byte(`[]`),
						Metadata:      []byte(`{}`),
						ExtensionMeta: []byte(`{}`),
					},
				)
				require.NoError(t, err)
				require.Equal(t, versionID, skillEntryID)
			},
		},
		{
			name: "update existing skill version via upsert",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) {
				versionID := createSkillEntry(t, queries, regID, "test-skill", testSkillVersion, nil, nil)
				insertSkill(t, queries, versionID, "test-namespace")
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, _ uuid.UUID) {
				// Look up the entry_id for the existing skill
				existingRows, err := queries.GetSkillVersion(
					context.Background(),
					GetSkillVersionParams{
						Name:    "test-skill",
						Version: testSkillVersion,
					},
				)
				require.NoError(t, err)
				require.NotEmpty(t, existingRows)
				existing := existingRows[0]

				// Upsert should update the existing row
				skillVersionID, err := queries.UpsertSkillVersionForSync(
					context.Background(),
					UpsertSkillVersionForSyncParams{
						VersionID:     existing.SkillVersionID,
						Namespace:     "test-namespace",
						Status:        NullSkillStatus{SkillStatus: SkillStatusDEPRECATED, Valid: true},
						License:       ptr.String("Apache-2.0"),
						Compatibility: ptr.String("vscode"),
						AllowedTools:  []string{"updated-tool"},
						Repository:    []byte(`{"url":"https://github.com/updated/repo"}`),
						Icons:         []byte(`[{"src":"updated-icon.png"}]`),
						Metadata:      []byte(`{"updated":"true"}`),
						ExtensionMeta: []byte(`{"ext":"updated"}`),
					},
				)
				require.NoError(t, err)
				require.Equal(t, existing.SkillVersionID, skillVersionID)

				// Verify the update
				skillRows, err := queries.GetSkillVersion(
					context.Background(),
					GetSkillVersionParams{
						Name:    "test-skill",
						Version: testSkillVersion,
					},
				)
				require.NoError(t, err)
				require.NotEmpty(t, skillRows)
				require.Equal(t, SkillStatusDEPRECATED, skillRows[0].Status)
				require.NotNil(t, skillRows[0].License)
				require.Equal(t, "Apache-2.0", *skillRows[0].License)
			},
		},
		{
			name: "upsert skill version with invalid entry_id",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries, _ uuid.UUID) {},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, _ uuid.UUID) {
				_, err := queries.UpsertSkillVersionForSync(
					context.Background(),
					UpsertSkillVersionForSyncParams{
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

func TestGetSkillVersion(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		setupFunc    func(t *testing.T, queries *Queries, regID uuid.UUID) (string, string)
		scenarioFunc func(t *testing.T, queries *Queries, regID uuid.UUID, skillName, version string)
	}{
		{
			name: "get skill version with minimal fields",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) (string, string) {
				entryID := createSkillEntry(t, queries, regID, "test-skill", testSkillVersion, nil, nil)
				insertSkill(t, queries, entryID, "test-namespace")
				//nolint:goconst
				return "test-skill", testSkillVersion
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, _ uuid.UUID, skillName, version string) {
				skillRows, err := queries.GetSkillVersion(
					context.Background(),
					GetSkillVersionParams{
						Name:    skillName,
						Version: version,
					},
				)
				require.NoError(t, err)
				require.NotEmpty(t, skillRows)
				skill := skillRows[0]
				require.Equal(t, skillName, skill.Name)
				require.Equal(t, version, skill.Version)
				require.Equal(t, "git", skill.RegistryType)
				require.Equal(t, "test-namespace", skill.Namespace)
				require.Equal(t, SkillStatusACTIVE, skill.Status)
				require.False(t, skill.IsLatest)
				require.NotNil(t, skill.ID)
				require.NotNil(t, skill.CreatedAt)
			},
		},
		{
			name: "get skill version with all fields",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) (string, string) {
				versionID := createSkillEntry(t, queries, regID, "test-skill", testSkillVersion,
					ptr.String("A test skill"), ptr.String("Test Skill"))

				_, err := queries.InsertSkillVersion(
					context.Background(),
					InsertSkillVersionParams{
						VersionID:     versionID,
						Namespace:     "full-namespace",
						Status:        NullSkillStatus{SkillStatus: SkillStatusACTIVE, Valid: true},
						License:       ptr.String("MIT"),
						Compatibility: ptr.String("cursor,vscode"),
						AllowedTools:  []string{"tool-a", "tool-b"},
						Repository:    []byte(`{"url":"https://github.com/test/repo"}`),
						Icons:         []byte(`[{"src":"icon.png"}]`),
						Metadata:      []byte(`{"key":"value"}`),
						ExtensionMeta: []byte(`{"ext":"meta"}`),
					},
				)
				require.NoError(t, err)
				return "test-skill", testSkillVersion
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, _ uuid.UUID, skillName, version string) {
				skillRows, err := queries.GetSkillVersion(
					context.Background(),
					GetSkillVersionParams{
						Name:    skillName,
						Version: version,
					},
				)
				require.NoError(t, err)
				require.NotEmpty(t, skillRows)
				skill := skillRows[0]
				require.Equal(t, skillName, skill.Name)
				require.Equal(t, version, skill.Version)
				require.NotNil(t, skill.Description)
				require.Equal(t, "A test skill", *skill.Description)
				require.NotNil(t, skill.Title)
				require.Equal(t, "Test Skill", *skill.Title)
				require.Equal(t, "full-namespace", skill.Namespace)
				require.Equal(t, SkillStatusACTIVE, skill.Status)
				require.NotNil(t, skill.License)
				require.Equal(t, "MIT", *skill.License)
				require.NotNil(t, skill.Compatibility)
				require.Equal(t, "cursor,vscode", *skill.Compatibility)
				require.Equal(t, []string{"tool-a", "tool-b"}, skill.AllowedTools)
				assert.JSONEq(t, `{"url":"https://github.com/test/repo"}`, string(skill.Repository))
				assert.JSONEq(t, `[{"src":"icon.png"}]`, string(skill.Icons))
				assert.JSONEq(t, `{"key":"value"}`, string(skill.Metadata))
				assert.JSONEq(t, `{"ext":"meta"}`, string(skill.ExtensionMeta))
			},
		},
		{
			name: "get skill version marked as latest",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) (string, string) {
				versionID := createSkillEntry(t, queries, regID, "test-skill", testSkillVersion, nil, nil)
				insertSkill(t, queries, versionID, "test-namespace")

				_, err := queries.UpsertLatestSkillVersion(
					context.Background(),
					UpsertLatestSkillVersionParams{
						SourceID:  regID,
						Name:      "test-skill",
						Version:   testSkillVersion,
						VersionID: versionID,
					},
				)
				require.NoError(t, err)
				return "test-skill", testSkillVersion
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, _ uuid.UUID, skillName, version string) {
				skillRows, err := queries.GetSkillVersion(
					context.Background(),
					GetSkillVersionParams{
						Name:    skillName,
						Version: version,
					},
				)
				require.NoError(t, err)
				require.NotEmpty(t, skillRows)
				skill := skillRows[0]
				require.Equal(t, skillName, skill.Name)
				require.Equal(t, version, skill.Version)
				require.True(t, skill.IsLatest)
			},
		},
		{
			name: "get skill version using latest alias",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) (string, string) {
				versionID := createSkillEntry(t, queries, regID, "test-skill", "2.0.0", nil, nil)
				insertSkill(t, queries, versionID, "test-namespace")

				_, err := queries.UpsertLatestSkillVersion(
					context.Background(),
					UpsertLatestSkillVersionParams{
						SourceID:  regID,
						Name:      "test-skill",
						Version:   "2.0.0",
						VersionID: versionID,
					},
				)
				require.NoError(t, err)
				//nolint:goconst
				return "test-skill", "latest"
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, _ uuid.UUID, skillName, version string) {
				skillRows, err := queries.GetSkillVersion(
					context.Background(),
					GetSkillVersionParams{
						Name:    skillName,
						Version: version,
					},
				)
				require.NoError(t, err)
				require.NotEmpty(t, skillRows)
				skill := skillRows[0]
				require.Equal(t, skillName, skill.Name)
				require.Equal(t, "2.0.0", skill.Version)
				require.True(t, skill.IsLatest)
			},
		},
		{
			name: "get skill version filtered by source IDs",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) (string, string) {
				versionID := createSkillEntry(t, queries, regID, "test-skill", testSkillVersion, nil, nil)
				insertSkill(t, queries, versionID, "test-namespace")
				return "test-skill", testSkillVersion
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, _ uuid.UUID, skillName, version string) {
				regName := "test-registry"
				skillRows, err := queries.GetSkillVersion(
					context.Background(),
					GetSkillVersionParams{
						Name:         skillName,
						Version:      version,
						RegistryName: &regName,
					},
				)
				require.NoError(t, err)
				require.NotEmpty(t, skillRows)
				require.Equal(t, skillName, skillRows[0].Name)
				require.Equal(t, version, skillRows[0].Version)

				// Wrong registry name returns empty results
				wrongName := "nonexistent-registry"
				wrongRows, err := queries.GetSkillVersion(
					context.Background(),
					GetSkillVersionParams{
						Name:         skillName,
						Version:      version,
						RegistryName: &wrongName,
					},
				)
				require.NoError(t, err)
				require.Empty(t, wrongRows)
			},
		},
		{
			name: "get non-existent skill version",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries, _ uuid.UUID) (string, string) {
				return "non-existent-skill", testSkillVersion
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, _ uuid.UUID, skillName, version string) {
				skillRows, err := queries.GetSkillVersion(
					context.Background(),
					GetSkillVersionParams{
						Name:    skillName,
						Version: version,
					},
				)
				require.NoError(t, err)
				require.Empty(t, skillRows)
			},
		},
		{
			name: "get skill version with wrong version",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) (string, string) {
				entryID := createSkillEntry(t, queries, regID, "test-skill", testSkillVersion, nil, nil)
				insertSkill(t, queries, entryID, "test-namespace")
				return "test-skill", "9.9.9"
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, _ uuid.UUID, skillName, version string) {
				skillRows, err := queries.GetSkillVersion(
					context.Background(),
					GetSkillVersionParams{
						Name:    skillName,
						Version: version,
					},
				)
				require.NoError(t, err)
				require.Empty(t, skillRows)
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

			skillName, version := tc.setupFunc(t, queries, regID)
			tc.scenarioFunc(t, queries, regID, skillName, version)
		})
	}
}

func TestListSkills(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		setupFunc    func(t *testing.T, queries *Queries, regID uuid.UUID)
		scenarioFunc func(t *testing.T, queries *Queries, regID uuid.UUID)
	}{
		{
			name: "no skills",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries, _ uuid.UUID) {},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, _ uuid.UUID) {
				skills, err := queries.ListSkills(
					context.Background(),
					ListSkillsParams{
						Size: 10,
					},
				)
				require.NoError(t, err)
				require.Empty(t, skills)
			},
		},
		{
			name: "list single skill",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) {
				entryID := createSkillEntry(t, queries, regID, "test-skill", testSkillVersion, nil, nil)
				insertSkill(t, queries, entryID, "test-namespace")
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, _ uuid.UUID) {
				skills, err := queries.ListSkills(
					context.Background(),
					ListSkillsParams{
						Size: 10,
					},
				)
				require.NoError(t, err)
				require.Len(t, skills, 1)
				require.Equal(t, "test-skill", skills[0].Name)
				require.Equal(t, testSkillVersion, skills[0].Version)
				require.Equal(t, "test-namespace", skills[0].Namespace)
				require.Equal(t, "git", skills[0].RegistryType)
			},
		},
		{
			name: "list multiple skills",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) {
				for _, name := range []string{"alpha-skill", "beta-skill", "gamma-skill"} {
					entryID := createSkillEntry(t, queries, regID, name, testSkillVersion, nil, nil)
					insertSkill(t, queries, entryID, "test-namespace")
				}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, _ uuid.UUID) {
				skills, err := queries.ListSkills(
					context.Background(),
					ListSkillsParams{
						Size: 10,
					},
				)
				require.NoError(t, err)
				require.Len(t, skills, 3)
				// Verify ordering by name ASC
				assert.Equal(t, "alpha-skill", skills[0].Name)
				assert.Equal(t, "beta-skill", skills[1].Name)
				assert.Equal(t, "gamma-skill", skills[2].Name)
			},
		},
		{
			name: "list skills with cursor pagination",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) {
				for _, name := range []string{"alpha-skill", "beta-skill", "gamma-skill"} {
					entryID := createSkillEntry(t, queries, regID, name, testSkillVersion, nil, nil)
					insertSkill(t, queries, entryID, "test-namespace")
				}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, _ uuid.UUID) {
				// Get first page
				allSkills, err := queries.ListSkills(
					context.Background(),
					ListSkillsParams{
						Size: 10,
					},
				)
				require.NoError(t, err)
				require.Len(t, allSkills, 3)

				// Use cursor to skip past first skill
				cursorName := allSkills[0].Name
				cursorVersion := allSkills[0].Version
				skills, err := queries.ListSkills(
					context.Background(),
					ListSkillsParams{
						CursorName:    &cursorName,
						CursorVersion: &cursorVersion,
						Size:          10,
					},
				)
				require.NoError(t, err)
				require.Len(t, skills, 2)
				assert.Equal(t, "beta-skill", skills[0].Name)
				assert.Equal(t, "gamma-skill", skills[1].Name)
			},
		},
		{
			name: "list skills with limit",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) {
				for _, name := range []string{"alpha-skill", "beta-skill", "gamma-skill"} {
					entryID := createSkillEntry(t, queries, regID, name, testSkillVersion, nil, nil)
					insertSkill(t, queries, entryID, "test-namespace")
				}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, _ uuid.UUID) {
				skills, err := queries.ListSkills(
					context.Background(),
					ListSkillsParams{
						Size: 2,
					},
				)
				require.NoError(t, err)
				require.Len(t, skills, 2)
			},
		},
		{
			name: "list skills filtered by namespace",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) {
				entryID1 := createSkillEntry(t, queries, regID, "skill-a", testSkillVersion, nil, nil)
				insertSkill(t, queries, entryID1, "namespace-1")

				entryID2 := createSkillEntry(t, queries, regID, "skill-b", testSkillVersion, nil, nil)
				insertSkill(t, queries, entryID2, "namespace-2")
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, _ uuid.UUID) {
				//nolint:goconst
				ns := "namespace-1"
				skills, err := queries.ListSkills(
					context.Background(),
					ListSkillsParams{
						Namespace: &ns,
						Size:      10,
					},
				)
				require.NoError(t, err)
				require.Len(t, skills, 1)
				assert.Equal(t, "skill-a", skills[0].Name)
				assert.Equal(t, "namespace-1", skills[0].Namespace)
			},
		},
		{
			name: "list skills filtered by name",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) {
				for _, name := range []string{"skill-a", "skill-b", "skill-c"} {
					entryID := createSkillEntry(t, queries, regID, name, testSkillVersion, nil, nil)
					insertSkill(t, queries, entryID, "test-namespace")
				}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, _ uuid.UUID) {
				name := "skill-b"
				skills, err := queries.ListSkills(
					context.Background(),
					ListSkillsParams{
						Name: &name,
						Size: 10,
					},
				)
				require.NoError(t, err)
				require.Len(t, skills, 1)
				assert.Equal(t, "skill-b", skills[0].Name)
			},
		},
		{
			name: "list skills with search",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) {
				entryID1 := createSkillEntry(t, queries, regID, "code-review", testSkillVersion,
					ptr.String("Automated code review tool"), ptr.String("Code Review"))
				insertSkill(t, queries, entryID1, "test-namespace")

				entryID2 := createSkillEntry(t, queries, regID, "test-runner", testSkillVersion,
					ptr.String("Run unit tests"), ptr.String("Test Runner"))
				insertSkill(t, queries, entryID2, "test-namespace")
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, _ uuid.UUID) {
				search := "code"
				skills, err := queries.ListSkills(
					context.Background(),
					ListSkillsParams{
						Search: &search,
						Size:   10,
					},
				)
				require.NoError(t, err)
				require.Len(t, skills, 1)
				assert.Equal(t, "code-review", skills[0].Name)
			},
		},
		{
			name: "list skills with updated_since filter",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) {
				oldTime := time.Now().UTC().Add(-1 * time.Hour)
				recentTime := time.Now().UTC()

				oldEntryID, err := queries.InsertRegistryEntry(
					context.Background(),
					InsertRegistryEntryParams{
						Name:      "old-skill",
						SourceID:  regID,
						EntryType: EntryTypeSKILL,
						CreatedAt: &oldTime,
						UpdatedAt: &oldTime,
					},
				)
				require.NoError(t, err)
				versionID1, err := queries.InsertEntryVersion(
					context.Background(),
					InsertEntryVersionParams{
						EntryID:   oldEntryID,
						Version:   testSkillVersion,
						CreatedAt: &oldTime,
						UpdatedAt: &oldTime,
					},
				)
				require.NoError(t, err)
				insertSkill(t, queries, versionID1, "test-namespace")

				recentEntryID, err := queries.InsertRegistryEntry(
					context.Background(),
					InsertRegistryEntryParams{
						Name:      "recent-skill",
						SourceID:  regID,
						EntryType: EntryTypeSKILL,
						CreatedAt: &recentTime,
						UpdatedAt: &recentTime,
					},
				)
				require.NoError(t, err)
				versionID2, err := queries.InsertEntryVersion(
					context.Background(),
					InsertEntryVersionParams{
						EntryID:   recentEntryID,
						Version:   testSkillVersion,
						CreatedAt: &recentTime,
						UpdatedAt: &recentTime,
					},
				)
				require.NoError(t, err)
				insertSkill(t, queries, versionID2, "test-namespace")
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, _ uuid.UUID) {
				since := time.Now().UTC().Add(-30 * time.Minute)
				skills, err := queries.ListSkills(
					context.Background(),
					ListSkillsParams{
						UpdatedSince: &since,
						Size:         10,
					},
				)
				require.NoError(t, err)
				require.Len(t, skills, 1)
				assert.Equal(t, "recent-skill", skills[0].Name)
			},
		},
		{
			name: "list skills filtered by registry name",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) {
				entryID := createSkillEntry(t, queries, regID, "test-skill", testSkillVersion, nil, nil)
				insertSkill(t, queries, entryID, "test-namespace")
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, _ uuid.UUID) {
				regID := getRegistryID(t, queries, "test-registry")
				skills, err := queries.ListSkills(
					context.Background(),
					ListSkillsParams{
						RegistryID: &regID,
						Size:       10,
					},
				)
				require.NoError(t, err)
				require.Len(t, skills, 1)

				// Wrong registry ID returns empty
				wrongID := uuid.New()
				skills, err = queries.ListSkills(
					context.Background(),
					ListSkillsParams{
						RegistryID: &wrongID,
						Size:       10,
					},
				)
				require.NoError(t, err)
				require.Empty(t, skills)
			},
		},
		{
			name: "list skills with multiple versions ordered correctly",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) {
				createdAt := time.Now().UTC()
				entryID, err := queries.InsertRegistryEntry(
					context.Background(),
					InsertRegistryEntryParams{
						Name:      "test-skill",
						SourceID:  regID,
						EntryType: EntryTypeSKILL,
						CreatedAt: &createdAt,
						UpdatedAt: &createdAt,
					},
				)
				require.NoError(t, err)
				for _, version := range []string{testSkillVersion, "2.0.0", "3.0.0"} {
					versionID, vErr := queries.InsertEntryVersion(
						context.Background(),
						InsertEntryVersionParams{
							EntryID:   entryID,
							Version:   version,
							CreatedAt: &createdAt,
							UpdatedAt: &createdAt,
						},
					)
					require.NoError(t, vErr)
					insertSkill(t, queries, versionID, "test-namespace")
				}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, _ uuid.UUID) {
				skills, err := queries.ListSkills(
					context.Background(),
					ListSkillsParams{
						Size: 10,
					},
				)
				require.NoError(t, err)
				require.Len(t, skills, 3)
				// ORDER BY name ASC, version ASC
				assert.Equal(t, testSkillVersion, skills[0].Version)
				assert.Equal(t, "2.0.0", skills[1].Version)
				assert.Equal(t, "3.0.0", skills[2].Version)
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

func TestUpsertLatestSkillVersion(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		setupFunc    func(t *testing.T, queries *Queries, regID uuid.UUID) []uuid.UUID
		scenarioFunc func(t *testing.T, queries *Queries, regID uuid.UUID, ids []uuid.UUID)
	}{
		{
			name: "insert latest skill version",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) []uuid.UUID {
				entryID := createSkillEntry(t, queries, regID, "test-skill", testSkillVersion, nil, nil)
				skillID := insertSkill(t, queries, entryID, "test-namespace")
				return []uuid.UUID{skillID}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, regID uuid.UUID, ids []uuid.UUID) {
				latestID, err := queries.UpsertLatestSkillVersion(
					context.Background(),
					UpsertLatestSkillVersionParams{
						SourceID:  regID,
						Name:      "test-skill",
						Version:   testSkillVersion,
						VersionID: ids[0],
					},
				)
				require.NoError(t, err)
				require.Equal(t, ids[0], latestID)
			},
		},
		{
			name: "update existing latest skill version",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) []uuid.UUID {
				createdAt := time.Now().UTC()
				entryID, err := queries.InsertRegistryEntry(
					context.Background(),
					InsertRegistryEntryParams{
						Name:      "test-skill",
						SourceID:  regID,
						EntryType: EntryTypeSKILL,
						CreatedAt: &createdAt,
						UpdatedAt: &createdAt,
					},
				)
				require.NoError(t, err)

				var versionIDs []uuid.UUID
				for _, version := range []string{testSkillVersion, "2.0.0"} {
					versionID, vErr := queries.InsertEntryVersion(
						context.Background(),
						InsertEntryVersionParams{
							EntryID:   entryID,
							Version:   version,
							CreatedAt: &createdAt,
							UpdatedAt: &createdAt,
						},
					)
					require.NoError(t, vErr)
					insertSkill(t, queries, versionID, "test-namespace")
					versionIDs = append(versionIDs, versionID)
				}

				// Set initial latest version
				latestID, err := queries.UpsertLatestSkillVersion(
					context.Background(),
					UpsertLatestSkillVersionParams{
						SourceID:  regID,
						Name:      "test-skill",
						Version:   testSkillVersion,
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
				latestID, err := queries.UpsertLatestSkillVersion(
					context.Background(),
					UpsertLatestSkillVersionParams{
						SourceID:  regID,
						Name:      "test-skill",
						Version:   "2.0.0",
						VersionID: ids[1],
					},
				)
				require.NoError(t, err)
				require.Equal(t, ids[1], latestID)
			},
		},
		{
			name: "upsert latest skill version with invalid reg_id",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries, _ uuid.UUID) []uuid.UUID {
				return []uuid.UUID{uuid.New()}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, _ uuid.UUID, ids []uuid.UUID) {
				_, err := queries.UpsertLatestSkillVersion(
					context.Background(),
					UpsertLatestSkillVersionParams{
						SourceID:  uuid.New(),
						Name:      "test-skill",
						Version:   testSkillVersion,
						VersionID: ids[0],
					},
				)
				require.Error(t, err)
			},
		},
		{
			name: "upsert latest skill version with invalid entry_id",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries, _ uuid.UUID) []uuid.UUID {
				return []uuid.UUID{uuid.New()}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, regID uuid.UUID, ids []uuid.UUID) {
				_, err := queries.UpsertLatestSkillVersion(
					context.Background(),
					UpsertLatestSkillVersionParams{
						SourceID:  regID,
						Name:      "test-skill",
						Version:   testSkillVersion,
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

func TestDeleteOrphanedSkills(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		setupFunc    func(t *testing.T, queries *Queries, regID uuid.UUID) []uuid.UUID
		scenarioFunc func(t *testing.T, queries *Queries, regID uuid.UUID, entryIDs []uuid.UUID)
	}{
		{
			name: "delete orphaned skills keeping specified IDs",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) []uuid.UUID {
				var entryIDs []uuid.UUID
				for _, name := range []string{"keep-skill", "orphan-skill-1", "orphan-skill-2"} {
					entryID := createSkillEntry(t, queries, regID, name, testSkillVersion, nil, nil)
					insertSkill(t, queries, entryID, "test-namespace")
					entryIDs = append(entryIDs, entryID)
				}
				return entryIDs
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, regID uuid.UUID, entryIDs []uuid.UUID) {
				// Keep only the first skill
				err := queries.DeleteOrphanedSkills(
					context.Background(),
					DeleteOrphanedSkillsParams{
						SourceID: regID,
						KeepIds:  []uuid.UUID{entryIDs[0]},
					},
				)
				require.NoError(t, err)

				// Verify only the kept skill remains
				skills, err := queries.ListSkills(
					context.Background(),
					ListSkillsParams{Size: 10},
				)
				require.NoError(t, err)
				require.Len(t, skills, 1)
				assert.Equal(t, "keep-skill", skills[0].Name)
			},
		},
		{
			name: "delete orphaned skills with empty keep list deletes all",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) []uuid.UUID {
				var entryIDs []uuid.UUID
				for _, name := range []string{"skill-a", "skill-b"} {
					entryID := createSkillEntry(t, queries, regID, name, testSkillVersion, nil, nil)
					insertSkill(t, queries, entryID, "test-namespace")
					entryIDs = append(entryIDs, entryID)
				}
				return entryIDs
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, regID uuid.UUID, _ []uuid.UUID) {
				err := queries.DeleteOrphanedSkills(
					context.Background(),
					DeleteOrphanedSkillsParams{
						SourceID: regID,
						KeepIds:  []uuid.UUID{},
					},
				)
				require.NoError(t, err)

				skills, err := queries.ListSkills(
					context.Background(),
					ListSkillsParams{Size: 10},
				)
				require.NoError(t, err)
				require.Empty(t, skills)
			},
		},
		{
			name: "delete orphaned skills when none exist",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries, _ uuid.UUID) []uuid.UUID {
				return nil
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, regID uuid.UUID, _ []uuid.UUID) {
				err := queries.DeleteOrphanedSkills(
					context.Background(),
					DeleteOrphanedSkillsParams{
						SourceID: regID,
						KeepIds:  []uuid.UUID{},
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

func TestInsertSkillGitPackage(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		setupFunc    func(t *testing.T, queries *Queries, regID uuid.UUID) uuid.UUID
		scenarioFunc func(t *testing.T, queries *Queries, skillID uuid.UUID)
	}{
		{
			name: "insert git package with minimal fields",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) uuid.UUID {
				entryID := createSkillEntry(t, queries, regID, "test-skill", testSkillVersion, nil, nil)
				skillVersionID := insertSkill(t, queries, entryID, "test-namespace")
				return skillVersionID
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, skillVersionID uuid.UUID) {
				err := queries.InsertSkillGitPackage(
					context.Background(),
					InsertSkillGitPackageParams{
						SkillID: skillVersionID,
						Url:     "https://github.com/test/skill-repo",
					},
				)
				require.NoError(t, err)
			},
		},
		{
			name: "insert git package with all fields",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) uuid.UUID {
				entryID := createSkillEntry(t, queries, regID, "test-skill", testSkillVersion, nil, nil)
				skillVersionID := insertSkill(t, queries, entryID, "test-namespace")
				return skillVersionID
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, skillVersionID uuid.UUID) {
				err := queries.InsertSkillGitPackage(
					context.Background(),
					InsertSkillGitPackageParams{
						SkillID:   skillVersionID,
						Url:       "https://github.com/test/skill-repo",
						Ref:       ptr.String("refs/tags/v1.0.0"),
						CommitSha: ptr.String("abc123def456"),
						Subfolder: ptr.String("skills/my-skill"),
					},
				)
				require.NoError(t, err)
			},
		},
		{
			name: "insert git package with invalid skill_entry_id",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries, _ uuid.UUID) uuid.UUID {
				return uuid.New()
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, skillVersionID uuid.UUID) {
				err := queries.InsertSkillGitPackage(
					context.Background(),
					InsertSkillGitPackageParams{
						SkillID: skillVersionID,
						Url:     "https://github.com/test/skill-repo",
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

func TestInsertSkillOciPackage(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		setupFunc    func(t *testing.T, queries *Queries, regID uuid.UUID) uuid.UUID
		scenarioFunc func(t *testing.T, queries *Queries, skillVersionID uuid.UUID)
	}{
		{
			name: "insert oci package with minimal fields",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) uuid.UUID {
				entryID := createSkillEntry(t, queries, regID, "test-skill", testSkillVersion, nil, nil)
				skillVersionID := insertSkill(t, queries, entryID, "test-namespace")
				return skillVersionID
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, skillVersionID uuid.UUID) {
				err := queries.InsertSkillOciPackage(
					context.Background(),
					InsertSkillOciPackageParams{
						SkillID:    skillVersionID,
						Identifier: "ghcr.io/test/skill:v1.0.0",
					},
				)
				require.NoError(t, err)
			},
		},
		{
			name: "insert oci package with all fields",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) uuid.UUID {
				entryID := createSkillEntry(t, queries, regID, "test-skill", testSkillVersion, nil, nil)
				skillVersionID := insertSkill(t, queries, entryID, "test-namespace")
				return skillVersionID
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, skillVersionID uuid.UUID) {
				err := queries.InsertSkillOciPackage(
					context.Background(),
					InsertSkillOciPackageParams{
						SkillID:    skillVersionID,
						Identifier: "ghcr.io/test/skill:v1.0.0",
						Digest:     ptr.String("sha256:abcdef1234567890"),
						MediaType:  ptr.String("application/vnd.oci.image.manifest.v1+json"),
					},
				)
				require.NoError(t, err)
			},
		},
		{
			name: "insert oci package with invalid skill_entry_id",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries, _ uuid.UUID) uuid.UUID {
				return uuid.New()
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, skillVersionID uuid.UUID) {
				err := queries.InsertSkillOciPackage(
					context.Background(),
					InsertSkillOciPackageParams{
						SkillID:    skillVersionID,
						Identifier: "ghcr.io/test/skill:v1.0.0",
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

func TestListSkillGitPackages(t *testing.T) {
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
				entryID := createSkillEntry(t, queries, regID, "test-skill", testSkillVersion, nil, nil)
				insertSkill(t, queries, entryID, "test-namespace")
				return []uuid.UUID{entryID}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, entryIDs []uuid.UUID) {
				packages, err := queries.ListSkillGitPackages(context.Background(), entryIDs)
				require.NoError(t, err)
				require.Empty(t, packages)
			},
		},
		{
			name: "list single git package",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) []uuid.UUID {
				entryID := createSkillEntry(t, queries, regID, "test-skill", testSkillVersion, nil, nil)
				skillVersionID := insertSkill(t, queries, entryID, "test-namespace")

				err := queries.InsertSkillGitPackage(
					context.Background(),
					InsertSkillGitPackageParams{
						SkillID:   skillVersionID,
						Url:       "https://github.com/test/skill-repo",
						Ref:       ptr.String("refs/tags/v1.0.0"),
						CommitSha: ptr.String("abc123"),
						Subfolder: ptr.String("skills/my-skill"),
					},
				)
				require.NoError(t, err)
				return []uuid.UUID{entryID}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, entryIDs []uuid.UUID) {
				packages, err := queries.ListSkillGitPackages(context.Background(), entryIDs)
				require.NoError(t, err)
				require.Len(t, packages, 1)
				assert.Equal(t, entryIDs[0], packages[0].SkillID)
				assert.Equal(t, "https://github.com/test/skill-repo", packages[0].Url)
				require.NotNil(t, packages[0].Ref)
				assert.Equal(t, "refs/tags/v1.0.0", *packages[0].Ref)
				require.NotNil(t, packages[0].CommitSha)
				assert.Equal(t, "abc123", *packages[0].CommitSha)
				require.NotNil(t, packages[0].Subfolder)
				assert.Equal(t, "skills/my-skill", *packages[0].Subfolder)
			},
		},
		{
			name: "list git packages for multiple skills",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) []uuid.UUID {
				var entryIDs []uuid.UUID
				for i, name := range []string{"skill-1", "skill-2"} {
					entryID := createSkillEntry(t, queries, regID, name, testSkillVersion, nil, nil)
					skillVersionID := insertSkill(t, queries, entryID, "test-namespace")

					err := queries.InsertSkillGitPackage(
						context.Background(),
						InsertSkillGitPackageParams{
							SkillID: skillVersionID,
							Url:     fmt.Sprintf("https://github.com/test/skill-repo-%d", i+1),
						},
					)
					require.NoError(t, err)
					entryIDs = append(entryIDs, entryID)
				}
				return entryIDs
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, entryIDs []uuid.UUID) {
				packages, err := queries.ListSkillGitPackages(context.Background(), entryIDs)
				require.NoError(t, err)
				require.Len(t, packages, 2)

				entryIDMap := make(map[uuid.UUID]bool)
				for _, pkg := range packages {
					entryIDMap[pkg.SkillID] = true
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
				packages, err := queries.ListSkillGitPackages(context.Background(), entryIDs)
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

func TestListSkillOciPackages(t *testing.T) {
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
				entryID := createSkillEntry(t, queries, regID, "test-skill", testSkillVersion, nil, nil)
				insertSkill(t, queries, entryID, "test-namespace")
				return []uuid.UUID{entryID}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, entryIDs []uuid.UUID) {
				packages, err := queries.ListSkillOciPackages(context.Background(), entryIDs)
				require.NoError(t, err)
				require.Empty(t, packages)
			},
		},
		{
			name: "list single oci package",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) []uuid.UUID {
				entryID := createSkillEntry(t, queries, regID, "test-skill", testSkillVersion, nil, nil)
				skillVersionID := insertSkill(t, queries, entryID, "test-namespace")

				err := queries.InsertSkillOciPackage(
					context.Background(),
					InsertSkillOciPackageParams{
						SkillID:    skillVersionID,
						Identifier: "ghcr.io/test/skill:v1.0.0",
						Digest:     ptr.String("sha256:abcdef1234567890"),
						MediaType:  ptr.String("application/vnd.oci.image.manifest.v1+json"),
					},
				)
				require.NoError(t, err)
				return []uuid.UUID{entryID}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, entryIDs []uuid.UUID) {
				packages, err := queries.ListSkillOciPackages(context.Background(), entryIDs)
				require.NoError(t, err)
				require.Len(t, packages, 1)
				assert.Equal(t, entryIDs[0], packages[0].SkillID)
				assert.Equal(t, "ghcr.io/test/skill:v1.0.0", packages[0].Identifier)
				require.NotNil(t, packages[0].Digest)
				assert.Equal(t, "sha256:abcdef1234567890", *packages[0].Digest)
				require.NotNil(t, packages[0].MediaType)
				assert.Equal(t, "application/vnd.oci.image.manifest.v1+json", *packages[0].MediaType)
			},
		},
		{
			name: "list oci packages for multiple skills",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) []uuid.UUID {
				var entryIDs []uuid.UUID
				for i, name := range []string{"skill-1", "skill-2"} {
					entryID := createSkillEntry(t, queries, regID, name, testSkillVersion, nil, nil)
					skillVersionID := insertSkill(t, queries, entryID, "test-namespace")

					err := queries.InsertSkillOciPackage(
						context.Background(),
						InsertSkillOciPackageParams{
							SkillID:    skillVersionID,
							Identifier: fmt.Sprintf("ghcr.io/test/skill-%d:v1.0.0", i+1),
						},
					)
					require.NoError(t, err)
					entryIDs = append(entryIDs, entryID)
				}
				return entryIDs
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, entryIDs []uuid.UUID) {
				packages, err := queries.ListSkillOciPackages(context.Background(), entryIDs)
				require.NoError(t, err)
				require.Len(t, packages, 2)

				entryIDMap := make(map[uuid.UUID]bool)
				for _, pkg := range packages {
					entryIDMap[pkg.SkillID] = true
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
				packages, err := queries.ListSkillOciPackages(context.Background(), entryIDs)
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
