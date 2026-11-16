package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/internal/authorization"
	readermocks "github.com/stacklok/toolhive-registry-server/internal/authorization/mocks"
	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/catalog"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

func TestAnonymousViewPermitDoesNotBypassAuthenticationGates(t *testing.T) {
	t.Parallel()
	mw, _ := admissionTestAuth(t)
	reader, ops := admissionCapabilities(t)
	reader.(*readermocks.MockReader).EXPECT().List(gomock.Any(), "engineering", gomock.Any()).Return(catalog.Page{}, nil)
	router, err := NewCatalogServer(reader, ops, admissionTestGate(t, `permit(principal is Registry::Principal,
 action == Registry::Action::"catalog.read", resource == Registry::View::"engineering") when { !principal.authenticated };`), true, mw)
	require.NoError(t, err)
	for _, tc := range []struct {
		path, token string
		status      int
	}{
		{"/registry/engineering/v0.1/servers", "", 200},
		{"/registry/finance/v0.1/servers", "", 403},
		{"/registry/engineering/v0.1/servers", "invalid", 401},
		{"/v1/registries", "", 401},
		{"/v1/sources/engineering/entries", "", 401},
	} {
		request := httptest.NewRequest(http.MethodGet, tc.path, nil)
		if tc.token != "" {
			request.Header.Set("Authorization", "Bearer "+tc.token)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		require.Equal(t, tc.status, response.Code, tc.path)
	}
}

func TestConsumerViewAdmissionEveryRouteBeforeBackend(t *testing.T) {
	t.Parallel()
	mw, cfg := admissionTestAuth(t)
	policy := filepath.Join(t.TempDir(), "views.cedar")
	require.NoError(t, os.WriteFile(policy, []byte(`permit(principal is Registry::Principal,
 action == Registry::Action::"catalog.read", resource == Registry::View::"engineering")
 when { principal.authenticated && principal has groups && principal.groups.contains("engineering") };`), 0600))
	for _, custom := range []bool{true, false} {
		t.Run(map[bool]string{true: "custom", false: "omitted default"}[custom], func(t *testing.T) {
			t.Parallel()
			path := ""
			if custom {
				path = policy
			}
			engine, err := authorization.Load(path)
			require.NoError(t, err)
			gate, err := authorization.NewRegistrar(engine, []config.IdentityMapping{{Issuer: cfg.OAuth.Providers[0].IssuerURL,
				Groups: &config.AttributeMapping{Claim: "scope", Format: "space-delimited"}}}, nil)
			require.NoError(t, err)
			reader, ops := admissionCapabilities(t)
			backend := reader.(*readermocks.MockReader)
			router, err := NewCatalogServer(reader, ops, gate, true, mw)
			require.NoError(t, err)
			for _, route := range admissionManifest(true) {
				if route.action != authorization.CatalogRead {
					continue
				}
				for _, view := range []string{"engineering", "eng%69neering", "finance", "Engineering", "engineering%252Fteam"} {
					for _, version := range []string{"latest", "1.0.0"} {
						allowed := !custom || view == "engineering" || view == "eng%69neering"
						canonical := strings.ReplaceAll(view, "eng%69neering", "engineering")
						if strings.Contains(route.template, "{serverName}") {
							// The encoded slash in serverName makes Chi route on RawPath.
							canonical = strings.ReplaceAll(canonical, "%252F", "%2F")
						} else {
							canonical = strings.ReplaceAll(canonical, "%252F", "/")
						}
						kind := persistence.ServerKind
						if strings.Contains(route.template, "/skills") {
							kind = persistence.SkillKind
						} else if strings.Contains(route.template, "/plugins") {
							kind = persistence.PluginKind
						}
						if allowed {
							switch {
							case strings.HasSuffix(route.template, "/versions"):
								backend.EXPECT().ListVersions(gomock.Any(), canonical, gomock.Any(), gomock.Any()).Return(catalog.Page{}, nil)
							case strings.Contains(route.template, "{version}") || strings.HasSuffix(route.template, "/{name}"):
								backend.EXPECT().Get(gomock.Any(), canonical, gomock.Any(), gomock.Any()).Return(persistence.Entry{}, persistence.ErrNotFound)
							default:
								backend.EXPECT().List(gomock.Any(), canonical, gomock.Any()).DoAndReturn(func(_ context.Context, _ string, q catalog.Query) (catalog.Page, error) {
									require.Equal(t, kind, q.Kind)
									return catalog.Page{NextCursor: "unchanged-cursor"}, nil
								})
							}
						}
						path := strings.NewReplacer("{registryName}", view, "{serverName}", "io.example%2Fserver", "{namespace}", "example", "{name}", "test", "{version}", version).Replace(route.template)
						request := httptest.NewRequest(http.MethodGet, path, nil)
						request.Header.Set("Authorization", "Bearer valid")
						response := httptest.NewRecorder()
						router.ServeHTTP(response, request)
						if !allowed {
							require.Equal(t, 403, response.Code, path)
						} else if strings.Contains(route.template, "{version}") || strings.HasSuffix(route.template, "/{name}") {
							require.Equal(t, 404, response.Code, path)
						} else {
							require.Equal(t, 200, response.Code, path)
							if !strings.HasSuffix(route.template, "/versions") {
								require.Contains(t, response.Body.String(), "unchanged-cursor")
							}
						}
					}
				}
			}
			if custom {
				for _, path := range []string{"/v1/registries", "/v1/registries/engineering/entries", "/v1/sources", "/v1/sources/engineering/entries", "/openapi.json"} {
					request := httptest.NewRequest(http.MethodGet, path, nil)
					request.Header.Set("Authorization", "Bearer valid")
					response := httptest.NewRecorder()
					router.ServeHTTP(response, request)
					require.Equal(t, 403, response.Code, path)
				}
			}
		})
	}
}
