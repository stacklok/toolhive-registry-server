package coordinator

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/status"
	pkgsync "github.com/stacklok/toolhive-registry-server/internal/sync"
	"github.com/stacklok/toolhive-registry-server/internal/sync/state"
)

// Coordinator manages background synchronization scheduling and execution for multiple registries
type Coordinator interface {
	// Start begins background sync coordination for all registries
	// Blocks until context is cancelled or an unrecoverable error occurs
	Start(ctx context.Context) error

	// Stop gracefully stops the coordinator and all registry sync loops
	Stop() error
}

// defaultCoordinator is the default implementation of Coordinator
type defaultCoordinator struct {
	manager pkgsync.Manager
	config  *config.Config

	// Lifecycle management
	cancelFunc context.CancelFunc
	done       chan struct{}

	statusSvc state.RegistryStateService
}

// New creates a new coordinator with injected dependencies
func New(
	manager pkgsync.Manager,
	statusSvc state.RegistryStateService,
	cfg *config.Config,
) Coordinator {
	return &defaultCoordinator{
		manager:   manager,
		statusSvc: statusSvc,
		config:    cfg,
		done:      make(chan struct{}),
	}
}

// Start begins background sync coordination for all registries
func (c *defaultCoordinator) Start(ctx context.Context) error {
	slog.Info("Starting background sync coordinator", "registry_count", len(c.config.Registries))

	// Create cancellable context for this coordinator
	coordCtx, cancel := context.WithCancel(ctx)
	c.cancelFunc = cancel
	defer func() {
		close(c.done)
		slog.Info("Background sync coordinator shutting down")
	}()

	// Load or initialize sync status for all registries
	if err := c.statusSvc.Initialize(ctx, c.config.Registries); err != nil {
		return fmt.Errorf("failed to initialize registry sync status: %w", err)
	}

	// Get the sync interval from the first synced registry's policy (or use default)
	// Since we now have a single coordinator loop, we use a global interval
	interval := c.getCoordinatorInterval()
	slog.Info("Configured coordinator sync interval", "interval", interval)

	// Create ticker for periodic sync checks
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Perform initial sync check
	c.processNextSyncJob(coordCtx)

	// Run the coordinator loop
	for {
		select {
		case <-ticker.C:
			c.processNextSyncJob(coordCtx)
		case <-coordCtx.Done():
			slog.Info("Sync coordinator stopping")
			return nil
		}
	}
}

// Stop gracefully stops the coordinator
func (c *defaultCoordinator) Stop() error {
	if c.cancelFunc != nil {
		slog.Info("Stopping sync coordinator")
		c.cancelFunc()
		// Wait for coordinator to finish
		<-c.done
	}
	return nil
}

// getCoordinatorInterval determines the sync interval for the coordinator
// It uses the minimum interval from all synced registries, or a default
func (c *defaultCoordinator) getCoordinatorInterval() time.Duration {
	minInterval := time.Minute // default
	found := false

	for _, regCfg := range c.config.Registries {
		// Skip non-synced registries
		if regCfg.IsNonSyncedRegistry() {
			continue
		}

		interval := getSyncInterval(regCfg.SyncPolicy)
		if !found || interval < minInterval {
			minInterval = interval
			found = true
		}
	}

	return minInterval
}

// processNextSyncJob gets the next job and processes it if available
func (c *defaultCoordinator) processNextSyncJob(ctx context.Context) {
	// Get the next sync job using the predicate to check if sync is needed
	regCfg, err := c.manager.GetNextSyncJob(ctx, func(syncStatus *status.SyncStatus) bool {
		// Only process registries that are not currently syncing
		if syncStatus.Phase == status.SyncPhaseSyncing {
			return false
		}

		// Use the manager's ShouldSync logic to determine if this registry needs syncing
		// We need the registry config to call ShouldSync, so we'll check this below
		return true
	})

	if err != nil {
		slog.Error("Error getting next sync job", "error", err)
		return
	}

	// No job available
	if regCfg == nil {
		return
	}

	// Skip non-synced registries - they don't sync from external sources
	if regCfg.IsNonSyncedRegistry() {
		slog.Debug("Skipping sync for non-synced registry",
			"registry", regCfg.Name,
			"type", regCfg.GetType())
		return
	}

	// Get the current sync status to pass to ShouldSync
	syncStatus, err := c.statusSvc.GetSyncStatus(ctx, regCfg.Name)
	if err != nil {
		slog.Error("Error getting sync status", "registry", regCfg.Name, "error", err)
		return
	}

	// Double-check with ShouldSync before proceeding
	reason := c.manager.ShouldSync(ctx, regCfg, syncStatus, false)
	if !reason.ShouldSync() {
		slog.Debug("Registry does not need sync",
			"registry", regCfg.Name,
			"reason", reason.String())
		// Update status back to not syncing since we're not going to sync
		syncStatus.Phase = status.SyncPhaseComplete
		if updateErr := c.statusSvc.UpdateSyncStatus(ctx, regCfg.Name, syncStatus); updateErr != nil {
			slog.Error("Error updating sync status", "registry", regCfg.Name, "error", updateErr)
		}
		return
	}

	// Perform the sync
	c.performRegistrySync(ctx, regCfg)
}

// performRegistrySync executes the sync operation for a registry
func (c *defaultCoordinator) performRegistrySync(ctx context.Context, regCfg *config.RegistryConfig) {
	registryName := regCfg.Name

	// Set up the final status update in a defer block to ensure that we always
	// clean up the status of the sync at the end of this function.
	// Set a default error here in case the function is killed by an unexpected error.
	syncStatus := &status.SyncStatus{
		Phase:   status.SyncPhaseFailed,
		Message: fmt.Sprintf("Unexpected failure while syncing registry %s", registryName),
	}
	defer func() {
		if err := c.statusSvc.UpdateSyncStatus(ctx, registryName, syncStatus); err != nil {
			slog.Error("Error updating sync status",
				"registry", registryName,
				"error", err)
		}
	}()

	slog.Info("Starting sync operation", "registry", registryName)

	// Perform sync
	result, syncErr := c.manager.PerformSync(ctx, regCfg)

	// Update status based on result
	now := time.Now()
	if syncErr != nil {
		syncStatus.Phase = status.SyncPhaseFailed
		syncStatus.Message = syncErr.Message
		slog.Error("Sync failed",
			"registry", registryName,
			"error", syncErr.Message)
	} else {
		syncStatus.Phase = status.SyncPhaseComplete
		syncStatus.Message = "Sync completed successfully"
		syncStatus.LastSyncTime = &now
		syncStatus.LastSyncHash = result.Hash
		syncStatus.ServerCount = result.ServerCount
		syncStatus.AttemptCount = 0
		hashPreview := result.Hash
		if len(hashPreview) > 8 {
			hashPreview = hashPreview[:8]
		}
		slog.Info("Sync completed successfully",
			"registry", registryName,
			"server_count", result.ServerCount,
			"hash", hashPreview)
	}
}
