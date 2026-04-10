//go:build integration

package authz_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/config"
)

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
