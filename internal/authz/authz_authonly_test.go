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
