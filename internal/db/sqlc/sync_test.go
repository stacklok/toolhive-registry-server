package sqlc

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	_ "github.com/lib/pq" // Register postgres driver
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/database"
)

func TestGetRegistrySync(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		setupFunc    func(t *testing.T, queries *Queries, db *pgx.Conn) []pgtype.UUID
		scenarioFunc func(t *testing.T, queries *Queries, ids []pgtype.UUID)
	}{
		{
			name: "no sync record",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries, _ *pgx.Conn) []pgtype.UUID {
				// Return non-existent ID
				return []pgtype.UUID{{Bytes: uuid.New(), Valid: true}}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, ids []pgtype.UUID) {
				_, err := queries.GetRegistrySync(context.Background(), ids[0])
				require.Error(t, err)
				require.ErrorIs(t, err, sql.ErrNoRows)
			},
		},
		{
			name: "get sync record",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, _ *pgx.Conn) []pgtype.UUID {
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
						ErrorMsg:   pgtype.Text{Valid: false},
					},
				)
				require.NoError(t, err)
				require.NotNil(t, syncID)
				return []pgtype.UUID{syncID}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, ids []pgtype.UUID) {
				sync, err := queries.GetRegistrySync(context.Background(), ids[0])
				require.NoError(t, err)
				require.NotNil(t, sync)
				require.Equal(t, ids[0], sync.ID)
				require.Equal(t, SyncStatusINPROGRESS, sync.SyncStatus)
				require.False(t, sync.ErrorMsg.Valid)
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
		setupFunc    func(t *testing.T, queries *Queries) []pgtype.UUID
		scenarioFunc func(t *testing.T, queries *Queries, ids []pgtype.UUID)
	}{
		{
			name: "insert sync with IN_PROGRESS status",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries) []pgtype.UUID {
				regID, err := queries.InsertRegistry(
					context.Background(),
					InsertRegistryParams{
						Name:    "test-registry",
						RegType: RegistryTypeREMOTE,
					},
				)
				require.NoError(t, err)
				require.NotNil(t, regID)
				return []pgtype.UUID{regID}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, ids []pgtype.UUID) {
				_, err := queries.InsertRegistrySync(
					context.Background(),
					InsertRegistrySyncParams{
						RegID:      ids[0],
						SyncStatus: SyncStatusINPROGRESS,
						ErrorMsg:   pgtype.Text{Valid: false},
					},
				)
				require.NoError(t, err)
			},
		},
		{
			name: "insert sync with COMPLETED status",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries) []pgtype.UUID {
				regID, err := queries.InsertRegistry(
					context.Background(),
					InsertRegistryParams{
						Name:    "test-registry",
						RegType: RegistryTypeREMOTE,
					},
				)
				require.NoError(t, err)
				require.NotNil(t, regID)
				return []pgtype.UUID{regID}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, ids []pgtype.UUID) {
				_, err := queries.InsertRegistrySync(
					context.Background(),
					InsertRegistrySyncParams{
						RegID:      ids[0],
						SyncStatus: SyncStatusCOMPLETED,
						ErrorMsg:   pgtype.Text{Valid: false},
					},
				)
				require.NoError(t, err)
			},
		},
		{
			name: "insert sync with FAILED status",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries) []pgtype.UUID {
				regID, err := queries.InsertRegistry(
					context.Background(),
					InsertRegistryParams{
						Name:    "test-registry",
						RegType: RegistryTypeREMOTE,
					},
				)
				require.NoError(t, err)
				require.NotNil(t, regID)
				return []pgtype.UUID{regID}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, ids []pgtype.UUID) {
				_, err := queries.InsertRegistrySync(
					context.Background(),
					InsertRegistrySyncParams{
						RegID:      ids[0],
						SyncStatus: SyncStatusFAILED,
						ErrorMsg:   pgtype.Text{Valid: false},
					},
				)
				require.NoError(t, err)
			},
		},
		{
			name: "insert sync with error message",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries) []pgtype.UUID {
				regID, err := queries.InsertRegistry(
					context.Background(),
					InsertRegistryParams{
						Name:    "test-registry",
						RegType: RegistryTypeREMOTE,
					},
				)
				require.NoError(t, err)
				require.NotNil(t, regID)
				return []pgtype.UUID{regID}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, ids []pgtype.UUID) {
				_, err := queries.InsertRegistrySync(
					context.Background(),
					InsertRegistrySyncParams{
						RegID:      ids[0],
						SyncStatus: SyncStatusFAILED,
						ErrorMsg:   pgtype.Text{String: "sync failed with error", Valid: true},
					},
				)
				require.NoError(t, err)
			},
		},
		{
			name: "insert sync without error message",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries) []pgtype.UUID {
				regID, err := queries.InsertRegistry(
					context.Background(),
					InsertRegistryParams{
						Name:    "test-registry",
						RegType: RegistryTypeREMOTE,
					},
				)
				require.NoError(t, err)
				require.NotNil(t, regID)
				return []pgtype.UUID{regID}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, ids []pgtype.UUID) {
				_, err := queries.InsertRegistrySync(
					context.Background(),
					InsertRegistrySyncParams{
						RegID:      ids[0],
						SyncStatus: SyncStatusINPROGRESS,
						ErrorMsg:   pgtype.Text{Valid: false},
					},
				)
				require.NoError(t, err)
			},
		},
		{
			name: "insert sync with invalid reg_id",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries) []pgtype.UUID {
				// Return non-existent registry ID
				return []pgtype.UUID{{Bytes: uuid.New(), Valid: true}}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, ids []pgtype.UUID) {
				_, err := queries.InsertRegistrySync(
					context.Background(),
					InsertRegistrySyncParams{
						RegID:      ids[0],
						SyncStatus: SyncStatusINPROGRESS,
						ErrorMsg:   pgtype.Text{Valid: false},
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
		setupFunc    func(t *testing.T, queries *Queries, db *pgx.Conn) []pgtype.UUID
		scenarioFunc func(t *testing.T, queries *Queries, ids []pgtype.UUID)
	}{
		{
			name: "update sync status to COMPLETED",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, _ *pgx.Conn) []pgtype.UUID {
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
						ErrorMsg:   pgtype.Text{Valid: false},
					},
				)
				require.NoError(t, err)
				require.NotNil(t, syncID)
				return []pgtype.UUID{syncID}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, ids []pgtype.UUID) {
				endedAt := time.Now().UTC()
				err := queries.UpdateRegistrySync(
					context.Background(),
					UpdateRegistrySyncParams{
						ID:         ids[0],
						SyncStatus: SyncStatusCOMPLETED,
						ErrorMsg:   pgtype.Text{Valid: false},
						EndedAt:    pgtype.Timestamptz{Time: endedAt, Valid: true},
					},
				)
				require.NoError(t, err)

				// Verify the update
				sync, err := queries.GetRegistrySync(context.Background(), ids[0])
				require.NoError(t, err)
				require.Equal(t, SyncStatusCOMPLETED, sync.SyncStatus)
				require.True(t, sync.EndedAt.Valid)
			},
		},
		{
			name: "update sync status to FAILED with error message",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, _ *pgx.Conn) []pgtype.UUID {
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
						ErrorMsg:   pgtype.Text{Valid: false},
					},
				)
				require.NoError(t, err)
				require.NotNil(t, syncID)
				return []pgtype.UUID{syncID}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, ids []pgtype.UUID) {
				endedAt := time.Now().UTC()
				err := queries.UpdateRegistrySync(
					context.Background(),
					UpdateRegistrySyncParams{
						ID:         ids[0],
						SyncStatus: SyncStatusFAILED,
						ErrorMsg:   pgtype.Text{String: "update error message", Valid: true},
						EndedAt:    pgtype.Timestamptz{Time: endedAt, Valid: true},
					},
				)
				require.NoError(t, err)

				// Verify the update
				sync, err := queries.GetRegistrySync(context.Background(), ids[0])
				require.NoError(t, err)
				require.Equal(t, SyncStatusFAILED, sync.SyncStatus)
				require.True(t, sync.ErrorMsg.Valid)
				require.Equal(t, "update error message", sync.ErrorMsg.String)
				require.True(t, sync.EndedAt.Valid)
			},
		},
		{
			name: "update sync with ended_at timestamp",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, _ *pgx.Conn) []pgtype.UUID {
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
						ErrorMsg:   pgtype.Text{Valid: false},
					},
				)
				require.NoError(t, err)
				require.NotNil(t, syncID)
				return []pgtype.UUID{syncID}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, ids []pgtype.UUID) {
				endedAt := time.Now().UTC()
				err := queries.UpdateRegistrySync(
					context.Background(),
					UpdateRegistrySyncParams{
						ID:         ids[0],
						SyncStatus: SyncStatusCOMPLETED,
						ErrorMsg:   pgtype.Text{Valid: false},
						EndedAt:    pgtype.Timestamptz{Time: endedAt, Valid: true},
					},
				)
				require.NoError(t, err)

				// Verify the update
				sync, err := queries.GetRegistrySync(context.Background(), ids[0])
				require.NoError(t, err)
				require.True(t, sync.EndedAt.Valid)
				require.True(t, sync.EndedAt.Time.Equal(endedAt) || sync.EndedAt.Time.After(endedAt.Add(-time.Second)))
			},
		},
		{
			name: "update sync without ended_at",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(t *testing.T, queries *Queries, _ *pgx.Conn) []pgtype.UUID {
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
						ErrorMsg:   pgtype.Text{Valid: false},
					},
				)
				require.NoError(t, err)
				require.NotNil(t, syncID)
				return []pgtype.UUID{syncID}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, ids []pgtype.UUID) {
				err := queries.UpdateRegistrySync(
					context.Background(),
					UpdateRegistrySyncParams{
						ID:         ids[0],
						SyncStatus: SyncStatusCOMPLETED,
						ErrorMsg:   pgtype.Text{Valid: false},
						EndedAt:    pgtype.Timestamptz{Valid: false},
					},
				)
				require.NoError(t, err)

				// Verify the update
				sync, err := queries.GetRegistrySync(context.Background(), ids[0])
				require.NoError(t, err)
				require.Equal(t, SyncStatusCOMPLETED, sync.SyncStatus)
				require.False(t, sync.EndedAt.Valid)
			},
		},
		{
			name: "update non-existent sync",
			//nolint:thelper // We want to see these lines in the test output
			setupFunc: func(_ *testing.T, _ *Queries, _ *pgx.Conn) []pgtype.UUID {
				// Return non-existent sync ID
				return []pgtype.UUID{{Bytes: uuid.New(), Valid: true}}
			},
			//nolint:thelper // We want to see these lines in the test output
			scenarioFunc: func(t *testing.T, queries *Queries, ids []pgtype.UUID) {
				endedAt := time.Now().UTC()
				err := queries.UpdateRegistrySync(
					context.Background(),
					UpdateRegistrySyncParams{
						ID:         ids[0],
						SyncStatus: SyncStatusCOMPLETED,
						ErrorMsg:   pgtype.Text{Valid: false},
						EndedAt:    pgtype.Timestamptz{Time: endedAt, Valid: true},
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
