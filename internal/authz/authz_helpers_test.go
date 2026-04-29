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
	"log/slog"
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
			Name: "shared-catalog",
			File:       &config.FileConfig{Path: writeFixture("shared-catalog", sharedData)},
			SyncPolicy: &config.SyncPolicyConfig{Interval: "10s"},
			Claims:     map[string]any{"org": "acme"},
		},
		{
			Name: "platform-tools",
			File:       &config.FileConfig{Path: writeFixture("platform-tools", platformData)},
			SyncPolicy: &config.SyncPolicyConfig{Interval: "10s"},
			Claims:     map[string]any{"org": "acme", "team": "platform"},
		},
		{
			Name: "data-tools",
			File:       &config.FileConfig{Path: writeFixture("data-tools", dataToolsData)},
			SyncPolicy: &config.SyncPolicyConfig{Interval: "10s"},
			Claims:     map[string]any{"org": "acme", "team": "data"},
		},
		// Tag the managed "internal" source with a tenant-wide claim so any acme
		// caller can reference it. Default-deny on empty claims (auth.md §4)
		// would otherwise make this source unreferenceable for everyone but
		// super-admin.
		{Name: "internal", Claims: map[string]any{"org": "acme"}, Managed: &config.ManagedConfig{}},
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

	return &config.DatabaseConfig{
		Host:     host,
		Port:     port,
		User:     user,
		Password: password,
		Database: dbName,
		SSLMode:  "disable",
	}
}

// ---------------------------------------------------------------------------
// Test environment
// ---------------------------------------------------------------------------

type testEnv struct {
	baseURL     string
	internalURL string
	oidc        *mockOIDCServer
}

func setupEnv(t *testing.T, authCfg *config.AuthConfig) *testEnv {
	t.Helper()
	return setupEnvCustom(t, authCfg, baseSourceConfigs(t), baseRegistryConfigs())
}

func setupEnvCustom(t *testing.T, authCfg *config.AuthConfig, sources []config.SourceConfig, registries []config.RegistryConfig) *testEnv {
	t.Helper()

	// Silence application slog output — the server emits many INFO messages
	// (startup, sync, HTTP requests) that clutter test output without adding
	// diagnostic value.
	orig := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(func() { slog.SetDefault(orig) })

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
		registryapp.WithInternalAddress(":0"),
		registryapp.WithCoordinatorOptions(coordinator.TestingWithPollingInterval(500*time.Millisecond)),
	)
	require.NoError(t, err)

	// Get random free ports for both the main and internal servers.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := listener.Addr().String()
	listener.Close()

	internalListener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	internalAddr := internalListener.Addr().String()
	internalListener.Close()

	app.GetHTTPServer().Addr = addr
	app.GetInternalHTTPServer().Addr = internalAddr

	errCh := make(chan error, 1)
	go func() { errCh <- app.Start() }()

	t.Cleanup(func() {
		_ = app.Stop(5 * time.Second)
	})

	baseURL := "http://" + addr
	internalURL := "http://" + internalAddr
	waitForReady(t, internalURL)

	return &testEnv{
		baseURL:     baseURL,
		internalURL: internalURL,
		oidc:        oidcServer,
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

// ---------------------------------------------------------------------------
// Sync helpers
// ---------------------------------------------------------------------------

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
// Request body types
// ---------------------------------------------------------------------------

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

type publishReq struct {
	Claims map[string]any         `json:"claims,omitempty"`
	Server *upstreamv0.ServerJSON `json:"server,omitempty"`
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
