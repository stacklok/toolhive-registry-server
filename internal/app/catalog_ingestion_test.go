package app

import (
	"context"
	"os"
	"testing"
	"time"

	mcp "github.com/stacklok/toolhive/cmd/thv-operator/api/v1beta1"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/stacklok/toolhive-registry-server/internal/app/storage"
	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/kubernetes"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/ingest"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/postgres"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/reconcile"
)

func TestCatalogFirstMatchingHashNeedsNewBaselineAndFencesOldWork(t *testing.T) {
	t.Parallel()
	cfg := catalogTestConfig(t)
	conn, pool := catalogPG(t, cfg)
	cfg.Sources = nil
	cfg.Registries = nil
	defs, err := postgres.NewDefinitions(pool)
	require.NoError(t, err)
	payload := `{"version":"1.0.0","meta":{"last_updated":"2025-01-15T10:30:00Z"},"data":{"servers":[{"$schema":"https://example.org/server.schema.json","name":"com.example/inline","description":"historical","version":"1"}]}}`
	source, err := defs.CreateSource(t.Context(), persistence.SourceDefinition{Name: "unlinked", File: &persistence.FileSpec{Data: payload}})
	require.NoError(t, err)
	jobs, err := postgres.NewJobs(pool, 65536)
	require.NoError(t, err)
	coordinator, err := reconcile.New(jobs, ingest.New(ingest.Options{}), reconcile.Options{PollInterval: time.Second, LeaseDuration: time.Minute})
	require.NoError(t, err)
	require.NoError(t, coordinator.Sync(t.Context(), source.Name))
	oldStatus, err := jobs.Status(t.Context(), source.Name)
	require.NoError(t, err)
	require.True(t, oldStatus.BaselineValid)
	oldWork, err := jobs.Acquire(t.Context(), source.Name, time.Second)
	require.NoError(t, err)
	_, err = conn.Exec(t.Context(), `UPDATE registry_sync SET lease_expires_at=clock_timestamp()-interval '1 second'`)
	require.NoError(t, err)
	approveCatalog(t, conn, cfg, false)
	app := constructCatalog(t, cfg, pool, nil)
	pending, err := jobs.Status(t.Context(), source.Name)
	require.NoError(t, err)
	require.False(t, pending.BaselineValid)
	require.NoError(t, app.Start())
	require.Eventually(t, func() bool {
		status, err := jobs.Status(t.Context(), source.Name)
		return err == nil && status.BaselineValid
	}, 3*time.Second, 10*time.Millisecond)
	current, err := jobs.Status(t.Context(), source.Name)
	require.NoError(t, err)
	require.Equal(t, oldStatus.LastHash, current.LastHash)
	fetched, err := ingest.New(ingest.Options{}).Fetch(t.Context(), source)
	require.NoError(t, err)
	require.ErrorIs(t, jobs.CommitSnapshot(t.Context(), oldWork, persistence.SyncResult{Hash: oldStatus.LastHash, FilterHash: oldStatus.LastFilterHash}, fetched.Snapshot), persistence.ErrStale)
	require.NoError(t, app.Stop())
}

func TestCatalogOperatorEmptyStartupAndAPIScopeCRUD(t *testing.T) {
	t.Parallel()
	cfg := catalogTestConfig(t)
	conn, pool := catalogPG(t, cfg)
	cfg.WatchNamespace = "two,one"
	issuer, token := catalogJWKS(t)
	cfg.Auth = &config.AuthConfig{Mode: config.AuthModeOAuth, InsecureAllowHTTP: true, OAuth: &config.OAuthConfig{ResourceURL: "https://catalog.example.org", Providers: []config.OAuthProviderConfig{{Name: "test", IssuerURL: issuer.URL, Audience: "catalog-test", JwksUrl: issuer.URL + "/jwks", AllowPrivateIP: true}}}}
	cfg.Admin.Enabled = true
	cfg.Authorization.AllowAnonymousCatalog = true
	// catalogTestConfig's policy intentionally grants only reads; use the explicit
	// authenticated fixture policy for this administrative acceptance test.
	require.NoError(t, os.WriteFile(cfg.Authorization.PolicyFile, []byte(`permit(principal,action,resource) when { principal.authenticated };`), 0600))
	_, err := conn.Exec(t.Context(), `INSERT INTO source(name,source_type,source_config,syncable,creation_type) VALUES('historical','kubernetes','{"namespaces":["one","two"]}',false,'API');
 INSERT INTO registry_entry(source_id,entry_type,name) SELECT id,'MCP','com.example/deleted-during-downtime' FROM source;
 INSERT INTO entry_version(entry_id,name,version,description) SELECT id,name,'1','stale export' FROM registry_entry;
 INSERT INTO mcp_server(version_id) SELECT id FROM entry_version`)
	require.NoError(t, err)
	approveCatalog(t, conn, cfg, false)
	scheme := runtime.NewScheme()
	require.NoError(t, mcp.AddToScheme(scheme))
	api := fake.NewClientBuilder().WithScheme(scheme).Build()
	app, err := NewCatalogApp(t.Context(), CatalogOptions{Config: cfg, Storage: storage.CatalogPostgres{BorrowedPool: pool}, Operator: &kubernetes.StandaloneFactory{API: api}, HTTP: []RegistryAppOptions{WithAddress("127.0.0.1:0"), WithInternalAddress("127.0.0.1:0")}, Reconcile: reconcile.Options{PollInterval: time.Hour, LeaseDuration: time.Minute}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = app.Stop() })
	require.NoError(t, app.Start())
	public, _ := app.Addresses()
	require.Eventually(t, func() bool { return app.runtime.Ready() }, 3*time.Second, 10*time.Millisecond)
	var count int
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*) FROM registry_entry WHERE name='com.example/deleted-during-downtime'`).Scan(&count))
	require.Zero(t, count)
	credential := token("operator")
	catalogRequest(t, public, "PUT", "/v1/sources/api-operator", `{"kubernetes":{}}`, credential, 201)
	defs, err := postgres.NewDefinitions(pool)
	require.NoError(t, err)
	stored, err := defs.GetSource(t.Context(), "api-operator")
	require.NoError(t, err)
	require.Equal(t, []string{"one", "two"}, stored.Kubernetes.Namespaces)
	jobs, err := postgres.NewJobs(pool, 65536)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		status, err := jobs.Status(t.Context(), "api-operator")
		return err == nil && status.BaselineValid
	}, 3*time.Second, 10*time.Millisecond)
	var acquired persistence.Job
	require.Eventually(t, func() bool {
		var acquireErr error
		acquired, acquireErr = jobs.Acquire(t.Context(), "api-operator", time.Minute)
		return acquireErr == nil
	}, 3*time.Second, 10*time.Millisecond)
	require.Equal(t, stored.Kubernetes.Namespaces, acquired.Source.Kubernetes.Namespaces)
	require.NoError(t, jobs.Fail(t.Context(), acquired, persistence.FailureCancelled))
	catalogRequest(t, public, "PUT", "/v1/sources/api-operator", `{"kubernetes":{"namespaces":["three"]}}`, credential, 200)
	require.Eventually(t, func() bool { return app.runtime.Ready() }, 3*time.Second, 10*time.Millisecond)
	stored, err = defs.GetSource(t.Context(), "api-operator")
	require.NoError(t, err)
	require.Equal(t, []string{"three"}, stored.Kubernetes.Namespaces)
	catalogRequest(t, public, "DELETE", "/v1/sources/api-operator", "", credential, 204)
	_, err = defs.GetSource(t.Context(), "api-operator")
	require.ErrorIs(t, err, persistence.ErrNotFound)
	require.NoError(t, app.Stop())
	require.NoError(t, pool.Ping(context.Background()))
}
