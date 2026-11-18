package kubernetes_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	mcp "github.com/stacklok/toolhive/cmd/thv-operator/api/v1beta1"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/stacklok/toolhive-registry-server/internal/kubernetes"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/ingest"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/reconcile"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/reconcile/mocks"
)

const exportKey = "toolhive.stacklok.dev/registry-export"
const retiredKey = "toolhive.stacklok.dev/authz-claims"

func standaloneScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	require.NoError(t, mcp.AddToScheme(s))
	return s
}
func standaloneDefinition() persistence.SourceDefinition {
	return persistence.SourceDefinition{ID: "operator-incarnation", Name: "operator", Kubernetes: &persistence.KubernetesSpec{Namespaces: []string{"one", "two"}}}
}
func exportObject(kind string, annotations map[string]string) client.Object {
	meta := metav1.ObjectMeta{Name: strings.ToLower(kind), Namespace: "two", Annotations: annotations}
	switch kind {
	case "MCPServer":
		return &mcp.MCPServer{ObjectMeta: meta, Spec: mcp.MCPServerSpec{Image: "example.org/image:1.0.0"}}
	case "VirtualMCPServer":
		return &mcp.VirtualMCPServer{ObjectMeta: meta}
	default:
		return &mcp.MCPRemoteProxy{ObjectMeta: meta}
	}
}
func standaloneOptions() reconcile.Options {
	return reconcile.Options{PollInterval: time.Hour, LeaseDuration: time.Minute}
}

func TestStandaloneRecordsAnnotationClaims(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		annotation string
		synced     bool
		recorded   string
	}{
		{"valid claims replace source claims", `{"team":"private"}`, true, `{"team":"private"}`},
		{"empty object", `{}`, true, `{}`},
		{"json null", `null`, true, `null`},
		{"empty annotation means no override", "", true, ""},
		{"malformed annotation skips the entry", "{malformed sensitive value}", false, ""},
		{"nested value skips the entry", `{"team":{"a":"b"}}`, false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			obj := exportObject("MCPServer", map[string]string{
				exportKey: "true", retiredKey: tc.annotation,
				"toolhive.stacklok.dev/registry-url":         "https://example.org/mcp",
				"toolhive.stacklok.dev/registry-description": "description",
			})
			api := fake.NewClientBuilder().WithScheme(standaloneScheme(t)).WithObjects(obj).Build()
			d := standaloneDefinition()
			require.NoError(t, kubernetes.PreflightStandaloneOperator(t.Context(), api, d))
			claims := kubernetes.NewAnnotationClaims()
			jobs := mocks.NewMockJobs(gomock.NewController(t))
			job := persistence.Job{Source: d, LeaseID: "lease", Generation: 1}
			jobs.EXPECT().Acquire(gomock.Any(), d.Name, time.Minute).Return(job, nil)
			jobs.EXPECT().Status(gomock.Any(), d.Name).Return(persistence.JobStatus{SourceID: d.ID}, nil)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var serverName string
			jobs.EXPECT().CommitSnapshot(gomock.Any(), job, gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, _ persistence.Job, _ persistence.SyncResult, snapshot model.Snapshot) error {
					if tc.synced {
						require.Len(t, snapshot.Data.Servers, 1)
						serverName = snapshot.Data.Servers[0].Name
					} else {
						require.Empty(t, snapshot.Data.Servers, "an entry with an invalid annotation must not sync")
					}
					cancel()
					return nil
				})
			runner, err := kubernetes.NewStandaloneOperator(api, jobs, d, standaloneOptions(), claims)
			require.NoError(t, err)
			require.NoError(t, runner.Run(ctx))
			recorded := claims.EntryClaims(d.Name)
			if tc.recorded == "" {
				require.Empty(t, recorded)
				return
			}
			require.Equal(t, map[string][]byte{serverName: []byte(tc.recorded)}, recorded)
			require.Empty(t, claims.EntryClaims("other-source"))
		})
	}
}

func TestStandaloneNonExportedAnnotationsIgnoredAndPreflightReadOnly(t *testing.T) {
	t.Parallel()
	for _, exportValue := range []string{"", "false", "True", " true"} {
		t.Run(exportValue, func(t *testing.T) {
			t.Parallel()
			var objects []client.Object
			for _, kind := range []string{"MCPServer", "VirtualMCPServer", "MCPRemoteProxy"} {
				objects = append(objects, exportObject(kind, map[string]string{exportKey: exportValue, retiredKey: "malformed"}))
			}
			var lists atomic.Int32
			api := fake.NewClientBuilder().WithScheme(standaloneScheme(t)).WithObjects(objects...).WithInterceptorFuncs(interceptor.Funcs{
				List: func(ctx context.Context, api client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
					lists.Add(1)
					return api.List(ctx, list, opts...)
				},
			}).Build()
			d := standaloneDefinition()
			d.Kubernetes.Namespaces = nil
			jobs := mocks.NewMockJobs(gomock.NewController(t))
			_, err := kubernetes.NewStandaloneOperator(api, jobs, d, standaloneOptions(), nil)
			require.NoError(t, err)
			require.Zero(t, lists.Load())
			require.NoError(t, kubernetes.PreflightStandaloneOperator(t.Context(), api, d))
			require.EqualValues(t, 3, lists.Load())
			job := persistence.Job{Source: d, LeaseID: "lease"}
			jobs.EXPECT().Acquire(gomock.Any(), d.Name, time.Minute).Return(job, nil)
			jobs.EXPECT().Status(gomock.Any(), d.Name).Return(persistence.JobStatus{SourceID: d.ID}, nil)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			jobs.EXPECT().CommitSnapshot(gomock.Any(), job, gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, _ persistence.Job, _ persistence.SyncResult, snapshot model.Snapshot) error {
				require.Empty(t, snapshot.Data.Servers)
				cancel()
				return nil
			})
			runner, err := kubernetes.NewStandaloneOperator(api, jobs, d, standaloneOptions(), nil)
			require.NoError(t, err)
			require.NoError(t, runner.Run(ctx))
			// The borrowed client remains usable after runner cleanup.
			require.NoError(t, api.List(t.Context(), &mcp.MCPServerList{}))
		})
	}
}

func TestStandaloneLaterNamespaceKindAndIncompleteListsNeverCommit(t *testing.T) {
	t.Parallel()
	for _, incomplete := range []bool{false, true} {
		for _, laterNamespace := range []bool{false, true} {
			t.Run(strings.Join([]string{map[bool]string{false: "error", true: "continuation"}[incomplete], map[bool]string{false: "later-kind", true: "later-namespace"}[laterNamespace]}, "/"), func(t *testing.T) {
				t.Parallel()
				api := fake.NewClientBuilder().WithScheme(standaloneScheme(t)).WithObjects(exportObject("MCPServer", map[string]string{
					exportKey: "true", "toolhive.stacklok.dev/registry-url": "https://example.org/mcp", "toolhive.stacklok.dev/registry-description": "description",
				})).WithInterceptorFuncs(interceptor.Funcs{List: func(ctx context.Context, api client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
					options := &client.ListOptions{}
					options.ApplyOptions(opts)
					_, lastKind := list.(*mcp.MCPRemoteProxyList)
					if (laterNamespace && options.Namespace == "two") || (!laterNamespace && lastKind) {
						if incomplete {
							list.SetContinue("more")
							return nil
						}
						return errors.New("sensitive upstream error")
					}
					return api.List(ctx, list, opts...)
				}}).Build()
				d := standaloneDefinition()
				err := kubernetes.PreflightStandaloneOperator(t.Context(), api, d)
				require.ErrorIs(t, err, ingest.ErrFetch)
				require.NotContains(t, err.Error(), "sensitive")
				jobs := mocks.NewMockJobs(gomock.NewController(t))
				job := persistence.Job{Source: d, LeaseID: "lease"}
				jobs.EXPECT().Acquire(gomock.Any(), d.Name, time.Minute).Return(job, nil)
				jobs.EXPECT().Fail(gomock.Any(), job, persistence.FailureFetch).Return(nil)
				runner, err := kubernetes.NewStandaloneOperator(api, jobs, d, standaloneOptions(), nil)
				require.NoError(t, err)
				require.ErrorIs(t, runner.Run(t.Context()), ingest.ErrFetch)
			})
		}
	}
}

func TestStandaloneRecordsAnnotationClaimsAcrossNamespaces(t *testing.T) {
	t.Parallel()
	annotated := func(namespace, claims string) client.Object {
		obj := exportObject("MCPServer", map[string]string{
			exportKey: "true", retiredKey: claims,
			"toolhive.stacklok.dev/registry-url":         "https://example.org/mcp",
			"toolhive.stacklok.dev/registry-description": "description",
		})
		obj.SetNamespace(namespace)
		return obj
	}
	api := fake.NewClientBuilder().WithScheme(standaloneScheme(t)).
		WithObjects(annotated("one", `{"team":"one"}`), annotated("two", `{"team":"two"}`)).Build()
	d := standaloneDefinition()
	claims := kubernetes.NewAnnotationClaims()
	jobs := mocks.NewMockJobs(gomock.NewController(t))
	job := persistence.Job{Source: d, LeaseID: "lease", Generation: 1}
	jobs.EXPECT().Acquire(gomock.Any(), d.Name, time.Minute).Return(job, nil)
	jobs.EXPECT().Status(gomock.Any(), d.Name).Return(persistence.JobStatus{SourceID: d.ID}, nil)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	jobs.EXPECT().CommitSnapshot(gomock.Any(), job, gomock.Any(), gomock.Any()).DoAndReturn(
		func(context.Context, persistence.Job, persistence.SyncResult, model.Snapshot) error {
			cancel()
			return nil
		})
	runner, err := kubernetes.NewStandaloneOperator(api, jobs, d, standaloneOptions(), claims)
	require.NoError(t, err)
	require.NoError(t, runner.Run(ctx))
	// The later namespace's list must not erase the earlier one's annotations.
	recorded := claims.EntryClaims(d.Name)
	require.Len(t, recorded, 2)
	var values []string
	for _, v := range recorded {
		values = append(values, string(v))
	}
	require.ElementsMatch(t, []string{`{"team":"one"}`, `{"team":"two"}`}, values)
}

// An annotation-only change leaves the snapshot hash unchanged, so the
// coordinator would complete the sync without committing and the claims hook
// would never run. The claims store must force a commit for it, and only then.
func TestStandaloneAnnotationChangeForcesCommit(t *testing.T) {
	t.Parallel()
	obj := exportObject("MCPServer", map[string]string{
		exportKey: "true", retiredKey: `{"team":"a"}`,
		"toolhive.stacklok.dev/registry-url":         "https://example.org/mcp",
		"toolhive.stacklok.dev/registry-description": "description",
	})
	quiet := exportObject("VirtualMCPServer", nil) // not exported; updating it only wakes the runner
	api := fake.NewClientBuilder().WithScheme(standaloneScheme(t)).WithObjects(obj, quiet).Build()
	d := standaloneDefinition()
	claims := kubernetes.NewAnnotationClaims()
	jobs := mocks.NewMockJobs(gomock.NewController(t))
	job := persistence.Job{Source: d, LeaseID: "lease", Generation: 1}
	jobs.EXPECT().Acquire(gomock.Any(), d.Name, time.Minute).AnyTimes().Return(job, nil)
	var last atomic.Pointer[persistence.SyncResult]
	jobs.EXPECT().Status(gomock.Any(), d.Name).AnyTimes().DoAndReturn(func(context.Context, string) (persistence.JobStatus, error) {
		status := persistence.JobStatus{SourceID: d.ID}
		if r := last.Load(); r != nil {
			status.BaselineValid, status.LastHash, status.LastFilterHash = true, r.Hash, r.FilterHash
		}
		return status, nil
	})
	events := make(chan string, 10)
	jobs.EXPECT().CommitSnapshot(gomock.Any(), job, gomock.Any(), gomock.Any()).AnyTimes().DoAndReturn(
		func(_ context.Context, _ persistence.Job, result persistence.SyncResult, _ model.Snapshot) error {
			last.Store(&result)
			events <- "commit"
			return nil
		})
	jobs.EXPECT().Complete(gomock.Any(), job, gomock.Any()).AnyTimes().DoAndReturn(
		func(context.Context, persistence.Job, persistence.SyncResult) error { events <- "complete"; return nil })
	ctx, cancel := context.WithCancel(t.Context())
	runner, err := kubernetes.NewStandaloneOperator(api, jobs, d, standaloneOptions(), claims)
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- runner.Run(ctx) }()
	next := func() string {
		t.Helper()
		select {
		case e := <-events:
			return e
		case <-time.After(10 * time.Second):
			require.FailNow(t, "no sync")
			return ""
		}
	}
	require.Equal(t, "commit", next(), "startup commits")
	update := func(o client.Object, mutate func(map[string]string)) {
		t.Helper()
		require.NoError(t, api.Get(t.Context(), client.ObjectKeyFromObject(o), o))
		a := o.GetAnnotations()
		if a == nil {
			a = map[string]string{}
		}
		mutate(a)
		o.SetAnnotations(a)
		require.NoError(t, api.Update(t.Context(), o))
	}
	update(quiet, func(a map[string]string) { a["touch"] = "1" })
	require.Equal(t, "complete", next(), "nothing changed: no commit")
	update(obj, func(a map[string]string) { a[retiredKey] = `{"team":"b"}` })
	require.Equal(t, "commit", next(), "annotation-only change commits")
	require.Equal(t, map[string][]byte{"com.toolhive.k8s.two/mcpserver": []byte(`{"team":"b"}`)}, claims.EntryClaims(d.Name))
	update(quiet, func(a map[string]string) { a["touch"] = "2" })
	require.Equal(t, "complete", next(), "after the commit, unchanged again")
	cancel()
	require.NoError(t, <-done)
}

// syncOnce runs a fresh worker (as another replica or a restarted process
// would) with its own claims store against a shared stored sync result, and
// reports whether its startup sync committed or completed.
func syncOnce(t *testing.T, api client.WithWatch, stored *atomic.Pointer[persistence.SyncResult]) string {
	t.Helper()
	d := standaloneDefinition()
	jobs := mocks.NewMockJobs(gomock.NewController(t))
	job := persistence.Job{Source: d, LeaseID: "lease", Generation: 1}
	jobs.EXPECT().Acquire(gomock.Any(), d.Name, time.Minute).Return(job, nil)
	jobs.EXPECT().Status(gomock.Any(), d.Name).DoAndReturn(func(context.Context, string) (persistence.JobStatus, error) {
		status := persistence.JobStatus{SourceID: d.ID}
		if r := stored.Load(); r != nil {
			status.BaselineValid, status.LastHash, status.LastFilterHash = true, r.Hash, r.FilterHash
		}
		return status, nil
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var outcome string
	jobs.EXPECT().CommitSnapshot(gomock.Any(), job, gomock.Any(), gomock.Any()).MaxTimes(1).DoAndReturn(
		func(_ context.Context, _ persistence.Job, result persistence.SyncResult, _ model.Snapshot) error {
			stored.Store(&result)
			outcome = "commit"
			cancel()
			return nil
		})
	jobs.EXPECT().Complete(gomock.Any(), job, gomock.Any()).MaxTimes(1).DoAndReturn(
		func(_ context.Context, _ persistence.Job, result persistence.SyncResult) error {
			require.Equal(t, stored.Load().FilterHash, result.FilterHash, "an unchanged sync stores the same digest")
			outcome = "complete"
			cancel()
			return nil
		})
	worker, err := kubernetes.NewStandaloneOperator(api, jobs, d, standaloneOptions(), kubernetes.NewAnnotationClaims())
	require.NoError(t, err)
	require.NoError(t, worker.Run(ctx))
	return outcome
}

// The decision to rewrite claims compares against what the database holds, not
// against what one process last saw, so it holds across replicas and restarts.
func TestStandaloneClaimsDigestIsSharedThroughStorage(t *testing.T) {
	t.Parallel()
	obj := exportObject("MCPServer", map[string]string{
		exportKey: "true", retiredKey: `{"team":"a"}`,
		"toolhive.stacklok.dev/registry-url":         "https://example.org/mcp",
		"toolhive.stacklok.dev/registry-description": "description",
	})
	api := fake.NewClientBuilder().WithScheme(standaloneScheme(t)).WithObjects(obj).Build()
	var stored atomic.Pointer[persistence.SyncResult]
	stored.Store(&persistence.SyncResult{Hash: "written-before-the-digest-existed"})
	require.Equal(t, "commit", syncOnce(t, api, &stored), "no stored digest: claims are written once")
	require.Contains(t, stored.Load().FilterHash, "|claims=")
	require.Equal(t, "complete", syncOnce(t, api, &stored), "another process with the same annotations writes nothing")
	require.NoError(t, api.Get(t.Context(), client.ObjectKeyFromObject(obj), obj))
	obj.GetAnnotations()[retiredKey] = `{"team":"b"}`
	require.NoError(t, api.Update(t.Context(), obj))
	require.Equal(t, "commit", syncOnce(t, api, &stored), "a changed annotation is written by any process")
}

func TestStandaloneRebuiltWorkerForgetsOldScopes(t *testing.T) {
	t.Parallel()
	obj := exportObject("MCPServer", map[string]string{
		exportKey: "true", retiredKey: `{"team":"a"}`,
		"toolhive.stacklok.dev/registry-url":         "https://example.org/mcp",
		"toolhive.stacklok.dev/registry-description": "description",
	})
	api := fake.NewClientBuilder().WithScheme(standaloneScheme(t)).WithObjects(obj).Build()
	d := standaloneDefinition()
	claims := kubernetes.NewAnnotationClaims()
	jobs := mocks.NewMockJobs(gomock.NewController(t))
	job := persistence.Job{Source: d, LeaseID: "lease", Generation: 1}
	jobs.EXPECT().Acquire(gomock.Any(), d.Name, time.Minute).Return(job, nil)
	jobs.EXPECT().Status(gomock.Any(), d.Name).Return(persistence.JobStatus{SourceID: d.ID}, nil)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	jobs.EXPECT().CommitSnapshot(gomock.Any(), job, gomock.Any(), gomock.Any()).DoAndReturn(
		func(context.Context, persistence.Job, persistence.SyncResult, model.Snapshot) error {
			cancel()
			return nil
		})
	worker, err := kubernetes.NewStandaloneOperator(api, jobs, d, standaloneOptions(), claims)
	require.NoError(t, err)
	require.NoError(t, worker.Run(ctx))
	require.Len(t, claims.EntryClaims(d.Name), 1)
	// The source is redefined to watch only namespace "one"; the scope it no
	// longer lists must not keep applying its annotation.
	d.Kubernetes.Namespaces = []string{"one"}
	_, err = kubernetes.NewStandaloneOperator(api, jobs, d, standaloneOptions(), claims)
	require.NoError(t, err)
	require.Empty(t, claims.EntryClaims(d.Name))
}

func TestStandaloneIgnoresClaimsOfUnexportedResources(t *testing.T) {
	t.Parallel()
	// Exported, but without the URL annotation the fetcher requires: its claims
	// (even malformed ones) are neither recorded nor reported.
	for _, annotation := range []string{`{"team":"a"}`, "{malformed"} {
		obj := exportObject("MCPServer", map[string]string{
			exportKey: "true", retiredKey: annotation,
			"toolhive.stacklok.dev/registry-description": "description",
		})
		api := fake.NewClientBuilder().WithScheme(standaloneScheme(t)).WithObjects(obj).Build()
		d := standaloneDefinition()
		claims := kubernetes.NewAnnotationClaims()
		jobs := mocks.NewMockJobs(gomock.NewController(t))
		job := persistence.Job{Source: d, LeaseID: "lease", Generation: 1}
		jobs.EXPECT().Acquire(gomock.Any(), d.Name, time.Minute).Return(job, nil)
		jobs.EXPECT().Status(gomock.Any(), d.Name).Return(persistence.JobStatus{SourceID: d.ID}, nil)
		ctx, cancel := context.WithCancel(t.Context())
		jobs.EXPECT().CommitSnapshot(gomock.Any(), job, gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, _ persistence.Job, _ persistence.SyncResult, snapshot model.Snapshot) error {
				require.Empty(t, snapshot.Data.Servers)
				cancel()
				return nil
			})
		worker, err := kubernetes.NewStandaloneOperator(api, jobs, d, standaloneOptions(), claims)
		require.NoError(t, err)
		require.NoError(t, worker.Run(ctx))
		require.Empty(t, claims.EntryClaims(d.Name), annotation)
		cancel()
	}
}
