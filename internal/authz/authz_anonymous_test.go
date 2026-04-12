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
		internal   bool
	}{
		{"health", "GET", "/health", 200, true},
		{"readiness", "GET", "/readiness", 200, true},
		{"list servers without token", "GET", "/registry/acme-all/v0.1/servers", 200, false},
		{"list sources without token", "GET", "/v1/sources", 200, false},
		{"list registries without token", "GET", "/v1/registries", 200, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			base := env.baseURL
			if tt.internal {
				base = env.internalURL
			}
			resp := doRequest(t, tt.method, base+tt.path, "", nil)
			assertStatus(t, resp, tt.wantStatus)
		})
	}
}
