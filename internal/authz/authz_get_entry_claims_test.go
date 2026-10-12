//go:build integration

package authz_test

import (
	"testing"

	"github.com/stacklok/toolhive-registry-server/internal/config"
)

// Both retired claims methods are authenticated static tombstones. Claims and
// mutation protection remain tested at the service/storage and publication seams.
func TestAuthzIntegration_GetEntryClaims(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	env := setupEnv(t, &config.AuthConfig{Mode: config.AuthModeOAuth, Authz: authzRolesConfig()})
	for _, tc := range []struct {
		name, method, path string
		claims             map[string]any
		status             int
	}{
		{"writer read", "GET", "server/test", map[string]any{"org": "acme", "role": "writer"}, 410},
		{"writer retag", "PUT", "server/test", map[string]any{"org": "acme", "role": "writer"}, 410},
		{"super admin retag", "PUT", "server/test", map[string]any{"role": "super-admin"}, 410},
		{"cross team read", "GET", "server/test", map[string]any{"team": "other", "role": "writer"}, 410},
		{"no role read", "GET", "server/test", map[string]any{"org": "acme"}, 410},
		{"nonexistent read", "GET", "server/missing", map[string]any{"org": "acme", "role": "writer"}, 410},
		{"invalid kind read", "GET", "widget/test", map[string]any{"org": "acme", "role": "writer"}, 410},
		{"unauthenticated read", "GET", "server/test", nil, 401},
		{"unauthenticated retag", "PUT", "server/test", nil, 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			token := ""
			if tc.claims != nil {
				token = env.oidc.token(t, tc.claims)
			}
			resp := doRequest(t, tc.method, env.baseURL+"/v1/entries/"+tc.path+"/claims", token, map[string]any{"claims": map[string]any{}})
			assertStatus(t, resp, tc.status)
		})
	}
}
