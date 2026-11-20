package reconcile_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/ingest"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/reconcile/mocks"
)

// The fetch has no overall deadline (timeouts are per request), so a fetch can
// outlast the lease. The coordinator renews the lease while it works and the
// attempt still commits.
func TestFetchLongerThanLeaseCommits(t *testing.T) {
	t.Parallel()
	const ttl = 60 * time.Millisecond
	ctrl := gomock.NewController(t)
	jobs, fetcher := mocks.NewMockJobs(ctrl), mocks.NewMockFetcher(ctrl)
	job := jobFixture()
	var renewals atomic.Int32
	jobs.EXPECT().Acquire(gomock.Any(), job.Source.Name, ttl).Return(job, nil)
	jobs.EXPECT().Renew(gomock.Any(), job, ttl).MinTimes(4).DoAndReturn(
		func(_ context.Context, j persistence.Job, _ time.Duration) (persistence.Job, error) {
			renewals.Add(1)
			return j, nil
		})
	fetcher.EXPECT().Fetch(gomock.Any(), job.Source).DoAndReturn(
		func(ctx context.Context, _ persistence.SourceDefinition) (ingest.Result, error) {
			started := time.Now()
			require.Eventually(t, func() bool { return renewals.Load() >= 4 }, 2*time.Second, time.Millisecond)
			require.Greater(t, time.Since(started), ttl, "the fetch outlasted one lease duration")
			require.NoError(t, ctx.Err())
			return ingest.Result{Snapshot: snapshotFixture(), Hash: "hash"}, nil
		})
	jobs.EXPECT().Status(gomock.Any(), job.Source.Name).Return(persistence.JobStatus{SourceID: job.Source.ID}, nil)
	jobs.EXPECT().CommitSnapshot(gomock.Any(), job, gomock.Any(), gomock.Any()).Return(nil)
	require.NoError(t, newCoordinator(t, jobs, fetcher, ttl).Sync(t.Context(), job.Source.Name))
}
