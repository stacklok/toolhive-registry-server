package kubernetes

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	toolhivetypes "github.com/stacklok/toolhive-core/registry/types"
	mcpv1beta1 "github.com/stacklok/toolhive/cmd/thv-operator/api/v1beta1"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"github.com/stacklok/toolhive-registry-server/database"
	"github.com/stacklok/toolhive-registry-server/internal/db/sqlc"
	"github.com/stacklok/toolhive-registry-server/internal/sync/writer"
	"github.com/stacklok/toolhive-registry-server/internal/sync/writer/mocks"
)

type snapshotState struct {
	registry *toolhivetypes.UpstreamRegistry
	calls    int
	err      error
}

func newSnapshotMock(t *testing.T) (*mocks.MockSyncWriter, *snapshotState) {
	t.Helper()
	w := mocks.NewMockSyncWriter(gomock.NewController(t))
	state := &snapshotState{}
	w.EXPECT().Store(gomock.Any(), "operator", gomock.Any()).AnyTimes().DoAndReturn(
		func(_ context.Context, _ string, snapshot *toolhivetypes.UpstreamRegistry, opts ...writer.StoreOption) error {
			// This fixture has no per-entry claims, so passing options would
			// silently change the contract being characterized.
			require.Empty(t, opts)
			if state.err != nil {
				return state.err
			}
			state.registry = snapshot
			state.calls++
			return nil
		},
	)
	return w, state
}

type failingListClient struct {
	client.Client
	failKind      string
	failNamespace string
}

func (c failingListClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	options := &client.ListOptions{}
	options.ApplyOptions(opts)
	if fmt.Sprintf("%T", list) == c.failKind && (c.failNamespace == "" || options.Namespace == c.failNamespace) {
		return errors.New("incomplete list")
	}
	return c.Client.List(ctx, list, opts...)
}

type blockingListClient struct {
	client.Client
	started chan struct{}
}

func (c blockingListClient) List(ctx context.Context, _ client.ObjectList, _ ...client.ListOption) error {
	close(c.started)
	<-ctx.Done()
	return ctx.Err()
}

func TestReconcileCancelledDuringListDoesNotStore(t *testing.T) {
	t.Parallel()
	scheme := runtime.NewScheme()
	require.NoError(t, mcpv1beta1.AddToScheme(scheme))
	w, state := newSnapshotMock(t)
	state.registry = &toolhivetypes.UpstreamRegistry{}
	state.registry.Data.Servers = []upstreamv0.ServerJSON{{Name: "stale.example/server", Version: "1.0.0"}}
	started := make(chan struct{})
	r := &MCPServerReconciler{client: blockingListClient{Client: fake.NewClientBuilder().WithScheme(scheme).Build(), started: started}, syncWriter: w, registryName: "operator"}
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan error, 1)
	go func() { _, err := r.Reconcile(ctx, ctrl.Request{}); finished <- err }()
	<-started
	cancel()
	require.ErrorIs(t, <-finished, context.Canceled)
	require.Zero(t, state.calls)
	require.Equal(t, "stale.example/server", state.registry.Data.Servers[0].Name)
}

func TestStartupRefreshCacheSyncFailure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	events := make(chan event.GenericEvent, 1)
	require.ErrorContains(t, enqueueStartupRefresh(ctx, func(context.Context) bool { return false }, events), "cache did not sync")
	require.Empty(t, events)
}

// TestCurrentLimitationCrossKindIdentityCollision exercises the real writer.
// A namespace/name shared by different CR kinds maps to the same public ID;
// the failed replacement must leave the previous export intact.
func TestCurrentLimitationCrossKindIdentityCollision(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, cleanup := database.SetupTestDB(t)
	defer cleanup()
	pool, err := pgxpool.New(ctx, db.Config().ConnString())
	require.NoError(t, err)
	defer pool.Close()
	sourceID, err := sqlc.New(pool).UpsertSource(ctx, sqlc.UpsertSourceParams{Name: "operator", CreationType: sqlc.CreationTypeCONFIG, SourceType: "kubernetes"})
	require.NoError(t, err)
	w, err := writer.NewDBSyncWriter(pool, 65536)
	require.NoError(t, err)
	scheme := runtime.NewScheme()
	require.NoError(t, mcpv1beta1.AddToScheme(scheme))
	annotations := map[string]string{defaultRegistryExportAnnotation: "true", defaultRegistryURLAnnotation: "https://example.com", defaultRegistryDescriptionAnnotation: "original"}
	server := createTestMCPServerForPredicate(annotations)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(server).Build()
	r := &MCPServerReconciler{client: c, syncWriter: w, registryName: "operator", namespaces: []string{server.Namespace}}
	_, err = r.Reconcile(ctx, ctrl.Request{})
	require.NoError(t, err)
	const persisted = `SELECT e.name,v.version,v.description FROM registry_entry e JOIN entry_version v ON v.entry_id=e.id WHERE e.source_id=$1`
	var name, version, description string
	require.NoError(t, pool.QueryRow(ctx, persisted, sourceID).Scan(&name, &version, &description))
	virtual := createTestVirtualMCPServer(server.Name, server.Namespace, annotations)
	require.NoError(t, c.Create(ctx, virtual))
	_, err = r.Reconcile(ctx, ctrl.Request{})
	require.Error(t, err)
	var gotName, gotVersion, gotDescription string
	require.NoError(t, pool.QueryRow(ctx, persisted, sourceID).Scan(&gotName, &gotVersion, &gotDescription))
	require.Equal(t, []string{name, version, description}, []string{gotName, gotVersion, gotDescription})
	var count int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM registry_entry WHERE source_id=$1`, sourceID).Scan(&count))
	require.Equal(t, 1, count)
}

func TestReconcileCompleteConfiguredSnapshot(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	scheme := runtime.NewScheme()
	require.NoError(t, mcpv1beta1.AddToScheme(scheme))
	annotations := map[string]string{
		defaultRegistryExportAnnotation:      "true",
		defaultRegistryURLAnnotation:         "https://example.com",
		defaultRegistryDescriptionAnnotation: "description",
	}
	makeServer := func(namespace, name string) *mcpv1beta1.MCPServer {
		return &mcpv1beta1.MCPServer{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name, Annotations: annotations}, Spec: mcpv1beta1.MCPServerSpec{Image: "example/image:1", Transport: "stdio"}}
	}
	first := makeServer("one", "first")
	second := makeServer("two", "second")
	outside := makeServer("outside", "outside")
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(first, second, outside).Build()
	w, state := newSnapshotMock(t)
	r := &MCPServerReconciler{client: c, syncWriter: w, registryName: "operator", namespaces: []string{"one", "two"}}

	for _, req := range []ctrl.Request{{NamespacedName: client.ObjectKeyFromObject(first)}, {NamespacedName: client.ObjectKeyFromObject(second)}} {
		_, err := r.Reconcile(ctx, req)
		require.NoError(t, err)
		require.Len(t, state.registry.Data.Servers, 2)
	}
	for _, kind := range []string{"*v1beta1.MCPServerList", "*v1beta1.VirtualMCPServerList", "*v1beta1.MCPRemoteProxyList"} {
		r.client = failingListClient{Client: c, failKind: kind, failNamespace: "two"}
		_, err := r.Reconcile(ctx, ctrl.Request{})
		require.Error(t, err)
		require.Equal(t, 2, state.calls)
		require.Equal(t, []string{"com.toolhive.k8s.one/first", "com.toolhive.k8s.two/second"}, []string{state.registry.Data.Servers[0].Name, state.registry.Data.Servers[1].Name})
	}
	r.client = c
	state.err = errors.New("writer unavailable")
	_, err := r.Reconcile(ctx, ctrl.Request{})
	require.ErrorContains(t, err, "writer unavailable")
	require.Equal(t, 2, state.calls)
	state.err = nil
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = r.Reconcile(cancelled, ctrl.Request{})
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 2, state.calls)
	// A deletion from the first namespace must still retain the second.
	require.NoError(t, c.Delete(ctx, first))
	_, err = r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(first)})
	require.NoError(t, err)
	require.Len(t, state.registry.Data.Servers, 1)
	require.NoError(t, c.Delete(ctx, second))
	_, err = r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(second)})
	require.NoError(t, err)
	require.Empty(t, state.registry.Data.Servers)
}

func TestReconcileOperatorKindsAndUpdates(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	scheme := runtime.NewScheme()
	require.NoError(t, mcpv1beta1.AddToScheme(scheme))
	annotations := map[string]string{
		defaultRegistryExportAnnotation: "true", defaultRegistryURLAnnotation: "https://example.com", defaultRegistryDescriptionAnnotation: "original",
	}
	server := createTestMCPServerForPredicate(annotations)
	virtual := createTestVirtualMCPServer("virtual", "default", annotations)
	proxy := createTestMCPRemoteProxy("proxy", "default", annotations)
	ignored := createTestVirtualMCPServer("ignored", "default", map[string]string{defaultRegistryURLAnnotation: "https://example.com", defaultRegistryDescriptionAnnotation: "ignored"})
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(server, virtual, proxy, ignored).Build()
	w, state := newSnapshotMock(t)
	r := &MCPServerReconciler{client: c, syncWriter: w, registryName: "operator", namespaces: []string{"default"}}
	refresh := func(want int) {
		t.Helper()
		_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(server)})
		require.NoError(t, err)
		require.Len(t, state.registry.Data.Servers, want)
	}
	refresh(3)
	virtual.Annotations = map[string]string{
		defaultRegistryExportAnnotation: "true", defaultRegistryURLAnnotation: "https://example.com", defaultRegistryDescriptionAnnotation: "updated",
	}
	require.NoError(t, c.Update(ctx, virtual))
	refresh(3)
	found := false
	for _, entry := range state.registry.Data.Servers {
		if entry.Description == "updated" {
			found = true
		}
	}
	require.True(t, found)
	proxy.Annotations = map[string]string{defaultRegistryExportAnnotation: "false"}
	require.NoError(t, c.Update(ctx, proxy))
	refresh(2)
	require.NoError(t, c.Delete(ctx, server))
	refresh(1)
}

func TestStartupRefreshDropsRemovedNamespace(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	scheme := runtime.NewScheme()
	require.NoError(t, mcpv1beta1.AddToScheme(scheme))
	annotations := map[string]string{
		defaultRegistryExportAnnotation: "true", defaultRegistryURLAnnotation: "https://example.com", defaultRegistryDescriptionAnnotation: "description",
	}
	old := createTestMCPServerForPredicate(annotations)
	old.Namespace = "removed"
	current := createTestMCPServerForPredicate(annotations)
	current.Namespace = "current"
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(old, current).Build()
	w, state := newSnapshotMock(t)
	state.registry = &toolhivetypes.UpstreamRegistry{}
	state.registry.Data.Servers = []upstreamv0.ServerJSON{{Name: "stale.example/server", Version: "1.0.0"}}
	r := &MCPServerReconciler{client: c, syncWriter: w, registryName: "operator", namespaces: []string{"current"}}
	startup := make(chan event.GenericEvent)
	finished := make(chan error, 1)
	go func() { finished <- enqueueStartupRefresh(ctx, func(context.Context) bool { return true }, startup) }()
	initialEvent := <-startup
	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(initialEvent.Object)})
	require.NoError(t, err)
	require.Len(t, state.registry.Data.Servers, 1)
	require.Equal(t, "com.toolhive.k8s.current/test-server", state.registry.Data.Servers[0].Name)
	cancel()
	require.NoError(t, <-finished)
}

func TestReconcileAllNamespacesAndStartupEmpty(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	scheme := runtime.NewScheme()
	require.NoError(t, mcpv1beta1.AddToScheme(scheme))
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	w, state := newSnapshotMock(t)
	state.registry = &toolhivetypes.UpstreamRegistry{}
	state.registry.Data.Servers = []upstreamv0.ServerJSON{{Name: "stale.example/server", Version: "1.0.0"}}
	r := &MCPServerReconciler{client: c, syncWriter: w, registryName: "operator"}
	startup := make(chan event.GenericEvent)
	finished := make(chan error, 1)
	go func() {
		finished <- enqueueStartupRefresh(ctx, func(context.Context) bool { return true }, startup)
	}()
	initialEvent := <-startup
	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(initialEvent.Object)})
	require.NoError(t, err)
	require.Equal(t, 1, state.calls)
	require.Empty(t, state.registry.Data.Servers)
	annotations := map[string]string{
		defaultRegistryExportAnnotation: "true", defaultRegistryURLAnnotation: "https://example.com", defaultRegistryDescriptionAnnotation: "description",
	}
	for _, namespace := range []string{"one", "two"} {
		obj := createTestMCPServerForPredicate(annotations)
		obj.Namespace = namespace
		require.NoError(t, c.Create(ctx, obj))
	}
	_, err = r.Reconcile(ctx, ctrl.Request{})
	require.NoError(t, err)
	require.Len(t, state.registry.Data.Servers, 2)
	cancel()
	require.NoError(t, <-finished)
}
