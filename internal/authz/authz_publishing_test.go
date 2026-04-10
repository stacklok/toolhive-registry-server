//go:build integration

package authz_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/config"
)

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
