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

// Coordinator manages background synchronization scheduling and execution for multiple registries
type Coordinator interface {
	// Start begins background sync coordination for all registries
	// Blocks until context is cancelled or an unrecoverable error occurs
	Start(ctx context.Context) error

	// Stop gracefully stops the coordinator and all registry sync loops
	Stop() error

	// GetStatus returns current sync status for a specific registry (thread-safe)
	GetStatus(registryName string) *status.SyncStatus

	// GetAllStatus returns sync status for all registries (thread-safe)
	GetAllStatus() map[string]*status.SyncStatus
}

// registrySync manages sync state for a single registry
type registrySync struct {
	config     *config.RegistryConfig
	cancelFunc context.CancelFunc
	done       chan struct{}
}

// defaultCoordinator is the default implementation of Coordinator
type defaultCoordinator struct {
	manager           pkgsync.Manager
	statusPersistence status.StatusPersistence
	config            *config.Config

	// Thread-safe status management (per-registry)
	mu             sync.RWMutex
	cachedStatuses map[string]*status.SyncStatus

	// Lifecycle management
	registrySyncs map[string]*registrySync
	cancelFunc    context.CancelFunc
	done          chan struct{}
	wg            sync.WaitGroup
}

// New creates a new coordinator with injected dependencies
func New(
	manager pkgsync.Manager,
	statusPersistence status.StatusPersistence,
	cfg *config.Config,
) Coordinator {
	return &defaultCoordinator{
		manager:           manager,
		statusPersistence: statusPersistence,
		config:            cfg,
		cachedStatuses:    make(map[string]*status.SyncStatus),
		registrySyncs:     make(map[string]*registrySync),
		done:              make(chan struct{}),
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
	c.loadOrInitializeAllStatus(coordCtx)

	// Start sync loop for each registry (skip managed registries)
	for i := range c.config.Registries {
		regCfg := &c.config.Registries[i]

		// Skip managed registries - they don't sync from external sources
		if regCfg.GetType() == config.SourceTypeManaged {
			logger.Infof("Registry '%s': Skipping sync loop (managed registry)", regCfg.Name)
			continue
		}

		c.startRegistrySync(coordCtx, regCfg)
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

// GetStatus returns current sync status for a specific registry (thread-safe)
func (c *defaultCoordinator) GetStatus(registryName string) *status.SyncStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Return a copy to prevent external modification
	syncStatus, exists := c.cachedStatuses[registryName]
	if !exists || syncStatus == nil {
		return nil
	}
	statusCopy := *syncStatus
	return &statusCopy
}

// GetAllStatus returns sync status for all registries (thread-safe)
func (c *defaultCoordinator) GetAllStatus() map[string]*status.SyncStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Return a deep copy to prevent external modification
	result := make(map[string]*status.SyncStatus)
	for name, syncStatus := range c.cachedStatuses {
		if syncStatus != nil {
			statusCopy := *syncStatus
			result[name] = &statusCopy
		}
	}
	return result
}

// loadOrInitializeAllStatus loads existing status or creates defaults for all registries
func (c *defaultCoordinator) loadOrInitializeAllStatus(ctx context.Context) {
	for i := range c.config.Registries {
		regCfg := &c.config.Registries[i]
		c.loadOrInitializeRegistryStatus(ctx, regCfg)
	}
}

// loadOrInitializeRegistryStatus loads existing status or creates default for a specific registry
func (c *defaultCoordinator) loadOrInitializeRegistryStatus(ctx context.Context, regCfg *config.RegistryConfig) {
	registryName := regCfg.Name
	isManaged := regCfg.GetType() == config.SourceTypeManaged

	syncStatus, err := c.statusPersistence.LoadStatus(ctx, registryName)
	if err != nil {
		logger.Warnf("Registry '%s': Failed to load sync status, initializing with defaults: %v", registryName, err)

		// Managed registries get a different default status
		if isManaged {
			syncStatus = &status.SyncStatus{
				Phase:   status.SyncPhaseComplete,
				Message: "Managed registry (data managed via API)",
			}
		} else {
			syncStatus = &status.SyncStatus{
				Phase:   status.SyncPhaseFailed,
				Message: "No previous sync status found",
			}
		}
	}

	// Check if this is a brand new status (no file existed)
	if syncStatus.Phase == "" && syncStatus.LastSyncTime == nil {
		logger.Infof("Registry '%s': No previous sync status found, initializing with defaults", registryName)

		// Managed registries get a different default status
		if isManaged {
			syncStatus.Phase = status.SyncPhaseComplete
			syncStatus.Message = "Managed registry (data managed via API)"
		} else {
			syncStatus.Phase = status.SyncPhaseFailed
			syncStatus.Message = "No previous sync status found"
		}

		// Persist the default status immediately
		if err := c.statusPersistence.SaveStatus(ctx, registryName, syncStatus); err != nil {
			logger.Warnf("Registry '%s': Failed to persist default sync status: %v", registryName, err)
		}
	} else if !isManaged && syncStatus.Phase == status.SyncPhaseSyncing {
		// If status was left in Syncing state (only for non-managed registries),
		// it means the previous run was interrupted. Reset it to Failed so the sync will be triggered
		logger.Warnf("Registry '%s': Previous sync was interrupted (status=Syncing), resetting to Failed", registryName)
		syncStatus.Phase = status.SyncPhaseFailed
		syncStatus.Message = "Previous sync was interrupted"
		// Persist the corrected status
		if err := c.statusPersistence.SaveStatus(ctx, registryName, syncStatus); err != nil {
			logger.Warnf("Registry '%s': Failed to persist corrected sync status: %v", registryName, err)
		}
	}

	// Log the loaded/initialized status
	if isManaged {
		logger.Infof("Registry '%s': Managed registry - data managed via API", registryName)
	} else if syncStatus.LastSyncTime != nil {
		logger.Infof("Registry '%s': Loaded sync status: phase=%s, last sync at %s, %d servers",
			registryName, syncStatus.Phase, syncStatus.LastSyncTime.Format(time.RFC3339), syncStatus.ServerCount)
	} else {
		logger.Infof("Registry '%s': Sync status: phase=%s, no previous sync", registryName, syncStatus.Phase)
	}

	// Store in cached status
	c.mu.Lock()
	c.cachedStatuses[registryName] = syncStatus
	c.mu.Unlock()
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

// withRegistryStatus executes a function while holding the status lock for a specific registry
func (c *defaultCoordinator) withRegistryStatus(registryName string, fn func(*status.SyncStatus)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if syncStatus, exists := c.cachedStatuses[registryName]; exists {
		fn(syncStatus)
	}
}
