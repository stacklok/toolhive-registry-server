package operator_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/ingest"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/operator"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/reconcile"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/reconcile/mocks"
)

func TestOperatorListMixedCancellationRemainsSanitizedFailure(t *testing.T) {
	t.Parallel()
	for _, mixed := range []bool{false, true} {
		t.Run(fmt.Sprintf("mixed=%t", mixed), func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			api := fake.NewClientBuilder().WithScheme(scheme(t)).WithInterceptorFuncs(interceptor.Funcs{
				List: func(ctx context.Context, _ client.WithWatch, _ client.ObjectList, _ ...client.ListOption) error {
					cancel()
					<-ctx.Done()
					if mixed {
						return fmt.Errorf("SECRET wrapper: %w", errors.Join(ctx.Err(), errors.New("SECRET list failure")))
					}
					return fmt.Errorf("SECRET wrapper: %w", ctx.Err())
				},
			}).Build()
			fetcher, err := operator.NewFetcher(api, definition())
			require.NoError(t, err)
			result, err := fetcher.Fetch(ctx, definition())
			require.ErrorIs(t, err, context.Canceled)
			if mixed {
				require.ErrorIs(t, err, ingest.ErrFetch)
			} else {
				require.NotErrorIs(t, err, ingest.ErrFetch)
			}
			require.NotContains(t, err.Error(), "SECRET")
			require.Empty(t, result.Hash)
			require.Empty(t, result.Snapshot.Data.Servers)
		})
	}
}

func TestRunnerActiveSyncShutdownResults(t *testing.T) {
	t.Parallel()
	for _, failure := range []string{"clean", "backend", "cleanup", "watch and cleanup"} {
		t.Run(failure, func(t *testing.T) {
			t.Parallel()
			w := watch.NewRaceFreeFake()
			api := fake.NewClientBuilder().WithScheme(scheme(t)).WithInterceptorFuncs(interceptor.Funcs{
				Watch: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) (watch.Interface, error) {
					return w, nil
				},
			}).Build()
			jobs := mocks.NewMockJobs(gomock.NewController(t))
			job := persistence.Job{Source: definition(), LeaseID: "lease", Generation: 1}
			backendErr, cleanupErr := errors.New("commit failed"), errors.New("cleanup failed")
			jobs.EXPECT().Acquire(gomock.Any(), job.Source.Name, time.Minute).Return(job, nil)
			jobs.EXPECT().Status(gomock.Any(), job.Source.Name).Return(persistence.JobStatus{SourceID: job.Source.ID}, nil)
			entered, canceled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
			jobs.EXPECT().CommitSnapshot(gomock.Any(), job, gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, _ persistence.Job, _ persistence.SyncResult, _ model.Snapshot) error {
				close(entered)
				<-ctx.Done()
				close(canceled)
				<-release
				if failure == "backend" {
					return fmt.Errorf("commit: %w", errors.Join(ctx.Err(), backendErr))
				}
				return fmt.Errorf("commit: %w", ctx.Err())
			})
			jobs.EXPECT().Fail(gomock.Any(), job, persistence.FailureCancelled).DoAndReturn(func(ctx context.Context, _ persistence.Job, _ persistence.FailureCategory) error {
				require.NoError(t, ctx.Err())
				if failure == "cleanup" || failure == "watch and cleanup" {
					return errors.Join(context.Canceled, cleanupErr)
				}
				return nil
			})
			runner, err := operator.NewRunner(api, jobs, definition(), reconcile.Options{PollInterval: time.Hour, LeaseDuration: time.Minute})
			require.NoError(t, err)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- runner.Run(ctx) }()
			select {
			case <-entered:
			case <-time.After(3 * time.Second):
				t.Fatal("runner did not enter Sync commit")
			}
			if failure == "watch and cleanup" {
				w.Error(&metav1.Status{Reason: metav1.StatusReasonForbidden})
			} else {
				cancel()
			}
			select {
			case <-canceled:
			case <-time.After(3 * time.Second):
				t.Fatal("runner did not cancel Sync")
			}
			select {
			case <-done:
				t.Fatal("runner returned before Sync joined")
			default:
			}
			close(release)
			select {
			case err := <-done:
				switch failure {
				case "clean":
					require.NoError(t, err)
				case "backend":
					require.ErrorIs(t, err, backendErr)
				default:
					require.ErrorIs(t, err, cleanupErr)
				}
				if failure == "watch and cleanup" {
					require.ErrorIs(t, err, operator.ErrWatch)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("runner did not join Sync")
			}
			require.True(t, w.IsStopped())
			require.False(t, runner.Ready())
		})
	}
}
