// Package sync provides synchronization management interfaces and implementations
// for registry resources in the ToolHive Registry Server.
//
// This package implements a clean separation of concerns by extracting all
// sync-related logic into dedicated interfaces and types:
//
// # Core Interfaces
//
//   - Manager: Main interface for orchestrating sync operations (domain logic)
//   - DataChangeDetector: Detects changes in source data using hash comparison
//   - AutomaticSyncChecker: Manages time-based automatic sync scheduling
//
// # Coordinator Package
//
// The sync/coordinator subpackage provides the orchestration layer that schedules
// and executes background sync operations. It handles ticker-based periodic syncs,
// status persistence, and lifecycle management. See pkg/sync/coordinator for details.
//
// # Result Types
//
//   - Result: Contains the outcome of successful sync operations (hash, server count)
//   - Error: Structured error type with Kubernetes condition information
//
// # Sync Decision Making
//
// The Manager.ShouldSync method evaluates multiple factors to determine if a sync
// is needed, returning a decision (bool) and reason (string):
//
//   - Registry state (failed, not ready, complete)
//   - Filter configuration changes (via hash comparison)
//   - Source data changes (via hash comparison)
//   - Sync interval elapsed (time-based automatic sync)
//   - Manual sync requests
//
// The coordinator package (pkg/sync/coordinator) handles periodic sync scheduling
// using the configured sync interval from server configuration.
//
// # Sync Reasons
//
// The package defines extensive reason constants to track why syncs occur or don't:
//
//   - ReasonAlreadyInProgress: Sync already running
//   - ReasonRegistryNotReady: Initial sync or recovery from failure
//   - ReasonSourceDataChanged: Source data hash changed
//   - ReasonFilterChanged: Filter configuration modified
//   - ReasonManualWithChanges: Manual sync requested with detected changes
//   - ReasonManualNoChanges: Manual sync requested but no changes detected
//   - ReasonUpToDateWithPolicy: No sync needed, data is current
//   - ReasonUpToDateNoPolicy: No sync needed, no automatic policy
//   - ReasonErrorCheckingChanges: Error during change detection
//
// # Kubernetes Status Integration
//
// The package defines condition types for Kubernetes status reporting:
//
//   - ConditionSourceAvailable: Source is accessible
//   - ConditionDataValid: Registry data passes validation
//   - ConditionSyncSuccessful: Last sync operation succeeded
//   - ConditionAPIReady: Registry API is ready to serve requests
//
// The Error type includes condition information for automatic status updates:
//   - ConditionType: Which condition to update
//   - ConditionReason: Reason code for the condition
//   - Message: Human-readable error message
//
// # Key Features
//
//   - Hash-based change detection to avoid unnecessary syncs
//   - Filter change detection via hash comparison
//   - Manual sync support
//   - Configurable sync intervals via server configuration
//   - Comprehensive error handling with structured Error types
//   - Detailed sync reason tracking for observability
//   - Resource cleanup on deletion
//
// # Implementation
//
// The package follows Go best practices by using interfaces for testability
// and dependency injection, while providing concrete struct implementations
// (DefaultSyncManager, DefaultDataChangeDetector, DefaultAutomaticSyncChecker)
// for actual functionality.
package sync
