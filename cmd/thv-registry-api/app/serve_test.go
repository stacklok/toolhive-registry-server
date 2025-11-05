package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stacklok/toolhive-registry-server/pkg/config"
	"github.com/stacklok/toolhive-registry-server/pkg/status"
	"github.com/stacklok/toolhive-registry-server/pkg/sync"
	syncmocks "github.com/stacklok/toolhive-registry-server/pkg/sync/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	ctrl "sigs.k8s.io/controller-runtime"
)

func TestPerformSync_StatusPersistence(t *testing.T) {
	tests := []struct {
		name           string
		syncError      *sync.Error
		expectedPhase  status.SyncPhase
		expectedMsg    string
		shouldHaveHash bool
	}{
		{
			name:           "successful sync updates status to Complete",
			syncError:      nil,
			expectedPhase:  status.SyncPhaseComplete,
			expectedMsg:    "Sync completed successfully",
			shouldHaveHash: true,
		},
		{
			name: "failed sync updates status to Failed",
			syncError: &sync.Error{
				Message: "sync failed due to network error",
			},
			expectedPhase:  status.SyncPhaseFailed,
			expectedMsg:    "sync failed due to network error",
			shouldHaveHash: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockCtrl := gomock.NewController(t)
			defer mockCtrl.Finish()

			// Create temporary directory for status file
			tempDir := t.TempDir()
			statusFile := filepath.Join(tempDir, "status.json")

			// Create mock sync manager
			mockSyncMgr := syncmocks.NewMockManager(mockCtrl)

			// Setup mock behavior
			if tt.syncError == nil {
				mockSyncMgr.EXPECT().
					PerformSync(gomock.Any(), gomock.Any()).
					Return(ctrl.Result{}, &sync.Result{
						Hash:        "test-hash-123",
						ServerCount: 42,
					}, nil)
			} else {
				mockSyncMgr.EXPECT().
					PerformSync(gomock.Any(), gomock.Any()).
					Return(ctrl.Result{}, nil, tt.syncError)
			}

			// Create status persistence
			statusPersistence := status.NewFileStatusPersistence(statusFile)

			// Create initial status
			syncStatus := &status.SyncStatus{
				Phase:   status.SyncPhaseFailed,
				Message: "Initial state",
			}

			// Create minimal config
			cfg := &config.Config{
				RegistryName: "test-registry",
			}

			// Execute performSync
			ctx := context.Background()
			performSync(ctx, mockSyncMgr, cfg, syncStatus, statusPersistence)

			// Verify the status object was updated correctly
			assert.Equal(t, tt.expectedPhase, syncStatus.Phase, "Phase should be updated")
			assert.Equal(t, tt.expectedMsg, syncStatus.Message, "Message should be updated")

			if tt.shouldHaveHash {
				assert.Equal(t, "test-hash-123", syncStatus.LastSyncHash, "Hash should be set")
				assert.Equal(t, 42, syncStatus.ServerCount, "Server count should be set")
				assert.NotNil(t, syncStatus.LastSyncTime, "Last sync time should be set")
			}

			// Verify the status was persisted to disk
			require.FileExists(t, statusFile, "Status file should be created")

			// Load the persisted status and verify
			loadedStatus, err := statusPersistence.LoadStatus(ctx)
			require.NoError(t, err, "Should load status from disk")
			assert.Equal(t, tt.expectedPhase, loadedStatus.Phase, "Persisted phase should match")
			assert.Equal(t, tt.expectedMsg, loadedStatus.Message, "Persisted message should match")

			if tt.shouldHaveHash {
				assert.Equal(t, "test-hash-123", loadedStatus.LastSyncHash, "Persisted hash should match")
				assert.Equal(t, 42, loadedStatus.ServerCount, "Persisted server count should match")
			}
		})
	}
}

func TestPerformSync_AlwaysPersists(t *testing.T) {
	t.Run("status is persisted even if sync panics", func(t *testing.T) {
		// Note: This test verifies that defer works, but we can't actually
		// test panic recovery since PerformSync would need to panic.
		// This is more of a structural verification.

		mockCtrl := gomock.NewController(t)
		defer mockCtrl.Finish()

		tempDir := t.TempDir()
		statusFile := filepath.Join(tempDir, "status.json")

		mockSyncMgr := syncmocks.NewMockManager(mockCtrl)
		mockSyncMgr.EXPECT().
			PerformSync(gomock.Any(), gomock.Any()).
			Return(ctrl.Result{}, &sync.Result{Hash: "hash", ServerCount: 1}, nil)

		statusPersistence := status.NewFileStatusPersistence(statusFile)
		syncStatus := &status.SyncStatus{}
		cfg := &config.Config{RegistryName: "test"}

		performSync(context.Background(), mockSyncMgr, cfg, syncStatus, statusPersistence)

		// Verify file exists (proves defer executed)
		assert.FileExists(t, statusFile)
	})
}

func TestPerformSync_SyncingPhasePersistedImmediately(t *testing.T) {
	t.Run("Syncing phase is persisted before sync starts", func(t *testing.T) {
		mockCtrl := gomock.NewController(t)
		defer mockCtrl.Finish()

		tempDir := t.TempDir()
		statusFile := filepath.Join(tempDir, "status.json")
		statusPersistence := status.NewFileStatusPersistence(statusFile)

		// Create a channel to coordinate the test
		syncStarted := make(chan struct{})

		mockSyncMgr := syncmocks.NewMockManager(mockCtrl)
		mockSyncMgr.EXPECT().
			PerformSync(gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, cfg *config.Config) (ctrl.Result, *sync.Result, *sync.Error) {
				// Signal that sync has started
				close(syncStarted)

				// At this point, the Syncing status should already be persisted
				// We'll verify this after the function returns

				// Simulate a long-running sync
				time.Sleep(100 * time.Millisecond)

				return ctrl.Result{}, &sync.Result{Hash: "test-hash", ServerCount: 10}, nil
			})

		syncStatus := &status.SyncStatus{
			Phase: status.SyncPhaseFailed,
		}
		cfg := &config.Config{RegistryName: "test"}

		// Run performSync in a goroutine so we can check the status while it's running
		done := make(chan struct{})
		go func() {
			performSync(context.Background(), mockSyncMgr, cfg, syncStatus, statusPersistence)
			close(done)
		}()

		// Wait for sync to start
		<-syncStarted

		// Load status from disk - it should show Syncing
		loadedStatus, err := statusPersistence.LoadStatus(context.Background())
		require.NoError(t, err)
		assert.Equal(t, status.SyncPhaseSyncing, loadedStatus.Phase, "Status should be Syncing while sync is in progress")
		assert.NotNil(t, loadedStatus.LastAttempt, "LastAttempt should be set")
		assert.Equal(t, 1, loadedStatus.AttemptCount, "AttemptCount should be incremented")

		// Wait for sync to complete
		<-done

		// Verify final status is Complete
		finalStatus, err := statusPersistence.LoadStatus(context.Background())
		require.NoError(t, err)
		assert.Equal(t, status.SyncPhaseComplete, finalStatus.Phase, "Final status should be Complete")
	})
}

func TestPerformSync_PhaseTransitions(t *testing.T) {
	tests := []struct {
		name           string
		initialPhase   status.SyncPhase
		syncResult     *sync.Result
		syncError      *sync.Error
		expectedPhases []status.SyncPhase // Phases we expect to see during the test
	}{
		{
			name:         "transitions from initial to Syncing to Complete",
			initialPhase: status.SyncPhaseFailed,
			syncResult:   &sync.Result{Hash: "abc123", ServerCount: 50},
			syncError:    nil,
			expectedPhases: []status.SyncPhase{
				status.SyncPhaseFailed,  // Initial
				status.SyncPhaseSyncing, // During sync
				status.SyncPhaseComplete, // After success
			},
		},
		{
			name:         "transitions from Complete to Syncing to Failed",
			initialPhase: status.SyncPhaseComplete,
			syncResult:   nil,
			syncError:    &sync.Error{Message: "network timeout"},
			expectedPhases: []status.SyncPhase{
				status.SyncPhaseComplete, // Initial
				status.SyncPhaseSyncing,  // During sync
				status.SyncPhaseFailed,   // After failure
			},
		},
		{
			name:         "transitions from empty to Syncing to Complete",
			initialPhase: "",
			syncResult:   &sync.Result{Hash: "def456", ServerCount: 25},
			syncError:    nil,
			expectedPhases: []status.SyncPhase{
				"",                       // Initial (empty)
				status.SyncPhaseSyncing, // During sync
				status.SyncPhaseComplete, // After success
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockCtrl := gomock.NewController(t)
			defer mockCtrl.Finish()

			tempDir := t.TempDir()
			statusFile := filepath.Join(tempDir, "status.json")
			statusPersistence := status.NewFileStatusPersistence(statusFile)

			// Track observed phases
			observedPhases := []status.SyncPhase{tt.initialPhase}
			syncStarted := make(chan struct{})

			mockSyncMgr := syncmocks.NewMockManager(mockCtrl)
			mockSyncMgr.EXPECT().
				PerformSync(gomock.Any(), gomock.Any()).
				DoAndReturn(func(ctx context.Context, cfg *config.Config) (ctrl.Result, *sync.Result, *sync.Error) {
					close(syncStarted)
					time.Sleep(50 * time.Millisecond) // Simulate work
					return ctrl.Result{}, tt.syncResult, tt.syncError
				})

			syncStatus := &status.SyncStatus{Phase: tt.initialPhase}
			cfg := &config.Config{RegistryName: "test"}

			// Run performSync in goroutine
			done := make(chan struct{})
			go func() {
				performSync(context.Background(), mockSyncMgr, cfg, syncStatus, statusPersistence)
				close(done)
			}()

			// Wait for sync to start and capture Syncing phase
			<-syncStarted
			loadedStatus, _ := statusPersistence.LoadStatus(context.Background())
			if loadedStatus != nil {
				observedPhases = append(observedPhases, loadedStatus.Phase)
			}

			// Wait for completion and capture final phase
			<-done
			finalStatus, err := statusPersistence.LoadStatus(context.Background())
			require.NoError(t, err)
			observedPhases = append(observedPhases, finalStatus.Phase)

			// Verify phase transitions
			assert.Equal(t, tt.expectedPhases, observedPhases, "Phase transitions should match expected sequence")

			// Verify final status details
			if tt.syncError == nil {
				assert.Equal(t, status.SyncPhaseComplete, finalStatus.Phase)
				assert.Equal(t, "Sync completed successfully", finalStatus.Message)
				assert.NotNil(t, finalStatus.LastSyncTime)
				assert.Equal(t, tt.syncResult.Hash, finalStatus.LastSyncHash)
				assert.Equal(t, tt.syncResult.ServerCount, finalStatus.ServerCount)
				assert.Equal(t, 0, finalStatus.AttemptCount, "AttemptCount should reset to 0 on success")
			} else {
				assert.Equal(t, status.SyncPhaseFailed, finalStatus.Phase)
				assert.Equal(t, tt.syncError.Message, finalStatus.Message)
			}
		})
	}
}

func TestUpdateStatusForSkippedSync(t *testing.T) {
	tests := []struct {
		name          string
		initialPhase  status.SyncPhase
		initialMsg    string
		reason        string
		shouldUpdate  bool
		expectedMsg   string
	}{
		{
			name:         "updates message when phase is Complete",
			initialPhase: status.SyncPhaseComplete,
			initialMsg:   "Sync completed successfully",
			reason:       "up-to-date",
			shouldUpdate: true,
			expectedMsg:  "Sync skipped: up-to-date",
		},
		{
			name:         "does not update when phase is Failed",
			initialPhase: status.SyncPhaseFailed,
			initialMsg:   "Network error",
			reason:       "up-to-date",
			shouldUpdate: false,
			expectedMsg:  "Network error",
		},
		{
			name:         "does not update when phase is Syncing",
			initialPhase: status.SyncPhaseSyncing,
			initialMsg:   "",
			reason:       "up-to-date",
			shouldUpdate: false,
			expectedMsg:  "",
		},
		{
			name:         "does not update when phase is empty",
			initialPhase: "",
			initialMsg:   "No previous sync",
			reason:       "up-to-date",
			shouldUpdate: false,
			expectedMsg:  "No previous sync",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()
			statusFile := filepath.Join(tempDir, "status.json")
			statusPersistence := status.NewFileStatusPersistence(statusFile)

			syncStatus := &status.SyncStatus{
				Phase:   tt.initialPhase,
				Message: tt.initialMsg,
			}

			// Save initial status
			err := statusPersistence.SaveStatus(context.Background(), syncStatus)
			require.NoError(t, err)

			// Call updateStatusForSkippedSync
			updateStatusForSkippedSync(context.Background(), syncStatus, statusPersistence, tt.reason)

			// Verify the status
			assert.Equal(t, tt.expectedMsg, syncStatus.Message, "Message should be updated correctly")

			// Verify it was persisted (or not)
			loadedStatus, err := statusPersistence.LoadStatus(context.Background())
			require.NoError(t, err)
			assert.Equal(t, tt.expectedMsg, loadedStatus.Message, "Persisted message should match")
			assert.Equal(t, tt.initialPhase, loadedStatus.Phase, "Phase should not change")
		})
	}
}

func TestGetSyncInterval(t *testing.T) {
	tests := []struct {
		name     string
		policy   *config.SyncPolicyConfig
		expected time.Duration
	}{
		{
			name: "valid interval is parsed",
			policy: &config.SyncPolicyConfig{
				Interval: "5m",
			},
			expected: 5 * time.Minute,
		},
		{
			name: "invalid interval returns default",
			policy: &config.SyncPolicyConfig{
				Interval: "invalid",
			},
			expected: time.Minute,
		},
		{
			name:     "nil policy returns default",
			policy:   nil,
			expected: time.Minute,
		},
		{
			name: "empty interval returns default",
			policy: &config.SyncPolicyConfig{
				Interval: "",
			},
			expected: time.Minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getSyncInterval(tt.policy)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMain(m *testing.M) {
	// Run tests
	code := m.Run()
	os.Exit(code)
}
