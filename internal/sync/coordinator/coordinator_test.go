package coordinator

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	syncmocks "github.com/stacklok/toolhive-registry-server/internal/sync/mocks"
	statemocks "github.com/stacklok/toolhive-registry-server/internal/sync/state/mocks"
)

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

func TestCalculatePollingInterval(t *testing.T) {
	t.Parallel()

	// Run multiple iterations to verify jitter is within expected range
	for i := 0; i < 100; i++ {
		interval := calculatePollingInterval()

		// Verify interval is within basePollingInterval ± pollingJitter
		minInterval := basePollingInterval - pollingJitter
		maxInterval := basePollingInterval + pollingJitter

		assert.GreaterOrEqual(t, interval, minInterval,
			"Interval should be >= base - jitter")
		assert.LessOrEqual(t, interval, maxInterval,
			"Interval should be <= base + jitter")
	}
}
