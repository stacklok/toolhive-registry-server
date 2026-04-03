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
		Sources:    baseSourceConfigs(t),
		Registries: baseRegistryConfigs(),
		Database:   dbCfg,
		Auth:       authCfg,
	}

	ctx := context.Background()
	app, err := registryapp.NewRegistryApp(ctx,
		registryapp.WithConfig(cfg),
		registryapp.WithAddress(":0"),
		registryapp.WithCoordinatorOptions(coordinator.WithPollingInterval(2*time.Second)),
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
		// Note: shared-catalog may take an extra tick to sync; don't block on it here
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
			// shared-catalog entries are visible to all acme users (no team claim = open)
			// These entries have claims org=acme only, so both team writers should see them
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
