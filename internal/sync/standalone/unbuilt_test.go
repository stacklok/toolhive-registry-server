package standalone

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/reconcile"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/reconcile/mocks"
)

// idleWorker runs until cancelled.
type idleWorker struct {
	started chan struct{}
	ready   atomic.Bool
}

func (w *idleWorker) Ready() bool { return w.ready.Load() }
func (w *idleWorker) Run(ctx context.Context) error {
	w.ready.Store(true)
	close(w.started)
	<-ctx.Done()
	return nil
}

func setUnbuiltRetry(t *testing.T, d time.Duration) {
	t.Helper()
	oldMin, oldMax := unbuiltRetryMin, unbuiltRetryMax
	t.Cleanup(func() { unbuiltRetryMin, unbuiltRetryMax = oldMin, oldMax })
	unbuiltRetryMin, unbuiltRetryMax = d, d
}

// A Kubernetes source whose worker cannot be built fails alone: the failure is
// recorded on that source, its sibling keeps running, the runtime does not
// stop, and the build is retried until it succeeds.
//
//nolint:paralleltest // mutates the package-level retry delays
func TestUnbuildableWorkerFailsOnlyItsSource(t *testing.T) {
	setUnbuiltRetry(t, 5*time.Millisecond)
	ctrl := gomock.NewController(t)
	jobs := mocks.NewMockJobs(ctrl)
	broken := persistence.SourceDefinition{ID: "broken-id", Name: "broken",
		Kubernetes: &persistence.KubernetesSpec{Namespaces: []string{"Bad_NS"}}}
	healthy := persistence.SourceDefinition{ID: "healthy-id", Name: "healthy",
		Kubernetes: &persistence.KubernetesSpec{Namespaces: []string{"default"}}}
	sources := lifecycleSources{list: func(context.Context) ([]persistence.SourceDefinition, error) {
		return []persistence.SourceDefinition{broken, healthy}, nil
	}}
	jobs.EXPECT().ClaimNext(gomock.Any(), time.Minute).Return(persistence.Job{}, false, nil).AnyTimes()
	job := persistence.Job{Source: broken, LeaseID: "lease"}
	recorded := make(chan struct{})
	jobs.EXPECT().Acquire(gomock.Any(), "broken", time.Minute).Return(job, nil)
	jobs.EXPECT().Fail(gomock.Any(), job, persistence.FailureValidation).DoAndReturn(
		func(context.Context, persistence.Job, persistence.FailureCategory) error { close(recorded); return nil })

	sibling, rebuilt := &idleWorker{started: make(chan struct{})}, &idleWorker{started: make(chan struct{})}
	var builds atomic.Int32
	invalid := fmt.Errorf("%w: namespace %q is not a valid namespace name", persistence.ErrInvalid, "Bad_NS")
	r, err := New(sources, jobs, mocks.NewMockFetcher(ctrl), Options{
		Reconcile: reconcile.Options{PollInterval: time.Hour, LeaseDuration: time.Minute},
		Operator: func(d persistence.SourceDefinition) (Worker, error) {
			if d.Name == healthy.Name {
				return sibling, nil
			}
			// The first two builds fail with the same cause, which is recorded once.
			if builds.Add(1) <= 2 {
				return nil, invalid
			}
			return rebuilt, nil
		},
		Preflight: func(context.Context, persistence.SourceDefinition) error { return nil },
	})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()

	lifecycleBarrier(t, sibling.started)
	lifecycleBarrier(t, recorded)
	lifecycleBarrier(t, rebuilt.started)
	require.EqualValues(t, 3, builds.Load())
	require.Eventually(t, r.Ready, 3*time.Second, time.Millisecond)
	select {
	case err := <-done:
		t.Fatalf("runtime stopped: %v", err)
	default:
	}
	cancel()
	require.NoError(t, lifecycleResult(t, done))
}

// Recording the build failure skips a source another replica holds, and stops
// the runtime only when storage itself fails.
//
//nolint:paralleltest // mutates the package-level retry delays
func TestUnbuiltWorkerRecording(t *testing.T) {
	setUnbuiltRetry(t, time.Hour)
	d := persistence.SourceDefinition{ID: "id", Name: "broken", Kubernetes: &persistence.KubernetesSpec{}}
	for _, tc := range []struct {
		name       string
		acquireErr error
		acquireID  string
		wantErr    error
	}{
		{name: "lease held by another replica", acquireErr: persistence.ErrBusy},
		{name: "source removed", acquireErr: persistence.ErrNotFound},
		{name: "source recreated", acquireID: "newer"},
		{name: "storage unavailable stops the runtime", acquireErr: persistence.ErrUnavailable, wantErr: persistence.ErrUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			jobs := mocks.NewMockJobs(gomock.NewController(t))
			job := persistence.Job{Source: d, LeaseID: "lease"}
			if tc.acquireID != "" {
				job.Source.ID = tc.acquireID
			}
			jobs.EXPECT().Acquire(gomock.Any(), d.Name, time.Minute).Return(job, tc.acquireErr)
			w := &unbuiltWorker{jobs: jobs, definition: d, lease: time.Minute, err: errors.New("client unavailable"),
				build: func(persistence.SourceDefinition) (Worker, error) { return nil, errors.New("unexpected build") }}
			require.True(t, w.Ready())
			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan error, 1)
			go func() { done <- w.Run(ctx) }()
			if tc.wantErr != nil {
				require.ErrorIs(t, lifecycleResult(t, done), tc.wantErr)
				cancel()
				return
			}
			require.Never(t, func() bool { return len(done) > 0 }, 30*time.Millisecond, time.Millisecond)
			cancel()
			require.NoError(t, lifecycleResult(t, done))
		})
	}
}
