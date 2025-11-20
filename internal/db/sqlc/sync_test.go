package sqlc

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/aws/smithy-go/ptr"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	_ "github.com/lib/pq" // Register postgres driver
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/database"
)

func TestGetRegistrySync(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		setupFunc    func(t *testing.T, queries *Queries, db *pgx.Conn) []uuid.UUID
		scenarioFunc func(t *testing.T, queries *Queries, ids []uuid.UUID)
	}{
		{
			name: "no sync record",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries, _ *pgx.Conn) []uuid.UUID {
				// Return non-existent ID
				return []uuid.UUID{uuid.New()}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, ids []uuid.UUID) {
				_, err := queries.GetRegistrySync(context.Background(), ids[0])
				require.Error(t, err)
				require.ErrorIs(t, err, sql.ErrNoRows)
			},
		},
		{
			name: "get sync record",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, _ *pgx.Conn) []uuid.UUID {
				// Create a registry first
				regID, err := queries.InsertRegistry(
					context.Background(),
					InsertRegistryParams{
						Name:    "test-registry",
						RegType: RegistryTypeREMOTE,
					},
				)
				require.NoError(t, err)
				require.NotNil(t, regID)

				// Insert a sync record
				syncID, err := queries.InsertRegistrySync(
					context.Background(),
					InsertRegistrySyncParams{
						RegID:      regID,
						SyncStatus: SyncStatusINPROGRESS,
						ErrorMsg:   nil,
					},
				)
				require.NoError(t, err)
				require.NotNil(t, syncID)
				return []uuid.UUID{syncID}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, ids []uuid.UUID) {
				sync, err := queries.GetRegistrySync(context.Background(), ids[0])
				require.NoError(t, err)
				require.NotNil(t, sync)
				require.Equal(t, ids[0], sync.ID)
				require.Equal(t, SyncStatusINPROGRESS, sync.SyncStatus)
				require.Nil(t, sync.ErrorMsg)
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

			id := tc.setupFunc(t, queries, db)
			tc.scenarioFunc(t, queries, id)
		})
	}
}

func TestInsertRegistrySync(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		setupFunc    func(t *testing.T, queries *Queries) []uuid.UUID
		scenarioFunc func(t *testing.T, queries *Queries, ids []uuid.UUID)
	}{
		{
			name: "insert sync with IN_PROGRESS status",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries) []uuid.UUID {
				regID, err := queries.InsertRegistry(
					context.Background(),
					InsertRegistryParams{
						Name:    "test-registry",
						RegType: RegistryTypeREMOTE,
					},
				)
				require.NoError(t, err)
				require.NotNil(t, regID)
				return []uuid.UUID{regID}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, ids []uuid.UUID) {
				_, err := queries.InsertRegistrySync(
					context.Background(),
					InsertRegistrySyncParams{
						RegID:      ids[0],
						SyncStatus: SyncStatusINPROGRESS,
						ErrorMsg:   nil,
					},
				)
				require.NoError(t, err)
			},
		},
		{
			name: "insert sync with COMPLETED status",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries) []uuid.UUID {
				regID, err := queries.InsertRegistry(
					context.Background(),
					InsertRegistryParams{
						Name:    "test-registry",
						RegType: RegistryTypeREMOTE,
					},
				)
				require.NoError(t, err)
				require.NotNil(t, regID)
				return []uuid.UUID{regID}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, ids []uuid.UUID) {
				_, err := queries.InsertRegistrySync(
					context.Background(),
					InsertRegistrySyncParams{
						RegID:      ids[0],
						SyncStatus: SyncStatusCOMPLETED,
						ErrorMsg:   nil,
					},
				)
				require.NoError(t, err)
			},
		},
		{
			name: "insert sync with FAILED status",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries) []uuid.UUID {
				regID, err := queries.InsertRegistry(
					context.Background(),
					InsertRegistryParams{
						Name:    "test-registry",
						RegType: RegistryTypeREMOTE,
					},
				)
				require.NoError(t, err)
				require.NotNil(t, regID)
				return []uuid.UUID{regID}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, ids []uuid.UUID) {
				_, err := queries.InsertRegistrySync(
					context.Background(),
					InsertRegistrySyncParams{
						RegID:      ids[0],
						SyncStatus: SyncStatusFAILED,
						ErrorMsg:   nil,
					},
				)
				require.NoError(t, err)
			},
		},
		{
			name: "insert sync with error message",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries) []uuid.UUID {
				regID, err := queries.InsertRegistry(
					context.Background(),
					InsertRegistryParams{
						Name:    "test-registry",
						RegType: RegistryTypeREMOTE,
					},
				)
				require.NoError(t, err)
				require.NotNil(t, regID)
				return []uuid.UUID{regID}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, ids []uuid.UUID) {
				_, err := queries.InsertRegistrySync(
					context.Background(),
					InsertRegistrySyncParams{
						RegID:      ids[0],
						SyncStatus: SyncStatusFAILED,
						ErrorMsg:   ptr.String("sync failed with error"),
					},
				)
				require.NoError(t, err)
			},
		},
		{
			name: "insert sync without error message",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries) []uuid.UUID {
				regID, err := queries.InsertRegistry(
					context.Background(),
					InsertRegistryParams{
						Name:    "test-registry",
						RegType: RegistryTypeREMOTE,
					},
				)
				require.NoError(t, err)
				require.NotNil(t, regID)
				return []uuid.UUID{regID}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, ids []uuid.UUID) {
				_, err := queries.InsertRegistrySync(
					context.Background(),
					InsertRegistrySyncParams{
						RegID:      ids[0],
						SyncStatus: SyncStatusINPROGRESS,
						ErrorMsg:   nil,
					},
				)
				require.NoError(t, err)
			},
		},
		{
			name: "insert sync with invalid reg_id",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries) []uuid.UUID {
				// Return non-existent registry ID
				return []uuid.UUID{uuid.New()}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, ids []uuid.UUID) {
				_, err := queries.InsertRegistrySync(
					context.Background(),
					InsertRegistrySyncParams{
						RegID:      ids[0],
						SyncStatus: SyncStatusINPROGRESS,
						ErrorMsg:   nil,
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

			regID := tc.setupFunc(t, queries)
			tc.scenarioFunc(t, queries, regID)
		})
	}
}

func TestUpdateRegistrySync(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		setupFunc    func(t *testing.T, queries *Queries, db *pgx.Conn) []uuid.UUID
		scenarioFunc func(t *testing.T, queries *Queries, ids []uuid.UUID)
	}{
		{
			name: "update sync status to COMPLETED",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, _ *pgx.Conn) []uuid.UUID {
				// Create a registry
				regID, err := queries.InsertRegistry(
					context.Background(),
					InsertRegistryParams{
						Name:    "test-registry",
						RegType: RegistryTypeREMOTE,
					},
				)
				require.NoError(t, err)
				require.NotNil(t, regID)

				// Insert a sync record
				syncID, err := queries.InsertRegistrySync(
					context.Background(),
					InsertRegistrySyncParams{
						RegID:      regID,
						SyncStatus: SyncStatusINPROGRESS,
						ErrorMsg:   nil,
					},
				)
				require.NoError(t, err)
				require.NotNil(t, syncID)
				return []uuid.UUID{syncID}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, ids []uuid.UUID) {
				endedAt := time.Now().UTC()
				err := queries.UpdateRegistrySync(
					context.Background(),
					UpdateRegistrySyncParams{
						ID:         ids[0],
						SyncStatus: SyncStatusCOMPLETED,
						ErrorMsg:   nil,
						EndedAt:    &endedAt,
					},
				)
				require.NoError(t, err)

				// Verify the update
				sync, err := queries.GetRegistrySync(context.Background(), ids[0])
				require.NoError(t, err)
				require.Equal(t, SyncStatusCOMPLETED, sync.SyncStatus)
				require.True(t, sync.EndedAt.Equal(endedAt))
			},
		},
		{
			name: "update sync status to FAILED with error message",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, _ *pgx.Conn) []uuid.UUID {
				// Create a registry
				regID, err := queries.InsertRegistry(
					context.Background(),
					InsertRegistryParams{
						Name:    "test-registry",
						RegType: RegistryTypeREMOTE,
					},
				)
				require.NoError(t, err)
				require.NotNil(t, regID)

				// Insert a sync record
				syncID, err := queries.InsertRegistrySync(
					context.Background(),
					InsertRegistrySyncParams{
						RegID:      regID,
						SyncStatus: SyncStatusINPROGRESS,
						ErrorMsg:   nil,
					},
				)
				require.NoError(t, err)
				require.NotNil(t, syncID)
				return []uuid.UUID{syncID}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, ids []uuid.UUID) {
				endedAt := time.Now().UTC()
				err := queries.UpdateRegistrySync(
					context.Background(),
					UpdateRegistrySyncParams{
						ID:         ids[0],
						SyncStatus: SyncStatusFAILED,
						ErrorMsg:   ptr.String("update error message"),
						EndedAt:    &endedAt,
					},
				)
				require.NoError(t, err)

				// Verify the update
				sync, err := queries.GetRegistrySync(context.Background(), ids[0])
				require.NoError(t, err)
				require.Equal(t, SyncStatusFAILED, sync.SyncStatus)
				require.NotNil(t, sync.ErrorMsg)
				require.Equal(t, "update error message", *sync.ErrorMsg)
				require.True(t, !sync.EndedAt.IsZero())
			},
		},
		{
			name: "update sync with ended_at timestamp",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, _ *pgx.Conn) []uuid.UUID {
				// Create a registry
				regID, err := queries.InsertRegistry(
					context.Background(),
					InsertRegistryParams{
						Name:    "test-registry",
						RegType: RegistryTypeREMOTE,
					},
				)
				require.NoError(t, err)
				require.NotNil(t, regID)

				// Insert a sync record
				syncID, err := queries.InsertRegistrySync(
					context.Background(),
					InsertRegistrySyncParams{
						RegID:      regID,
						SyncStatus: SyncStatusINPROGRESS,
						ErrorMsg:   nil,
					},
				)
				require.NoError(t, err)
				require.NotNil(t, syncID)
				return []uuid.UUID{syncID}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, ids []uuid.UUID) {
				endedAt := time.Now().UTC()
				err := queries.UpdateRegistrySync(
					context.Background(),
					UpdateRegistrySyncParams{
						ID:         ids[0],
						SyncStatus: SyncStatusCOMPLETED,
						ErrorMsg:   nil,
						EndedAt:    &endedAt,
					},
				)
				require.NoError(t, err)

				// Verify the update
				sync, err := queries.GetRegistrySync(context.Background(), ids[0])
				require.NoError(t, err)
				require.True(t, !sync.EndedAt.IsZero())
				require.True(t, sync.EndedAt.Equal(endedAt) || sync.EndedAt.After(endedAt.Add(-time.Second)))
			},
		},
		{
			name: "update sync without ended_at",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, _ *pgx.Conn) []uuid.UUID {
				// Create a registry
				regID, err := queries.InsertRegistry(
					context.Background(),
					InsertRegistryParams{
						Name:    "test-registry",
						RegType: RegistryTypeREMOTE,
					},
				)
				require.NoError(t, err)
				require.NotNil(t, regID)

				// Insert a sync record
				syncID, err := queries.InsertRegistrySync(
					context.Background(),
					InsertRegistrySyncParams{
						RegID:      regID,
						SyncStatus: SyncStatusINPROGRESS,
						ErrorMsg:   nil,
					},
				)
				require.NoError(t, err)
				require.NotNil(t, syncID)
				return []uuid.UUID{syncID}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, ids []uuid.UUID) {
				err := queries.UpdateRegistrySync(
					context.Background(),
					UpdateRegistrySyncParams{
						ID:         ids[0],
						SyncStatus: SyncStatusCOMPLETED,
						ErrorMsg:   nil,
					},
				)
				require.NoError(t, err)

				// Verify the update
				sync, err := queries.GetRegistrySync(context.Background(), ids[0])
				require.NoError(t, err)
				require.Equal(t, SyncStatusCOMPLETED, sync.SyncStatus)
				require.Nil(t, sync.EndedAt)
			},
		},
		{
			name: "update non-existent sync",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries, _ *pgx.Conn) []uuid.UUID {
				// Return non-existent sync ID
				return []uuid.UUID{uuid.New()}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, ids []uuid.UUID) {
				endedAt := time.Now().UTC()
				err := queries.UpdateRegistrySync(
					context.Background(),
					UpdateRegistrySyncParams{
						ID:         ids[0],
						SyncStatus: SyncStatusCOMPLETED,
						ErrorMsg:   nil,
						EndedAt:    &endedAt,
					},
				)
				// Update should not error even if no rows are affected
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

			syncID := tc.setupFunc(t, queries, db)
			tc.scenarioFunc(t, queries, syncID)
		})
	}
}
