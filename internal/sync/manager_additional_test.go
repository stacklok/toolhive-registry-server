package sync

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/stacklok/toolhive-registry-server/internal/status"
)

func TestDefaultSyncManager_isSyncNeededForState(t *testing.T) {
	t.Parallel()

	now := time.Now()

	tests := []struct {
		name        string
		syncStatus  *status.SyncStatus
		expected    bool
		description string
	}{
		{
			name: "sync needed when sync status is failed",
			syncStatus: &status.SyncStatus{
				Phase: status.SyncPhaseFailed,
			},
			expected:    true,
			description: "Should need sync when sync phase is failed",
		},
		{
			name: "sync needed when sync status is syncing",
			syncStatus: &status.SyncStatus{
				Phase: status.SyncPhaseSyncing,
			},
			expected:    true,
			description: "Should need sync when sync phase is syncing",
		},
		{
			name: "sync not needed when sync status is complete",
			syncStatus: &status.SyncStatus{
				Phase: status.SyncPhaseComplete,
			},
			expected:    false,
			description: "Should not need sync when sync phase is complete",
		},
		{
			name: "sync needed when no sync status and overall phase is failed",
			syncStatus: &status.SyncStatus{
				Phase: status.SyncPhaseFailed,
			},
			expected:    true,
			description: "Should need sync when no sync status but overall phase is failed",
		},
		{
			name: "sync not needed when sync complete, pending phase, but has last sync time",
			syncStatus: &status.SyncStatus{
				Phase:        status.SyncPhaseComplete,
				LastSyncTime: &now,
			},
			expected:    false,
			description: "Should not need sync when sync complete but overall pending (waiting for API)",
		},
		{
			name: "sync needed when sync never happened",
			syncStatus: &status.SyncStatus{
				Phase: "",
			},
			expected:    true,
			description: "Should need sync when sync never happened",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			manager := &DefaultSyncManager{}
			result := manager.isSyncNeededForState(tt.syncStatus)

			assert.Equal(t, tt.expected, result, tt.description)
		})
	}
}

func TestResult_Struct(t *testing.T) {
	t.Parallel()

	// Test Result struct creation and field access
	result := &Result{
		Hash:        "test-hash-123",
		ServerCount: 42,
	}

	assert.Equal(t, "test-hash-123", result.Hash)
	assert.Equal(t, 42, result.ServerCount)
}

func TestResult_ZeroValues(t *testing.T) {
	t.Parallel()

	// Test Result struct with zero values
	result := &Result{}

	assert.Equal(t, "", result.Hash)
	assert.Equal(t, 0, result.ServerCount)
}

func TestSyncReasonConstants(t *testing.T) {
	t.Parallel()

	// Verify sync reason constants are properly defined
	assert.Equal(t, "sync-already-in-progress", ReasonAlreadyInProgress)
	assert.Equal(t, "registry-not-ready", ReasonRegistryNotReady)
	assert.Equal(t, "error-checking-sync-need", ReasonErrorCheckingSyncNeed)
	assert.Equal(t, "error-checking-data-changes", ReasonErrorCheckingChanges)
	assert.Equal(t, "error-parsing-sync-interval", ReasonErrorParsingInterval)
	assert.Equal(t, "source-data-changed", ReasonSourceDataChanged)
	assert.Equal(t, "manual-sync-with-data-changes", ReasonManualWithChanges)
	assert.Equal(t, "manual-sync-no-data-changes", ReasonManualNoChanges)
	assert.Equal(t, "up-to-date-with-policy", ReasonUpToDateWithPolicy)
	assert.Equal(t, "up-to-date-no-policy", ReasonUpToDateNoPolicy)
}

func TestConditionReasonConstants(t *testing.T) {
	t.Parallel()

	// Verify condition reason constants are properly defined
	assert.Equal(t, "HandlerCreationFailed", conditionReasonHandlerCreationFailed)
	assert.Equal(t, "ValidationFailed", conditionReasonValidationFailed)
	assert.Equal(t, "FetchFailed", conditionReasonFetchFailed)
	assert.Equal(t, "StorageFailed", conditionReasonStorageFailed)
}

// Test helper function that was added during our refactoring
func TestDefaultSyncManager_isSyncNeededForState_EdgeCases(t *testing.T) {
	t.Parallel()

	manager := &DefaultSyncManager{}

	t.Run("handles nil registry", func(t *testing.T) {
		t.Parallel()

		// This should not panic but return sensible default
		result := manager.isSyncNeededForState(&status.SyncStatus{})
		assert.True(t, result, "Should need sync for empty registry")
	})

	t.Run("handles registry with empty status", func(t *testing.T) {
		t.Parallel()

		result := manager.isSyncNeededForState(&status.SyncStatus{})
		assert.True(t, result, "Should need sync for empty status")
	})

	t.Run("handles registry with sync status but empty phase", func(t *testing.T) {
		t.Parallel()

		syncStatus := &status.SyncStatus{
			Phase: "",
		}
		result := manager.isSyncNeededForState(syncStatus)
		// Empty phase is treated as needing sync since it's not complete
		assert.True(t, result, "Should need sync for empty sync phase")
	})
}

// Test the integration between the helper function and main ShouldSync logic
func TestDefaultSyncManager_isSyncNeededForState_Integration(t *testing.T) {
	t.Parallel()
	now := time.Now()

	tests := []struct {
		name            string
		setupSyncStatus func() *status.SyncStatus
		expectedSync    bool
		description     string
	}{
		{
			name: "registry transitioning from syncing to complete",
			setupSyncStatus: func() *status.SyncStatus {
				return &status.SyncStatus{
					Phase: status.SyncPhaseSyncing,
				}
			},
			expectedSync: true,
			description:  "Should need sync when currently syncing",
		},
		{
			name: "registry in stable ready state",
			setupSyncStatus: func() *status.SyncStatus {
				return &status.SyncStatus{
					Phase:        status.SyncPhaseComplete,
					LastSyncTime: &now,
					LastSyncHash: "stable-hash",
					ServerCount:  5,
				}
			},
			expectedSync: false,
			description:  "Should not need sync when in stable ready state",
		},
		{
			name: "registry after successful sync, API still deploying",
			setupSyncStatus: func() *status.SyncStatus {
				return &status.SyncStatus{
					Phase:        status.SyncPhaseComplete,
					LastSyncTime: &now,
					LastSyncHash: "recent-hash",
					ServerCount:  3,
				}
			},
			expectedSync: false,
			description:  "Should not need sync when sync is complete but overall pending due to API",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			manager := &DefaultSyncManager{}
			result := manager.isSyncNeededForState(tt.setupSyncStatus())

			assert.Equal(t, tt.expectedSync, result, tt.description)
		})
	}
}
