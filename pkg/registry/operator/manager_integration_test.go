//go:build integration

package operator_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	mcp "github.com/stacklok/toolhive/cmd/thv-operator/api/v1beta1"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	ext "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	metrics "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/ingest"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/operator"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/reconcile"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/reconcile/mocks"
)

func TestRegisteredOperator(t *testing.T) {
	t.Parallel()
	preserve := true
	var crds []*ext.CustomResourceDefinition
	for _, kind := range []struct{ singular, plural, kind string }{{"mcpserver", "mcpservers", "MCPServer"}, {"virtualmcpserver", "virtualmcpservers", "VirtualMCPServer"}, {"mcpremoteproxy", "mcpremoteproxies", "MCPRemoteProxy"}} {
		crds = append(crds, &ext.CustomResourceDefinition{ObjectMeta: metav1.ObjectMeta{Name: kind.plural + "." + mcp.GroupVersion.Group}, Spec: ext.CustomResourceDefinitionSpec{Group: mcp.GroupVersion.Group, Names: ext.CustomResourceDefinitionNames{Plural: kind.plural, Singular: kind.singular, Kind: kind.kind}, Scope: ext.NamespaceScoped, Versions: []ext.CustomResourceDefinitionVersion{{Name: "v1beta1", Served: true, Storage: true, Schema: &ext.CustomResourceValidation{OpenAPIV3Schema: &ext.JSONSchemaProps{Type: "object", XPreserveUnknownFields: &preserve}}}}}})
	}
	existing := false
	env := &envtest.Environment{CRDs: crds, UseExistingCluster: &existing}
	cfg, err := env.Start()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, env.Stop()) })
	s := scheme(t)
	require.NoError(t, corev1.AddToScheme(s))
	api, err := client.NewWithWatch(cfg, client.Options{Scheme: s})
	require.NoError(t, err)
	for _, namespace := range []string{"one", "two"} {
		require.NoError(t, api.Create(t.Context(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}))
	}
	server := &mcp.MCPServer{ObjectMeta: metadata("server", "one"), Spec: mcp.MCPServerSpec{Image: "example.org/image:1.0.0"}}
	virtual := &mcp.VirtualMCPServer{ObjectMeta: metadata("virtual", "two")}
	proxy := &mcp.MCPRemoteProxy{ObjectMeta: metadata("proxy", "two")}
	for _, obj := range []client.Object{server, virtual, proxy} {
		require.NoError(t, api.Create(t.Context(), obj))
	}

	for _, empty := range []bool{false, true} {
		events := make(chan model.Snapshot, 100)
		jobs := mocks.NewMockJobs(gomock.NewController(t))
		job := persistence.Job{Source: definition(), LeaseID: "lease", Generation: 1}
		jobs.EXPECT().Acquire(gomock.Any(), job.Source.Name, time.Minute).AnyTimes().Return(job, nil)
		jobs.EXPECT().Status(gomock.Any(), job.Source.Name).AnyTimes().Return(persistence.JobStatus{SourceID: job.Source.ID}, nil)
		jobs.EXPECT().CommitSnapshot(gomock.Any(), job, gomock.Any(), gomock.Any()).AnyTimes().DoAndReturn(func(ctx context.Context, _ persistence.Job, _ persistence.SyncResult, snapshot model.Snapshot) error {
			select {
			case events <- snapshot:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		jobs.EXPECT().Fail(gomock.Any(), job, persistence.FailureCancelled).AnyTimes().Return(nil)
		runner, err := operator.NewRunner(api, jobs, definition(), reconcile.Options{PollInterval: time.Hour, LeaseDuration: time.Minute})
		require.NoError(t, err)
		mgr, err := ctrl.NewManager(cfg, ctrl.Options{Scheme: s, LeaderElection: true, LeaderElectionID: "public-operator", LeaderElectionNamespace: "default", LeaderElectionReleaseOnCancel: true, Metrics: metrics.Options{BindAddress: "0"}, HealthProbeBindAddress: "0"})
		require.NoError(t, err)
		require.NoError(t, runner.Register(mgr))
		require.False(t, runner.Ready())
		stop, stopped, result := startRegisteredManager(t, mgr)
		waitSnapshot := func(count int) {
			t.Helper()
			timer := time.NewTimer(20 * time.Second)
			defer timer.Stop()
			for {
				select {
				case snap := <-events:
					if len(snap.Data.Servers) == count {
						return
					}
				case <-stopped:
					require.FailNow(t, "manager stopped", result())
				case <-timer.C:
					require.FailNow(t, "snapshot not delivered")
				}
			}
		}
		if empty {
			waitSnapshot(0)
		} else {
			waitSnapshot(3)
			require.Eventually(t, runner.Ready, time.Second, time.Millisecond)
			virtual.Annotations[export] = "false"
			require.NoError(t, api.Update(t.Context(), virtual))
			waitSnapshot(2)
			proxy.Annotations["toolhive.stacklok.dev/registry-description"] = "updated"
			require.NoError(t, api.Update(t.Context(), proxy))
			require.NoError(t, api.Delete(t.Context(), server))
			waitSnapshot(1)
		}
		stop()
		require.NoError(t, result())
		require.False(t, runner.Ready())
		if !empty {
			require.NoError(t, api.Delete(t.Context(), proxy))
		} // deleted during downtime: next startup must clear it
	}

	for _, mode := range []string{"startup watch failure", "startup list failure", "busy then success", "watch error after ready", "watch closure after ready"} {
		t.Run(mode, func(t *testing.T) {
			jobs := mocks.NewMockJobs(gomock.NewController(t))
			job := persistence.Job{Source: definition(), LeaseID: "lease", Generation: 1}
			// Controlled watches use the real direct API client for lists. The runner
			// deliberately bypasses the manager cache; cache sync is not its barrier.
			watches := make([]*watch.RaceFreeFakeWatcher, 6)
			for i := range watches {
				watches[i] = watch.NewRaceFreeFake()
			}
			var watchCalls, acquireCalls, listCalls atomic.Int32
			var allowAcquire, acquired atomic.Bool
			api := interceptor.NewClient(api, interceptor.Funcs{
				Watch: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) (watch.Interface, error) {
					i := int(watchCalls.Add(1)) - 1
					if mode == "startup watch failure" && i == 2 {
						return nil, errors.New("injected watch access failure")
					}
					return watches[i], nil
				},
				List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
					listCalls.Add(1)
					if !acquired.Load() {
						t.Error("List before Acquire succeeded")
						return persistence.ErrInvalid
					}
					if mode == "startup list failure" {
						return errors.New("injected list access failure")
					}
					return c.List(ctx, list, opts...)
				},
			})
			entered, release := make(chan struct{}, 1), make(chan struct{})
			if mode != "startup watch failure" {
				jobs.EXPECT().Acquire(gomock.Any(), job.Source.Name, time.Minute).AnyTimes().DoAndReturn(func(context.Context, string, time.Duration) (persistence.Job, error) {
					acquireCalls.Add(1)
					if mode == "busy then success" && !allowAcquire.Load() {
						return persistence.Job{}, persistence.ErrBusy
					}
					acquired.Store(true)
					return job, nil
				})
				if mode == "startup list failure" {
					jobs.EXPECT().Fail(gomock.Any(), job, persistence.FailureFetch).Return(nil)
				} else {
					jobs.EXPECT().Status(gomock.Any(), job.Source.Name).AnyTimes().Return(persistence.JobStatus{SourceID: job.Source.ID}, nil)
					jobs.EXPECT().CommitSnapshot(gomock.Any(), job, gomock.Any(), gomock.Any()).AnyTimes().DoAndReturn(func(ctx context.Context, _ persistence.Job, _ persistence.SyncResult, snapshot model.Snapshot) error {
						if !acquired.Load() {
							t.Error("Commit before Acquire succeeded")
						}
						require.Empty(t, snapshot.Data.Servers)
						select {
						case entered <- struct{}{}:
						default:
						}
						select {
						case <-release:
							return nil
						case <-ctx.Done():
							return ctx.Err()
						}
					})
					jobs.EXPECT().Fail(gomock.Any(), job, persistence.FailureCancelled).AnyTimes().Return(nil)
				}
			}
			runner, err := operator.NewRunner(api, jobs, definition(), reconcile.Options{PollInterval: 20 * time.Millisecond, LeaseDuration: time.Minute})
			require.NoError(t, err)
			mgr, err := ctrl.NewManager(cfg, ctrl.Options{Scheme: s, Metrics: metrics.Options{BindAddress: "0"}, HealthProbeBindAddress: "0"})
			require.NoError(t, err)
			require.NoError(t, runner.Register(mgr))
			require.False(t, runner.Ready())
			stop, stopped, result := startRegisteredManager(t, mgr)
			if mode == "startup watch failure" || mode == "startup list failure" {
				select {
				case <-stopped:
				case <-time.After(20 * time.Second):
					t.Fatal("startup failure did not reach manager Start")
				}
				if mode == "startup watch failure" {
					require.ErrorIs(t, result(), operator.ErrWatch)
					require.Zero(t, acquireCalls.Load())
					require.Zero(t, listCalls.Load())
				} else {
					require.ErrorIs(t, result(), ingest.ErrFetch)
				}
			} else {
				if mode == "busy then success" {
					require.Eventually(t, func() bool { return acquireCalls.Load() >= 2 }, 20*time.Second, time.Millisecond)
					require.False(t, runner.Ready())
					require.Zero(t, listCalls.Load())
					select {
					case <-entered:
						t.Fatal("Commit before Acquire succeeded")
					default:
					}
					allowAcquire.Store(true)
				}
				select {
				case <-entered:
				case <-stopped:
					require.FailNow(t, "manager stopped before commit", result())
				case <-time.After(20 * time.Second):
					t.Fatal("startup snapshot did not reach commit")
				}
				require.False(t, runner.Ready())
				close(release)
				require.Eventually(t, runner.Ready, time.Second, time.Millisecond)
				if mode == "busy then success" {
					stop()
					require.NoError(t, result())
				} else {
					if mode == "watch error after ready" {
						watches[0].Error(&metav1.Status{Reason: metav1.StatusReasonForbidden})
					} else {
						watches[0].Stop()
					}
					select {
					case <-stopped:
					case <-time.After(20 * time.Second):
						t.Fatal("runtime watch failure did not reach manager Start")
					}
					require.ErrorIs(t, result(), operator.ErrWatch)
				}
			}
			require.False(t, runner.Ready())
			owned := int(watchCalls.Load())
			if mode == "startup watch failure" {
				owned-- // The failed watch was never returned to the runner.
			}
			for _, w := range watches[:owned] {
				require.True(t, w.IsStopped(), "runner must stop every owned watch")
			}
			// The borrowed API client remains usable after the manager's host policy
			// handles Run's error. Only the host cancels/stops the manager.
			require.NoError(t, api.Get(t.Context(), client.ObjectKey{Name: "one"}, &corev1.Namespace{}))
		})
	}
}

// A closed channel broadcasts completion without consuming the error. Cleanup
// can always cancel and join, even after a failure branch observes completion.
func startRegisteredManager(t *testing.T, mgr ctrl.Manager) (func(), <-chan struct{}, func() error) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	stopped := make(chan struct{})
	var result error
	go func() {
		result = mgr.Start(ctx)
		close(stopped)
	}()
	stop := func() {
		t.Helper()
		cancel()
		select {
		case <-stopped:
		case <-time.After(20 * time.Second):
			t.Fatal("manager shutdown stalled")
		}
	}
	t.Cleanup(stop)
	return stop, stopped, func() error { <-stopped; return result }
}
