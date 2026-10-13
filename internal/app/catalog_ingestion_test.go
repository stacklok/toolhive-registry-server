package app

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	mcp "github.com/stacklok/toolhive/cmd/thv-operator/api/v1beta1"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/stacklok/toolhive-registry-server/database"
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
	_, err = conn.Exec(t.Context(), `INSERT INTO source(name,source_type,source_config,syncable,creation_type)
 VALUES('unlinked','file',jsonb_build_object('data',$1::text),false,'API')`, payload)
	require.NoError(t, err)
	source, err := defs.GetSource(t.Context(), "unlinked")
	require.NoError(t, err)
	jobs, err := postgres.NewJobs(pool, 65536)
	require.NoError(t, err)
	initial, err := ingest.New(ingest.Options{}).Fetch(t.Context(), source)
	require.NoError(t, err)
	oldWork := persistence.Job{Source: source}
	require.NoError(t, conn.QueryRow(t.Context(), `INSERT INTO registry_sync(source_id,sync_status,last_sync_hash,last_applied_filter_hash,applied_generation,
 lease_id,lease_generation,lease_expires_at) SELECT id,'IN_PROGRESS',$1,'',definition_generation,gen_random_uuid(),
 definition_generation,clock_timestamp()+interval '1 hour' FROM source WHERE name='unlinked'
 RETURNING lease_id::text,lease_generation,lease_expires_at`, initial.Hash).Scan(&oldWork.LeaseID, &oldWork.Generation, &oldWork.ExpiresAt))
	oldStatus, err := jobs.Status(t.Context(), source.Name)
	require.NoError(t, err)
	require.True(t, oldStatus.BaselineValid)
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

func TestCatalogOperatorPostgresHTTPOwnershipAndScopeChange(t *testing.T) {
	t.Parallel()
	cfg := catalogTestConfig(t)
	conn, pool := catalogPG(t, cfg)
	cfg.WatchNamespace = "two,one"
	cfg.Registries = nil
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
	approveCatalog(t, conn, cfg, false, true)
	scheme := runtime.NewScheme()
	require.NoError(t, mcp.AddToScheme(scheme))
	annotations := func(url string) map[string]string {
		return map[string]string{
			"toolhive.stacklok.dev/registry-export":      "true",
			"toolhive.stacklok.dev/registry-url":         url,
			"toolhive.stacklok.dev/registry-description": "exported",
		}
	}
	api := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		&mcp.MCPServer{ObjectMeta: metav1.ObjectMeta{Name: "one-server", Namespace: "one", Annotations: annotations("https://exports.example/one")}, Spec: mcp.MCPServerSpec{Image: "example.org/one:1.0.0"}},
		&mcp.VirtualMCPServer{ObjectMeta: metav1.ObjectMeta{Name: "two-virtual", Namespace: "two", Annotations: annotations("https://exports.example/two-virtual")}},
		&mcp.MCPRemoteProxy{ObjectMeta: metav1.ObjectMeta{Name: "two-proxy", Namespace: "two", Annotations: annotations("https://exports.example/two-proxy")}},
	).Build()
	require.NoError(t, FinalizeExposure(t.Context(), CatalogOptions{Config: cfg, Storage: storage.CatalogPostgres{BorrowedPool: pool}, Operator: &kubernetes.StandaloneFactory{API: api}}))
	require.NoError(t, database.MigrateUp(t.Context(), conn))
	app, err := NewCatalogApp(t.Context(), CatalogOptions{Config: cfg, Storage: storage.CatalogPostgres{BorrowedPool: pool}, Operator: &kubernetes.StandaloneFactory{API: api}, HTTP: []RegistryAppOptions{WithAddress("127.0.0.1:0"), WithInternalAddress("127.0.0.1:0")}, Reconcile: reconcile.Options{PollInterval: 10 * time.Millisecond, LeaseDuration: time.Minute}})
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
		status, statusErr := jobs.Status(t.Context(), "api-operator")
		return statusErr == nil && status.BaselineValid
	}, 3*time.Second, 10*time.Millisecond)
	catalogRequest(t, public, "PUT", "/v1/registries/public", `{"sources":["managed","api-operator"]}`, credential, 201)
	require.Eventually(t, func() bool {
		body, _ := catalogRequest(t, public, "GET", "/registry/public/v0.1/servers", "", credential, 200)
		return string(body) != "" &&
			strings.Contains(string(body), "com.toolhive.k8s.one/one-server") &&
			strings.Contains(string(body), "com.toolhive.k8s.two/two-virtual") &&
			strings.Contains(string(body), "com.toolhive.k8s.two/two-proxy")
	}, 3*time.Second, 10*time.Millisecond)

	// Managed publication/removal is source-local: the ordered operator source keeps
	// its discovered entries after the managed version is removed.
	catalogRequest(t, public, "POST", "/v1/entries", `{"server":{"$schema":"https://example.org/schema.json","name":"com.example/managed","description":"managed","version":"1"}}`, credential, 201)
	body, _ := catalogRequest(t, public, "GET", "/registry/public/v0.1/servers", "", credential, 200)
	require.Contains(t, string(body), "com.example/managed")
	require.Contains(t, string(body), "com.toolhive.k8s.two/two-proxy")
	// Reconfiguration fences the prior all-scope worker. Its replacement commits the
	// retained namespace before readiness returns; removed namespace exports disappear
	// while independently managed data survives the operator scope change.
	catalogRequest(t, public, "PUT", "/v1/sources/api-operator", `{"kubernetes":{"namespaces":["one"]}}`, credential, 200)
	require.Eventually(t, func() bool {
		current, _ := catalogRequest(t, public, "GET", "/registry/public/v0.1/servers", "", credential, 200)
		return strings.Contains(string(current), "com.example/managed") &&
			strings.Contains(string(current), "com.toolhive.k8s.one/one-server") &&
			!strings.Contains(string(current), "com.toolhive.k8s.two/two-virtual") &&
			!strings.Contains(string(current), "com.toolhive.k8s.two/two-proxy")
	}, 3*time.Second, 10*time.Millisecond)
	stored, err = defs.GetSource(t.Context(), "api-operator")
	require.NoError(t, err)
	require.Equal(t, []string{"one"}, stored.Kubernetes.Namespaces)
	require.Eventually(t, func() bool {
		status, statusErr := jobs.Status(t.Context(), "api-operator")
		return statusErr == nil && status.BaselineValid && app.runtime.Ready()
	}, 3*time.Second, 10*time.Millisecond)

	// Managed removal remains source-local after the operator's replacement commit.
	catalogRequest(t, public, "DELETE", "/v1/entries/server/com.example%2Fmanaged/versions/1", "", credential, 204)
	require.Eventually(t, func() bool {
		current, _ := catalogRequest(t, public, "GET", "/registry/public/v0.1/servers", "", credential, 200)
		return !strings.Contains(string(current), "com.example/managed") && strings.Contains(string(current), "com.toolhive.k8s.one/one-server")
	}, 3*time.Second, 10*time.Millisecond)
	catalogRequest(t, public, "DELETE", "/v1/registries/public", "", credential, 204)
	catalogRequest(t, public, "DELETE", "/v1/sources/api-operator", "", credential, 204)
	_, err = defs.GetSource(t.Context(), "api-operator")
	require.ErrorIs(t, err, persistence.ErrNotFound)
	require.NoError(t, app.Stop())
	require.NoError(t, pool.Ping(context.Background()))
}
