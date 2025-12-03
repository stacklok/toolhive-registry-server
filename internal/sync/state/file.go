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

func (f *fileStateService) UpdateStatusAtomically(
	ctx context.Context,
	registryName string,
	testAndUpdateFn func(syncStatus *status.SyncStatus) bool,
) (bool, error) {
	// This method duplicates code from GetSyncStatus and UpdateSyncStatus
	// I have duplicated the code due to the triviality of the logic.
	f.mu.Lock()
	defer f.mu.Unlock()

	// Get the sync status from cache
	syncStatus, exists := f.cachedStatuses[registryName]
	if !exists || syncStatus == nil {
		return false, fmt.Errorf("sync status for registry %s not found", registryName)
	}

	shouldUpdate := testAndUpdateFn(syncStatus)
	if shouldUpdate {
		if err := f.statusPersistence.SaveStatus(ctx, registryName, syncStatus); err != nil {
			return false, err
		}
		f.cachedStatuses[registryName] = syncStatus
	}
	return shouldUpdate, nil
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
