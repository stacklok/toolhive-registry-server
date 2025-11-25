package sync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/filtering"
	"github.com/stacklok/toolhive-registry-server/internal/sources"
	"github.com/stacklok/toolhive-registry-server/internal/status"
)

// Result contains the result of a successful sync operation
type Result struct {
	Hash        string
	ServerCount int
}

// Reason represents the decision and reason for whether a sync should occur
type Reason int

//nolint:revive // Group comments are for categorization, not per-constant documentation
const (
	// Reasons that require sync
	ReasonRegistryNotReady Reason = iota
	ReasonFilterChanged
	ReasonSourceDataChanged
	ReasonErrorCheckingChanges
	ReasonManualWithChanges

	// Reasons that do NOT require sync
	ReasonAlreadyInProgress
	ReasonManualNoChanges
	ReasonErrorParsingInterval
	ReasonErrorCheckingSyncNeed
	ReasonUpToDateWithPolicy
	ReasonUpToDateNoPolicy
)

// String returns the string representation of the sync reason
func (r Reason) String() string {
	switch r {
	case ReasonRegistryNotReady:
		return "registry-not-ready"
	case ReasonFilterChanged:
		return "filter-changed"
	case ReasonSourceDataChanged:
		return "source-data-changed"
	case ReasonErrorCheckingChanges:
		return "error-checking-data-changes"
	case ReasonManualWithChanges:
		return "manual-sync-with-data-changes"
	case ReasonAlreadyInProgress:
		return "sync-already-in-progress"
	case ReasonManualNoChanges:
		return "manual-sync-no-data-changes"
	case ReasonErrorParsingInterval:
		return "error-parsing-sync-interval"
	case ReasonErrorCheckingSyncNeed:
		return "error-checking-sync-need"
	case ReasonUpToDateWithPolicy:
		return "up-to-date-with-policy"
	case ReasonUpToDateNoPolicy:
		return "up-to-date-no-policy"
	default:
		return "unknown-sync-reason"
	}
}

// ShouldSync returns true if sync is needed, false otherwise
func (r Reason) ShouldSync() bool {
	return r <= ReasonManualWithChanges
}

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
	// ShouldSync determines if a sync operation is needed for a specific registry
	// Returns the sync reason which encodes both whether sync is needed and the reason
	ShouldSync(
		ctx context.Context, regCfg *config.RegistryConfig, syncStatus *status.SyncStatus, manualSyncRequested bool,
	) Reason

	// PerformSync executes the complete sync operation for a specific registry
	PerformSync(ctx context.Context, regCfg *config.RegistryConfig) (*Result, *Error)

	// Delete cleans up storage resources for a specific registry
	Delete(ctx context.Context, registryName string) error
}

// DataChangeDetector detects changes in source data
type DataChangeDetector interface {
	// IsDataChanged checks if source data has changed by comparing hashes for a specific registry
	IsDataChanged(ctx context.Context, regCfg *config.RegistryConfig, syncStatus *status.SyncStatus) (bool, error)
}

// AutomaticSyncChecker handles automatic sync timing logic
type AutomaticSyncChecker interface {
	// IsIntervalSyncNeeded checks if sync is needed based on time interval for a specific registry
	// Returns (syncNeeded, nextSyncTime, error) where nextSyncTime is always in the future
	IsIntervalSyncNeeded(regCfg *config.RegistryConfig, syncStatus *status.SyncStatus) (bool, time.Time, error)
}

// defaultSyncManager is the default implementation of Manager
type defaultSyncManager struct {
	registryHandlerFactory sources.RegistryHandlerFactory
	storageManager         sources.StorageManager
	filterService          filtering.FilterService
	dataChangeDetector     DataChangeDetector
	automaticSyncChecker   AutomaticSyncChecker
}

// NewDefaultSyncManager creates a new defaultSyncManager
func NewDefaultSyncManager(
	registryHandlerFactory sources.RegistryHandlerFactory, storageManager sources.StorageManager) Manager {
	return &defaultSyncManager{
		registryHandlerFactory: registryHandlerFactory,
		storageManager:         storageManager,
		filterService:          filtering.NewDefaultFilterService(),
		dataChangeDetector:     &defaultDataChangeDetector{registryHandlerFactory: registryHandlerFactory},
		automaticSyncChecker:   &defaultAutomaticSyncChecker{},
	}
}

// ShouldSync determines if a sync operation is needed for a specific registry
// Returns a Reason which encodes both whether sync is needed and the reason
func (s *defaultSyncManager) ShouldSync(
	ctx context.Context,
	regCfg *config.RegistryConfig,
	syncStatus *status.SyncStatus,
	manualSyncRequested bool,
) Reason {
	ctxLogger := log.FromContext(ctx)

	// If registry is currently syncing, don't start another sync
	if syncStatus.Phase == status.SyncPhaseSyncing {
		return ReasonAlreadyInProgress
	}

	// Check if sync is needed based on registry state
	syncNeededForState := s.isSyncNeededForState(syncStatus)
	// Check if filter has changed
	filterChanged := s.isFilterChanged(ctx, regCfg, syncStatus)
	// Check if interval has elapsed
	checkIntervalElapsed, _, err := s.automaticSyncChecker.IsIntervalSyncNeeded(regCfg, syncStatus)
	if err != nil {
		ctxLogger.Error(err, "Failed to determine if interval has elapsed")
		return ReasonErrorCheckingSyncNeed
	}

	reason := ReasonUpToDateNoPolicy

	// Check if update is needed for state, manual sync, or filter change
	dataChangedString := "N/A"
	if syncNeededForState || manualSyncRequested || filterChanged || checkIntervalElapsed {
		// Check if source data has changed
		dataChanged, err := s.dataChangeDetector.IsDataChanged(ctx, regCfg, syncStatus)
		if err != nil {
			ctxLogger.Error(err, "Failed to determine if data has changed")
			reason = ReasonErrorCheckingChanges
		} else {
			ctxLogger.Info("Checked data changes", "dataChanged", dataChanged)
			if dataChanged {
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
				if manualSyncRequested {
					reason = ReasonManualNoChanges
				}
			}
		}
		dataChangedString = fmt.Sprintf("%t", dataChanged)
	}

	ctxLogger.Info("ShouldSync", "syncNeededForState", syncNeededForState, "filterChanged", filterChanged,
		"manualSyncRequested", manualSyncRequested, "checkIntervalElapsed", checkIntervalElapsed, "dataChanged", dataChangedString)
	ctxLogger.Info("ShouldSync returning", "reason", reason.String(), "shouldSync", reason.ShouldSync())

	return reason
}

// isSyncNeededForState checks if sync is needed based on the registry's current state
func (*defaultSyncManager) isSyncNeededForState(syncStatus *status.SyncStatus) bool {
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
func (*defaultSyncManager) isFilterChanged(
	ctx context.Context, regCfg *config.RegistryConfig, syncStatus *status.SyncStatus,
) bool {
	logger := log.FromContext(ctx)

	currentFilter := regCfg.Filter
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

// PerformSync performs the complete sync operation for a specific registry
// Returns sync result on success, or error on failure
func (s *defaultSyncManager) PerformSync(
	ctx context.Context, regCfg *config.RegistryConfig,
) (*Result, *Error) {
	// Fetch and process registry data
	fetchResult, err := s.fetchAndProcessRegistryData(ctx, regCfg)
	if err != nil {
		return nil, err
	}

	// Store the processed registry data
	if err := s.storeRegistryData(ctx, regCfg, fetchResult); err != nil {
		return nil, err
	}

	// Return sync result with data for status collector
	syncResult := &Result{
		Hash:        fetchResult.Hash,
		ServerCount: fetchResult.ServerCount,
	}

	return syncResult, nil
}

// Delete cleans up storage resources for a specific registry
func (s *defaultSyncManager) Delete(ctx context.Context, registryName string) error {
	return s.storageManager.Delete(ctx, registryName)
}

// fetchAndProcessRegistryData handles registry handler creation, validation, fetch, and filtering
func (s *defaultSyncManager) fetchAndProcessRegistryData(
	ctx context.Context,
	regCfg *config.RegistryConfig) (*sources.FetchResult, *Error) {
	ctxLogger := log.FromContext(ctx)

	// Get registry handler
	registryHandler, err := s.registryHandlerFactory.CreateHandler(regCfg)
	if err != nil {
		ctxLogger.Error(err, "Failed to create registry handler")
		return nil, &Error{
			Err:             err,
			Message:         fmt.Sprintf("Failed to create registry handler: %v", err),
			ConditionType:   ConditionSourceAvailable,
			ConditionReason: conditionReasonHandlerCreationFailed,
		}
	}

	// Validate registry configuration
	if err := registryHandler.Validate(regCfg); err != nil {
		ctxLogger.Error(err, "Registry validation failed")
		return nil, &Error{
			Err:             err,
			Message:         fmt.Sprintf("Registry validation failed: %v", err),
			ConditionType:   ConditionSourceAvailable,
			ConditionReason: conditionReasonValidationFailed,
		}
	}

	// Execute fetch operation
	fetchResult, err := registryHandler.FetchRegistry(ctx, regCfg)
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
	if err := s.applyFilteringIfConfigured(ctx, regCfg, fetchResult); err != nil {
		return nil, err
	}

	return fetchResult, nil
}

// applyFilteringIfConfigured applies filtering to fetch result if registry has filter configuration
func (s *defaultSyncManager) applyFilteringIfConfigured(
	ctx context.Context,
	regCfg *config.RegistryConfig,
	fetchResult *sources.FetchResult) *Error {
	ctxLogger := log.FromContext(ctx)

	if regCfg.Filter != nil {
		ctxLogger.Info("Applying registry filters",
			"hasNameFilters", regCfg.Filter.Names != nil,
			"hasTagFilters", regCfg.Filter.Tags != nil)

		// Apply filtering to UpstreamRegistry
		filteredServerReg, err := s.filterService.ApplyFilters(ctx, fetchResult.Registry, regCfg.Filter)
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
		fetchResult.Registry = filteredServerReg
		fetchResult.ServerCount = len(filteredServerReg.Servers)

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
func (s *defaultSyncManager) storeRegistryData(
	ctx context.Context,
	regCfg *config.RegistryConfig,
	fetchResult *sources.FetchResult) *Error {
	ctxLogger := log.FromContext(ctx)

	if err := s.storageManager.Store(ctx, regCfg.Name, fetchResult.Registry); err != nil {
		ctxLogger.Error(err, "Failed to store registry data")
		return &Error{
			Err:             err,
			Message:         fmt.Sprintf("Storage failed: %v", err),
			ConditionType:   ConditionSyncSuccessful,
			ConditionReason: conditionReasonStorageFailed,
		}
	}

	ctxLogger.Info("Registry data stored successfully", "registryName", regCfg.Name)

	return nil
}
