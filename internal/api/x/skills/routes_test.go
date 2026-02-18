package skills

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	thvregistry "github.com/stacklok/toolhive/pkg/registry/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/internal/service"
	"github.com/stacklok/toolhive-registry-server/internal/service/mocks"
)

// skillsRouterWithRegistryMount returns a router that mounts skills under
// /{registryName}/v0.1/x/dev.toolhive/skills so URL param registryName is set.
func skillsRouterWithRegistryMount(svc service.RegistryService) http.Handler {
	r := chi.NewRouter()
	r.Mount("/{registryName}/v0.1/x/dev.toolhive/skills", Router(svc))
	return r
}

func TestListSkills(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		path       string
		setupMocks func(m *mocks.MockRegistryService)
		wantStatus int
		wantError  string
	}{
		{
			name: "valid query returns 200",
			path: "/myreg/v0.1/x/dev.toolhive/skills",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().ListSkills(gomock.Any(), gomock.Any()).
					Return(&service.ListSkillsResult{
						Skills: []*service.Skill{
							{Namespace: "io.github.stacklok", Name: "pdf-processor", Version: "1.0.0", Description: "Extract text"},
						},
					}, nil)
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "service error returns 500",
			path: "/myreg/v0.1/x/dev.toolhive/skills",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().ListSkills(gomock.Any(), gomock.Any()).
					Return(nil, fmt.Errorf("database error"))
			},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name:       "invalid limit returns 400",
			path:       "/myreg/v0.1/x/dev.toolhive/skills?limit=notanint",
			wantStatus: http.StatusBadRequest,
			wantError:  "invalid limit parameter: must be an integer",
		},
		{
			name:       "limit over max returns 400",
			path:       "/myreg/v0.1/x/dev.toolhive/skills?limit=101",
			wantStatus: http.StatusBadRequest,
			wantError:  "invalid limit parameter: must be between 1 and 100",
		},
		{
			name:       "empty registry name returns 400",
			path:       "/%20/v0.1/x/dev.toolhive/skills",
			wantStatus: http.StatusBadRequest,
			wantError:  "registryName cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			t.Cleanup(ctrl.Finish)
			mockSvc := mocks.NewMockRegistryService(ctrl)
			if tt.setupMocks != nil {
				tt.setupMocks(mockSvc)
			}
			router := skillsRouterWithRegistryMount(mockSvc)

			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)
			assert.Equal(t, tt.wantStatus, rr.Code, "status code")
			if tt.wantError != "" {
				var body map[string]string
				require.NoError(t, json.NewDecoder(rr.Body).Decode(&body))
				assert.Equal(t, tt.wantError, body["error"], "error message")
			}
		})
	}
}

func TestGetLatestVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		path       string
		setupMocks func(m *mocks.MockRegistryService)
		wantStatus int
		wantError  string
	}{
		{
			name: "valid path returns 200",
			path: "/myreg/v0.1/x/dev.toolhive/skills/io.github.stacklok/pdf-processor",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().GetSkillVersion(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&service.Skill{
						Namespace:   "io.github.stacklok",
						Name:        "pdf-processor",
						Version:     "1.0.0",
						Description: "Extract text from PDFs",
					}, nil)
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "not found returns 404",
			path: "/myreg/v0.1/x/dev.toolhive/skills/io.github.stacklok/nonexistent",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().GetSkillVersion(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Return(nil, fmt.Errorf("%w: nonexistent", service.ErrNotFound))
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "empty namespace returns 400",
			path:       "/myreg/v0.1/x/dev.toolhive/skills/%20/pdf-processor",
			wantStatus: http.StatusBadRequest,
			wantError:  "namespace cannot be empty",
		},
		{
			name:       "empty name returns 400",
			path:       "/myreg/v0.1/x/dev.toolhive/skills/io.github.stacklok/%20",
			wantStatus: http.StatusBadRequest,
			wantError:  "name cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			t.Cleanup(ctrl.Finish)
			mockSvc := mocks.NewMockRegistryService(ctrl)
			if tt.setupMocks != nil {
				tt.setupMocks(mockSvc)
			}
			router := skillsRouterWithRegistryMount(mockSvc)

			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)
			assert.Equal(t, tt.wantStatus, rr.Code)
			if tt.wantError != "" {
				var body map[string]string
				require.NoError(t, json.NewDecoder(rr.Body).Decode(&body))
				assert.Equal(t, tt.wantError, body["error"])
			}
		})
	}
}

func TestListVersions(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	mockSvc := mocks.NewMockRegistryService(ctrl)

	mockSvc.EXPECT().ListSkills(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(&service.ListSkillsResult{
			Skills: []*service.Skill{
				{Namespace: "io.github.stacklok", Name: "pdf-processor", Version: "1.0.0"},
				{Namespace: "io.github.stacklok", Name: "pdf-processor", Version: "0.9.0"},
			},
		}, nil)

	router := skillsRouterWithRegistryMount(mockSvc)

	req := httptest.NewRequest(http.MethodGet, "/myreg/v0.1/x/dev.toolhive/skills/io.github.stacklok/pdf-processor/versions", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)

	var resp SkillListResponse
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	assert.Equal(t, 2, resp.Metadata.Count)
}

func TestGetVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		path       string
		setupMocks func(m *mocks.MockRegistryService)
		wantStatus int
		wantError  string
	}{
		{
			name: "valid path returns 200",
			path: "/myreg/v0.1/x/dev.toolhive/skills/io.github.stacklok/pdf-processor/versions/1.0.0",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().GetSkillVersion(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&service.Skill{
						Namespace:   "io.github.stacklok",
						Name:        "pdf-processor",
						Version:     "1.0.0",
						Description: "Extract text from PDFs",
					}, nil)
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "not found returns 404",
			path: "/myreg/v0.1/x/dev.toolhive/skills/io.github.stacklok/pdf-processor/versions/9.9.9",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().GetSkillVersion(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Return(nil, fmt.Errorf("%w: pdf-processor@9.9.9", service.ErrNotFound))
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "empty version returns 400",
			path:       "/myreg/v0.1/x/dev.toolhive/skills/io.github.stacklok/pdf-processor/versions/%20",
			wantStatus: http.StatusBadRequest,
			wantError:  "version cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			t.Cleanup(ctrl.Finish)
			mockSvc := mocks.NewMockRegistryService(ctrl)
			if tt.setupMocks != nil {
				tt.setupMocks(mockSvc)
			}
			router := skillsRouterWithRegistryMount(mockSvc)

			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)
			assert.Equal(t, tt.wantStatus, rr.Code)
			if tt.wantError != "" {
				var body map[string]string
				require.NoError(t, json.NewDecoder(rr.Body).Decode(&body))
				assert.Equal(t, tt.wantError, body["error"])
			}
		})
	}
}

func TestPublishSkill(t *testing.T) {
	t.Parallel()

	validBody := thvregistry.Skill{
		Namespace:   "io.github.stacklok",
		Name:        "pdf-processor",
		Description: "Extract text from PDFs",
		Version:     "1.0.0",
	}

	tests := []struct {
		name       string
		body       any
		setupMocks func(m *mocks.MockRegistryService)
		wantStatus int
		wantError  string
	}{
		{
			name: "valid body returns 201",
			body: validBody,
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().PublishSkill(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&service.Skill{
						Namespace:   "io.github.stacklok",
						Name:        "pdf-processor",
						Description: "Extract text from PDFs",
						Version:     "1.0.0",
					}, nil)
			},
			wantStatus: http.StatusCreated,
		},
		{
			name: "version conflict returns 409",
			body: validBody,
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().PublishSkill(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(nil, fmt.Errorf("%w: pdf-processor@1.0.0", service.ErrVersionAlreadyExists))
			},
			wantStatus: http.StatusConflict,
		},
		{
			name: "not managed returns 403",
			body: validBody,
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().PublishSkill(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(nil, service.ErrNotManagedRegistry)
			},
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "missing namespace returns 400",
			body:       thvregistry.Skill{Name: "x", Description: "d", Version: "1"},
			wantStatus: http.StatusBadRequest,
			wantError:  "namespace is required",
		},
		{
			name:       "missing name returns 400",
			body:       thvregistry.Skill{Namespace: "io.x", Description: "d", Version: "1"},
			wantStatus: http.StatusBadRequest,
			wantError:  "name is required",
		},
		{
			name:       "missing description returns 400",
			body:       thvregistry.Skill{Namespace: "io.x", Name: "n", Version: "1"},
			wantStatus: http.StatusBadRequest,
			wantError:  "description is required",
		},
		{
			name:       "missing version returns 400",
			body:       thvregistry.Skill{Namespace: "io.x", Name: "n", Description: "d"},
			wantStatus: http.StatusBadRequest,
			wantError:  "version is required",
		},
		{
			name:       "invalid JSON returns 400",
			body:       `{invalid`,
			wantStatus: http.StatusBadRequest,
			wantError:  "Invalid request body:",
		},
		{
			name:       "empty body returns 400",
			body:       "",
			wantStatus: http.StatusBadRequest,
			wantError:  "Invalid request body:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			t.Cleanup(ctrl.Finish)
			mockSvc := mocks.NewMockRegistryService(ctrl)
			if tt.setupMocks != nil {
				tt.setupMocks(mockSvc)
			}
			router := skillsRouterWithRegistryMount(mockSvc)

			var reqBody bytes.Buffer
			if tt.body != nil {
				switch b := tt.body.(type) {
				case string:
					reqBody.WriteString(b)
				default:
					require.NoError(t, json.NewEncoder(&reqBody).Encode(b))
				}
			}

			bodyReader := bytes.NewReader([]byte{})
			if reqBody.Len() > 0 {
				bodyReader = bytes.NewReader(reqBody.Bytes())
			}

			req := httptest.NewRequest(http.MethodPost, "/myreg/v0.1/x/dev.toolhive/skills", bodyReader)
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)
			assert.Equal(t, tt.wantStatus, rr.Code)
			if tt.wantError != "" {
				var body map[string]string
				require.NoError(t, json.NewDecoder(rr.Body).Decode(&body))
				assert.Contains(t, body["error"], tt.wantError)
			}
		})
	}
}

func TestDeleteVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		path       string
		setupMocks func(m *mocks.MockRegistryService)
		wantStatus int
		wantError  string
	}{
		{
			name: "valid path returns 204",
			path: "/myreg/v0.1/x/dev.toolhive/skills/io.github.stacklok/pdf-processor/versions/1.0.0",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().DeleteSkillVersion(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Return(nil)
			},
			wantStatus: http.StatusNoContent,
		},
		{
			name: "not found returns 404",
			path: "/myreg/v0.1/x/dev.toolhive/skills/io.github.stacklok/pdf-processor/versions/9.9.9",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().DeleteSkillVersion(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Return(fmt.Errorf("%w: pdf-processor@9.9.9", service.ErrNotFound))
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "not managed returns 403",
			path: "/myreg/v0.1/x/dev.toolhive/skills/io.github.stacklok/pdf-processor/versions/1.0.0",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().DeleteSkillVersion(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Return(service.ErrNotManagedRegistry)
			},
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "empty version returns 400",
			path:       "/myreg/v0.1/x/dev.toolhive/skills/io.github.stacklok/pdf-processor/versions/%20",
			wantStatus: http.StatusBadRequest,
			wantError:  "version cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			t.Cleanup(ctrl.Finish)
			mockSvc := mocks.NewMockRegistryService(ctrl)
			if tt.setupMocks != nil {
				tt.setupMocks(mockSvc)
			}
			router := skillsRouterWithRegistryMount(mockSvc)

			req := httptest.NewRequest(http.MethodDelete, tt.path, nil)
			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)
			assert.Equal(t, tt.wantStatus, rr.Code)
			if tt.wantError != "" {
				var body map[string]string
				require.NoError(t, json.NewDecoder(rr.Body).Decode(&body))
				assert.Equal(t, tt.wantError, body["error"])
			}
		})
	}
}
