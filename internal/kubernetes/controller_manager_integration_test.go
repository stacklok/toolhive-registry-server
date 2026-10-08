//go:build integration

package kubernetes

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	toolhivetypes "github.com/stacklok/toolhive-core/registry/types"
	"github.com/stacklok/toolhive-registry-server/internal/sync/writer"
	"github.com/stacklok/toolhive-registry-server/internal/sync/writer/mocks"
	mcpv1beta1 "github.com/stacklok/toolhive/cmd/thv-operator/api/v1beta1"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

var startupControllerID atomic.Uint64

// TestRegisteredStartupRefresh verifies registration + cache sync + channel
// delivery through a running controller, not a manual Reconcile call. Run with
// KUBEBUILDER_ASSETS set to installed kube-apiserver/etcd/kubectl binaries.
func TestRegisteredStartupRefresh(t *testing.T) {
	preserve := true
	crds := make([]*apiextensionsv1.CustomResourceDefinition, 0, 3)
	for _, kind := range []struct{ singular, plural, kind string }{
		{"mcpserver", "mcpservers", "MCPServer"},
		{"virtualmcpserver", "virtualmcpservers", "VirtualMCPServer"},
		{"mcpremoteproxy", "mcpremoteproxies", "MCPRemoteProxy"},
	} {
		crds = append(crds, &apiextensionsv1.CustomResourceDefinition{
			ObjectMeta: metav1.ObjectMeta{Name: kind.plural + "." + mcpv1beta1.GroupVersion.Group},
			Spec: apiextensionsv1.CustomResourceDefinitionSpec{
				Group:    mcpv1beta1.GroupVersion.Group,
				Names:    apiextensionsv1.CustomResourceDefinitionNames{Plural: kind.plural, Singular: kind.singular, Kind: kind.kind},
				Scope:    apiextensionsv1.NamespaceScoped,
				Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{Name: "v1beta1", Served: true, Storage: true, Schema: &apiextensionsv1.CustomResourceValidation{OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{Type: "object", XPreserveUnknownFields: &preserve}}}},
			},
		})
	}
	useExistingCluster := false
	env := &envtest.Environment{CRDs: crds, UseExistingCluster: &useExistingCluster}
	cfg, err := env.Start()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, env.Stop()) })
	scheme := runtime.NewScheme()
	require.NoError(t, mcpv1beta1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))
	api, err := client.New(cfg, client.Options{Scheme: scheme})
	require.NoError(t, err)
	ctx := context.Background()
	for _, name := range []string{"current", "removed"} {
		require.NoError(t, api.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}))
	}
	annotations := map[string]string{defaultRegistryExportAnnotation: "true", defaultRegistryURLAnnotation: "https://example.com", defaultRegistryDescriptionAnnotation: "current"}
	removed := createTestMCPServerForPredicate(annotations)
	removed.Namespace = "removed"
	require.NoError(t, api.Create(ctx, removed))
	for _, tc := range []struct {
		name  string
		scope []string
	}{
		{"empty scope snapshot", []string{"default"}},
		{"removed namespace excluded", []string{"current"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			events := make(chan *toolhivetypes.UpstreamRegistry, 1)
			mock := mocks.NewMockSyncWriter(gomock.NewController(t))
			destination := fmt.Sprintf("operator-%s-%d", tc.scope[0], startupControllerID.Add(1))
			mock.EXPECT().Store(gomock.Any(), destination, gomock.Any()).AnyTimes().DoAndReturn(func(_ context.Context, _ string, reg *toolhivetypes.UpstreamRegistry, _ ...writer.StoreOption) error {
				select {
				case events <- reg:
				default:
				}
				return nil
			})
			mgr, mgrErr := ctrl.NewManager(cfg, ctrl.Options{Scheme: scheme, LeaderElection: true, LeaderElectionID: "contract-" + tc.scope[0], LeaderElectionNamespace: "default", Metrics: metricsserver.Options{BindAddress: "0"}, Cache: cache.Options{DefaultNamespaces: map[string]cache.Config{tc.scope[0]: {}}}})
			require.NoError(t, mgrErr)
			reconciler := &MCPServerReconciler{syncWriter: mock, registryName: destination, namespaces: tc.scope}
			require.NoError(t, reconciler.SetupWithManager(mgr))
			running, cancel := context.WithCancel(ctx)
			finished := make(chan struct{})
			var startErr error
			go func() {
				startErr = mgr.Start(running)
				close(finished)
			}()
			t.Cleanup(func() {
				cancel()
				select {
				case <-finished:
					require.NoError(t, startErr)
				case <-time.After(20 * time.Second):
					t.Error("manager did not stop after cancellation")
				}
			})
			select {
			case snapshot := <-events:
				require.Empty(t, snapshot.Data.Servers)
			case <-finished:
				require.FailNow(t, "manager stopped before startup delivery", startErr)
			case <-time.After(20 * time.Second):
				require.FailNow(t, "registered startup refresh was not delivered")
			}
		})
	}
}
