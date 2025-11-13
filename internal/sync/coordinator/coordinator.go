package coordinator

import (
	"context"
	"sync"
	"time"

	"github.com/stacklok/toolhive/pkg/logger"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/status"
	pkgsync "github.com/stacklok/toolhive-registry-server/internal/sync"
)

// Coordinator manages background synchronization scheduling and execution
type Coordinator interface {
	// Start begins background sync coordination
	// Blocks until context is cancelled or an unrecoverable error occurs
	Start(ctx context.Context) error

	// Stop gracefully stops the coordinator
	Stop() error

	// GetStatus returns current sync status (thread-safe)
	GetStatus() *status.SyncStatus
}

// DefaultCoordinator is the default implementation of Coordinator
type DefaultCoordinator struct {
	manager           pkgsync.Manager
	statusPersistence status.StatusPersistence
	config            *config.Config

	// Thread-safe status management
	mu           sync.Mutex
	cachedStatus *status.SyncStatus

	// Lifecycle management
	cancelFunc context.CancelFunc
	done       chan struct{}
}

// New creates a new coordinator with injected dependencies
func New(
	manager pkgsync.Manager,
	statusPersistence status.StatusPersistence,
	cfg *config.Config,
) Coordinator {
	return &DefaultCoordinator{
		manager:           manager,
		statusPersistence: statusPersistence,
		config:            cfg,
		done:              make(chan struct{}),
	}
}

// Start begins background sync coordination
func (c *DefaultCoordinator) Start(ctx context.Context) error {
	logger.Info("Starting background sync coordinator")

	// Create cancellable context for this coordinator
	coordCtx, cancel := context.WithCancel(ctx)
	c.cancelFunc = cancel
	defer func() {
		close(c.done)
		logger.Info("Background sync coordinator shutting down")
	}()

	// Load or initialize sync status
	c.loadOrInitializeStatus(coordCtx)

	// Get sync interval from policy
	interval := getSyncInterval(c.config.SyncPolicy)
	logger.Infof("Configured sync interval: %v", interval)

	// Create ticker for periodic sync
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Perform initial sync check
	c.checkSync(coordCtx, "initial")

	// Continue with periodic sync
	for {
		select {
		case <-ticker.C:
			c.checkSync(coordCtx, "periodic")
		case <-coordCtx.Done():
			return nil
		}
	}
}

// Stop gracefully stops the coordinator
func (c *DefaultCoordinator) Stop() error {
	if c.cancelFunc != nil {
		logger.Info("Stopping sync coordinator...")
		c.cancelFunc()
		// Wait for coordinator to finish
		<-c.done
	}
	return nil
}

// GetStatus returns current sync status (thread-safe)
func (c *DefaultCoordinator) GetStatus() *status.SyncStatus {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Return a copy to prevent external modification
	if c.cachedStatus == nil {
		return nil
	}
	statusCopy := *c.cachedStatus
	return &statusCopy
}

// loadOrInitializeStatus loads existing status or creates default
func (c *DefaultCoordinator) loadOrInitializeStatus(ctx context.Context) {
	syncStatus, err := c.statusPersistence.LoadStatus(ctx)
	if err != nil {
		logger.Warnf("Failed to load sync status, initializing with defaults: %v", err)
		syncStatus = &status.SyncStatus{
			Phase:   status.SyncPhaseFailed,
			Message: "No previous sync status found",
		}
	}

	// Check if this is a brand new status (no file existed)
	if syncStatus.Phase == "" && syncStatus.LastSyncTime == nil {
		logger.Info("No previous sync status found, initializing with defaults")
		syncStatus.Phase = status.SyncPhaseFailed
		syncStatus.Message = "No previous sync status found"
		// Persist the default status immediately
		if err := c.statusPersistence.SaveStatus(ctx, syncStatus); err != nil {
			logger.Warnf("Failed to persist default sync status: %v", err)
		}
	} else if syncStatus.Phase == status.SyncPhaseSyncing {
		// If status was left in Syncing state, it means the previous run was interrupted
		// Reset it to Failed so the sync will be triggered
		logger.Warn("Previous sync was interrupted (status=Syncing), resetting to Failed")
		syncStatus.Phase = status.SyncPhaseFailed
		syncStatus.Message = "Previous sync was interrupted"
		// Persist the corrected status
		if err := c.statusPersistence.SaveStatus(ctx, syncStatus); err != nil {
			logger.Warnf("Failed to persist corrected sync status: %v", err)
		}
	}

	// Log the loaded/initialized status
	if syncStatus.LastSyncTime != nil {
		logger.Infof("Loaded sync status: phase=%s, last sync at %s, %d servers",
			syncStatus.Phase, syncStatus.LastSyncTime.Format(time.RFC3339), syncStatus.ServerCount)
	} else {
		logger.Infof("Sync status: phase=%s, no previous sync", syncStatus.Phase)
	}

	// Store in cached status
	c.mu.Lock()
	c.cachedStatus = syncStatus
	c.mu.Unlock()
}

// withStatus executes a function while holding the status lock
func (c *DefaultCoordinator) withStatus(fn func(*status.SyncStatus)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	fn(c.cachedStatus)
}
