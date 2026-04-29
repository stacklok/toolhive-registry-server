//go:build integration

package authz_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/config"
)

// ---------------------------------------------------------------------------
// Test 3: Full authz mode (auth=yes, authz=yes)
// ---------------------------------------------------------------------------

//nolint:paralleltest,tparallel // subtests must run sequentially — publish, visibility, delete depend on ordering
func TestAuthzIntegration_FullAuthzMode(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := setupEnv(t, &config.AuthConfig{
		Mode:  config.AuthModeOAuth,
		Authz: authzRolesConfig(),
	})

	// Persona tokens
	superAdmin := env.oidc.token(t, map[string]any{"org": "acme", "role": "super-admin"})
	platformAdmin := env.oidc.token(t, map[string]any{"org": "acme", "team": "platform", "role": "admin"})
	platformWriter := env.oidc.token(t, map[string]any{"org": "acme", "team": "platform", "role": "writer"})
	dataWriter := env.oidc.token(t, map[string]any{"org": "acme", "team": "data", "role": "writer"})
	outsider := env.oidc.token(t, map[string]any{"org": "contoso", "role": "admin"})

	// Wait for file sources to sync (shared-catalog, platform-tools, data-tools)
	waitForSync(t, env, superAdmin)

	// Section 1: System endpoints
	t.Run("system endpoints", func(t *testing.T) {
		tests := []struct {
			name       string
			path       string
			wantStatus int
			internal   bool
		}{
			{"health", "/health", 200, true},
			{"readiness", "/readiness", 200, true},
			{"oauth-protected-resource", "/.well-known/oauth-protected-resource", 200, false},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				base := env.baseURL
				if tt.internal {
					base = env.internalURL
				}
				resp := doRequest(t, "GET", base+tt.path, "", nil)
				assertStatus(t, resp, tt.wantStatus)
			})
		}
	})

	// Section 2: Unauthenticated access
	t.Run("unauthenticated access", func(t *testing.T) {
		tests := []struct {
			name       string
			path       string
			token      string
			wantStatus int
		}{
			{"servers no token", "/registry/acme-all/v0.1/servers", "", 401},
			{"sources no token", "/v1/sources", "", 401},
			{"registries no token", "/v1/registries", "", 401},
			{"servers malformed token", "/registry/acme-all/v0.1/servers", "not.a.valid.jwt", 401},
			{"servers empty bearer", "/registry/acme-all/v0.1/servers", " ", 401},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				var resp *http.Response
				if tt.token == " " {
					// Send Authorization header with empty Bearer value
					req, err := http.NewRequest("GET", env.baseURL+tt.path, nil)
					require.NoError(t, err)
					req.Header.Set("Authorization", "Bearer ")
					client := &http.Client{Timeout: 10 * time.Second}
					resp, err = client.Do(req)
					require.NoError(t, err)
				} else {
					resp = doRequest(t, "GET", env.baseURL+tt.path, tt.token, nil)
				}
				assertStatus(t, resp, tt.wantStatus)
			})
		}
	})

	// Section 3: Registry access gate
	t.Run("registry access gate", func(t *testing.T) {
		tests := []struct {
			name       string
			registry   string
			token      string
			wantStatus int
		}{
			{"platform-writer acme-all", "acme-all", platformWriter, 200},
			{"data-writer acme-all", "acme-all", dataWriter, 200},
			{"platform-writer acme-platform", "acme-platform", platformWriter, 200},
			{"data-writer acme-platform blocked", "acme-platform", dataWriter, 403},
			{"data-writer acme-data", "acme-data", dataWriter, 200},
			{"platform-writer acme-data blocked", "acme-data", platformWriter, 403},
			{"outsider acme-all blocked", "acme-all", outsider, 403},
			{"outsider acme-platform blocked", "acme-platform", outsider, 403},
			{"super-admin acme-all", "acme-all", superAdmin, 200},
			{"super-admin acme-platform", "acme-platform", superAdmin, 200},
			{"super-admin acme-data", "acme-data", superAdmin, 200},
			{"super-admin does-not-exist", "does-not-exist", superAdmin, 404},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				resp := doRequest(t, "GET", env.baseURL+"/registry/"+tt.registry+"/v0.1/servers", tt.token, nil)
				assertStatus(t, resp, tt.wantStatus)
			})
		}
	})

	// Section 4: Per-user entry filtering
	t.Run("per-user entry filtering", func(t *testing.T) {
		tests := []struct {
			name       string
			registry   string
			search     string
			token      string
			wantStatus int
			wantFound  bool
		}{
			{"platform-writer sees deploy-helper", "acme-all", "deploy-helper", platformWriter, 200, true},
			{"platform-writer sees infra-scanner", "acme-all", "infra-scanner", platformWriter, 200, true},
			{"platform-writer no data-pipeline", "acme-all", "data-pipeline", platformWriter, 200, false},
			{"platform-writer no ml-trainer", "acme-all", "ml-trainer", platformWriter, 200, false},
			{"data-writer sees data-pipeline", "acme-all", "data-pipeline", dataWriter, 200, true},
			{"data-writer sees ml-trainer", "acme-all", "ml-trainer", dataWriter, 200, true},
			{"data-writer no deploy-helper", "acme-all", "deploy-helper", dataWriter, 200, false},
			{"data-writer no infra-scanner", "acme-all", "infra-scanner", dataWriter, 200, false},
			// shared-catalog entries (claims: org=acme, no team) are visible to all acme users
			{"platform-writer sees shared", "acme-all", "shared-tool-1", platformWriter, 200, true},
			{"data-writer sees shared", "acme-all", "shared-tool-1", dataWriter, 200, true},
			{"super-admin sees deploy-helper", "acme-all", "deploy-helper", superAdmin, 200, true},
			{"super-admin sees data-pipeline", "acme-all", "data-pipeline", superAdmin, 200, true},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				url := fmt.Sprintf("%s/registry/%s/v0.1/servers?search=%s", env.baseURL, tt.registry, tt.search)
				resp := doRequest(t, "GET", url, tt.token, nil)
				body := assertStatus(t, resp, tt.wantStatus)
				if tt.wantFound {
					assert.Contains(t, body, tt.search, "expected to find %q in response", tt.search)
				} else {
					var result struct {
						Metadata struct {
							Count int `json:"count"`
						} `json:"metadata"`
					}
					require.NoError(t, json.Unmarshal([]byte(body), &result))
					assert.Equal(t, 0, result.Metadata.Count, "expected 0 servers for %q", tt.search)
				}
			})
		}
	})

	// Section 5: Admin source listing
	t.Run("admin source listing", func(t *testing.T) {
		t.Run("platform-admin lists sources", func(t *testing.T) {
			resp := doRequest(t, "GET", env.baseURL+"/v1/sources", platformAdmin, nil)
			body := assertStatus(t, resp, 200)
			assert.Contains(t, body, "shared-catalog")
		})

		t.Run("platform-admin gets shared-catalog", func(t *testing.T) {
			resp := doRequest(t, "GET", env.baseURL+"/v1/sources/shared-catalog", platformAdmin, nil)
			assertStatus(t, resp, 200)
		})

		t.Run("platform-admin gets platform-tools", func(t *testing.T) {
			resp := doRequest(t, "GET", env.baseURL+"/v1/sources/platform-tools", platformAdmin, nil)
			assertStatus(t, resp, 200)
		})

		t.Run("platform-admin cannot see data-tools", func(t *testing.T) {
			resp := doRequest(t, "GET", env.baseURL+"/v1/sources/data-tools", platformAdmin, nil)
			assertStatus(t, resp, 404)
		})

		t.Run("platform-writer forbidden from sources", func(t *testing.T) {
			resp := doRequest(t, "GET", env.baseURL+"/v1/sources", platformWriter, nil)
			assertStatus(t, resp, 403)
		})

		t.Run("data-writer forbidden from sources", func(t *testing.T) {
			resp := doRequest(t, "GET", env.baseURL+"/v1/sources", dataWriter, nil)
			assertStatus(t, resp, 403)
		})

		t.Run("outsider sees no sources", func(t *testing.T) {
			// Default-deny on empty claims (auth.md §4) plus the tenant-wide
			// {org: acme} claim on every fixture source means a contoso outsider
			// covers nothing. There are no truly-public sources in this fixture.
			resp := doRequest(t, "GET", env.baseURL+"/v1/sources", outsider, nil)
			body := assertStatus(t, resp, 200)
			var result struct {
				Sources []struct {
					Name string `json:"name"`
				} `json:"sources"`
			}
			require.NoError(t, json.Unmarshal([]byte(body), &result))
			assert.Empty(t, result.Sources)
		})

		t.Run("super-admin sees all sources", func(t *testing.T) {
			resp := doRequest(t, "GET", env.baseURL+"/v1/sources", superAdmin, nil)
			body := assertStatus(t, resp, 200)
			assert.Contains(t, body, "data-tools")
		})
	})

	// Section 6: Admin registry listing
	t.Run("admin registry listing", func(t *testing.T) {
		t.Run("platform-admin lists registries", func(t *testing.T) {
			resp := doRequest(t, "GET", env.baseURL+"/v1/registries", platformAdmin, nil)
			body := assertStatus(t, resp, 200)
			assert.Contains(t, body, "acme-all")
		})

		t.Run("platform-admin gets acme-platform", func(t *testing.T) {
			resp := doRequest(t, "GET", env.baseURL+"/v1/registries/acme-platform", platformAdmin, nil)
			assertStatus(t, resp, 200)
		})

		t.Run("platform-admin cannot see acme-data", func(t *testing.T) {
			resp := doRequest(t, "GET", env.baseURL+"/v1/registries/acme-data", platformAdmin, nil)
			assertStatus(t, resp, 404)
		})

		t.Run("platform-writer lists registries", func(t *testing.T) {
			resp := doRequest(t, "GET", env.baseURL+"/v1/registries", platformWriter, nil)
			assertStatus(t, resp, 200)
		})

		t.Run("data-writer lists registries", func(t *testing.T) {
			resp := doRequest(t, "GET", env.baseURL+"/v1/registries", dataWriter, nil)
			assertStatus(t, resp, 200)
		})

		t.Run("super-admin sees all registries", func(t *testing.T) {
			resp := doRequest(t, "GET", env.baseURL+"/v1/registries", superAdmin, nil)
			body := assertStatus(t, resp, 200)
			assert.Contains(t, body, "acme-data")
		})
	})

	// Section 7: Publish
	t.Run("publish", func(t *testing.T) {
		t.Run("platform-writer publishes with matching claims", func(t *testing.T) {
			resp := doRequest(t, "POST", env.baseURL+"/v1/entries", platformWriter, publishReq{
				Claims: map[string]any{"org": "acme", "team": "platform"},
				Server: serverJSON("custom-linter"),
			})
			assertStatus(t, resp, 201)
		})

		t.Run("platform-writer blocked with mismatched claims", func(t *testing.T) {
			resp := doRequest(t, "POST", env.baseURL+"/v1/entries", platformWriter, publishReq{
				Claims: map[string]any{"org": "acme", "team": "finance"},
				Server: serverJSON("sneaky-tool"),
			})
			assertStatus(t, resp, 403)
		})

		t.Run("data-writer publishes with matching claims", func(t *testing.T) {
			resp := doRequest(t, "POST", env.baseURL+"/v1/entries", dataWriter, publishReq{
				Claims: map[string]any{"org": "acme", "team": "data"},
				Server: serverJSON("data-analyzer"),
			})
			assertStatus(t, resp, 201)
		})

		t.Run("outsider blocked from publish", func(t *testing.T) {
			resp := doRequest(t, "POST", env.baseURL+"/v1/entries", outsider, publishReq{
				Claims: map[string]any{"org": "contoso"},
				Server: serverJSON("bad-tool"),
			})
			assertStatus(t, resp, 403)
		})

		t.Run("platform-writer duplicate publish", func(t *testing.T) {
			resp := doRequest(t, "POST", env.baseURL+"/v1/entries", platformWriter, publishReq{
				Claims: map[string]any{"org": "acme", "team": "platform"},
				Server: serverJSON("custom-linter"),
			})
			assertStatus(t, resp, 409)
		})
	})

	// Section 8: Published entry visibility
	t.Run("published entry visibility", func(t *testing.T) {
		tests := []struct {
			name      string
			search    string
			token     string
			wantFound bool
		}{
			{"platform-writer sees custom-linter", "custom-linter", platformWriter, true},
			{"data-writer no custom-linter", "custom-linter", dataWriter, false},
			{"data-writer sees data-analyzer", "data-analyzer", dataWriter, true},
			{"platform-writer no data-analyzer", "data-analyzer", platformWriter, false},
			{"super-admin sees custom-linter", "custom-linter", superAdmin, true},
			{"super-admin sees data-analyzer", "data-analyzer", superAdmin, true},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				url := fmt.Sprintf("%s/registry/acme-all/v0.1/servers?search=%s", env.baseURL, tt.search)
				resp := doRequest(t, "GET", url, tt.token, nil)
				body := assertStatus(t, resp, 200)
				if tt.wantFound {
					assert.Contains(t, body, tt.search)
				} else {
					var result struct {
						Metadata struct {
							Count int `json:"count"`
						} `json:"metadata"`
					}
					require.NoError(t, json.Unmarshal([]byte(body), &result))
					assert.Equal(t, 0, result.Metadata.Count)
				}
			})
		}
	})

	// Section 9: Delete
	t.Run("delete", func(t *testing.T) {
		t.Run("data-writer cannot delete custom-linter", func(t *testing.T) {
			resp := doRequest(t, "DELETE", env.baseURL+"/v1/entries/server/io.test%2Fcustom-linter/versions/1.0.0", dataWriter, nil)
			assertStatus(t, resp, 403)
		})

		t.Run("platform-writer deletes custom-linter", func(t *testing.T) {
			resp := doRequest(t, "DELETE", env.baseURL+"/v1/entries/server/io.test%2Fcustom-linter/versions/1.0.0", platformWriter, nil)
			assertStatus(t, resp, 204)
		})

		t.Run("super-admin deletes data-analyzer", func(t *testing.T) {
			resp := doRequest(t, "DELETE", env.baseURL+"/v1/entries/server/io.test%2Fdata-analyzer/versions/1.0.0", superAdmin, nil)
			assertStatus(t, resp, 204)
		})

		t.Run("platform-writer delete already deleted", func(t *testing.T) {
			resp := doRequest(t, "DELETE", env.baseURL+"/v1/entries/server/io.test%2Fcustom-linter/versions/1.0.0", platformWriter, nil)
			assertStatus(t, resp, 404)
		})
	})

	// Section 10: Config-managed protection
	t.Run("config-managed protection", func(t *testing.T) {
		t.Run("cannot delete config source", func(t *testing.T) {
			resp := doRequest(t, "DELETE", env.baseURL+"/v1/sources/shared-catalog", platformAdmin, nil)
			assertStatus(t, resp, 403)
		})

		t.Run("cannot update config source", func(t *testing.T) {
			resp := doRequest(t, "PUT", env.baseURL+"/v1/sources/shared-catalog", platformAdmin, map[string]any{
				"managed": map[string]any{},
				"claims":  map[string]any{"org": "acme"},
			})
			assertStatus(t, resp, 403)
		})

		t.Run("cannot delete config registry", func(t *testing.T) {
			resp := doRequest(t, "DELETE", env.baseURL+"/v1/registries/acme-all", platformAdmin, nil)
			assertStatus(t, resp, 403)
		})
	})
}
