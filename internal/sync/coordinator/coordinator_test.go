package coordinator

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/status"
	statusmocks "github.com/stacklok/toolhive-registry-server/internal/status/mocks"
	"github.com/stacklok/toolhive-registry-server/internal/sync"
	syncmocks "github.com/stacklok/toolhive-registry-server/internal/sync/mocks"
)

const (
	testRegistryName = "test-registry"
)

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

func TestCoordinator_New(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockManager := syncmocks.NewMockManager(ctrl)
	mockStatusPersistence := statusmocks.NewMockStatusPersistence(ctrl)
	cfg := &config.Config{
		RegistryName: "test-registry",
		Registries: []config.RegistryConfig{
			{Name: "registry-1"},
		},
	}

	coordinator := New(mockManager, mockStatusPersistence, cfg)

	require.NotNil(t, coordinator)
}

func TestCoordinator_GetStatus(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockManager := syncmocks.NewMockManager(ctrl)
	mockStatusPersistence := statusmocks.NewMockStatusPersistence(ctrl)
	cfg := &config.Config{
		RegistryName: "test-registry",
		Registries: []config.RegistryConfig{
			{Name: "registry-1"},
		},
	}

	coordinator := New(mockManager, mockStatusPersistence, cfg).(*defaultCoordinator)

	// Set up cached status
	now := time.Now()
	testStatus := &status.SyncStatus{
		Phase:        status.SyncPhaseComplete,
		LastSyncTime: &now,
		ServerCount:  5,
	}
	coordinator.cachedStatuses["registry-1"] = testStatus

	// Test getting existing status
	result := coordinator.GetStatus("registry-1")
	require.NotNil(t, result)
	assert.Equal(t, testStatus.Phase, result.Phase)
	assert.Equal(t, testStatus.ServerCount, result.ServerCount)

	// Verify it's a copy (not the same pointer)
	assert.NotSame(t, testStatus, result)

	// Test getting non-existent status
	result = coordinator.GetStatus("non-existent")
	assert.Nil(t, result)
}

func TestCoordinator_GetAllStatus(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockManager := syncmocks.NewMockManager(ctrl)
	mockStatusPersistence := statusmocks.NewMockStatusPersistence(ctrl)
	cfg := &config.Config{
		RegistryName: "test-registry",
		Registries: []config.RegistryConfig{
			{Name: "registry-1"},
			{Name: "registry-2"},
		},
	}

	coordinator := New(mockManager, mockStatusPersistence, cfg).(*defaultCoordinator)

	// Set up cached statuses
	status1 := &status.SyncStatus{
		Phase:       status.SyncPhaseComplete,
		ServerCount: 5,
	}
	status2 := &status.SyncStatus{
		Phase:       status.SyncPhaseComplete,
		ServerCount: 10,
	}
	coordinator.cachedStatuses["registry-1"] = status1
	coordinator.cachedStatuses["registry-2"] = status2

	// Test getting all statuses
	result := coordinator.GetAllStatus()
	require.NotNil(t, result)
	assert.Len(t, result, 2)

	// Verify registry-1
	assert.Contains(t, result, "registry-1")
	assert.Equal(t, status1.Phase, result["registry-1"].Phase)
	assert.Equal(t, status1.ServerCount, result["registry-1"].ServerCount)
	assert.NotSame(t, status1, result["registry-1"]) // Verify it's a copy

	// Verify registry-2
	assert.Contains(t, result, "registry-2")
	assert.Equal(t, status2.Phase, result["registry-2"].Phase)
	assert.Equal(t, status2.ServerCount, result["registry-2"].ServerCount)
	assert.NotSame(t, status2, result["registry-2"]) // Verify it's a copy
}

func TestCoordinator_GetAllStatus_EmptyCache(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockManager := syncmocks.NewMockManager(ctrl)
	mockStatusPersistence := statusmocks.NewMockStatusPersistence(ctrl)
	cfg := &config.Config{
		RegistryName: "test-registry",
		Registries:   []config.RegistryConfig{},
	}

	coordinator := New(mockManager, mockStatusPersistence, cfg)

	result := coordinator.GetAllStatus()
	require.NotNil(t, result)
	assert.Empty(t, result)
}

func TestCoordinator_Stop_BeforeStart(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockManager := syncmocks.NewMockManager(ctrl)
	mockStatusPersistence := statusmocks.NewMockStatusPersistence(ctrl)
	cfg := &config.Config{
		RegistryName: "test-registry",
		Registries:   []config.RegistryConfig{},
	}

	coordinator := New(mockManager, mockStatusPersistence, cfg)

	// Stop should not panic if called before Start
	err := coordinator.Stop()
	assert.NoError(t, err)
}

func TestPerformRegistrySync_StatusPersistence(t *testing.T) {
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

			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			// Create temporary directory for status file
			tempDir := t.TempDir()

			// Create mock sync manager
			mockSyncMgr := syncmocks.NewMockManager(ctrl)

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
			statusPersistence := status.NewFileStatusPersistence(tempDir)

			// Create config
			registryName := testRegistryName
			cfg := &config.Config{
				RegistryName: "global-registry",
				Registries: []config.RegistryConfig{
					{Name: registryName},
				},
			}
			regCfg := &cfg.Registries[0]

			// Create coordinator with initial status
			coord := &defaultCoordinator{
				manager:           mockSyncMgr,
				statusPersistence: statusPersistence,
				config:            cfg,
				cachedStatuses: map[string]*status.SyncStatus{
					registryName: {
						Phase:   status.SyncPhaseFailed,
						Message: "Initial state",
					},
				},
			}

			// Execute performRegistrySync
			ctx := context.Background()
			coord.performRegistrySync(ctx, regCfg)

			// Verify the status object was updated correctly
			cachedStatus := coord.cachedStatuses[registryName]
			assert.Equal(t, tt.expectedPhase, cachedStatus.Phase, "Phase should be updated")
			assert.Equal(t, tt.expectedMsg, cachedStatus.Message, "Message should be updated")

			if tt.shouldHaveHash {
				assert.Equal(t, "test-hash-123", cachedStatus.LastSyncHash, "Hash should be set")
				assert.Equal(t, 42, cachedStatus.ServerCount, "ServerCount should be set")
				assert.NotNil(t, cachedStatus.LastSyncTime, "LastSyncTime should be set")
			}

			// Verify status was persisted to file
			loadedStatus, err := statusPersistence.LoadStatus(ctx, registryName)
			require.NoError(t, err)
			assert.Equal(t, tt.expectedPhase, loadedStatus.Phase, "Persisted phase should match")
			assert.Equal(t, tt.expectedMsg, loadedStatus.Message, "Persisted message should match")

			if tt.shouldHaveHash {
				assert.Equal(t, "test-hash-123", loadedStatus.LastSyncHash, "Persisted hash should match")
			}
		})
	}
}

func TestPerformRegistrySync_AlwaysPersists(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	tempDir := t.TempDir()
	mockSyncMgr := syncmocks.NewMockManager(ctrl)

	// Mock successful sync
	mockSyncMgr.EXPECT().
		PerformSync(gomock.Any(), gomock.Any()).
		Return(&sync.Result{
			Hash:        "test-hash",
			ServerCount: 10,
		}, nil)

	statusPersistence := status.NewFileStatusPersistence(tempDir)
	registryName := testRegistryName

	cfg := &config.Config{
		RegistryName: "global-registry",
		Registries: []config.RegistryConfig{
			{Name: registryName},
		},
	}
	regCfg := &cfg.Registries[0]

	coord := &defaultCoordinator{
		manager:           mockSyncMgr,
		statusPersistence: statusPersistence,
		config:            cfg,
		cachedStatuses: map[string]*status.SyncStatus{
			registryName: {
				Phase:   status.SyncPhaseFailed,
				Message: "Initial",
			},
		},
	}

	// Execute
	coord.performRegistrySync(context.Background(), regCfg)

	// Verify status file exists and was written
	loadedStatus, err := statusPersistence.LoadStatus(context.Background(), registryName)
	require.NoError(t, err)
	assert.NotNil(t, loadedStatus)
	assert.Equal(t, status.SyncPhaseComplete, loadedStatus.Phase)
}

func TestPerformRegistrySync_SyncingPhasePersistedImmediately(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	tempDir := t.TempDir()
	mockSyncMgr := syncmocks.NewMockManager(ctrl)

	// Mock sync that takes some time and succeeds
	syncCalled := make(chan struct{})
	mockSyncMgr.EXPECT().
		PerformSync(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ *config.RegistryConfig) (*sync.Result, *sync.Error) {
			close(syncCalled)
			time.Sleep(50 * time.Millisecond) // Simulate work
			return &sync.Result{
				Hash:        "test-hash",
				ServerCount: 5,
			}, nil
		})

	statusPersistence := status.NewFileStatusPersistence(tempDir)
	registryName := testRegistryName

	cfg := &config.Config{
		RegistryName: "global-registry",
		Registries: []config.RegistryConfig{
			{Name: registryName},
		},
	}
	regCfg := &cfg.Registries[0]

	coord := &defaultCoordinator{
		manager:           mockSyncMgr,
		statusPersistence: statusPersistence,
		config:            cfg,
		cachedStatuses: map[string]*status.SyncStatus{
			registryName: {
				Phase: status.SyncPhaseFailed,
			},
		},
	}

	// Execute sync in background
	done := make(chan struct{})
	go func() {
		coord.performRegistrySync(context.Background(), regCfg)
		close(done)
	}()

	// Wait for sync to be called
	<-syncCalled

	// While sync is in progress, check that status shows "Syncing"
	loadedStatus, err := statusPersistence.LoadStatus(context.Background(), registryName)
	require.NoError(t, err)
	assert.Equal(t, status.SyncPhaseSyncing, loadedStatus.Phase, "Status should be Syncing while operation is in progress")

	// Wait for completion
	<-done

	// Final status should be Complete
	finalStatus, err := statusPersistence.LoadStatus(context.Background(), registryName)
	require.NoError(t, err)
	assert.Equal(t, status.SyncPhaseComplete, finalStatus.Phase)
}

func TestPerformRegistrySync_PhaseTransitions(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	tempDir := t.TempDir()
	mockSyncMgr := syncmocks.NewMockManager(ctrl)

	mockSyncMgr.EXPECT().
		PerformSync(gomock.Any(), gomock.Any()).
		Return(&sync.Result{
			Hash:        "new-hash",
			ServerCount: 15,
		}, nil)

	statusPersistence := status.NewFileStatusPersistence(tempDir)
	registryName := testRegistryName

	cfg := &config.Config{
		RegistryName: "global-registry",
		Registries: []config.RegistryConfig{
			{Name: registryName},
		},
	}
	regCfg := &cfg.Registries[0]

	// Start with Failed status
	initialStatus := &status.SyncStatus{
		Phase:   status.SyncPhaseFailed,
		Message: "Previous sync failed",
	}

	coord := &defaultCoordinator{
		manager:           mockSyncMgr,
		statusPersistence: statusPersistence,
		config:            cfg,
		cachedStatuses: map[string]*status.SyncStatus{
			registryName: initialStatus,
		},
	}

	// Execute sync
	coord.performRegistrySync(context.Background(), regCfg)

	// Verify transition: Failed -> Syncing -> Complete
	finalStatus := coord.cachedStatuses[registryName]
	assert.Equal(t, status.SyncPhaseComplete, finalStatus.Phase)
	assert.Equal(t, "Sync completed successfully", finalStatus.Message)
	assert.Equal(t, "new-hash", finalStatus.LastSyncHash)
	assert.Equal(t, 15, finalStatus.ServerCount)
}

func TestUpdateStatusForSkippedSync(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	tempDir := t.TempDir()
	mockSyncMgr := syncmocks.NewMockManager(ctrl)

	statusPersistence := status.NewFileStatusPersistence(tempDir)
	registryName := testRegistryName

	cfg := &config.Config{
		RegistryName: "global-registry",
		Registries: []config.RegistryConfig{
			{Name: registryName},
		},
	}
	regCfg := &cfg.Registries[0]

	coord := &defaultCoordinator{
		manager:           mockSyncMgr,
		statusPersistence: statusPersistence,
		config:            cfg,
		cachedStatuses: map[string]*status.SyncStatus{
			registryName: {
				Phase:   status.SyncPhaseComplete,
				Message: "Previous sync completed",
			},
		},
	}

	// Call updateStatusForSkippedSync
	reason := sync.ReasonUpToDateWithPolicy
	coord.updateStatusForSkippedSync(context.Background(), regCfg, reason)

	// Verify status is updated but phase remains the same
	finalStatus := coord.cachedStatuses[registryName]
	assert.Equal(t, status.SyncPhaseComplete, finalStatus.Phase, "Phase should remain Complete")
	assert.Contains(t, finalStatus.Message, reason, "Message should include skip reason")

	// Verify status was persisted
	loadedStatus, err := statusPersistence.LoadStatus(context.Background(), registryName)
	require.NoError(t, err)
	assert.Equal(t, status.SyncPhaseComplete, loadedStatus.Phase)
}

func TestCheckRegistrySync_PerformsSyncWhenNeeded(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	tempDir := t.TempDir()
	mockSyncMgr := syncmocks.NewMockManager(ctrl)

	registryName := testRegistryName
	cfg := &config.Config{
		RegistryName: "global-registry",
		Registries: []config.RegistryConfig{
			{Name: registryName},
		},
	}
	regCfg := &cfg.Registries[0]

	// Mock ShouldSync to return true
	mockSyncMgr.EXPECT().
		ShouldSync(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(true, sync.ReasonSourceDataChanged, (*time.Time)(nil))

	// Mock PerformSync
	mockSyncMgr.EXPECT().
		PerformSync(gomock.Any(), gomock.Any()).
		Return(&sync.Result{
			Hash:        "new-hash",
			ServerCount: 20,
		}, nil)

	statusPersistence := status.NewFileStatusPersistence(tempDir)

	coord := &defaultCoordinator{
		manager:           mockSyncMgr,
		statusPersistence: statusPersistence,
		config:            cfg,
		cachedStatuses: map[string]*status.SyncStatus{
			registryName: {
				Phase: status.SyncPhaseComplete,
			},
		},
	}

	// Execute checkRegistrySync
	coord.checkRegistrySync(context.Background(), regCfg, "automatic")

	// Verify sync was performed
	finalStatus := coord.cachedStatuses[registryName]
	assert.Equal(t, status.SyncPhaseComplete, finalStatus.Phase)
	assert.Equal(t, "new-hash", finalStatus.LastSyncHash)
}

func TestCheckRegistrySync_SkipsSyncWhenNotNeeded(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	tempDir := t.TempDir()
	mockSyncMgr := syncmocks.NewMockManager(ctrl)

	registryName := testRegistryName
	cfg := &config.Config{
		RegistryName: "global-registry",
		Registries: []config.RegistryConfig{
			{Name: registryName},
		},
	}
	regCfg := &cfg.Registries[0]

	// Mock ShouldSync to return false
	mockSyncMgr.EXPECT().
		ShouldSync(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(false, sync.ReasonUpToDateWithPolicy, (*time.Time)(nil))

	// PerformSync should NOT be called
	mockSyncMgr.EXPECT().
		PerformSync(gomock.Any(), gomock.Any()).
		Times(0)

	statusPersistence := status.NewFileStatusPersistence(tempDir)

	coord := &defaultCoordinator{
		manager:           mockSyncMgr,
		statusPersistence: statusPersistence,
		config:            cfg,
		cachedStatuses: map[string]*status.SyncStatus{
			registryName: {
				Phase:        status.SyncPhaseComplete,
				LastSyncHash: "existing-hash",
			},
		},
	}

	// Execute checkRegistrySync
	coord.checkRegistrySync(context.Background(), regCfg, "automatic")

	// Verify sync was NOT performed (hash unchanged)
	finalStatus := coord.cachedStatuses[registryName]
	assert.Equal(t, status.SyncPhaseComplete, finalStatus.Phase)
	assert.Equal(t, "existing-hash", finalStatus.LastSyncHash)
}

func TestLoadOrInitializeRegistryStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		setupFile       bool
		existingPhase   status.SyncPhase
		expectedPhase   status.SyncPhase
		expectedMessage string
	}{
		{
			name:            "loads existing status from file",
			setupFile:       true,
			existingPhase:   status.SyncPhaseComplete,
			expectedPhase:   status.SyncPhaseComplete,
			expectedMessage: "Previous sync",
		},
		{
			name:            "initializes new status when file doesn't exist",
			setupFile:       false,
			expectedPhase:   status.SyncPhaseFailed,
			expectedMessage: "No previous sync status found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			tempDir := t.TempDir()
			registryName := testRegistryName

			statusPersistence := status.NewFileStatusPersistence(tempDir)

			// Setup: Create existing status file if needed
			if tt.setupFile {
				existingStatus := &status.SyncStatus{
					Phase:   tt.existingPhase,
					Message: tt.expectedMessage,
				}
				err := statusPersistence.SaveStatus(context.Background(), registryName, existingStatus)
				require.NoError(t, err)
			}

			mockSyncMgr := syncmocks.NewMockManager(ctrl)
			cfg := &config.Config{
				RegistryName: "global-registry",
				Registries: []config.RegistryConfig{
					{Name: registryName},
				},
			}

			coord := &defaultCoordinator{
				manager:           mockSyncMgr,
				statusPersistence: statusPersistence,
				config:            cfg,
				cachedStatuses:    make(map[string]*status.SyncStatus),
			}

			// Execute
			coord.loadOrInitializeRegistryStatus(context.Background(), &cfg.Registries[0])

			// Verify
			loadedStatus := coord.cachedStatuses[registryName]
			require.NotNil(t, loadedStatus)
			assert.Equal(t, tt.expectedPhase, loadedStatus.Phase)
			assert.Contains(t, loadedStatus.Message, tt.expectedMessage)
		})
	}
}

func TestMultiRegistry_IndependentSyncStatus(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	tempDir := t.TempDir()
	mockSyncMgr := syncmocks.NewMockManager(ctrl)
	statusPersistence := status.NewFileStatusPersistence(tempDir)

	// Setup two registries
	registry1 := "registry-1"
	registry2 := "registry-2"

	cfg := &config.Config{
		RegistryName: "global-registry",
		Registries: []config.RegistryConfig{
			{Name: registry1},
			{Name: registry2},
		},
	}

	coord := &defaultCoordinator{
		manager:           mockSyncMgr,
		statusPersistence: statusPersistence,
		config:            cfg,
		cachedStatuses: map[string]*status.SyncStatus{
			registry1: {
				Phase:        status.SyncPhaseComplete,
				LastSyncHash: "hash-1",
				ServerCount:  10,
			},
			registry2: {
				Phase:        status.SyncPhaseFailed,
				LastSyncHash: "",
				ServerCount:  0,
			},
		},
	}

	// Get individual statuses
	status1 := coord.GetStatus(registry1)
	status2 := coord.GetStatus(registry2)

	// Verify they are independent
	assert.Equal(t, status.SyncPhaseComplete, status1.Phase)
	assert.Equal(t, "hash-1", status1.LastSyncHash)
	assert.Equal(t, 10, status1.ServerCount)

	assert.Equal(t, status.SyncPhaseFailed, status2.Phase)
	assert.Empty(t, status2.LastSyncHash)
	assert.Equal(t, 0, status2.ServerCount)

	// Update registry-2
	mockSyncMgr.EXPECT().
		PerformSync(gomock.Any(), gomock.Any()).
		Return(&sync.Result{
			Hash:        "hash-2",
			ServerCount: 25,
		}, nil)

	coord.performRegistrySync(context.Background(), &cfg.Registries[1])

	// Verify registry-1 is unchanged
	status1After := coord.GetStatus(registry1)
	assert.Equal(t, status.SyncPhaseComplete, status1After.Phase)
	assert.Equal(t, "hash-1", status1After.LastSyncHash)

	// Verify registry-2 is updated
	status2After := coord.GetStatus(registry2)
	assert.Equal(t, status.SyncPhaseComplete, status2After.Phase)
	assert.Equal(t, "hash-2", status2After.LastSyncHash)
	assert.Equal(t, 25, status2After.ServerCount)
}

func TestManagedRegistry_StatusInitialization(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	tempDir := t.TempDir()
	mockSyncMgr := syncmocks.NewMockManager(ctrl)
	statusPersistence := status.NewFileStatusPersistence(tempDir)

	// Create config with one managed registry
	cfg := &config.Config{
		RegistryName: "global-registry",
		Registries: []config.RegistryConfig{
			{
				Name:    "managed-registry",
				Managed: &config.ManagedConfig{},
			},
		},
	}

	coord := &defaultCoordinator{
		manager:           mockSyncMgr,
		statusPersistence: statusPersistence,
		config:            cfg,
		cachedStatuses:    make(map[string]*status.SyncStatus),
	}

	// Initialize status for managed registry
	coord.loadOrInitializeRegistryStatus(context.Background(), &cfg.Registries[0])

	// Verify managed registry gets Complete status (not Failed)
	managedStatus := coord.cachedStatuses["managed-registry"]
	require.NotNil(t, managedStatus)
	assert.Equal(t, status.SyncPhaseComplete, managedStatus.Phase)
	assert.Contains(t, managedStatus.Message, "Managed registry")
	assert.Contains(t, managedStatus.Message, "data managed via API")
}

func TestManagedRegistry_MixedWithSyncedRegistries(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	tempDir := t.TempDir()
	mockSyncMgr := syncmocks.NewMockManager(ctrl)
	statusPersistence := status.NewFileStatusPersistence(tempDir)

	// Create config with both managed and synced registries
	cfg := &config.Config{
		RegistryName: "global-registry",
		Registries: []config.RegistryConfig{
			{
				Name: "file-registry",
				File: &config.FileConfig{
					Path: "/data/registry.json",
				},
				SyncPolicy: &config.SyncPolicyConfig{
					Interval: "30m",
				},
			},
			{
				Name:    "managed-registry-1",
				Managed: &config.ManagedConfig{},
			},
			{
				Name: "git-registry",
				Git: &config.GitConfig{
					Repository: "https://github.com/example/repo.git",
				},
				SyncPolicy: &config.SyncPolicyConfig{
					Interval: "1h",
				},
			},
			{
				Name:    "managed-registry-2",
				Managed: &config.ManagedConfig{},
			},
		},
	}

	coord := &defaultCoordinator{
		manager:           mockSyncMgr,
		statusPersistence: statusPersistence,
		config:            cfg,
		cachedStatuses:    make(map[string]*status.SyncStatus),
	}

	// Initialize all statuses
	coord.loadOrInitializeAllStatus(context.Background())

	// Verify all statuses exist
	allStatuses := coord.GetAllStatus()
	require.Len(t, allStatuses, 4)

	// Verify managed registries have Complete status
	managedStatus1 := coord.GetStatus("managed-registry-1")
	require.NotNil(t, managedStatus1)
	assert.Equal(t, status.SyncPhaseComplete, managedStatus1.Phase)
	assert.Contains(t, managedStatus1.Message, "Managed registry")

	managedStatus2 := coord.GetStatus("managed-registry-2")
	require.NotNil(t, managedStatus2)
	assert.Equal(t, status.SyncPhaseComplete, managedStatus2.Phase)
	assert.Contains(t, managedStatus2.Message, "Managed registry")

	// Verify synced registries have Failed status (not yet synced)
	fileStatus := coord.GetStatus("file-registry")
	require.NotNil(t, fileStatus)
	assert.Equal(t, status.SyncPhaseFailed, fileStatus.Phase)

	gitStatus := coord.GetStatus("git-registry")
	require.NotNil(t, gitStatus)
	assert.Equal(t, status.SyncPhaseFailed, gitStatus.Phase)
}
