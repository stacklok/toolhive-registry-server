package operator_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	mcp "github.com/stacklok/toolhive/cmd/thv-operator/api/v1beta1"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/operator"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/reconcile"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/reconcile/mocks"
)

// A duplicate name/version among exported resources is recorded as a
// validation failure for this source only; the runner keeps watching and syncs
// again once an event fixes it.
func TestRunnerRecordsValidationFailureAndKeepsWatching(t *testing.T) {
	t.Parallel()
	server := exportedServer("same")
	virtual := &mcp.VirtualMCPServer{ObjectMeta: metadata("same", "one")}
	virtual.Annotations["toolhive.stacklok.dev/registry-version"] = "1.0.0"
	api := fake.NewClientBuilder().WithScheme(scheme(t)).WithObjects(server, virtual).Build()
	jobs := mocks.NewMockJobs(gomock.NewController(t))
	job := persistence.Job{Source: definition(), LeaseID: "lease", Generation: 1}
	var failures, commits, servers atomic.Int32
	jobs.EXPECT().Acquire(gomock.Any(), job.Source.Name, time.Minute).AnyTimes().Return(job, nil)
	jobs.EXPECT().Status(gomock.Any(), job.Source.Name).AnyTimes().Return(persistence.JobStatus{SourceID: job.Source.ID}, nil)
	jobs.EXPECT().Fail(gomock.Any(), job, persistence.FailureValidation).AnyTimes().DoAndReturn(
		func(context.Context, persistence.Job, persistence.FailureCategory) error {
			failures.Add(1)
			return nil
		})
	jobs.EXPECT().CommitSnapshot(gomock.Any(), job, gomock.Any(), gomock.Any()).AnyTimes().DoAndReturn(
		func(_ context.Context, _ persistence.Job, _ persistence.SyncResult, snapshot model.Snapshot) error {
			servers.Store(int32(len(snapshot.Data.Servers)))
			commits.Add(1)
			return nil
		})
	runner, err := operator.NewRunner(api, jobs, definition(), reconcile.Options{PollInterval: time.Hour, LeaseDuration: time.Minute},
		operator.WithResyncInterval(time.Hour))
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- runner.Run(ctx) }()
	require.Eventually(t, func() bool { return failures.Load() == 1 }, 3*time.Second, time.Millisecond)
	require.Eventually(t, runner.Ready, time.Second, time.Millisecond, "a recorded validation failure must not block readiness")
	select {
	case err := <-done:
		t.Fatalf("runner stopped on a per-source validation failure: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	require.Zero(t, commits.Load())
	require.NoError(t, api.Delete(t.Context(), virtual))
	require.Eventually(t, func() bool { return commits.Load() == 1 }, 3*time.Second, time.Millisecond)
	require.EqualValues(t, 1, servers.Load())
	cancel()
	require.NoError(t, <-done)
}

// A changed definition is not recorded as a failure of the new definition: the
// runner returns so the host constructs a runner for it.
func TestRunnerReturnsOnChangedDefinition(t *testing.T) {
	t.Parallel()
	api := fake.NewClientBuilder().WithScheme(scheme(t)).Build()
	jobs := mocks.NewMockJobs(gomock.NewController(t))
	changed := definition()
	changed.Kubernetes.Namespaces = []string{"one"}
	jobs.EXPECT().Acquire(gomock.Any(), changed.Name, time.Minute).Return(persistence.Job{Source: changed, LeaseID: "lease", Generation: 2}, nil)
	runner, err := operator.NewRunner(api, jobs, definition(), reconcile.Options{PollInterval: time.Millisecond, LeaseDuration: time.Minute})
	require.NoError(t, err)
	require.ErrorIs(t, runner.Run(t.Context()), persistence.ErrStale)
}

// Events that cannot change the export do not trigger a full list: objects not
// exported now or at the last fetch, and updates that change neither the
// generation nor the annotations. Annotation changes and deletes of exported
// objects do.
func TestRunnerSkipsIrrelevantWatchEvents(t *testing.T) {
	t.Parallel()
	watched := exportedServer("watched")
	plain := &mcp.MCPServer{ObjectMeta: metav1.ObjectMeta{Name: "plain", Namespace: "one"}, Spec: mcp.MCPServerSpec{Image: "example.org/image:1.0.0"}}
	api := fake.NewClientBuilder().WithScheme(scheme(t)).WithObjects(watched, plain).Build()
	_, counts := startCountingRunner(t, api, time.Hour, noAcquireErr, operator.WithResyncInterval(time.Hour))
	quiet := func(msg string) {
		t.Helper()
		require.Never(t, func() bool { return counts.acquires.Load() > 1 }, 150*time.Millisecond, 5*time.Millisecond, msg)
	}
	require.NoError(t, api.Create(t.Context(), &mcp.MCPServer{ObjectMeta: metav1.ObjectMeta{Name: "other", Namespace: "one"}}))
	quiet("creating a non-exported object")
	require.NoError(t, api.Get(t.Context(), client.ObjectKeyFromObject(plain), plain))
	plain.Labels = map[string]string{"touched": "true"}
	require.NoError(t, api.Update(t.Context(), plain))
	quiet("updating a non-exported object")
	require.NoError(t, api.Get(t.Context(), client.ObjectKeyFromObject(watched), watched))
	watched.Labels = map[string]string{"touched": "true"}
	require.NoError(t, api.Update(t.Context(), watched))
	quiet("updating an exported object without a generation or annotation change")

	watched.Annotations["toolhive.stacklok.dev/registry-description"] = "changed"
	require.NoError(t, api.Update(t.Context(), watched))
	require.Eventually(t, func() bool { return counts.commits.Load() == 2 }, 3*time.Second, time.Millisecond)
	require.NoError(t, api.Delete(t.Context(), watched))
	require.Eventually(t, func() bool { return counts.commits.Load() == 3 }, 3*time.Second, time.Millisecond)
	require.Zero(t, counts.servers.Load())
}

func TestFetcherRelevant(t *testing.T) {
	t.Parallel()
	exported := exportedServer("exported")
	exported.Generation = 1
	plain := &mcp.MCPServer{ObjectMeta: metav1.ObjectMeta{Name: "plain", Namespace: "one", Generation: 1}}
	api := fake.NewClientBuilder().WithScheme(scheme(t)).WithObjects(exported, plain).Build()
	f, err := operator.NewFetcher(api, definition())
	require.NoError(t, err)
	require.True(t, f.Relevant(watch.Event{Type: watch.Modified, Object: plain.DeepCopy()}), "every event is relevant before the first fetch")
	_, err = f.Fetch(t.Context(), definition())
	require.NoError(t, err)
	require.NoError(t, api.Get(t.Context(), client.ObjectKeyFromObject(exported), exported))
	require.NoError(t, api.Get(t.Context(), client.ObjectKeyFromObject(plain), plain))
	modify := func(base client.Object, change func(client.Object)) client.Object {
		object, ok := base.DeepCopyObject().(client.Object)
		require.True(t, ok)
		change(object)
		return object
	}
	for _, tc := range []struct {
		name     string
		event    watch.Event
		relevant bool
	}{
		{"unchanged exported object", watch.Event{Type: watch.Modified, Object: exported.DeepCopy()}, false},
		{"label-only change", watch.Event{Type: watch.Modified, Object: modify(exported, func(o client.Object) {
			o.SetLabels(map[string]string{"a": "b"})
		})}, false},
		{"generation change", watch.Event{Type: watch.Modified, Object: modify(exported, func(o client.Object) {
			o.SetGeneration(o.GetGeneration() + 1)
		})}, true},
		{"annotation change", watch.Event{Type: watch.Modified, Object: modify(exported, func(o client.Object) {
			o.SetAnnotations(map[string]string{export: "true", "toolhive.stacklok.dev/registry-title": "new"})
		})}, true},
		{"export removed", watch.Event{Type: watch.Modified, Object: modify(exported, func(o client.Object) { o.SetAnnotations(nil) })}, true},
		{"exported object deleted", watch.Event{Type: watch.Deleted, Object: exported.DeepCopy()}, true},
		{"non-exported object changed", watch.Event{Type: watch.Modified, Object: modify(plain, func(o client.Object) { o.SetGeneration(5) })}, false},
		{"non-exported object deleted", watch.Event{Type: watch.Deleted, Object: plain.DeepCopy()}, false},
		{"object newly exported", watch.Event{Type: watch.Modified, Object: modify(plain, func(o client.Object) {
			o.SetAnnotations(map[string]string{export: "true"})
		})}, true},
		{"new exported object", watch.Event{Type: watch.Added, Object: exportedServer("new")}, true},
		{"same name, other kind, not exported", watch.Event{Type: watch.Added,
			Object: &mcp.VirtualMCPServer{ObjectMeta: metav1.ObjectMeta{Name: "exported", Namespace: "one"}}}, false},
		{"bookmark", watch.Event{Type: watch.Bookmark, Object: exported.DeepCopy()}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.relevant, f.Relevant(tc.event))
		})
	}
}
