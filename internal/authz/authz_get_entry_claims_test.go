//go:build integration

package authz_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/config"
)

// TestAuthzIntegration_GetEntryClaims exercises the GET /v1/entries/{type}/{name}/claims
// endpoint across roles:
//   - manageEntries (writer) and superAdmin can read claims (200)
//   - admin (manageSources + manageRegistries only) is denied (403)
//   - tokens with no matching role are denied (403)
//   - unauthenticated requests are rejected (401)
//
//nolint:paralleltest,tparallel // subtests share state — publish first, then read
func TestAuthzIntegration_GetEntryClaims(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := setupEnv(t, &config.AuthConfig{
		Mode:  config.AuthModeOAuth,
		Authz: authzRolesConfig(),
	})

	platformWriter := env.oidc.token(t, map[string]any{"org": "acme", "team": "platform", "role": "writer"})
	platformAdmin := env.oidc.token(t, map[string]any{"org": "acme", "team": "platform", "role": "admin"})
	superAdmin := env.oidc.token(t, map[string]any{"org": "acme", "role": "super-admin"})
	noRole := env.oidc.token(t, map[string]any{"org": "acme"})

	waitForSync(t, env, superAdmin)

	const claimsPath = "/v1/entries/server/io.test%2Fget-claims-entry/claims"
	publishedClaims := map[string]any{"org": "acme", "team": "platform"}

	t.Run("writer publishes entry", func(t *testing.T) {
		resp := doRequest(t, "POST", env.baseURL+"/v1/entries", platformWriter, publishReq{
			Claims: publishedClaims,
			Server: serverJSON("get-claims-entry"),
		})
		assertStatus(t, resp, 201)
	})

	assertClaims := func(t *testing.T, body string, want map[string]any) {
		t.Helper()
		var resp struct {
			Claims map[string]any `json:"claims"`
		}
		require.NoError(t, json.Unmarshal([]byte(body), &resp))
		assert.Equal(t, want, resp.Claims)
	}

	t.Run("manageEntries reads claims", func(t *testing.T) {
		resp := doRequest(t, "GET", env.baseURL+claimsPath, platformWriter, nil)
		body := assertStatus(t, resp, 200)
		assertClaims(t, body, publishedClaims)
	})

	t.Run("superAdmin reads claims", func(t *testing.T) {
		resp := doRequest(t, "GET", env.baseURL+claimsPath, superAdmin, nil)
		body := assertStatus(t, resp, 200)
		assertClaims(t, body, publishedClaims)
	})

	t.Run("manageSources/manageRegistries denied", func(t *testing.T) {
		resp := doRequest(t, "GET", env.baseURL+claimsPath, platformAdmin, nil)
		assertStatus(t, resp, 403)
	})

	t.Run("token with no role denied", func(t *testing.T) {
		resp := doRequest(t, "GET", env.baseURL+claimsPath, noRole, nil)
		assertStatus(t, resp, 403)
	})

	t.Run("unauthenticated rejected", func(t *testing.T) {
		resp := doRequest(t, "GET", env.baseURL+claimsPath, "", nil)
		assertStatus(t, resp, 401)
	})

	t.Run("not found returns 404", func(t *testing.T) {
		resp := doRequest(t, "GET",
			env.baseURL+"/v1/entries/server/io.test%2Fnon-existent/claims",
			platformWriter, nil)
		assertStatus(t, resp, 404)
	})

	t.Run("invalid entry type returns 400", func(t *testing.T) {
		resp := doRequest(t, "GET",
			env.baseURL+"/v1/entries/widget/io.test%2Fget-claims-entry/claims",
			platformWriter, nil)
		assertStatus(t, resp, 400)
	})

	t.Run("cleanup", func(t *testing.T) {
		resp := doRequest(t, "DELETE",
			env.baseURL+"/v1/entries/server/io.test%2Fget-claims-entry/versions/1.0.0",
			platformWriter, nil)
		assertStatus(t, resp, 204)
	})
}
