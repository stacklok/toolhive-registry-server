//go:build integration

package authz_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/config"
)

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

	// Use file-data sources (not managed) since the base config already has a
	// managed source and at most one is allowed globally.
	fileDataSource := map[string]any{
		"file":   map[string]any{"data": `{"version":"1.0.0","last_updated":"2025-01-15T10:30:00Z","servers":{}}`},
		"claims": map[string]any{"org": "acme", "team": "platform"},
	}

	t.Run("create API source with matching claims", func(t *testing.T) {
		resp := doRequest(t, "PUT", env.baseURL+"/v1/sources/test-api-src", platformAdmin, fileDataSource)
		assertStatus(t, resp, 201)
	})

	t.Run("create source with non-subset claims", func(t *testing.T) {
		resp := doRequest(t, "PUT", env.baseURL+"/v1/sources/bad-src", platformAdmin, map[string]any{
			"file":   map[string]any{"data": `{"version":"1.0.0","last_updated":"2025-01-15T10:30:00Z","servers":{}}`},
			"claims": map[string]any{"org": "acme", "team": "finance"},
		})
		assertStatus(t, resp, 403)
	})

	t.Run("update API source", func(t *testing.T) {
		resp := doRequest(t, "PUT", env.baseURL+"/v1/sources/test-api-src", platformAdmin, fileDataSource)
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
