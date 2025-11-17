package sqlc

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
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
				id, err := queries.InsertRegistry(
					context.Background(),
					InsertRegistryParams{
						Name:    "test-registry",
						RegType: RegistryTypeFILE,
					},
				)
				require.NoError(t, err)
				require.NotNil(t, id)
			},
		},
		{
			name: "insert registry with local type",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries) {},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries) {
				id, err := queries.InsertRegistry(
					context.Background(),
					InsertRegistryParams{
						Name:    "test-registry",
						RegType: RegistryTypeLOCAL,
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
				id, err := queries.InsertRegistry(
					context.Background(),
					InsertRegistryParams{
						Name:    "test-registry",
						RegType: RegistryTypeREMOTE,
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
				id, err := queries.InsertRegistry(
					context.Background(),
					InsertRegistryParams{
						Name:    "test-registry",
						RegType: RegistryTypeFILE,
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
				id, err := queries.InsertRegistry(
					context.Background(),
					InsertRegistryParams{
						Name:    "test-registry",
						RegType: RegistryTypeLOCAL,
					},
				)
				require.NoError(t, err)
				require.NotNil(t, id)
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries) {
				id, err := queries.InsertRegistry(
					context.Background(),
					InsertRegistryParams{
						Name:    "test-registry",
						RegType: RegistryTypeLOCAL,
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
				id, err := queries.InsertRegistry(
					context.Background(),
					InsertRegistryParams{
						Name:    "test-registry",
						RegType: RegistryTypeREMOTE,
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
				for i := range 2 {
					id, err := queries.InsertRegistry(
						context.Background(),
						InsertRegistryParams{
							Name:    fmt.Sprintf("test-registry-%d", i),
							RegType: RegistryTypeREMOTE,
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
				for i := range 2 {
					id, err := queries.InsertRegistry(
						context.Background(),
						InsertRegistryParams{
							Name:    fmt.Sprintf("test-registry-%d", i),
							RegType: RegistryTypeREMOTE,
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
					Next: pgtype.Timestamptz{
						Time:  nextTime,
						Valid: true,
					},
				})
				require.NoError(t, err)
				require.NotEmpty(t, registries)
				require.Len(t, registries, 2)
				require.Equal(t, "test-registry-1", registries[0].Name)
				require.Equal(t, RegistryTypeREMOTE, registries[0].RegType)
				require.Equal(t, "test-registry-0", registries[1].Name)
				require.Equal(t, RegistryTypeREMOTE, registries[1].RegType)

				require.True(t, registries[0].CreatedAt.Time.After(registries[1].CreatedAt.Time))
				require.True(t, registries[0].CreatedAt.Time.After(nextTime))
				require.True(t, registries[1].CreatedAt.Time.After(nextTime))
			},
		},
		{
			name: "list registries with prev page",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries) {
				for i := range 2 {
					id, err := queries.InsertRegistry(
						context.Background(),
						InsertRegistryParams{
							Name:    fmt.Sprintf("test-registry-%d", i),
							RegType: RegistryTypeREMOTE,
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
					Prev: pgtype.Timestamptz{
						Time:  prevTime,
						Valid: true,
					},
				})
				require.NoError(t, err)
				require.NotEmpty(t, registries)
				require.Len(t, registries, 2)
				require.Equal(t, "test-registry-0", registries[0].Name)
				require.Equal(t, RegistryTypeREMOTE, registries[0].RegType)
				require.Equal(t, "test-registry-1", registries[1].Name)
				require.Equal(t, RegistryTypeREMOTE, registries[1].RegType)

				require.True(t, registries[0].CreatedAt.Time.Before(registries[1].CreatedAt.Time))
				require.True(t, registries[0].CreatedAt.Time.Before(prevTime))
				require.True(t, registries[1].CreatedAt.Time.Before(prevTime))
			},
		},
		{
			name: "list registries with limit",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries) {
				for i := range 2 {
					id, err := queries.InsertRegistry(
						context.Background(),
						InsertRegistryParams{
							Name:    fmt.Sprintf("test-registry-%d", i),
							RegType: RegistryTypeREMOTE,
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
		setupFunc    func(t *testing.T, queries *Queries) []pgtype.UUID
		scenarioFunc func(t *testing.T, queries *Queries, id pgtype.UUID)
	}{
		{
			name: "no registry",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries) []pgtype.UUID {
				// Return non-existent ID
				return []pgtype.UUID{{Bytes: uuid.New(), Valid: true}}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, id pgtype.UUID) {
				_, err := queries.GetRegistry(context.Background(), id)
				require.Error(t, err)
				require.ErrorIs(t, err, sql.ErrNoRows)
			},
		},
		{
			name: "get registry",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries) []pgtype.UUID {
				id, err := queries.InsertRegistry(context.Background(), InsertRegistryParams{
					Name:    "test-registry",
					RegType: RegistryTypeREMOTE,
				})
				require.NoError(t, err)
				require.NotNil(t, id)
				return []pgtype.UUID{id}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, id pgtype.UUID) {
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
