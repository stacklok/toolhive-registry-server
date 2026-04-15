//go:build integration

package authz_test

import (
	"testing"

	"github.com/stacklok/toolhive-registry-server/internal/config"
)

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
		internal   bool
	}{
		{"health no token", "GET", "/health", "", 200, true},
		{"readiness no token", "GET", "/readiness", "", 200, true},
		{"servers no token", "GET", "/registry/acme-all/v0.1/servers", "", 401, false},
		{"sources no token", "GET", "/v1/sources", "", 401, false},
		{"registries no token", "GET", "/v1/registries", "", 401, false},
		{"servers valid token", "GET", "/registry/acme-all/v0.1/servers", validToken, 200, false},
		{"sources valid token", "GET", "/v1/sources", validToken, 200, false},
		{"registries valid token", "GET", "/v1/registries", validToken, 200, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			base := env.baseURL
			if tt.internal {
				base = env.internalURL
			}
			resp := doRequest(t, tt.method, base+tt.path, tt.token, nil)
			assertStatus(t, resp, tt.wantStatus)
		})
	}
}
