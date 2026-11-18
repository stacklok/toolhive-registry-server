package kubernetes

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	mcp "github.com/stacklok/toolhive/cmd/thv-operator/api/v1beta1"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/reconcile"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/reconcile/mocks"
)

// The tests in this file shrink the package-level restart delays, so none of
// them may run in parallel.

// watchesPerRun is one watch per exported kind in each of the two namespaces
// of restartDefinition.
const watchesPerRun = 6

func restartDefinition() persistence.SourceDefinition {
	return persistence.SourceDefinition{ID: "operator-incarnation", Name: "operator", Kubernetes: &persistence.KubernetesSpec{Namespaces: []string{"one", "two"}}}
}

func restartOptions() reconcile.Options {
	return reconcile.Options{PollInterval: time.Hour, LeaseDuration: time.Minute}
}

func setRestartDelays(t *testing.T, first, ceiling, reset time.Duration) {
	t.Helper()
	oldMin, oldMax, oldReset := watchRestartMin, watchRestartMax, watchRestartReset
	t.Cleanup(func() { watchRestartMin, watchRestartMax, watchRestartReset = oldMin, oldMax, oldReset })
	watchRestartMin, watchRestartMax, watchRestartReset = first, ceiling, reset
}

// watchFailingClient returns a client whose every Watch call fails, recording
// the time of each call. onWatch may return a different error for call n.
func watchFailingClient(t *testing.T, calls *[]time.Time, mu *sync.Mutex, onWatch func(n int) error) client.WithWatch {
	t.Helper()
	s := runtime.NewScheme()
	require.NoError(t, mcp.AddToScheme(s))
	return fake.NewClientBuilder().WithScheme(s).WithInterceptorFuncs(interceptor.Funcs{
		Watch: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) (watch.Interface, error) {
			mu.Lock()
			*calls = append(*calls, time.Now())
			n := len(*calls)
			mu.Unlock()
			return nil, onWatch(n)
		},
	}).Build()
}

//nolint:paralleltest // mutates the package-level restart delays
func TestOperatorWorkerRestartsAfterWatchEnds(t *testing.T) {
	for _, tc := range []struct {
		name string
		end  func(*watch.RaceFreeFakeWatcher)
	}{
		{name: "watch closed", end: func(w *watch.RaceFreeFakeWatcher) { w.Stop() }},
		{name: "watch error event", end: func(w *watch.RaceFreeFakeWatcher) {
			w.Error(&metav1.Status{Reason: metav1.StatusReasonExpired})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setRestartDelays(t, time.Millisecond, 4*time.Millisecond, time.Hour)
			s := runtime.NewScheme()
			require.NoError(t, mcp.AddToScheme(s))
			var mu sync.Mutex
			var watches []*watch.RaceFreeFakeWatcher
			watchCount := func() int { mu.Lock(); defer mu.Unlock(); return len(watches) }
			api := fake.NewClientBuilder().WithScheme(s).WithInterceptorFuncs(interceptor.Funcs{
				Watch: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) (watch.Interface, error) {
					w := watch.NewRaceFreeFake()
					mu.Lock()
					watches = append(watches, w)
					mu.Unlock()
					return w, nil
				},
			}).Build()
			d := restartDefinition()
			job := persistence.Job{Source: d, LeaseID: "lease", Generation: 1}
			jobs := mocks.NewMockJobs(gomock.NewController(t))
			var acquires, commits atomic.Int32
			jobs.EXPECT().Acquire(gomock.Any(), d.Name, time.Minute).AnyTimes().DoAndReturn(
				func(context.Context, string, time.Duration) (persistence.Job, error) {
					acquires.Add(1)
					return job, nil
				})
			jobs.EXPECT().Status(gomock.Any(), d.Name).AnyTimes().Return(persistence.JobStatus{SourceID: d.ID}, nil)
			jobs.EXPECT().CommitSnapshot(gomock.Any(), job, gomock.Any(), gomock.Any()).AnyTimes().DoAndReturn(
				func(context.Context, persistence.Job, persistence.SyncResult, model.Snapshot) error {
					commits.Add(1)
					return nil
				})
			worker, err := NewStandaloneOperator(api, jobs, d, restartOptions(), nil)
			require.NoError(t, err)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- worker.Run(ctx) }()

			require.Eventually(t, func() bool { return worker.Ready() && commits.Load() == 1 }, 3*time.Second, time.Millisecond)
			require.Equal(t, watchesPerRun, watchCount())

			mu.Lock()
			tc.end(watches[0])
			mu.Unlock()

			// The runner returns operator.ErrWatch; the worker runs it again, which
			// re-establishes every watch and starts with a full refresh.
			require.Eventually(t, func() bool {
				return worker.Ready() && commits.Load() == 2 && watchCount() == 2*watchesPerRun
			}, 3*time.Second, time.Millisecond)
			require.EqualValues(t, 2, acquires.Load())
			mu.Lock()
			for _, w := range watches[:watchesPerRun] {
				require.True(t, w.IsStopped(), "the ended run's watches are released")
			}
			mu.Unlock()
			select {
			case err := <-done:
				t.Fatalf("worker returned after an ended watch: %v", err)
			default:
			}

			cancel()
			select {
			case err := <-done:
				require.NoError(t, err)
			case <-time.After(3 * time.Second):
				t.Fatal("worker did not stop on cancellation")
			}
			require.False(t, worker.Ready())
		})
	}
}

//nolint:paralleltest // mutates the package-level restart delays
func TestOperatorWorkerRestartBackoff(t *testing.T) {
	ms := time.Millisecond
	for _, tc := range []struct {
		name                  string
		first, ceiling, reset time.Duration
		// minGaps are lower bounds of each delay between runs. maxTotal bounds
		// all delays together, well below what they would sum to without the
		// cap (5ms doubling eight times: 1275ms) or the reset (20ms: 5100ms).
		minGaps  []time.Duration
		maxTotal time.Duration
	}{
		{
			name: "doubles up to the maximum", first: 5 * ms, ceiling: 20 * ms, reset: time.Hour,
			minGaps: []time.Duration{5 * ms, 10 * ms, 20 * ms, 20 * ms, 20 * ms, 20 * ms, 20 * ms, 20 * ms}, maxTotal: time.Second,
		},
		{
			// A zero reset threshold makes every run count as long-lived.
			name: "resets after a long run", first: 20 * ms, ceiling: time.Hour, reset: 0,
			minGaps: []time.Duration{20 * ms, 20 * ms, 20 * ms, 20 * ms, 20 * ms, 20 * ms, 20 * ms, 20 * ms}, maxTotal: 2 * time.Second,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setRestartDelays(t, tc.first, tc.ceiling, tc.reset)
			restarts := len(tc.minGaps)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var mu sync.Mutex
			var calls []time.Time
			// Each run fails its first Watch, so a run is one call. The last call
			// cancels and reports cancellation, so the worker returns nil.
			api := watchFailingClient(t, &calls, &mu, func(n int) error {
				if n == restarts+1 {
					cancel()
					return context.Canceled
				}
				return errors.New("watch refused")
			})
			worker, err := NewStandaloneOperator(api, mocks.NewMockJobs(gomock.NewController(t)), restartDefinition(), restartOptions(), nil)
			require.NoError(t, err)
			require.NoError(t, worker.Run(ctx))
			require.False(t, worker.Ready())

			mu.Lock()
			defer mu.Unlock()
			require.Len(t, calls, restarts+1)
			for i, want := range tc.minGaps {
				require.GreaterOrEqual(t, calls[i+1].Sub(calls[i]), want, "delay before run %d", i+2)
			}
			require.Less(t, calls[restarts].Sub(calls[0]), tc.maxTotal)
		})
	}
}

//nolint:paralleltest // mutates the package-level restart delays
func TestOperatorWorkerCancelDuringRestartDelay(t *testing.T) {
	setRestartDelays(t, time.Hour, time.Hour, time.Hour)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var mu sync.Mutex
	var calls []time.Time
	watched := make(chan struct{})
	api := watchFailingClient(t, &calls, &mu, func(int) error {
		close(watched)
		return errors.New("watch refused")
	})
	worker, err := NewStandaloneOperator(api, mocks.NewMockJobs(gomock.NewController(t)), restartDefinition(), restartOptions(), nil)
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()
	<-watched
	// The run failed with operator.ErrWatch; the worker is now in its hour-long delay.
	require.Never(t, func() bool { return len(done) > 0 }, 50*time.Millisecond, time.Millisecond)
	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not return promptly when canceled during the restart delay")
	}
	mu.Lock()
	defer mu.Unlock()
	require.Len(t, calls, 1)
}

// After a watch ends, the restarted runner is not ready yet, so the library
// returns its first failure. Once the source has synced, the worker must retry
// that failure rather than stop sync; an ownership error still returns.
//
//nolint:paralleltest // mutates the package-level restart delays
func TestOperatorWorkerFailureAfterRestart(t *testing.T) {
	for _, tc := range []struct {
		name    string
		failure error
		retried bool
	}{
		{name: "transient storage failure is retried", failure: errors.New("database unavailable"), retried: true},
		{name: "ownership error returns", failure: persistence.ErrNotFound, retried: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setRestartDelays(t, time.Millisecond, 4*time.Millisecond, time.Hour)
			s := runtime.NewScheme()
			require.NoError(t, mcp.AddToScheme(s))
			var mu sync.Mutex
			var watches []*watch.RaceFreeFakeWatcher
			api := fake.NewClientBuilder().WithScheme(s).WithInterceptorFuncs(interceptor.Funcs{
				Watch: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) (watch.Interface, error) {
					w := watch.NewRaceFreeFake()
					mu.Lock()
					watches = append(watches, w)
					mu.Unlock()
					return w, nil
				},
			}).Build()
			d := restartDefinition()
			job := persistence.Job{Source: d, LeaseID: "lease", Generation: 1}
			jobs := mocks.NewMockJobs(gomock.NewController(t))
			var acquires, commits atomic.Int32
			jobs.EXPECT().Acquire(gomock.Any(), d.Name, time.Minute).AnyTimes().DoAndReturn(
				func(context.Context, string, time.Duration) (persistence.Job, error) {
					if acquires.Add(1) == 2 { // the first sync of the restarted run
						return persistence.Job{}, tc.failure
					}
					return job, nil
				})
			jobs.EXPECT().Status(gomock.Any(), d.Name).AnyTimes().Return(persistence.JobStatus{SourceID: d.ID}, nil)
			jobs.EXPECT().CommitSnapshot(gomock.Any(), job, gomock.Any(), gomock.Any()).AnyTimes().DoAndReturn(
				func(context.Context, persistence.Job, persistence.SyncResult, model.Snapshot) error {
					commits.Add(1)
					return nil
				})
			worker, err := NewStandaloneOperator(api, jobs, d, restartOptions(), nil)
			require.NoError(t, err)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- worker.Run(ctx) }()
			require.Eventually(t, func() bool { return worker.Ready() && commits.Load() == 1 }, 3*time.Second, time.Millisecond)
			mu.Lock()
			watches[0].Stop()
			mu.Unlock()

			if !tc.retried {
				select {
				case err := <-done:
					require.ErrorIs(t, err, tc.failure)
				case <-time.After(3 * time.Second):
					t.Fatal("ownership error did not return")
				}
				return
			}
			require.Eventually(t, func() bool { return worker.Ready() && commits.Load() == 2 }, 3*time.Second, time.Millisecond)
			require.EqualValues(t, 3, acquires.Load(), "the failed sync was retried by another run")
			require.Empty(t, done, "the worker keeps running")
			cancel()
			require.NoError(t, <-done)
		})
	}
}
