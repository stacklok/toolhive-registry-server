package sqlc

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq" // Register postgres driver
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/database"
)

func TestInsertRegistry(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		setupFunc    func(t *testing.T, queries *Queries)
		scenarioFunc func(t *testing.T, queries *Queries)
	}{
		{
			name: "insert registry",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries) {},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries) {
				createdAt := time.Now().UTC()
				id, err := queries.InsertRegistry(
					context.Background(),
					InsertRegistryParams{
						Name:         "test-registry",
						RegType:      RegistryTypeFILE,
						CreationType: CreationTypeCONFIG,
						CreatedAt:    &createdAt,
						UpdatedAt:    &createdAt,
					},
				)
				require.NoError(t, err)
				require.NotNil(t, id)
			},
		},
		{
			name: "insert registry with managed type",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries) {},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries) {
				createdAt := time.Now().UTC()
				id, err := queries.InsertRegistry(
					context.Background(),
					InsertRegistryParams{
						Name:         "test-registry",
						RegType:      RegistryTypeMANAGED,
						CreationType: CreationTypeCONFIG,
						CreatedAt:    &createdAt,
						UpdatedAt:    &createdAt,
					},
				)
				require.NoError(t, err)
				require.NotNil(t, id)
			},
		},
		{
			name: "insert registry with remote type",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries) {},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries) {
				createdAt := time.Now().UTC()
				id, err := queries.InsertRegistry(
					context.Background(),
					InsertRegistryParams{
						Name:         "test-registry",
						RegType:      RegistryTypeREMOTE,
						CreationType: CreationTypeCONFIG,
						CreatedAt:    &createdAt,
						UpdatedAt:    &createdAt,
					},
				)
				require.NoError(t, err)
				require.NotNil(t, id)
			},
		},
		{
			name: "insert registry with file type",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries) {},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries) {
				createdAt := time.Now().UTC()
				id, err := queries.InsertRegistry(
					context.Background(),
					InsertRegistryParams{
						Name:         "test-registry",
						RegType:      RegistryTypeFILE,
						CreationType: CreationTypeCONFIG,
						CreatedAt:    &createdAt,
						UpdatedAt:    &createdAt,
					},
				)
				require.NoError(t, err)
				require.NotNil(t, id)
			},
		},
		{
			name: "insert registry with duplicate name",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries) {
				createdAt := time.Now().UTC()
				id, err := queries.InsertRegistry(
					context.Background(),
					InsertRegistryParams{
						Name:         "test-registry",
						RegType:      RegistryTypeMANAGED,
						CreationType: CreationTypeCONFIG,
						CreatedAt:    &createdAt,
						UpdatedAt:    &createdAt,
					},
				)
				require.NoError(t, err)
				require.NotNil(t, id)
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries) {
				createdAt := time.Now().UTC()
				id, err := queries.InsertRegistry(
					context.Background(),
					InsertRegistryParams{
						Name:         "test-registry",
						RegType:      RegistryTypeMANAGED,
						CreationType: CreationTypeCONFIG,
						CreatedAt:    &createdAt,
						UpdatedAt:    &createdAt,
					},
				)
				require.Error(t, err)
				require.NotNil(t, id)
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

func TestListRegistries(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		setupFunc    func(t *testing.T, queries *Queries)
		scenarioFunc func(t *testing.T, queries *Queries)
	}{
		{
			name: "no registries",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries) {},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries) {
				registries, err := queries.ListRegistries(context.Background(), ListRegistriesParams{
					Size: 10,
				})
				require.NoError(t, err)
				require.Empty(t, registries)
			},
		},
		{
			name: "one registry",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries) {
				createdAt := time.Now().UTC()
				id, err := queries.InsertRegistry(
					context.Background(),
					InsertRegistryParams{
						Name:         "test-registry",
						RegType:      RegistryTypeREMOTE,
						CreationType: CreationTypeCONFIG,
						CreatedAt:    &createdAt,
						UpdatedAt:    &createdAt,
					},
				)
				require.NoError(t, err)
				require.NotNil(t, id)
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries) {
				registries, err := queries.ListRegistries(context.Background(), ListRegistriesParams{
					Size: 10,
				})
				require.NoError(t, err)
				require.NotEmpty(t, registries)
				require.Len(t, registries, 1)
				require.Equal(t, "test-registry", registries[0].Name)
				require.Equal(t, RegistryTypeREMOTE, registries[0].RegType)
			},
		},
		{
			name: "multiple registries",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries) {
				createdAt := time.Now().UTC().Add(-1 * time.Minute)
				for i := range 2 {
					createdAt = createdAt.Add(1 * time.Second)
					id, err := queries.InsertRegistry(
						context.Background(),
						InsertRegistryParams{
							Name:         fmt.Sprintf("test-registry-%d", i),
							RegType:      RegistryTypeREMOTE,
							CreationType: CreationTypeCONFIG,
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
				registries, err := queries.ListRegistries(context.Background(), ListRegistriesParams{
					Size: 10,
				})
				require.NoError(t, err)
				require.NotEmpty(t, registries)
				require.Len(t, registries, 2)
				require.Equal(t, "test-registry-0", registries[0].Name)
				require.Equal(t, RegistryTypeREMOTE, registries[0].RegType)
				require.Equal(t, "test-registry-1", registries[1].Name)
				require.Equal(t, RegistryTypeREMOTE, registries[1].RegType)
			},
		},
		{
			name: "list registries with next page",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries) {
				createdAt := time.Now().UTC().Add(-1 * time.Minute)
				for i := range 2 {
					createdAt = createdAt.Add(1 * time.Second)
					id, err := queries.InsertRegistry(
						context.Background(),
						InsertRegistryParams{
							Name:         fmt.Sprintf("test-registry-%d", i),
							RegType:      RegistryTypeREMOTE,
							CreationType: CreationTypeCONFIG,
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
				registries, err := queries.ListRegistries(context.Background(), ListRegistriesParams{
					Size: 10,
					Next: &nextTime,
				})
				require.NoError(t, err)
				require.NotEmpty(t, registries)
				require.Len(t, registries, 2)
				require.Equal(t, "test-registry-1", registries[0].Name)
				require.Equal(t, RegistryTypeREMOTE, registries[0].RegType)
				require.Equal(t, "test-registry-0", registries[1].Name)
				require.Equal(t, RegistryTypeREMOTE, registries[1].RegType)

				require.True(t, registries[0].CreatedAt.After(*registries[1].CreatedAt))
				require.True(t, registries[0].CreatedAt.After(nextTime))
				require.True(t, registries[1].CreatedAt.After(nextTime))
			},
		},
		{
			name: "list registries with prev page",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries) {
				createdAt := time.Now().UTC().Add(-1 * time.Minute)
				for i := range 2 {
					createdAt = createdAt.Add(1 * time.Second)
					id, err := queries.InsertRegistry(
						context.Background(),
						InsertRegistryParams{
							Name:         fmt.Sprintf("test-registry-%d", i),
							RegType:      RegistryTypeREMOTE,
							CreationType: CreationTypeCONFIG,
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
				registries, err := queries.ListRegistries(context.Background(), ListRegistriesParams{
					Size: 10,
					Prev: &prevTime,
				})
				require.NoError(t, err)
				require.NotEmpty(t, registries)
				require.Len(t, registries, 2)
				require.Equal(t, "test-registry-0", registries[0].Name)
				require.Equal(t, RegistryTypeREMOTE, registries[0].RegType)
				require.Equal(t, "test-registry-1", registries[1].Name)
				require.Equal(t, RegistryTypeREMOTE, registries[1].RegType)

				require.True(t, registries[0].CreatedAt.Before(*registries[1].CreatedAt))
				require.True(t, registries[0].CreatedAt.Before(prevTime))
				require.True(t, registries[1].CreatedAt.Before(prevTime))
			},
		},
		{
			name: "list registries with limit",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries) {
				createdAt := time.Now().UTC().Add(-1 * time.Minute)
				for i := range 2 {
					createdAt = createdAt.Add(1 * time.Second)
					id, err := queries.InsertRegistry(
						context.Background(),
						InsertRegistryParams{
							Name:         fmt.Sprintf("test-registry-%d", i),
							RegType:      RegistryTypeREMOTE,
							CreationType: CreationTypeCONFIG,
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
				registries, err := queries.ListRegistries(context.Background(), ListRegistriesParams{
					Size: 1,
				})
				require.NoError(t, err)
				require.NotEmpty(t, registries)
				require.Len(t, registries, 1)
				require.Equal(t, "test-registry-0", registries[0].Name)
				require.Equal(t, RegistryTypeREMOTE, registries[0].RegType)
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

func TestGetRegistry(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		setupFunc    func(t *testing.T, queries *Queries) []uuid.UUID
		scenarioFunc func(t *testing.T, queries *Queries, id uuid.UUID)
	}{
		{
			name: "no registry",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries) []uuid.UUID {
				// Return non-existent ID
				return []uuid.UUID{uuid.New()}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, id uuid.UUID) {
				_, err := queries.GetRegistry(context.Background(), id)
				require.Error(t, err)
				require.ErrorIs(t, err, sql.ErrNoRows)
			},
		},
		{
			name: "get registry",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries) []uuid.UUID {
				createdAt := time.Now().UTC()
				id, err := queries.InsertRegistry(context.Background(), InsertRegistryParams{
					Name:         "test-registry",
					RegType:      RegistryTypeREMOTE,
					CreationType: CreationTypeCONFIG,
					CreatedAt:    &createdAt,
					UpdatedAt:    &createdAt,
				})
				require.NoError(t, err)
				require.NotNil(t, id)
				return []uuid.UUID{id}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, id uuid.UUID) {
				registry, err := queries.GetRegistry(context.Background(), id)
				require.NoError(t, err)
				require.NotNil(t, registry)
				require.Equal(t, "test-registry", registry.Name)
				require.Equal(t, RegistryTypeREMOTE, registry.RegType)
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

func TestBulkUpsertRegistries(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		setupFunc    func(t *testing.T, queries *Queries)
		scenarioFunc func(t *testing.T, queries *Queries)
	}{
		{
			name: "upsert new registries",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries) {},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries) {
				now := time.Now().UTC()
				result, err := queries.BulkUpsertRegistries(context.Background(), BulkUpsertRegistriesParams{
					Names:         []string{"reg1", "reg2"},
					RegTypes:      []RegistryType{RegistryTypeREMOTE, RegistryTypeFILE},
					CreationTypes: []CreationType{CreationTypeCONFIG, CreationTypeCONFIG},
					CreatedAts:    []time.Time{now, now},
					UpdatedAts:    []time.Time{now, now},
				})
				require.NoError(t, err)
				require.Len(t, result, 2)
			},
		},
		{
			name: "update existing CONFIG registry",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries) {
				now := time.Now().UTC()
				_, err := queries.InsertRegistry(context.Background(), InsertRegistryParams{
					Name:         "config-reg",
					RegType:      RegistryTypeREMOTE,
					CreationType: CreationTypeCONFIG,
					CreatedAt:    &now,
					UpdatedAt:    &now,
				})
				require.NoError(t, err)
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries) {
				now := time.Now().UTC().Add(1 * time.Hour)
				result, err := queries.BulkUpsertRegistries(context.Background(), BulkUpsertRegistriesParams{
					Names:         []string{"config-reg"},
					RegTypes:      []RegistryType{RegistryTypeREMOTE},
					CreationTypes: []CreationType{CreationTypeCONFIG},
					CreatedAts:    []time.Time{now},
					UpdatedAts:    []time.Time{now},
				})
				require.NoError(t, err)
				require.Len(t, result, 1)
				require.Equal(t, "config-reg", result[0].Name)
			},
		},
		{
			name: "cannot update API registry",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries) {
				now := time.Now().UTC()
				_, err := queries.InsertRegistry(context.Background(), InsertRegistryParams{
					Name:         "api-reg",
					RegType:      RegistryTypeMANAGED,
					CreationType: CreationTypeAPI,
					CreatedAt:    &now,
					UpdatedAt:    &now,
				})
				require.NoError(t, err)
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries) {
				now := time.Now().UTC().Add(1 * time.Hour)
				result, err := queries.BulkUpsertRegistries(context.Background(), BulkUpsertRegistriesParams{
					Names:         []string{"api-reg"},
					RegTypes:      []RegistryType{RegistryTypeMANAGED},
					CreationTypes: []CreationType{CreationTypeCONFIG},
					CreatedAts:    []time.Time{now},
					UpdatedAts:    []time.Time{now},
				})
				// The upsert should succeed but not update the API registry
				// Because WHERE clause prevents the update, no rows are returned
				require.NoError(t, err)
				require.Len(t, result, 0)

				// Verify the registry still exists and is API type
				reg, err := queries.GetRegistryByName(context.Background(), "api-reg")
				require.NoError(t, err)
				require.Equal(t, CreationTypeAPI, reg.CreationType)
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

func TestDeleteRegistriesNotInList(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		setupFunc    func(t *testing.T, queries *Queries) []uuid.UUID
		scenarioFunc func(t *testing.T, queries *Queries, keepIDs []uuid.UUID)
	}{
		{
			name: "delete CONFIG registries not in list",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries) []uuid.UUID {
				now := time.Now().UTC()
				id1, err := queries.InsertRegistry(context.Background(), InsertRegistryParams{
					Name:         "keep-reg",
					RegType:      RegistryTypeREMOTE,
					CreationType: CreationTypeCONFIG,
					CreatedAt:    &now,
					UpdatedAt:    &now,
				})
				require.NoError(t, err)

				_, err = queries.InsertRegistry(context.Background(), InsertRegistryParams{
					Name:         "delete-reg",
					RegType:      RegistryTypeFILE,
					CreationType: CreationTypeCONFIG,
					CreatedAt:    &now,
					UpdatedAt:    &now,
				})
				require.NoError(t, err)

				return []uuid.UUID{id1}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, keepIDs []uuid.UUID) {
				err := queries.DeleteRegistriesNotInList(context.Background(), keepIDs)
				require.NoError(t, err)

				// Verify only keep-reg remains
				_, err = queries.GetRegistryByName(context.Background(), "keep-reg")
				require.NoError(t, err)

				_, err = queries.GetRegistryByName(context.Background(), "delete-reg")
				require.Error(t, err)
				require.ErrorIs(t, err, sql.ErrNoRows)
			},
		},
		{
			name: "do not delete API registries",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries) []uuid.UUID {
				now := time.Now().UTC()
				id1, err := queries.InsertRegistry(context.Background(), InsertRegistryParams{
					Name:         "config-reg",
					RegType:      RegistryTypeREMOTE,
					CreationType: CreationTypeCONFIG,
					CreatedAt:    &now,
					UpdatedAt:    &now,
				})
				require.NoError(t, err)

				_, err = queries.InsertRegistry(context.Background(), InsertRegistryParams{
					Name:         "api-reg",
					RegType:      RegistryTypeMANAGED,
					CreationType: CreationTypeAPI,
					CreatedAt:    &now,
					UpdatedAt:    &now,
				})
				require.NoError(t, err)

				return []uuid.UUID{id1}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, keepIDs []uuid.UUID) {
				err := queries.DeleteRegistriesNotInList(context.Background(), keepIDs)
				require.NoError(t, err)

				// Verify both registries still exist - API registry is protected
				_, err = queries.GetRegistryByName(context.Background(), "config-reg")
				require.NoError(t, err)

				_, err = queries.GetRegistryByName(context.Background(), "api-reg")
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

func TestGetAPIRegistriesByNames(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		setupFunc    func(t *testing.T, queries *Queries)
		scenarioFunc func(t *testing.T, queries *Queries)
	}{
		{
			name: "find API registries by names",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries) {
				now := time.Now().UTC()
				_, err := queries.InsertRegistry(context.Background(), InsertRegistryParams{
					Name:         "api-reg",
					RegType:      RegistryTypeMANAGED,
					CreationType: CreationTypeAPI,
					CreatedAt:    &now,
					UpdatedAt:    &now,
				})
				require.NoError(t, err)

				_, err = queries.InsertRegistry(context.Background(), InsertRegistryParams{
					Name:         "config-reg",
					RegType:      RegistryTypeREMOTE,
					CreationType: CreationTypeCONFIG,
					CreatedAt:    &now,
					UpdatedAt:    &now,
				})
				require.NoError(t, err)
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries) {
				result, err := queries.GetAPIRegistriesByNames(context.Background(), []string{"api-reg", "config-reg", "nonexistent"})
				require.NoError(t, err)
				require.Len(t, result, 1)
				require.Equal(t, "api-reg", result[0].Name)
				require.Equal(t, CreationTypeAPI, result[0].CreationType)
			},
		},
		{
			name: "no API registries found",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries) {
				now := time.Now().UTC()
				_, err := queries.InsertRegistry(context.Background(), InsertRegistryParams{
					Name:         "config-reg",
					RegType:      RegistryTypeREMOTE,
					CreationType: CreationTypeCONFIG,
					CreatedAt:    &now,
					UpdatedAt:    &now,
				})
				require.NoError(t, err)
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries) {
				result, err := queries.GetAPIRegistriesByNames(context.Background(), []string{"config-reg"})
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
