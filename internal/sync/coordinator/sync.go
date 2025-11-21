package coordinator

import (
	"context"
	"fmt"
	"time"

	"github.com/stacklok/toolhive/pkg/logger"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/status"
)

// checkRegistrySync performs a sync check and updates status accordingly for a specific registry
func (c *defaultCoordinator) checkRegistrySync(ctx context.Context, regCfg *config.RegistryConfig, checkType string) {
	registryName := regCfg.Name

	// Check if sync is needed (read status under lock)
	// ShouldSync will return false with ReasonAlreadyInProgress if Phase == SyncPhaseSyncing
	var shouldSync bool
	var reason string
	c.withRegistryStatus(registryName, func(syncStatus *status.SyncStatus) {
		shouldSync, reason, _ = c.manager.ShouldSync(ctx, regCfg, syncStatus, false)
	})
	logger.Infof("Registry '%s': %s sync check: shouldSync=%v, reason=%s", registryName, checkType, shouldSync, reason)

	if shouldSync {
		c.performRegistrySync(ctx, regCfg)
	} else {
		c.updateStatusForSkippedSync(ctx, regCfg, reason)
	}
}

// performRegistrySync executes a sync operation and updates status for a specific registry
func (c *defaultCoordinator) performRegistrySync(ctx context.Context, regCfg *config.RegistryConfig) {
	registryName := regCfg.Name

	// Ensure status is persisted at the end, whatever the result
	defer func() {
		c.withRegistryStatus(registryName, func(syncStatus *status.SyncStatus) {
			if err := c.statusPersistence.SaveStatus(ctx, registryName, syncStatus); err != nil {
				logger.Errorf("Registry '%s': Failed to persist final sync status: %v", registryName, err)
			}
		})
	}()

	// Update status: Syncing (under lock)
	var attemptCount int
	c.withRegistryStatus(registryName, func(syncStatus *status.SyncStatus) {
		syncStatus.Phase = status.SyncPhaseSyncing
		syncStatus.Message = "Sync in progress"
		now := time.Now()
		syncStatus.LastAttempt = &now
		syncStatus.AttemptCount++
		attemptCount = syncStatus.AttemptCount

		// Persist the "Syncing" state immediately so it's visible
		if err := c.statusPersistence.SaveStatus(ctx, registryName, syncStatus); err != nil {
			logger.Warnf("Registry '%s': Failed to persist syncing status: %v", registryName, err)
		}
	})

	logger.Infof("Registry '%s': Starting sync operation (attempt %d)", registryName, attemptCount)

	// Perform sync (outside lock - this can take a long time)
	result, err := c.manager.PerformSync(ctx, regCfg)

	// Update status based on result (under lock)
	now := time.Now()
	c.withRegistryStatus(registryName, func(syncStatus *status.SyncStatus) {
		if err != nil {
			syncStatus.Phase = status.SyncPhaseFailed
			syncStatus.Message = err.Message
			logger.Errorf("Registry '%s': Sync failed: %v", registryName, err)
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
			logger.Infof("Registry '%s': Sync completed successfully: %d servers, hash=%s", registryName, result.ServerCount, hashPreview)
		}
	})
}

// updateStatusForSkippedSync updates the status when a sync check determines sync is not needed
func (c *defaultCoordinator) updateStatusForSkippedSync(ctx context.Context, regCfg *config.RegistryConfig, reason string) {
	registryName := regCfg.Name

	// Only update if we have a previous successful sync
	// Don't overwrite Failed/Syncing states with "skipped" messages
	c.withRegistryStatus(registryName, func(syncStatus *status.SyncStatus) {
		syncStatus.Phase = status.SyncPhaseComplete
		syncStatus.Message = fmt.Sprintf("Sync skipped: %s", reason)
		if err := c.statusPersistence.SaveStatus(ctx, registryName, syncStatus); err != nil {
			logger.Warnf("Registry '%s': Failed to persist skipped sync status: %v", registryName, err)
		}
	})
}
