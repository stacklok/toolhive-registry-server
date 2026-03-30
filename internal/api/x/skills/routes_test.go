package skills

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
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

// applyListSkillsOptions applies service.Option functions to a ListSkillsOptions
// struct so tests can inspect which options were passed by the handler.
func applyListSkillsOptions(t *testing.T, opts []service.Option) *service.ListSkillsOptions {
	t.Helper()
	result := &service.ListSkillsOptions{}
	for _, opt := range opts {
		require.NoError(t, opt(result))
	}
	return result
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

func TestListSkillsSearchFilter(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	mockSvc := mocks.NewMockRegistryService(ctrl)

	mockSvc.EXPECT().ListSkills(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ any, opts ...service.Option) (*service.ListSkillsResult, error) {
			resolved := applyListSkillsOptions(t, opts)
			assert.Equal(t, "myreg", resolved.RegistryName)
			require.NotNil(t, resolved.Search)
			assert.Equal(t, "pdf", *resolved.Search)
			return &service.ListSkillsResult{Skills: []*service.Skill{}}, nil
		})

	router := skillsRouterWithRegistryMount(mockSvc)
	req := httptest.NewRequest(http.MethodGet, "/myreg/v0.1/x/dev.toolhive/skills?search=pdf", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestListSkillsCursorPassed(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	mockSvc := mocks.NewMockRegistryService(ctrl)

	mockSvc.EXPECT().ListSkills(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ any, opts ...service.Option) (*service.ListSkillsResult, error) {
			resolved := applyListSkillsOptions(t, opts)
			require.NotNil(t, resolved.Cursor)
			assert.Equal(t, "abc123", *resolved.Cursor)
			return &service.ListSkillsResult{Skills: []*service.Skill{}}, nil
		})

	router := skillsRouterWithRegistryMount(mockSvc)
	req := httptest.NewRequest(http.MethodGet, "/myreg/v0.1/x/dev.toolhive/skills?cursor=abc123", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestListSkillsNamespaceQueryParamIgnored(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	mockSvc := mocks.NewMockRegistryService(ctrl)

	// The handler passes exactly 2 options (registryName + limit) even when
	// ?namespace=foo is in the URL -- the namespace query param must be ignored.
	mockSvc.EXPECT().ListSkills(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ any, opts ...service.Option) (*service.ListSkillsResult, error) {
			resolved := applyListSkillsOptions(t, opts)
			assert.Empty(t, resolved.Namespace, "namespace must not be forwarded from query param")
			return &service.ListSkillsResult{Skills: []*service.Skill{}}, nil
		})

	router := skillsRouterWithRegistryMount(mockSvc)
	req := httptest.NewRequest(http.MethodGet, "/myreg/v0.1/x/dev.toolhive/skills?namespace=io.github.stacklok", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestListSkillsRegistryNotFoundReturns404(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	mockSvc := mocks.NewMockRegistryService(ctrl)

	mockSvc.EXPECT().ListSkills(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil, fmt.Errorf("%w: nosuchreg", service.ErrRegistryNotFound))

	router := skillsRouterWithRegistryMount(mockSvc)
	req := httptest.NewRequest(http.MethodGet, "/nosuchreg/v0.1/x/dev.toolhive/skills", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusNotFound, rr.Code)

	var body map[string]string
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&body))
	assert.Contains(t, body["error"], "registry not found")
}

func TestListSkillsInsufficientClaimsReturns403(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	mockSvc := mocks.NewMockRegistryService(ctrl)

	mockSvc.EXPECT().ListSkills(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil, fmt.Errorf("%w: gated-reg", service.ErrClaimsInsufficient))

	router := skillsRouterWithRegistryMount(mockSvc)
	req := httptest.NewRequest(http.MethodGet, "/gated-reg/v0.1/x/dev.toolhive/skills", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusForbidden, rr.Code)

	var body map[string]string
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&body))
	assert.Contains(t, body["error"], "forbidden")
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
			name: "insufficient claims returns 403",
			path: "/gated/v0.1/x/dev.toolhive/skills/io.github.stacklok/pdf-processor",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().GetSkillVersion(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Return(nil, fmt.Errorf("%w: gated", service.ErrClaimsInsufficient))
			},
			wantStatus: http.StatusForbidden,
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

func TestListVersionsInsufficientClaimsReturns403(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	mockSvc := mocks.NewMockRegistryService(ctrl)

	mockSvc.EXPECT().ListSkills(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil, fmt.Errorf("%w: gated-reg", service.ErrClaimsInsufficient))

	router := skillsRouterWithRegistryMount(mockSvc)
	req := httptest.NewRequest(http.MethodGet, "/gated-reg/v0.1/x/dev.toolhive/skills/io.github.stacklok/pdf-processor/versions", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusForbidden, rr.Code)

	var body map[string]string
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&body))
	assert.Contains(t, body["error"], "forbidden")
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
			name: "insufficient claims returns 403",
			path: "/gated/v0.1/x/dev.toolhive/skills/io.github.stacklok/pdf-processor/versions/1.0.0",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().GetSkillVersion(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Return(nil, fmt.Errorf("%w: gated", service.ErrClaimsInsufficient))
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
