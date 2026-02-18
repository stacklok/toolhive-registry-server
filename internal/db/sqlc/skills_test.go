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
			Name:        name,
			Version:     version,
			RegID:       regID,
			EntryType:   EntryTypeSKILL,
			Description: description,
			Title:       title,
			CreatedAt:   &createdAt,
			UpdatedAt:   &createdAt,
		},
	)
	require.NoError(t, err)
	return entryID
}

//nolint:thelper // We want to see these lines in the test output
func insertSkill(
	t *testing.T,
	queries *Queries,
	entryID uuid.UUID,
	namespace string,
) uuid.UUID {
	skillEntryID, err := queries.InsertSkillVersion(
		context.Background(),
		InsertSkillVersionParams{
			EntryID:       entryID,
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
				entryID := createSkillEntry(t, queries, regID, "test-skill", "1.0.0", nil, nil)

				skillEntryID, err := queries.InsertSkillVersion(
					context.Background(),
					InsertSkillVersionParams{
						EntryID:       entryID,
						Namespace:     "test-namespace",
						Repository:    []byte(`{}`),
						Icons:         []byte(`[]`),
						Metadata:      []byte(`{}`),
						ExtensionMeta: []byte(`{}`),
					},
				)
				require.NoError(t, err)
				require.Equal(t, entryID, skillEntryID)
			},
		},
		{
			name: "insert skill version with all fields",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries, _ uuid.UUID) {},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) {
				entryID := createSkillEntry(t, queries, regID, "test-skill", "1.0.0",
					ptr.String("A test skill"), ptr.String("Test Skill"))

				skillEntryID, err := queries.InsertSkillVersion(
					context.Background(),
					InsertSkillVersionParams{
						EntryID:       entryID,
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
				require.Equal(t, entryID, skillEntryID)
			},
		},
		{
			name: "insert duplicate skill version fails",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) {
				entryID := createSkillEntry(t, queries, regID, "test-skill", "1.0.0", nil, nil)
				insertSkill(t, queries, entryID, "test-namespace")
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) {
				// Inserting a duplicate registry entry (same name+version+reg_id) should fail
				_, err := queries.InsertRegistryEntry(
					context.Background(),
					InsertRegistryEntryParams{
						Name:      "test-skill",
						Version:   "1.0.0",
						RegID:     regID,
						EntryType: EntryTypeSKILL,
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
						EntryID:       uuid.New(),
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
				entryID := createSkillEntry(t, queries, regID, "test-skill", "1.0.0", nil, nil)

				skillEntryID, err := queries.InsertSkillVersion(
					context.Background(),
					InsertSkillVersionParams{
						EntryID:       entryID,
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
				skill, err := queries.GetSkillVersion(
					context.Background(),
					GetSkillVersionParams{
						Name:    "test-skill",
						Version: "1.0.0",
					},
				)
				require.NoError(t, err)
				require.Equal(t, skillEntryID, skill.SkillEntryID)
				require.Equal(t, SkillStatusACTIVE, skill.Status)
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
				entryID := createSkillEntry(t, queries, regID, "test-skill", "1.0.0", nil, nil)

				skillEntryID, err := queries.InsertSkillVersionForSync(
					context.Background(),
					InsertSkillVersionForSyncParams{
						EntryID:       entryID,
						Namespace:     "test-namespace",
						Repository:    []byte(`{}`),
						Icons:         []byte(`[]`),
						Metadata:      []byte(`{}`),
						ExtensionMeta: []byte(`{}`),
					},
				)
				require.NoError(t, err)
				require.Equal(t, entryID, skillEntryID)
			},
		},
		{
			name: "insert skill version for sync with all fields",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries, _ uuid.UUID) {},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) {
				entryID := createSkillEntry(t, queries, regID, "test-skill", "1.0.0",
					ptr.String("Sync skill"), ptr.String("Sync Skill Title"))

				skillEntryID, err := queries.InsertSkillVersionForSync(
					context.Background(),
					InsertSkillVersionForSyncParams{
						EntryID:       entryID,
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
				require.Equal(t, entryID, skillEntryID)
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
						EntryID:       uuid.New(),
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
				entryID := createSkillEntry(t, queries, regID, "test-skill", "1.0.0", nil, nil)

				skillEntryID, err := queries.UpsertSkillVersionForSync(
					context.Background(),
					UpsertSkillVersionForSyncParams{
						EntryID:       entryID,
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
				require.Equal(t, entryID, skillEntryID)
			},
		},
		{
			name: "update existing skill version via upsert",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) {
				entryID := createSkillEntry(t, queries, regID, "test-skill", "1.0.0", nil, nil)
				insertSkill(t, queries, entryID, "test-namespace")
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, _ uuid.UUID) {
				// Look up the entry_id for the existing skill
				existing, err := queries.GetSkillVersion(
					context.Background(),
					GetSkillVersionParams{
						Name:    "test-skill",
						Version: "1.0.0",
					},
				)
				require.NoError(t, err)

				// Upsert should update the existing row
				skillEntryID, err := queries.UpsertSkillVersionForSync(
					context.Background(),
					UpsertSkillVersionForSyncParams{
						EntryID:       existing.SkillEntryID,
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
				require.Equal(t, existing.SkillEntryID, skillEntryID)

				// Verify the update
				skill, err := queries.GetSkillVersion(
					context.Background(),
					GetSkillVersionParams{
						Name:    "test-skill",
						Version: "1.0.0",
					},
				)
				require.NoError(t, err)
				require.Equal(t, SkillStatusDEPRECATED, skill.Status)
				require.NotNil(t, skill.License)
				require.Equal(t, "Apache-2.0", *skill.License)
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
						EntryID:       uuid.New(),
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
		scenarioFunc func(t *testing.T, queries *Queries, skillName, version string)
	}{
		{
			name: "get skill version with minimal fields",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) (string, string) {
				entryID := createSkillEntry(t, queries, regID, "test-skill", "1.0.0", nil, nil)
				insertSkill(t, queries, entryID, "test-namespace")
				//nolint:goconst
				return "test-skill", "1.0.0"
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, skillName, version string) {
				skill, err := queries.GetSkillVersion(
					context.Background(),
					GetSkillVersionParams{
						Name:    skillName,
						Version: version,
					},
				)
				require.NoError(t, err)
				require.Equal(t, skillName, skill.Name)
				require.Equal(t, version, skill.Version)
				require.Equal(t, RegistryTypeREMOTE, skill.RegistryType)
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
				entryID := createSkillEntry(t, queries, regID, "test-skill", "1.0.0",
					ptr.String("A test skill"), ptr.String("Test Skill"))

				_, err := queries.InsertSkillVersion(
					context.Background(),
					InsertSkillVersionParams{
						EntryID:       entryID,
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
				return "test-skill", "1.0.0"
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, skillName, version string) {
				skill, err := queries.GetSkillVersion(
					context.Background(),
					GetSkillVersionParams{
						Name:    skillName,
						Version: version,
					},
				)
				require.NoError(t, err)
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
				entryID := createSkillEntry(t, queries, regID, "test-skill", "1.0.0", nil, nil)
				insertSkill(t, queries, entryID, "test-namespace")

				_, err := queries.UpsertLatestSkillVersion(
					context.Background(),
					UpsertLatestSkillVersionParams{
						RegID:   regID,
						Name:    "test-skill",
						Version: "1.0.0",
						EntryID: entryID,
					},
				)
				require.NoError(t, err)
				return "test-skill", "1.0.0"
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, skillName, version string) {
				skill, err := queries.GetSkillVersion(
					context.Background(),
					GetSkillVersionParams{
						Name:    skillName,
						Version: version,
					},
				)
				require.NoError(t, err)
				require.Equal(t, skillName, skill.Name)
				require.Equal(t, version, skill.Version)
				require.True(t, skill.IsLatest)
			},
		},
		{
			name: "get skill version using latest alias",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) (string, string) {
				entryID := createSkillEntry(t, queries, regID, "test-skill", "2.0.0", nil, nil)
				insertSkill(t, queries, entryID, "test-namespace")

				_, err := queries.UpsertLatestSkillVersion(
					context.Background(),
					UpsertLatestSkillVersionParams{
						RegID:   regID,
						Name:    "test-skill",
						Version: "2.0.0",
						EntryID: entryID,
					},
				)
				require.NoError(t, err)
				//nolint:goconst
				return "test-skill", "latest"
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, skillName, version string) {
				skill, err := queries.GetSkillVersion(
					context.Background(),
					GetSkillVersionParams{
						Name:    skillName,
						Version: version,
					},
				)
				require.NoError(t, err)
				require.Equal(t, skillName, skill.Name)
				require.Equal(t, "2.0.0", skill.Version)
				require.True(t, skill.IsLatest)
			},
		},
		{
			name: "get skill version filtered by registry name",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) (string, string) {
				entryID := createSkillEntry(t, queries, regID, "test-skill", "1.0.0", nil, nil)
				insertSkill(t, queries, entryID, "test-namespace")
				return "test-skill", "1.0.0"
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, skillName, version string) {
				regName := "test-registry"
				skill, err := queries.GetSkillVersion(
					context.Background(),
					GetSkillVersionParams{
						Name:         skillName,
						Version:      version,
						RegistryName: &regName,
					},
				)
				require.NoError(t, err)
				require.Equal(t, skillName, skill.Name)
				require.Equal(t, version, skill.Version)

				// Wrong registry name returns error
				wrongRegName := "wrong-registry"
				_, err = queries.GetSkillVersion(
					context.Background(),
					GetSkillVersionParams{
						Name:         skillName,
						Version:      version,
						RegistryName: &wrongRegName,
					},
				)
				require.Error(t, err)
			},
		},
		{
			name: "get non-existent skill version",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries, _ uuid.UUID) (string, string) {
				return "non-existent-skill", "1.0.0"
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, skillName, version string) {
				_, err := queries.GetSkillVersion(
					context.Background(),
					GetSkillVersionParams{
						Name:    skillName,
						Version: version,
					},
				)
				require.Error(t, err)
			},
		},
		{
			name: "get skill version with wrong version",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) (string, string) {
				entryID := createSkillEntry(t, queries, regID, "test-skill", "1.0.0", nil, nil)
				insertSkill(t, queries, entryID, "test-namespace")
				return "test-skill", "9.9.9"
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, skillName, version string) {
				_, err := queries.GetSkillVersion(
					context.Background(),
					GetSkillVersionParams{
						Name:    skillName,
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

			skillName, version := tc.setupFunc(t, queries, regID)
			tc.scenarioFunc(t, queries, skillName, version)
		})
	}
}

func TestListSkills(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		setupFunc    func(t *testing.T, queries *Queries, regID uuid.UUID)
		scenarioFunc func(t *testing.T, queries *Queries)
	}{
		{
			name: "no skills",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries, _ uuid.UUID) {},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries) {
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
				entryID := createSkillEntry(t, queries, regID, "test-skill", "1.0.0", nil, nil)
				insertSkill(t, queries, entryID, "test-namespace")
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries) {
				skills, err := queries.ListSkills(
					context.Background(),
					ListSkillsParams{
						Size: 10,
					},
				)
				require.NoError(t, err)
				require.Len(t, skills, 1)
				require.Equal(t, "test-skill", skills[0].Name)
				require.Equal(t, "1.0.0", skills[0].Version)
				require.Equal(t, "test-namespace", skills[0].Namespace)
				require.Equal(t, RegistryTypeREMOTE, skills[0].RegistryType)
			},
		},
		{
			name: "list multiple skills",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) {
				for _, name := range []string{"alpha-skill", "beta-skill", "gamma-skill"} {
					entryID := createSkillEntry(t, queries, regID, name, "1.0.0", nil, nil)
					insertSkill(t, queries, entryID, "test-namespace")
				}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries) {
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
					entryID := createSkillEntry(t, queries, regID, name, "1.0.0", nil, nil)
					insertSkill(t, queries, entryID, "test-namespace")
				}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries) {
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
					entryID := createSkillEntry(t, queries, regID, name, "1.0.0", nil, nil)
					insertSkill(t, queries, entryID, "test-namespace")
				}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries) {
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
				entryID1 := createSkillEntry(t, queries, regID, "skill-a", "1.0.0", nil, nil)
				insertSkill(t, queries, entryID1, "namespace-1")

				entryID2 := createSkillEntry(t, queries, regID, "skill-b", "1.0.0", nil, nil)
				insertSkill(t, queries, entryID2, "namespace-2")
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries) {
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
					entryID := createSkillEntry(t, queries, regID, name, "1.0.0", nil, nil)
					insertSkill(t, queries, entryID, "test-namespace")
				}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries) {
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
				entryID1 := createSkillEntry(t, queries, regID, "code-review", "1.0.0",
					ptr.String("Automated code review tool"), ptr.String("Code Review"))
				insertSkill(t, queries, entryID1, "test-namespace")

				entryID2 := createSkillEntry(t, queries, regID, "test-runner", "1.0.0",
					ptr.String("Run unit tests"), ptr.String("Test Runner"))
				insertSkill(t, queries, entryID2, "test-namespace")
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries) {
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

				entryID1, err := queries.InsertRegistryEntry(
					context.Background(),
					InsertRegistryEntryParams{
						Name:      "old-skill",
						Version:   "1.0.0",
						RegID:     regID,
						EntryType: EntryTypeSKILL,
						CreatedAt: &oldTime,
						UpdatedAt: &oldTime,
					},
				)
				require.NoError(t, err)
				insertSkill(t, queries, entryID1, "test-namespace")

				entryID2, err := queries.InsertRegistryEntry(
					context.Background(),
					InsertRegistryEntryParams{
						Name:      "recent-skill",
						Version:   "1.0.0",
						RegID:     regID,
						EntryType: EntryTypeSKILL,
						CreatedAt: &recentTime,
						UpdatedAt: &recentTime,
					},
				)
				require.NoError(t, err)
				insertSkill(t, queries, entryID2, "test-namespace")
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries) {
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
				entryID := createSkillEntry(t, queries, regID, "test-skill", "1.0.0", nil, nil)
				insertSkill(t, queries, entryID, "test-namespace")
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries) {
				regName := "test-registry"
				skills, err := queries.ListSkills(
					context.Background(),
					ListSkillsParams{
						RegistryName: &regName,
						Size:         10,
					},
				)
				require.NoError(t, err)
				require.Len(t, skills, 1)

				// Wrong registry name returns empty
				wrongRegName := "wrong-registry"
				skills, err = queries.ListSkills(
					context.Background(),
					ListSkillsParams{
						RegistryName: &wrongRegName,
						Size:         10,
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
				for _, version := range []string{"1.0.0", "2.0.0", "3.0.0"} {
					entryID := createSkillEntry(t, queries, regID, "test-skill", version, nil, nil)
					insertSkill(t, queries, entryID, "test-namespace")
				}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries) {
				skills, err := queries.ListSkills(
					context.Background(),
					ListSkillsParams{
						Size: 10,
					},
				)
				require.NoError(t, err)
				require.Len(t, skills, 3)
				// ORDER BY name ASC, version ASC
				assert.Equal(t, "1.0.0", skills[0].Version)
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
			tc.scenarioFunc(t, queries)
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
				entryID := createSkillEntry(t, queries, regID, "test-skill", "1.0.0", nil, nil)
				skillID := insertSkill(t, queries, entryID, "test-namespace")
				return []uuid.UUID{skillID}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, regID uuid.UUID, ids []uuid.UUID) {
				latestID, err := queries.UpsertLatestSkillVersion(
					context.Background(),
					UpsertLatestSkillVersionParams{
						RegID:   regID,
						Name:    "test-skill",
						Version: "1.0.0",
						EntryID: ids[0],
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
				var skillIDs []uuid.UUID
				for _, version := range []string{"1.0.0", "2.0.0"} {
					entryID := createSkillEntry(t, queries, regID, "test-skill", version, nil, nil)
					skillID := insertSkill(t, queries, entryID, "test-namespace")
					skillIDs = append(skillIDs, skillID)
				}

				// Set initial latest version
				latestID, err := queries.UpsertLatestSkillVersion(
					context.Background(),
					UpsertLatestSkillVersionParams{
						RegID:   regID,
						Name:    "test-skill",
						Version: "1.0.0",
						EntryID: skillIDs[0],
					},
				)
				require.NoError(t, err)
				require.Equal(t, skillIDs[0], latestID)

				return skillIDs
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, regID uuid.UUID, ids []uuid.UUID) {
				// Update latest to version 2.0.0
				latestID, err := queries.UpsertLatestSkillVersion(
					context.Background(),
					UpsertLatestSkillVersionParams{
						RegID:   regID,
						Name:    "test-skill",
						Version: "2.0.0",
						EntryID: ids[1],
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
						RegID:   uuid.New(),
						Name:    "test-skill",
						Version: "1.0.0",
						EntryID: ids[0],
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
						RegID:   regID,
						Name:    "test-skill",
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
					entryID := createSkillEntry(t, queries, regID, name, "1.0.0", nil, nil)
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
						RegID:   regID,
						KeepIds: []uuid.UUID{entryIDs[0]},
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
					entryID := createSkillEntry(t, queries, regID, name, "1.0.0", nil, nil)
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
						RegID:   regID,
						KeepIds: []uuid.UUID{},
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
						RegID:   regID,
						KeepIds: []uuid.UUID{},
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
		scenarioFunc func(t *testing.T, queries *Queries, entryID uuid.UUID)
	}{
		{
			name: "insert git package with minimal fields",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) uuid.UUID {
				entryID := createSkillEntry(t, queries, regID, "test-skill", "1.0.0", nil, nil)
				insertSkill(t, queries, entryID, "test-namespace")
				return entryID
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, entryID uuid.UUID) {
				err := queries.InsertSkillGitPackage(
					context.Background(),
					InsertSkillGitPackageParams{
						SkillEntryID: entryID,
						Url:          "https://github.com/test/skill-repo",
					},
				)
				require.NoError(t, err)
			},
		},
		{
			name: "insert git package with all fields",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) uuid.UUID {
				entryID := createSkillEntry(t, queries, regID, "test-skill", "1.0.0", nil, nil)
				insertSkill(t, queries, entryID, "test-namespace")
				return entryID
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, entryID uuid.UUID) {
				err := queries.InsertSkillGitPackage(
					context.Background(),
					InsertSkillGitPackageParams{
						SkillEntryID: entryID,
						Url:          "https://github.com/test/skill-repo",
						Ref:          ptr.String("refs/tags/v1.0.0"),
						CommitSha:    ptr.String("abc123def456"),
						Subfolder:    ptr.String("skills/my-skill"),
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
			scenarioFunc: func(t *testing.T, queries *Queries, entryID uuid.UUID) {
				err := queries.InsertSkillGitPackage(
					context.Background(),
					InsertSkillGitPackageParams{
						SkillEntryID: entryID,
						Url:          "https://github.com/test/skill-repo",
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
		scenarioFunc func(t *testing.T, queries *Queries, entryID uuid.UUID)
	}{
		{
			name: "insert oci package with minimal fields",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) uuid.UUID {
				entryID := createSkillEntry(t, queries, regID, "test-skill", "1.0.0", nil, nil)
				insertSkill(t, queries, entryID, "test-namespace")
				return entryID
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, entryID uuid.UUID) {
				err := queries.InsertSkillOciPackage(
					context.Background(),
					InsertSkillOciPackageParams{
						SkillEntryID: entryID,
						Identifier:   "ghcr.io/test/skill:v1.0.0",
					},
				)
				require.NoError(t, err)
			},
		},
		{
			name: "insert oci package with all fields",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, regID uuid.UUID) uuid.UUID {
				entryID := createSkillEntry(t, queries, regID, "test-skill", "1.0.0", nil, nil)
				insertSkill(t, queries, entryID, "test-namespace")
				return entryID
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, entryID uuid.UUID) {
				err := queries.InsertSkillOciPackage(
					context.Background(),
					InsertSkillOciPackageParams{
						SkillEntryID: entryID,
						Identifier:   "ghcr.io/test/skill:v1.0.0",
						Digest:       ptr.String("sha256:abcdef1234567890"),
						MediaType:    ptr.String("application/vnd.oci.image.manifest.v1+json"),
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
			scenarioFunc: func(t *testing.T, queries *Queries, entryID uuid.UUID) {
				err := queries.InsertSkillOciPackage(
					context.Background(),
					InsertSkillOciPackageParams{
						SkillEntryID: entryID,
						Identifier:   "ghcr.io/test/skill:v1.0.0",
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
				entryID := createSkillEntry(t, queries, regID, "test-skill", "1.0.0", nil, nil)
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
				entryID := createSkillEntry(t, queries, regID, "test-skill", "1.0.0", nil, nil)
				insertSkill(t, queries, entryID, "test-namespace")

				err := queries.InsertSkillGitPackage(
					context.Background(),
					InsertSkillGitPackageParams{
						SkillEntryID: entryID,
						Url:          "https://github.com/test/skill-repo",
						Ref:          ptr.String("refs/tags/v1.0.0"),
						CommitSha:    ptr.String("abc123"),
						Subfolder:    ptr.String("skills/my-skill"),
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
				assert.Equal(t, entryIDs[0], packages[0].SkillEntryID)
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
					entryID := createSkillEntry(t, queries, regID, name, "1.0.0", nil, nil)
					insertSkill(t, queries, entryID, "test-namespace")

					err := queries.InsertSkillGitPackage(
						context.Background(),
						InsertSkillGitPackageParams{
							SkillEntryID: entryID,
							Url:          fmt.Sprintf("https://github.com/test/skill-repo-%d", i+1),
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
					entryIDMap[pkg.SkillEntryID] = true
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
				entryID := createSkillEntry(t, queries, regID, "test-skill", "1.0.0", nil, nil)
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
				entryID := createSkillEntry(t, queries, regID, "test-skill", "1.0.0", nil, nil)
				insertSkill(t, queries, entryID, "test-namespace")

				err := queries.InsertSkillOciPackage(
					context.Background(),
					InsertSkillOciPackageParams{
						SkillEntryID: entryID,
						Identifier:   "ghcr.io/test/skill:v1.0.0",
						Digest:       ptr.String("sha256:abcdef1234567890"),
						MediaType:    ptr.String("application/vnd.oci.image.manifest.v1+json"),
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
				assert.Equal(t, entryIDs[0], packages[0].SkillEntryID)
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
					entryID := createSkillEntry(t, queries, regID, name, "1.0.0", nil, nil)
					insertSkill(t, queries, entryID, "test-namespace")

					err := queries.InsertSkillOciPackage(
						context.Background(),
						InsertSkillOciPackageParams{
							SkillEntryID: entryID,
							Identifier:   fmt.Sprintf("ghcr.io/test/skill-%d:v1.0.0", i+1),
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
					entryIDMap[pkg.SkillEntryID] = true
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
