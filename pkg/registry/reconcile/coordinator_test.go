package reconcile_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	thv "github.com/stacklok/toolhive-core/registry/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/ingest"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/reconcile"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/reconcile/mocks"
)

func jobFixture() persistence.Job {
	return persistence.Job{Source: persistence.SourceDefinition{ID: "incarnation", Name: "test", API: &persistence.APISpec{Endpoint: "https://example.org"}, Schedule: "1h"}, LeaseID: "lease", Generation: 1}
}
func snapshotFixture() model.Snapshot {
	var s model.Snapshot
	s.Data.Servers = []model.Server{{Schema: "https://example.org/server.schema.json", Name: "io.example/server", Version: "1", Description: "Server"}}
	s.Data.Skills = []thv.Skill{{Namespace: "io.example", Name: "skill", Version: "1", Description: "Skill"}}
	s.Data.Plugins = []thv.Plugin{{Namespace: "io.example", Name: "plugin", Version: "1", Description: "Plugin"}}
	return s
}
func newCoordinator(t *testing.T, jobs persistence.Jobs, fetcher reconcile.Fetcher, ttl time.Duration) *reconcile.Coordinator {
	t.Helper()
	c, err := reconcile.New(jobs, fetcher, reconcile.Options{PollInterval: 20 * time.Millisecond, LeaseDuration: ttl})
	require.NoError(t, err)
	return c
}

func TestSnapshotDecisions(t *testing.T) {
	t.Parallel()
	filterHash := fmt.Sprintf("%x", sha256.Sum256([]byte("null")))
	for _, tc := range []struct {
		name             string
		baseline         bool
		hash, filterHash string
		filter           *persistence.Filter
		completeErr      error
		wantComplete     bool
	}{
		{name: "first snapshot"},
		{name: "invalidated baseline", hash: "hash", filterHash: filterHash},
		{name: "unchanged", baseline: true, hash: "hash", filterHash: filterHash, wantComplete: true},
		{name: "changed hash", baseline: true, hash: "old", filterHash: filterHash},
		{name: "changed filter", baseline: true, hash: "hash", filterHash: filterHash, filter: &persistence.Filter{Names: &persistence.NameFilter{Include: []string{"io.example/skill"}}}},
		{name: "complete conflict falls back", baseline: true, hash: "hash", filterHash: filterHash, wantComplete: true, completeErr: persistence.ErrConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			jobs := mocks.NewMockJobs(ctrl)
			fetcher := mocks.NewMockFetcher(ctrl)
			job := jobFixture()
			job.Source.Filter = tc.filter
			jobs.EXPECT().Acquire(gomock.Any(), job.Source.Name, time.Minute).Return(job, nil)
			fetcher.EXPECT().Fetch(gomock.Any(), job.Source).Return(ingest.Result{Snapshot: snapshotFixture(), Hash: "hash"}, nil)
			jobs.EXPECT().Status(gomock.Any(), job.Source.Name).Return(persistence.JobStatus{SourceID: job.Source.ID, BaselineValid: tc.baseline, LastHash: tc.hash, LastFilterHash: tc.filterHash}, nil)
			if tc.wantComplete {
				jobs.EXPECT().Complete(gomock.Any(), job, gomock.Any()).Return(tc.completeErr)
			}
			if !tc.wantComplete || tc.completeErr != nil {
				jobs.EXPECT().CommitSnapshot(gomock.Any(), job, gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, _ persistence.Job, result persistence.SyncResult, snapshot model.Snapshot) error {
					require.Equal(t, "hash", result.Hash)
					require.NotEmpty(t, result.FilterHash)
					require.Len(t, snapshot.Data.Skills, 1)
					if tc.filter == nil {
						require.Len(t, snapshot.Data.Servers, 1)
						require.Len(t, snapshot.Data.Plugins, 1)
					} else {
						require.Empty(t, snapshot.Data.Servers)
						require.Empty(t, snapshot.Data.Plugins)
					}
					return nil
				})
			}
			c := newCoordinator(t, jobs, fetcher, time.Minute)
			require.NoError(t, c.Sync(t.Context(), job.Source.Name))
			require.False(t, c.Ready())
		})
	}
}

func TestFailureCategories(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                string
		fetchErr, commitErr error
		category            persistence.FailureCategory
		want                error
	}{
		{"fetch", errors.New("password=secret"), nil, persistence.FailureFetch, ingest.ErrFetch},
		{"validation", persistence.ErrInvalid, nil, persistence.FailureValidation, persistence.ErrInvalid},
		{"storage", nil, persistence.ErrUnavailable, persistence.FailureStorage, persistence.ErrUnavailable},
		{"stale", nil, persistence.ErrStale, "", persistence.ErrStale},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			jobs := mocks.NewMockJobs(ctrl)
			fetcher := mocks.NewMockFetcher(ctrl)
			job := jobFixture()
			jobs.EXPECT().Acquire(gomock.Any(), job.Source.Name, time.Minute).Return(job, nil)
			fetcher.EXPECT().Fetch(gomock.Any(), job.Source).Return(ingest.Result{Snapshot: snapshotFixture(), Hash: "hash"}, tc.fetchErr)
			if tc.fetchErr == nil {
				jobs.EXPECT().Status(gomock.Any(), job.Source.Name).Return(persistence.JobStatus{SourceID: job.Source.ID}, nil)
				jobs.EXPECT().CommitSnapshot(gomock.Any(), job, gomock.Any(), gomock.Any()).Return(tc.commitErr)
			}
			if tc.category != "" {
				jobs.EXPECT().Fail(gomock.Any(), job, tc.category).Return(nil)
			}
			err := newCoordinator(t, jobs, fetcher, time.Minute).Sync(t.Context(), job.Source.Name)
			require.ErrorIs(t, err, tc.want)
			require.NotContains(t, err.Error(), "secret")
		})
	}
}

func TestRenewAndCancellation(t *testing.T) {
	t.Parallel()
	for _, phase := range []string{"fetch", "commit"} {
		for _, mode := range []string{"cancel", "renew deadline", "renew fails", "renew mixed", "renew stale", "renew succeeds"} {
			t.Run(phase+"/"+mode, func(t *testing.T) {
				t.Parallel()
				ctrl := gomock.NewController(t)
				jobs := mocks.NewMockJobs(ctrl)
				fetcher := mocks.NewMockFetcher(ctrl)
				job := jobFixture()
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				ttl := 60 * time.Millisecond
				jobs.EXPECT().Acquire(gomock.Any(), job.Source.Name, ttl).Return(job, nil)
				var renewCalls atomic.Int32
				renewErr := error(nil)
				if mode == "renew deadline" {
					renewErr = context.DeadlineExceeded
				}
				if mode == "renew fails" {
					renewErr = persistence.ErrUnavailable
				}
				if mode == "renew mixed" {
					renewErr = errors.Join(context.Canceled, persistence.ErrUnavailable)
				}
				if mode == "renew stale" {
					renewErr = persistence.ErrStale
				}
				jobs.EXPECT().Renew(gomock.Any(), job, ttl).AnyTimes().DoAndReturn(func(ctx context.Context, j persistence.Job, _ time.Duration) (persistence.Job, error) {
					renewCalls.Add(1)
					if mode == "cancel" {
						cancel()
						<-ctx.Done()
						return j, ctx.Err()
					}
					if mode == "renew deadline" {
						<-ctx.Done()
						return j, ctx.Err()
					}
					return j, renewErr
				})
				block := func(ctx context.Context) error {
					if mode == "renew succeeds" {
						require.Eventually(t, func() bool { return renewCalls.Load() > 0 }, time.Second, time.Millisecond)
						return nil
					}
					<-ctx.Done()
					return ctx.Err()
				}
				fetcher.EXPECT().Fetch(gomock.Any(), job.Source).DoAndReturn(func(ctx context.Context, _ persistence.SourceDefinition) (ingest.Result, error) {
					if phase == "fetch" {
						if err := block(ctx); err != nil {
							return ingest.Result{}, err
						}
					}
					return ingest.Result{Snapshot: snapshotFixture(), Hash: "hash"}, nil
				})
				if phase == "commit" || mode == "renew succeeds" {
					jobs.EXPECT().Status(gomock.Any(), job.Source.Name).Return(persistence.JobStatus{SourceID: job.Source.ID}, nil)
					jobs.EXPECT().CommitSnapshot(gomock.Any(), job, gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, _ persistence.Job, _ persistence.SyncResult, _ model.Snapshot) error {
						if phase == "commit" {
							return block(ctx)
						}
						return nil
					})
				}
				if mode == "cancel" || mode == "renew deadline" || mode == "renew fails" || mode == "renew mixed" {
					category := persistence.FailureStorage
					if mode == "cancel" {
						category = persistence.FailureCancelled
					}
					jobs.EXPECT().Fail(gomock.Any(), job, category).DoAndReturn(func(ctx context.Context, _ persistence.Job, _ persistence.FailureCategory) error {
						require.NoError(t, ctx.Err())
						_, ok := ctx.Deadline()
						require.True(t, ok)
						return nil
					})
				}
				err := newCoordinator(t, jobs, fetcher, ttl).Sync(ctx, job.Source.Name)
				switch mode {
				case "renew succeeds":
					require.NoError(t, err)
				case "cancel":
					require.ErrorIs(t, err, context.Canceled)
				case "renew deadline":
					require.NoError(t, ctx.Err())
					require.ErrorIs(t, err, context.DeadlineExceeded)
					require.ErrorIs(t, err, persistence.ErrUnavailable)
				default:
					require.ErrorIs(t, err, renewErr)
				}
			})
		}
	}
}

func TestRunRetriesAndIsolatesSources(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	jobs, fetcher := mocks.NewMockJobs(ctrl), mocks.NewMockFetcher(ctrl)
	c := newCoordinator(t, jobs, fetcher, time.Minute)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	failed, healthy := jobFixture(), jobFixture()
	healthy.Source.Name, healthy.Source.ID, healthy.LeaseID = "other", "other-id", "other-lease"
	gomock.InOrder(
		jobs.EXPECT().ClaimNext(gomock.Any(), time.Minute).Return(failed, true, nil),
		fetcher.EXPECT().Fetch(gomock.Any(), failed.Source).Return(ingest.Result{}, errors.New("secret")),
		jobs.EXPECT().Fail(gomock.Any(), failed, persistence.FailureFetch).Return(nil),
		jobs.EXPECT().ClaimNext(gomock.Any(), time.Minute).DoAndReturn(func(context.Context, time.Duration) (persistence.Job, bool, error) {
			require.True(t, c.Ready())
			return healthy, true, nil
		}),
		fetcher.EXPECT().Fetch(gomock.Any(), healthy.Source).Return(ingest.Result{Snapshot: snapshotFixture(), Hash: "healthy"}, nil),
		jobs.EXPECT().Status(gomock.Any(), healthy.Source.Name).Return(persistence.JobStatus{SourceID: healthy.Source.ID}, nil),
		jobs.EXPECT().CommitSnapshot(gomock.Any(), healthy, gomock.Any(), gomock.Any()).DoAndReturn(func(context.Context, persistence.Job, persistence.SyncResult, model.Snapshot) error {
			cancel()
			return nil
		}),
	)
	require.NoError(t, c.Run(ctx))
	require.False(t, c.Ready())
}

func TestStatusDuringAcquiredWork(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name            string
		status          persistence.JobStatus
		statusErr, want error
	}{
		{"deleted during fetch", persistence.JobStatus{}, persistence.ErrNotFound, persistence.ErrStale},
		{"recreated during fetch", persistence.JobStatus{SourceID: "new-incarnation"}, nil, persistence.ErrStale},
		{"backend unavailable", persistence.JobStatus{}, persistence.ErrUnavailable, persistence.ErrUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			jobs := mocks.NewMockJobs(gomock.NewController(t))
			fetcher := mocks.NewMockFetcher(gomock.NewController(t))
			job := jobFixture()
			gomock.InOrder(
				jobs.EXPECT().Acquire(gomock.Any(), job.Source.Name, time.Minute).Return(job, nil),
				fetcher.EXPECT().Fetch(gomock.Any(), job.Source).Return(ingest.Result{Snapshot: snapshotFixture(), Hash: "hash"}, nil),
				jobs.EXPECT().Status(gomock.Any(), job.Source.Name).Return(tc.status, tc.statusErr),
			)
			if tc.want == persistence.ErrUnavailable {
				jobs.EXPECT().Fail(gomock.Any(), job, persistence.FailureStorage).Return(persistence.ErrStale)
			}
			require.ErrorIs(t, newCoordinator(t, jobs, fetcher, time.Minute).Sync(t.Context(), job.Source.Name), tc.want)
		})
	}
}

func TestRunStatusFailureIsolation(t *testing.T) {
	t.Parallel()
	for _, statusErr := range []error{persistence.ErrNotFound, persistence.ErrUnavailable} {
		t.Run(statusErr.Error(), func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			jobs, fetcher := mocks.NewMockJobs(ctrl), mocks.NewMockFetcher(ctrl)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			deleted, healthy := jobFixture(), jobFixture()
			healthy.Source.Name, healthy.Source.ID = "other", "other-id"
			gomock.InOrder(
				jobs.EXPECT().ClaimNext(gomock.Any(), time.Minute).Return(deleted, true, nil),
				fetcher.EXPECT().Fetch(gomock.Any(), deleted.Source).Return(ingest.Result{Snapshot: snapshotFixture(), Hash: "hash"}, nil),
				jobs.EXPECT().Status(gomock.Any(), deleted.Source.Name).Return(persistence.JobStatus{}, statusErr),
			)
			if statusErr == persistence.ErrUnavailable {
				jobs.EXPECT().Fail(gomock.Any(), deleted, persistence.FailureStorage).Return(persistence.ErrStale)
			} else {
				gomock.InOrder(
					jobs.EXPECT().ClaimNext(gomock.Any(), time.Minute).Return(healthy, true, nil),
					fetcher.EXPECT().Fetch(gomock.Any(), healthy.Source).Return(ingest.Result{Snapshot: snapshotFixture(), Hash: "healthy"}, nil),
					jobs.EXPECT().Status(gomock.Any(), healthy.Source.Name).Return(persistence.JobStatus{SourceID: healthy.Source.ID}, nil),
					jobs.EXPECT().CommitSnapshot(gomock.Any(), healthy, gomock.Any(), gomock.Any()).DoAndReturn(func(context.Context, persistence.Job, persistence.SyncResult, model.Snapshot) error {
						cancel()
						return nil
					}),
				)
			}
			c := newCoordinator(t, jobs, fetcher, time.Minute)
			err := c.Run(ctx)
			if statusErr == persistence.ErrUnavailable {
				require.ErrorIs(t, err, persistence.ErrUnavailable)
			} else {
				require.NoError(t, err)
			}
			require.False(t, c.Ready())
		})
	}
}

func TestFiltersDoNotHideInvalidSnapshots(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"fully filtered", "malformed filter", "invalid excluded entry"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			jobs, fetcher := mocks.NewMockJobs(ctrl), mocks.NewMockFetcher(ctrl)
			job := jobFixture()
			job.Source.Filter = &persistence.Filter{Names: &persistence.NameFilter{Include: []string{"excluded/*"}}}
			snapshot := snapshotFixture()
			if mode == "malformed filter" {
				job.Source.Filter.Names.Include = []string{"["}
			}
			if mode == "invalid excluded entry" {
				snapshot.Data.Servers[0].Version = ""
			}
			jobs.EXPECT().Acquire(gomock.Any(), job.Source.Name, time.Minute).Return(job, nil)
			fetcher.EXPECT().Fetch(gomock.Any(), job.Source).Return(ingest.Result{Snapshot: snapshot, Hash: "hash"}, nil)
			if mode == "fully filtered" {
				jobs.EXPECT().Status(gomock.Any(), job.Source.Name).Return(persistence.JobStatus{SourceID: job.Source.ID}, nil)
				jobs.EXPECT().CommitSnapshot(gomock.Any(), job, gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, _ persistence.Job, _ persistence.SyncResult, snapshot model.Snapshot) error {
					require.Empty(t, snapshot.Data.Servers)
					require.Empty(t, snapshot.Data.Skills)
					require.Empty(t, snapshot.Data.Plugins)
					return nil
				})
			} else {
				jobs.EXPECT().Fail(gomock.Any(), job, persistence.FailureValidation).Return(nil)
			}
			err := newCoordinator(t, jobs, fetcher, time.Minute).Sync(t.Context(), job.Source.Name)
			if mode == "fully filtered" {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, persistence.ErrInvalid)
			}
		})
	}
}

func TestConstructorRejectsInvalidOptions(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	jobs, fetcher := mocks.NewMockJobs(ctrl), mocks.NewMockFetcher(ctrl)
	for _, options := range []reconcile.Options{
		{}, {PollInterval: -time.Second, LeaseDuration: time.Minute},
		{PollInterval: time.Second, LeaseDuration: time.Nanosecond},
		{PollInterval: time.Second, LeaseDuration: 25 * time.Hour},
	} {
		_, err := reconcile.New(jobs, fetcher, options)
		require.ErrorIs(t, err, persistence.ErrInvalid)
	}
}

func TestRunLifecycle(t *testing.T) {
	t.Parallel()
	for _, fatal := range []bool{false, true} {
		t.Run(fmt.Sprint(fatal), func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			jobs := mocks.NewMockJobs(ctrl)
			fetcher := mocks.NewMockFetcher(ctrl)
			c := newCoordinator(t, jobs, fetcher, time.Minute)
			require.False(t, c.Ready())
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if fatal {
				jobs.EXPECT().ClaimNext(gomock.Any(), time.Minute).Return(persistence.Job{}, false, persistence.ErrUnavailable)
				require.ErrorIs(t, c.Run(ctx), persistence.ErrUnavailable)
			} else {
				var calls atomic.Int32
				jobs.EXPECT().ClaimNext(gomock.Any(), time.Minute).AnyTimes().DoAndReturn(func(context.Context, time.Duration) (persistence.Job, bool, error) {
					calls.Add(1)
					return persistence.Job{}, false, nil
				})
				done := make(chan error, 1)
				started := time.Now()
				go func() { done <- c.Run(ctx) }()
				require.Eventually(t, c.Ready, time.Second, time.Millisecond)
				require.ErrorIs(t, c.Run(ctx), persistence.ErrBusy)
				cancel()
				require.NoError(t, <-done)
				require.LessOrEqual(t, int64(calls.Load()), int64(time.Since(started)/(20*time.Millisecond))+2)
			}
			require.False(t, c.Ready())
		})
	}
}
