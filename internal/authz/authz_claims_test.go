//go:build integration

package authz_test

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/config"
)

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
			Name: "multi-team-src",
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

	// Publishing with empty claims is rejected when auth is enabled — empty
	// claims would create entries invisible to everyone except super-admins.
	t.Run("publish with empty claims rejected", func(t *testing.T) {
		resp := doRequest(t, "POST", env.baseURL+"/v1/entries", superAdmin, publishReq{
			Claims: map[string]any{},
			Server: serverJSON("open-entry"),
		})
		assertStatus(t, resp, 400)
	})

	t.Run("publish without claims rejected", func(t *testing.T) {
		resp := doRequest(t, "POST", env.baseURL+"/v1/entries", superAdmin, publishReq{
			Server: serverJSON("open-entry"),
		})
		assertStatus(t, resp, 400)
	})

	// Publish two entries with proper claims
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

	t.Run("superAdmin sees both entries", func(t *testing.T) {
		url := fmt.Sprintf("%s/registry/acme-all/v0.1/servers?search=-entry", env.baseURL)
		resp := doRequest(t, "GET", url, superAdmin, nil)
		body := assertStatus(t, resp, 200)
		assert.Contains(t, body, "acme-entry")
		assert.Contains(t, body, "platform-entry")
	})

	// Cleanup
	t.Run("cleanup acme-entry", func(t *testing.T) {
		resp := doRequest(t, "DELETE", env.baseURL+"/v1/entries/server/io.test%2Facme-entry/versions/1.0.0", superAdmin, nil)
		assertStatus(t, resp, 204)
	})

	t.Run("cleanup platform-entry", func(t *testing.T) {
		resp := doRequest(t, "DELETE", env.baseURL+"/v1/entries/server/io.test%2Fplatform-entry/versions/1.0.0", superAdmin, nil)
		assertStatus(t, resp, 204)
	})
}
