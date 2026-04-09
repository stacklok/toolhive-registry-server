//go:build integration

package authz_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/database"
	registryapp "github.com/stacklok/toolhive-registry-server/internal/app"
	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/sync/coordinator"
)

// ---------------------------------------------------------------------------
// Mock OIDC server
// ---------------------------------------------------------------------------

type mockOIDCServer struct {
	*httptest.Server
	privateKey *rsa.PrivateKey
	keyID      string
	issuerURL  string
}

func newMockOIDCServer(t *testing.T) *mockOIDCServer {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	s := &mockOIDCServer{
		privateKey: privateKey,
		keyID:      "test-key-1",
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                 s.issuerURL,
			"jwks_uri":               s.issuerURL + "/.well-known/jwks.json",
			"authorization_endpoint": s.issuerURL + "/authorize",
			"token_endpoint":         s.issuerURL + "/token",
		})
	})
	mux.HandleFunc("/.well-known/jwks.json", func(w http.ResponseWriter, _ *http.Request) {
		pub := &s.privateKey.PublicKey
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]any{{
				"kty": "RSA",
				"use": "sig",
				"alg": "RS256",
				"kid": s.keyID,
				"n":   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
				"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
			}},
		})
	})

	s.Server = httptest.NewServer(mux)
	s.issuerURL = s.URL
	return s
}

func (s *mockOIDCServer) token(t *testing.T, extraClaims map[string]any) string {
	t.Helper()
	claims := jwt.MapClaims{
		"iss": s.issuerURL,
		"sub": "test-user",
		"aud": "test-audience",
		"exp": time.Now().Add(time.Hour).Unix(),
		"iat": time.Now().Unix(),
	}
	for k, v := range extraClaims {
		claims[k] = v
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = s.keyID
	signed, err := tok.SignedString(s.privateKey)
	require.NoError(t, err)
	return signed
}

// ---------------------------------------------------------------------------
// Config builders
// ---------------------------------------------------------------------------

func baseSourceConfigs(t *testing.T) []config.SourceConfig {
	t.Helper()

	writeFixture := func(name, data string) string {
		f, err := os.CreateTemp(t.TempDir(), name+"-*.json")
		require.NoError(t, err)
		_, err = f.WriteString(data)
		require.NoError(t, err)
		require.NoError(t, f.Close())
		return f.Name()
	}

	sharedData := `{"version":"1.0.0","meta":{"last_updated":"2025-01-01T00:00:00Z"},"data":{"servers":[` +
		`{"name":"io.test/shared-tool-1","version":"1.0.0","description":"shared tool 1","packages":[{"registryType":"oci","identifier":"ghcr.io/test/shared-tool-1:latest","transport":{"type":"stdio"}}]},` +
		`{"name":"io.test/shared-tool-2","version":"1.0.0","description":"shared tool 2","packages":[{"registryType":"oci","identifier":"ghcr.io/test/shared-tool-2:latest","transport":{"type":"stdio"}}]}` +
		`]}}`
	platformData := `{"version":"1.0.0","meta":{"last_updated":"2025-01-01T00:00:00Z"},"data":{"servers":[` +
		`{"name":"io.test/deploy-helper","version":"1.0.0","description":"deploy helper","packages":[{"registryType":"oci","identifier":"ghcr.io/test/deploy-helper:latest","transport":{"type":"stdio"}}]},` +
		`{"name":"io.test/infra-scanner","version":"2.0.0","description":"infra scanner","packages":[{"registryType":"oci","identifier":"ghcr.io/test/infra-scanner:latest","transport":{"type":"stdio"}}]}` +
		`]}}`
	dataToolsData := `{"version":"1.0.0","meta":{"last_updated":"2025-01-01T00:00:00Z"},"data":{"servers":[` +
		`{"name":"io.test/data-pipeline","version":"1.0.0","description":"data pipeline","packages":[{"registryType":"oci","identifier":"ghcr.io/test/data-pipeline:latest","transport":{"type":"stdio"}}]},` +
		`{"name":"io.test/ml-trainer","version":"3.0.0","description":"ml trainer","packages":[{"registryType":"oci","identifier":"ghcr.io/test/ml-trainer:latest","transport":{"type":"stdio"}}]}` +
		`]}}`

	return []config.SourceConfig{
		{
			Name:       "shared-catalog",
			Format:     "upstream",
			File:       &config.FileConfig{Path: writeFixture("shared-catalog", sharedData)},
			SyncPolicy: &config.SyncPolicyConfig{Interval: "10s"},
			Claims:     map[string]any{"org": "acme"},
		},
		{
			Name:       "platform-tools",
			Format:     "upstream",
			File:       &config.FileConfig{Path: writeFixture("platform-tools", platformData)},
			SyncPolicy: &config.SyncPolicyConfig{Interval: "10s"},
			Claims:     map[string]any{"org": "acme", "team": "platform"},
		},
		{
			Name:       "data-tools",
			Format:     "upstream",
			File:       &config.FileConfig{Path: writeFixture("data-tools", dataToolsData)},
			SyncPolicy: &config.SyncPolicyConfig{Interval: "10s"},
			Claims:     map[string]any{"org": "acme", "team": "data"},
		},
		{Name: "internal", Managed: &config.ManagedConfig{}},
	}
}

func baseRegistryConfigs() []config.RegistryConfig {
	return []config.RegistryConfig{
		{Name: "acme-all", Claims: map[string]any{"org": "acme"}, Sources: []string{"shared-catalog", "platform-tools", "data-tools", "internal"}},
		{Name: "acme-platform", Claims: map[string]any{"org": "acme", "team": "platform"}, Sources: []string{"shared-catalog", "platform-tools", "internal"}},
		{Name: "acme-data", Claims: map[string]any{"org": "acme", "team": "data"}, Sources: []string{"shared-catalog", "data-tools", "internal"}},
	}
}

func authzRolesConfig() *config.AuthzConfig {
	return &config.AuthzConfig{
		Roles: config.RolesConfig{
			SuperAdmin:       []map[string]any{{"role": "super-admin"}},
			ManageSources:    []map[string]any{{"role": "admin"}},
			ManageRegistries: []map[string]any{{"role": "admin"}},
			ManageEntries:    []map[string]any{{"role": "writer"}},
		},
	}
}

func dbConfigFromConnStr(t *testing.T, connStr string) *config.DatabaseConfig {
	t.Helper()
	// Connection string format: postgres://user:pass@host:port/db?sslmode=disable
	// Parse manually to extract components
	connStr = strings.TrimPrefix(connStr, "postgres://")
	userPass, rest, _ := strings.Cut(connStr, "@")
	user, password, _ := strings.Cut(userPass, ":")
	hostPort, dbAndParams, _ := strings.Cut(rest, "/")
	host, portStr, _ := strings.Cut(hostPort, ":")
	dbName, _, _ := strings.Cut(dbAndParams, "?")

	port := 5432
	if portStr != "" {
		_, _ = fmt.Sscanf(portStr, "%d", &port)
	}

	// The DatabaseConfig doesn't carry a password field — the production system
	// uses pgpass/PGPASSFILE. For integration tests, set PGPASSWORD so pgx can
	// authenticate when the app creates its own connection pool.
	// All testcontainers use the same password so this is safe with parallel tests.
	if password != "" {
		os.Setenv("PGPASSWORD", password) //nolint:tenv // can't use t.Setenv in parallel tests
	}

	return &config.DatabaseConfig{
		Host:     host,
		Port:     port,
		User:     user,
		Database: dbName,
		SSLMode:  "disable",
	}
}

// ---------------------------------------------------------------------------
// Test environment
// ---------------------------------------------------------------------------

type testEnv struct {
	baseURL string
	oidc    *mockOIDCServer
}

func setupEnv(t *testing.T, authCfg *config.AuthConfig) *testEnv {
	t.Helper()
	return setupEnvCustom(t, authCfg, baseSourceConfigs(t), baseRegistryConfigs())
}

func setupEnvCustom(t *testing.T, authCfg *config.AuthConfig, sources []config.SourceConfig, registries []config.RegistryConfig) *testEnv {
	t.Helper()

	db, cleanup := database.SetupTestDB(t)
	t.Cleanup(cleanup)

	connStr := db.Config().ConnString()
	dbCfg := dbConfigFromConnStr(t, connStr)

	var oidcServer *mockOIDCServer
	if authCfg != nil && authCfg.Mode == config.AuthModeOAuth {
		oidcServer = newMockOIDCServer(t)
		t.Cleanup(oidcServer.Close)
		authCfg.OAuth = &config.OAuthConfig{
			ResourceURL: "http://localhost/test",
			Providers: []config.OAuthProviderConfig{{
				Name:           "test",
				IssuerURL:      oidcServer.issuerURL,
				Audience:       "test-audience",
				AllowPrivateIP: true,
			}},
		}
		authCfg.InsecureAllowHTTP = true
	}

	cfg := &config.Config{
		Sources:    sources,
		Registries: registries,
		Database:   dbCfg,
		Auth:       authCfg,
	}

	ctx := context.Background()
	app, err := registryapp.NewRegistryApp(ctx,
		registryapp.WithConfig(cfg),
		registryapp.WithAddress(":0"),
		registryapp.WithCoordinatorOptions(coordinator.TestingWithPollingInterval(500*time.Millisecond)),
	)
	require.NoError(t, err)

	// Get a random free port, assign it, and start the server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := listener.Addr().String()
	listener.Close()

	app.GetHTTPServer().Addr = addr

	errCh := make(chan error, 1)
	go func() { errCh <- app.Start() }()

	t.Cleanup(func() {
		_ = app.Stop(5 * time.Second)
	})

	baseURL := "http://" + addr
	waitForReady(t, baseURL)

	return &testEnv{
		baseURL: baseURL,
		oidc:    oidcServer,
	}
}

func waitForReady(t *testing.T, baseURL string) {
	t.Helper()
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := client.Get(baseURL + "/readiness")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("server did not become ready within timeout")
}

// ---------------------------------------------------------------------------
// HTTP helpers
// ---------------------------------------------------------------------------

func doRequest(t *testing.T, method, url, token string, body any) *http.Response {
	t.Helper()
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(t, err)
		bodyReader = strings.NewReader(string(b))
	}
	req, err := http.NewRequest(method, url, bodyReader)
	require.NoError(t, err)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	require.NoError(t, err)
	return resp
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return string(b)
}

func assertStatus(t *testing.T, resp *http.Response, want int) string {
	t.Helper()
	body := readBody(t, resp)
	assert.Equal(t, want, resp.StatusCode, "unexpected status for %s; body: %s", resp.Request.URL, body)
	return body
}

func serverJSON(name string) *upstreamv0.ServerJSON {
	return &upstreamv0.ServerJSON{
		Schema:      "https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json",
		Name:        "io.test/" + name,
		Description: name + " server",
		Version:     "1.0.0",
		Packages: []model.Package{{
			RegistryType: "oci",
			Identifier:   "ghcr.io/test/" + name + ":latest",
			Transport:    model.Transport{Type: "stdio"},
		}},
	}
}

type publishReq struct {
	Claims map[string]any         `json:"claims,omitempty"`
	Server *upstreamv0.ServerJSON `json:"server,omitempty"`
}

// waitForSyncSimple waits for a specific entry to appear at a specific path.
func waitForSyncSimple(t *testing.T, env *testEnv, token, path, search string) {
	t.Helper()
	client := &http.Client{Timeout: 5 * time.Second}
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		req, err := http.NewRequest("GET", env.baseURL+path, nil)
		require.NoError(t, err)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := client.Do(req)
		if err == nil {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode == 200 && strings.Contains(string(body), search) {
				return
			}
		}
		time.Sleep(1 * time.Second)
	}
	t.Fatalf("timed out waiting for %s at %s", search, path)
}

// waitForSyncAnonymous waits for at least one file source to sync in anonymous mode.
func waitForSyncAnonymous(t *testing.T, env *testEnv) {
	t.Helper()
	client := &http.Client{Timeout: 5 * time.Second}
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := client.Get(env.baseURL + "/registry/acme-all/v0.1/servers")
		if err == nil {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			// Any server data means at least one source has synced
			if resp.StatusCode == 200 && strings.Contains(string(body), "\"servers\"") &&
				!strings.Contains(string(body), "\"servers\":[]") {
				return
			}
		}
		time.Sleep(1 * time.Second)
	}
	t.Fatal("file sources did not sync within timeout")
}

// waitForSync waits for all file sources to sync by checking if entries from each are visible.
func waitForSync(t *testing.T, env *testEnv, token string) {
	t.Helper()
	client := &http.Client{Timeout: 5 * time.Second}
	// Check for entries from each file source: shared-catalog, platform-tools, data-tools
	checks := []struct {
		registry string
		search   string
	}{
		{"acme-platform", "deploy-helper"}, // from platform-tools
		{"acme-data", "data-pipeline"},     // from data-tools
		{"acme-all", "shared-tool-1"},      // from shared-catalog
	}
	deadline := time.Now().Add(60 * time.Second)
	for _, check := range checks {
		found := false
		for time.Now().Before(deadline) {
			req, err := http.NewRequest("GET",
				fmt.Sprintf("%s/registry/%s/v0.1/servers?search=%s", env.baseURL, check.registry, check.search), nil)
			require.NoError(t, err)
			req.Header.Set("Authorization", "Bearer "+token)
			resp, err := client.Do(req)
			if err == nil {
				body, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				if resp.StatusCode == 200 && strings.Contains(string(body), check.search) {
					found = true
					break
				}
			}
			time.Sleep(1 * time.Second)
		}
		if !found {
			t.Fatalf("timed out waiting for %s in registry %s", check.search, check.registry)
		}
	}
}

// ---------------------------------------------------------------------------
// Test 1: Anonymous mode (auth=no, authz=no)
// ---------------------------------------------------------------------------

func TestAuthzIntegration_AnonymousMode(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := setupEnv(t, &config.AuthConfig{
		Mode: config.AuthModeAnonymous,
	})

	// Wait for file sources to sync (anonymous mode, no token needed)
	waitForSyncAnonymous(t, env)

	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
	}{
		{"health", "GET", "/health", 200},
		{"readiness", "GET", "/readiness", 200},
		{"list servers without token", "GET", "/registry/acme-all/v0.1/servers", 200},
		{"list sources without token", "GET", "/v1/sources", 200},
		{"list registries without token", "GET", "/v1/registries", 200},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			resp := doRequest(t, tt.method, env.baseURL+tt.path, "", nil)
			assertStatus(t, resp, tt.wantStatus)
		})
	}
}

// ---------------------------------------------------------------------------
// Test 2: Auth-only mode (auth=yes, authz=no)
// ---------------------------------------------------------------------------

func TestAuthzIntegration_AuthOnlyMode(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := setupEnv(t, &config.AuthConfig{
		Mode: config.AuthModeOAuth,
	})

	validToken := env.oidc.token(t, map[string]any{"org": "acme", "team": "platform"})

	// Wait for file sources to sync — check acme-platform since that's what this token can access
	waitForSyncSimple(t, env, validToken, "/registry/acme-platform/v0.1/servers", "deploy-helper")

	tests := []struct {
		name       string
		method     string
		path       string
		token      string
		wantStatus int
	}{
		{"health no token", "GET", "/health", "", 200},
		{"readiness no token", "GET", "/readiness", "", 200},
		{"servers no token", "GET", "/registry/acme-all/v0.1/servers", "", 401},
		{"sources no token", "GET", "/v1/sources", "", 401},
		{"registries no token", "GET", "/v1/registries", "", 401},
		{"servers valid token", "GET", "/registry/acme-all/v0.1/servers", validToken, 200},
		{"sources valid token", "GET", "/v1/sources", validToken, 200},
		{"registries valid token", "GET", "/v1/registries", validToken, 200},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			resp := doRequest(t, tt.method, env.baseURL+tt.path, tt.token, nil)
			assertStatus(t, resp, tt.wantStatus)
		})
	}
}

// ---------------------------------------------------------------------------
// Test 3: Full authz mode (auth=yes, authz=yes)
// ---------------------------------------------------------------------------

//nolint:paralleltest,tparallel // subtests must run sequentially — publish, visibility, delete depend on ordering
func TestAuthzIntegration_FullAuthzMode(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := setupEnv(t, &config.AuthConfig{
		Mode:  config.AuthModeOAuth,
		Authz: authzRolesConfig(),
	})

	// Persona tokens
	superAdmin := env.oidc.token(t, map[string]any{"org": "acme", "role": "super-admin"})
	platformAdmin := env.oidc.token(t, map[string]any{"org": "acme", "team": "platform", "role": "admin"})
	platformWriter := env.oidc.token(t, map[string]any{"org": "acme", "team": "platform", "role": "writer"})
	dataWriter := env.oidc.token(t, map[string]any{"org": "acme", "team": "data", "role": "writer"})
	outsider := env.oidc.token(t, map[string]any{"org": "contoso", "role": "admin"})

	// Wait for file sources to sync (shared-catalog, platform-tools, data-tools)
	waitForSync(t, env, superAdmin)

	// Section 1: System endpoints
	t.Run("system endpoints", func(t *testing.T) {
		tests := []struct {
			name       string
			path       string
			wantStatus int
		}{
			{"health", "/health", 200},
			{"readiness", "/readiness", 200},
			{"oauth-protected-resource", "/.well-known/oauth-protected-resource", 200},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				resp := doRequest(t, "GET", env.baseURL+tt.path, "", nil)
				assertStatus(t, resp, tt.wantStatus)
			})
		}
	})

	// Section 2: Unauthenticated access
	t.Run("unauthenticated access", func(t *testing.T) {
		tests := []struct {
			name       string
			path       string
			token      string
			wantStatus int
		}{
			{"servers no token", "/registry/acme-all/v0.1/servers", "", 401},
			{"sources no token", "/v1/sources", "", 401},
			{"registries no token", "/v1/registries", "", 401},
			{"servers malformed token", "/registry/acme-all/v0.1/servers", "not.a.valid.jwt", 401},
			{"servers empty bearer", "/registry/acme-all/v0.1/servers", " ", 401},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				var resp *http.Response
				if tt.token == " " {
					// Send Authorization header with empty Bearer value
					req, err := http.NewRequest("GET", env.baseURL+tt.path, nil)
					require.NoError(t, err)
					req.Header.Set("Authorization", "Bearer ")
					client := &http.Client{Timeout: 10 * time.Second}
					resp, err = client.Do(req)
					require.NoError(t, err)
				} else {
					resp = doRequest(t, "GET", env.baseURL+tt.path, tt.token, nil)
				}
				assertStatus(t, resp, tt.wantStatus)
			})
		}
	})

	// Section 3: Registry access gate
	t.Run("registry access gate", func(t *testing.T) {
		tests := []struct {
			name       string
			registry   string
			token      string
			wantStatus int
		}{
			{"platform-writer acme-all", "acme-all", platformWriter, 200},
			{"data-writer acme-all", "acme-all", dataWriter, 200},
			{"platform-writer acme-platform", "acme-platform", platformWriter, 200},
			{"data-writer acme-platform blocked", "acme-platform", dataWriter, 403},
			{"data-writer acme-data", "acme-data", dataWriter, 200},
			{"platform-writer acme-data blocked", "acme-data", platformWriter, 403},
			{"outsider acme-all blocked", "acme-all", outsider, 403},
			{"outsider acme-platform blocked", "acme-platform", outsider, 403},
			{"super-admin acme-all", "acme-all", superAdmin, 200},
			{"super-admin acme-platform", "acme-platform", superAdmin, 200},
			{"super-admin acme-data", "acme-data", superAdmin, 200},
			{"super-admin does-not-exist", "does-not-exist", superAdmin, 404},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				resp := doRequest(t, "GET", env.baseURL+"/registry/"+tt.registry+"/v0.1/servers", tt.token, nil)
				assertStatus(t, resp, tt.wantStatus)
			})
		}
	})

	// Section 4: Per-user entry filtering
	t.Run("per-user entry filtering", func(t *testing.T) {
		tests := []struct {
			name       string
			registry   string
			search     string
			token      string
			wantStatus int
			wantFound  bool
		}{
			{"platform-writer sees deploy-helper", "acme-all", "deploy-helper", platformWriter, 200, true},
			{"platform-writer sees infra-scanner", "acme-all", "infra-scanner", platformWriter, 200, true},
			{"platform-writer no data-pipeline", "acme-all", "data-pipeline", platformWriter, 200, false},
			{"platform-writer no ml-trainer", "acme-all", "ml-trainer", platformWriter, 200, false},
			{"data-writer sees data-pipeline", "acme-all", "data-pipeline", dataWriter, 200, true},
			{"data-writer sees ml-trainer", "acme-all", "ml-trainer", dataWriter, 200, true},
			{"data-writer no deploy-helper", "acme-all", "deploy-helper", dataWriter, 200, false},
			{"data-writer no infra-scanner", "acme-all", "infra-scanner", dataWriter, 200, false},
			// shared-catalog entries (claims: org=acme, no team) are visible to all acme users
			{"platform-writer sees shared", "acme-all", "shared-tool-1", platformWriter, 200, true},
			{"data-writer sees shared", "acme-all", "shared-tool-1", dataWriter, 200, true},
			{"super-admin sees deploy-helper", "acme-all", "deploy-helper", superAdmin, 200, true},
			{"super-admin sees data-pipeline", "acme-all", "data-pipeline", superAdmin, 200, true},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				url := fmt.Sprintf("%s/registry/%s/v0.1/servers?search=%s", env.baseURL, tt.registry, tt.search)
				resp := doRequest(t, "GET", url, tt.token, nil)
				body := assertStatus(t, resp, tt.wantStatus)
				if tt.wantFound {
					assert.Contains(t, body, tt.search, "expected to find %q in response", tt.search)
				} else {
					var result struct {
						Metadata struct {
							Count int `json:"count"`
						} `json:"metadata"`
					}
					require.NoError(t, json.Unmarshal([]byte(body), &result))
					assert.Equal(t, 0, result.Metadata.Count, "expected 0 servers for %q", tt.search)
				}
			})
		}
	})

	// Section 5: Admin source listing
	t.Run("admin source listing", func(t *testing.T) {
		t.Run("platform-admin lists sources", func(t *testing.T) {
			resp := doRequest(t, "GET", env.baseURL+"/v1/sources", platformAdmin, nil)
			body := assertStatus(t, resp, 200)
			assert.Contains(t, body, "shared-catalog")
		})

		t.Run("platform-admin gets shared-catalog", func(t *testing.T) {
			resp := doRequest(t, "GET", env.baseURL+"/v1/sources/shared-catalog", platformAdmin, nil)
			assertStatus(t, resp, 200)
		})

		t.Run("platform-admin gets platform-tools", func(t *testing.T) {
			resp := doRequest(t, "GET", env.baseURL+"/v1/sources/platform-tools", platformAdmin, nil)
			assertStatus(t, resp, 200)
		})

		t.Run("platform-admin cannot see data-tools", func(t *testing.T) {
			resp := doRequest(t, "GET", env.baseURL+"/v1/sources/data-tools", platformAdmin, nil)
			assertStatus(t, resp, 404)
		})

		t.Run("platform-writer forbidden from sources", func(t *testing.T) {
			resp := doRequest(t, "GET", env.baseURL+"/v1/sources", platformWriter, nil)
			assertStatus(t, resp, 403)
		})

		t.Run("data-writer forbidden from sources", func(t *testing.T) {
			resp := doRequest(t, "GET", env.baseURL+"/v1/sources", dataWriter, nil)
			assertStatus(t, resp, 403)
		})

		t.Run("outsider sees only internal source", func(t *testing.T) {
			resp := doRequest(t, "GET", env.baseURL+"/v1/sources", outsider, nil)
			body := assertStatus(t, resp, 200)
			var result struct {
				Sources []struct {
					Name string `json:"name"`
				} `json:"sources"`
			}
			require.NoError(t, json.Unmarshal([]byte(body), &result))
			assert.Len(t, result.Sources, 1)
		})

		t.Run("super-admin sees all sources", func(t *testing.T) {
			resp := doRequest(t, "GET", env.baseURL+"/v1/sources", superAdmin, nil)
			body := assertStatus(t, resp, 200)
			assert.Contains(t, body, "data-tools")
		})
	})

	// Section 6: Admin registry listing
	t.Run("admin registry listing", func(t *testing.T) {
		t.Run("platform-admin lists registries", func(t *testing.T) {
			resp := doRequest(t, "GET", env.baseURL+"/v1/registries", platformAdmin, nil)
			body := assertStatus(t, resp, 200)
			assert.Contains(t, body, "acme-all")
		})

		t.Run("platform-admin gets acme-platform", func(t *testing.T) {
			resp := doRequest(t, "GET", env.baseURL+"/v1/registries/acme-platform", platformAdmin, nil)
			assertStatus(t, resp, 200)
		})

		t.Run("platform-admin cannot see acme-data", func(t *testing.T) {
			resp := doRequest(t, "GET", env.baseURL+"/v1/registries/acme-data", platformAdmin, nil)
			assertStatus(t, resp, 404)
		})

		t.Run("platform-writer lists registries", func(t *testing.T) {
			resp := doRequest(t, "GET", env.baseURL+"/v1/registries", platformWriter, nil)
			assertStatus(t, resp, 200)
		})

		t.Run("data-writer lists registries", func(t *testing.T) {
			resp := doRequest(t, "GET", env.baseURL+"/v1/registries", dataWriter, nil)
			assertStatus(t, resp, 200)
		})

		t.Run("super-admin sees all registries", func(t *testing.T) {
			resp := doRequest(t, "GET", env.baseURL+"/v1/registries", superAdmin, nil)
			body := assertStatus(t, resp, 200)
			assert.Contains(t, body, "acme-data")
		})
	})

	// Section 7: Publish
	t.Run("publish", func(t *testing.T) {
		t.Run("platform-writer publishes with matching claims", func(t *testing.T) {
			resp := doRequest(t, "POST", env.baseURL+"/v1/entries", platformWriter, publishReq{
				Claims: map[string]any{"org": "acme", "team": "platform"},
				Server: serverJSON("custom-linter"),
			})
			assertStatus(t, resp, 201)
		})

		t.Run("platform-writer blocked with mismatched claims", func(t *testing.T) {
			resp := doRequest(t, "POST", env.baseURL+"/v1/entries", platformWriter, publishReq{
				Claims: map[string]any{"org": "acme", "team": "finance"},
				Server: serverJSON("sneaky-tool"),
			})
			assertStatus(t, resp, 403)
		})

		t.Run("data-writer publishes with matching claims", func(t *testing.T) {
			resp := doRequest(t, "POST", env.baseURL+"/v1/entries", dataWriter, publishReq{
				Claims: map[string]any{"org": "acme", "team": "data"},
				Server: serverJSON("data-analyzer"),
			})
			assertStatus(t, resp, 201)
		})

		t.Run("outsider blocked from publish", func(t *testing.T) {
			resp := doRequest(t, "POST", env.baseURL+"/v1/entries", outsider, publishReq{
				Claims: map[string]any{"org": "contoso"},
				Server: serverJSON("bad-tool"),
			})
			assertStatus(t, resp, 403)
		})

		t.Run("platform-writer duplicate publish", func(t *testing.T) {
			resp := doRequest(t, "POST", env.baseURL+"/v1/entries", platformWriter, publishReq{
				Claims: map[string]any{"org": "acme", "team": "platform"},
				Server: serverJSON("custom-linter"),
			})
			assertStatus(t, resp, 409)
		})
	})

	// Section 8: Published entry visibility
	t.Run("published entry visibility", func(t *testing.T) {
		tests := []struct {
			name      string
			search    string
			token     string
			wantFound bool
		}{
			{"platform-writer sees custom-linter", "custom-linter", platformWriter, true},
			{"data-writer no custom-linter", "custom-linter", dataWriter, false},
			{"data-writer sees data-analyzer", "data-analyzer", dataWriter, true},
			{"platform-writer no data-analyzer", "data-analyzer", platformWriter, false},
			{"super-admin sees custom-linter", "custom-linter", superAdmin, true},
			{"super-admin sees data-analyzer", "data-analyzer", superAdmin, true},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				url := fmt.Sprintf("%s/registry/acme-all/v0.1/servers?search=%s", env.baseURL, tt.search)
				resp := doRequest(t, "GET", url, tt.token, nil)
				body := assertStatus(t, resp, 200)
				if tt.wantFound {
					assert.Contains(t, body, tt.search)
				} else {
					var result struct {
						Metadata struct {
							Count int `json:"count"`
						} `json:"metadata"`
					}
					require.NoError(t, json.Unmarshal([]byte(body), &result))
					assert.Equal(t, 0, result.Metadata.Count)
				}
			})
		}
	})

	// Section 9: Delete
	t.Run("delete", func(t *testing.T) {
		t.Run("data-writer cannot delete custom-linter", func(t *testing.T) {
			resp := doRequest(t, "DELETE", env.baseURL+"/v1/entries/server/io.test%2Fcustom-linter/versions/1.0.0", dataWriter, nil)
			assertStatus(t, resp, 403)
		})

		t.Run("platform-writer deletes custom-linter", func(t *testing.T) {
			resp := doRequest(t, "DELETE", env.baseURL+"/v1/entries/server/io.test%2Fcustom-linter/versions/1.0.0", platformWriter, nil)
			assertStatus(t, resp, 204)
		})

		t.Run("super-admin deletes data-analyzer", func(t *testing.T) {
			resp := doRequest(t, "DELETE", env.baseURL+"/v1/entries/server/io.test%2Fdata-analyzer/versions/1.0.0", superAdmin, nil)
			assertStatus(t, resp, 204)
		})

		t.Run("platform-writer delete already deleted", func(t *testing.T) {
			resp := doRequest(t, "DELETE", env.baseURL+"/v1/entries/server/io.test%2Fcustom-linter/versions/1.0.0", platformWriter, nil)
			assertStatus(t, resp, 404)
		})
	})

	// Section 10: Config-managed protection
	t.Run("config-managed protection", func(t *testing.T) {
		t.Run("cannot delete config source", func(t *testing.T) {
			resp := doRequest(t, "DELETE", env.baseURL+"/v1/sources/shared-catalog", platformAdmin, nil)
			assertStatus(t, resp, 403)
		})

		t.Run("cannot update config source", func(t *testing.T) {
			resp := doRequest(t, "PUT", env.baseURL+"/v1/sources/shared-catalog", platformAdmin, map[string]any{
				"managed": map[string]any{},
				"claims":  map[string]any{"org": "acme"},
			})
			assertStatus(t, resp, 403)
		})

		t.Run("cannot delete config registry", func(t *testing.T) {
			resp := doRequest(t, "DELETE", env.baseURL+"/v1/registries/acme-all", platformAdmin, nil)
			assertStatus(t, resp, 403)
		})
	})
}

// ---------------------------------------------------------------------------
// Additional helpers
// ---------------------------------------------------------------------------

func serverJSONWithVersion(name, version string) *upstreamv0.ServerJSON {
	return &upstreamv0.ServerJSON{
		Schema:      "https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json",
		Name:        "io.test/" + name,
		Description: name + " server",
		Version:     version,
		Packages: []model.Package{{
			RegistryType: "oci",
			Identifier:   "ghcr.io/test/" + name + ":latest",
			Transport:    model.Transport{Type: "stdio"},
		}},
	}
}

type publishSkillReq struct {
	Claims map[string]any `json:"claims,omitempty"`
	Skill  map[string]any `json:"skill,omitempty"`
}

func parseRegistryNames(t *testing.T, body string) []string {
	t.Helper()
	var result struct {
		Registries []struct {
			Name string `json:"name"`
		} `json:"registries"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &result))
	names := make([]string, len(result.Registries))
	for i, r := range result.Registries {
		names[i] = r.Name
	}
	return names
}

// ---------------------------------------------------------------------------
// Test 4: Source CRUD
// ---------------------------------------------------------------------------

//nolint:paralleltest,tparallel // subtests must run sequentially — create, update, delete depend on ordering
func TestAuthzIntegration_SourceCRUD(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := setupEnv(t, &config.AuthConfig{
		Mode:  config.AuthModeOAuth,
		Authz: authzRolesConfig(),
	})

	platformAdmin := env.oidc.token(t, map[string]any{"org": "acme", "team": "platform", "role": "admin"})
	outsider := env.oidc.token(t, map[string]any{"org": "contoso", "role": "admin"})
	superAdmin := env.oidc.token(t, map[string]any{"org": "acme", "role": "super-admin"})

	waitForSync(t, env, superAdmin)

	t.Run("create API source with matching claims", func(t *testing.T) {
		resp := doRequest(t, "PUT", env.baseURL+"/v1/sources/test-api-src", platformAdmin, map[string]any{
			"managed": map[string]any{},
			"claims":  map[string]any{"org": "acme", "team": "platform"},
		})
		assertStatus(t, resp, 201)
	})

	t.Run("create source with non-subset claims", func(t *testing.T) {
		resp := doRequest(t, "PUT", env.baseURL+"/v1/sources/bad-src", platformAdmin, map[string]any{
			"managed": map[string]any{},
			"claims":  map[string]any{"org": "acme", "team": "finance"},
		})
		assertStatus(t, resp, 403)
	})

	t.Run("update API source", func(t *testing.T) {
		resp := doRequest(t, "PUT", env.baseURL+"/v1/sources/test-api-src", platformAdmin, map[string]any{
			"managed": map[string]any{},
			"claims":  map[string]any{"org": "acme", "team": "platform"},
		})
		assertStatus(t, resp, 200)
	})

	t.Run("outsider cannot see API source", func(t *testing.T) {
		resp := doRequest(t, "GET", env.baseURL+"/v1/sources/test-api-src", outsider, nil)
		assertStatus(t, resp, 404)
	})

	t.Run("superAdmin sees API source", func(t *testing.T) {
		resp := doRequest(t, "GET", env.baseURL+"/v1/sources/test-api-src", superAdmin, nil)
		assertStatus(t, resp, 200)
	})

	t.Run("delete API source", func(t *testing.T) {
		resp := doRequest(t, "DELETE", env.baseURL+"/v1/sources/test-api-src", platformAdmin, nil)
		assertStatus(t, resp, 204)
	})

	t.Run("delete again 404", func(t *testing.T) {
		resp := doRequest(t, "DELETE", env.baseURL+"/v1/sources/test-api-src", platformAdmin, nil)
		assertStatus(t, resp, 404)
	})
}

// ---------------------------------------------------------------------------
// Test 5: Registry CRUD
// ---------------------------------------------------------------------------

//nolint:paralleltest,tparallel // subtests must run sequentially — create, update, delete depend on ordering
func TestAuthzIntegration_RegistryCRUD(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := setupEnv(t, &config.AuthConfig{
		Mode:  config.AuthModeOAuth,
		Authz: authzRolesConfig(),
	})

	platformAdmin := env.oidc.token(t, map[string]any{"org": "acme", "team": "platform", "role": "admin"})
	outsider := env.oidc.token(t, map[string]any{"org": "contoso", "role": "admin"})
	superAdmin := env.oidc.token(t, map[string]any{"org": "acme", "role": "super-admin"})

	waitForSync(t, env, superAdmin)

	t.Run("create API registry", func(t *testing.T) {
		resp := doRequest(t, "PUT", env.baseURL+"/v1/registries/test-reg", platformAdmin, map[string]any{
			"sources": []string{"shared-catalog", "internal"},
			"claims":  map[string]any{"org": "acme", "team": "platform"},
		})
		assertStatus(t, resp, 201)
	})

	t.Run("create registry with non-subset claims", func(t *testing.T) {
		resp := doRequest(t, "PUT", env.baseURL+"/v1/registries/bad-reg", platformAdmin, map[string]any{
			"sources": []string{"shared-catalog"},
			"claims":  map[string]any{"org": "acme", "team": "finance"},
		})
		assertStatus(t, resp, 403)
	})

	t.Run("update API registry", func(t *testing.T) {
		resp := doRequest(t, "PUT", env.baseURL+"/v1/registries/test-reg", platformAdmin, map[string]any{
			"sources": []string{"shared-catalog", "platform-tools", "internal"},
			"claims":  map[string]any{"org": "acme", "team": "platform"},
		})
		assertStatus(t, resp, 200)
	})

	t.Run("outsider cannot see API registry", func(t *testing.T) {
		resp := doRequest(t, "GET", env.baseURL+"/v1/registries/test-reg", outsider, nil)
		assertStatus(t, resp, 404)
	})

	t.Run("cannot update config-managed registry", func(t *testing.T) {
		resp := doRequest(t, "PUT", env.baseURL+"/v1/registries/acme-all", superAdmin, map[string]any{
			"sources": []string{"shared-catalog"},
			"claims":  map[string]any{"org": "acme"},
		})
		assertStatus(t, resp, 403)
	})

	t.Run("delete API registry", func(t *testing.T) {
		resp := doRequest(t, "DELETE", env.baseURL+"/v1/registries/test-reg", platformAdmin, nil)
		assertStatus(t, resp, 204)
	})
}

// ---------------------------------------------------------------------------
// Test 6: Source delete protection (source referenced by registry)
// ---------------------------------------------------------------------------

//nolint:paralleltest,tparallel // subtests must run sequentially — create, delete depend on ordering
func TestAuthzIntegration_SourceDeleteProtection(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := setupEnv(t, &config.AuthConfig{
		Mode:  config.AuthModeOAuth,
		Authz: authzRolesConfig(),
	})

	platformAdmin := env.oidc.token(t, map[string]any{"org": "acme", "team": "platform", "role": "admin"})
	superAdmin := env.oidc.token(t, map[string]any{"org": "acme", "role": "super-admin"})

	waitForSync(t, env, superAdmin)

	// Create a temp fixture file for the file source
	fixtureData := `{"version":"1.0.0","meta":{"last_updated":"2025-01-01T00:00:00Z"},"data":{"servers":[` +
		`{"name":"io.test/ref-tool","version":"1.0.0","description":"ref tool","packages":[{"registryType":"oci","identifier":"ghcr.io/test/ref-tool:latest","transport":{"type":"stdio"}}]}` +
		`]}}`
	f, err := os.CreateTemp(t.TempDir(), "ref-fixture-*.json")
	require.NoError(t, err)
	_, err = f.WriteString(fixtureData)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	t.Run("create source", func(t *testing.T) {
		resp := doRequest(t, "PUT", env.baseURL+"/v1/sources/ref-src", platformAdmin, map[string]any{
			"format":     "upstream",
			"file":       map[string]any{"path": f.Name()},
			"syncPolicy": map[string]any{"interval": "5m"},
			"claims":     map[string]any{"org": "acme", "team": "platform"},
		})
		assertStatus(t, resp, 201)
	})

	t.Run("create registry referencing source", func(t *testing.T) {
		resp := doRequest(t, "PUT", env.baseURL+"/v1/registries/ref-reg", platformAdmin, map[string]any{
			"sources": []string{"ref-src"},
			"claims":  map[string]any{"org": "acme", "team": "platform"},
		})
		assertStatus(t, resp, 201)
	})

	t.Run("delete source blocked", func(t *testing.T) {
		resp := doRequest(t, "DELETE", env.baseURL+"/v1/sources/ref-src", platformAdmin, nil)
		assertStatus(t, resp, 409)
	})

	t.Run("delete registry first", func(t *testing.T) {
		resp := doRequest(t, "DELETE", env.baseURL+"/v1/registries/ref-reg", platformAdmin, nil)
		assertStatus(t, resp, 204)
	})

	t.Run("now delete source succeeds", func(t *testing.T) {
		resp := doRequest(t, "DELETE", env.baseURL+"/v1/sources/ref-src", platformAdmin, nil)
		assertStatus(t, resp, 204)
	})
}

// ---------------------------------------------------------------------------
// Test 7: Publish claim consistency (same entry name must have same claims)
// ---------------------------------------------------------------------------

//nolint:paralleltest,tparallel // subtests must run sequentially — publish ordering matters
func TestAuthzIntegration_PublishClaimConsistency(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := setupEnv(t, &config.AuthConfig{
		Mode:  config.AuthModeOAuth,
		Authz: authzRolesConfig(),
	})

	platformWriter := env.oidc.token(t, map[string]any{"org": "acme", "team": "platform", "role": "writer"})
	superAdmin := env.oidc.token(t, map[string]any{"org": "acme", "role": "super-admin"})

	waitForSync(t, env, superAdmin)

	t.Run("publish v1.0.0", func(t *testing.T) {
		resp := doRequest(t, "POST", env.baseURL+"/v1/entries", platformWriter, publishReq{
			Claims: map[string]any{"org": "acme", "team": "platform"},
			Server: serverJSONWithVersion("multi-ver", "1.0.0"),
		})
		assertStatus(t, resp, 201)
	})

	t.Run("publish v2.0.0 same claims", func(t *testing.T) {
		resp := doRequest(t, "POST", env.baseURL+"/v1/entries", platformWriter, publishReq{
			Claims: map[string]any{"org": "acme", "team": "platform"},
			Server: serverJSONWithVersion("multi-ver", "2.0.0"),
		})
		assertStatus(t, resp, 201)
	})

	t.Run("publish v3.0.0 different claims", func(t *testing.T) {
		resp := doRequest(t, "POST", env.baseURL+"/v1/entries", superAdmin, publishReq{
			Claims: map[string]any{"org": "acme", "team": "data"},
			Server: serverJSONWithVersion("multi-ver", "3.0.0"),
		})
		assertStatus(t, resp, 409)
	})

	t.Run("duplicate v1.0.0", func(t *testing.T) {
		resp := doRequest(t, "POST", env.baseURL+"/v1/entries", platformWriter, publishReq{
			Claims: map[string]any{"org": "acme", "team": "platform"},
			Server: serverJSONWithVersion("multi-ver", "1.0.0"),
		})
		assertStatus(t, resp, 409)
	})
}

// ---------------------------------------------------------------------------
// Test 8: Registry list filtering
// ---------------------------------------------------------------------------

func TestAuthzIntegration_RegistryListFiltering(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := setupEnv(t, &config.AuthConfig{
		Mode:  config.AuthModeOAuth,
		Authz: authzRolesConfig(),
	})

	platformWriter := env.oidc.token(t, map[string]any{"org": "acme", "team": "platform", "role": "writer"})
	dataWriter := env.oidc.token(t, map[string]any{"org": "acme", "team": "data", "role": "writer"})
	outsider := env.oidc.token(t, map[string]any{"org": "contoso", "role": "admin"})
	superAdmin := env.oidc.token(t, map[string]any{"org": "acme", "role": "super-admin"})

	waitForSync(t, env, superAdmin)

	tests := []struct {
		name       string
		token      string
		wantIn     []string
		wantNotIn  []string
	}{
		{
			name:      "platformWriter sees acme-all and acme-platform",
			token:     platformWriter,
			wantIn:    []string{"acme-all", "acme-platform"},
			wantNotIn: []string{"acme-data"},
		},
		{
			name:      "dataWriter sees acme-all and acme-data",
			token:     dataWriter,
			wantIn:    []string{"acme-all", "acme-data"},
			wantNotIn: []string{"acme-platform"},
		},
		{
			name:      "outsider sees empty list",
			token:     outsider,
			wantIn:    nil,
			wantNotIn: []string{"acme-all", "acme-platform", "acme-data"},
		},
		{
			name:      "superAdmin sees all three",
			token:     superAdmin,
			wantIn:    []string{"acme-all", "acme-platform", "acme-data"},
			wantNotIn: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := doRequest(t, "GET", env.baseURL+"/v1/registries", tt.token, nil)
			body := assertStatus(t, resp, 200)
			names := parseRegistryNames(t, body)
			for _, want := range tt.wantIn {
				assert.Contains(t, names, want, "expected registry %q in list", want)
			}
			for _, notWant := range tt.wantNotIn {
				assert.NotContains(t, names, notWant, "unexpected registry %q in list", notWant)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Test 9: Skill lifecycle (publish, visibility, delete)
// ---------------------------------------------------------------------------

//nolint:paralleltest,tparallel // subtests must run sequentially — publish, visibility, delete depend on ordering
func TestAuthzIntegration_SkillLifecycle(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := setupEnv(t, &config.AuthConfig{
		Mode:  config.AuthModeOAuth,
		Authz: authzRolesConfig(),
	})

	platformWriter := env.oidc.token(t, map[string]any{"org": "acme", "team": "platform", "role": "writer"})
	dataWriter := env.oidc.token(t, map[string]any{"org": "acme", "team": "data", "role": "writer"})
	superAdmin := env.oidc.token(t, map[string]any{"org": "acme", "role": "super-admin"})

	waitForSync(t, env, superAdmin)

	t.Run("publish platform skill", func(t *testing.T) {
		resp := doRequest(t, "POST", env.baseURL+"/v1/entries", platformWriter, publishSkillReq{
			Claims: map[string]any{"org": "acme", "team": "platform"},
			Skill: map[string]any{
				"namespace":   "io.test",
				"name":        "plat-skill",
				"version":     "1.0.0",
				"title":       "Platform Skill",
				"description": "test",
			},
		})
		assertStatus(t, resp, 201)
	})

	t.Run("publish data skill", func(t *testing.T) {
		resp := doRequest(t, "POST", env.baseURL+"/v1/entries", dataWriter, publishSkillReq{
			Claims: map[string]any{"org": "acme", "team": "data"},
			Skill: map[string]any{
				"namespace":   "io.test",
				"name":        "data-skill",
				"version":     "1.0.0",
				"title":       "Data Skill",
				"description": "test",
			},
		})
		assertStatus(t, resp, 201)
	})

	t.Run("platform-writer sees platform skill", func(t *testing.T) {
		url := fmt.Sprintf("%s/registry/acme-all/v0.1/x/dev.toolhive/skills?search=plat-skill", env.baseURL)
		resp := doRequest(t, "GET", url, platformWriter, nil)
		body := assertStatus(t, resp, 200)
		assert.Contains(t, body, "plat-skill")
	})

	t.Run("platform-writer no data skill", func(t *testing.T) {
		url := fmt.Sprintf("%s/registry/acme-all/v0.1/x/dev.toolhive/skills?search=data-skill", env.baseURL)
		resp := doRequest(t, "GET", url, platformWriter, nil)
		body := assertStatus(t, resp, 200)
		var result struct {
			Metadata struct {
				Count int `json:"count"`
			} `json:"metadata"`
		}
		require.NoError(t, json.Unmarshal([]byte(body), &result))
		assert.Equal(t, 0, result.Metadata.Count, "expected 0 skills for data-skill")
	})

	t.Run("data-writer sees data skill", func(t *testing.T) {
		url := fmt.Sprintf("%s/registry/acme-all/v0.1/x/dev.toolhive/skills?search=data-skill", env.baseURL)
		resp := doRequest(t, "GET", url, dataWriter, nil)
		body := assertStatus(t, resp, 200)
		assert.Contains(t, body, "data-skill")
	})

	t.Run("super-admin sees both", func(t *testing.T) {
		url := fmt.Sprintf("%s/registry/acme-all/v0.1/x/dev.toolhive/skills", env.baseURL)
		resp := doRequest(t, "GET", url, superAdmin, nil)
		body := assertStatus(t, resp, 200)
		assert.Contains(t, body, "plat-skill")
		assert.Contains(t, body, "data-skill")
	})

	t.Run("wrong owner cannot delete", func(t *testing.T) {
		resp := doRequest(t, "DELETE", env.baseURL+"/v1/entries/skill/plat-skill/versions/1.0.0", dataWriter, nil)
		assertStatus(t, resp, 403)
	})

	t.Run("owner deletes skill", func(t *testing.T) {
		resp := doRequest(t, "DELETE", env.baseURL+"/v1/entries/skill/plat-skill/versions/1.0.0", platformWriter, nil)
		assertStatus(t, resp, 204)
	})

	t.Run("delete again 404", func(t *testing.T) {
		resp := doRequest(t, "DELETE", env.baseURL+"/v1/entries/skill/plat-skill/versions/1.0.0", platformWriter, nil)
		assertStatus(t, resp, 404)
	})
}

// ---------------------------------------------------------------------------
// Test 10: Source shadowing (first source in registry wins)
// ---------------------------------------------------------------------------

//nolint:paralleltest,tparallel // subtests must run sequentially
func TestAuthzIntegration_SourceShadowing(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	writeFixture := func(name, data string) string {
		f, err := os.CreateTemp(t.TempDir(), name+"-*.json")
		require.NoError(t, err)
		_, err = f.WriteString(data)
		require.NoError(t, err)
		require.NoError(t, f.Close())
		return f.Name()
	}

	highPrioData := `{"version":"1.0.0","meta":{"last_updated":"2025-01-01T00:00:00Z"},"data":{"servers":[` +
		`{"name":"io.test/overlap-tool","version":"1.0.0","description":"from high-prio","packages":[{"registryType":"oci","identifier":"ghcr.io/test/overlap-tool:latest","transport":{"type":"stdio"}}]}` +
		`]}}`
	lowPrioData := `{"version":"1.0.0","meta":{"last_updated":"2025-01-01T00:00:00Z"},"data":{"servers":[` +
		`{"name":"io.test/overlap-tool","version":"1.0.0","description":"from low-prio","packages":[{"registryType":"oci","identifier":"ghcr.io/test/overlap-tool:latest","transport":{"type":"stdio"}}]}` +
		`]}}`

	sources := []config.SourceConfig{
		{
			Name:       "high-prio-src",
			Format:     "upstream",
			File:       &config.FileConfig{Path: writeFixture("high-prio", highPrioData)},
			SyncPolicy: &config.SyncPolicyConfig{Interval: "10s"},
			Claims:     map[string]any{"org": "acme"},
		},
		{
			Name:       "low-prio-src",
			Format:     "upstream",
			File:       &config.FileConfig{Path: writeFixture("low-prio", lowPrioData)},
			SyncPolicy: &config.SyncPolicyConfig{Interval: "10s"},
			Claims:     map[string]any{"org": "acme"},
		},
		{Name: "internal", Managed: &config.ManagedConfig{}},
	}

	registries := []config.RegistryConfig{
		{
			Name:    "shadow-reg",
			Claims:  map[string]any{"org": "acme"},
			Sources: []string{"high-prio-src", "low-prio-src", "internal"},
		},
	}

	env := setupEnvCustom(t, &config.AuthConfig{
		Mode:  config.AuthModeOAuth,
		Authz: authzRolesConfig(),
	}, sources, registries)

	user := env.oidc.token(t, map[string]any{"org": "acme", "role": "writer"})

	waitForSyncSimple(t, env, user, "/registry/shadow-reg/v0.1/servers?search=overlap-tool", "overlap-tool")

	resp := doRequest(t, "GET", env.baseURL+"/registry/shadow-reg/v0.1/servers?search=overlap-tool", user, nil)
	body := assertStatus(t, resp, 200)
	assert.Contains(t, body, "from high-prio")
	assert.NotContains(t, body, "from low-prio")
}

// ---------------------------------------------------------------------------
// Test 11: Array claim values (OR-within-array)
// ---------------------------------------------------------------------------

//nolint:paralleltest,tparallel // subtests must run sequentially
func TestAuthzIntegration_ArrayClaimValues(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	writeFixture := func(name, data string) string {
		f, err := os.CreateTemp(t.TempDir(), name+"-*.json")
		require.NoError(t, err)
		_, err = f.WriteString(data)
		require.NoError(t, err)
		require.NoError(t, f.Close())
		return f.Name()
	}

	multiTeamData := `{"version":"1.0.0","meta":{"last_updated":"2025-01-01T00:00:00Z"},"data":{"servers":[` +
		`{"name":"io.test/multi-team-tool","version":"1.0.0","description":"multi-team tool","packages":[{"registryType":"oci","identifier":"ghcr.io/test/multi-team-tool:latest","transport":{"type":"stdio"}}]}` +
		`]}}`

	sources := []config.SourceConfig{
		{
			Name:       "multi-team-src",
			Format:     "upstream",
			File:       &config.FileConfig{Path: writeFixture("multi-team", multiTeamData)},
			SyncPolicy: &config.SyncPolicyConfig{Interval: "10s"},
			Claims:     map[string]any{"org": "acme", "team": []any{"platform", "data"}},
		},
		{Name: "internal", Managed: &config.ManagedConfig{}},
	}

	registries := []config.RegistryConfig{
		{
			Name:    "array-claims-reg",
			Claims:  map[string]any{"org": "acme"},
			Sources: []string{"multi-team-src", "internal"},
		},
	}

	env := setupEnvCustom(t, &config.AuthConfig{
		Mode:  config.AuthModeOAuth,
		Authz: authzRolesConfig(),
	}, sources, registries)

	superAdmin := env.oidc.token(t, map[string]any{"org": "acme", "role": "super-admin"})

	waitForSyncSimple(t, env, superAdmin, "/registry/array-claims-reg/v0.1/servers?search=multi-team-tool", "multi-team-tool")

	// Entry inherits source claims {"team": ["platform", "data"]} which means
	// the user must have ALL listed values. A user with both teams sees it;
	// a user with only one team does not.
	searchURL := fmt.Sprintf("%s/registry/array-claims-reg/v0.1/servers?search=multi-team-tool", env.baseURL)

	t.Run("user with both teams sees entry", func(t *testing.T) {
		bothTeams := env.oidc.token(t, map[string]any{"org": "acme", "team": []any{"platform", "data"}})
		resp := doRequest(t, "GET", searchURL, bothTeams, nil)
		body := assertStatus(t, resp, 200)
		assert.Contains(t, body, "multi-team-tool")
	})

	t.Run("user with only platform does not see entry", func(t *testing.T) {
		platformOnly := env.oidc.token(t, map[string]any{"org": "acme", "team": "platform"})
		resp := doRequest(t, "GET", searchURL, platformOnly, nil)
		body := assertStatus(t, resp, 200)
		var result struct {
			Metadata struct {
				Count int `json:"count"`
			} `json:"metadata"`
		}
		require.NoError(t, json.Unmarshal([]byte(body), &result))
		assert.Equal(t, 0, result.Metadata.Count, "single-team user should not see multi-team entry")
	})

	t.Run("finance user does not see entry", func(t *testing.T) {
		financeUser := env.oidc.token(t, map[string]any{"org": "acme", "team": "finance"})
		resp := doRequest(t, "GET", searchURL, financeUser, nil)
		body := assertStatus(t, resp, 200)
		var result struct {
			Metadata struct {
				Count int `json:"count"`
			} `json:"metadata"`
		}
		require.NoError(t, json.Unmarshal([]byte(body), &result))
		assert.Equal(t, 0, result.Metadata.Count, "expected 0 servers for finance user")
	})

	t.Run("superAdmin sees entry", func(t *testing.T) {
		resp := doRequest(t, "GET", searchURL, superAdmin, nil)
		body := assertStatus(t, resp, 200)
		assert.Contains(t, body, "multi-team-tool")
	})
}

// ---------------------------------------------------------------------------
// Test 12: Admin entry listing (source and registry entries endpoints)
// ---------------------------------------------------------------------------

//nolint:paralleltest,tparallel // subtests must run sequentially
func TestAuthzIntegration_AdminEntryListing(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := setupEnv(t, &config.AuthConfig{
		Mode:  config.AuthModeOAuth,
		Authz: authzRolesConfig(),
	})

	superAdmin := env.oidc.token(t, map[string]any{"org": "acme", "role": "super-admin"})
	platformAdmin := env.oidc.token(t, map[string]any{"org": "acme", "team": "platform", "role": "admin"})
	platformWriter := env.oidc.token(t, map[string]any{"org": "acme", "team": "platform", "role": "writer"})

	waitForSync(t, env, superAdmin)

	t.Run("source entries listing", func(t *testing.T) {
		resp := doRequest(t, "GET", env.baseURL+"/v1/sources/shared-catalog/entries", platformAdmin, nil)
		body := assertStatus(t, resp, 200)
		assert.Contains(t, body, "entries")
	})

	t.Run("source entries contains expected server", func(t *testing.T) {
		resp := doRequest(t, "GET", env.baseURL+"/v1/sources/shared-catalog/entries", platformAdmin, nil)
		body := assertStatus(t, resp, 200)
		assert.Contains(t, body, "shared-tool-1")
	})

	t.Run("registry entries listing", func(t *testing.T) {
		resp := doRequest(t, "GET", env.baseURL+"/v1/registries/acme-all/entries", platformAdmin, nil)
		body := assertStatus(t, resp, 200)
		assert.Contains(t, body, "entries")
	})

	t.Run("registry entries contains expected server", func(t *testing.T) {
		resp := doRequest(t, "GET", env.baseURL+"/v1/registries/acme-all/entries", platformAdmin, nil)
		body := assertStatus(t, resp, 200)
		assert.Contains(t, body, "shared-tool-1")
	})

	t.Run("writer forbidden from source entries", func(t *testing.T) {
		resp := doRequest(t, "GET", env.baseURL+"/v1/sources/shared-catalog/entries", platformWriter, nil)
		assertStatus(t, resp, 403)
	})

	t.Run("writer can read registries but not registry entries", func(t *testing.T) {
		resp := doRequest(t, "GET", env.baseURL+"/v1/registries/acme-all/entries", platformWriter, nil)
		assertStatus(t, resp, 403)
	})
}

// ---------------------------------------------------------------------------
// Test 13: Multi-version consumer queries
// ---------------------------------------------------------------------------

//nolint:paralleltest,tparallel // subtests must run sequentially — publish, query, delete depend on ordering
func TestAuthzIntegration_MultiVersionConsumerQueries(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := setupEnv(t, &config.AuthConfig{
		Mode:  config.AuthModeOAuth,
		Authz: authzRolesConfig(),
	})

	platformWriter := env.oidc.token(t, map[string]any{"org": "acme", "team": "platform", "role": "writer"})
	dataWriter := env.oidc.token(t, map[string]any{"org": "acme", "team": "data", "role": "writer"})
	superAdmin := env.oidc.token(t, map[string]any{"org": "acme", "role": "super-admin"})

	waitForSync(t, env, superAdmin)

	// Publish two versions of versioned-tool
	t.Run("publish v1.0.0", func(t *testing.T) {
		resp := doRequest(t, "POST", env.baseURL+"/v1/entries", platformWriter, publishReq{
			Claims: map[string]any{"org": "acme", "team": "platform"},
			Server: serverJSONWithVersion("versioned-tool", "1.0.0"),
		})
		assertStatus(t, resp, 201)
	})

	t.Run("publish v2.0.0", func(t *testing.T) {
		resp := doRequest(t, "POST", env.baseURL+"/v1/entries", platformWriter, publishReq{
			Claims: map[string]any{"org": "acme", "team": "platform"},
			Server: serverJSONWithVersion("versioned-tool", "2.0.0"),
		})
		assertStatus(t, resp, 201)
	})

	t.Run("list versions", func(t *testing.T) {
		url := fmt.Sprintf("%s/registry/acme-all/v0.1/servers/io.test%%2Fversioned-tool/versions", env.baseURL)
		resp := doRequest(t, "GET", url, platformWriter, nil)
		body := assertStatus(t, resp, 200)
		assert.Contains(t, body, "1.0.0")
		assert.Contains(t, body, "2.0.0")
	})

	t.Run("get specific version", func(t *testing.T) {
		url := fmt.Sprintf("%s/registry/acme-all/v0.1/servers/io.test%%2Fversioned-tool/versions/1.0.0", env.baseURL)
		resp := doRequest(t, "GET", url, platformWriter, nil)
		body := assertStatus(t, resp, 200)
		assert.Contains(t, body, "1.0.0")
	})

	t.Run("get latest version", func(t *testing.T) {
		url := fmt.Sprintf("%s/registry/acme-all/v0.1/servers/io.test%%2Fversioned-tool/versions/latest", env.baseURL)
		resp := doRequest(t, "GET", url, platformWriter, nil)
		body := assertStatus(t, resp, 200)
		assert.Contains(t, body, "2.0.0")
	})

	t.Run("wrong team cannot see versions", func(t *testing.T) {
		url := fmt.Sprintf("%s/registry/acme-all/v0.1/servers/io.test%%2Fversioned-tool/versions", env.baseURL)
		resp := doRequest(t, "GET", url, dataWriter, nil)
		body := assertStatus(t, resp, 200)
		var result struct {
			Metadata struct {
				Count int `json:"count"`
			} `json:"metadata"`
		}
		require.NoError(t, json.Unmarshal([]byte(body), &result))
		assert.Equal(t, 0, result.Metadata.Count, "data user should not see platform entry versions")
	})

	t.Run("superAdmin sees versions", func(t *testing.T) {
		url := fmt.Sprintf("%s/registry/acme-all/v0.1/servers/io.test%%2Fversioned-tool/versions", env.baseURL)
		resp := doRequest(t, "GET", url, superAdmin, nil)
		body := assertStatus(t, resp, 200)
		assert.Contains(t, body, "1.0.0")
	})

	// Cleanup: delete both versions
	t.Run("cleanup v1.0.0", func(t *testing.T) {
		resp := doRequest(t, "DELETE", env.baseURL+"/v1/entries/server/io.test%2Fversioned-tool/versions/1.0.0", platformWriter, nil)
		assertStatus(t, resp, 204)
	})

	t.Run("cleanup v2.0.0", func(t *testing.T) {
		resp := doRequest(t, "DELETE", env.baseURL+"/v1/entries/server/io.test%2Fversioned-tool/versions/2.0.0", platformWriter, nil)
		assertStatus(t, resp, 204)
	})
}

// ---------------------------------------------------------------------------
// Test 14: Empty claims behavior
// ---------------------------------------------------------------------------

//nolint:paralleltest,tparallel // subtests must run sequentially — publish, query, delete depend on ordering
func TestAuthzIntegration_EmptyClaimsBehavior(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := setupEnv(t, &config.AuthConfig{
		Mode:  config.AuthModeOAuth,
		Authz: authzRolesConfig(),
	})

	platformWriter := env.oidc.token(t, map[string]any{"org": "acme", "team": "platform", "role": "writer"})
	dataWriter := env.oidc.token(t, map[string]any{"org": "acme", "team": "data", "role": "writer"})
	superAdmin := env.oidc.token(t, map[string]any{"org": "acme", "role": "super-admin"})

	waitForSync(t, env, superAdmin)

	// Publish three entries as superAdmin (bypasses all claim checks)
	t.Run("publish open-entry with empty claims", func(t *testing.T) {
		resp := doRequest(t, "POST", env.baseURL+"/v1/entries", superAdmin, publishReq{
			Claims: map[string]any{},
			Server: serverJSON("open-entry"),
		})
		assertStatus(t, resp, 201)
	})

	t.Run("publish acme-entry with org claim", func(t *testing.T) {
		resp := doRequest(t, "POST", env.baseURL+"/v1/entries", superAdmin, publishReq{
			Claims: map[string]any{"org": "acme"},
			Server: serverJSON("acme-entry"),
		})
		assertStatus(t, resp, 201)
	})

	t.Run("publish platform-entry with org and team claims", func(t *testing.T) {
		resp := doRequest(t, "POST", env.baseURL+"/v1/entries", superAdmin, publishReq{
			Claims: map[string]any{"org": "acme", "team": "platform"},
			Server: serverJSON("platform-entry"),
		})
		assertStatus(t, resp, 201)
	})

	// Visibility checks
	// Entries with empty claims {} are stored as NULL in the database (the claims
	// serializer treats empty maps as nil). The read-path filter returns false for
	// NULL record claims (default-deny), so only superAdmin can see them.
	t.Run("platform user does NOT see empty-claims entry", func(t *testing.T) {
		url := fmt.Sprintf("%s/registry/acme-all/v0.1/servers?search=open-entry", env.baseURL)
		resp := doRequest(t, "GET", url, platformWriter, nil)
		body := assertStatus(t, resp, 200)
		var result struct {
			Metadata struct {
				Count int `json:"count"`
			} `json:"metadata"`
		}
		require.NoError(t, json.Unmarshal([]byte(body), &result))
		assert.Equal(t, 0, result.Metadata.Count, "empty-claims entry should be invisible to regular users")
	})

	t.Run("data user does NOT see empty-claims entry", func(t *testing.T) {
		url := fmt.Sprintf("%s/registry/acme-all/v0.1/servers?search=open-entry", env.baseURL)
		resp := doRequest(t, "GET", url, dataWriter, nil)
		body := assertStatus(t, resp, 200)
		var result struct {
			Metadata struct {
				Count int `json:"count"`
			} `json:"metadata"`
		}
		require.NoError(t, json.Unmarshal([]byte(body), &result))
		assert.Equal(t, 0, result.Metadata.Count, "empty-claims entry should be invisible to regular users")
	})

	t.Run("platform user sees acme entry", func(t *testing.T) {
		url := fmt.Sprintf("%s/registry/acme-all/v0.1/servers?search=acme-entry", env.baseURL)
		resp := doRequest(t, "GET", url, platformWriter, nil)
		body := assertStatus(t, resp, 200)
		assert.Contains(t, body, "acme-entry")
	})

	t.Run("platform user sees platform entry", func(t *testing.T) {
		url := fmt.Sprintf("%s/registry/acme-all/v0.1/servers?search=platform-entry", env.baseURL)
		resp := doRequest(t, "GET", url, platformWriter, nil)
		body := assertStatus(t, resp, 200)
		assert.Contains(t, body, "platform-entry")
	})

	t.Run("data user sees acme entry", func(t *testing.T) {
		url := fmt.Sprintf("%s/registry/acme-all/v0.1/servers?search=acme-entry", env.baseURL)
		resp := doRequest(t, "GET", url, dataWriter, nil)
		body := assertStatus(t, resp, 200)
		assert.Contains(t, body, "acme-entry")
	})

	t.Run("data user does NOT see platform entry", func(t *testing.T) {
		url := fmt.Sprintf("%s/registry/acme-all/v0.1/servers?search=platform-entry", env.baseURL)
		resp := doRequest(t, "GET", url, dataWriter, nil)
		body := assertStatus(t, resp, 200)
		var result struct {
			Metadata struct {
				Count int `json:"count"`
			} `json:"metadata"`
		}
		require.NoError(t, json.Unmarshal([]byte(body), &result))
		assert.Equal(t, 0, result.Metadata.Count, "expected 0 servers for data user searching platform-entry")
	})

	t.Run("superAdmin sees all three", func(t *testing.T) {
		url := fmt.Sprintf("%s/registry/acme-all/v0.1/servers?search=-entry", env.baseURL)
		resp := doRequest(t, "GET", url, superAdmin, nil)
		body := assertStatus(t, resp, 200)
		assert.Contains(t, body, "open-entry")
		assert.Contains(t, body, "acme-entry")
		assert.Contains(t, body, "platform-entry")
	})

	// Cleanup: delete all three entries
	t.Run("cleanup open-entry", func(t *testing.T) {
		resp := doRequest(t, "DELETE", env.baseURL+"/v1/entries/server/io.test%2Fopen-entry/versions/1.0.0", superAdmin, nil)
		assertStatus(t, resp, 204)
	})

	t.Run("cleanup acme-entry", func(t *testing.T) {
		resp := doRequest(t, "DELETE", env.baseURL+"/v1/entries/server/io.test%2Facme-entry/versions/1.0.0", superAdmin, nil)
		assertStatus(t, resp, 204)
	})

	t.Run("cleanup platform-entry", func(t *testing.T) {
		resp := doRequest(t, "DELETE", env.baseURL+"/v1/entries/server/io.test%2Fplatform-entry/versions/1.0.0", superAdmin, nil)
		assertStatus(t, resp, 204)
	})
}

// ---------------------------------------------------------------------------
// Test 15: Me endpoint
// ---------------------------------------------------------------------------

//nolint:paralleltest,tparallel // subtests must run sequentially
func TestAuthzIntegration_MeEndpoint(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := setupEnv(t, &config.AuthConfig{
		Mode:  config.AuthModeOAuth,
		Authz: authzRolesConfig(),
	})

	tests := []struct {
		name         string
		extraClaims  map[string]any
		wantStatus   int
		wantContains []string
	}{
		{
			name:         "platform writer",
			extraClaims:  map[string]any{"org": "acme", "team": "platform", "role": "writer"},
			wantStatus:   200,
			wantContains: []string{"manageEntries", "test-user"},
		},
		{
			name:         "platform admin",
			extraClaims:  map[string]any{"org": "acme", "team": "platform", "role": "admin"},
			wantStatus:   200,
			wantContains: []string{"manageSources", "manageRegistries"},
		},
		{
			name:         "super admin",
			extraClaims:  map[string]any{"role": "super-admin"},
			wantStatus:   200,
			wantContains: []string{"superAdmin"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token := env.oidc.token(t, tt.extraClaims)
			resp := doRequest(t, "GET", env.baseURL+"/v1/me", token, nil)
			body := assertStatus(t, resp, tt.wantStatus)
			for _, want := range tt.wantContains {
				assert.Contains(t, body, want)
			}
		})
	}

	t.Run("no roles", func(t *testing.T) {
		token := env.oidc.token(t, map[string]any{"org": "acme"})
		resp := doRequest(t, "GET", env.baseURL+"/v1/me", token, nil)
		body := assertStatus(t, resp, 200)
		assert.Contains(t, body, "subject")

		var result struct {
			Subject string   `json:"subject"`
			Roles   []string `json:"roles"`
		}
		require.NoError(t, json.Unmarshal([]byte(body), &result))
		assert.Empty(t, result.Roles, "expected empty roles for user with no matching role claims")
	})

	t.Run("no auth", func(t *testing.T) {
		resp := doRequest(t, "GET", env.baseURL+"/v1/me", "", nil)
		assertStatus(t, resp, 401)
	})
}
