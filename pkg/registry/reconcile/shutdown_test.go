package reconcile_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/ingest"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/reconcile/mocks"
)

func TestCommittedAttemptRetainsRenewalFault(t *testing.T) {
	t.Parallel()
	for _, failure := range []string{"clean", "stale", "backend"} {
		t.Run(failure, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			jobs, fetcher := mocks.NewMockJobs(ctrl), mocks.NewMockFetcher(ctrl)
			job := jobFixture()
			ttl := 60 * time.Millisecond
			backendErr := errors.New("renewal backend failure")
			entered := make(chan struct{})
			jobs.EXPECT().Acquire(gomock.Any(), job.Source.Name, ttl).Return(job, nil)
			fetcher.EXPECT().Fetch(gomock.Any(), job.Source).Return(ingest.Result{Snapshot: snapshotFixture(), Hash: "hash"}, nil)
			jobs.EXPECT().Status(gomock.Any(), job.Source.Name).Return(persistence.JobStatus{SourceID: job.Source.ID}, nil)
			jobs.EXPECT().Renew(gomock.Any(), job, ttl).DoAndReturn(func(ctx context.Context, j persistence.Job, _ time.Duration) (persistence.Job, error) {
				close(entered)
				<-ctx.Done()
				switch failure {
				case "backend":
					return j, fmt.Errorf("renew: %w", errors.Join(ctx.Err(), backendErr))
				case "stale":
					return j, fmt.Errorf("renew: %w", errors.Join(ctx.Err(), persistence.ErrStale))
				default:
					return j, ctx.Err()
				}
			})
			jobs.EXPECT().CommitSnapshot(gomock.Any(), job, gomock.Any(), gomock.Any()).DoAndReturn(func(context.Context, persistence.Job, persistence.SyncResult, model.Snapshot) error {
				select {
				case <-entered:
					return nil
				case <-time.After(3 * time.Second):
					t.Error("renewal did not start")
					return persistence.ErrUnavailable
				}
			})
			err := newCoordinator(t, jobs, fetcher, ttl).Sync(t.Context(), job.Source.Name)
			if failure == "backend" {
				require.ErrorIs(t, err, backendErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestActiveAttemptShutdownResults(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"run", "sync"} {
		for _, stage := range []string{"fetch", "status", "commit", "complete"} {
			for _, failure := range []string{"clean", "backend", "cleanup", "both", "stale cleanup", "stale mixed cleanup"} {
				t.Run(mode+"/"+stage+"/"+failure, func(t *testing.T) {
					t.Parallel()
					ctrl := gomock.NewController(t)
					jobs, fetcher := mocks.NewMockJobs(ctrl), mocks.NewMockFetcher(ctrl)
					job := jobFixture()
					backendErr, cleanupErr := errors.New("active backend failure"), errors.New("cleanup backend failure")
					entered, release := make(chan struct{}), make(chan struct{})
					ctx, cancel := context.WithCancel(t.Context())
					defer cancel()
					blocked := func(ctx context.Context) error {
						close(entered)
						<-ctx.Done()
						<-release
						if failure == "backend" || failure == "both" || failure == "stale cleanup" {
							controlErr := persistence.ErrInvalid
							if stage == "status" {
								controlErr = persistence.ErrNotFound
							}
							if stage == "complete" {
								controlErr = persistence.ErrConflict
							}
							return fmt.Errorf("active operation: %w", errors.Join(ctx.Err(), errors.Join(backendErr, context.DeadlineExceeded, controlErr)))
						}
						return fmt.Errorf("active operation: %w", errors.Join(ctx.Err(), context.DeadlineExceeded))
					}
					if mode == "run" {
						jobs.EXPECT().ClaimNext(gomock.Any(), time.Minute).Return(job, true, nil)
					} else {
						jobs.EXPECT().Acquire(gomock.Any(), job.Source.Name, time.Minute).Return(job, nil)
					}
					fetcher.EXPECT().Fetch(gomock.Any(), job.Source).DoAndReturn(func(ctx context.Context, _ persistence.SourceDefinition) (ingest.Result, error) {
						if stage == "fetch" {
							return ingest.Result{}, blocked(ctx)
						}
						return ingest.Result{Snapshot: snapshotFixture(), Hash: "hash"}, nil
					})
					if stage != "fetch" {
						jobs.EXPECT().Status(gomock.Any(), job.Source.Name).DoAndReturn(func(ctx context.Context, _ string) (persistence.JobStatus, error) {
							if stage == "status" {
								return persistence.JobStatus{}, blocked(ctx)
							}
							return persistence.JobStatus{SourceID: job.Source.ID, BaselineValid: stage == "complete", LastHash: "hash", LastFilterHash: fmt.Sprintf("%x", sha256.Sum256([]byte("null")))}, nil
						})
					}
					if stage == "commit" {
						jobs.EXPECT().CommitSnapshot(gomock.Any(), job, gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, _ persistence.Job, _ persistence.SyncResult, _ model.Snapshot) error {
							return blocked(ctx)
						})
					}
					if stage == "complete" {
						jobs.EXPECT().Complete(gomock.Any(), job, gomock.Any()).DoAndReturn(func(ctx context.Context, _ persistence.Job, _ persistence.SyncResult) error {
							return blocked(ctx)
						})
					}
					jobs.EXPECT().Fail(gomock.Any(), job, persistence.FailureCancelled).DoAndReturn(func(ctx context.Context, _ persistence.Job, _ persistence.FailureCategory) error {
						require.NoError(t, ctx.Err())
						_, bounded := ctx.Deadline()
						require.True(t, bounded)
						switch failure {
						case "cleanup", "both":
							return errors.Join(context.Canceled, cleanupErr)
						case "stale mixed cleanup":
							return fmt.Errorf("cleanup: %w", errors.Join(persistence.ErrStale, context.Canceled, cleanupErr))
						case "stale cleanup":
							return persistence.ErrStale
						default:
							return nil
						}
					})
					c := newCoordinator(t, jobs, fetcher, time.Minute)
					done := make(chan error, 1)
					go func() {
						if mode == "run" {
							done <- c.Run(ctx)
						} else {
							done <- c.Sync(ctx, job.Source.Name)
						}
					}()
					select {
					case <-entered:
					case <-time.After(3 * time.Second):
						t.Fatal("attempt did not reach active operation")
					}
					cancel()
					close(release)
					select {
					case err := <-done:
						switch failure {
						case "backend", "stale cleanup":
							require.ErrorIs(t, err, backendErr)
						case "cleanup", "stale mixed cleanup":
							require.ErrorIs(t, err, cleanupErr)
						case "both":
							require.ErrorIs(t, err, backendErr)
							require.ErrorIs(t, err, cleanupErr)
						case "clean":
							if mode == "run" {
								require.NoError(t, err)
							} else {
								require.ErrorIs(t, err, context.Canceled)
							}
						}
					case <-time.After(3 * time.Second):
						t.Fatal("attempt did not join")
					}
					require.False(t, c.Ready())
				})
			}
		}
	}
}
