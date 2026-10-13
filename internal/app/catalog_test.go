package app

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/database"
	operatormocks "github.com/stacklok/toolhive-registry-server/internal/app/mocks"
	"github.com/stacklok/toolhive-registry-server/internal/app/storage"
	storagemocks "github.com/stacklok/toolhive-registry-server/internal/app/storage/mocks"
	"github.com/stacklok/toolhive-registry-server/internal/authorization"
	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/db"
	"github.com/stacklok/toolhive-registry-server/internal/kubernetes"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/ingest"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/reconcile"
	fetchmocks "github.com/stacklok/toolhive-registry-server/pkg/registry/reconcile/mocks"
)

func catalogTestConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg := createValidTestConfig()
	cfg.Sources = []config.SourceConfig{{Name: "managed", Managed: &config.ManagedConfig{}}}
	cfg.Registries = []config.RegistryConfig{{Name: "public", Sources: []string{"managed"}}}
	cfg.Database = &config.DatabaseConfig{Host: "localhost", Port: 5432, User: "test", Database: "test", SSLMode: "disable"}
	cfg.Authorization.PolicyFile = filepath.Join(t.TempDir(), "policy.cedar")
	require.NoError(t, os.WriteFile(cfg.Authorization.PolicyFile, []byte(`permit(principal, action == Registry::Action::"catalog.read", resource);`), 0600))
	return cfg
}
func catalogRevision(t *testing.T, cfg *config.Config) string {
	t.Helper()
	engine, err := authorization.Load(cfg.Authorization.PolicyFile)
	require.NoError(t, err)
	review, err := cfg.ExposureReview(engine.Revision())
	require.NoError(t, err)
	return review.Revision
}

func TestCatalogPreparationMandatoryPhases(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{"invalid-config", "invalid-policy", "nil-open", "preflight", "operator", "consume", "initialize"} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			cfg := catalogTestConfig(t)
			ctrl := gomock.NewController(t)
			factory := storagemocks.NewMockCatalogFactory(ctrl)
			store := storagemocks.NewMockCatalogStorage(ctrl)
			operator := operatormocks.NewMockCatalogOperator(ctrl)
			fail := errors.New("preparation blocked")
			switch stage {
			case "invalid-config":
				cfg.Sources[0].Claims = map[string]any{}
			case "invalid-policy":
				require.NoError(t, os.WriteFile(cfg.Authorization.PolicyFile, []byte("// no policy"), 0600))
			case "nil-open":
				factory.EXPECT().Open(gomock.Any(), gomock.Any()).Return(nil, nil)
			default:
				factory.EXPECT().Open(gomock.Any(), gomock.Any()).Return(store, nil)
				store.EXPECT().Close()
				first := store.EXPECT().Preflight(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any())
				if stage == "preflight" {
					first.Return(nil, fail)
					break
				}
				historical := []persistence.SourceDefinition(nil)
				if stage == "operator" {
					historical = []persistence.SourceDefinition{{ID: "old", Name: "unlinked-api", Origin: persistence.OriginAPI, Kubernetes: &persistence.KubernetesSpec{Namespaces: []string{"all"}}}}
				}
				first.Return(historical, nil)
				if stage == "operator" {
					operator.EXPECT().Preflight(gomock.Any(), historical[0]).After(first).Return(kubernetes.ErrRetiredExportClaims)
					break
				}
				consume := store.EXPECT().Consume(gomock.Any(), catalogRevision(t, cfg)).After(first)
				if stage == "consume" {
					consume.Return(fail)
					break
				}
				consume.Return(nil)
				store.EXPECT().Initialize(gomock.Any(), gomock.Any(), gomock.Any()).After(consume).Return(storage.CatalogComponents{}, fail)
			}
			app, err := NewCatalogApp(t.Context(), CatalogOptions{Config: cfg, Storage: factory, Operator: operator, HTTP: []RegistryAppOptions{WithAddress("127.0.0.1:0"), WithInternalAddress("127.0.0.1:0")}})
			require.Error(t, err)
			require.Nil(t, app)
		})
	}
}

func TestCatalogFrozenPolicyAndFinalOverrides(t *testing.T) {
	t.Parallel()
	cfg := catalogTestConfig(t)
	cfg.Authorization.AllowAnonymousCatalog = false
	cfg.Auth.Mode = config.AuthModeOAuth
	// Final overrides are applied before validation and fingerprinting.
	final := catalogTestConfig(t)
	cfg.Authorization = final.Authorization
	override := func(c *config.Config) error { c.Auth = &config.AuthConfig{Mode: config.AuthModeAnonymous}; return nil }
	expected := *cfg
	require.NoError(t, override(&expected))
	revision := catalogRevision(t, &expected)
	ctrl := gomock.NewController(t)
	factory := storagemocks.NewMockCatalogFactory(ctrl)
	store := storagemocks.NewMockCatalogStorage(ctrl)
	factory.EXPECT().Open(gomock.Any(), gomock.Any()).DoAndReturn(func(context.Context, *config.Config) (storage.CatalogStorage, error) {
		require.NoError(t, os.WriteFile(cfg.Authorization.PolicyFile, []byte(`forbid(principal,action,resource);`), 0600))
		return store, nil
	})
	store.EXPECT().Preflight(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil)
	store.EXPECT().Consume(gomock.Any(), revision).Return(errors.New("stop after frozen binding"))
	store.EXPECT().Close()
	_, err := NewCatalogApp(t.Context(), CatalogOptions{Config: cfg, Overrides: []func(*config.Config) error{override}, Storage: factory})
	require.ErrorContains(t, err, "frozen binding")
	require.Equal(t, config.AuthModeOAuth, cfg.Auth.Mode, "caller config must not be modified")
}

func catalogPG(t *testing.T, cfg *config.Config) (*pgx.Conn, *pgxpool.Pool) {
	t.Helper()
	conn, _ := database.SetupTestDB(t)
	cc := conn.Config()
	cfg.Database = &config.DatabaseConfig{Host: cc.Host, Port: int(cc.Port), User: cc.User, Password: cc.Password, Database: cc.Database, SSLMode: "disable"}
	pool, err := pgxpool.New(t.Context(), cc.ConnString())
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return conn, pool
}
func approveCatalog(t *testing.T, conn *pgx.Conn, cfg *config.Config, empty bool) {
	t.Helper()
	revision := catalogRevision(t, cfg)
	inventory, err := db.InventoryExposure(t.Context(), conn, revision)
	require.NoError(t, err)
	ack, err := db.AcknowledgeExposure(t.Context(), conn, revision, inventory.Digest, !empty, true, empty)
	require.NoError(t, err)
	if !empty {
		_, err = db.RetireExposureClaims(t.Context(), conn, revision, ack.ID, ack.PostFenceDigest, true, true)
		require.NoError(t, err)
	}
}
func constructCatalog(t *testing.T, cfg *config.Config, pool *pgxpool.Pool, fetcher reconcile.Fetcher) *CatalogApp {
	t.Helper()
	app, err := NewCatalogApp(t.Context(), CatalogOptions{Config: cfg, Storage: storage.CatalogPostgres{BorrowedPool: pool}, Fetcher: fetcher,
		HTTP: []RegistryAppOptions{WithAddress("127.0.0.1:0"), WithInternalAddress("127.0.0.1:0")}, Reconcile: reconcile.Options{PollInterval: 10 * time.Millisecond, LeaseDuration: time.Minute}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = app.Stop() })
	return app
}
func TestCatalogMissingAndStaleReceiptsLeaveDatabaseUntouched(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"missing-empty", "missing-unlabeled", "pending-no-retirement", "stale-config", "stale-policy", "stale-catalog"} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			cfg := catalogTestConfig(t)
			conn, pool := catalogPG(t, cfg)
			if state != "missing-empty" {
				_, err := conn.Exec(t.Context(), `INSERT INTO source(name,source_type,source_config,syncable,creation_type) VALUES('history','managed','{}',false,'API')`)
				require.NoError(t, err)
				cfg.Sources = nil
				cfg.Registries = nil
			}
			if state == "pending-no-retirement" {
				rev := catalogRevision(t, cfg)
				inventory, err := db.InventoryExposure(t.Context(), conn, rev)
				require.NoError(t, err)
				_, err = db.AcknowledgeExposure(t.Context(), conn, rev, inventory.Digest, true, true, false)
				require.NoError(t, err)
			} else if strings.HasPrefix(state, "stale-") {
				approveCatalog(t, conn, cfg, false)
			}
			switch state {
			case "stale-config":
				cfg.Authorization.AllowAnonymousCatalog = false
				cfg.Auth.Mode = config.AuthModeOAuth
				cfg.Auth.OAuth = &config.OAuthConfig{Providers: []config.OAuthProviderConfig{{Name: "id", IssuerURL: "https://example.org", Audience: "test"}}}
			case "stale-policy":
				require.NoError(t, os.WriteFile(cfg.Authorization.PolicyFile, []byte(`forbid(principal,action,resource);`), 0600))
			case "stale-catalog":
				_, err := conn.Exec(t.Context(), `UPDATE source SET updated_at=clock_timestamp()`)
				require.NoError(t, err)
			}
			before, err := db.InventoryExposure(t.Context(), conn, catalogRevision(t, cfg))
			require.NoError(t, err)
			_, err = NewCatalogApp(t.Context(), CatalogOptions{Config: cfg, Storage: storage.CatalogPostgres{BorrowedPool: pool}})
			require.Error(t, err)
			after, err := db.InventoryExposure(t.Context(), conn, catalogRevision(t, cfg))
			require.NoError(t, err)
			require.Equal(t, before.Digest, after.Digest)
			require.NoError(t, pool.Ping(t.Context()))
		})
	}
}

func catalogJWKS(t *testing.T) (*httptest.Server, func(string) string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	var issuer string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/.well-known/openid-configuration" {
			_ = json.NewEncoder(w).Encode(map[string]any{"issuer": issuer, "jwks_uri": issuer + "/jwks"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]any{{"kty": "RSA", "use": "sig", "alg": "RS256", "kid": "test", "n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes())}}})
	}))
	issuer = server.URL
	t.Cleanup(server.Close)
	return server, func(subject string) string {
		token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{"iss": issuer, "sub": subject, "name": subject + "-display", "preferred_username": subject + "-username", "email": subject + "@private.invalid", "aud": "catalog-test", "exp": time.Now().Add(time.Hour).Unix(), "team": subject})
		token.Header["kid"] = "test"
		value, err := token.SignedString(key)
		require.NoError(t, err)
		return value
	}
}
func catalogRequest(t *testing.T, address, method, path, body, token string, status int) ([]byte, http.Header) {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), method, "http://"+address+path, strings.NewReader(body))
	require.NoError(t, err)
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Equalf(t, status, response.StatusCode, "%s %s: %s", method, path, data)
	return data, response.Header
}
func TestCatalogCompleteHTTPPostgresAndConsumedRestart(t *testing.T) {
	t.Parallel()
	cfg := catalogTestConfig(t)
	conn, pool := catalogPG(t, cfg)
	issuer, token := catalogJWKS(t)
	cfg.Auth = &config.AuthConfig{Mode: config.AuthModeOAuth, InsecureAllowHTTP: true, OAuth: &config.OAuthConfig{ResourceURL: "https://catalog.example.org", Providers: []config.OAuthProviderConfig{{Name: "test", IssuerURL: issuer.URL, Audience: "catalog-test", JwksUrl: issuer.URL + "/jwks", AllowPrivateIP: true}}}}
	cfg.Authorization.AllowAnonymousCatalog = false
	cfg.Admin.Enabled = true
	cfg.Audit = &config.AuditConfig{Enabled: true, LogFile: filepath.Join(t.TempDir(), "audit.json")}
	require.NoError(t, os.WriteFile(cfg.Authorization.PolicyFile, []byte(`permit(principal,action,resource) when { principal.authenticated };`), 0600))
	approveCatalog(t, conn, cfg, true)
	app := constructCatalog(t, cfg, pool, nil)
	require.False(t, app.runtime.Ready())
	require.ErrorIs(t, app.SyncInline(t.Context(), "x"), persistence.ErrBusy)
	require.NoError(t, app.Start())
	public, internal := app.Addresses()
	require.Eventually(t, func() bool { return app.runtime.Ready() }, 5*time.Second, 10*time.Millisecond)
	a, b := token("finance"), token("engineering")
	for _, path := range []string{"/registry/public/v0.1/servers", "/v1/sources", "/openapi.json"} {
		_, headers := catalogRequest(t, public, "GET", path, "", "", 401)
		require.Contains(t, headers.Get("WWW-Authenticate"), "Bearer")
		require.Contains(t, headers.Get("WWW-Authenticate"), "resource_metadata=")
	}
	for _, path := range []string{"/health", "/readiness", "/version", "/metrics"} {
		catalogRequest(t, public, "GET", path, "", a, 404)
	}
	catalogRequest(t, internal, "GET", "/health", "", "", 200)
	catalogRequest(t, internal, "GET", "/readiness", "", "", 200)
	catalogRequest(t, public, "GET", "/.well-known/oauth-protected-resource", "", "", 200)
	for _, version := range []string{"1", "2"} {
		catalogRequest(t, public, "POST", "/v1/entries", `{"server":{"$schema":"https://example.org/schema.json","name":"com.example/item","description":"shared","version":"`+version+`"}}`, a, 201)
		catalogRequest(t, public, "POST", "/v1/entries", `{"skill":{"namespace":"com.example","name":"item","description":"shared","version":"`+version+`","metadata":{"claims":null}}}`, a, 201)
		catalogRequest(t, public, "POST", "/v1/entries", `{"plugin":{"namespace":"com.example","name":"item","description":"shared","version":"`+version+`","_meta":{"claims":{"team":"metadata"}}}}`, a, 201)
	}
	routes := []string{"/servers", "/servers/com.example%2Fitem/versions", "/servers/com.example%2Fitem/versions/1",
		"/x/dev.toolhive/skills", "/x/dev.toolhive/skills/", "/x/dev.toolhive/skills/com.example/item", "/x/dev.toolhive/skills/com.example/item/versions", "/x/dev.toolhive/skills/com.example/item/versions/1",
		"/x/dev.toolhive/plugins", "/x/dev.toolhive/plugins/", "/x/dev.toolhive/plugins/com.example/item", "/x/dev.toolhive/plugins/com.example/item/versions", "/x/dev.toolhive/plugins/com.example/item/versions/1"}
	for _, suffix := range routes {
		first, _ := catalogRequest(t, public, "GET", "/registry/public/v0.1"+suffix, "", a, 200)
		second, _ := catalogRequest(t, public, "GET", "/registry/public/v0.1"+suffix, "", b, 200)
		require.Equal(t, string(first), string(second))
	}
	first, _ := catalogRequest(t, public, "GET", "/registry/public/v0.1/servers?limit=1", "", a, 200)
	var page struct {
		Metadata struct {
			NextCursor string `json:"nextCursor"`
		} `json:"metadata"`
	}
	require.NoError(t, json.Unmarshal(first, &page))
	require.NotEmpty(t, page.Metadata.NextCursor)
	cursorPath := "/registry/public/v0.1/servers?limit=1&cursor=" + url.QueryEscape(page.Metadata.NextCursor)
	nextA, _ := catalogRequest(t, public, "GET", cursorPath, "", a, 200)
	nextB, _ := catalogRequest(t, public, "GET", cursorPath, "", b, 200)
	require.Equal(t, string(nextA), string(nextB))
	for _, path := range []string{"/v1/sources", "/v1/sources/managed", "/v1/sources/managed/entries", "/v1/registries", "/v1/registries/public", "/v1/registries/public/entries"} {
		catalogRequest(t, public, "GET", path, "", a, 200)
	}
	catalogRequest(t, public, "PUT", "/v1/sources/new", `{"file":{"path":"/not-used"},"syncPolicy":{"interval":"24h"}}`, a, 201)
	catalogRequest(t, public, "PUT", "/v1/sources/new", `{"file":{"path":"/not-used"},"syncPolicy":{"interval":"24h"}}`, a, 200)
	catalogRequest(t, public, "PUT", "/v1/registries/new", `{"sources":["managed"]}`, a, 201)
	catalogRequest(t, public, "PUT", "/v1/registries/new", `{"sources":["managed"]}`, a, 200)
	catalogRequest(t, public, "DELETE", "/v1/registries/new", "", a, 204)
	catalogRequest(t, public, "DELETE", "/v1/sources/new", "", a, 204)
	catalogRequest(t, public, "DELETE", "/v1/entries/server/com.example%2Fitem/versions/1", "", a, 204)
	for _, path := range []string{"/v1/me", "/v1/entries/server/com.example%2Fitem/claims"} {
		catalogRequest(t, public, "GET", path, "", "", 401)
		catalogRequest(t, public, "GET", path, "", a, 410)
	}
	catalogRequest(t, public, "PUT", "/v1/entries/server/com.example%2Fitem/claims", `{}`, a, 410)
	for _, body := range []string{`{"claims":null,"sources":["managed"]}`, `{"CLAIMS":{},"sources":["managed"]}`} {
		catalogRequest(t, public, "PUT", "/v1/registries/new", body, a, 400)
	}
	require.NoError(t, app.Stop())
	require.NoError(t, app.Wait())
	require.NoError(t, pool.Ping(t.Context()))
	// Historical config/policy/catalog digests are not perpetual restart bindings.
	require.NoError(t, os.WriteFile(cfg.Authorization.PolicyFile, []byte(`permit(principal,action,resource) when { principal.authenticated }; // edited`), 0600))
	cfg.Registries[0].Name = "changed"
	restart := constructCatalog(t, cfg, pool, nil)
	require.NoError(t, restart.Start())
	restarted, _ := restart.Addresses()
	catalogRequest(t, restarted, "GET", "/registry/changed/v0.1/servers", "", a, 200)
	require.NoError(t, restart.Stop())
	_, err := conn.Exec(t.Context(), `UPDATE exposure_acknowledgement SET consumed_target_digest='sha256:'||repeat('f',64)`)
	require.NoError(t, err)
	_, err = NewCatalogApp(t.Context(), CatalogOptions{Config: cfg, Storage: storage.CatalogPostgres{BorrowedPool: pool}})
	require.ErrorContains(t, err, "target mismatch")
}

func TestCatalogShutdownJoinsCallerOwnedInlineBeforeCleanup(t *testing.T) {
	t.Parallel()
	cfg := catalogTestConfig(t)
	conn, pool := catalogPG(t, cfg)
	approveCatalog(t, conn, cfg, true)
	ctrl := gomock.NewController(t)
	fetcher := fetchmocks.NewMockFetcher(ctrl)
	app, err := NewCatalogApp(t.Context(), CatalogOptions{Config: cfg, Storage: storage.CatalogPostgres{BorrowedPool: pool}, Fetcher: fetcher, HTTP: []RegistryAppOptions{WithAddress("127.0.0.1:0"), WithInternalAddress("127.0.0.1:0")}, Reconcile: reconcile.Options{PollInterval: time.Hour, LeaseDuration: time.Minute}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = app.Stop() })
	require.NoError(t, app.Start())
	require.Eventually(t, func() bool { return app.runtime.Ready() }, time.Second, 5*time.Millisecond)
	_, err = pool.Exec(t.Context(), `INSERT INTO source(name,source_type,source_config,syncable,creation_type) VALUES('inline','file','{"data":"persisted"}',false,'API')`)
	require.NoError(t, err)
	started := make(chan struct{})
	cancelled := make(chan struct{})
	release := make(chan struct{})
	fetcher.EXPECT().Fetch(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, _ persistence.SourceDefinition) (ingest.Result, error) {
		close(started)
		<-ctx.Done()
		close(cancelled)
		<-release
		require.NoError(t, pool.Ping(context.Background()))
		return ingest.Result{}, ctx.Err()
	})
	inlineDone := make(chan error, 1)
	go func() { inlineDone <- app.SyncInline(context.Background(), "inline") }()
	<-started
	stopped := make(chan error, 1)
	go func() { stopped <- app.Stop() }()
	<-cancelled
	select {
	case <-stopped:
		t.Fatal("shutdown returned before inline joined")
	default:
	}
	close(release)
	require.Error(t, <-inlineDone)
	require.NoError(t, <-stopped)
	require.NoError(t, pool.Ping(t.Context()))
}
