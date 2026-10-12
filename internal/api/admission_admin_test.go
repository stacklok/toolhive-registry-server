package api

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/internal/authorization"
	"github.com/stacklok/toolhive-registry-server/internal/service"
	mocksvc "github.com/stacklok/toolhive-registry-server/internal/service/mocks"
)

func TestAdmissionAdminWrites(t *testing.T) {
	t.Parallel()
	mw, cfg := admissionTestAuth(t)
	for _, kind := range []string{"server", "skill", "plugin"} {
		for _, allowed := range []bool{false, true} {
			t.Run(kind+map[bool]string{true: " allow", false: " deny"}[allowed], func(t *testing.T) {
				t.Parallel()
				raw := mocksvc.NewMockRegistryService(gomock.NewController(t))
				policy := `forbid(principal,action,resource);`
				if allowed {
					policy = admissionTestPermit
					switch kind {
					case "server":
						raw.EXPECT().PublishServerVersion(gomock.Any(), gomock.Any()).Return(nil, service.ErrClaimsInsufficient)
						raw.EXPECT().DeleteServerVersion(gomock.Any(), gomock.Any()).Return(service.ErrClaimsInsufficient)
					case "skill":
						raw.EXPECT().PublishSkill(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, service.ErrClaimsInsufficient)
						raw.EXPECT().DeleteSkillVersion(gomock.Any(), gomock.Any()).Return(service.ErrClaimsInsufficient)
					case "plugin":
						raw.EXPECT().PublishPlugin(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, service.ErrClaimsInsufficient)
						raw.EXPECT().DeletePluginVersion(gomock.Any(), gomock.Any()).Return(service.ErrClaimsInsufficient)
					}
				}
				router, err := NewAdmissionServer(raw, admissionTestGate(t, policy), true, mw, WithAuthConfig(cfg))
				require.NoError(t, err)
				data, err := json.Marshal(map[string]any{kind: map[string]any{}})
				require.NoError(t, err)
				publish := httptest.NewRequest("POST", "/v1/entries", bytes.NewReader(data))
				publish.Header.Set("Authorization", "Bearer valid")
				response := httptest.NewRecorder()
				router.ServeHTTP(response, publish)
				require.Equal(t, 403, response.Code)
				remove := httptest.NewRequest("DELETE", "/v1/entries/"+kind+"/test/versions/1.0.0", nil)
				remove.Header.Set("Authorization", "Bearer valid")
				response = httptest.NewRecorder()
				router.ServeHTTP(response, remove)
				require.Equal(t, 403, response.Code)
			})
		}
	}
	for _, family := range []string{"sources", "registries"} {
		for _, update := range []bool{false, true} {
			t.Run(family+map[bool]string{true: " update", false: " create"}[update], func(t *testing.T) {
				t.Parallel()
				raw := mocksvc.NewMockRegistryService(gomock.NewController(t))
				switch family {
				case "sources":
					if update {
						gomock.InOrder(raw.EXPECT().CreateSource(gomock.Any(), "test", gomock.Any()).Return(nil, service.ErrSourceAlreadyExists), raw.EXPECT().UpdateSource(gomock.Any(), "test", gomock.Any()).Return(&service.SourceInfo{Name: "test"}, nil))
					} else {
						raw.EXPECT().CreateSource(gomock.Any(), "test", gomock.Any()).Return(&service.SourceInfo{Name: "test"}, nil)
					}
				case "registries":
					if update {
						gomock.InOrder(raw.EXPECT().CreateRegistry(gomock.Any(), "test", gomock.Any()).Return(nil, service.ErrRegistryAlreadyExists), raw.EXPECT().UpdateRegistry(gomock.Any(), "test", gomock.Any()).Return(&service.RegistryInfo{Name: "test"}, nil))
					} else {
						raw.EXPECT().CreateRegistry(gomock.Any(), "test", gomock.Any()).Return(&service.RegistryInfo{Name: "test"}, nil)
					}
				}
				action := authorization.SourcesWrite
				if family == "registries" {
					action = authorization.ViewsWrite
				}
				policy := `permit(principal,action == Registry::Action::"` + string(action) + `",resource);`
				router, err := NewAdmissionServer(raw, admissionTestGate(t, policy), true, mw, WithAuthConfig(cfg))
				require.NoError(t, err)
				request := httptest.NewRequest("PUT", "/v1/"+family+"/test", bytes.NewReader([]byte(`{}`)))
				request.Header.Set("Authorization", "Bearer valid")
				response := httptest.NewRecorder()
				router.ServeHTTP(response, request)
				if update {
					require.Equal(t, 200, response.Code)
				} else {
					require.Equal(t, 201, response.Code)
				}
			})
		}
	}
}
