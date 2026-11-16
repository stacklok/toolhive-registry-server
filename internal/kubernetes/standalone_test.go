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

func TestStandaloneOptedInRetiredAnnotationBlocksBeforeConversion(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"MCPServer", "VirtualMCPServer", "MCPRemoteProxy"} {
		for _, value := range []string{"", "{malformed sensitive value}", `{"team":"private"}`} {
			t.Run(kind+"/"+value, func(t *testing.T) {
				t.Parallel()
				// No URL/description: conversion would skip this object. The
				// standalone compatibility check must still reject its presence.
				obj := exportObject(kind, map[string]string{exportKey: "true", retiredKey: value})
				api := fake.NewClientBuilder().WithScheme(standaloneScheme(t)).WithObjects(obj).Build()
				d := standaloneDefinition()
				require.ErrorIs(t, kubernetes.PreflightStandaloneOperator(t.Context(), api, d), kubernetes.ErrRetiredExportClaims)
				jobs := mocks.NewMockJobs(gomock.NewController(t))
				job := persistence.Job{Source: d, LeaseID: "lease", Generation: 1}
				jobs.EXPECT().Acquire(gomock.Any(), d.Name, time.Minute).Return(job, nil)
				jobs.EXPECT().Fail(gomock.Any(), job, persistence.FailureFetch).Return(nil)
				var alerts []error
				runner, err := kubernetes.NewStandaloneOperator(api, jobs, d, standaloneOptions(), func(err error) { alerts = append(alerts, err) })
				require.NoError(t, err)
				require.False(t, runner.Ready())
				require.ErrorIs(t, runner.Run(t.Context()), ingest.ErrFetch)
				require.Equal(t, []error{kubernetes.ErrRetiredExportClaims}, alerts)
				require.False(t, runner.Ready())
			})
		}
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

func TestStandaloneIntroducedAnnotationAlertsAndRetainsLastSnapshot(t *testing.T) {
	t.Parallel()
	for _, empty := range []bool{false, true} {
		t.Run(map[bool]string{false: "explicit-metadata", true: "valid-empty-startup"}[empty], func(t *testing.T) {
			t.Parallel()
			obj := exportObject("MCPServer", map[string]string{exportKey: "true", "toolhive.stacklok.dev/registry-url": "https://export.example/mcp", "toolhive.stacklok.dev/registry-description": "export description"})
			builder := fake.NewClientBuilder().WithScheme(standaloneScheme(t))
			if !empty {
				builder.WithObjects(obj)
			}
			api := builder.Build()
			d := standaloneDefinition()
			jobs := mocks.NewMockJobs(gomock.NewController(t))
			job := persistence.Job{Source: d, LeaseID: "lease"}
			jobs.EXPECT().Acquire(gomock.Any(), d.Name, time.Minute).Return(job, nil).AnyTimes()
			jobs.EXPECT().Status(gomock.Any(), d.Name).Return(persistence.JobStatus{SourceID: d.ID}, nil)
			committed := make(chan struct{})
			jobs.EXPECT().CommitSnapshot(gomock.Any(), job, gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, _ persistence.Job, _ persistence.SyncResult, snapshot model.Snapshot) error {
				if empty {
					require.Empty(t, snapshot.Data.Servers)
				} else {
					require.Len(t, snapshot.Data.Servers, 1)
					require.Equal(t, "https://export.example/mcp", snapshot.Data.Servers[0].Remotes[0].URL)
					require.Equal(t, "export description", snapshot.Data.Servers[0].Description)
				}
				close(committed)
				return nil
			})
			failed := make(chan struct{}, 1)
			jobs.EXPECT().Fail(gomock.Any(), job, persistence.FailureFetch).DoAndReturn(func(context.Context, persistence.Job, persistence.FailureCategory) error {
				select {
				case failed <- struct{}{}:
				default:
				}
				return nil
			}).AnyTimes()
			alerts := make(chan error, 8)
			runner, err := kubernetes.NewStandaloneOperator(api, jobs, d, standaloneOptions(), func(err error) {
				select {
				case alerts <- err:
				default:
				}
			})
			require.NoError(t, err)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- runner.Run(ctx) }()
			select {
			case <-committed:
			case <-time.After(time.Second):
				t.Fatal("startup did not commit")
			}
			require.Eventually(t, runner.Ready, time.Second, time.Millisecond)
			obj.GetAnnotations()[retiredKey] = ""
			if empty {
				require.NoError(t, api.Create(t.Context(), obj))
			} else {
				require.NoError(t, api.Update(t.Context(), obj))
			}
			select {
			case err := <-alerts:
				require.ErrorIs(t, err, kubernetes.ErrRetiredExportClaims)
			case <-time.After(time.Second):
				t.Fatal("compatibility alert missing")
			}
			select {
			case <-failed:
			case <-time.After(time.Second):
				t.Fatal("failed snapshot not recorded")
			}
			require.True(t, runner.Ready())
			cancel()
			// Cancellation may race with the already-observed refused refresh.
			// Its classified failure must not be silently normalized away.
			if err := <-done; err != nil {
				require.ErrorIs(t, err, ingest.ErrFetch)
			}
			require.NoError(t, api.List(t.Context(), &mcp.MCPRemoteProxyList{}))
		})
	}
}
