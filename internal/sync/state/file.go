package state

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/status"
)

type fileStateService struct {
	statusPersistence status.StatusPersistence

	// Thread-safe status management (per-registry)
	mu             sync.RWMutex
	cachedStatuses map[string]*status.SyncStatus
}

// NewFileStateService creates a new file-based registry state service
func NewFileStateService(statusPersistence status.StatusPersistence) RegistryStateService {
	return &fileStateService{
		statusPersistence: statusPersistence,
		cachedStatuses:    make(map[string]*status.SyncStatus),
	}
}

func (f *fileStateService) Initialize(ctx context.Context, registryConfigs []config.RegistryConfig) error {
	for _, conf := range registryConfigs {
		f.loadOrInitializeRegistryStatus(ctx, conf.Name, conf.IsNonSyncedRegistry(), conf.GetType())
	}
	return nil
}

func (f *fileStateService) ListSyncStatuses(_ context.Context) (map[string]*status.SyncStatus, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	// Return a deep copy to prevent external modification
	result := make(map[string]*status.SyncStatus)
	for name, syncStatus := range f.cachedStatuses {
		if syncStatus != nil {
			statusCopy := *syncStatus
			result[name] = &statusCopy
		}
	}
	return result, nil
}

func (f *fileStateService) GetSyncStatus(_ context.Context, registryName string) (*status.SyncStatus, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	// Return a copy to prevent external modification
	syncStatus, exists := f.cachedStatuses[registryName]
	// TODO: We should return an error if the registry does not exist.
	if !exists || syncStatus == nil {
		return nil, nil
	}
	statusCopy := *syncStatus
	return &statusCopy, nil
}

func (f *fileStateService) UpdateSyncStatus(ctx context.Context, registryName string, syncStatus *status.SyncStatus) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.statusPersistence.SaveStatus(ctx, registryName, syncStatus); err != nil {
		return err
	}
	// I'm not sure if keeping a cache of these statuses in memory is useful assuming
	// production deployments will use Postgres.
	f.cachedStatuses[registryName] = syncStatus
	return nil
}

func (f *fileStateService) loadOrInitializeRegistryStatus(
	ctx context.Context,
	registryName string,
	isNonSynced bool,
	regType string,
) {
	syncStatus, err := f.statusPersistence.LoadStatus(ctx, registryName)
	if err != nil {
		slog.Warn("Failed to load sync status, initializing with defaults",
			"registry", registryName,
			"error", err)

		// Non-synced registries (managed and kubernetes) get a different default status
		if isNonSynced {
			syncStatus = &status.SyncStatus{
				Phase:   status.SyncPhaseComplete,
				Message: fmt.Sprintf("Non-synced registry (type: %s)", regType),
			}
		} else {
			syncStatus = &status.SyncStatus{
				Phase:   status.SyncPhaseFailed,
				Message: "No previous sync status found",
			}
		}
	}

	/*
	 * Note that the cleanup logic is not shared with the database.
	 * It assumes that only one process at a time will access the backing
	 * store. This assumption breaks down if multiple servers share a database.
	 */

	// Check if this is a new status (no file existed)
	if syncStatus.Phase == "" && syncStatus.LastSyncTime == nil {
		slog.Info("No previous sync status found, initializing with defaults", "registry", registryName)

		// Non-synced registries (managed and kubernetes) get a different default status
		if isNonSynced {
			syncStatus.Phase = status.SyncPhaseComplete
			syncStatus.Message = fmt.Sprintf("Non-synced registry (type: %s)", regType)
		} else {
			syncStatus.Phase = status.SyncPhaseFailed
			syncStatus.Message = "No previous sync status found"
		}

		// Persist the default status immediately
		if err := f.statusPersistence.SaveStatus(ctx, registryName, syncStatus); err != nil {
			slog.Warn("Failed to persist default sync status",
				"registry", registryName,
				"error", err)
		}
	} else if syncStatus.Phase == status.SyncPhaseSyncing && !isNonSynced {
		// If status was left in Syncing state (only for synced registries),
		// it means the previous run was interrupted. Reset it to Failed so the sync will be triggered
		slog.Warn("Previous sync was interrupted (status=Syncing), resetting to Failed", "registry", registryName)
		syncStatus.Phase = status.SyncPhaseFailed
		syncStatus.Message = "Previous sync was interrupted"
		// Persist the corrected status
		if err := f.statusPersistence.SaveStatus(ctx, registryName, syncStatus); err != nil {
			slog.Warn("Failed to persist corrected sync status",
				"registry", registryName,
				"error", err)
		}
	}

	// Log the loaded/initialized status
	if isNonSynced {
		slog.Info("Non-synced registry",
			"registry", registryName,
			"type", regType)
	} else if syncStatus.LastSyncTime != nil {
		slog.Info("Loaded sync status",
			"registry", registryName,
			"phase", syncStatus.Phase,
			"last_sync", syncStatus.LastSyncTime.Format(time.RFC3339),
			"server_count", syncStatus.ServerCount)
	} else {
		slog.Info("Sync status initialized",
			"registry", registryName,
			"phase", syncStatus.Phase,
			"message", "no previous sync")
	}

	// Store in cached status
	f.mu.Lock()
	f.cachedStatuses[registryName] = syncStatus
	f.mu.Unlock()
}

func (f *fileStateService) GetNextSyncJob(
	ctx context.Context,
	cfg *config.Config,
	predicate func(*config.RegistryConfig, *status.SyncStatus) bool,
) (*config.RegistryConfig, error) {
	// Grab the lock to ensure atomic operation
	f.mu.Lock()
	defer f.mu.Unlock()

	// Build a map of registry names to configs for quick lookup
	configMap := make(map[string]*config.RegistryConfig)
	for i := range cfg.Registries {
		configMap[cfg.Registries[i].Name] = &cfg.Registries[i]
	}

	// Create a sortable list of registries with their sync status
	// Sort by LastSyncTime (ended_at equivalent) in ascending order, nil first
	type registryWithStatus struct {
		name       string
		syncStatus *status.SyncStatus
		lastUpdate *time.Time
	}

	var registries []registryWithStatus
	for name, syncStatus := range f.cachedStatuses {
		// Only consider registries that are in the config
		if _, exists := configMap[name]; exists {
			registries = append(registries, registryWithStatus{
				name:       name,
				syncStatus: syncStatus,
				lastUpdate: syncStatus.LastSyncTime,
			})
		}
	}

	// Sort by last update time (ascending, nil first)
	// Using a simple bubble sort since the list is typically small
	for i := 0; i < len(registries); i++ {
		for j := i + 1; j < len(registries); j++ {
			// nil times come first
			if registries[i].lastUpdate != nil &&
				(registries[j].lastUpdate == nil ||
					registries[j].lastUpdate.Before(*registries[i].lastUpdate)) {
				registries[i], registries[j] = registries[j], registries[i]
			}
		}
	}

	// Iterate through sorted registries and find one that matches the predicate
	for _, reg := range registries {
		// Skip non-synced registries - they don't sync from external sources
		if configMap[reg.name].IsNonSyncedRegistry() {
			continue
		}

		// Check if this registry matches the predicate
		if predicate(configMap[reg.name], reg.syncStatus) {
			// Update the registry to IN_PROGRESS state
			reg.syncStatus.Phase = status.SyncPhaseSyncing
			now := time.Now()
			reg.syncStatus.LastAttempt = &now

			// Persist the updated status
			if err := f.statusPersistence.SaveStatus(ctx, reg.name, reg.syncStatus); err != nil {
				return nil, fmt.Errorf("failed to update registry status: %w", err)
			}

			// Update the cached status
			f.cachedStatuses[reg.name] = reg.syncStatus

			// Return the matching registry configuration
			return configMap[reg.name], nil
		}
	}

	// No matching registry found
	return nil, nil
}
