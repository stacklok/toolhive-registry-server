package state

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/db/pgtypes"
	"github.com/stacklok/toolhive-registry-server/internal/db/sqlc"
	"github.com/stacklok/toolhive-registry-server/internal/status"
)

type dbStatusService struct {
	pool *pgxpool.Pool
	// registryConfigsMap caches the registry configs by name from the last Initialize call
	registryConfigsMap map[string]*config.RegistryConfig
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
	// Build registry configs map for caching
	d.registryConfigsMap = make(map[string]*config.RegistryConfig, len(registryConfigs))
	for i := range registryConfigs {
		d.registryConfigsMap[registryConfigs[i].Name] = &registryConfigs[i]
	}

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
	creationTypes := make([]sqlc.CreationType, len(registryConfigs))
	syncSchedules := make([]pgtypes.Interval, len(registryConfigs))
	createdAts := make([]time.Time, len(registryConfigs))
	updatedAts := make([]time.Time, len(registryConfigs))

	for i, reg := range registryConfigs {
		names[i] = reg.Name
		regType, err := mapConfigTypeToDBType(reg.GetType())
		if err != nil {
			return err
		}
		regTypes[i] = regType
		creationTypes[i] = sqlc.CreationTypeCONFIG
		syncSchedules[i] = getSyncScheduleIntervalFromConfig(&reg)
		createdAts[i] = now
		updatedAts[i] = now
	}

	// Validate that registry types haven't changed for existing registries
	if err := validateRegistryTypes(ctx, queries, names, regTypes); err != nil {
		return err
	}

	// Check for API registries that would be overwritten
	if err := checkForAPIRegistryConflicts(ctx, queries, names); err != nil {
		return err
	}

	// Bulk upsert all registries - returns IDs and names
	upsertedRegistries, err := queries.BulkUpsertRegistries(ctx, sqlc.BulkUpsertRegistriesParams{
		Names:         names,
		RegTypes:      regTypes,
		CreationTypes: creationTypes,
		SyncSchedules: syncSchedules,
		CreatedAts:    createdAts,
		UpdatedAts:    updatedAts,
	})
	if err != nil {
		return err
	}

	// Verify all registries were upserted (sanity check)
	if len(upsertedRegistries) != len(names) {
		return fmt.Errorf("expected to upsert %d registries but only %d were returned", len(names), len(upsertedRegistries))
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
		isNonSynced := reg.IsNonSyncedRegistry()
		syncStatuses[i], errorMsgs[i] = getInitialSyncStatus(isNonSynced, reg.GetType())
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

// checkForAPIRegistryConflicts verifies that none of the registries being upserted are API-created registries
func checkForAPIRegistryConflicts(ctx context.Context, queries *sqlc.Queries, names []string) error {
	apiRegistries, err := queries.GetAPIRegistriesByNames(ctx, names)
	if err != nil {
		return fmt.Errorf("failed to check for API registries: %w", err)
	}
	if len(apiRegistries) > 0 {
		// Build list of conflicting registry names
		conflictNames := make([]string, len(apiRegistries))
		for i, reg := range apiRegistries {
			conflictNames[i] = reg.Name
		}
		return fmt.Errorf("cannot overwrite API-created registries: %v", conflictNames)
	}
	return nil
}

// validateRegistryTypes checks that registry types haven't changed for existing registries
func validateRegistryTypes(ctx context.Context, queries *sqlc.Queries, names []string, regTypes []sqlc.RegistryType) error {
	existingRegistries, err := queries.ListAllRegistryNames(ctx)
	if err != nil {
		return fmt.Errorf("failed to list existing registries: %w", err)
	}

	// Build a map of config name -> type for quick lookup
	configTypeMap := make(map[string]sqlc.RegistryType)
	for i, name := range names {
		configTypeMap[name] = regTypes[i]
	}

	// Check if any existing registry has a different type in the config
	for _, existingName := range existingRegistries {
		if configType, exists := configTypeMap[existingName]; exists {
			// Registry exists in both DB and config - check if type matches
			existingReg, err := queries.GetRegistryByName(ctx, existingName)
			if err != nil {
				return fmt.Errorf("failed to get registry %s: %w", existingName, err)
			}
			if existingReg.RegType != configType {
				return fmt.Errorf("registry '%s' type cannot be changed from %s to %s",
					existingName, existingReg.RegType, configType)
			}
		}
	}

	return nil
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
		SyncSchedule: intervalToString(row.SyncSchedule),
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

// dbSyncRowByLastUpdateToStatus converts a ListRegistrySyncsByLastUpdateRow to a status.SyncStatus
func dbSyncRowByLastUpdateToStatus(row sqlc.ListRegistrySyncsByLastUpdateRow) *status.SyncStatus {
	syncStatus := &status.SyncStatus{
		Phase:        dbSyncStatusToPhase(row.SyncStatus),
		LastAttempt:  row.StartedAt,
		LastSyncTime: row.EndedAt,
		AttemptCount: int(row.AttemptCount),
		ServerCount:  int(row.ServerCount),
		SyncSchedule: intervalToString(row.SyncSchedule),
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
func mapConfigTypeToDBType(configType config.SourceType) (sqlc.RegistryType, error) {
	switch configType {
	case config.SourceTypeGit:
		return sqlc.RegistryTypeREMOTE, nil
	case config.SourceTypeAPI:
		return sqlc.RegistryTypeREMOTE, nil
	case config.SourceTypeFile:
		return sqlc.RegistryTypeFILE, nil
	case config.SourceTypeManaged:
		return sqlc.RegistryTypeMANAGED, nil
	case config.SourceTypeKubernetes:
		return sqlc.RegistryTypeKUBERNETES, nil
	default:
		return "", fmt.Errorf("unrecognized registry type: %s", configType)
	}
}

// getInitialSyncStatus returns the initial sync status and error message for a registry.
// Non-synced registries (managed and kubernetes) start with COMPLETED status since they don't
// sync from external sources. Synced registries start with FAILED to trigger initial sync.
func getInitialSyncStatus(isNonSynced bool, regType config.SourceType) (sqlc.SyncStatus, string) {
	if isNonSynced {
		return sqlc.SyncStatusCOMPLETED, fmt.Sprintf("Non-synced registry (type: %s)", regType)
	}
	return sqlc.SyncStatusFAILED, "No previous sync status found"
}

// intervalToString converts a pgtypes.Interval to a duration string (e.g., "30m", "1h").
// Returns empty string for NULL intervals.
func intervalToString(interval pgtypes.Interval) string {
	if !interval.Valid {
		return ""
	}
	return interval.Duration.String()
}

// getSyncScheduleIntervalFromConfig extracts the sync schedule interval from a registry config.
// Returns a NULL interval for non-synced registries (managed, kubernetes) or if no sync policy is configured.
func getSyncScheduleIntervalFromConfig(reg *config.RegistryConfig) pgtypes.Interval {
	if reg.IsNonSyncedRegistry() {
		return pgtypes.NewNullInterval()
	}
	if reg.SyncPolicy == nil || reg.SyncPolicy.Interval == "" {
		return pgtypes.NewNullInterval()
	}
	// Parse the duration string from config (e.g., "30m", "1h")
	interval, err := pgtypes.ParseDuration(reg.SyncPolicy.Interval)
	if err != nil {
		// If parsing fails, return NULL interval
		// This shouldn't happen in production as config validation should catch this
		return pgtypes.NewNullInterval()
	}
	return interval
}

func (d *dbStatusService) GetNextSyncJob(
	ctx context.Context,
	predicate func(*config.RegistryConfig, *status.SyncStatus) bool,
) (*config.RegistryConfig, error) {
	// Start a transaction
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Create queries with transaction
	queries := sqlc.New(d.pool).WithTx(tx)

	// List all registries ordered by last update (ended_at) in ascending order
	// Using FOR UPDATE SKIP LOCKED to prevent race conditions
	registries, err := queries.ListRegistrySyncsByLastUpdate(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list registries: %w", err)
	}

	// Iterate through registries and find one that matches the predicate
	for _, reg := range registries {
		syncStatus := dbSyncRowByLastUpdateToStatus(reg)

		// Find the matching registry configuration from cached configs
		regCfg, ok := d.registryConfigsMap[reg.Name]
		if !ok {
			// Registry exists in DB but not in config - this shouldn't happen
			// but handle gracefully by continuing to next registry
			continue
		}

		// Skip non-synced registries - they don't sync from external sources
		if regCfg.IsNonSyncedRegistry() {
			continue
		}

		// Check if this registry matches the predicate
		if predicate(regCfg, syncStatus) {
			// Update the registry to IN_PROGRESS state
			now := time.Now()
			err = queries.UpdateRegistrySyncStatusByName(ctx, sqlc.UpdateRegistrySyncStatusByNameParams{
				Name:       reg.Name,
				SyncStatus: sqlc.SyncStatusINPROGRESS,
				StartedAt:  &now,
			})
			if err != nil {
				return nil, fmt.Errorf("failed to update registry status: %w", err)
			}

			// Commit the transaction
			if err := tx.Commit(ctx); err != nil {
				return nil, fmt.Errorf("failed to commit transaction: %w", err)
			}

			// Return the registry configuration
			return regCfg, nil
		}
	}

	// No matching registry found - commit transaction and return nil
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil, nil
}
