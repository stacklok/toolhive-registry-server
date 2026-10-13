package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
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

	"github.com/stacklok/toolhive-registry-server/internal/app/storage"
	storagemocks "github.com/stacklok/toolhive-registry-server/internal/app/storage/mocks"
	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/db"
	"github.com/stacklok/toolhive-registry-server/internal/kubernetes"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/ingest"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/reconcile"
	fetchmocks "github.com/stacklok/toolhive-registry-server/pkg/registry/reconcile/mocks"
)

func TestCatalogOperatorAuthoritativeGateBeforeConsume(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"MCPServer", "VirtualMCPServer", "MCPRemoteProxy"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			cfg := catalogTestConfig(t)
			conn, pool := catalogPG(t, cfg)
			cfg.Sources = append(cfg.Sources, config.SourceConfig{Name: "operator", Kubernetes: &config.KubernetesConfig{}})
			cfg.WatchNamespace = "two,one"
			approveCatalog(t, conn, cfg, true)
			scheme := runtime.NewScheme()
			require.NoError(t, mcp.AddToScheme(scheme))
			meta := metav1.ObjectMeta{Name: "exported", Namespace: "two", Annotations: map[string]string{"toolhive.stacklok.dev/registry-export": "true", "toolhive.stacklok.dev/authz-claims": ""}}
			var object client.Object
			switch kind {
			case "MCPServer":
				object = &mcp.MCPServer{ObjectMeta: meta}
			case "VirtualMCPServer":
				object = &mcp.VirtualMCPServer{ObjectMeta: meta}
			default:
				object = &mcp.MCPRemoteProxy{ObjectMeta: meta}
			}
			var watches atomic.Int32
			api := fake.NewClientBuilder().WithScheme(scheme).WithObjects(object).WithInterceptorFuncs(interceptor.Funcs{Watch: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) (watch.Interface, error) {
				watches.Add(1)
				return nil, errors.New("unexpected watch")
			}}).Build()
			revision := catalogRevision(t, cfg)
			before, err := db.InventoryExposure(t.Context(), conn, revision)
			require.NoError(t, err)
			_, err = NewCatalogApp(t.Context(), CatalogOptions{Config: cfg, Storage: storage.CatalogPostgres{BorrowedPool: pool}, Operator: &kubernetes.StandaloneFactory{API: api}})
			require.ErrorIs(t, err, kubernetes.ErrRetiredExportClaims)
			require.Zero(t, watches.Load())
			after, err := db.InventoryExposure(t.Context(), conn, revision)
			require.NoError(t, err)
			require.Equal(t, before.Digest, after.Digest)
			var consumed bool
			require.NoError(t, conn.QueryRow(t.Context(), `SELECT consumed_at IS NOT NULL FROM exposure_acknowledgement`).Scan(&consumed))
			require.False(t, consumed)
		})
	}
}

func TestCatalogHistoricalClaimedPayloadRequiresRetirement(t *testing.T) {
	t.Parallel()
	cfg := catalogTestConfig(t)
	conn, pool := catalogPG(t, cfg)
	cfg.Sources = nil
	cfg.Registries = nil
	_, err := conn.Exec(t.Context(), `INSERT INTO source(name,source_type,source_config,syncable,creation_type,claims) VALUES('history','managed','{}',false,'API','{"team":"private"}');
 INSERT INTO registry(name,creation_type,claims) VALUES('history','API','{}');
 INSERT INTO registry_source(registry_id,source_id,position) SELECT r.id,s.id,0 FROM registry r,source s;
 INSERT INTO registry_entry(source_id,entry_type,name,claims) SELECT id,'MCP','com.example/historical','{"team":"private"}' FROM source;
 INSERT INTO entry_version(entry_id,name,version,description) SELECT id,name,'1','reviewed historical payload' FROM registry_entry;
 INSERT INTO mcp_server(version_id) SELECT id FROM entry_version`)
	require.NoError(t, err)
	_, err = NewCatalogApp(t.Context(), CatalogOptions{Config: cfg, Storage: storage.CatalogPostgres{BorrowedPool: pool}})
	require.Error(t, err)
	revision := catalogRevision(t, cfg)
	inventory, err := db.InventoryExposure(t.Context(), conn, revision)
	require.NoError(t, err)
	ack, err := db.AcknowledgeExposure(t.Context(), conn, revision, inventory.Digest, true, true, false)
	require.NoError(t, err)
	_, err = NewCatalogApp(t.Context(), CatalogOptions{Config: cfg, Storage: storage.CatalogPostgres{BorrowedPool: pool}})
	require.ErrorContains(t, err, "retirement")
	_, err = db.RetireExposureClaims(t.Context(), conn, revision, ack.ID, ack.PostFenceDigest, true, true)
	require.NoError(t, err)
	app := constructCatalog(t, cfg, pool, nil)
	require.NoError(t, app.Start())
	public, _ := app.Addresses()
	data, _ := catalogRequest(t, public, "GET", "/registry/history/v0.1/servers", "", "", 200)
	require.Contains(t, string(data), "reviewed historical payload")
	require.NotContains(t, string(data), "private")
	require.NoError(t, app.Stop())
}

func TestCatalogInlinePostCommitFailureRetainsSnapshotAndTruthfulAudit(t *testing.T) {
	t.Parallel()
	cfg := catalogTestConfig(t)
	conn, pool := catalogPG(t, cfg)
	issuer, token := catalogJWKS(t)
	cfg.Auth = &config.AuthConfig{Mode: config.AuthModeOAuth, InsecureAllowHTTP: true, OAuth: &config.OAuthConfig{ResourceURL: "https://catalog.example.org", Providers: []config.OAuthProviderConfig{{Name: "test", IssuerURL: issuer.URL, Audience: "catalog-test", JwksUrl: issuer.URL + "/jwks", AllowPrivateIP: true}}}}
	cfg.Admin.Enabled = true
	cfg.Authorization.AllowAnonymousCatalog = false
	require.NoError(t, os.WriteFile(cfg.Authorization.PolicyFile, []byte(`permit(principal,action,resource) when { principal.authenticated };`), 0600))
	logfile := filepath.Join(t.TempDir(), "audit.json")
	cfg.Audit = &config.AuditConfig{Enabled: true, LogFile: logfile}
	approveCatalog(t, conn, cfg, true)
	ctrl := gomock.NewController(t)
	fetcher := fetchmocks.NewMockFetcher(ctrl)
	var fail atomic.Bool
	fetcher.EXPECT().Fetch(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, d persistence.SourceDefinition) (ingest.Result, error) {
		if fail.Load() {
			return ingest.Result{}, errors.New("SECRET callback failure")
		}
		return ingest.New(ingest.Options{}).Fetch(ctx, d)
	}).AnyTimes()
	app := constructCatalog(t, cfg, pool, fetcher)
	require.NoError(t, app.Start())
	public, _ := app.Addresses()
	require.Eventually(t, func() bool { return app.runtime.Ready() }, time.Second, 5*time.Millisecond)
	payload := `{"version":"1.0.0","meta":{"last_updated":"2025-01-15T10:30:00Z"},"data":{"servers":[{"$schema":"https://example.org/server.schema.json","name":"com.example/inline","description":"last good","version":"1"}]}}`
	body, err := json.Marshal(map[string]any{"file": map[string]any{"data": payload}})
	require.NoError(t, err)
	credential := token("operator")
	catalogRequest(t, public, "PUT", "/v1/sources/inline", string(body), credential, 201)
	catalogRequest(t, public, "PUT", "/v1/registries/inline", `{"sources":["inline"]}`, credential, 201)
	fail.Store(true)
	updated := strings.Replace(payload, "last good", "attempted new", 1)
	body, err = json.Marshal(map[string]any{"file": map[string]any{"data": updated}})
	require.NoError(t, err)
	data, _ := catalogRequest(t, public, "PUT", "/v1/sources/inline", string(body), credential, 503)
	require.Contains(t, string(data), `"committed":true`)
	require.NotContains(t, string(data), "SECRET")
	data, _ = catalogRequest(t, public, "GET", "/registry/inline/v0.1/servers", "", credential, 200)
	require.Contains(t, string(data), "last good")
	require.NotContains(t, string(data), "attempted new")
	// A failed creation must report source.create even though HTTP is 503.
	catalogRequest(t, public, "PUT", "/v1/sources/failed-new", string(body), credential, 503)
	// A real concurrent fetch failure remains a shutdown result, not a log-only error.
	if err := app.Stop(); err != nil {
		require.ErrorIs(t, err, ingest.ErrFetch)
	}
	auditData, err := os.ReadFile(logfile)
	require.NoError(t, err)
	var committedCreate bool
	for _, line := range strings.Split(string(auditData), "\n") {
		if strings.Contains(line, "source.create") && strings.Contains(line, "failed-new") {
			require.Contains(t, line, "mutation_committed")
			committedCreate = true
		}
	}
	require.True(t, committedCreate)
	require.NotContains(t, string(auditData), "SECRET")
	require.NotContains(t, string(auditData), credential)
}

func TestCatalogRunCancellationBeforeStartIsClean(t *testing.T) {
	t.Parallel()
	cfg := catalogTestConfig(t)
	conn, pool := catalogPG(t, cfg)
	approveCatalog(t, conn, cfg, true)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	app, err := NewCatalogApp(ctx, CatalogOptions{Config: cfg, Storage: storage.CatalogPostgres{BorrowedPool: pool}})
	require.NoError(t, err)
	cancel()
	require.NoError(t, app.Run())
	require.NoError(t, pool.Ping(t.Context()))
}

func TestCatalogRenewDeadlinePropagatesThroughWait(t *testing.T) {
	t.Parallel()
	cfg := catalogTestConfig(t)
	conn, pool := catalogPG(t, cfg)
	approveCatalog(t, conn, cfg, true)
	backend, err := (storage.CatalogPostgres{BorrowedPool: pool}).Open(t.Context(), cfg)
	require.NoError(t, err)
	ctrl := gomock.NewController(t)
	factory := storagemocks.NewMockCatalogFactory(ctrl)
	store := storagemocks.NewMockCatalogStorage(ctrl)
	jobs, fetcher := fetchmocks.NewMockJobs(ctrl), fetchmocks.NewMockFetcher(ctrl)
	ttl := 60 * time.Millisecond
	job := persistence.Job{Source: persistence.SourceDefinition{ID: "source-id", Name: "source", File: &persistence.FileSpec{Path: "/registry.json"}, Schedule: "1h"}, LeaseID: "lease", Generation: 1}
	jobs.EXPECT().ClaimNext(gomock.Any(), ttl).Return(job, true, nil)
	fetcher.EXPECT().Fetch(gomock.Any(), job.Source).DoAndReturn(func(ctx context.Context, _ persistence.SourceDefinition) (ingest.Result, error) {
		<-ctx.Done()
		return ingest.Result{}, ctx.Err()
	})
	jobs.EXPECT().Renew(gomock.Any(), job, ttl).DoAndReturn(func(ctx context.Context, j persistence.Job, _ time.Duration) (persistence.Job, error) {
		<-ctx.Done()
		return j, ctx.Err()
	})
	jobs.EXPECT().Fail(gomock.Any(), job, persistence.FailureStorage).Return(nil)
	factory.EXPECT().Open(gomock.Any(), gomock.Any()).Return(store, nil)
	store.EXPECT().Preflight(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil)
	store.EXPECT().Consume(gomock.Any(), catalogRevision(t, cfg)).DoAndReturn(backend.Consume)
	store.EXPECT().Initialize(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, s []persistence.SourceDefinition, v []persistence.ViewDefinition) (storage.CatalogComponents, error) {
		components, err := backend.Initialize(ctx, s, v)
		components.Jobs = jobs
		return components, err
	})
	store.EXPECT().Close().Do(backend.Close)
	app, err := NewCatalogApp(t.Context(), CatalogOptions{Config: cfg, Storage: factory, Fetcher: fetcher,
		Reconcile: reconcile.Options{PollInterval: time.Hour, LeaseDuration: ttl},
		HTTP:      []RegistryAppOptions{WithAddress("127.0.0.1:0"), WithInternalAddress("127.0.0.1:0")}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = app.Stop() })
	require.NoError(t, app.Start())
	err = app.Wait()
	require.NoError(t, t.Context().Err())
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.ErrorIs(t, err, persistence.ErrUnavailable)
	require.ErrorIs(t, app.Stop(), persistence.ErrUnavailable)
	require.NoError(t, pool.Ping(t.Context()))
}

func TestCatalogTerminalRuntimeErrorJoinsAndClosesExactlyOnce(t *testing.T) {
	t.Parallel()
	cfg := catalogTestConfig(t)
	conn, pool := catalogPG(t, cfg)
	approveCatalog(t, conn, cfg, true)
	backend, err := (storage.CatalogPostgres{BorrowedPool: pool}).Open(t.Context(), cfg)
	require.NoError(t, err)
	ctrl := gomock.NewController(t)
	factory := storagemocks.NewMockCatalogFactory(ctrl)
	store := storagemocks.NewMockCatalogStorage(ctrl)
	jobs := fetchmocks.NewMockJobs(ctrl)
	jobs.EXPECT().ClaimNext(gomock.Any(), time.Minute).Return(persistence.Job{}, false, persistence.ErrUnavailable)
	factory.EXPECT().Open(gomock.Any(), gomock.Any()).Return(store, nil)
	store.EXPECT().Preflight(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil)
	store.EXPECT().Consume(gomock.Any(), catalogRevision(t, cfg)).DoAndReturn(backend.Consume)
	store.EXPECT().Initialize(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, s []persistence.SourceDefinition, v []persistence.ViewDefinition) (storage.CatalogComponents, error) {
		components, err := backend.Initialize(ctx, s, v)
		if err != nil {
			return components, err
		}
		components.Jobs = jobs
		return components, nil
	})
	store.EXPECT().Close().Do(backend.Close)
	app, err := NewCatalogApp(t.Context(), CatalogOptions{Config: cfg, Storage: factory, HTTP: []RegistryAppOptions{WithAddress("127.0.0.1:0"), WithInternalAddress("127.0.0.1:0")}})
	require.NoError(t, err)
	require.NoError(t, app.Start())
	require.ErrorIs(t, app.Wait(), persistence.ErrUnavailable)
	require.ErrorIs(t, app.Stop(), persistence.ErrUnavailable)
	require.NoError(t, pool.Ping(t.Context()))
}
