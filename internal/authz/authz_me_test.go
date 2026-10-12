//go:build integration

package authz_test

import (
	"testing"

	"github.com/stacklok/toolhive-registry-server/internal/config"
)

func TestAuthzIntegration_MeEndpoint(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	env := setupEnv(t, &config.AuthConfig{Mode: config.AuthModeOAuth, Authz: authzRolesConfig()})
	for _, tc := range []struct {
		name   string
		claims map[string]any
		status int
	}{
		{"writer", map[string]any{"org": "acme", "role": "writer"}, 410},
		{"admin", map[string]any{"org": "acme", "role": "admin"}, 410},
		{"super admin", map[string]any{"role": "super-admin"}, 410},
		{"no role", map[string]any{"org": "acme"}, 410},
		{"unauthenticated", nil, 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			token := ""
			if tc.claims != nil {
				token = env.oidc.token(t, tc.claims)
			}
			resp := doRequest(t, "GET", env.baseURL+"/v1/me", token, nil)
			assertStatus(t, resp, tc.status)
		})
	}
}
