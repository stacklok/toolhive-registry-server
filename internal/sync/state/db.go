package state

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/db"
	"github.com/stacklok/toolhive-registry-server/internal/db/pgtypes"
	"github.com/stacklok/toolhive-registry-server/internal/db/sqlc"
	"github.com/stacklok/toolhive-registry-server/internal/status"
)

type dbStatusService struct {
	pool *pgxpool.Pool
	// sourceConfigsMap caches the source configs by name from the last Initialize call
	sourceConfigsMap map[string]*config.SourceConfig
}

// ErrRegistryNotFound is returned when a registry can't be found.
var ErrRegistryNotFound = errors.New("registry not found")

// NewDBStateService creates a new database-backed registry state service
func NewDBStateService(pool *pgxpool.Pool) RegistryStateService {
	return &dbStatusService{
		pool: pool,
	}
}

func (d *dbStatusService) Initialize(ctx context.Context, cfg *config.Config) error {
	sourceConfigs := cfg.Sources

	// Build source configs map for caching
	d.sourceConfigsMap = make(map[string]*config.SourceConfig, len(sourceConfigs))
	for i := range sourceConfigs {
		d.sourceConfigsMap[sourceConfigs[i].Name] = &sourceConfigs[i]
	}

	// Start a transaction for atomicity
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	queries := sqlc.New(d.pool).WithTx(tx)
	now := time.Now()

	if len(sourceConfigs) == 0 {
		// No sources in config - delete all CONFIG entries from DB.
		if err := queries.DeleteConfigRegistriesNotInList(ctx, []string{}); err != nil {
			return err
		}
		if err := queries.DeleteConfigSourcesNotInList(ctx, []uuid.UUID{}); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}

	// Prepare bulk upsert parameters
	names, upsertParams := buildBulkUpsertParams(sourceConfigs, now)

	// Check for API sources that would be overwritten
	if err := checkForAPIRegistryConflicts(ctx, queries, names); err != nil {
		return err
	}

	// Check that config-managed sources don't conflict with API-managed sources
	if err := checkManagedSourceLimit(ctx, queries, sourceConfigs); err != nil {
		return err
	}

	// Bulk upsert all CONFIG sources - returns IDs and names
	upsertedSources, err := queries.BulkUpsertConfigSources(ctx, upsertParams)
	if err != nil {
		return err
	}

	// Verify all sources were upserted (sanity check)
	if len(upsertedSources) != len(names) {
		return fmt.Errorf("expected to upsert %d sources but only %d were returned", len(names), len(upsertedSources))
	}

	// Build map from source name to ID for sync initialization and registry linking
	sourceNameToID := make(map[string]uuid.UUID)
	upsertedIDs := make([]uuid.UUID, len(upsertedSources))
	for i, src := range upsertedSources {
		sourceNameToID[src.Name] = src.ID
		upsertedIDs[i] = src.ID
	}

	// Propagate source claims to existing entries (drift correction for claims-only changes)
	if err := propagateSourceClaimsToEntries(ctx, queries, sourceConfigs, sourceNameToID); err != nil {
		return err
	}

	// Upsert registry rows and link to sources based on config
	if err := upsertRegistryRowsAndLinks(ctx, queries, cfg.Registries, sourceNameToID, &now); err != nil {
		return err
	}

	// Initialize sync statuses for all sources
	if err := initializeSyncStatuses(ctx, queries, sourceConfigs, sourceNameToID); err != nil {
		return err
	}

	// Clean up orphaned CONFIG entries
	if err := cleanupOrphanedConfigEntries(ctx, queries, cfg.Registries, upsertedIDs); err != nil {
		return err
	}

	// Commit the transaction
	return tx.Commit(ctx)
}

// initializeSyncStatuses initializes sync status rows for all sources.
func initializeSyncStatuses(
	ctx context.Context,
	queries *sqlc.Queries,
	sourceConfigs []config.SourceConfig,
	sourceNameToID map[string]uuid.UUID,
) error {
	srcIDs := make([]uuid.UUID, len(sourceConfigs))
	syncStatuses := make([]sqlc.SyncStatus, len(sourceConfigs))
	errorMsgs := make([]string, len(sourceConfigs))

	for i, src := range sourceConfigs {
		srcIDs[i] = sourceNameToID[src.Name]
		isNonSynced := src.IsNonSyncedSource()
		syncStatuses[i], errorMsgs[i] = getInitialSyncStatus(isNonSynced, src.GetType())
	}

	return queries.BulkInitializeSourceSyncs(ctx, sqlc.BulkInitializeSourceSyncsParams{
		SourceIds:    srcIDs,
		SyncStatuses: syncStatuses,
		ErrorMsgs:    errorMsgs,
	})
}

// cleanupOrphanedConfigEntries removes CONFIG registry and source rows that are no longer in the config.
func cleanupOrphanedConfigEntries(
	ctx context.Context,
	queries *sqlc.Queries,
	registries []config.RegistryConfig,
	upsertedSourceIDs []uuid.UUID,
) error {
	// Collect registry names from config for cleanup
	configRegistryNames := make([]string, len(registries))
	for i, reg := range registries {
		configRegistryNames[i] = reg.Name
	}

	// Delete CONFIG registry rows not in the configured list
	if err := queries.DeleteConfigRegistriesNotInList(ctx, configRegistryNames); err != nil {
		return fmt.Errorf("failed to delete orphaned registry rows: %w", err)
	}

	// Delete any CONFIG sources not in the upserted list
	// CASCADE will automatically delete associated sync statuses
	return queries.DeleteConfigSourcesNotInList(ctx, upsertedSourceIDs)
}

// buildBulkUpsertParams prepares the parameter arrays for BulkUpsertConfigSources.
func buildBulkUpsertParams(
	sourceConfigs []config.SourceConfig, now time.Time,
) ([]string, sqlc.BulkUpsertConfigSourcesParams) {
	n := len(sourceConfigs)
	names := make([]string, n)
	sourceTypes := make([]string, n)
	sourceConfigsJSON := make([][]byte, n)
	filterConfigs := make([][]byte, n)
	syncSchedules := make([]pgtypes.Interval, n)
	syncables := make([]bool, n)
	claimsArr := make([][]byte, n)
	createdAts := make([]time.Time, n)
	updatedAts := make([]time.Time, n)

	for i, src := range sourceConfigs {
		names[i] = src.Name
		sourceTypes[i] = string(src.GetType())
		sourceConfigsJSON[i] = serializeSourceConfig(&src)
		filterConfigs[i] = serializeFilterConfig(src.Filter)
		syncSchedules[i] = getSyncScheduleIntervalFromConfig(&src)
		syncables[i] = !src.IsNonSyncedSource()
		claimsArr[i] = db.SerializeClaims(src.Claims)
		createdAts[i] = now
		updatedAts[i] = now
	}

	return names, sqlc.BulkUpsertConfigSourcesParams{
		Names:         names,
		SourceTypes:   sourceTypes,
		SourceConfigs: sourceConfigsJSON,
		FilterConfigs: filterConfigs,
		SyncSchedules: syncSchedules,
		Syncables:     syncables,
		Claims:        claimsArr,
		CreatedAts:    createdAts,
		UpdatedAts:    updatedAts,
	}
}

// checkForAPIRegistryConflicts verifies that none of the sources being upserted are API-created
func checkForAPIRegistryConflicts(ctx context.Context, queries *sqlc.Queries, names []string) error {
	apiSources, err := queries.GetAPISourcesByNames(ctx, names)
	if err != nil {
		return fmt.Errorf("failed to check for API sources: %w", err)
	}
	if len(apiSources) > 0 {
		conflictNames := make([]string, len(apiSources))
		for i, src := range apiSources {
			conflictNames[i] = src.Name
		}
		return fmt.Errorf("cannot overwrite API-created sources: %v", conflictNames)
	}
	return nil
}

// checkManagedSourceLimit verifies that config sources don't introduce a managed
// source when an API-created managed source already exists in the database.
func checkManagedSourceLimit(ctx context.Context, queries *sqlc.Queries, sourceConfigs []config.SourceConfig) error {
	configHasManaged := false
	for _, src := range sourceConfigs {
		if src.GetType() == config.SourceTypeManaged {
			configHasManaged = true
			break
		}
	}
	if !configHasManaged {
		return nil
	}

	managedSources, err := queries.GetManagedSources(ctx)
	if err != nil {
		return fmt.Errorf("failed to check managed source limit: %w", err)
	}
	for _, src := range managedSources {
		if src.CreationType == sqlc.CreationTypeAPI {
			return fmt.Errorf("cannot load config with a managed source: "+
				"an API-created managed source %q already exists", src.Name)
		}
	}
	return nil
}

// upsertRegistryRowsAndLinks creates or updates CONFIG registry rows and links them to sources.
func upsertRegistryRowsAndLinks(
	ctx context.Context,
	queries *sqlc.Queries,
	registries []config.RegistryConfig,
	sourceNameToID map[string]uuid.UUID,
	now *time.Time,
) error {
	for _, reg := range registries {
		claims := db.SerializeClaims(reg.Claims)

		registryRow, err := queries.UpsertRegistry(ctx, sqlc.UpsertRegistryParams{
			Name:         reg.Name,
			Claims:       claims,
			CreationType: sqlc.CreationTypeCONFIG,
			CreatedAt:    now,
			UpdatedAt:    now,
		})
		if err != nil {
			return fmt.Errorf("failed to upsert registry %s: %w", reg.Name, err)
		}

		// Unlink existing sources for this registry before re-linking
		if err := queries.UnlinkAllRegistrySources(ctx, registryRow.ID); err != nil {
			return fmt.Errorf("failed to unlink sources for registry %s: %w", reg.Name, err)
		}

		for position, sourceName := range reg.Sources {
			sourceID, ok := sourceNameToID[sourceName]
			if !ok {
				return fmt.Errorf("registry %s references unknown source %s", reg.Name, sourceName)
			}
			if err := queries.LinkRegistrySource(ctx, sqlc.LinkRegistrySourceParams{
				RegistryID: registryRow.ID,
				SourceID:   sourceID,
				Position:   int32(position),
			}); err != nil {
				return fmt.Errorf("failed to link registry %s to source %s: %w", reg.Name, sourceName, err)
			}
		}
	}
	return nil
}

// propagateSourceClaimsToEntries updates entry claims to match their source's current claims.
// This handles drift correction when source claims change in config but data doesn't change,
// so the sync coordinator won't re-sync and entry claims would otherwise stay stale.
func propagateSourceClaimsToEntries(
	ctx context.Context,
	queries *sqlc.Queries,
	sourceConfigs []config.SourceConfig,
	sourceNameToID map[string]uuid.UUID,
) error {
	for _, src := range sourceConfigs {
		sourceID, ok := sourceNameToID[src.Name]
		if !ok {
			continue
		}
		if err := queries.PropagateSourceClaimsToEntries(ctx, sqlc.PropagateSourceClaimsToEntriesParams{
			Claims:   db.SerializeClaims(src.Claims),
			SourceID: sourceID,
		}); err != nil {
			return fmt.Errorf("failed to propagate claims for source %s: %w", src.Name, err)
		}
	}
	return nil
}

func (d *dbStatusService) ListSyncStatuses(ctx context.Context) (map[string]*status.SyncStatus, error) {
	queries := sqlc.New(d.pool)

	rows, err := queries.ListSourceSyncs(ctx)
	if err != nil {
		return nil, err
	}

	// Build map of source name to sync status
	result := make(map[string]*status.SyncStatus)
	for _, row := range rows {
		result[row.Name] = dbSyncRowToStatus(row)
	}

	return result, nil
}

func (d *dbStatusService) GetSyncStatus(ctx context.Context, registryName string) (*status.SyncStatus, error) {
	queries := sqlc.New(d.pool)

	registrySync, err := queries.GetSourceSyncByName(ctx, registryName)
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
	err := queries.UpsertSourceSyncByName(ctx, sqlc.UpsertSourceSyncByNameParams{
		Name:                  registryName,
		SyncStatus:            syncPhaseToDBStatus(syncStatus.Phase),
		ErrorMsg:              errorMsg,
		StartedAt:             syncStatus.LastAttempt,
		EndedAt:               syncStatus.LastSyncTime,
		AttemptCount:          int64(syncStatus.AttemptCount),
		LastSyncHash:          lastSyncHash,
		LastAppliedFilterHash: lastAppliedFilterHash,
		ServerCount:           int64(syncStatus.ServerCount),
		SkillCount:            int64(syncStatus.SkillCount),
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
		SkillCount:   int(dbSync.SkillCount),
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

// dbSyncRowToStatus converts a ListSourceSyncsRow to a status.SyncStatus
func dbSyncRowToStatus(row sqlc.ListSourceSyncsRow) *status.SyncStatus {
	syncStatus := &status.SyncStatus{
		Phase:        dbSyncStatusToPhase(row.SyncStatus),
		LastAttempt:  row.StartedAt,
		LastSyncTime: row.EndedAt,
		AttemptCount: int(row.AttemptCount),
		ServerCount:  int(row.ServerCount),
		SkillCount:   int(row.SkillCount),
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

// dbSyncRowByLastUpdateToStatus converts a ListSourceSyncsByLastUpdateRow to a status.SyncStatus
func dbSyncRowByLastUpdateToStatus(row sqlc.ListSourceSyncsByLastUpdateRow) *status.SyncStatus {
	syncStatus := &status.SyncStatus{
		Phase:        dbSyncStatusToPhase(row.SyncStatus),
		LastAttempt:  row.StartedAt,
		LastSyncTime: row.EndedAt,
		AttemptCount: int(row.AttemptCount),
		ServerCount:  int(row.ServerCount),
		SkillCount:   int(row.SkillCount),
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

// getInitialSyncStatus returns the initial sync status and error message for a source.
// Non-synced sources (managed and kubernetes) start with COMPLETED status since they don't
// sync from external sources. Synced sources start with FAILED to trigger initial sync.
func getInitialSyncStatus(isNonSynced bool, srcType config.SourceType) (sqlc.SyncStatus, string) {
	if isNonSynced {
		return sqlc.SyncStatusCOMPLETED, fmt.Sprintf("Non-synced source (type: %s)", srcType)
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

// getSyncScheduleIntervalFromConfig extracts the sync schedule interval from a source config.
// Returns a NULL interval for non-synced sources (managed, kubernetes) or if no sync policy is configured.
func getSyncScheduleIntervalFromConfig(src *config.SourceConfig) pgtypes.Interval {
	if src.IsNonSyncedSource() {
		return pgtypes.NewNullInterval()
	}
	if src.SyncPolicy == nil || src.SyncPolicy.Interval == "" {
		return pgtypes.NewNullInterval()
	}
	// Parse the duration string from config (e.g., "30m", "1h")
	interval, err := pgtypes.ParseDuration(src.SyncPolicy.Interval)
	if err != nil {
		// If parsing fails, return NULL interval
		// This shouldn't happen in production as config validation should catch this
		return pgtypes.NewNullInterval()
	}
	return interval
}

func (d *dbStatusService) GetNextSyncJob(
	ctx context.Context,
	predicate func(*config.SourceConfig, *status.SyncStatus) bool,
) (*config.SourceConfig, error) {
	// Start a transaction
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Create queries with transaction
	queries := sqlc.New(d.pool).WithTx(tx)

	// List all sources ordered by last update (ended_at) in ascending order
	// Using FOR UPDATE SKIP LOCKED to prevent race conditions
	sources, err := queries.ListSourceSyncsByLastUpdate(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list sources: %w", err)
	}

	// Iterate through sources and find one that matches the predicate
	for _, src := range sources {
		syncStatus := dbSyncRowByLastUpdateToStatus(src)

		// Find the matching source configuration from cached configs (CONFIG sources)
		// or load from database (API-created sources)
		srcCfg, ok := d.sourceConfigsMap[src.Name]
		if !ok {
			// Source exists in DB but not in config cache
			// This is expected for API-created sources - load from database
			var loadErr error
			srcCfg, loadErr = loadSourceConfigFromDB(ctx, queries, src.Name)
			if loadErr != nil {
				// Failed to load config - skip this source and continue
				continue
			}
		}

		// Skip non-synced sources - they don't sync from external sources
		if srcCfg.IsNonSyncedSource() {
			continue
		}

		// Check if this source matches the predicate
		if predicate(srcCfg, syncStatus) {
			// Update the source to IN_PROGRESS state
			now := time.Now()
			err = queries.UpdateSourceSyncStatusByName(ctx, sqlc.UpdateSourceSyncStatusByNameParams{
				Name:       src.Name,
				SyncStatus: sqlc.SyncStatusINPROGRESS,
				StartedAt:  &now,
			})
			if err != nil {
				return nil, fmt.Errorf("failed to update source status: %w", err)
			}

			// Commit the transaction
			if err := tx.Commit(ctx); err != nil {
				return nil, fmt.Errorf("failed to commit transaction: %w", err)
			}

			// Return the source configuration
			return srcCfg, nil
		}
	}

	// No matching source found - commit transaction and return nil
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil, nil
}

// serializeSourceConfig serializes the source configuration from a source config to JSON bytes.
// Returns nil for empty configs.
func serializeSourceConfig(src *config.SourceConfig) []byte {
	var sourceConfig any
	switch {
	case src.Git != nil:
		sourceConfig = src.Git
	case src.API != nil:
		sourceConfig = src.API
	case src.File != nil:
		sourceConfig = src.File
	case src.Managed != nil:
		sourceConfig = src.Managed
	case src.Kubernetes != nil:
		sourceConfig = src.Kubernetes
	default:
		return nil
	}

	data, err := json.Marshal(sourceConfig)
	if err != nil {
		return nil
	}
	return data
}

// serializeFilterConfig serializes the filter configuration to JSON bytes.
// Returns nil for nil filters.
func serializeFilterConfig(filter *config.FilterConfig) []byte {
	if filter == nil {
		return nil
	}

	data, err := json.Marshal(filter)
	if err != nil {
		return nil
	}
	return data
}

// loadSourceConfigFromDB loads a source configuration from the database.
// This is used for API-created sources that are not in the config file cache.
func loadSourceConfigFromDB(ctx context.Context, queries *sqlc.Queries, name string) (*config.SourceConfig, error) {
	src, err := queries.GetSourceByName(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("failed to get source %s: %w", name, err)
	}

	// Build the source config from database fields
	srcCfg := &config.SourceConfig{
		Name: src.Name,
	}

	// Parse sync schedule from interval
	if src.SyncSchedule.Valid && src.SyncSchedule.Duration > 0 {
		srcCfg.SyncPolicy = &config.SyncPolicyConfig{
			Interval: src.SyncSchedule.Duration.String(),
		}
	}

	// Parse filter config from JSONB
	if src.FilterConfig != nil {
		var filterConfig config.FilterConfig
		if err := json.Unmarshal(src.FilterConfig, &filterConfig); err == nil {
			srcCfg.Filter = &filterConfig
		}
	}

	// Determine source type and parse source config
	sourceType := config.SourceType(src.SourceType)
	if src.SourceConfig != nil {
		if err := parseSourceConfig(srcCfg, sourceType, src.SourceConfig); err != nil {
			return nil, fmt.Errorf("failed to parse source config for source %s: %w", name, err)
		}
	}

	return srcCfg, nil
}

// parseSourceConfig parses the source configuration from JSONB into the appropriate config field
func parseSourceConfig(srcCfg *config.SourceConfig, sourceType config.SourceType, sourceConfig []byte) error {
	switch sourceType {
	case config.SourceTypeGit:
		var gitConfig config.GitConfig
		if err := json.Unmarshal(sourceConfig, &gitConfig); err != nil {
			return err
		}
		srcCfg.Git = &gitConfig
	case config.SourceTypeAPI:
		var apiConfig config.APIConfig
		if err := json.Unmarshal(sourceConfig, &apiConfig); err != nil {
			return err
		}
		srcCfg.API = &apiConfig
	case config.SourceTypeFile:
		var fileConfig config.FileConfig
		if err := json.Unmarshal(sourceConfig, &fileConfig); err != nil {
			return err
		}
		srcCfg.File = &fileConfig
	case config.SourceTypeManaged:
		var managedConfig config.ManagedConfig
		if err := json.Unmarshal(sourceConfig, &managedConfig); err != nil {
			return err
		}
		srcCfg.Managed = &managedConfig
	case config.SourceTypeKubernetes:
		var k8sConfig config.KubernetesConfig
		if err := json.Unmarshal(sourceConfig, &k8sConfig); err != nil {
			return err
		}
		srcCfg.Kubernetes = &k8sConfig
	}
	return nil
}
