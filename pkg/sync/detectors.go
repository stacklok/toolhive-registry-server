package sync

import (
	"context"
	"time"

	"github.com/stacklok/toolhive-registry-server/pkg/config"
	"github.com/stacklok/toolhive-registry-server/pkg/sources"
	"github.com/stacklok/toolhive-registry-server/pkg/status"
)

// DefaultDataChangeDetector implements DataChangeDetector
type DefaultDataChangeDetector struct {
	sourceHandlerFactory sources.SourceHandlerFactory
}

// IsDataChanged checks if source data has changed by comparing hashes
func (d *DefaultDataChangeDetector) IsDataChanged(ctx context.Context, config *config.Config, syncStatus *status.SyncStatus) (bool, error) {
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
	sourceHandler, err := d.sourceHandlerFactory.CreateHandler(config.Source.Type)
	if err != nil {
		return true, err
	}

	// Get current hash from source
	currentHash, err := sourceHandler.CurrentHash(ctx, config)
	if err != nil {
		return true, err
	}

	// Compare hashes - data changed if different
	return currentHash != lastSyncHash, nil
}

// DefaultAutomaticSyncChecker implements AutomaticSyncChecker
type DefaultAutomaticSyncChecker struct{}

// IsIntervalSyncNeeded checks if sync is needed based on time interval
// Returns: (syncNeeded, nextSyncTime, error)
// nextSyncTime is always a future time when the next sync should occur
func (*DefaultAutomaticSyncChecker) IsIntervalSyncNeeded(config *config.Config, syncStatus *status.SyncStatus) (bool, time.Time, error) {
	if config.SyncPolicy == nil || config.SyncPolicy.Interval == "" {
		return false, time.Time{}, nil
	}

	// Parse the sync interval
	interval, err := time.ParseDuration(config.SyncPolicy.Interval)
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
