package standalone_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/internal/sync/standalone"
	standalonemocks "github.com/stacklok/toolhive-registry-server/internal/sync/standalone/mocks"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/ingest"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/reconcile/mocks"
)

func TestInlineActiveShutdownResults(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{"status", "sync status", "sync commit", "sync cleanup"} {
		for _, mixed := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/mixed=%t", stage, mixed), func(t *testing.T) {
				t.Parallel()
				ctrl := gomock.NewController(t)
				sources, jobs, fetcher := standalonemocks.NewMockSources(ctrl), mocks.NewMockJobs(ctrl), mocks.NewMockFetcher(ctrl)
				d := persistence.SourceDefinition{ID: "inline-id", Name: "inline", File: &persistence.FileSpec{Data: "persisted"}}
				sources.EXPECT().ListSources(gomock.Any()).Return([]persistence.SourceDefinition{d}, nil)
				jobs.EXPECT().ClaimNext(gomock.Any(), time.Minute).Return(persistence.Job{}, false, nil).AnyTimes()
				backendErr := errors.New("inline backend failed")
				entered, release := make(chan struct{}), make(chan struct{})
				blocked := func(ctx context.Context) error {
					close(entered)
					<-ctx.Done()
					<-release
					if mixed && stage != "sync cleanup" {
						return fmt.Errorf("inline: %w", errors.Join(ctx.Err(), errors.Join(backendErr, context.DeadlineExceeded)))
					}
					return fmt.Errorf("inline: %w", errors.Join(ctx.Err(), context.DeadlineExceeded))
				}
				if stage == "status" {
					jobs.EXPECT().Status(gomock.Any(), d.Name).DoAndReturn(func(ctx context.Context, _ string) (persistence.JobStatus, error) {
						return persistence.JobStatus{}, blocked(ctx)
					})
				} else {
					job := persistence.Job{Source: d, LeaseID: "lease", Generation: 1}
					jobs.EXPECT().Status(gomock.Any(), d.Name).Return(persistence.JobStatus{SourceID: d.ID}, nil)
					jobs.EXPECT().Acquire(gomock.Any(), d.Name, time.Minute).Return(job, nil)
					fetcher.EXPECT().Fetch(gomock.Any(), d).Return(ingest.Result{Hash: "hash"}, nil)
					if stage == "sync status" {
						jobs.EXPECT().Status(gomock.Any(), d.Name).DoAndReturn(func(ctx context.Context, _ string) (persistence.JobStatus, error) {
							return persistence.JobStatus{}, blocked(ctx)
						})
					} else {
						jobs.EXPECT().Status(gomock.Any(), d.Name).Return(persistence.JobStatus{SourceID: d.ID}, nil)
						jobs.EXPECT().CommitSnapshot(gomock.Any(), job, gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, _ persistence.Job, _ persistence.SyncResult, _ model.Snapshot) error {
							return blocked(ctx)
						})
					}
					jobs.EXPECT().Fail(gomock.Any(), job, persistence.FailureCancelled).DoAndReturn(func(ctx context.Context, _ persistence.Job, _ persistence.FailureCategory) error {
						require.NoError(t, ctx.Err())
						if mixed && stage == "sync cleanup" {
							return errors.Join(context.Canceled, backendErr)
						}
						return nil
					})
				}
				r, err := standalone.New(sources, jobs, fetcher, options())
				require.NoError(t, err)
				cancel, done := run(t, r)
				wait(t, entered)
				cancel()
				select {
				case <-done:
					t.Fatal("runtime returned before inline work joined")
				default:
				}
				close(release)
				select {
				case err := <-done:
					if mixed {
						require.ErrorIs(t, err, backendErr)
					} else {
						require.NoError(t, err)
					}
				case <-time.After(3 * time.Second):
					t.Fatal("runtime did not join inline work")
				}
				require.False(t, r.Ready())
			})
		}
	}
}

func TestInlineFetchRetryAndCleanupFailure(t *testing.T) {
	t.Parallel()
	for _, cleanupFails := range []bool{false, true} {
		t.Run(fmt.Sprintf("cleanup fails=%t", cleanupFails), func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			sources, jobs, fetcher := standalonemocks.NewMockSources(ctrl), mocks.NewMockJobs(ctrl), mocks.NewMockFetcher(ctrl)
			d := persistence.SourceDefinition{ID: "inline-id", Name: "inline", File: &persistence.FileSpec{Data: "persisted"}}
			job := persistence.Job{Source: d, LeaseID: "lease", Generation: 1}
			sources.EXPECT().ListSources(gomock.Any()).Return([]persistence.SourceDefinition{d}, nil).AnyTimes()
			jobs.EXPECT().ClaimNext(gomock.Any(), time.Minute).Return(persistence.Job{}, false, nil).AnyTimes()
			jobs.EXPECT().Status(gomock.Any(), d.Name).Return(persistence.JobStatus{SourceID: d.ID}, nil).AnyTimes()
			jobs.EXPECT().Acquire(gomock.Any(), d.Name, time.Minute).Return(job, nil).AnyTimes()
			fetcher.EXPECT().Fetch(gomock.Any(), d).Return(ingest.Result{}, errors.New("transient fetch failure"))
			cleanupErr := errors.New("terminal cleanup failure")
			fail := jobs.EXPECT().Fail(gomock.Any(), job, persistence.FailureFetch)
			if cleanupFails {
				fail.Return(cleanupErr)
			} else {
				fail.Return(nil)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if !cleanupFails {
				fetcher.EXPECT().Fetch(gomock.Any(), d).After(fail).Return(ingest.Result{Hash: "hash"}, nil)
				jobs.EXPECT().CommitSnapshot(gomock.Any(), job, gomock.Any(), gomock.Any()).DoAndReturn(func(context.Context, persistence.Job, persistence.SyncResult, model.Snapshot) error {
					cancel()
					return nil
				})
			}
			opts := options()
			opts.Reconcile.PollInterval = time.Millisecond
			r, err := standalone.New(sources, jobs, fetcher, opts)
			require.NoError(t, err)
			done := make(chan error, 1)
			go func() { done <- r.Run(ctx) }()
			select {
			case err := <-done:
				if cleanupFails {
					require.ErrorIs(t, err, cleanupErr)
					require.ErrorIs(t, err, ingest.ErrFetch)
				} else {
					require.NoError(t, err, "a recoverable fetch failure must retry before shutdown")
				}
			case <-time.After(3 * time.Second):
				t.Fatal("runtime did not finish")
			}
		})
	}
}

func TestDiscoveryJoinsLateBackendError(t *testing.T) {
	t.Parallel()
	for _, terminal := range []string{"initial cancel", "refresh cancel", "refresh worker"} {
		t.Run(terminal, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			sources, jobs, fetcher := standalonemocks.NewMockSources(ctrl), mocks.NewMockJobs(ctrl), mocks.NewMockFetcher(ctrl)
			d := persistence.SourceDefinition{ID: "inline-id", Name: "inline", File: &persistence.FileSpec{Data: "persisted"}}
			workerErr, discoveryErr := errors.New("active worker failed"), errors.New("discovery backend failed")
			workerStarted, workerFail := make(chan struct{}), make(chan struct{})
			if terminal != "initial cancel" {
				sources.EXPECT().ListSources(gomock.Any()).Return([]persistence.SourceDefinition{d}, nil)
				jobs.EXPECT().ClaimNext(gomock.Any(), time.Minute).Return(persistence.Job{}, false, nil).AnyTimes()
				jobs.EXPECT().Status(gomock.Any(), d.Name).DoAndReturn(func(ctx context.Context, _ string) (persistence.JobStatus, error) {
					close(workerStarted)
					select {
					case <-workerFail:
						return persistence.JobStatus{}, fmt.Errorf("worker: %w", errors.Join(context.Canceled, persistence.ErrInvalid, workerErr))
					case <-ctx.Done():
						return persistence.JobStatus{}, ctx.Err()
					}
				})
			}
			entered, canceled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
			sources.EXPECT().ListSources(gomock.Any()).DoAndReturn(func(ctx context.Context) ([]persistence.SourceDefinition, error) {
				close(entered)
				<-ctx.Done()
				close(canceled)
				<-release
				if terminal == "initial cancel" {
					return nil, discoveryErr
				}
				return nil, fmt.Errorf("delayed list: %w", errors.Join(ctx.Err(), discoveryErr))
			})
			r, err := standalone.New(sources, jobs, fetcher, options())
			require.NoError(t, err)
			cancel, done := run(t, r)
			refreshed := make(chan error, 1)
			if terminal != "initial cancel" {
				wait(t, workerStarted)
				go func() { refreshed <- r.Refresh(t.Context()) }()
			}
			wait(t, entered)
			if terminal == "refresh worker" {
				close(workerFail)
			} else {
				cancel()
			}
			wait(t, canceled)
			select {
			case <-done:
				t.Fatal("runtime returned without joining discovery")
			default:
			}
			close(release)
			select {
			case err := <-done:
				require.ErrorIs(t, err, discoveryErr)
				if terminal == "refresh worker" {
					require.ErrorIs(t, err, workerErr)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("runtime did not join discovery")
			}
			if terminal != "initial cancel" {
				select {
				case <-refreshed:
				case <-time.After(3 * time.Second):
					t.Fatal("Refresh did not return")
				}
			}
			require.False(t, r.Ready())
		})
	}
}
