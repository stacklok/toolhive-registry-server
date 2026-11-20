package operator_test

import (
	"context"
	"errors"
	"strconv"
	"strings"
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
	// A changed scope or incarnation is stale: nothing is recorded against the
	// new definition and the runner returns so the host builds a new one.
	changed := definition()
	changed.Kubernetes.Namespaces = []string{"one"}
	_, err = f.Fetch(t.Context(), changed)
	require.ErrorIs(t, err, persistence.ErrStale)
	changed = definition()
	changed.ID = "recreated"
	_, err = f.Fetch(t.Context(), changed)
	require.ErrorIs(t, err, persistence.ErrStale)
}

// Kubernetes exports keep v1.5.4's acceptance: every resource with the export,
// URL and description annotations is exported, with no schema validation.
func TestExportsWithoutSchemaValidation(t *testing.T) {
	t.Parallel()
	const (
		category    = "toolhive.stacklok.dev/registry-category"
		description = "toolhive.stacklok.dev/registry-description"
		icon        = "toolhive.stacklok.dev/registry-icon"
		title       = "toolhive.stacklok.dev/registry-title"
		tools       = "toolhive.stacklok.dev/tools"
		url         = "toolhive.stacklok.dev/registry-url"
	)
	annotated := func(name string, annotations map[string]string) metav1.ObjectMeta {
		m := metadata(name, "one")
		for k, v := range annotations {
			m.Annotations[k] = v
		}
		return m
	}
	objects := []client.Object{
		&mcp.MCPServer{ObjectMeta: annotated("latest-tag", nil), Spec: mcp.MCPServerSpec{Image: "example.org/image:latest"}},
		&mcp.MCPServer{ObjectMeta: annotated("no-tag", nil), Spec: mcp.MCPServerSpec{Image: "example.org/image"}},
		&mcp.VirtualMCPServer{ObjectMeta: annotated("upper-category", map[string]string{category: "Databases"})},
		&mcp.VirtualMCPServer{ObjectMeta: annotated("short-category", map[string]string{category: "x"})},
		&mcp.VirtualMCPServer{ObjectMeta: annotated("empty-description", map[string]string{description: ""})},
		&mcp.VirtualMCPServer{ObjectMeta: annotated("long-description", map[string]string{description: strings.Repeat("x", 101)})},
		&mcp.VirtualMCPServer{ObjectMeta: annotated("long-title", map[string]string{title: strings.Repeat("t", 200)})},
		&mcp.MCPRemoteProxy{ObjectMeta: annotated("no-scheme", map[string]string{url: "example.org/mcp"})},
		&mcp.MCPRemoteProxy{ObjectMeta: annotated("relative-icon", map[string]string{icon: "icons/logo.png"})},
		&mcp.MCPRemoteProxy{ObjectMeta: annotated("odd-tools", map[string]string{tools: "[\"a.b\",\"a.b\"]"})},
	}
	c := fake.NewClientBuilder().WithScheme(scheme(t)).WithObjects(objects...).Build()
	f, err := operator.NewFetcher(c, definition())
	require.NoError(t, err)
	got, err := f.Fetch(t.Context(), definition())
	require.NoError(t, err)
	require.Len(t, got.Snapshot.Data.Servers, len(objects))
	// Each is rejected by the strict publication check, accepted by storage safety.
	for _, server := range got.Snapshot.Data.Servers {
		single := model.Snapshot{}
		single.Data.Servers = []model.Server{server}
		require.ErrorIs(t, persistence.ValidateSnapshot(&single), persistence.ErrInvalid, server.Name)
	}
	require.NoError(t, persistence.ValidateStorableSnapshot(&got.Snapshot))
}

func TestHistoricalCrossKindNameIdentityNotRewritten(t *testing.T) {
	t.Parallel()
	// Characterization only: the user decision about different CRD kinds sharing
	// namespace/name (including different versions) is still pending.
	for _, version := range []string{"1.0.0", "2.0.0"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			server := &mcp.MCPServer{ObjectMeta: metadata("same", "one"), Spec: mcp.MCPServerSpec{Image: "example.org/image:1.0.0"}}
			virtual := &mcp.VirtualMCPServer{ObjectMeta: metadata("same", "one")}
			virtual.Annotations["toolhive.stacklok.dev/registry-version"] = version
			api := fake.NewClientBuilder().WithScheme(scheme(t)).WithObjects(server, virtual).Build()
			fetcher, err := operator.NewFetcher(api, definition())
			require.NoError(t, err)
			got, err := fetcher.Fetch(t.Context(), definition())
			require.NoError(t, err)
			if version == "1.0.0" {
				// Duplicate name/version exports fail the source at the storage
				// check rather than silently pick a resource.
				require.Len(t, got.Snapshot.Data.Servers, 2)
				require.ErrorIs(t, persistence.ValidateStorableSnapshot(&got.Snapshot), persistence.ErrInvalid)
				return
			}
			require.Len(t, got.Snapshot.Data.Servers, 2)
			for _, s := range got.Snapshot.Data.Servers {
				require.Equal(t, "com.toolhive.k8s.one/same", s.Name)
			}
			require.Equal(t, "1.0.0", got.Snapshot.Data.Servers[0].Version)
			require.Equal(t, version, got.Snapshot.Data.Servers[1].Version)
		})
	}
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
