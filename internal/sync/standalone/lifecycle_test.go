package standalone

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/reconcile"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/reconcile/mocks"
)

type lifecycleSources struct {
	list func(context.Context) ([]persistence.SourceDefinition, error)
}

func (s lifecycleSources) ListSources(ctx context.Context) ([]persistence.SourceDefinition, error) {
	return s.list(ctx)
}

type snapshotContext struct {
	context.Context
	captured chan struct{}
	release  chan struct{}
	once     sync.Once
}

func (c *snapshotContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.captured); <-c.release })
	return c.Context.Done()
}

func lifecycleResult(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(3 * time.Second):
		t.Fatal("lifecycle operation did not finish")
		return nil
	}
}

func lifecycleBarrier(t *testing.T, reached <-chan struct{}) {
	t.Helper()
	select {
	case <-reached:
	case <-time.After(3 * time.Second):
		t.Fatal("lifecycle barrier not reached")
	}
}

type lifecycleWorker struct{ started chan<- struct{} }

func (lifecycleWorker) Ready() bool { return true }

func (w lifecycleWorker) Run(ctx context.Context) error {
	w.started <- struct{}{}
	<-ctx.Done()
	return nil
}

func TestDiscoveryCompletionIgnoresNilWorkerAfterLifetimeCancellation(t *testing.T) {
	t.Parallel()
	listed, release := make(chan struct{}), make(chan struct{})
	r := &Runtime{
		sources: lifecycleSources{list: func(context.Context) ([]persistence.SourceDefinition, error) {
			close(listed)
			<-release
			return nil, nil
		}},
		workers: map[string]*owned{"worker": {done: make(chan error, 1)}},
	}
	lifetime, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := r.discover(t.Context(), lifetime, false, nil, func() {})
		result <- err
	}()
	lifecycleBarrier(t, listed)
	cancel()
	r.workers["worker"].done <- nil
	close(release)
	require.NoError(t, lifecycleResult(t, result))
}

func TestCleanStopsCanRepeat(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	jobs := mocks.NewMockJobs(ctrl)
	jobs.EXPECT().ClaimNext(gomock.Any(), time.Minute).Return(persistence.Job{}, false, nil).AnyTimes()
	started := make(chan struct{}, 2)
	r, err := New(
		lifecycleSources{list: func(context.Context) ([]persistence.SourceDefinition, error) {
			return []persistence.SourceDefinition{{ID: "one", Name: "one", Kubernetes: &persistence.KubernetesSpec{}}}, nil
		}},
		jobs, mocks.NewMockFetcher(ctrl),
		Options{
			Reconcile: reconcile.Options{PollInterval: time.Hour, LeaseDuration: time.Minute},
			Operator: func(persistence.SourceDefinition) (Worker, error) {
				return lifecycleWorker{started: started}, nil
			},
			Preflight: func(context.Context, persistence.SourceDefinition) error { return nil },
		},
	)
	require.NoError(t, err)
	for range 2 {
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)
		go func() { done <- r.Run(ctx) }()
		lifecycleBarrier(t, started)
		cancel()
		require.NoError(t, lifecycleResult(t, done))
	}
}

func TestRefreshLifecycleSnapshot(t *testing.T) {
	t.Parallel()
	for _, delayed := range []bool{false, true} {
		t.Run(map[bool]string{false: "concurrent-first-start", true: "snapshot-before-restart"}[delayed], func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			jobs, fetcher := mocks.NewMockJobs(ctrl), mocks.NewMockFetcher(ctrl)
			jobs.EXPECT().ClaimNext(gomock.Any(), time.Minute).Return(persistence.Job{}, false, nil).AnyTimes()
			listed := make(chan struct{})
			var lists atomic.Int32
			sources := lifecycleSources{list: func(ctx context.Context) ([]persistence.SourceDefinition, error) {
				if lists.Add(1) == 1 {
					close(listed)
					<-ctx.Done()
					return nil, ctx.Err()
				}
				return nil, nil
			}}
			r, err := New(sources, jobs, fetcher, Options{
				Reconcile: reconcile.Options{PollInterval: time.Hour, LeaseDuration: time.Minute},
				Operator: func(persistence.SourceDefinition) (Worker, error) {
					t.Fatal("empty discovery must not create workers")
					return nil, persistence.ErrInvalid
				},
				Preflight: func(context.Context, persistence.SourceDefinition) error { return nil },
			})
			require.NoError(t, err)
			require.ErrorIs(t, r.Refresh(t.Context()), persistence.ErrBusy)
			ctx, cancel := context.WithCancel(t.Context())
			t.Cleanup(cancel)
			done, refreshed := make(chan error, 1), make(chan error, 1)
			request := &snapshotContext{Context: t.Context(), captured: make(chan struct{}), release: make(chan struct{})}
			release := sync.OnceFunc(func() { close(request.release) })
			t.Cleanup(release)
			r.mu.Lock()
			go func() { done <- r.Run(ctx) }()
			if !delayed {
				go func() { refreshed <- r.Refresh(t.Context()) }()
			}
			r.mu.Unlock()
			lifecycleBarrier(t, listed)
			if delayed {
				go func() { refreshed <- r.Refresh(request) }()
				lifecycleBarrier(t, request.captured)
			}
			r.mu.Lock()
			oldRefresh, oldDone := r.refresh, r.done
			r.mu.Unlock()
			cancel()
			require.NoError(t, lifecycleResult(t, done))
			if !delayed {
				require.ErrorIs(t, lifecycleResult(t, refreshed), persistence.ErrBusy)
			}
			ctx, cancel = context.WithCancel(t.Context())
			t.Cleanup(cancel)
			go func() { done <- r.Run(ctx) }()
			require.Eventually(t, r.Ready, time.Second, time.Millisecond)
			r.mu.Lock()
			newRefresh, newDone := r.refresh, r.done
			r.mu.Unlock()
			require.NotEqual(t, oldRefresh, newRefresh)
			require.NotEqual(t, oldDone, newDone)
			if delayed {
				release()
				require.ErrorIs(t, lifecycleResult(t, refreshed), persistence.ErrBusy)
			}
			require.EqualValues(t, 2, lists.Load(), "old requests must not enter the restarted run")
			cancel()
			require.NoError(t, lifecycleResult(t, done))
		})
	}
}
