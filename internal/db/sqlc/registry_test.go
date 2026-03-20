package sqlc

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/database"
	"github.com/stacklok/toolhive-registry-server/internal/db/pgtypes"
)

func TestInsertSource(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		setupFunc    func(t *testing.T, queries *Queries)
		scenarioFunc func(t *testing.T, queries *Queries)
	}{
		{
			name: "insert source",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries) {},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries) {
				createdAt := time.Now().UTC()
				id, err := queries.UpsertSource(
					context.Background(),
					UpsertSourceParams{
						CreationType: CreationTypeCONFIG,
						Name:         "test-source",
						SourceType:   "file",
						Syncable:     true,
						CreatedAt:    &createdAt,
						UpdatedAt:    &createdAt,
					},
				)
				require.NoError(t, err)
				require.NotNil(t, id)
			},
		},
		{
			name: "insert source with managed type",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries) {},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries) {
				createdAt := time.Now().UTC()
				id, err := queries.UpsertSource(
					context.Background(),
					UpsertSourceParams{
						CreationType: CreationTypeCONFIG,
						Name:         "test-source",
						SourceType:   "managed",
						Syncable:     false, // managed sources are not syncable
						CreatedAt:    &createdAt,
						UpdatedAt:    &createdAt,
					},
				)
				require.NoError(t, err)
				require.NotNil(t, id)
			},
		},
		{
			name: "insert source with git type",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries) {},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries) {
				createdAt := time.Now().UTC()
				id, err := queries.UpsertSource(
					context.Background(),
					UpsertSourceParams{
						CreationType: CreationTypeCONFIG,
						Name:         "test-source",
						SourceType:   "git",
						Syncable:     true,
						CreatedAt:    &createdAt,
						UpdatedAt:    &createdAt,
					},
				)
				require.NoError(t, err)
				require.NotNil(t, id)
			},
		},
		{
			name: "insert source with file type",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries) {},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries) {
				createdAt := time.Now().UTC()
				id, err := queries.UpsertSource(
					context.Background(),
					UpsertSourceParams{
						CreationType: CreationTypeCONFIG,
						Name:         "test-source",
						SourceType:   "file",
						Syncable:     true,
						CreatedAt:    &createdAt,
						UpdatedAt:    &createdAt,
					},
				)
				require.NoError(t, err)
				require.NotNil(t, id)
			},
		},
		{
			name: "insert source with duplicate name",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries) {
				createdAt := time.Now().UTC()
				source, err := queries.InsertSource(
					context.Background(),
					InsertSourceParams{
						CreationType: CreationTypeCONFIG,
						Name:         "test-source",
						SourceType:   "managed",
						Syncable:     false, // managed sources are not syncable
						CreatedAt:    &createdAt,
						UpdatedAt:    &createdAt,
					},
				)
				require.NoError(t, err)
				require.NotEmpty(t, source.ID)
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries) {
				createdAt := time.Now().UTC()
				_, err := queries.InsertSource(
					context.Background(),
					InsertSourceParams{
						CreationType: CreationTypeCONFIG,
						Name:         "test-source",
						SourceType:   "managed",
						Syncable:     false, // managed sources are not syncable
						CreatedAt:    &createdAt,
						UpdatedAt:    &createdAt,
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

			tc.setupFunc(t, queries)
			tc.scenarioFunc(t, queries)
		})
	}
}

func TestListSources(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		setupFunc    func(t *testing.T, queries *Queries)
		scenarioFunc func(t *testing.T, queries *Queries)
	}{
		{
			name: "no sources",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries) {},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries) {
				sources, err := queries.ListSources(context.Background(), ListSourcesParams{
					Size: 10,
				})
				require.NoError(t, err)
				require.Empty(t, sources)
			},
		},
		{
			name: "one source",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries) {
				createdAt := time.Now().UTC()
				id, err := queries.UpsertSource(
					context.Background(),
					UpsertSourceParams{
						CreationType: CreationTypeCONFIG,
						Name:         "test-source",
						SourceType:   "git",
						Syncable:     true,
						CreatedAt:    &createdAt,
						UpdatedAt:    &createdAt,
					},
				)
				require.NoError(t, err)
				require.NotNil(t, id)
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries) {
				sources, err := queries.ListSources(context.Background(), ListSourcesParams{
					Size: 10,
				})
				require.NoError(t, err)
				require.NotEmpty(t, sources)
				require.Len(t, sources, 1)
				require.Equal(t, "test-source", sources[0].Name)
				require.Equal(t, "git", sources[0].SourceType)
			},
		},
		{
			name: "multiple sources",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries) {
				createdAt := time.Now().UTC().Add(-1 * time.Minute)
				for i := range 2 {
					createdAt = createdAt.Add(1 * time.Second)
					id, err := queries.UpsertSource(
						context.Background(),
						UpsertSourceParams{
							CreationType: CreationTypeCONFIG,
							Name:         fmt.Sprintf("test-source-%d", i),
							SourceType:   "git",
							Syncable:     true,
							CreatedAt:    &createdAt,
							UpdatedAt:    &createdAt,
						},
					)
					require.NoError(t, err)
					require.NotNil(t, id)
				}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries) {
				sources, err := queries.ListSources(context.Background(), ListSourcesParams{
					Size: 10,
				})
				require.NoError(t, err)
				require.NotEmpty(t, sources)
				require.Len(t, sources, 2)
				require.Equal(t, "test-source-0", sources[0].Name)
				require.Equal(t, "git", sources[0].SourceType)
				require.Equal(t, "test-source-1", sources[1].Name)
				require.Equal(t, "git", sources[1].SourceType)
			},
		},
		{
			name: "list sources with next page",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries) {
				createdAt := time.Now().UTC().Add(-1 * time.Minute)
				for i := range 2 {
					createdAt = createdAt.Add(1 * time.Second)
					id, err := queries.UpsertSource(
						context.Background(),
						UpsertSourceParams{
							CreationType: CreationTypeCONFIG,
							Name:         fmt.Sprintf("test-source-%d", i),
							SourceType:   "git",
							Syncable:     true,
							CreatedAt:    &createdAt,
							UpdatedAt:    &createdAt,
						},
					)
					require.NoError(t, err)
					require.NotNil(t, id)
				}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries) {
				nextTime := time.Now().UTC().Add(-10 * time.Minute)
				sources, err := queries.ListSources(context.Background(), ListSourcesParams{
					Size: 10,
					Next: &nextTime,
				})
				require.NoError(t, err)
				require.NotEmpty(t, sources)
				require.Len(t, sources, 2)
				require.Equal(t, "test-source-1", sources[0].Name)
				require.Equal(t, "git", sources[0].SourceType)
				require.Equal(t, "test-source-0", sources[1].Name)
				require.Equal(t, "git", sources[1].SourceType)

				require.True(t, sources[0].CreatedAt.After(*sources[1].CreatedAt))
				require.True(t, sources[0].CreatedAt.After(nextTime))
				require.True(t, sources[1].CreatedAt.After(nextTime))
			},
		},
		{
			name: "list sources with prev page",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries) {
				createdAt := time.Now().UTC().Add(-1 * time.Minute)
				for i := range 2 {
					createdAt = createdAt.Add(1 * time.Second)
					id, err := queries.UpsertSource(
						context.Background(),
						UpsertSourceParams{
							CreationType: CreationTypeCONFIG,
							Name:         fmt.Sprintf("test-source-%d", i),
							SourceType:   "git",
							Syncable:     true,
							CreatedAt:    &createdAt,
							UpdatedAt:    &createdAt,
						},
					)
					require.NoError(t, err)
					require.NotNil(t, id)
				}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries) {
				prevTime := time.Now().UTC().Add(10 * time.Minute)
				sources, err := queries.ListSources(context.Background(), ListSourcesParams{
					Size: 10,
					Prev: &prevTime,
				})
				require.NoError(t, err)
				require.NotEmpty(t, sources)
				require.Len(t, sources, 2)
				require.Equal(t, "test-source-0", sources[0].Name)
				require.Equal(t, "git", sources[0].SourceType)
				require.Equal(t, "test-source-1", sources[1].Name)
				require.Equal(t, "git", sources[1].SourceType)

				require.True(t, sources[0].CreatedAt.Before(*sources[1].CreatedAt))
				require.True(t, sources[0].CreatedAt.Before(prevTime))
				require.True(t, sources[1].CreatedAt.Before(prevTime))
			},
		},
		{
			name: "list sources with limit",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries) {
				createdAt := time.Now().UTC().Add(-1 * time.Minute)
				for i := range 2 {
					createdAt = createdAt.Add(1 * time.Second)
					id, err := queries.UpsertSource(
						context.Background(),
						UpsertSourceParams{
							CreationType: CreationTypeCONFIG,
							Name:         fmt.Sprintf("test-source-%d", i),
							SourceType:   "git",
							Syncable:     true,
							CreatedAt:    &createdAt,
							UpdatedAt:    &createdAt,
						},
					)
					require.NoError(t, err)
					require.NotNil(t, id)
				}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries) {
				sources, err := queries.ListSources(context.Background(), ListSourcesParams{
					Size: 1,
				})
				require.NoError(t, err)
				require.NotEmpty(t, sources)
				require.Len(t, sources, 1)
				require.Equal(t, "test-source-0", sources[0].Name)
				require.Equal(t, "git", sources[0].SourceType)
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

			tc.setupFunc(t, queries)
			tc.scenarioFunc(t, queries)
		})
	}
}

func TestGetSource(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		setupFunc    func(t *testing.T, queries *Queries) []uuid.UUID
		scenarioFunc func(t *testing.T, queries *Queries, id uuid.UUID)
	}{
		{
			name: "no source",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries) []uuid.UUID {
				// Return non-existent ID
				return []uuid.UUID{uuid.New()}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, id uuid.UUID) {
				_, err := queries.GetSource(context.Background(), id)
				require.Error(t, err)
				require.ErrorIs(t, err, sql.ErrNoRows)
			},
		},
		{
			name: "get source",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries) []uuid.UUID {
				createdAt := time.Now().UTC()
				id, err := queries.UpsertSource(context.Background(), UpsertSourceParams{
					CreationType: CreationTypeCONFIG,
					Name:         "test-source",
					SourceType:   "git",
					Syncable:     true,
					CreatedAt:    &createdAt,
					UpdatedAt:    &createdAt,
				})
				require.NoError(t, err)
				require.NotNil(t, id)
				return []uuid.UUID{id}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, id uuid.UUID) {
				source, err := queries.GetSource(context.Background(), id)
				require.NoError(t, err)
				require.NotNil(t, source)
				require.Equal(t, "test-source", source.Name)
				require.Equal(t, "git", source.SourceType)
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

			ids := tc.setupFunc(t, queries)
			tc.scenarioFunc(t, queries, ids[0])
		})
	}
}

func TestBulkUpsertSources(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		setupFunc    func(t *testing.T, queries *Queries)
		scenarioFunc func(t *testing.T, queries *Queries)
	}{
		{
			name: "upsert new sources",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries) {},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries) {
				now := time.Now().UTC()
				result, err := queries.BulkUpsertConfigSources(context.Background(), BulkUpsertConfigSourcesParams{
					Names:         []string{"src1", "src2"},
					SourceTypes:   []string{"git", "file"},
					Formats:       []string{"upstream", "upstream"},
					SourceConfigs: [][]byte{nil, nil},
					FilterConfigs: [][]byte{nil, nil},
					SyncSchedules: []pgtypes.Interval{pgtypes.NewNullInterval(), pgtypes.NewNullInterval()},
					Syncables:     []bool{true, true},
					CreatedAts:    []time.Time{now, now},
					UpdatedAts:    []time.Time{now, now},
				})
				require.NoError(t, err)
				require.Len(t, result, 2)
			},
		},
		{
			name: "update existing CONFIG source",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries) {
				now := time.Now().UTC()
				_, err := queries.UpsertSource(context.Background(), UpsertSourceParams{
					CreationType: CreationTypeCONFIG,
					Name:         "config-src",
					SourceType:   "git",
					Syncable:     true,
					CreatedAt:    &now,
					UpdatedAt:    &now,
				})
				require.NoError(t, err)
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries) {
				now := time.Now().UTC().Add(1 * time.Hour)
				result, err := queries.BulkUpsertConfigSources(context.Background(), BulkUpsertConfigSourcesParams{
					Names:         []string{"config-src"},
					SourceTypes:   []string{"git"},
					Formats:       []string{"upstream"},
					SourceConfigs: [][]byte{nil},
					FilterConfigs: [][]byte{nil},
					SyncSchedules: []pgtypes.Interval{pgtypes.NewNullInterval()},
					Syncables:     []bool{true},
					CreatedAts:    []time.Time{now},
					UpdatedAts:    []time.Time{now},
				})
				require.NoError(t, err)
				require.Len(t, result, 1)
				require.Equal(t, "config-src", result[0].Name)
			},
		},
		{
			name: "cannot update API source",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries) {
				now := time.Now().UTC()
				_, err := queries.InsertSource(context.Background(), InsertSourceParams{
					CreationType: CreationTypeAPI,
					Name:         "api-src",
					SourceType:   "managed",
					Syncable:     false, // managed sources are not syncable
					CreatedAt:    &now,
					UpdatedAt:    &now,
				})
				require.NoError(t, err)
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries) {
				now := time.Now().UTC().Add(1 * time.Hour)
				result, err := queries.BulkUpsertConfigSources(context.Background(), BulkUpsertConfigSourcesParams{
					Names:         []string{"api-src"},
					SourceTypes:   []string{"managed"},
					Formats:       []string{"upstream"},
					SourceConfigs: [][]byte{nil},
					FilterConfigs: [][]byte{nil},
					SyncSchedules: []pgtypes.Interval{pgtypes.NewNullInterval()},
					Syncables:     []bool{false},
					CreatedAts:    []time.Time{now},
					UpdatedAts:    []time.Time{now},
				})
				// The upsert should succeed but not update the API source
				// Because WHERE clause prevents the update, no rows are returned
				require.NoError(t, err)
				require.Len(t, result, 0)

				// Verify the source still exists and is API type
				src, err := queries.GetSourceByName(context.Background(), "api-src")
				require.NoError(t, err)
				require.Equal(t, CreationTypeAPI, src.CreationType)
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

			tc.setupFunc(t, queries)
			tc.scenarioFunc(t, queries)
		})
	}
}

func TestDeleteConfigSourcesNotInList(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		setupFunc    func(t *testing.T, queries *Queries) []uuid.UUID
		scenarioFunc func(t *testing.T, queries *Queries, keepIDs []uuid.UUID)
	}{
		{
			name: "delete CONFIG sources not in list",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries) []uuid.UUID {
				now := time.Now().UTC()
				id1, err := queries.UpsertSource(context.Background(), UpsertSourceParams{
					CreationType: CreationTypeCONFIG,
					Name:         "keep-src",
					SourceType:   "git",
					Syncable:     true,
					CreatedAt:    &now,
					UpdatedAt:    &now,
				})
				require.NoError(t, err)

				_, err = queries.UpsertSource(context.Background(), UpsertSourceParams{
					CreationType: CreationTypeCONFIG,
					Name:         "delete-src",
					SourceType:   "file",
					Syncable:     true,
					CreatedAt:    &now,
					UpdatedAt:    &now,
				})
				require.NoError(t, err)

				return []uuid.UUID{id1}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, keepIDs []uuid.UUID) {
				err := queries.DeleteConfigSourcesNotInList(context.Background(), keepIDs)
				require.NoError(t, err)

				// Verify only keep-src remains
				_, err = queries.GetSourceByName(context.Background(), "keep-src")
				require.NoError(t, err)

				_, err = queries.GetSourceByName(context.Background(), "delete-src")
				require.Error(t, err)
				require.ErrorIs(t, err, sql.ErrNoRows)
			},
		},
		{
			name: "do not delete API sources",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries) []uuid.UUID {
				now := time.Now().UTC()
				id1, err := queries.UpsertSource(context.Background(), UpsertSourceParams{
					CreationType: CreationTypeCONFIG,
					Name:         "config-src",
					SourceType:   "git",
					Syncable:     true,
					CreatedAt:    &now,
					UpdatedAt:    &now,
				})
				require.NoError(t, err)

				_, err = queries.InsertSource(context.Background(), InsertSourceParams{
					CreationType: CreationTypeAPI,
					Name:         "api-src",
					SourceType:   "managed",
					Syncable:     false, // managed sources are not syncable
					CreatedAt:    &now,
					UpdatedAt:    &now,
				})
				require.NoError(t, err)

				return []uuid.UUID{id1}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, keepIDs []uuid.UUID) {
				err := queries.DeleteConfigSourcesNotInList(context.Background(), keepIDs)
				require.NoError(t, err)

				// Verify both sources still exist - API source is protected
				_, err = queries.GetSourceByName(context.Background(), "config-src")
				require.NoError(t, err)

				_, err = queries.GetSourceByName(context.Background(), "api-src")
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

			keepIDs := tc.setupFunc(t, queries)
			tc.scenarioFunc(t, queries, keepIDs)
		})
	}
}

func TestGetAPISourcesByNames(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		setupFunc    func(t *testing.T, queries *Queries)
		scenarioFunc func(t *testing.T, queries *Queries)
	}{
		{
			name: "find API sources by names",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries) {
				now := time.Now().UTC()
				_, err := queries.InsertSource(context.Background(), InsertSourceParams{
					CreationType: CreationTypeAPI,
					Name:         "api-src",
					SourceType:   "managed",
					Syncable:     false, // managed sources are not syncable
					CreatedAt:    &now,
					UpdatedAt:    &now,
				})
				require.NoError(t, err)

				_, err = queries.UpsertSource(context.Background(), UpsertSourceParams{
					CreationType: CreationTypeCONFIG,
					Name:         "config-src",
					SourceType:   "git",
					Syncable:     true,
					CreatedAt:    &now,
					UpdatedAt:    &now,
				})
				require.NoError(t, err)
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries) {
				result, err := queries.GetAPISourcesByNames(context.Background(), []string{"api-src", "config-src", "nonexistent"})
				require.NoError(t, err)
				require.Len(t, result, 1)
				require.Equal(t, "api-src", result[0].Name)
				require.Equal(t, CreationTypeAPI, result[0].CreationType)
			},
		},
		{
			name: "no API sources found",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries) {
				now := time.Now().UTC()
				_, err := queries.UpsertSource(context.Background(), UpsertSourceParams{
					CreationType: CreationTypeCONFIG,
					Name:         "config-src",
					SourceType:   "git",
					Syncable:     true,
					CreatedAt:    &now,
					UpdatedAt:    &now,
				})
				require.NoError(t, err)
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries) {
				result, err := queries.GetAPISourcesByNames(context.Background(), []string{"config-src"})
				require.NoError(t, err)
				require.Len(t, result, 0)
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

			tc.setupFunc(t, queries)
			tc.scenarioFunc(t, queries)
		})
	}
}
