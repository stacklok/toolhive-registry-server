package sync

import (
	"context"
	"time"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/status"
)

// Result contains the result of a successful sync operation
type Result struct {
	Hash        string
	ServerCount int
}

// Sync reason constants
const (
	// Registry state related reasons
	ReasonAlreadyInProgress = "sync-already-in-progress"
	ReasonRegistryNotReady  = "registry-not-ready"

	// Filter change related reasons
	ReasonFilterChanged = "filter-changed"

	// Data change related reasons
	ReasonSourceDataChanged    = "source-data-changed"
	ReasonErrorCheckingChanges = "error-checking-data-changes"

	// Manual sync related reasons
	ReasonManualWithChanges = "manual-sync-with-data-changes"
	ReasonManualNoChanges   = "manual-sync-no-data-changes"

	// Automatic sync related reasons
	ReasonErrorParsingInterval  = "error-parsing-sync-interval"
	ReasonErrorCheckingSyncNeed = "error-checking-sync-need"

	// Up-to-date reasons
	ReasonUpToDateWithPolicy = "up-to-date-with-policy"
	ReasonUpToDateNoPolicy   = "up-to-date-no-policy"
)

// Manual sync annotation detection reasons
const (
	ManualSyncReasonNoAnnotations    = "no-annotations"
	ManualSyncReasonNoTrigger        = "no-manual-trigger"
	ManualSyncReasonAlreadyProcessed = "manual-trigger-already-processed"
	ManualSyncReasonRequested        = "manual-sync-requested"
)

// Condition reasons for status conditions
const (
	// Failure reasons
	conditionReasonHandlerCreationFailed = "HandlerCreationFailed"
	conditionReasonValidationFailed      = "ValidationFailed"
	conditionReasonFetchFailed           = "FetchFailed"
	conditionReasonStorageFailed         = "StorageFailed"
)

// Condition types for Config
const (
	// ConditionSourceAvailable indicates whether the source is available and accessible
	ConditionSourceAvailable = "SourceAvailable"

	// ConditionDataValid indicates whether the registry data is valid
	ConditionDataValid = "DataValid"

	// ConditionSyncSuccessful indicates whether the last sync was successful
	ConditionSyncSuccessful = "SyncSuccessful"

	// ConditionAPIReady indicates whether the registry API is ready
	ConditionAPIReady = "APIReady"
)

// Error represents a structured error with condition information for operator components
type Error struct {
	Err             error
	Message         string
	ConditionType   string
	ConditionReason string
}

func (e *Error) Error() string {
	return e.Message
}

func (e *Error) Unwrap() error {
	return e.Err
}

// Manager manages synchronization operations for Registry resources
//
//go:generate mockgen -destination=mocks/mock_manager.go -package=mocks github.com/stacklok/toolhive-registry-server/internal/sync Manager
type Manager interface {
	// ShouldSync determines if a sync operation is needed
	ShouldSync(
		ctx context.Context, cfg *config.Config, syncStatus *status.SyncStatus, manualSyncRequested bool,
	) (bool, string, *time.Time)

	// PerformSync executes the complete sync operation
	PerformSync(ctx context.Context, cfg *config.Config) (*Result, *Error)

	// Delete cleans up storage resources for the Registry
	Delete(ctx context.Context, cfg *config.Config) error
}

// DataChangeDetector detects changes in source data
type DataChangeDetector interface {
	// IsDataChanged checks if source data has changed by comparing hashes
	IsDataChanged(ctx context.Context, cfg *config.Config, syncStatus *status.SyncStatus) (bool, error)
}

// AutomaticSyncChecker handles automatic sync timing logic
type AutomaticSyncChecker interface {
	// IsIntervalSyncNeeded checks if sync is needed based on time interval
	// Returns (syncNeeded, nextSyncTime, error) where nextSyncTime is always in the future
	IsIntervalSyncNeeded(cfg *config.Config, syncStatus *status.SyncStatus) (bool, time.Time, error)
}
