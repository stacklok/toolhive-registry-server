package coordinator

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/stacklok/toolhive/pkg/logger"

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

// registrySync manages sync state for a single registry
type registrySync struct {
	config     *config.RegistryConfig
	cancelFunc context.CancelFunc
	done       chan struct{}
}

// defaultCoordinator is the default implementation of Coordinator
type defaultCoordinator struct {
	manager pkgsync.Manager
	config  *config.Config

	// Thread-safe status management (per-registry)
	mu sync.RWMutex

	// Lifecycle management
	registrySyncs map[string]*registrySync
	cancelFunc    context.CancelFunc
	done          chan struct{}
	wg            sync.WaitGroup

	statusSvc state.RegistryStateService
}

// New creates a new coordinator with injected dependencies
func New(
	manager pkgsync.Manager,
	statusSvc state.RegistryStateService,
	cfg *config.Config,
) Coordinator {
	return &defaultCoordinator{
		manager:       manager,
		statusSvc:     statusSvc,
		config:        cfg,
		registrySyncs: make(map[string]*registrySync),
		done:          make(chan struct{}),
	}
}

// Start begins background sync coordination for all registries
func (c *defaultCoordinator) Start(ctx context.Context) error {
	logger.Infof("Starting background sync coordinator for %d registries", len(c.config.Registries))

	// Create cancellable context for this coordinator
	coordCtx, cancel := context.WithCancel(ctx)
	c.cancelFunc = cancel
	defer func() {
		close(c.done)
		logger.Info("Background sync coordinator shutting down")
	}()

	// Load or initialize sync status for all registries
	if err := c.statusSvc.Initialize(ctx, c.config.Registries); err != nil {
		return fmt.Errorf("failed to initialize registry sync status: %w", err)
	}

	// Start sync loop for each registry (skip managed registries)
	for _, regCfg := range c.config.Registries {

		// Skip managed registries - they don't sync from external sources
		if regCfg.GetType() == config.SourceTypeManaged {
			logger.Infof("Registry '%s': Skipping sync loop (managed registry)", regCfg.Name)
			continue
		}

		c.startRegistrySync(coordCtx, &regCfg)
	}

	// Wait for context cancellation
	<-coordCtx.Done()

	// Wait for all registry sync goroutines to finish
	c.wg.Wait()

	return nil
}

// Stop gracefully stops the coordinator and all registry sync loops
func (c *defaultCoordinator) Stop() error {
	if c.cancelFunc != nil {
		logger.Info("Stopping sync coordinator for all registries...")
		c.cancelFunc()
		// Wait for coordinator to finish (which waits for all registry syncs)
		<-c.done
	}
	return nil
}

// startRegistrySync starts a sync loop for a specific registry
func (c *defaultCoordinator) startRegistrySync(parentCtx context.Context, regCfg *config.RegistryConfig) {
	registryName := regCfg.Name

	// Create cancellable context for this registry
	regCtx, cancel := context.WithCancel(parentCtx)

	// Store registry sync info
	c.mu.Lock()
	c.registrySyncs[registryName] = &registrySync{
		config:     regCfg,
		cancelFunc: cancel,
		done:       make(chan struct{}),
	}
	c.mu.Unlock()

	// Increment wait group
	c.wg.Add(1)

	// Start sync goroutine for this registry
	go func() {
		defer c.wg.Done()
		defer close(c.registrySyncs[registryName].done)

		c.runRegistrySync(regCtx, regCfg)
	}()
}

// runRegistrySync runs the sync loop for a specific registry
func (c *defaultCoordinator) runRegistrySync(ctx context.Context, regCfg *config.RegistryConfig) {
	registryName := regCfg.Name
	logger.Infof("Registry '%s': Starting sync loop", registryName)

	// Get sync interval from registry policy
	interval := getSyncInterval(regCfg.SyncPolicy)
	logger.Infof("Registry '%s': Configured sync interval: %v", registryName, interval)

	// Create ticker for periodic sync
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Perform initial sync check
	c.checkRegistrySync(ctx, regCfg, "initial")

	// Continue with periodic sync
	for {
		select {
		case <-ticker.C:
			c.checkRegistrySync(ctx, regCfg, "periodic")
		case <-ctx.Done():
			logger.Infof("Registry '%s': Sync loop stopping", registryName)
			return
		}
	}
}

// checkRegistrySync performs a sync check and updates status accordingly for a specific registry
func (c *defaultCoordinator) checkRegistrySync(ctx context.Context, regCfg *config.RegistryConfig, _ string) {
	registryName := regCfg.Name
	var attemptCount int

	// Check if we should sync, and set the status as an atomic operation.
	// If we are not ready to sync, then do nothing.
	// The lock is logically cleared by updating the status on completion (or error).
	statusUpdated, err := c.statusSvc.UpdateStatusAtomically(
		ctx,
		registryName,
		func(syncStatus *status.SyncStatus) bool {
			// TODO: Maybe `ShouldSync` should live here, not manager?
			reason := c.manager.ShouldSync(ctx, regCfg, syncStatus, false)
			if reason.ShouldSync() {
				syncStatus.Phase = status.SyncPhaseSyncing
				syncStatus.Message = "Sync in progress"
				now := time.Now()
				syncStatus.LastAttempt = &now
				syncStatus.AttemptCount++
				attemptCount = syncStatus.AttemptCount
			}
			return reason.ShouldSync()
		},
	)
	if err != nil {
		logger.Warnf("error while checking sync status of registry %s: %v", regCfg.Name, err)
	}

	// Registry is either not ready for a sync, or sync is in progress already.
	if !statusUpdated {
		return
	}

	// Set up the final status update in a defer block to ensure that we always
	// clean up the status of the sync at the end of this function.
	// Set a default error here in case the function is killed by an unexpected error.
	syncStatus := &status.SyncStatus{
		Phase:   status.SyncPhaseFailed,
		Message: fmt.Sprintf("Unexpected failure while syncing registry %s", registryName),
	}
	defer func() {
		if err := c.statusSvc.UpdateSyncStatus(ctx, registryName, syncStatus); err != nil {
			logger.Errorf("error while updating status of registry %s: %v", registryName, err)
		}
	}()

	logger.Infof("Registry '%s': Starting sync operation (attempt %d)", registryName, attemptCount)
	// Perform sync (outside lock - this can take a long time)
	result, syncErr := c.manager.PerformSync(ctx, regCfg)

	// Update status based on result
	now := time.Now()
	if syncErr != nil {
		syncStatus.Phase = status.SyncPhaseFailed
		syncStatus.Message = syncErr.Message
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
}
