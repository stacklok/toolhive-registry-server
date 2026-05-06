package coordinator

import (
	"context"
	"sort"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/sources"
	"github.com/stacklok/toolhive-registry-server/internal/status"
	pkgsync "github.com/stacklok/toolhive-registry-server/internal/sync"
	syncmocks "github.com/stacklok/toolhive-registry-server/internal/sync/mocks"
	"github.com/stacklok/toolhive-registry-server/internal/sync/state"
	statemocks "github.com/stacklok/toolhive-registry-server/internal/sync/state/mocks"
)

func TestCoordinator_New(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockManager := syncmocks.NewMockManager(ctrl)
	mockStateSvc := statemocks.NewMockRegistryStateService(ctrl)
	cfg := &config.Config{
		Sources: []config.SourceConfig{
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
		Sources: []config.SourceConfig{},
	}

	coordinator := New(mockManager, mockStateSvc, cfg)

	// Stop should not panic if called before Start
	err := coordinator.Stop()
	assert.NoError(t, err)
}

// TestPerformRegistrySync_FailureAdvancesEndedAt is a regression test for a
// coordinator starvation bug observed in v1.4.0 staging: when a syncable source
// failed every attempt, only the success branch of performRegistrySync set
// LastSyncTime. That field is persisted as registry_sync.ended_at, and
// ListSourceSyncsByLastUpdate orders by `ended_at ASC NULLS FIRST LIMIT 1`, so
// the failing source perpetually sorted first and starved every other syncable
// source. The fix moves the LastSyncTime assignment to run on both branches.
func TestPerformRegistrySync_FailureAdvancesEndedAt(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockManager := syncmocks.NewMockManager(ctrl)
	mockStateSvc := statemocks.NewMockRegistryStateService(ctrl)

	regCfg := &config.SourceConfig{Name: "broken-source"}
	cfg := &config.Config{Sources: []config.SourceConfig{*regCfg}}

	syncErr := &pkgsync.Error{Message: "fetch failed"}
	mockManager.EXPECT().
		PerformSync(gomock.Any(), regCfg, gomock.Nil()).
		Return(nil, syncErr)

	var captured *status.SyncStatus
	mockStateSvc.EXPECT().
		UpdateSyncStatus(gomock.Any(), "broken-source", gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, s *status.SyncStatus) error {
			captured = s
			return nil
		})

	c := New(mockManager, mockStateSvc, cfg).(*defaultCoordinator)
	c.performRegistrySync(context.Background(), regCfg, nil)

	require.NotNil(t, captured, "UpdateSyncStatus must be called even on failure")
	assert.Equal(t, status.SyncPhaseFailed, captured.Phase)
	assert.Equal(t, "fetch failed", captured.Message)
	require.NotNil(t, captured.LastSyncTime,
		"LastSyncTime must be set on failure so registry_sync.ended_at advances "+
			"and the failing source does not starve other syncable sources")
}

// TestProcessNextSyncJob_FailingSourceDoesNotStarveOthers is the higher-level
// regression test for the same bug: simulate two syncable sources, one of which
// always fails, against an in-memory state service that mirrors the real DB
// ordering (`ended_at ASC NULLS FIRST, name ASC`), and assert that the healthy
// source is picked by the second tick.
func TestProcessNextSyncJob_FailingSourceDoesNotStarveOthers(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockManager := syncmocks.NewMockManager(ctrl)

	// Both sources are syncable. Names are chosen so the alphabetical
	// tiebreaker picks "alpha" first when both have ended_at = NULL.
	alpha := &config.SourceConfig{Name: "alpha", Git: &config.GitConfig{Repository: "https://example.invalid/broken.git"}}
	beta := &config.SourceConfig{Name: "beta", Git: &config.GitConfig{Repository: "https://example.invalid/good.git"}}

	fakeState := newFakeStateService(alpha, beta)

	cfg := &config.Config{Sources: []config.SourceConfig{*alpha, *beta}}

	// Both sources always need syncing.
	mockManager.EXPECT().
		ShouldSync(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(pkgsync.ReasonRegistryNotReady, (*sources.FetchResult)(nil)).
		AnyTimes()

	// alpha always fails; beta always succeeds.
	mockManager.EXPECT().
		PerformSync(gomock.Any(), alpha, gomock.Any()).
		Return(nil, &pkgsync.Error{Message: "broken upstream"}).
		AnyTimes()
	mockManager.EXPECT().
		PerformSync(gomock.Any(), beta, gomock.Any()).
		Return(&pkgsync.Result{Hash: "h", ServerCount: 1, SkillCount: 0}, nil).
		AnyTimes()

	c := New(mockManager, fakeState, cfg).(*defaultCoordinator)

	const maxTicks = 5
	var betaPickedTick int
	for tick := 1; tick <= maxTicks; tick++ {
		c.processNextSyncJob(context.Background())
		if fakeState.lastPicked() == "beta" {
			betaPickedTick = tick
			break
		}
	}

	require.NotZero(t, betaPickedTick,
		"beta was never selected within %d ticks — failing source starved the scheduler", maxTicks)
	assert.LessOrEqual(t, betaPickedTick, 2,
		"beta should be selected by the second tick once alpha's ended_at is advanced")
}

// Compile-time assertion that fakeStateService satisfies the interface.
var _ state.RegistryStateService = (*fakeStateService)(nil)

// fakeStateService is a minimal in-memory state service that mirrors the
// production `ListSourceSyncsByLastUpdate` ordering: pick the syncable source
// with the earliest `ended_at` (NULLS FIRST), tiebreaker on source name.
// It is *only* used for testing the coordinator's tick behaviour — it does not
// implement Initialize, persistence, or anything else not needed here.
type fakeStateService struct {
	mu       sync.Mutex
	configs  map[string]*config.SourceConfig
	statuses map[string]*status.SyncStatus
	picked   string
}

func newFakeStateService(srcs ...*config.SourceConfig) *fakeStateService {
	f := &fakeStateService{
		configs:  make(map[string]*config.SourceConfig, len(srcs)),
		statuses: make(map[string]*status.SyncStatus, len(srcs)),
	}
	for _, s := range srcs {
		f.configs[s.Name] = s
		// Initial state mirrors BulkInitializeSourceSyncs: ended_at NULL.
		f.statuses[s.Name] = &status.SyncStatus{Phase: status.SyncPhaseFailed}
	}
	return f
}

func (f *fakeStateService) lastPicked() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.picked
}

func (*fakeStateService) Initialize(_ context.Context, _ *config.Config) error {
	return nil
}

func (f *fakeStateService) ListSyncStatuses(_ context.Context) (map[string]*status.SyncStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make(map[string]*status.SyncStatus, len(f.statuses))
	for k, v := range f.statuses {
		dup := *v
		out[k] = &dup
	}
	return out, nil
}

func (f *fakeStateService) GetSyncStatus(_ context.Context, name string) (*status.SyncStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.statuses[name]
	if !ok {
		return nil, state.ErrRegistryNotFound
	}
	dup := *s
	return &dup, nil
}

func (f *fakeStateService) UpdateSyncStatus(_ context.Context, name string, s *status.SyncStatus) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	dup := *s
	f.statuses[name] = &dup
	return nil
}

func (f *fakeStateService) GetNextSyncJob(
	_ context.Context,
	predicate func(*config.SourceConfig, *status.SyncStatus) bool,
) (*config.SourceConfig, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Mirror `ORDER BY ended_at ASC NULLS FIRST, name ASC` from
	// database/queries/sync.sql::ListSourceSyncsByLastUpdate.
	names := make([]string, 0, len(f.configs))
	for n := range f.configs {
		names = append(names, n)
	}
	sort.SliceStable(names, func(i, j int) bool {
		ai := f.statuses[names[i]].LastSyncTime
		aj := f.statuses[names[j]].LastSyncTime
		switch {
		case ai == nil && aj != nil:
			return true
		case ai != nil && aj == nil:
			return false
		case ai != nil && aj != nil && !ai.Equal(*aj):
			return ai.Before(*aj)
		default:
			return names[i] < names[j]
		}
	})

	for _, n := range names {
		cfg := f.configs[n]
		if cfg.IsNonSyncedSource() {
			continue
		}
		s := f.statuses[n]
		dup := *s
		if predicate(cfg, &dup) {
			f.picked = n
			return cfg, nil
		}
	}
	return nil, nil
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
