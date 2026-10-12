package operator_test

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	mcp "github.com/stacklok/toolhive/cmd/thv-operator/api/v1beta1"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
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

const export = "toolhive.stacklok.dev/registry-export"

func definition() persistence.SourceDefinition {
	return persistence.SourceDefinition{ID: "incarnation", Name: "operator", Kubernetes: &persistence.KubernetesSpec{Namespaces: []string{"one", "two"}}}
}
func scheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	require.NoError(t, mcp.AddToScheme(s))
	return s
}
func metadata(name, namespace string) metav1.ObjectMeta {
	return metav1.ObjectMeta{Name: name, Namespace: namespace, Annotations: map[string]string{export: "true", "toolhive.stacklok.dev/registry-url": "https://example.org/mcp", "toolhive.stacklok.dev/registry-description": "description", "toolhive.stacklok.dev/authz-claims": "{invalid, ignored by reusable adapter}"}}
}

func TestCompleteScopeAndExportChanges(t *testing.T) {
	t.Parallel()
	server := &mcp.MCPServer{ObjectMeta: metadata("server", "one"), Spec: mcp.MCPServerSpec{Image: "example.org/image:1.0.0"}}
	virtual := &mcp.VirtualMCPServer{ObjectMeta: metadata("virtual", "two")}
	proxy := &mcp.MCPRemoteProxy{ObjectMeta: metadata("proxy", "two")}
	ignored := &mcp.VirtualMCPServer{ObjectMeta: metadata("ignored", "one")}
	ignored.Annotations[export] = "True"
	outside := &mcp.VirtualMCPServer{ObjectMeta: metadata("outside", "other")}
	c := fake.NewClientBuilder().WithScheme(scheme(t)).WithObjects(server, virtual, proxy, ignored, outside).Build()
	f, err := operator.NewFetcher(c, definition())
	require.NoError(t, err)
	got, err := f.Fetch(t.Context(), definition())
	require.NoError(t, err)
	require.Len(t, got.Snapshot.Data.Servers, 3)
	originalHash := got.Hash
	for _, s := range got.Snapshot.Data.Servers {
		require.Equal(t, "https://example.org/mcp", s.Remotes[0].URL)
		require.NotContains(t, s.Meta.PublisherProvided, "authz-claims")
	}
	proxy.Annotations["toolhive.stacklok.dev/registry-description"] = "changed"
	require.NoError(t, c.Update(t.Context(), proxy))
	got, err = f.Fetch(t.Context(), definition())
	require.NoError(t, err)
	require.NotEqual(t, originalHash, got.Hash)
	require.Len(t, got.Snapshot.Data.Servers, 3)
	virtual.Annotations[export] = "false"
	require.NoError(t, c.Update(t.Context(), virtual))
	require.NoError(t, c.Delete(t.Context(), server))
	got, err = f.Fetch(t.Context(), definition())
	require.NoError(t, err)
	require.Len(t, got.Snapshot.Data.Servers, 1)
	require.NoError(t, c.Delete(t.Context(), proxy))
	got, err = f.Fetch(t.Context(), definition())
	require.NoError(t, err)
	require.Empty(t, got.Snapshot.Data.Servers)
	require.NotEmpty(t, got.Hash)
	changed := definition()
	changed.Kubernetes.Namespaces = []string{"one"}
	_, err = f.Fetch(t.Context(), changed)
	require.ErrorIs(t, err, persistence.ErrInvalid)
	changed = definition()
	changed.ID = "recreated"
	_, err = f.Fetch(t.Context(), changed)
	require.ErrorIs(t, err, persistence.ErrInvalid)
}

func TestFailedListNeverReturnsPartial(t *testing.T) {
	t.Parallel()
	for _, incomplete := range []bool{false, true} {
		t.Run(strconv.FormatBool(incomplete), func(t *testing.T) {
			t.Parallel()
			c := fake.NewClientBuilder().WithScheme(scheme(t)).WithInterceptorFuncs(interceptor.Funcs{List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				if _, ok := list.(*mcp.MCPRemoteProxyList); ok {
					if incomplete {
						list.SetContinue("more")
						return nil
					}
					return errors.New("secret response")
				}
				return c.List(ctx, list, opts...)
			}}).Build()
			f, err := operator.NewFetcher(c, definition())
			require.NoError(t, err)
			got, err := f.Fetch(t.Context(), definition())
			require.ErrorIs(t, err, ingest.ErrFetch)
			require.Empty(t, got.Hash)
			require.Empty(t, got.Snapshot.Data.Servers)
		})
	}
}

func TestStartupReadyOnlyAfterEmptyCommit(t *testing.T) {
	t.Parallel()
	c := fake.NewClientBuilder().WithScheme(scheme(t)).Build()
	jobs := mocks.NewMockJobs(gomock.NewController(t))
	job := persistence.Job{Source: definition(), LeaseID: "lease", Generation: 1}
	jobs.EXPECT().Acquire(gomock.Any(), job.Source.Name, time.Minute).Return(job, nil)
	jobs.EXPECT().Status(gomock.Any(), job.Source.Name).Return(persistence.JobStatus{SourceID: job.Source.ID}, nil)
	entered := make(chan struct{})
	release := make(chan struct{})
	jobs.EXPECT().CommitSnapshot(gomock.Any(), job, gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, _ persistence.Job, _ persistence.SyncResult, snapshot model.Snapshot) error {
		require.Empty(t, snapshot.Data.Servers)
		close(entered)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	runner, err := operator.NewRunner(c, jobs, definition(), reconcile.Options{PollInterval: time.Hour, LeaseDuration: time.Minute})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- runner.Run(ctx) }()
	<-entered
	require.False(t, runner.Ready())
	close(release)
	require.Eventually(t, runner.Ready, time.Second, time.Millisecond)
	cancel()
	require.NoError(t, <-done)
	require.False(t, runner.Ready())
}

func TestWatchFailuresSurface(t *testing.T) {
	t.Parallel()
	for _, startup := range []bool{false, true} {
		t.Run(strconv.FormatBool(startup), func(t *testing.T) {
			t.Parallel()
			c := fake.NewClientBuilder().WithScheme(scheme(t)).WithInterceptorFuncs(interceptor.Funcs{Watch: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) (watch.Interface, error) {
				if startup {
					return nil, errors.New("secret authorization detail")
				}
				w := watch.NewRaceFreeFake()
				w.Error(&metav1.Status{Reason: metav1.StatusReasonForbidden})
				return w, nil
			}}).Build()
			jobs := mocks.NewMockJobs(gomock.NewController(t))
			jobs.EXPECT().Acquire(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().DoAndReturn(func(ctx context.Context, _ string, _ time.Duration) (persistence.Job, error) {
				<-ctx.Done()
				return persistence.Job{}, ctx.Err()
			})
			runner, err := operator.NewRunner(c, jobs, definition(), reconcile.Options{PollInterval: time.Second, LeaseDuration: time.Minute})
			require.NoError(t, err)
			require.ErrorIs(t, runner.Run(t.Context()), operator.ErrWatch)
			require.False(t, runner.Ready())
		})
	}
}
