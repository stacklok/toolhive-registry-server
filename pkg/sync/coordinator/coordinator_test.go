package coordinator

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/pkg/config"
	"github.com/stacklok/toolhive-registry-server/pkg/status"
	"github.com/stacklok/toolhive-registry-server/pkg/sync"
	syncmocks "github.com/stacklok/toolhive-registry-server/pkg/sync/mocks"
)

func TestPerformSync_StatusPersistence(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
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
					Return(&sync.Result{
						Hash:        "test-hash-123",
						ServerCount: 42,
					}, nil)
			} else {
				mockSyncMgr.EXPECT().
					PerformSync(gomock.Any(), gomock.Any()).
					Return(nil, tt.syncError)
			}

			// Create status persistence
			statusPersistence := status.NewFileStatusPersistence(statusFile)

			// Create coordinator with initial status
			cfg := &config.Config{
				RegistryName: "test-registry",
			}

			coord := &DefaultCoordinator{
				manager:           mockSyncMgr,
				statusPersistence: statusPersistence,
				config:            cfg,
				cachedStatus: &status.SyncStatus{
					Phase:   status.SyncPhaseFailed,
					Message: "Initial state",
				},
			}

			// Execute performSync
			ctx := context.Background()
			coord.performSync(ctx)

			// Verify the status object was updated correctly
			assert.Equal(t, tt.expectedPhase, coord.cachedStatus.Phase, "Phase should be updated")
			assert.Equal(t, tt.expectedMsg, coord.cachedStatus.Message, "Message should be updated")

			if tt.shouldHaveHash {
				assert.Equal(t, "test-hash-123", coord.cachedStatus.LastSyncHash, "Hash should be set")
				assert.Equal(t, 42, coord.cachedStatus.ServerCount, "Server count should be set")
				assert.NotNil(t, coord.cachedStatus.LastSyncTime, "Last sync time should be set")
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
	t.Parallel()
	t.Run("status is persisted even if sync succeeds", func(t *testing.T) {
		t.Parallel()
		mockCtrl := gomock.NewController(t)
		defer mockCtrl.Finish()

		tempDir := t.TempDir()
		statusFile := filepath.Join(tempDir, "status.json")

		mockSyncMgr := syncmocks.NewMockManager(mockCtrl)
		mockSyncMgr.EXPECT().
			PerformSync(gomock.Any(), gomock.Any()).
			Return(&sync.Result{Hash: "hash", ServerCount: 1}, nil)

		statusPersistence := status.NewFileStatusPersistence(statusFile)
		cfg := &config.Config{RegistryName: "test"}

		coord := &DefaultCoordinator{
			manager:           mockSyncMgr,
			statusPersistence: statusPersistence,
			config:            cfg,
			cachedStatus:      &status.SyncStatus{},
		}

		coord.performSync(context.Background())

		// Verify file exists (proves defer executed)
		assert.FileExists(t, statusFile)
	})
}

func TestPerformSync_SyncingPhasePersistedImmediately(t *testing.T) {
	t.Parallel()
	t.Run("Syncing phase is persisted before sync starts", func(t *testing.T) {
		t.Parallel()
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
			DoAndReturn(func(_ context.Context, _ *config.Config) (*sync.Result, *sync.Error) {
				// Signal that sync has started
				close(syncStarted)

				// At this point, the Syncing status should already be persisted
				// We'll verify this after the function returns

				// Simulate a long-running sync
				time.Sleep(100 * time.Millisecond)

				return &sync.Result{Hash: "test-hash", ServerCount: 10}, nil
			})

		cfg := &config.Config{RegistryName: "test"}
		coord := &DefaultCoordinator{
			manager:           mockSyncMgr,
			statusPersistence: statusPersistence,
			config:            cfg,
			cachedStatus: &status.SyncStatus{
				Phase: status.SyncPhaseFailed,
			},
		}

		// Run performSync in a goroutine so we can check the status while it's running
		done := make(chan struct{})
		go func() {
			coord.performSync(context.Background())
			close(done)
		}()

		// Wait for sync to start
		<-syncStarted

		// Now verify that the status file has Syncing phase already persisted
		loadedStatus, err := statusPersistence.LoadStatus(context.Background())
		assert.NoError(t, err)
		assert.Equal(t, status.SyncPhaseSyncing, loadedStatus.Phase, "Syncing phase should be persisted immediately")
		assert.Equal(t, "Sync in progress", loadedStatus.Message)

		// Wait for performSync to complete
		<-done

		// Final status should be Complete
		finalStatus, err := statusPersistence.LoadStatus(context.Background())
		assert.NoError(t, err)
		assert.Equal(t, status.SyncPhaseComplete, finalStatus.Phase)
	})
}

func TestPerformSync_PhaseTransitions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		initialPhase  status.SyncPhase
		syncResult    *sync.Result
		syncError     *sync.Error
		expectedPhase status.SyncPhase
	}{
		{
			name:          "Failed -> Syncing -> Complete on successful sync",
			initialPhase:  status.SyncPhaseFailed,
			syncResult:    &sync.Result{Hash: "abc123", ServerCount: 5},
			syncError:     nil,
			expectedPhase: status.SyncPhaseComplete,
		},
		{
			name:         "Failed -> Syncing -> Failed on failed sync",
			initialPhase: status.SyncPhaseFailed,
			syncResult:   nil,
			syncError: &sync.Error{
				Message: "network timeout",
			},
			expectedPhase: status.SyncPhaseFailed,
		},
		{
			name:          "Complete -> Syncing -> Complete on successful resync",
			initialPhase:  status.SyncPhaseComplete,
			syncResult:    &sync.Result{Hash: "xyz789", ServerCount: 10},
			syncError:     nil,
			expectedPhase: status.SyncPhaseComplete,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
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
				DoAndReturn(func(_ context.Context, _ *config.Config) (*sync.Result, *sync.Error) {
					close(syncStarted)
					time.Sleep(50 * time.Millisecond) // Simulate work
					return tt.syncResult, tt.syncError
				})

			cfg := &config.Config{RegistryName: "test"}
			coord := &DefaultCoordinator{
				manager:           mockSyncMgr,
				statusPersistence: statusPersistence,
				config:            cfg,
				cachedStatus:      &status.SyncStatus{Phase: tt.initialPhase},
			}

			// Run performSync in goroutine
			done := make(chan struct{})
			go func() {
				coord.performSync(context.Background())
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
			assert.Contains(t, observedPhases, status.SyncPhaseSyncing, "Should transition through Syncing")
			assert.Equal(t, tt.expectedPhase, finalStatus.Phase, "Should end in expected phase")
		})
	}
}

func TestUpdateStatusForSkippedSync(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		initialPhase status.SyncPhase
		initialMsg   string
		reason       string
		expectedMsg  string
	}{
		{
			name:         "updates message when phase is Complete",
			initialPhase: status.SyncPhaseComplete,
			initialMsg:   "Sync completed successfully",
			reason:       "up-to-date-no-policy",
			expectedMsg:  "Sync skipped: up-to-date-no-policy",
		},
		{
			name:         "does update message when phase is Failed",
			initialPhase: status.SyncPhaseFailed,
			initialMsg:   "Previous sync failed",
			reason:       "up-to-date-no-policy",
			expectedMsg:  "Sync skipped: up-to-date-no-policy",
		},
		{
			name:         "does update message when phase is Syncing",
			initialPhase: status.SyncPhaseSyncing,
			initialMsg:   "Sync in progress",
			reason:       "already-in-progress",
			expectedMsg:  "Sync skipped: already-in-progress",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tempDir := t.TempDir()
			statusFile := filepath.Join(tempDir, "status.json")
			statusPersistence := status.NewFileStatusPersistence(statusFile)

			coord := &DefaultCoordinator{
				statusPersistence: statusPersistence,
				cachedStatus: &status.SyncStatus{
					Phase:   tt.initialPhase,
					Message: tt.initialMsg,
				},
			}

			// Save initial status
			err := statusPersistence.SaveStatus(context.Background(), coord.cachedStatus)
			require.NoError(t, err)

			// Call updateStatusForSkippedSync
			coord.updateStatusForSkippedSync(context.Background(), tt.reason)

			// Verify the status
			assert.Equal(t, tt.expectedMsg, coord.cachedStatus.Message, "Message should be updated correctly")

			// Verify it was persisted (or not)
			loadedStatus, err := statusPersistence.LoadStatus(context.Background())
			require.NoError(t, err)
			assert.Equal(t, tt.expectedMsg, loadedStatus.Message, "Persisted message should match")
			assert.Equal(t, status.SyncPhaseComplete, loadedStatus.Phase, "Phase should not change")
		})
	}
}

func TestGetSyncInterval(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		policy   *config.SyncPolicyConfig
		expected time.Duration
	}{
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
		{
			name: "valid interval is parsed correctly",
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := getSyncInterval(tt.policy)
			assert.Equal(t, tt.expected, result)
		})
	}
}
