package operator_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	mcp "github.com/stacklok/toolhive/cmd/thv-operator/api/v1beta1"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/operator"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/reconcile"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/reconcile/mocks"
)

// syncCounts tracks how often a runner tried to sync (Acquire) and how often a
// sync committed, and how many servers the last commit carried.
type syncCounts struct {
	acquires, commits, servers atomic.Int32
}

// startCountingRunner runs a runner over api whose jobs always succeed, except
// that the nth Acquire returns acquireErr(n) when it is non-nil. The runner is
// stopped, and must return nil, before the mock controller finishes.
func startCountingRunner(
	t *testing.T, api client.WithWatch, poll time.Duration, acquireErr func(n int32) error, opts ...operator.RunnerOption,
) (*operator.Runner, *syncCounts) {
	t.Helper()
	jobs := mocks.NewMockJobs(gomock.NewController(t))
	job := persistence.Job{Source: definition(), LeaseID: "lease", Generation: 1}
	counts := &syncCounts{}
	jobs.EXPECT().Acquire(gomock.Any(), job.Source.Name, time.Minute).AnyTimes().DoAndReturn(
		func(context.Context, string, time.Duration) (persistence.Job, error) {
			if err := acquireErr(counts.acquires.Add(1)); err != nil {
				return persistence.Job{}, err
			}
			return job, nil
		})
	jobs.EXPECT().Status(gomock.Any(), job.Source.Name).AnyTimes().Return(persistence.JobStatus{SourceID: job.Source.ID}, nil)
	jobs.EXPECT().CommitSnapshot(gomock.Any(), job, gomock.Any(), gomock.Any()).AnyTimes().DoAndReturn(
		func(_ context.Context, _ persistence.Job, _ persistence.SyncResult, snapshot model.Snapshot) error {
			counts.servers.Store(int32(len(snapshot.Data.Servers)))
			counts.commits.Add(1)
			return nil
		})
	runner, err := operator.NewRunner(api, jobs, definition(), reconcile.Options{PollInterval: poll, LeaseDuration: time.Minute}, opts...)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- runner.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		require.NoError(t, <-done)
	})
	require.Eventually(t, func() bool { return runner.Ready() && counts.commits.Load() == 1 }, 3*time.Second, time.Millisecond)
	return runner, counts
}

func noAcquireErr(int32) error { return nil }

func exportedServer(name string) *mcp.MCPServer {
	return &mcp.MCPServer{ObjectMeta: metadata(name, "one"), Spec: mcp.MCPServerSpec{Image: "example.org/image:1.0.0"}}
}

func TestRunnerResyncInterval(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		opts   []operator.RunnerOption
		resync bool
	}{
		// PollInterval is tiny in every case: an idle runner must not poll at it.
		{name: "default waits for events", resync: false},
		{name: "short interval resyncs without events", opts: []operator.RunnerOption{operator.WithResyncInterval(10 * time.Millisecond)}, resync: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			api := fake.NewClientBuilder().WithScheme(scheme(t)).Build()
			runner, counts := startCountingRunner(t, api, 5*time.Millisecond, noAcquireErr, tc.opts...)
			if tc.resync {
				require.Eventually(t, func() bool { return counts.commits.Load() >= 3 }, 3*time.Second, time.Millisecond)
			} else {
				require.Never(t, func() bool { return counts.acquires.Load() > 1 }, 200*time.Millisecond, 5*time.Millisecond)
				require.EqualValues(t, 1, counts.commits.Load())
			}
			require.True(t, runner.Ready())
		})
	}
}

func TestRunnerRejectsNonPositiveResyncInterval(t *testing.T) {
	t.Parallel()
	api := fake.NewClientBuilder().WithScheme(scheme(t)).Build()
	jobs := mocks.NewMockJobs(gomock.NewController(t))
	for _, d := range []time.Duration{0, -time.Second} {
		_, err := operator.NewRunner(api, jobs, definition(), reconcile.Options{PollInterval: time.Second, LeaseDuration: time.Minute},
			operator.WithResyncInterval(d))
		require.ErrorIs(t, err, persistence.ErrInvalid, "interval %v", d)
	}
}

func TestRunnerWatchEventSyncsPromptly(t *testing.T) {
	t.Parallel()
	api := fake.NewClientBuilder().WithScheme(scheme(t)).Build()
	// Long poll and resync intervals: only the watch event can trigger the sync.
	_, counts := startCountingRunner(t, api, time.Hour, noAcquireErr, operator.WithResyncInterval(time.Hour))
	require.Zero(t, counts.servers.Load())
	require.NoError(t, api.Create(t.Context(), exportedServer("created")))
	require.Eventually(t, func() bool { return counts.commits.Load() == 2 }, 3*time.Second, time.Millisecond)
	require.EqualValues(t, 1, counts.servers.Load(), "the event-triggered sync lists the new object")
}

func TestRunnerFailedSyncAfterReadyRetriesAtPollInterval(t *testing.T) {
	t.Parallel()
	api := fake.NewClientBuilder().WithScheme(scheme(t)).Build()
	// The 2nd and 3rd attempts find the lease busy; the 4th succeeds.
	busy := func(n int32) error {
		if n == 2 || n == 3 {
			return persistence.ErrBusy
		}
		return nil
	}
	runner, counts := startCountingRunner(t, api, 5*time.Millisecond, busy, operator.WithResyncInterval(time.Hour))
	require.NoError(t, api.Create(t.Context(), exportedServer("created")))
	require.Eventually(t, func() bool { return counts.commits.Load() == 2 }, 3*time.Second, time.Millisecond,
		"failed syncs must retry at PollInterval, not the hour-long resync interval")
	require.EqualValues(t, 4, counts.acquires.Load())
	require.EqualValues(t, 1, counts.servers.Load())
	require.True(t, runner.Ready(), "a transient failure after ready keeps the last good snapshot")
	// Once a sync succeeds again the runner goes back to waiting for events.
	require.Never(t, func() bool { return counts.acquires.Load() > 4 }, 200*time.Millisecond, 5*time.Millisecond)
}
