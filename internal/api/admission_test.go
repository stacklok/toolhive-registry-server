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
	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/service"
	mocksvc "github.com/stacklok/toolhive-registry-server/internal/service/mocks"
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
		active := r.Form.Get("token") == "valid"
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"active": active, "iss": issuer, "sub": "operator", "aud": "registry", "exp": time.Now().Add(time.Hour).Unix()}))
	}))
	t.Cleanup(provider.Close)
	issuer = provider.URL
	ca := filepath.Join(t.TempDir(), "ca.pem")
	require.NoError(t, os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: provider.Certificate().Raw}), 0600))
	cfg := &config.AuthConfig{Mode: config.AuthModeOAuth, InsecureAllowHTTP: true, OAuth: &config.OAuthConfig{Providers: []config.OAuthProviderConfig{{Name: "test", IssuerURL: issuer, Audience: "registry", JwksUrl: issuer + "/jwks", IntrospectionURL: issuer + "/introspect", ClientID: "test", ClientSecretFile: "", CACertPath: ca, AllowPrivateIP: true}}}}
	cfg.OAuth.Realm = "admission-test"
	cfg.OAuth.ResourceURL = "https://registry.example.org"
	// ToolHive's authenticated RFC7662 client is selected by ClientID; no secret
	// or real provider is needed for the local introspection fixture.
	mw, _, err := auth.NewAdmissionAuthMiddleware(context.Background(), cfg, true, auth.DefaultValidatorFactory)
	require.NoError(t, err)
	return mw, cfg
}

const admissionTestPermit = `permit(principal is Registry::Principal, action, resource == Registry::Instance::"catalog");`

func TestAdmissionManifestActualRoutes(t *testing.T) {
	t.Parallel()
	mw, cfg := admissionTestAuth(t)
	for _, admin := range []bool{false, true} {
		raw := mocksvc.NewMockRegistryService(gomock.NewController(t))
		router, err := NewAdmissionServer(raw, admissionTestGate(t, `forbid(principal, action, resource);`), admin, mw, WithAuthConfig(cfg))
		require.NoError(t, err)
		expected := map[string]bool{}
		for _, route := range admissionManifest(admin) {
			expected[route.method+" "+route.template] = true
		}
		require.NoError(t, chi.Walk(router, func(method, path string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
			require.True(t, expected[method+" "+path])
			delete(expected, method+" "+path)
			return nil
		}))
		require.Empty(t, expected)
		for _, route := range admissionManifest(admin) {
			path := strings.NewReplacer("{registryName}", "default", "{serverName}", "io.example%2Fserver", "{namespace}", "example", "{name}", "test", "{version}", "latest", "{type}", "server").Replace(route.template)
			request := httptest.NewRequest(route.method, path, strings.NewReader(`{"server":{}}`))
			request.Header.Set("Authorization", "Bearer valid")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if route.retired {
				require.Equal(t, 410, response.Code, path)
			} else {
				require.Equal(t, 403, response.Code, path)
			}
		}
		for _, tc := range []struct {
			method, path string
			status       int
		}{
			{"GET", "/unknown", 404}, {"POST", "/registry/default/v0.1/servers", 405},
			{"HEAD", "/registry/default/v0.1/servers", 405}, {"OPTIONS", "/registry/default/v0.1/servers", 405},
			{"GET", "/health", 404}, {"GET", "/registry//default/v0.1/servers", 404},
			{"GET", "/registry/default/v0.1/not%2Fservers", 404},
		} {
			response := httptest.NewRecorder()
			request := httptest.NewRequest(tc.method, tc.path, nil)
			request.Header.Set("X-HTTP-Method-Override", "GET")
			router.ServeHTTP(response, request)
			require.Equal(t, tc.status, response.Code)
		}
		if !admin {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest("GET", "/v1/sources", nil))
			require.Equal(t, 404, response.Code)
		}
	}
}

func TestAdmissionConsumerDispatchAndLegacyDenial(t *testing.T) {
	t.Parallel()
	mw, cfg := admissionTestAuth(t)
	for _, tc := range []struct{ path, operation string }{
		{"/registry/default/v0.1/servers", "ListServers"},
		{"/registry/default/v0.1/servers/io.example%2Ftest/versions", "ListServerVersions"},
		{"/registry/default/v0.1/servers/io.example%2Ftest/versions/latest", "GetServerVersion"},
		{"/registry/default/v0.1/x/dev.toolhive/skills/", "ListSkills"},
		{"/registry/default/v0.1/x/dev.toolhive/skills/example/test", "GetSkillVersion"},
		{"/registry/default/v0.1/x/dev.toolhive/skills/example/test/versions", "ListSkills"},
		{"/registry/default/v0.1/x/dev.toolhive/skills/example/test/versions/1.0.0", "GetSkillVersion"},
		{"/registry/default/v0.1/x/dev.toolhive/plugins/", "ListPlugins"},
		{"/registry/default/v0.1/x/dev.toolhive/plugins/example/test", "GetPluginVersion"},
		{"/registry/default/v0.1/x/dev.toolhive/plugins/example/test/versions", "ListPlugins"},
		{"/registry/default/v0.1/x/dev.toolhive/plugins/example/test/versions/latest", "GetPluginVersion"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()
			raw := mocksvc.NewMockRegistryService(gomock.NewController(t))
			// The old service's rejection must survive Cedar Allow unchanged.
			switch tc.operation {
			case "ListServers":
				raw.EXPECT().ListServers(gomock.Any(), gomock.Any()).Return(nil, service.ErrClaimsInsufficient)
			case "ListServerVersions":
				raw.EXPECT().ListServerVersions(gomock.Any(), gomock.Any()).Return(nil, service.ErrClaimsInsufficient)
			case "GetServerVersion":
				raw.EXPECT().GetServerVersion(gomock.Any(), gomock.Any()).Return(nil, service.ErrClaimsInsufficient)
			case "ListSkills":
				raw.EXPECT().ListSkills(gomock.Any(), gomock.Any()).Return(nil, service.ErrClaimsInsufficient)
			case "GetSkillVersion":
				raw.EXPECT().GetSkillVersion(gomock.Any(), gomock.Any()).Return(nil, service.ErrClaimsInsufficient)
			case "ListPlugins":
				raw.EXPECT().ListPlugins(gomock.Any(), gomock.Any()).Return(nil, service.ErrClaimsInsufficient)
			case "GetPluginVersion":
				raw.EXPECT().GetPluginVersion(gomock.Any(), gomock.Any()).Return(nil, service.ErrClaimsInsufficient)
			}
			router, err := NewAdmissionServer(raw, admissionTestGate(t, admissionTestPermit), false, mw, WithAuthConfig(cfg))
			require.NoError(t, err)
			request := httptest.NewRequest("GET", tc.path, nil)
			request.Header.Set("Authorization", "Bearer valid")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			require.Equal(t, 403, response.Code)
			require.Empty(t, response.Header().Get("WWW-Authenticate"))
		})
	}
}

func TestAdmissionUnauthorizedChallenges(t *testing.T) {
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
				gate, err = authorization.NewRegistrar(engine, []config.IdentityMapping{{
					Issuer: cfg.OAuth.Providers[0].IssuerURL,
					Groups: &config.AttributeMapping{Claim: "sub", Format: "string-array"},
				}}, nil)
				require.NoError(t, err)
			}
			raw := mocksvc.NewMockRegistryService(gomock.NewController(t))
			router, err := NewAdmissionServer(raw, gate, true, mw, WithAuthConfig(cfg))
			require.NoError(t, err)
			for _, route := range admissionManifest(true) {
				if len(tc.headers) == 0 && !strings.HasPrefix(route.template, "/v1/") {
					continue // Anonymous catalog evidence is intentional.
				}
				if tc.badMapping && route.retired {
					continue // Tombstones require authentication, not Cedar attributes.
				}
				path := strings.NewReplacer("{registryName}", "default", "{serverName}", "test", "{namespace}", "example", "{name}", "test", "{version}", "latest", "{type}", "server").Replace(route.template)
				request := httptest.NewRequest(route.method, path, nil)
				if tc.headers != nil {
					request.Header["Authorization"] = tc.headers
				}
				response := httptest.NewRecorder()
				router.ServeHTTP(response, request)
				require.Equal(t, http.StatusUnauthorized, response.Code, path)
				require.Contains(t, response.Header().Get("WWW-Authenticate"), `Bearer realm="admission-test"`, path)
				require.Contains(t, response.Header().Get("WWW-Authenticate"), `error="`+tc.code+`"`, path)
				require.Contains(t, response.Header().Get("WWW-Authenticate"), `resource_metadata="https://registry.example.org/.well-known/oauth-protected-resource"`, path)
			}
		})
	}
}

func TestAdmissionWiringAndCredentials(t *testing.T) {
	t.Parallel()
	mw, cfg := admissionTestAuth(t)
	raw := mocksvc.NewMockRegistryService(gomock.NewController(t))
	for _, authn := range []func(http.Handler) http.Handler{mw, func(next http.Handler) http.Handler { return next }} {
		router, err := NewAdmissionServer(raw, admissionTestGate(t, `forbid(principal, action, resource);`), true, authn, WithAuthConfig(cfg))
		require.NoError(t, err)
		for _, token := range []string{"", "invalid"} {
			request := httptest.NewRequest("GET", "/registry/default/v0.1/servers", nil)
			if token != "" {
				request.Header.Set("Authorization", "Bearer "+token)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if token == "invalid" && response.Code != 500 {
				require.Equal(t, 401, response.Code)
			} else {
				require.Contains(t, []int{403, 500}, response.Code)
			}
		}
	}
	gate := admissionTestGate(t, admissionTestPermit)
	guarded, err := gate.Guard(raw)
	require.NoError(t, err)
	_, err = guarded.ListSources(context.Background())
	require.ErrorIs(t, err, authorization.ErrEvidence)
	router := chi.NewRouter()
	handler := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, err := guarded.ListSources(r.Context())
		require.ErrorIs(t, err, authorization.ErrEvidence)
	})
	require.NoError(t, gate.Register(router, "GET", "/mislabeled", authorization.CatalogRead, handler, mw))
	request := httptest.NewRequest("GET", "/mislabeled", nil)
	request.Header.Set("Authorization", "Bearer valid")
	router.ServeHTTP(httptest.NewRecorder(), request)
	require.Error(t, gate.Register(router, "GET", "/mislabeled", authorization.CatalogRead, handler, mw))
	require.Error(t, gate.Register(router, "GET", "/unknown", authorization.Action("root"), handler, mw))
	require.Error(t, gate.Register(router, "GET", "/nil", authorization.CatalogRead, nil, mw))
	alternate := chi.NewRouter()
	alternate.Mount("/alternate", router)
	request = httptest.NewRequest("GET", "/alternate/mislabeled", nil)
	request.Header.Set("Authorization", "Bearer valid")
	response := httptest.NewRecorder()
	alternate.ServeHTTP(response, request)
	require.Equal(t, 500, response.Code)
}
