//go:build integration

package authz_test

import (
	"testing"

	"github.com/stacklok/toolhive-registry-server/internal/config"
)

// ---------------------------------------------------------------------------
// Test 1: Anonymous mode (auth=no, authz=no)
// ---------------------------------------------------------------------------

func TestAuthzIntegration_AnonymousMode(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := setupEnv(t, &config.AuthConfig{
		Mode: config.AuthModeAnonymous,
	})

	// Wait for file sources to sync (anonymous mode, no token needed)
	waitForSyncAnonymous(t, env)

	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
	}{
		{"health", "GET", "/health", 200},
		{"readiness", "GET", "/readiness", 200},
		{"list servers without token", "GET", "/registry/acme-all/v0.1/servers", 200},
		{"list sources without token", "GET", "/v1/sources", 200},
		{"list registries without token", "GET", "/v1/registries", 200},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			resp := doRequest(t, tt.method, env.baseURL+tt.path, "", nil)
			assertStatus(t, resp, tt.wantStatus)
		})
	}
}
