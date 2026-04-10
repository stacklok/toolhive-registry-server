//go:build integration

package authz_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/stacklok/toolhive-registry-server/internal/config"
)

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
		name        string
		token       string
		wantVisible []string
		wantNotIn   []string
	}{
		{
			name:        "platformWriter sees acme-all and acme-platform",
			token:       platformWriter,
			wantVisible: []string{"acme-all", "acme-platform"},
			wantNotIn:   []string{"acme-data"},
		},
		{
			name:        "dataWriter sees acme-all and acme-data",
			token:       dataWriter,
			wantVisible: []string{"acme-all", "acme-data"},
			wantNotIn:   []string{"acme-platform"},
		},
		{
			name:        "outsider sees empty list",
			token:       outsider,
			wantVisible: nil,
			wantNotIn:   []string{"acme-all", "acme-platform", "acme-data"},
		},
		{
			name:        "superAdmin sees all three",
			token:       superAdmin,
			wantVisible: []string{"acme-all", "acme-platform", "acme-data"},
			wantNotIn:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := doRequest(t, "GET", env.baseURL+"/v1/registries", tt.token, nil)
			body := assertStatus(t, resp, 200)
			names := parseRegistryNames(t, body)
			for _, want := range tt.wantVisible {
				assert.Contains(t, names, want, "expected registry %q in list", want)
			}
			for _, notWant := range tt.wantNotIn {
				assert.NotContains(t, names, notWant, "unexpected registry %q in list", notWant)
			}
		})
	}
}
