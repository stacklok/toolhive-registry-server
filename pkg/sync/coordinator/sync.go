package coordinator

import (
	"context"
	"fmt"
	"time"

	"github.com/stacklok/toolhive-registry-server/pkg/status"
	"github.com/stacklok/toolhive/pkg/logger"
)

// checkSync performs a sync check and updates status accordingly
func (c *DefaultCoordinator) checkSync(ctx context.Context, checkType string) {
	// Check if sync is needed (read status under lock)
	// ShouldSync will return false with ReasonAlreadyInProgress if Phase == SyncPhaseSyncing
	var shouldSync bool
	var reason string
	c.withStatus(func(syncStatus *status.SyncStatus) {
		shouldSync, reason, _ = c.manager.ShouldSync(ctx, c.config, syncStatus, false)
	})
	logger.Infof("%s sync check: shouldSync=%v, reason=%s", checkType, shouldSync, reason)

	if shouldSync {
		c.performSync(ctx)
	} else {
		c.updateStatusForSkippedSync(ctx, reason)
	}
}

// performSync executes a sync operation and updates status
func (c *DefaultCoordinator) performSync(ctx context.Context) {
	// Ensure status is persisted at the end, whatever the result
	defer func() {
		c.withStatus(func(syncStatus *status.SyncStatus) {
			if err := c.statusPersistence.SaveStatus(ctx, syncStatus); err != nil {
				logger.Errorf("Failed to persist final sync status: %v", err)
			}
		})
	}()

	// Update status: Syncing (under lock)
	var attemptCount int
	c.withStatus(func(syncStatus *status.SyncStatus) {
		syncStatus.Phase = status.SyncPhaseSyncing
		syncStatus.Message = "Sync in progress"
		now := time.Now()
		syncStatus.LastAttempt = &now
		syncStatus.AttemptCount++
		attemptCount = syncStatus.AttemptCount

		// Persist the "Syncing" state immediately so it's visible
		if err := c.statusPersistence.SaveStatus(ctx, syncStatus); err != nil {
			logger.Warnf("Failed to persist syncing status: %v", err)
		}
	})

	logger.Infof("Starting sync operation (attempt %d)", attemptCount)

	// Perform sync (outside lock - this can take a long time)
	result, err := c.manager.PerformSync(ctx, c.config)

	// Update status based on result (under lock)
	now := time.Now()
	c.withStatus(func(syncStatus *status.SyncStatus) {
		if err != nil {
			syncStatus.Phase = status.SyncPhaseFailed
			syncStatus.Message = err.Message
			logger.Errorf("Sync failed: %v", err)
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
			logger.Infof("Sync completed successfully: %d servers, hash=%s", result.ServerCount, hashPreview)
		}
	})
}

// updateStatusForSkippedSync updates the status when a sync check determines sync is not needed
func (c *DefaultCoordinator) updateStatusForSkippedSync(ctx context.Context, reason string) {
	// Only update if we have a previous successful sync
	// Don't overwrite Failed/Syncing states with "skipped" messages
	c.withStatus(func(syncStatus *status.SyncStatus) {
		syncStatus.Phase = status.SyncPhaseComplete
		syncStatus.Message = fmt.Sprintf("Sync skipped: %s", reason)
		if err := c.statusPersistence.SaveStatus(ctx, syncStatus); err != nil {
			logger.Warnf("Failed to persist skipped sync status: %v", err)
		}
	})
}
