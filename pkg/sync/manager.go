package sync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/stacklok/toolhive-registry-server/pkg/config"
	"github.com/stacklok/toolhive-registry-server/pkg/filtering"
	"github.com/stacklok/toolhive-registry-server/pkg/sources"
	"github.com/stacklok/toolhive-registry-server/pkg/status"
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
//go:generate mockgen -destination=mocks/mock_manager.go -package=mocks github.com/stacklok/toolhive-registry-server/pkg/sync Manager
type Manager interface {
	// ShouldSync determines if a sync operation is needed
	ShouldSync(ctx context.Context, config *config.Config, syncStatus *status.SyncStatus, manualSyncRequested bool) (bool, string, *time.Time)

	// PerformSync executes the complete sync operation
	PerformSync(ctx context.Context, config *config.Config) (*Result, *Error)

	// Delete cleans up storage resources for the Registry
	Delete(ctx context.Context, config *config.Config) error
}

// DataChangeDetector detects changes in source data
type DataChangeDetector interface {
	// IsDataChanged checks if source data has changed by comparing hashes
	IsDataChanged(ctx context.Context, config *config.Config, syncStatus *status.SyncStatus) (bool, error)
}

// AutomaticSyncChecker handles automatic sync timing logic
type AutomaticSyncChecker interface {
	// IsIntervalSyncNeeded checks if sync is needed based on time interval
	// Returns (syncNeeded, nextSyncTime, error) where nextSyncTime is always in the future
	IsIntervalSyncNeeded(config *config.Config, syncStatus *status.SyncStatus) (bool, time.Time, error)
}

// DefaultSyncManager is the default implementation of Manager
type DefaultSyncManager struct {
	client               client.Client
	scheme               *runtime.Scheme
	sourceHandlerFactory sources.SourceHandlerFactory
	storageManager       sources.StorageManager
	filterService        filtering.FilterService
	dataChangeDetector   DataChangeDetector
	automaticSyncChecker AutomaticSyncChecker
}

// NewDefaultSyncManager creates a new DefaultSyncManager
func NewDefaultSyncManager(k8sClient client.Client, scheme *runtime.Scheme,
	sourceHandlerFactory sources.SourceHandlerFactory, storageManager sources.StorageManager) *DefaultSyncManager {
	return &DefaultSyncManager{
		client:               k8sClient,
		scheme:               scheme,
		sourceHandlerFactory: sourceHandlerFactory,
		storageManager:       storageManager,
		filterService:        filtering.NewDefaultFilterService(),
		dataChangeDetector:   &DefaultDataChangeDetector{sourceHandlerFactory: sourceHandlerFactory},
		automaticSyncChecker: &DefaultAutomaticSyncChecker{},
	}
}

// ShouldSync determines if a sync operation is needed
// Returns: (shouldSync bool, reason string, nextSyncTime *time.Time)
// nextSyncTime is always nil - timing is controlled by the configured sync interval
func (s *DefaultSyncManager) ShouldSync(
	ctx context.Context,
	config *config.Config,
	syncStatus *status.SyncStatus,
	manualSyncRequested bool,
) (bool, string, *time.Time) {
	ctxLogger := log.FromContext(ctx)

	// If registry is currently syncing, don't start another sync
	if syncStatus.Phase == status.SyncPhaseSyncing {
		return false, ReasonAlreadyInProgress, nil
	}

	// Check if sync is needed based on registry state
	syncNeededForState := s.isSyncNeededForState(syncStatus)
	// Check if filter has changed
	filterChanged := s.isFilterChanged(ctx, config, syncStatus)
	// Check if interval has elapsed
	checkIntervalElapsed, _, err := s.automaticSyncChecker.IsIntervalSyncNeeded(config, syncStatus)
	if err != nil {
		ctxLogger.Error(err, "Failed to determine if interval has elapsed")
		return false, ReasonErrorCheckingSyncNeed, nil
	}

	shouldSync := false
	reason := ReasonUpToDateNoPolicy

	// Check update needed for state, manual sync, or filter changed
	dataChangedString := "N/A"
	if syncNeededForState || manualSyncRequested || filterChanged || checkIntervalElapsed {
		// Check if source data has changed
		dataChanged, err := s.dataChangeDetector.IsDataChanged(ctx, config, syncStatus)
		if err != nil {
			ctxLogger.Error(err, "Failed to determine if data has changed")
			shouldSync = true
			reason = ReasonErrorCheckingChanges
		} else {
			ctxLogger.Info("Checked data changes", "dataChanged", dataChanged)
			if dataChanged {
				shouldSync = true
				if syncNeededForState {
					reason = ReasonRegistryNotReady
				} else if manualSyncRequested {
					reason = ReasonManualWithChanges
				} else if filterChanged {
					reason = ReasonFilterChanged
				} else {
					reason = ReasonSourceDataChanged
				}
			} else {
				shouldSync = false
				if manualSyncRequested {
					reason = ReasonManualNoChanges
				}
			}
		}
		dataChangedString = fmt.Sprintf("%t", dataChanged)
	}

	ctxLogger.Info("ShouldSync", "syncNeededForState", syncNeededForState, "filterChanged", filterChanged,
		"manualSyncRequested", manualSyncRequested, "checkIntervalElapsed", checkIntervalElapsed, "dataChanged", dataChangedString)
	ctxLogger.Info("ShouldSync returning", "shouldSync", shouldSync, "reason", reason)

	return shouldSync, reason, nil
}

// isSyncNeededForState checks if sync is needed based on the registry's current state
func (*DefaultSyncManager) isSyncNeededForState(syncStatus *status.SyncStatus) bool {
	// If we have sync status, use it to determine sync readiness
	if syncStatus != nil {
		syncPhase := syncStatus.Phase
		// If sync is failed, sync is needed
		if syncPhase == status.SyncPhaseFailed {
			return true
		}
		// If sync is not complete, sync is needed
		if syncPhase != status.SyncPhaseComplete {
			return true
		}
		// Sync is complete, no sync needed based on state
		return false
	}

	// If we don't have sync status, sync is needed
	return true
}

// isFilterChanged checks if the filter has changed compared to the last applied configuration
func (*DefaultSyncManager) isFilterChanged(ctx context.Context, config *config.Config, syncStatus *status.SyncStatus) bool {
	logger := log.FromContext(ctx)

	currentFilter := config.Filter
	currentFilterJSON, err := json.Marshal(currentFilter)
	if err != nil {
		logger.Error(err, "Failed to marshal current filter")
		return false
	}
	currentFilterHash := sha256.Sum256(currentFilterJSON)
	currentHashStr := hex.EncodeToString(currentFilterHash[:])

	lastHash := syncStatus.LastAppliedFilterHash
	if lastHash == "" {
		// First time - no change
		return false
	}

	logger.V(1).Info("Current filter hash", "currentFilterHash", currentHashStr)
	logger.V(1).Info("Last applied filter hash", "lastHash", lastHash)
	return currentHashStr != lastHash
}

// PerformSync performs the complete sync operation for the Registry
// Returns sync result on success, or error on failure
func (s *DefaultSyncManager) PerformSync(
	ctx context.Context, config *config.Config,
) (*Result, *Error) {
	// Fetch and process registry data
	fetchResult, err := s.fetchAndProcessRegistryData(ctx, config)
	if err != nil {
		return nil, err
	}

	// Store the processed registry data
	if err := s.storeRegistryData(ctx, config, fetchResult); err != nil {
		return nil, err
	}

	// Return sync result with data for status collector
	syncResult := &Result{
		Hash:        fetchResult.Hash,
		ServerCount: fetchResult.ServerCount,
	}

	return syncResult, nil
}

// Delete cleans up storage resources for the Registry
func (s *DefaultSyncManager) Delete(ctx context.Context, config *config.Config) error {
	return s.storageManager.Delete(ctx, config)
}

// fetchAndProcessRegistryData handles source handler creation, validation, fetch, and filtering
func (s *DefaultSyncManager) fetchAndProcessRegistryData(
	ctx context.Context,
	config *config.Config) (*sources.FetchResult, *Error) {
	ctxLogger := log.FromContext(ctx)

	// Get source handler
	sourceHandler, err := s.sourceHandlerFactory.CreateHandler(config.Source.Type)
	if err != nil {
		ctxLogger.Error(err, "Failed to create source handler")
		return nil, &Error{
			Err:             err,
			Message:         fmt.Sprintf("Failed to create source handler: %v", err),
			ConditionType:   ConditionSourceAvailable,
			ConditionReason: conditionReasonHandlerCreationFailed,
		}
	}

	// Validate source configuration
	if err := sourceHandler.Validate(&config.Source); err != nil {
		ctxLogger.Error(err, "Source validation failed")
		return nil, &Error{
			Err:             err,
			Message:         fmt.Sprintf("Source validation failed: %v", err),
			ConditionType:   ConditionSourceAvailable,
			ConditionReason: conditionReasonValidationFailed,
		}
	}

	// Execute fetch operation
	fetchResult, err := sourceHandler.FetchRegistry(ctx, config)
	if err != nil {
		ctxLogger.Error(err, "Fetch operation failed")
		// Sync attempt counting is now handled by the controller via status collector
		return nil, &Error{
			Err:             err,
			Message:         fmt.Sprintf("Fetch failed: %v", err),
			ConditionType:   ConditionSyncSuccessful,
			ConditionReason: conditionReasonFetchFailed,
		}
	}

	ctxLogger.Info("Registry data fetched successfully from source",
		"serverCount", fetchResult.ServerCount,
		"format", fetchResult.Format,
		"hash", fetchResult.Hash)

	// Apply filtering if configured
	if err := s.applyFilteringIfConfigured(ctx, config, fetchResult); err != nil {
		return nil, err
	}

	return fetchResult, nil
}

// applyFilteringIfConfigured applies filtering to fetch result if registry has filter configuration
func (s *DefaultSyncManager) applyFilteringIfConfigured(
	ctx context.Context,
	config *config.Config,
	fetchResult *sources.FetchResult) *Error {
	ctxLogger := log.FromContext(ctx)

	if config.Filter != nil {
		ctxLogger.Info("Applying registry filters",
			"hasNameFilters", config.Filter.Names != nil,
			"hasTagFilters", config.Filter.Tags != nil)

		filteredRegistry, err := s.filterService.ApplyFilters(ctx, fetchResult.Registry, config.Filter)
		if err != nil {
			ctxLogger.Error(err, "Registry filtering failed")
			return &Error{
				Err:             err,
				Message:         fmt.Sprintf("Filtering failed: %v", err),
				ConditionType:   ConditionSyncSuccessful,
				ConditionReason: conditionReasonFetchFailed,
			}
		}

		// Update fetch result with filtered data
		originalServerCount := fetchResult.ServerCount
		fetchResult.Registry = filteredRegistry
		fetchResult.ServerCount = len(filteredRegistry.Servers) + len(filteredRegistry.RemoteServers)

		ctxLogger.Info("Registry filtering completed",
			"originalServerCount", originalServerCount,
			"filteredServerCount", fetchResult.ServerCount,
			"serversFiltered", originalServerCount-fetchResult.ServerCount)
	} else {
		ctxLogger.Info("No filtering configured, using original registry data")
	}

	return nil
}

// storeRegistryData stores the registry data using the storage manager
func (s *DefaultSyncManager) storeRegistryData(
	ctx context.Context,
	config *config.Config,
	fetchResult *sources.FetchResult) *Error {
	ctxLogger := log.FromContext(ctx)

	if err := s.storageManager.Store(ctx, config, fetchResult.Registry); err != nil {
		ctxLogger.Error(err, "Failed to store registry data")
		return &Error{
			Err:             err,
			Message:         fmt.Sprintf("Storage failed: %v", err),
			ConditionType:   ConditionSyncSuccessful,
			ConditionReason: conditionReasonStorageFailed,
		}
	}

	ctxLogger.Info("Registry data stored successfully")
	// TODO add registry and source name to the context

	return nil
}
