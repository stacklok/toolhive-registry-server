package sync

import (
	"context"
	"time"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/sources"
	"github.com/stacklok/toolhive-registry-server/internal/status"
)

// defaultDataChangeDetector implements DataChangeDetector
type defaultDataChangeDetector struct {
	sourceHandlerFactory sources.SourceHandlerFactory
}

// IsDataChanged checks if source data has changed by comparing hashes for a specific registry
func (d *defaultDataChangeDetector) IsDataChanged(
	ctx context.Context, regCfg *config.RegistryConfig, syncStatus *status.SyncStatus,
) (bool, error) {
	// Check for hash in syncStatus first, then fallback
	var lastSyncHash string
	if syncStatus != nil {
		lastSyncHash = syncStatus.LastSyncHash
	}

	// If we don't have a last sync hash, consider data changed
	if lastSyncHash == "" {
		return true, nil
	}

	// Get source handler
	sourceHandler, err := d.sourceHandlerFactory.CreateHandler(regCfg)
	if err != nil {
		return true, err
	}

	// Get current hash from source
	currentHash, err := sourceHandler.CurrentHash(ctx, regCfg)
	if err != nil {
		return true, err
	}

	// Compare hashes - data changed if different
	return currentHash != lastSyncHash, nil
}

// defaultAutomaticSyncChecker implements AutomaticSyncChecker
type defaultAutomaticSyncChecker struct{}

// IsIntervalSyncNeeded checks if sync is needed based on time interval for a specific registry
// Returns: (syncNeeded, nextSyncTime, error)
// nextSyncTime is a future time when the next sync should occur, or zero time if no policy configured
func (*defaultAutomaticSyncChecker) IsIntervalSyncNeeded(
	regCfg *config.RegistryConfig, syncStatus *status.SyncStatus,
) (bool, time.Time, error) {
	if regCfg.SyncPolicy == nil || regCfg.SyncPolicy.Interval == "" {
		return false, time.Time{}, nil
	}

	// Parse the sync interval
	interval, err := time.ParseDuration(regCfg.SyncPolicy.Interval)
	if err != nil {
		return false, time.Time{}, err
	}

	now := time.Now()

	// Check for last sync time in syncStatus first, then fallback
	var lastSyncTime *time.Time
	if syncStatus != nil {
		lastSyncTime = syncStatus.LastAttempt
	}

	// If we don't have a last sync time, sync is needed
	if lastSyncTime == nil {
		return true, now.Add(interval), nil
	}

	// Calculate when next sync should happen based on last sync
	nextSyncTime := lastSyncTime.Add(interval)

	// Check if it's time for the next sync
	syncNeeded := now.After(nextSyncTime) || now.Equal(nextSyncTime)

	if syncNeeded {
		// If sync is needed now, calculate when the next one after this should be
		return true, now.Add(interval), nil
	}

	// Sync not needed yet, return the originally calculated next sync time
	return false, nextSyncTime, nil
}
