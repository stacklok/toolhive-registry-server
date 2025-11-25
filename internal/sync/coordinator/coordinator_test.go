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
	"github.com/stacklok/toolhive-registry-server/internal/sync"
	syncmocks "github.com/stacklok/toolhive-registry-server/internal/sync/mocks"
	statemocks "github.com/stacklok/toolhive-registry-server/internal/sync/state/mocks"
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
	mockStateSvc := statemocks.NewMockRegistryStateService(ctrl)
	cfg := &config.Config{
		RegistryName: "test-registry",
		Registries: []config.RegistryConfig{
			{Name: "registry-1"},
		},
	}

	coordinator := New(mockManager, mockStateSvc, cfg)

	require.NotNil(t, coordinator)
}

func TestCoordinator_Stop_BeforeStart(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockManager := syncmocks.NewMockManager(ctrl)
	mockStateSvc := statemocks.NewMockRegistryStateService(ctrl)
	cfg := &config.Config{
		RegistryName: "test-registry",
		Registries:   []config.RegistryConfig{},
	}

	coordinator := New(mockManager, mockStateSvc, cfg)

	// Stop should not panic if called before Start
	err := coordinator.Stop()
	assert.NoError(t, err)
}

func TestCheckRegistrySync_NotReadyToSync(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockManager := syncmocks.NewMockManager(ctrl)
	mockStateSvc := statemocks.NewMockRegistryStateService(ctrl)

	registryName := testRegistryName
	cfg := &config.Config{
		RegistryName: "global-registry",
		Registries: []config.RegistryConfig{
			{Name: registryName},
		},
	}
	regCfg := &cfg.Registries[0]

	// Mock UpdateStatusAtomically - the callback will call ShouldSync which returns false
	mockStateSvc.EXPECT().
		UpdateStatusAtomically(gomock.Any(), registryName, gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, fn func(*status.SyncStatus) bool) (bool, error) {
			// Call the callback with a mock status
			testStatus := &status.SyncStatus{
				Phase: status.SyncPhaseComplete,
			}

			// Mock ShouldSync returning false (not ready to sync)
			mockManager.EXPECT().
				ShouldSync(gomock.Any(), regCfg, testStatus, false).
				Return(sync.ReasonUpToDateWithPolicy)

			result := fn(testStatus)
			return result, nil
		})

	// PerformSync should NOT be called since we're not ready to sync
	mockManager.EXPECT().
		PerformSync(gomock.Any(), gomock.Any()).
		Times(0)

	coord := &defaultCoordinator{
		manager:   mockManager,
		config:    cfg,
		statusSvc: mockStateSvc,
	}

	// Execute checkRegistrySync
	coord.checkRegistrySync(context.Background(), regCfg, "periodic")
}

func TestCheckRegistrySync_SuccessfulSync(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockManager := syncmocks.NewMockManager(ctrl)
	mockStateSvc := statemocks.NewMockRegistryStateService(ctrl)

	registryName := testRegistryName
	cfg := &config.Config{
		RegistryName: "global-registry",
		Registries: []config.RegistryConfig{
			{Name: registryName},
		},
	}
	regCfg := &cfg.Registries[0]

	// Mock UpdateStatusAtomically - the callback will call ShouldSync which returns true
	mockStateSvc.EXPECT().
		UpdateStatusAtomically(gomock.Any(), registryName, gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, fn func(*status.SyncStatus) bool) (bool, error) {
			// Call the callback with a mock status
			testStatus := &status.SyncStatus{
				Phase: status.SyncPhaseComplete,
			}

			// Mock ShouldSync returning true (ready to sync)
			mockManager.EXPECT().
				ShouldSync(gomock.Any(), regCfg, testStatus, false).
				Return(sync.ReasonSourceDataChanged)

			result := fn(testStatus)

			// Verify the callback updated the status to Syncing
			assert.Equal(t, status.SyncPhaseSyncing, testStatus.Phase)
			assert.Equal(t, "Sync in progress", testStatus.Message)

			return result, nil
		})

	// Mock PerformSync returning success
	mockManager.EXPECT().
		PerformSync(gomock.Any(), regCfg).
		Return(&sync.Result{
			Hash:        "test-hash-123",
			ServerCount: 42,
		}, nil)

	// Mock UpdateSyncStatus for final status (in defer)
	mockStateSvc.EXPECT().
		UpdateSyncStatus(gomock.Any(), registryName, gomock.Any()).
		Do(func(_ context.Context, _ string, syncStatus *status.SyncStatus) {
			assert.Equal(t, status.SyncPhaseComplete, syncStatus.Phase)
			assert.Equal(t, "Sync completed successfully", syncStatus.Message)
			assert.Equal(t, "test-hash-123", syncStatus.LastSyncHash)
			assert.Equal(t, 42, syncStatus.ServerCount)
			assert.NotNil(t, syncStatus.LastSyncTime)
		})

	coord := &defaultCoordinator{
		manager:   mockManager,
		config:    cfg,
		statusSvc: mockStateSvc,
	}

	// Execute checkRegistrySync
	coord.checkRegistrySync(context.Background(), regCfg, "periodic")
}

func TestCheckRegistrySync_FailedSync(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockManager := syncmocks.NewMockManager(ctrl)
	mockStateSvc := statemocks.NewMockRegistryStateService(ctrl)

	registryName := testRegistryName
	cfg := &config.Config{
		RegistryName: "global-registry",
		Registries: []config.RegistryConfig{
			{Name: registryName},
		},
	}
	regCfg := &cfg.Registries[0]

	// Mock UpdateStatusAtomically - the callback will call ShouldSync which returns true
	mockStateSvc.EXPECT().
		UpdateStatusAtomically(gomock.Any(), registryName, gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, fn func(*status.SyncStatus) bool) (bool, error) {
			testStatus := &status.SyncStatus{
				Phase: status.SyncPhaseComplete,
			}

			mockManager.EXPECT().
				ShouldSync(gomock.Any(), regCfg, testStatus, false).
				Return(sync.ReasonSourceDataChanged)

			result := fn(testStatus)
			assert.Equal(t, status.SyncPhaseSyncing, testStatus.Phase)

			return result, nil
		})

	// Mock PerformSync returning error
	mockManager.EXPECT().
		PerformSync(gomock.Any(), regCfg).
		Return(nil, &sync.Error{
			Message: "sync failed due to network error",
		})

	// Mock UpdateSyncStatus for final failed status (in defer)
	mockStateSvc.EXPECT().
		UpdateSyncStatus(gomock.Any(), registryName, gomock.Any()).
		Do(func(_ context.Context, _ string, syncStatus *status.SyncStatus) {
			assert.Equal(t, status.SyncPhaseFailed, syncStatus.Phase)
			assert.Equal(t, "sync failed due to network error", syncStatus.Message)
		})

	coord := &defaultCoordinator{
		manager:   mockManager,
		config:    cfg,
		statusSvc: mockStateSvc,
	}

	// Execute checkRegistrySync
	coord.checkRegistrySync(context.Background(), regCfg, "periodic")
}

func TestCheckRegistrySync_AlwaysUpdatesFinalStatus(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockManager := syncmocks.NewMockManager(ctrl)
	mockStateSvc := statemocks.NewMockRegistryStateService(ctrl)

	registryName := testRegistryName
	cfg := &config.Config{
		RegistryName: "global-registry",
		Registries: []config.RegistryConfig{
			{Name: registryName},
		},
	}
	regCfg := &cfg.Registries[0]

	// Mock UpdateStatusAtomically
	mockStateSvc.EXPECT().
		UpdateStatusAtomically(gomock.Any(), registryName, gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, fn func(*status.SyncStatus) bool) (bool, error) {
			testStatus := &status.SyncStatus{Phase: status.SyncPhaseComplete}

			mockManager.EXPECT().
				ShouldSync(gomock.Any(), regCfg, testStatus, false).
				Return(sync.ReasonSourceDataChanged)

			return fn(testStatus), nil
		})

	// Mock PerformSync
	mockManager.EXPECT().
		PerformSync(gomock.Any(), regCfg).
		Return(&sync.Result{
			Hash:        "test-hash",
			ServerCount: 10,
		}, nil)

	// Mock UpdateSyncStatus - this should always be called via defer
	mockStateSvc.EXPECT().
		UpdateSyncStatus(gomock.Any(), registryName, gomock.Any()).
		Return(nil)

	coord := &defaultCoordinator{
		manager:   mockManager,
		config:    cfg,
		statusSvc: mockStateSvc,
	}

	// Execute checkRegistrySync
	coord.checkRegistrySync(context.Background(), regCfg, "periodic")
}

func TestStart_InitializesStateService(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockManager := syncmocks.NewMockManager(ctrl)
	mockStateSvc := statemocks.NewMockRegistryStateService(ctrl)

	registryName := testRegistryName
	cfg := &config.Config{
		RegistryName: "global-registry",
		Registries: []config.RegistryConfig{
			{Name: registryName},
		},
	}

	// Mock Initialize being called
	mockStateSvc.EXPECT().
		Initialize(gomock.Any(), cfg.Registries).
		Return(nil)

	// Mock UpdateStatusAtomically - may be called during initial sync check
	// Allow any number of calls since we're cancelling quickly
	mockStateSvc.EXPECT().
		UpdateStatusAtomically(gomock.Any(), registryName, gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, fn func(*status.SyncStatus) bool) (bool, error) {
			testStatus := &status.SyncStatus{Phase: status.SyncPhaseComplete}
			mockManager.EXPECT().
				ShouldSync(gomock.Any(), gomock.Any(), testStatus, false).
				Return(sync.ReasonUpToDateWithPolicy).
				AnyTimes()
			return fn(testStatus), nil
		}).
		AnyTimes()

	coord := &defaultCoordinator{
		manager:       mockManager,
		config:        cfg,
		statusSvc:     mockStateSvc,
		registrySyncs: make(map[string]*registrySync),
		done:          make(chan struct{}),
	}

	// Start in a goroutine and cancel immediately
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately to exit Start quickly

	err := coord.Start(ctx)

	// Should return nil when context is cancelled
	assert.NoError(t, err)
}

func TestStart_SkipsManagedRegistries(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockManager := syncmocks.NewMockManager(ctrl)
	mockStateSvc := statemocks.NewMockRegistryStateService(ctrl)

	cfg := &config.Config{
		RegistryName: "global-registry",
		Registries: []config.RegistryConfig{
			{Name: "managed-registry", Managed: &config.ManagedConfig{}},
		},
	}

	// Mock Initialize being called
	mockStateSvc.EXPECT().
		Initialize(gomock.Any(), cfg.Registries).
		Return(nil)

	// UpdateStatusAtomically should NOT be called for managed registries
	// since no sync loop is started

	coord := &defaultCoordinator{
		manager:       mockManager,
		config:        cfg,
		statusSvc:     mockStateSvc,
		registrySyncs: make(map[string]*registrySync),
		done:          make(chan struct{}),
	}

	// Start and cancel immediately
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := coord.Start(ctx)
	assert.NoError(t, err)

	// Verify no sync loop was started for the managed registry
	assert.Empty(t, coord.registrySyncs)
}

func TestStart_MixedManagedAndNonManagedRegistries(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockManager := syncmocks.NewMockManager(ctrl)
	mockStateSvc := statemocks.NewMockRegistryStateService(ctrl)

	gitRegistry := "git-registry"
	managedRegistry := "managed-registry"
	cfg := &config.Config{
		RegistryName: "global-registry",
		Registries: []config.RegistryConfig{
			{Name: gitRegistry, Git: &config.GitConfig{Repository: "https://example.com"}},
			{Name: managedRegistry, Managed: &config.ManagedConfig{}},
		},
	}

	// Mock Initialize being called
	mockStateSvc.EXPECT().
		Initialize(gomock.Any(), cfg.Registries).
		Return(nil)

	// Only the git registry should have sync checks
	mockStateSvc.EXPECT().
		UpdateStatusAtomically(gomock.Any(), gitRegistry, gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, fn func(*status.SyncStatus) bool) (bool, error) {
			testStatus := &status.SyncStatus{Phase: status.SyncPhaseComplete}
			mockManager.EXPECT().
				ShouldSync(gomock.Any(), gomock.Any(), testStatus, false).
				Return(sync.ReasonUpToDateWithPolicy).
				AnyTimes()
			return fn(testStatus), nil
		}).
		AnyTimes()

	// No sync checks for managed registry

	coord := &defaultCoordinator{
		manager:       mockManager,
		config:        cfg,
		statusSvc:     mockStateSvc,
		registrySyncs: make(map[string]*registrySync),
		done:          make(chan struct{}),
	}

	// Start and cancel quickly
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	err := coord.Start(ctx)
	assert.NoError(t, err)

	// Verify only the git registry has a sync loop
	coord.mu.RLock()
	defer coord.mu.RUnlock()
	assert.Contains(t, coord.registrySyncs, gitRegistry)
	assert.NotContains(t, coord.registrySyncs, managedRegistry)
}

func TestRunRegistrySync_PerformsInitialAndPeriodicSync(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockManager := syncmocks.NewMockManager(ctrl)
	mockStateSvc := statemocks.NewMockRegistryStateService(ctrl)

	registryName := testRegistryName
	cfg := &config.Config{
		RegistryName: "global-registry",
		Registries: []config.RegistryConfig{
			{
				Name: registryName,
				SyncPolicy: &config.SyncPolicyConfig{
					Interval: "10ms", // Very short interval for testing
				},
			},
		},
	}
	regCfg := &cfg.Registries[0]

	// We expect at least 2 sync checks: initial + at least one periodic
	// Mock UpdateStatusAtomically to return false (not ready) so sync is skipped
	mockStateSvc.EXPECT().
		UpdateStatusAtomically(gomock.Any(), registryName, gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, fn func(*status.SyncStatus) bool) (bool, error) {
			testStatus := &status.SyncStatus{Phase: status.SyncPhaseComplete}
			mockManager.EXPECT().
				ShouldSync(gomock.Any(), regCfg, testStatus, false).
				Return(sync.ReasonUpToDateWithPolicy)
			return fn(testStatus), nil
		}).
		MinTimes(2)

	coord := &defaultCoordinator{
		manager:   mockManager,
		config:    cfg,
		statusSvc: mockStateSvc,
	}

	// Run sync loop with a context that times out
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	coord.runRegistrySync(ctx, regCfg)

	// If we reach here, the sync loop ran and stopped correctly
}

func TestStartRegistrySync_CreatesRegistrySyncEntry(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockManager := syncmocks.NewMockManager(ctrl)
	mockStateSvc := statemocks.NewMockRegistryStateService(ctrl)

	registryName := testRegistryName
	cfg := &config.Config{
		RegistryName: "global-registry",
		Registries: []config.RegistryConfig{
			{Name: registryName},
		},
	}
	regCfg := &cfg.Registries[0]

	// Mock state service to skip syncs
	mockStateSvc.EXPECT().
		UpdateStatusAtomically(gomock.Any(), registryName, gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, fn func(*status.SyncStatus) bool) (bool, error) {
			testStatus := &status.SyncStatus{Phase: status.SyncPhaseComplete}
			mockManager.EXPECT().
				ShouldSync(gomock.Any(), gomock.Any(), testStatus, false).
				Return(sync.ReasonUpToDateWithPolicy).
				AnyTimes()
			return fn(testStatus), nil
		}).
		AnyTimes()

	coord := &defaultCoordinator{
		manager:       mockManager,
		config:        cfg,
		statusSvc:     mockStateSvc,
		registrySyncs: make(map[string]*registrySync),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start registry sync
	coord.startRegistrySync(ctx, regCfg)

	// Give goroutine time to start
	time.Sleep(10 * time.Millisecond)

	// Verify registry sync entry was created
	coord.mu.RLock()
	regSync, exists := coord.registrySyncs[registryName]
	coord.mu.RUnlock()

	assert.True(t, exists)
	assert.NotNil(t, regSync)
	assert.Equal(t, regCfg, regSync.config)
	assert.NotNil(t, regSync.cancelFunc)
	assert.NotNil(t, regSync.done)

	// Cancel and wait for cleanup
	cancel()
	<-regSync.done
}
