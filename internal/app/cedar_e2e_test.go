package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/database/dbtest"
	"github.com/stacklok/toolhive-registry-server/internal/auth"
	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/db"
	"github.com/stacklok/toolhive-registry-server/internal/sync/standalone"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/reconcile"
)

const (
	cedarTestIssuer = "https://idp.example.com"
	deniedBody      = `{"error":"authorization denied or unavailable"}` + "\n"

	// mainOnlyPolicy lets every caller read the "main" registry and nothing else.
	mainOnlyPolicy = `permit(principal, action == Registry::Action::"catalog.read", resource == Registry::View::"main");`

	// adminPolicy adds source administration for the "admin" subject.
	adminPolicy = mainOnlyPolicy + `
permit(
  principal,
  action in [Registry::Action::"sources.read", Registry::Action::"sources.write"],
  resource == Registry::Instance::"catalog"
) when { principal.authenticated && principal.subject == "admin" };`
)

// newCedarTestDB returns a fresh migrated database: a connection for
// assertions and a config for the app.
func newCedarTestDB(t *testing.T) (*pgx.Conn, *config.DatabaseConfig) {
	t.Helper()
	conn, cleanup := dbtest.SetupTestDB(t)
	t.Cleanup(cleanup)
	c := conn.Config()
	return conn, &config.DatabaseConfig{
		Host: c.Host, Port: int(c.Port), User: dbtest.DBUser, Password: dbtest.DBPass,
		Database: c.Database, SSLMode: "disable",
	}
}

func writePolicy(t *testing.T, policy string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "policy.cedar")
	require.NoError(t, os.WriteFile(path, []byte(policy), 0o600))
	return path
}

// cedarTestConfig is catalogTestConfig in Cedar mode with anonymous catalog
// access and a second registry, "secret", over the inline source.
func cedarTestConfig(t *testing.T, dbCfg *config.DatabaseConfig, policy string) *config.Config {
	t.Helper()
	cfg := catalogTestConfig(dbCfg)
	cfg.Registries = append(cfg.Registries, config.RegistryConfig{Name: "secret", Sources: []string{"inline"}})
	cfg.Authorization = &config.AuthorizationConfig{AllowAnonymousCatalog: true}
	if policy != "" {
		cfg.Authorization.PolicyFile = writePolicy(t, policy)
	}
	return cfg
}

// withBearerSubjectAuth injects authentication that accepts "Bearer <sub>"
// as a verified token for subject <sub>, and rejects anything else.
func withBearerSubjectAuth() RegistryAppOptions {
	return func(c *registryAppConfig) error {
		c.authMiddleware = func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				sub, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
				if !ok || sub == "" || sub == "invalid" {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				claims := jwt.MapClaims{"iss": cedarTestIssuer, "sub": sub}
				next.ServeHTTP(w, r.WithContext(auth.ContextWithClaims(r.Context(), claims)))
			})
		}
		return nil
	}
}

func withOperator(op catalogOperator) RegistryAppOptions {
	return func(c *registryAppConfig) error {
		c.operator = op
		return nil
	}
}

// rejectingOperator fails preflight for definitions scoped to "forbidden-ns"
// and records the namespaces of every definition it preflights.
type rejectingOperator struct {
	preflights atomic.Int32
}

var errForbiddenNamespace = errors.New("namespace not allowed")

func (o *rejectingOperator) Preflight(_ context.Context, d persistence.SourceDefinition) error {
	o.preflights.Add(1)
	if slices.Contains(d.Kubernetes.Namespaces, "forbidden-ns") {
		return errForbiddenNamespace
	}
	return nil
}

func (*rejectingOperator) Worker(persistence.Jobs, persistence.SourceDefinition, reconcile.Options) (standalone.Worker, error) {
	return &idleWorker{}, nil
}

func serveApp(app *RegistryApp, method, path, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	app.GetHTTPServer().Handler.ServeHTTP(rec, req)
	return rec
}

func recordedMode(t *testing.T, conn *pgx.Conn) string {
	t.Helper()
	var mode string
	require.NoError(t, conn.QueryRow(context.Background(), `SELECT mode FROM authorization_mode`).Scan(&mode))
	return mode
}

func TestNewRegistryApp_CedarModeAnonymousCatalog(t *testing.T) {
	t.Parallel()
	conn, dbCfg := newCedarTestDB(t)
	cfg := cedarTestConfig(t, dbCfg, mainOnlyPolicy)
	auditLog := filepath.Join(t.TempDir(), "audit.log")
	cfg.Audit = &config.AuditConfig{Enabled: true, LogFile: auditLog}
	require.NoError(t, cfg.ValidateAuthorizationMode(), "the test config is a valid Cedar config")

	app := newTestRegistryApp(t, WithConfig(cfg), WithReconcileOptions(fastReconcile))
	require.NotNil(t, app.components)
	require.NotNil(t, app.components.Sync)
	require.NotNil(t, app.components.RegistryService)
	assert.Equal(t, "cedar", recordedMode(t, conn))

	runSync(t, app.components.Sync)
	require.Eventually(t, func() bool {
		rec := serveApp(app, http.MethodGet, "/registry/main/v0.1/servers", "", "")
		return rec.Code == http.StatusOK && strings.Contains(rec.Body.String(), "io.example/weather")
	}, 15*time.Second, 50*time.Millisecond, "the permitted registry never served the inline server")

	rec := serveApp(app, http.MethodGet, "/registry/secret/v0.1/servers", "", "")
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Equal(t, deniedBody, rec.Body.String(), "a denial never reveals the registry's content")

	rec = serveApp(app, http.MethodGet, "/v1/sources", "", "")
	assert.Equal(t, http.StatusNotFound, rec.Code, "admin is disabled, so /v1 is not mounted")
	rec = serveApp(app, http.MethodGet, "/openapi.json", "", "")
	assert.Equal(t, http.StatusForbidden, rec.Code, "the policy grants no metadata.read")

	// The internal server is unaffected by the mode.
	rec = httptest.NewRecorder()
	app.GetInternalHTTPServer().Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	assert.Equal(t, http.StatusOK, rec.Code)

	app.cancelFunc() // closes the audit log
	events := readJSONLines(t, auditLog)
	var admissions, deniedReads int
	for _, e := range events {
		extra, _ := e["metadata"].(map[string]any)
		switch e["type"] {
		case "authorization.admission":
			admissions++
			fields, _ := extra["extra"].(map[string]any)
			assert.Contains(t, fields["policy_revision"], "sha256:")
		case "server.list":
			if target, _ := e["target"].(map[string]any); target["registry_name"] == "secret" {
				deniedReads++
				assert.Equal(t, "denied", e["outcome"])
			}
		}
	}
	assert.Positive(t, admissions, "Cedar decisions are audited")
	assert.Equal(t, 1, deniedReads, "the denied consumer read is audited with its own event type")
}

func TestNewRegistryApp_CedarModeAdmin(t *testing.T) {
	t.Parallel()
	_, dbCfg := newCedarTestDB(t)
	cfg := cedarTestConfig(t, dbCfg, adminPolicy)
	cfg.Auth = &config.AuthConfig{Mode: config.AuthModeOAuth, OAuth: &config.OAuthConfig{
		Realm: "corp", ResourceURL: "https://registry.example.com"}}
	cfg.Admin = &config.AdminConfig{Enabled: true}
	cfg.WatchNamespace = "ns-b,ns-a"
	require.NoError(t, cfg.ValidateAuthorizationMode(), "the test config is a valid Cedar config")
	operator := &rejectingOperator{}

	app := newTestRegistryApp(t, WithConfig(cfg), WithReconcileOptions(fastReconcile),
		withBearerSubjectAuth(), withOperator(operator))
	// Admin writes refresh the sync runtime, which must be running.
	runSync(t, app.components.Sync)

	// Anonymous catalog read is policy-checked.
	{
		assert.Equal(t, http.StatusOK, serveApp(app, http.MethodGet, "/registry/main/v0.1/servers", "", "").Code)
		assert.Equal(t, http.StatusForbidden, serveApp(app, http.MethodGet, "/registry/secret/v0.1/servers", "", "").Code)
	}

	// A bad token never falls back to anonymous.
	{
		rec := serveApp(app, http.MethodGet, "/registry/main/v0.1/servers", "invalid", "")
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	}

	// Anonymous admin call gets the OAuth challenge.
	{
		rec := serveApp(app, http.MethodGet, "/v1/sources", "", "")
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
		assert.Equal(t,
			`Bearer realm="corp", resource_metadata="https://registry.example.com/.well-known/oauth-protected-resource"`,
			rec.Header().Get("WWW-Authenticate"))
	}

	// Policy decides admin access.
	{
		assert.Equal(t, http.StatusForbidden, serveApp(app, http.MethodGet, "/v1/sources", "alice", "").Code)
		assert.Equal(t, http.StatusOK, serveApp(app, http.MethodGet, "/v1/sources", "admin", "").Code)
		assert.Equal(t, http.StatusForbidden, serveApp(app, http.MethodGet, "/v1/registries", "admin", "").Code,
			"views.read is not granted")
	}

	// Claims-mode endpoints do not exist.
	{
		assert.Equal(t, http.StatusNotFound, serveApp(app, http.MethodGet, "/v1/me", "admin", "").Code)
		assert.Equal(t, http.StatusNotFound,
			serveApp(app, http.MethodGet, "/v1/entries/server/io.example%2Fweather/claims", "admin", "").Code)
	}

	// Kubernetes sources are scoped and preflighted.
	{
		before := operator.preflights.Load()
		rec := serveApp(app, http.MethodPut, "/v1/sources/k8s-api", "admin", `{"kubernetes":{"Namespaces":[]}}`)
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		assert.Equal(t, []any{"ns-a", "ns-b"}, sourceNamespaces(t, rec), "the watch scope is persisted")
		assert.Greater(t, operator.preflights.Load(), before, "create runs the operator preflight")

		before = operator.preflights.Load()
		rec = serveApp(app, http.MethodPut, "/v1/sources/k8s-api", "admin", `{"kubernetes":{"Namespaces":["team-a"]}}`)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.Equal(t, []any{"team-a"}, sourceNamespaces(t, rec))
		assert.Greater(t, operator.preflights.Load(), before, "update runs the operator preflight")
	}

	// Scope and preflight errors reject the write.
	{
		rec := serveApp(app, http.MethodPut, "/v1/sources/k8s-empty", "admin", `{"kubernetes":{"Namespaces":[" "]}}`)
		assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		rec = serveApp(app, http.MethodPut, "/v1/sources/k8s-bad", "admin", `{"kubernetes":{"Namespaces":["forbidden-ns"]}}`)
		assert.GreaterOrEqual(t, rec.Code, http.StatusBadRequest, rec.Body.String())
		assert.NotContains(t, rec.Body.String(), errForbiddenNamespace.Error(), "the operator's error text is not leaked")

		for _, name := range []string{"k8s-empty", "k8s-bad"} {
			rec = serveApp(app, http.MethodGet, "/v1/sources/"+name, "admin", "")
			assert.Equal(t, http.StatusNotFound, rec.Code, "%s was not created", name)
		}
	}
}

func sourceNamespaces(t *testing.T, rec *httptest.ResponseRecorder) []any {
	t.Helper()
	var body struct {
		Type         string         `json:"type"`
		SourceConfig map[string]any `json:"sourceConfig"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body), rec.Body.String())
	require.Equal(t, "kubernetes", body.Type)
	namespaces, _ := body.SourceConfig["Namespaces"].([]any)
	return namespaces
}

func TestNewRegistryApp_CedarPolicyLoadFailure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		policy func(t *testing.T) string
	}{
		{name: "syntax error", policy: func(t *testing.T) string {
			t.Helper()
			return writePolicy(t, "permit(")
		}},
		{name: "schema violation", policy: func(t *testing.T) string {
			t.Helper()
			return writePolicy(t, `permit(principal, action == Registry::Action::"no.such.action", resource);`)
		}},
		{name: "empty policy", policy: func(t *testing.T) string {
			t.Helper()
			return writePolicy(t, "// nothing\n")
		}},
		{name: "missing file", policy: func(t *testing.T) string {
			t.Helper()
			return filepath.Join(t.TempDir(), "absent.cedar")
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			conn, dbCfg := newCedarTestDB(t)
			cfg := cedarTestConfig(t, dbCfg, "")
			cfg.Authorization.PolicyFile = tt.policy(t)

			app, err := NewRegistryApp(context.Background(), WithConfig(cfg), WithReconcileOptions(fastReconcile))
			require.Error(t, err)
			assert.Nil(t, app)
			assert.Contains(t, err.Error(), "failed to load authorization policy")

			// The policy loads before the mode is recorded, so a failed first
			// start leaves a fresh database free to start in either mode.
			var recorded int
			require.NoError(t, conn.QueryRow(context.Background(),
				`SELECT count(*) FROM authorization_mode`).Scan(&recorded))
			assert.Zero(t, recorded, "a broken policy must not tie the database to Cedar mode")
		})
	}
}

func TestNewRegistryApp_AuthorizationModeIsFixed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		first, then  config.AuthorizationMode
		wantMismatch bool
	}{
		{name: "claims database refuses Cedar", first: config.AuthorizationModeClaims, then: config.AuthorizationModeCedar,
			wantMismatch: true},
		{name: "Cedar database refuses claims", first: config.AuthorizationModeCedar, then: config.AuthorizationModeClaims,
			wantMismatch: true},
		{name: "Cedar database restarts in Cedar", first: config.AuthorizationModeCedar, then: config.AuthorizationModeCedar},
		{name: "claims database restarts in claims", first: config.AuthorizationModeClaims, then: config.AuthorizationModeClaims},
	}

	configFor := func(t *testing.T, dbCfg *config.DatabaseConfig, mode config.AuthorizationMode) *config.Config {
		t.Helper()
		if mode == config.AuthorizationModeCedar {
			return cedarTestConfig(t, dbCfg, mainOnlyPolicy)
		}
		return catalogTestConfig(dbCfg)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			conn, dbCfg := newCedarTestDB(t)
			newTestRegistryApp(t, WithConfig(configFor(t, dbCfg, tt.first)), WithReconcileOptions(fastReconcile))
			assert.Equal(t, string(tt.first), recordedMode(t, conn))

			app, err := NewRegistryApp(context.Background(),
				WithConfig(configFor(t, dbCfg, tt.then)), WithReconcileOptions(fastReconcile))
			if tt.wantMismatch {
				require.ErrorIs(t, err, db.ErrAuthorizationModeMismatch)
				assert.Nil(t, app)
			} else {
				require.NoError(t, err)
				app.cancelFunc()
			}
			assert.Equal(t, string(tt.first), recordedMode(t, conn), "the recorded mode never changes")
		})
	}
}

// TestNewRegistryApp_CedarModeStoresNoClaims gives both modes config claims
// (which Config validation would reject in Cedar mode) to show that Cedar mode
// neither applies config claims nor installs the entry claims hook.
func TestNewRegistryApp_CedarModeStoresNoClaims(t *testing.T) {
	t.Parallel()

	for _, mode := range []config.AuthorizationMode{config.AuthorizationModeClaims, config.AuthorizationModeCedar} {
		t.Run(string(mode), func(t *testing.T) {
			t.Parallel()
			conn, dbCfg := newCedarTestDB(t)
			cfg := catalogTestConfig(dbCfg)
			if mode == config.AuthorizationModeCedar {
				cfg = cedarTestConfig(t, dbCfg, mainOnlyPolicy)
			}
			cfg.Sources[0].Claims = map[string]any{"org": "acme"}
			cfg.Registries[0].Claims = map[string]any{"org": "acme"}

			app := newTestRegistryApp(t, WithConfig(cfg), WithReconcileOptions(fastReconcile))
			runSync(t, app.components.Sync)

			ctx := context.Background()
			var entries, withClaims int
			require.Eventually(t, func() bool {
				err := conn.QueryRow(ctx, `SELECT count(*), count(claims) FROM registry_entry`).Scan(&entries, &withClaims)
				return err == nil && entries > 0
			}, 15*time.Second, 50*time.Millisecond, "the inline source never synced")

			var sourceClaims, registryClaims *string
			require.NoError(t, conn.QueryRow(ctx, `SELECT claims::text FROM source WHERE name = 'inline'`).Scan(&sourceClaims))
			require.NoError(t, conn.QueryRow(ctx, `SELECT claims::text FROM registry WHERE name = 'main'`).Scan(&registryClaims))

			if mode == config.AuthorizationModeClaims {
				assert.Equal(t, entries, withClaims, "claims mode stamps synced entries with the source's claims")
				require.NotNil(t, sourceClaims)
				require.NotNil(t, registryClaims)
				return
			}
			assert.Zero(t, withClaims, "Cedar mode never writes registry_entry.claims")
			assert.Nil(t, sourceClaims, "Cedar mode skips the config claims step for sources")
			assert.Nil(t, registryClaims, "Cedar mode skips the config claims step for registries")
		})
	}
}
