// Package coordinator provides background synchronization coordination for registry resources.
//
// This package implements the orchestration layer that schedules and executes periodic
// sync operations. It sits on top of pkg/sync.Manager and handles:
//
//   - Background sync scheduling using time.Ticker
//   - Initial sync on startup
//   - Status persistence and thread-safe access
//   - Graceful shutdown
//
// # Architecture
//
// The coordinator separates concerns between:
//
//   - pkg/sync: Domain logic (what/when to sync, how to detect changes)
//   - pkg/sync/coordinator: Orchestration (scheduling, lifecycle, state management)
//   - cmd/app/serve: HTTP server lifecycle (just starts/stops coordinator)
//
// # Core Interface
//
// The Coordinator interface provides a simple lifecycle API:
//
//	type Coordinator interface {
//	    Start(ctx context.Context) error  // Begin background sync loop
//	    Stop() error                       // Graceful shutdown
//	    GetStatus() *status.SyncStatus     // Thread-safe status access
//	}
//
// # Usage Example
//
//	// Create dependencies
//	syncManager := sync.NewDefaultSyncManager(...)
//	stateService := state.NewDBStateService(pool)
//
//	// Create coordinator with injected dependencies
//	coordinator := coordinator.New(syncManager, stateService, config)
//
//	// Start background sync
//	ctx, cancel := context.WithCancel(context.Background())
//	defer cancel()
//
//	go coordinator.Start(ctx)
//
//	// ... run server ...
//
//	// Stop on shutdown
//	coordinator.Stop()
//
// # Thread Safety
//
// The coordinator maintains an in-memory cache of sync status that is accessed by
// multiple goroutines (ticker goroutine and sync execution). All access to the cached
// status is protected by an internal mutex, eliminating the need for external
// synchronization wrappers.
//
// # Status Persistence
//
// The coordinator accepts a RegistryStateService interface for state management,
// backed by PostgreSQL for persistence.
//
// # Sync Decision Flow
//
// 1. Ticker fires (based on configured interval)
// 2. Coordinator calls checkSync()
// 3. checkSync() calls Manager.ShouldSync() to decide
// 4. If needed, performSync() executes the sync
// 5. Status is updated and persisted at each phase transition
//
// # Error Handling
//
// The coordinator handles errors gracefully:
//
//   - Failed syncs are logged and status updated to "Failed"
//   - Coordinator continues running even after failures
//   - Next sync attempt occurs on next ticker interval
//   - Status persistence errors are logged but don't stop sync
//
// # Integration with pkg/sync
//
// The coordinator delegates all sync logic to pkg/sync.Manager:
//
//   - ShouldSync(): Determines if sync is needed
//   - PerformSync(): Executes the actual sync operation
//
// This keeps the coordinator focused on scheduling and state management,
// while sync business logic stays in pkg/sync.
package coordinator
