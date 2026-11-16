package standalone_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/internal/sync/standalone"
	standalonemocks "github.com/stacklok/toolhive-registry-server/internal/sync/standalone/mocks"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/ingest"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/reconcile"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/reconcile/mocks"
)

//go:generate mockgen -destination=mocks/git.go -package=mocks github.com/stacklok/toolhive-registry-server/pkg/registry/ingest GitReader

func options() standalone.Options {
	return standalone.Options{Reconcile: reconcile.Options{PollInterval: time.Hour, LeaseDuration: time.Minute},
		Operator:  func(persistence.SourceDefinition) (standalone.Worker, error) { return &worker{}, nil },
		Preflight: func(context.Context, persistence.SourceDefinition) error { return nil }}
}

type worker struct {
	started  chan struct{}
	stopping chan struct{}
	join     chan struct{}
	fatal    chan error
	ready    atomic.Bool
}

func (w *worker) Ready() bool { return w.ready.Load() }
func (w *worker) Run(ctx context.Context) error {
	w.ready.Store(true)
	defer w.ready.Store(false)
	if w.started != nil {
		close(w.started)
	}
	select {
	case err := <-w.fatal:
		return err
	case <-ctx.Done():
		if w.stopping != nil {
			close(w.stopping)
		}
		if w.join != nil {
			<-w.join
		}
		return nil
	}
}
func run(t *testing.T, r *standalone.Runtime) (context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()
	t.Cleanup(cancel)
	return cancel, done
}
func wait(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not reach lifecycle barrier")
	}
}
func operatorDefinition(id string, namespaces ...string) persistence.SourceDefinition {
	return persistence.SourceDefinition{ID: id, Name: "operator", Origin: persistence.OriginAPI, Kubernetes: &persistence.KubernetesSpec{Namespaces: namespaces}}
}

func TestDormantConstructionAndReadOnlyPreflight(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	sources, jobs, fetcher := standalonemocks.NewMockSources(ctrl), mocks.NewMockJobs(ctrl), mocks.NewMockFetcher(ctrl)
	defs := []persistence.SourceDefinition{
		operatorDefinition("api-unlinked"),
		{ID: "config-linked", Name: "inline", Origin: persistence.OriginConfig, File: &persistence.FileSpec{Data: "persisted"}},
		{ID: "api-linked", Name: "upstream", Origin: persistence.OriginAPI, API: &persistence.APISpec{Endpoint: "https://example.org"}, Schedule: "1h"},
		{ID: "config-unlinked", Name: "managed", Origin: persistence.OriginConfig, Managed: &persistence.ManagedSpec{}},
	}
	opts := options()
	var checked []persistence.SourceDefinition
	opts.Preflight = func(_ context.Context, d persistence.SourceDefinition) error {
		checked = append(checked, d)
		return nil
	}
	r, err := standalone.New(sources, jobs, fetcher, opts)
	require.NoError(t, err)
	require.False(t, r.Ready())
	sources.EXPECT().ListSources(gomock.Any()).Return(defs, nil)
	require.NoError(t, r.Preflight(t.Context()))
	require.Equal(t, defs, checked)
	defs[1].Filter = &persistence.Filter{}
	sources.EXPECT().ListSources(gomock.Any()).Return(defs, nil)
	require.ErrorIs(t, r.Preflight(t.Context()), persistence.ErrInvalid)
	require.ErrorIs(t, r.Refresh(t.Context()), persistence.ErrBusy)
}

func TestOwnershipReplacementJoinsBeforeStarting(t *testing.T) {
	t.Parallel()
	for _, change := range []string{"scope", "incarnation", "delete"} {
		t.Run(change, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			sources, jobs, fetcher := standalonemocks.NewMockSources(ctrl), mocks.NewMockJobs(ctrl), mocks.NewMockFetcher(ctrl)
			first := operatorDefinition("old", "one", "two")
			sources.EXPECT().ListSources(gomock.Any()).Return([]persistence.SourceDefinition{first}, nil)
			jobs.EXPECT().ClaimNext(gomock.Any(), time.Minute).Return(persistence.Job{}, false, nil)
			old := &worker{started: make(chan struct{}), stopping: make(chan struct{}), join: make(chan struct{})}
			newWorker := &worker{started: make(chan struct{})}
			var factories atomic.Int32
			opts := options()
			opts.Operator = func(persistence.SourceDefinition) (standalone.Worker, error) {
				if factories.Add(1) == 1 {
					return old, nil
				}
				return newWorker, nil
			}
			r, err := standalone.New(sources, jobs, fetcher, opts)
			require.NoError(t, err)
			cancel, done := run(t, r)
			wait(t, old.started)
			require.Eventually(t, r.Ready, time.Second, time.Millisecond)
			// Canonical ordering is not a scope change and caller cancellation
			// after Refresh must not cancel a worker owned by Run.
			reordered := operatorDefinition("old", "two", "one")
			sources.EXPECT().ListSources(gomock.Any()).Return([]persistence.SourceDefinition{reordered}, nil)
			request, requestCancel := context.WithCancel(t.Context())
			require.NoError(t, r.Refresh(request))
			requestCancel()
			require.EqualValues(t, 1, factories.Load())
			require.True(t, r.Ready())
			// Listing failure is not deletion.
			sources.EXPECT().ListSources(gomock.Any()).Return(nil, persistence.ErrUnavailable)
			require.ErrorIs(t, r.Refresh(t.Context()), persistence.ErrUnavailable)
			require.True(t, old.Ready())
			next := operatorDefinition("old", "one")
			if change == "incarnation" {
				next = operatorDefinition("recreated", "one", "two")
			}
			desired := []persistence.SourceDefinition{next}
			if change == "delete" {
				desired = nil
			}
			sources.EXPECT().ListSources(gomock.Any()).Return(desired, nil)
			refreshed := make(chan error, 1)
			go func() { refreshed <- r.Refresh(t.Context()) }()
			wait(t, old.stopping)
			require.False(t, r.Ready())
			require.EqualValues(t, 1, factories.Load())
			close(old.join)
			require.NoError(t, <-refreshed)
			if change != "delete" {
				wait(t, newWorker.started)
				require.EqualValues(t, 2, factories.Load())
			}
			cancel()
			require.NoError(t, <-done)
			require.False(t, r.Ready())
		})
	}
}

func TestTerminalWorkerFailureCancelsAndJoinsSiblings(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	sources, jobs, fetcher := standalonemocks.NewMockSources(ctrl), mocks.NewMockJobs(ctrl), mocks.NewMockFetcher(ctrl)
	one, two := operatorDefinition("one"), operatorDefinition("two")
	two.Name = "other"
	sources.EXPECT().ListSources(gomock.Any()).Return([]persistence.SourceDefinition{one, two}, nil)
	jobs.EXPECT().ClaimNext(gomock.Any(), time.Minute).Return(persistence.Job{}, false, nil).AnyTimes()
	failed := &worker{started: make(chan struct{}), fatal: make(chan error, 1)}
	sibling := &worker{started: make(chan struct{}), stopping: make(chan struct{}), join: make(chan struct{})}
	opts := options()
	opts.Operator = func(d persistence.SourceDefinition) (standalone.Worker, error) {
		if d.ID == "one" {
			return failed, nil
		}
		return sibling, nil
	}
	r, err := standalone.New(sources, jobs, fetcher, opts)
	require.NoError(t, err)
	_, done := run(t, r)
	wait(t, failed.started)
	wait(t, sibling.started)
	fatal := errors.New("terminal watch failure")
	failed.fatal <- fatal
	wait(t, sibling.stopping)
	select {
	case <-done:
		t.Fatal("Run returned before sibling joined")
	default:
	}
	close(sibling.join)
	require.ErrorIs(t, <-done, fatal)
	require.False(t, r.Ready())
}

func receive(t *testing.T, ch <-chan error) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(3 * time.Second):
		t.Fatal("runtime did not finish")
		return nil
	}
}

func TestDiscoveryTerminalFailureCancelsAndJoins(t *testing.T) {
	t.Parallel()
	for _, failure := range []string{"operator", "poller"} {
		for _, discovery := range []string{"refresh", "tick", "scope-mismatch"} {
			if failure == "operator" && discovery == "scope-mismatch" {
				continue
			}
			t.Run(failure+"/"+discovery, func(t *testing.T) {
				t.Parallel()
				ctrl := gomock.NewController(t)
				sources, jobs, fetcher := standalonemocks.NewMockSources(ctrl), mocks.NewMockJobs(ctrl), mocks.NewMockFetcher(ctrl)
				one, two := operatorDefinition("one"), operatorDefinition("two")
				two.Name = "other"
				failed := &worker{started: make(chan struct{}), fatal: make(chan error, 1)}
				sibling := &worker{started: make(chan struct{}), stopping: make(chan struct{}), join: make(chan struct{})}
				releaseSibling := sync.OnceFunc(func() { close(sibling.join) })
				t.Cleanup(releaseSibling)
				opts := options()
				if discovery == "tick" {
					opts.Reconcile.PollInterval = 10 * time.Millisecond
				}
				opts.Operator = func(d persistence.SourceDefinition) (standalone.Worker, error) {
					if d.ID == "one" {
						return failed, nil
					}
					return sibling, nil
				}
				listed, cancelled, joinList := make(chan struct{}), make(chan struct{}), make(chan struct{})
				releaseList := sync.OnceFunc(func() { close(joinList) })
				t.Cleanup(releaseList)
				gomock.InOrder(
					sources.EXPECT().ListSources(gomock.Any()).Return([]persistence.SourceDefinition{one, two}, nil),
					sources.EXPECT().ListSources(gomock.Any()).DoAndReturn(func(ctx context.Context) ([]persistence.SourceDefinition, error) {
						close(listed)
						<-ctx.Done()
						close(cancelled)
						<-joinList
						return nil, ctx.Err()
					}),
				)
				fatal := errors.New("original operator failure")
				if failure == "poller" {
					fatal = persistence.ErrUnavailable
					jobs.EXPECT().ClaimNext(gomock.Any(), time.Minute).DoAndReturn(func(ctx context.Context, _ time.Duration) (persistence.Job, bool, error) {
						select {
						case <-listed:
							return persistence.Job{}, false, fatal
						case <-ctx.Done():
							return persistence.Job{}, false, ctx.Err()
						}
					})
				} else {
					jobs.EXPECT().ClaimNext(gomock.Any(), time.Minute).Return(persistence.Job{}, false, nil).AnyTimes()
				}
				r, err := standalone.New(sources, jobs, fetcher, opts)
				require.NoError(t, err)
				_, done := run(t, r)
				wait(t, failed.started)
				wait(t, sibling.started)
				var refreshed chan error
				switch discovery {
				case "refresh":
					refreshed = make(chan error, 1)
					go func() { refreshed <- r.Refresh(t.Context()) }()
				case "scope-mismatch":
					failed.fatal <- persistence.ErrInvalid
				}
				wait(t, listed)
				if failure == "operator" {
					failed.fatal <- fatal
				}
				wait(t, cancelled)
				wait(t, sibling.stopping)
				select {
				case <-done:
					t.Fatal("Run returned before discovery and sibling joined")
				default:
				}
				// Release discovery, but keep the sibling's join blocked.
				releaseList()
				if refreshed != nil {
					require.ErrorIs(t, receive(t, refreshed), fatal)
				}
				select {
				case <-done:
					t.Fatal("Run returned before sibling joined")
				default:
				}
				releaseSibling()
				require.ErrorIs(t, receive(t, done), fatal)
				require.False(t, r.Ready())
			})
		}
	}
}

const inlineData = `{"version":"1.0.0","meta":{"last_updated":"2025-01-15T10:30:00Z"},"data":{"servers":[{"$schema":"https://static.modelcontextprotocol.io/schemas/2025-07-09/server.schema.json","name":"com.example/server","description":"persisted","version":"1.0.0"}]}}`

func TestSyncInlineUsesLeasedPersistedData(t *testing.T) {
	t.Parallel()
	for _, invalid := range []bool{false, true} {
		t.Run(map[bool]string{false: "valid", true: "invalid"}[invalid], func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			sources, jobs := standalonemocks.NewMockSources(ctrl), mocks.NewMockJobs(ctrl)
			d := persistence.SourceDefinition{ID: "inline", Name: "inline", File: &persistence.FileSpec{Data: inlineData}}
			if invalid {
				d.File.Data = "invalid persisted data"
			}
			job := persistence.Job{Source: d, LeaseID: "acquired", Generation: 2}
			jobs.EXPECT().Acquire(gomock.Any(), d.Name, time.Minute).Return(job, nil)
			if invalid {
				jobs.EXPECT().Fail(gomock.Any(), job, persistence.FailureValidation).Return(nil)
			} else {
				jobs.EXPECT().Status(gomock.Any(), d.Name).Return(persistence.JobStatus{SourceID: d.ID}, nil)
				jobs.EXPECT().CommitSnapshot(gomock.Any(), job, gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, _ persistence.Job, _ persistence.SyncResult, snapshot model.Snapshot) error {
					require.Equal(t, "persisted", snapshot.Data.Servers[0].Description)
					return nil
				})
			}
			fetcher := ingest.New(ingest.Options{ReadFile: func(context.Context, string) ([]byte, error) { t.Fatal("inline must not read a file"); return nil, nil }})
			r, err := standalone.New(sources, jobs, fetcher, options())
			require.NoError(t, err)
			err = r.SyncInline(t.Context(), d.Name)
			if invalid {
				require.ErrorIs(t, err, persistence.ErrInvalid)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestInlineStartupAndMissedNotificationRecoverWithoutRepeatedSnapshots(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	sources, jobs := standalonemocks.NewMockSources(ctrl), mocks.NewMockJobs(ctrl)
	d := persistence.SourceDefinition{ID: "inline", Name: "inline", Origin: persistence.OriginConfig, File: &persistence.FileSpec{Data: inlineData}}
	managed := persistence.SourceDefinition{ID: "managed", Name: "managed", Managed: &persistence.ManagedSpec{}}
	sources.EXPECT().ListSources(gomock.Any()).Return([]persistence.SourceDefinition{d, managed}, nil).AnyTimes()
	jobs.EXPECT().ClaimNext(gomock.Any(), time.Minute).Return(persistence.Job{}, false, nil).AnyTimes()
	var baseline atomic.Bool
	var commits atomic.Int32
	jobs.EXPECT().Status(gomock.Any(), d.Name).DoAndReturn(func(context.Context, string) (persistence.JobStatus, error) {
		return persistence.JobStatus{SourceID: d.ID, BaselineValid: baseline.Load()}, nil
	}).AnyTimes()
	job := persistence.Job{Source: d, LeaseID: "lease", Generation: 1}
	jobs.EXPECT().Acquire(gomock.Any(), d.Name, time.Minute).Return(job, nil).Times(2)
	jobs.EXPECT().CommitSnapshot(gomock.Any(), job, gomock.Any(), gomock.Any()).DoAndReturn(func(context.Context, persistence.Job, persistence.SyncResult, model.Snapshot) error {
		commits.Add(1)
		baseline.Store(true)
		return nil
	}).Times(2)
	opts := options()
	opts.Reconcile.PollInterval = 10 * time.Millisecond
	r, err := standalone.New(sources, jobs, ingest.New(ingest.Options{}), opts)
	require.NoError(t, err)
	cancel, done := run(t, r)
	require.Eventually(t, r.Ready, time.Second, time.Millisecond)
	require.EqualValues(t, 1, commits.Load())
	// Represents a definition-generation change/direct baseline invalidation
	// without a service notification. No new scheduler/hash state is required.
	baseline.Store(false)
	require.Eventually(t, func() bool { return commits.Load() == 2 && r.Ready() }, time.Second, time.Millisecond)
	time.Sleep(40 * time.Millisecond)
	require.EqualValues(t, 2, commits.Load())
	cancel()
	require.NoError(t, <-done)
}

func TestScheduledPrivateGitAndFiltersUsePublicComponents(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	sources, jobs, git := standalonemocks.NewMockSources(ctrl), mocks.NewMockJobs(ctrl), standalonemocks.NewMockGitReader(ctrl)
	d := persistence.SourceDefinition{ID: "git-id", Name: "git", Origin: persistence.OriginAPI,
		Git:      &persistence.GitSpec{Repository: "https://example.org/fixture", Auth: &persistence.GitAuth{Username: "fixture", PasswordFile: "/fixture/secret"}},
		Schedule: "1h", Filter: &persistence.Filter{Names: &persistence.NameFilter{Exclude: []string{"com.example/*"}}}}
	sources.EXPECT().ListSources(gomock.Any()).Return([]persistence.SourceDefinition{d}, nil)
	job := persistence.Job{Source: d, LeaseID: "lease", Generation: 1}
	var leased atomic.Bool
	jobs.EXPECT().ClaimNext(gomock.Any(), time.Minute).DoAndReturn(func(context.Context, time.Duration) (persistence.Job, bool, error) {
		leased.Store(true)
		return job, true, nil
	})
	git.EXPECT().Read(gomock.Any(), gomock.Any(), "synthetic-test-token").DoAndReturn(func(_ context.Context, spec persistence.GitSpec, _ string) ([]byte, error) {
		require.True(t, leased.Load())
		require.Equal(t, "registry.json", spec.Path)
		return []byte(inlineData), nil
	})
	fetcher := ingest.New(ingest.Options{Git: git, LoadSecret: func(_ context.Context, path string) (string, error) {
		require.True(t, leased.Load())
		require.Equal(t, "/fixture/secret", path)
		return "synthetic-test-token", nil
	}})
	jobs.EXPECT().Status(gomock.Any(), d.Name).Return(persistence.JobStatus{SourceID: d.ID}, nil)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	jobs.EXPECT().CommitSnapshot(gomock.Any(), job, gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, _ persistence.Job, result persistence.SyncResult, snapshot model.Snapshot) error {
		require.Empty(t, snapshot.Data.Servers)
		require.NotEmpty(t, result.FilterHash)
		cancel()
		return nil
	})
	r, err := standalone.New(sources, jobs, fetcher, options())
	require.NoError(t, err)
	require.NoError(t, r.Run(ctx))
}

func TestSourceCreationAndFenceMismatchRediscovery(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	sources, jobs, fetcher := standalonemocks.NewMockSources(ctrl), mocks.NewMockJobs(ctrl), mocks.NewMockFetcher(ctrl)
	sources.EXPECT().ListSources(gomock.Any()).Return(nil, nil)
	jobs.EXPECT().ClaimNext(gomock.Any(), time.Minute).Return(persistence.Job{}, false, nil)
	first := operatorDefinition("old", "one")
	old, next := &worker{started: make(chan struct{}), fatal: make(chan error, 1)}, &worker{started: make(chan struct{})}
	opts := options()
	opts.Operator = func(d persistence.SourceDefinition) (standalone.Worker, error) {
		if d.ID == "old" {
			return old, nil
		}
		return next, nil
	}
	r, err := standalone.New(sources, jobs, fetcher, opts)
	require.NoError(t, err)
	cancel, done := run(t, r)
	require.Eventually(t, r.Ready, time.Second, time.Millisecond)
	require.ErrorIs(t, r.Run(t.Context()), persistence.ErrBusy)
	sources.EXPECT().ListSources(gomock.Any()).Return([]persistence.SourceDefinition{first}, nil)
	require.NoError(t, r.Refresh(t.Context()))
	wait(t, old.started)
	// The public runner may discover a replaced incarnation before the next
	// source-list tick. Supervisor must join/reconstruct, not restart old scope.
	sources.EXPECT().ListSources(gomock.Any()).Return([]persistence.SourceDefinition{operatorDefinition("new", "two")}, nil)
	old.fatal <- persistence.ErrInvalid
	wait(t, next.started)
	require.Eventually(t, r.Ready, time.Second, time.Millisecond)
	cancel()
	require.NoError(t, <-done)
}

func TestInlineCancellationAndIncarnationGuard(t *testing.T) {
	t.Parallel()
	for _, recreated := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancel", true: "recreated"}[recreated], func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			sources, jobs, fetcher := standalonemocks.NewMockSources(ctrl), mocks.NewMockJobs(ctrl), mocks.NewMockFetcher(ctrl)
			d := persistence.SourceDefinition{ID: "old", Name: "inline", File: &persistence.FileSpec{Data: inlineData}}
			sources.EXPECT().ListSources(gomock.Any()).Return([]persistence.SourceDefinition{d}, nil)
			jobs.EXPECT().ClaimNext(gomock.Any(), time.Minute).Return(persistence.Job{}, false, nil).AnyTimes()
			jobs.EXPECT().Status(gomock.Any(), d.Name).Return(persistence.JobStatus{SourceID: d.ID}, nil)
			job := persistence.Job{Source: d, LeaseID: "lease"}
			if recreated {
				job.Source.ID = "new"
			}
			jobs.EXPECT().Acquire(gomock.Any(), d.Name, time.Minute).Return(job, nil)
			opts := options()
			r, err := standalone.New(sources, jobs, fetcher, opts)
			require.NoError(t, err)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			category := persistence.FailureCancelled
			if recreated {
				category = persistence.FailureFetch
			} else {
				fetcher.EXPECT().Fetch(gomock.Any(), d).DoAndReturn(func(ctx context.Context, _ persistence.SourceDefinition) (ingest.Result, error) {
					cancel()
					<-ctx.Done()
					return ingest.Result{}, ctx.Err()
				})
			}
			jobs.EXPECT().Fail(gomock.Any(), job, category).DoAndReturn(func(context.Context, persistence.Job, persistence.FailureCategory) error { cancel(); return nil })
			err = r.Run(ctx)
			if recreated {
				require.ErrorIs(t, err, ingest.ErrFetch, "cancellation during cleanup must not erase the completed attempt's failure")
			} else {
				require.NoError(t, err)
			}
			require.False(t, r.Ready())
		})
	}
}

func TestScheduledBackendFailureAndCancelledStartup(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	sources, jobs, fetcher := standalonemocks.NewMockSources(ctrl), mocks.NewMockJobs(ctrl), mocks.NewMockFetcher(ctrl)
	sources.EXPECT().ListSources(gomock.Any()).Return(nil, nil)
	jobs.EXPECT().ClaimNext(gomock.Any(), time.Minute).Return(persistence.Job{}, false, persistence.ErrUnavailable)
	r, err := standalone.New(sources, jobs, fetcher, options())
	require.NoError(t, err)
	require.ErrorIs(t, r.Run(t.Context()), persistence.ErrUnavailable)
	require.False(t, r.Ready())
	sources.EXPECT().ListSources(gomock.Any()).DoAndReturn(func(ctx context.Context) ([]persistence.SourceDefinition, error) { return nil, ctx.Err() })
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.NoError(t, r.Run(ctx))
}
