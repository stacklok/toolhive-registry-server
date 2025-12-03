package coordinator

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	syncmocks "github.com/stacklok/toolhive-registry-server/internal/sync/mocks"
	statemocks "github.com/stacklok/toolhive-registry-server/internal/sync/state/mocks"
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

func TestGetCoordinatorInterval(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		registries []config.RegistryConfig
		expected   time.Duration
	}{
		{
			name:       "no registries returns default",
			registries: []config.RegistryConfig{},
			expected:   time.Minute,
		},
		{
			name: "single registry with interval",
			registries: []config.RegistryConfig{
				{
					Name: "reg1",
					Git:  &config.GitConfig{},
					SyncPolicy: &config.SyncPolicyConfig{
						Interval: "5m",
					},
				},
			},
			expected: 5 * time.Minute,
		},
		{
			name: "multiple registries uses minimum",
			registries: []config.RegistryConfig{
				{
					Name: "reg1",
					Git:  &config.GitConfig{},
					SyncPolicy: &config.SyncPolicyConfig{
						Interval: "10m",
					},
				},
				{
					Name: "reg2",
					API:  &config.APIConfig{},
					SyncPolicy: &config.SyncPolicyConfig{
						Interval: "3m",
					},
				},
			},
			expected: 3 * time.Minute,
		},
		{
			name: "skips managed registries",
			registries: []config.RegistryConfig{
				{
					Name:    "managed",
					Managed: &config.ManagedConfig{},
					SyncPolicy: &config.SyncPolicyConfig{
						Interval: "1m",
					},
				},
				{
					Name: "git",
					Git:  &config.GitConfig{},
					SyncPolicy: &config.SyncPolicyConfig{
						Interval: "5m",
					},
				},
			},
			expected: 5 * time.Minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockManager := syncmocks.NewMockManager(ctrl)
			mockStateSvc := statemocks.NewMockRegistryStateService(ctrl)
			cfg := &config.Config{
				Registries: tt.registries,
			}

			coord := &defaultCoordinator{
				manager:   mockManager,
				config:    cfg,
				statusSvc: mockStateSvc,
				done:      make(chan struct{}),
			}

			result := coord.getCoordinatorInterval()
			assert.Equal(t, tt.expected, result)
		})
	}
}
