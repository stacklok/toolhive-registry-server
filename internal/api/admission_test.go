package api

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/internal/auth"
	"github.com/stacklok/toolhive-registry-server/internal/authorization"
	readermocks "github.com/stacklok/toolhive-registry-server/internal/authorization/mocks"
	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/catalog"
	adminmocks "github.com/stacklok/toolhive-registry-server/pkg/registry/catalog/mocks"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/httpapi"
)

func admissionTestGate(t *testing.T, policy string) *authorization.Registrar {
	t.Helper()
	path := filepath.Join(t.TempDir(), "policy.cedar")
	require.NoError(t, os.WriteFile(path, []byte(policy), 0600))
	engine, err := authorization.Load(path)
	require.NoError(t, err)
	gate, err := authorization.NewRegistrar(engine, nil, nil)
	require.NoError(t, err)
	return gate
}

func admissionTestAuth(t *testing.T) (func(http.Handler) http.Handler, *config.AuthConfig) {
	t.Helper()
	var issuer string
	provider := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"active": r.Form.Get("token") == "valid", "iss": issuer, "sub": "operator", "roles": []string{"superAdmin", "manageSources", "manageRegistries", "manageEntries"}, "aud": "registry", "exp": time.Now().Add(time.Hour).Unix()}))
	}))
	t.Cleanup(provider.Close)
	issuer = provider.URL
	ca := filepath.Join(t.TempDir(), "ca.pem")
	require.NoError(t, os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: provider.Certificate().Raw}), 0600))
	cfg := &config.AuthConfig{Mode: config.AuthModeOAuth, InsecureAllowHTTP: true, OAuth: &config.OAuthConfig{Providers: []config.OAuthProviderConfig{{Name: "test", IssuerURL: issuer, Audience: "registry", JwksUrl: issuer + "/jwks", IntrospectionURL: issuer + "/introspect", ClientID: "test", CACertPath: ca, AllowPrivateIP: true}}}}
	cfg.OAuth.Realm, cfg.OAuth.ResourceURL = "admission-test", "https://registry.example.org"
	mw, _, err := auth.NewCatalogAuthMiddleware(context.Background(), cfg, true, auth.DefaultValidatorFactory)
	require.NoError(t, err)
	return mw, cfg
}

func admissionCapabilities(t *testing.T) (catalog.Reader, httpapi.AdminOperations) {
	t.Helper()
	ctrl := gomock.NewController(t)
	diagnostics, err := catalog.NewDiagnostics(adminmocks.NewMockEntryDiagnostics(ctrl), adminmocks.NewMockSourceStatusReader(ctrl))
	require.NoError(t, err)
	return readermocks.NewMockReader(ctrl), httpapi.AdminOperations{Sources: adminmocks.NewMockSources(ctrl), Views: adminmocks.NewMockViews(ctrl), Publisher: adminmocks.NewMockManagedPublisher(ctrl), Diagnostics: diagnostics}
}

const admissionTestPermit = `permit(principal is Registry::Principal, action, resource == Registry::Instance::"catalog");`

func TestCatalogAdmissionActualRoutesAndRolesNeverGrant(t *testing.T) {
	t.Parallel()
	mw, _ := admissionTestAuth(t)
	for _, admin := range []bool{false, true} {
		reader, operations := admissionCapabilities(t)
		router, err := NewCatalogServer(reader, operations, admissionTestGate(t, `forbid(principal, action, resource);`), admin, mw)
		require.NoError(t, err)
		want := map[string]bool{
			"GET /openapi.json":                                                         true,
			"GET /registry/{registryName}/v0.1/servers":                                 true,
			"GET /registry/{registryName}/v0.1/servers/{serverName}/versions":           true,
			"GET /registry/{registryName}/v0.1/servers/{serverName}/versions/{version}": true,
		}
		for _, family := range []string{"skills", "plugins"} {
			for _, suffix := range []string{"", "/", "/{namespace}/{name}", "/{namespace}/{name}/versions", "/{namespace}/{name}/versions/{version}"} {
				want["GET /registry/{registryName}/v0.1/x/dev.toolhive/"+family+suffix] = true
			}
		}
		if admin {
			for _, family := range []string{"sources", "registries"} {
				for _, route := range []string{"GET /v1/" + family, "GET /v1/" + family + "/{name}", "GET /v1/" + family + "/{name}/entries", "PUT /v1/" + family + "/{name}", "DELETE /v1/" + family + "/{name}"} {
					want[route] = true
				}
			}
			for _, route := range []string{"POST /v1/entries", "DELETE /v1/entries/{type}/{name}/versions/{version}", "GET /v1/me", "GET /v1/entries/{type}/{name}/claims", "PUT /v1/entries/{type}/{name}/claims"} {
				want[route] = true
			}
		}
		actual := map[string]bool{}
		require.NoError(t, chi.Walk(router, func(method, path string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
			actual[method+" "+path] = true
			return nil
		}))
		require.Equal(t, want, actual)
		for route := range want {
			method, template, _ := strings.Cut(route, " ")
			path := strings.NewReplacer("{registryName}", "default", "{serverName}", "io.example%2Fserver", "{namespace}", "example", "{name}", "test", "{version}", "1.0.0", "{type}", "skill").Replace(template)
			body := ""
			switch {
			case method == "POST":
				body = `{"skill":{"namespace":"com.example","name":"test","version":"1","description":"published"}}`
			case method == "PUT" && strings.HasPrefix(template, "/v1/sources/"):
				body = `{"managed":{}}`
			case method == "PUT" && strings.HasPrefix(template, "/v1/registries/"):
				body = `{"sources":["test"]}`
			}
			request := httptest.NewRequest(method, path, strings.NewReader(body))
			request.Header.Set("Authorization", "Bearer valid")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if template == "/v1/me" || strings.HasSuffix(template, "/claims") {
				require.Equal(t, 410, response.Code, path)
			} else {
				require.Equal(t, 403, response.Code, path)
			}
		}
		for _, tc := range []struct {
			method, path string
			code         int
		}{
			{"GET", "/unknown", 404}, {"POST", "/registry/default/v0.1/servers", 405}, {"HEAD", "/registry/default/v0.1/servers", 405}, {"OPTIONS", "/registry/default/v0.1/servers", 405}, {"GET", "/health", 404}, {"GET", "/registry//default/v0.1/servers", 404}, {"GET", "/registry/default/v0.1/not%2Fservers", 404},
		} {
			response := httptest.NewRecorder()
			request := httptest.NewRequest(tc.method, tc.path, nil)
			request.Header.Set("X-HTTP-Method-Override", "GET")
			router.ServeHTTP(response, request)
			require.Equal(t, tc.code, response.Code)
		}
		if !admin {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest("GET", "/v1/sources", nil))
			require.Equal(t, 404, response.Code)
		}
	}
}

func TestCatalogAdmissionUnauthorizedChallenges(t *testing.T) {
	t.Parallel()
	mw, cfg := admissionTestAuth(t)
	for _, tc := range []struct {
		name       string
		headers    []string
		badMapping bool
		code       string
	}{
		{name: "missing admin credentials", code: "invalid_request"},
		{name: "duplicate credentials", headers: []string{"Bearer valid", "Bearer valid"}, code: "invalid_request"},
		{name: "oversized credentials", headers: []string{"Bearer " + strings.Repeat("x", 65536)}, code: "invalid_request"},
		{name: "malformed credentials", headers: []string{"Basic invalid"}, code: "invalid_request"},
		{name: "invalid token", headers: []string{"Bearer invalid"}, code: "invalid_token"},
		{name: "invalid mapped attributes", headers: []string{"Bearer valid"}, badMapping: true, code: "invalid_token"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gate := admissionTestGate(t, admissionTestPermit)
			if tc.badMapping {
				path := filepath.Join(t.TempDir(), "policy.cedar")
				require.NoError(t, os.WriteFile(path, []byte(admissionTestPermit), 0600))
				engine, err := authorization.Load(path)
				require.NoError(t, err)
				gate, err = authorization.NewRegistrar(engine, []config.IdentityMapping{{Issuer: cfg.OAuth.Providers[0].IssuerURL, Groups: &config.AttributeMapping{Claim: "sub", Format: "string-array"}}}, nil)
				require.NoError(t, err)
			}
			reader, operations := admissionCapabilities(t)
			router, err := NewCatalogServer(reader, operations, gate, true, mw)
			require.NoError(t, err)
			for _, route := range admissionManifest(true) {
				if len(tc.headers) == 0 && !strings.HasPrefix(route.template, "/v1/") {
					continue
				}
				if tc.badMapping && route.retired {
					continue
				}
				path := strings.NewReplacer("{registryName}", "default", "{serverName}", "test", "{namespace}", "example", "{name}", "test", "{version}", "latest", "{type}", "server").Replace(route.template)
				request := httptest.NewRequest(route.method, path, nil)
				if tc.headers != nil {
					request.Header["Authorization"] = tc.headers
				}
				response := httptest.NewRecorder()
				router.ServeHTTP(response, request)
				require.Equal(t, 401, response.Code, path)
				require.Contains(t, response.Header().Get("WWW-Authenticate"), `Bearer realm="admission-test"`, path)
				require.Contains(t, response.Header().Get("WWW-Authenticate"), `error="`+tc.code+`"`, path)
				require.Contains(t, response.Header().Get("WWW-Authenticate"), `resource_metadata="https://registry.example.org/.well-known/oauth-protected-resource"`, path)
			}
		})
	}
}
