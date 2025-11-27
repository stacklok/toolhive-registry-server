package state

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/db/sqlc"
	"github.com/stacklok/toolhive-registry-server/internal/status"
)

type dbStatusService struct {
	pool *pgxpool.Pool
}

// ErrRegistryNotFound is returned when a registry can't be found.
var ErrRegistryNotFound = errors.New("registry not found")

// NewDBStateService creates a new database-backed registry state service
func NewDBStateService(pool *pgxpool.Pool) RegistryStateService {
	return &dbStatusService{
		pool: pool,
	}
}

func (d *dbStatusService) Initialize(ctx context.Context, registryConfigs []config.RegistryConfig) error {
	// Start a transaction for atomicity
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	queries := sqlc.New(d.pool).WithTx(tx)
	now := time.Now()

	if len(registryConfigs) == 0 {
		// No registries in config - delete all registries from DB
		// We can't use BulkUpsertRegistries with empty arrays, so handle separately
		err := queries.DeleteRegistriesNotInList(ctx, []uuid.UUID{})
		if err != nil {
			return err
		}
		return tx.Commit(ctx)
	}

	// Prepare bulk upsert arrays
	names := make([]string, len(registryConfigs))
	regTypes := make([]sqlc.RegistryType, len(registryConfigs))
	createdAts := make([]time.Time, len(registryConfigs))
	updatedAts := make([]time.Time, len(registryConfigs))

	for i, reg := range registryConfigs {
		names[i] = reg.Name
		regTypes[i] = mapConfigTypeToDBType(reg.GetType())
		createdAts[i] = now
		updatedAts[i] = now
	}

	// Bulk upsert all registries - returns IDs and names
	upsertedRegistries, err := queries.BulkUpsertRegistries(ctx, sqlc.BulkUpsertRegistriesParams{
		Names:      names,
		RegTypes:   regTypes,
		CreatedAts: createdAts,
		UpdatedAts: updatedAts,
	})
	if err != nil {
		return err
	}

	// Build map from registry name to ID for sync initialization
	nameToID := make(map[string]uuid.UUID)
	upsertedIDs := make([]uuid.UUID, len(upsertedRegistries))
	for i, reg := range upsertedRegistries {
		nameToID[reg.Name] = reg.ID
		upsertedIDs[i] = reg.ID
	}

	// Prepare bulk sync initialization arrays
	regIDs := make([]uuid.UUID, len(registryConfigs))
	syncStatuses := make([]sqlc.SyncStatus, len(registryConfigs))
	errorMsgs := make([]string, len(registryConfigs))

	for i, reg := range registryConfigs {
		regIDs[i] = nameToID[reg.Name]
		isManaged := reg.GetType() == config.SourceTypeManaged
		syncStatuses[i], errorMsgs[i] = getInitialSyncStatus(isManaged)
	}

	// Bulk initialize sync statuses (ON CONFLICT DO NOTHING)
	err = queries.BulkInitializeRegistrySyncs(ctx, sqlc.BulkInitializeRegistrySyncsParams{
		RegIds:       regIDs,
		SyncStatuses: syncStatuses,
		ErrorMsgs:    errorMsgs,
	})
	if err != nil {
		return err
	}

	// Delete any registries not in the upserted list
	// CASCADE will automatically delete associated sync statuses
	err = queries.DeleteRegistriesNotInList(ctx, upsertedIDs)
	if err != nil {
		return err
	}

	// Commit the transaction
	return tx.Commit(ctx)
}

func (d *dbStatusService) ListSyncStatuses(ctx context.Context) (map[string]*status.SyncStatus, error) {
	queries := sqlc.New(d.pool)

	rows, err := queries.ListRegistrySyncs(ctx)
	if err != nil {
		return nil, err
	}

	// Build map of registry name to sync status
	result := make(map[string]*status.SyncStatus)
	for _, row := range rows {
		result[row.Name] = dbSyncRowToStatus(row)
	}

	return result, nil
}

func (d *dbStatusService) GetSyncStatus(ctx context.Context, registryName string) (*status.SyncStatus, error) {
	queries := sqlc.New(d.pool)

	registrySync, err := queries.GetRegistrySyncByName(ctx, registryName)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrRegistryNotFound
		}
		return nil, err
	}

	return dbSyncToStatus(registrySync), nil
}

func (d *dbStatusService) UpdateSyncStatus(ctx context.Context, registryName string, syncStatus *status.SyncStatus) error {
	queries := sqlc.New(d.pool)

	// Prepare nullable string fields
	var errorMsg *string
	if syncStatus.Message != "" {
		errorMsg = &syncStatus.Message
	}

	var lastSyncHash *string
	if syncStatus.LastSyncHash != "" {
		lastSyncHash = &syncStatus.LastSyncHash
	}

	var lastAppliedFilterHash *string
	if syncStatus.LastAppliedFilterHash != "" {
		lastAppliedFilterHash = &syncStatus.LastAppliedFilterHash
	}

	// Upsert the sync status
	err := queries.UpsertRegistrySyncByName(ctx, sqlc.UpsertRegistrySyncByNameParams{
		Name:                  registryName,
		SyncStatus:            syncPhaseToDBStatus(syncStatus.Phase),
		ErrorMsg:              errorMsg,
		StartedAt:             syncStatus.LastAttempt,
		EndedAt:               syncStatus.LastSyncTime,
		AttemptCount:          int64(syncStatus.AttemptCount),
		LastSyncHash:          lastSyncHash,
		LastAppliedFilterHash: lastAppliedFilterHash,
		ServerCount:           int64(syncStatus.ServerCount),
	})

	return err
}

func (d *dbStatusService) UpdateStatusAtomically(
	ctx context.Context,
	registryName string,
	testAndUpdateFn func(syncStatus *status.SyncStatus) bool,
) (bool, error) {
	// Start a transaction
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)

	// Create queries with transaction
	queries := sqlc.New(d.pool).WithTx(tx)

	// Get the current sync status
	registrySync, err := queries.GetRegistrySyncByName(ctx, registryName)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, ErrRegistryNotFound
		}
		return false, err
	}

	// Convert to status.SyncStatus
	syncStatus := dbSyncToStatus(registrySync)

	// Apply the test and update function
	shouldUpdate := testAndUpdateFn(syncStatus)

	// If the function modified the status, update it in the database
	if shouldUpdate {
		// Prepare nullable string fields
		var errorMsg *string
		if syncStatus.Message != "" {
			errorMsg = &syncStatus.Message
		}

		var lastSyncHash *string
		if syncStatus.LastSyncHash != "" {
			lastSyncHash = &syncStatus.LastSyncHash
		}

		var lastAppliedFilterHash *string
		if syncStatus.LastAppliedFilterHash != "" {
			lastAppliedFilterHash = &syncStatus.LastAppliedFilterHash
		}

		// Update using the upsert query
		err = queries.UpsertRegistrySyncByName(ctx, sqlc.UpsertRegistrySyncByNameParams{
			Name:                  registryName,
			SyncStatus:            syncPhaseToDBStatus(syncStatus.Phase),
			ErrorMsg:              errorMsg,
			StartedAt:             syncStatus.LastAttempt,
			EndedAt:               syncStatus.LastSyncTime,
			AttemptCount:          int64(syncStatus.AttemptCount),
			LastSyncHash:          lastSyncHash,
			LastAppliedFilterHash: lastAppliedFilterHash,
			ServerCount:           int64(syncStatus.ServerCount),
		})
		if err != nil {
			return false, err
		}

		// Commit the transaction
		if err := tx.Commit(ctx); err != nil {
			return false, err
		}
	} else {
		// No update needed, just commit (or rollback is fine too)
		if err := tx.Commit(ctx); err != nil {
			return false, err
		}
	}

	return shouldUpdate, nil
}

// dbSyncToStatus converts a database RegistrySync to a status.SyncStatus
func dbSyncToStatus(dbSync sqlc.RegistrySync) *status.SyncStatus {
	syncStatus := &status.SyncStatus{
		Phase:        dbSyncStatusToPhase(dbSync.SyncStatus),
		LastAttempt:  dbSync.StartedAt,
		LastSyncTime: dbSync.EndedAt,
		AttemptCount: int(dbSync.AttemptCount),
		ServerCount:  int(dbSync.ServerCount),
	}

	// Set message from error_msg if present
	if dbSync.ErrorMsg != nil {
		syncStatus.Message = *dbSync.ErrorMsg
	}

	// Set hash fields if present
	if dbSync.LastSyncHash != nil {
		syncStatus.LastSyncHash = *dbSync.LastSyncHash
	}
	if dbSync.LastAppliedFilterHash != nil {
		syncStatus.LastAppliedFilterHash = *dbSync.LastAppliedFilterHash
	}

	return syncStatus
}

// dbSyncRowToStatus converts a ListRegistrySyncsRow to a status.SyncStatus
func dbSyncRowToStatus(row sqlc.ListRegistrySyncsRow) *status.SyncStatus {
	syncStatus := &status.SyncStatus{
		Phase:        dbSyncStatusToPhase(row.SyncStatus),
		LastAttempt:  row.StartedAt,
		LastSyncTime: row.EndedAt,
		AttemptCount: int(row.AttemptCount),
		ServerCount:  int(row.ServerCount),
	}

	// Set message from error_msg if present
	if row.ErrorMsg != nil {
		syncStatus.Message = *row.ErrorMsg
	}

	// Set hash fields if present
	if row.LastSyncHash != nil {
		syncStatus.LastSyncHash = *row.LastSyncHash
	}
	if row.LastAppliedFilterHash != nil {
		syncStatus.LastAppliedFilterHash = *row.LastAppliedFilterHash
	}

	return syncStatus
}

// dbSyncStatusToPhase converts database sync_status enum to status.SyncPhase
func dbSyncStatusToPhase(dbStatus sqlc.SyncStatus) status.SyncPhase {
	switch dbStatus {
	case sqlc.SyncStatusINPROGRESS:
		return status.SyncPhaseSyncing
	case sqlc.SyncStatusCOMPLETED:
		return status.SyncPhaseComplete
	case sqlc.SyncStatusFAILED:
		return status.SyncPhaseFailed
	default:
		return status.SyncPhaseFailed
	}
}

// syncPhaseToDBStatus converts status.SyncPhase to database sync_status enum
func syncPhaseToDBStatus(phase status.SyncPhase) sqlc.SyncStatus {
	switch phase {
	case status.SyncPhaseSyncing:
		return sqlc.SyncStatusINPROGRESS
	case status.SyncPhaseComplete:
		return sqlc.SyncStatusCOMPLETED
	case status.SyncPhaseFailed:
		return sqlc.SyncStatusFAILED
	default:
		return sqlc.SyncStatusFAILED
	}
}

// mapConfigTypeToDBType maps config source types to database registry types
func mapConfigTypeToDBType(configType string) sqlc.RegistryType {
	switch configType {
	case config.SourceTypeGit:
		return sqlc.RegistryTypeREMOTE
	case config.SourceTypeAPI:
		return sqlc.RegistryTypeREMOTE
	case config.SourceTypeFile:
		return sqlc.RegistryTypeFILE
	case config.SourceTypeManaged:
		return sqlc.RegistryTypeMANAGED
	default:
		return sqlc.RegistryTypeMANAGED
	}
}

// getInitialSyncStatus returns the initial sync status and error message for a registry
func getInitialSyncStatus(isManaged bool) (sqlc.SyncStatus, string) {
	if isManaged {
		return sqlc.SyncStatusCOMPLETED, "Managed registry (data managed via API)"
	}
	return sqlc.SyncStatusFAILED, "No previous sync status found"
}
