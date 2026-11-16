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

	sourcemocks "github.com/stacklok/toolhive-registry-server/internal/sync/standalone/mocks"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/reconcile"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/reconcile/mocks"
)

func TestCancellationDuringJoinDoesNotEraseTerminalError(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ctrl := gomock.NewController(t)
	sources := sourcemocks.NewMockSources(ctrl)
	sources.EXPECT().ListSources(gomock.Any()).Return([]persistence.SourceDefinition{
		{ID: "one", Name: "one", Kubernetes: &persistence.KubernetesSpec{}},
		{ID: "two", Name: "two", Kubernetes: &persistence.KubernetesSpec{}},
	}, nil).AnyTimes()
	jobs := mocks.NewMockJobs(ctrl)
	jobs.EXPECT().ClaimNext(gomock.Any(), time.Minute).Return(persistence.Job{}, false, nil).AnyTimes()
	first, second := sourcemocks.NewMockWorker(ctrl), sourcemocks.NewMockWorker(ctrl)
	failed, started, joined := make(chan struct{}), make(chan struct{}), make(chan struct{})
	first.EXPECT().Run(gomock.Any()).DoAndReturn(func(ctx context.Context) error {
		select {
		case <-failed:
			return persistence.ErrUnavailable
		case <-ctx.Done():
			return nil
		}
	})
	second.EXPECT().Run(gomock.Any()).DoAndReturn(func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		cancel()
		close(joined)
		return nil
	})
	runtime, err := New(sources, jobs, mocks.NewMockFetcher(ctrl), Options{
		Reconcile: reconcile.Options{PollInterval: time.Hour, LeaseDuration: time.Minute},
		Preflight: func(context.Context, persistence.SourceDefinition) error { return nil },
		Operator: func(d persistence.SourceDefinition) (Worker, error) {
			if d.Name == "one" {
				return first, nil
			}
			return second, nil
		},
	})
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- runtime.Run(ctx) }()
	lifecycleBarrier(t, started)
	close(failed)
	require.ErrorIs(t, lifecycleResult(t, done), persistence.ErrUnavailable)
	lifecycleBarrier(t, joined)
	require.False(t, runtime.Ready())
}

func TestStopJoinsDelayedTerminalResultsAndDoesNotLeakAcrossRuns(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{"worker", "poller", "siblings", "clean"} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			sources := sourcemocks.NewMockSources(ctrl)
			sources.EXPECT().ListSources(gomock.Any()).Return([]persistence.SourceDefinition{
				{ID: "one", Name: "one", Kubernetes: &persistence.KubernetesSpec{}},
				{ID: "two", Name: "two", Kubernetes: &persistence.KubernetesSpec{}},
			}, nil).AnyTimes()
			firstErr, secondErr, pollErr := errors.New("first terminal"), errors.New("second terminal"), errors.New("poller terminal")
			started := make(chan struct{}, 3)
			joining := make(chan struct{}, 3)
			release := make(chan struct{})
			var clean atomic.Bool
			jobs := mocks.NewMockJobs(ctrl)
			jobs.EXPECT().ClaimNext(gomock.Any(), time.Minute).DoAndReturn(func(ctx context.Context, _ time.Duration) (persistence.Job, bool, error) {
				started <- struct{}{}
				<-ctx.Done()
				joining <- struct{}{}
				<-release
				if !clean.Load() && (stage == "poller" || stage == "siblings") {
					return persistence.Job{}, false, errors.Join(ctx.Err(), pollErr)
				}
				return persistence.Job{}, false, ctx.Err()
			}).AnyTimes()
			workers := map[string]*sourcemocks.MockWorker{}
			for _, name := range []string{"one", "two"} {
				worker := sourcemocks.NewMockWorker(ctrl)
				workers[name] = worker
				worker.EXPECT().Run(gomock.Any()).DoAndReturn(func(ctx context.Context) error {
					started <- struct{}{}
					<-ctx.Done()
					joining <- struct{}{}
					<-release
					if !clean.Load() && (stage == "worker" || stage == "siblings") {
						if name == "one" {
							return fmt.Errorf("worker join: %w", errors.Join(ctx.Err(), firstErr))
						}
						if stage == "siblings" {
							return errors.Join(ctx.Err(), secondErr)
						}
					}
					return fmt.Errorf("clean join: %w", ctx.Err())
				}).AnyTimes()
			}
			runtime, err := New(sources, jobs, mocks.NewMockFetcher(ctrl), Options{
				Reconcile: reconcile.Options{PollInterval: time.Hour, LeaseDuration: time.Minute},
				Preflight: func(context.Context, persistence.SourceDefinition) error { return nil },
				Operator:  func(d persistence.SourceDefinition) (Worker, error) { return workers[d.Name], nil },
			})
			require.NoError(t, err)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- runtime.Run(ctx) }()
			for range 3 {
				lifecycleBarrier(t, started)
			}
			cancel()
			for range 3 {
				lifecycleBarrier(t, joining)
			}
			select {
			case <-done:
				t.Fatal("Run returned before its siblings joined")
			default:
			}
			close(release)
			err = lifecycleResult(t, done)
			switch stage {
			case "worker":
				require.ErrorIs(t, err, firstErr)
			case "poller":
				require.ErrorIs(t, err, pollErr)
			case "siblings":
				require.ErrorIs(t, err, firstErr)
				require.ErrorIs(t, err, secondErr)
				require.ErrorIs(t, err, pollErr)
			case "clean":
				require.NoError(t, err)
			}
			clean.Store(true)
			ctx, cancel = context.WithCancel(t.Context())
			defer cancel()
			go func() { done <- runtime.Run(ctx) }()
			for range 3 {
				lifecycleBarrier(t, started)
			}
			cancel()
			require.NoError(t, lifecycleResult(t, done), "previous results must not contaminate a fresh run")
		})
	}
}
