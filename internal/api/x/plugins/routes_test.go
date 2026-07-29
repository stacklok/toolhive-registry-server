package plugins

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

// pluginsRouterWithRegistryMount returns a router that mounts plugins under
// /{registryName}/v0.1/x/dev.toolhive/plugins so URL param registryName is set.
func pluginsRouterWithRegistryMount(svc service.RegistryService) http.Handler {
	r := chi.NewRouter()
	r.Mount("/{registryName}/v0.1/x/dev.toolhive/plugins", Router(svc))
	return r
}

// applyListPluginsOptions applies service.Option functions to a ListPluginsOptions
// struct so tests can inspect which options were passed by the handler.
func applyListPluginsOptions(t *testing.T, opts []service.Option) *service.ListPluginsOptions {
	t.Helper()
	result := &service.ListPluginsOptions{}
	for _, opt := range opts {
		require.NoError(t, opt(result))
	}
	return result
}

func TestListPlugins(t *testing.T) {
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
			path: "/myreg/v0.1/x/dev.toolhive/plugins",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().ListPlugins(gomock.Any(), gomock.Any()).
					Return(&service.ListPluginsResult{
						Plugins: []*service.Plugin{
							{Namespace: "io.github.stacklok", Name: "auth-proxy", Version: "1.0.0", Description: "OAuth proxy"},
						},
					}, nil)
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "service error returns 500",
			path: "/myreg/v0.1/x/dev.toolhive/plugins",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().ListPlugins(gomock.Any(), gomock.Any()).
					Return(nil, fmt.Errorf("database error"))
			},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name:       "invalid limit returns 400",
			path:       "/myreg/v0.1/x/dev.toolhive/plugins?limit=notanint",
			wantStatus: http.StatusBadRequest,
			wantError:  "invalid limit parameter: must be an integer",
		},
		{
			name:       "limit over max returns 400",
			path:       "/myreg/v0.1/x/dev.toolhive/plugins?limit=101",
			wantStatus: http.StatusBadRequest,
			wantError:  "invalid limit parameter: must be between 1 and 100",
		},
		{
			name:       "empty registry name returns 400",
			path:       "/%20/v0.1/x/dev.toolhive/plugins",
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
			router := pluginsRouterWithRegistryMount(mockSvc)

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

func TestListPluginsSearchFilter(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	mockSvc := mocks.NewMockRegistryService(ctrl)

	mockSvc.EXPECT().ListPlugins(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ any, opts ...service.Option) (*service.ListPluginsResult, error) {
			resolved := applyListPluginsOptions(t, opts)
			assert.Equal(t, "myreg", resolved.RegistryName)
			require.NotNil(t, resolved.Search)
			assert.Equal(t, "auth", *resolved.Search)
			return &service.ListPluginsResult{Plugins: []*service.Plugin{}}, nil
		})

	router := pluginsRouterWithRegistryMount(mockSvc)
	req := httptest.NewRequest(http.MethodGet, "/myreg/v0.1/x/dev.toolhive/plugins?search=auth", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestListPluginsCursorPassed(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	mockSvc := mocks.NewMockRegistryService(ctrl)

	mockSvc.EXPECT().ListPlugins(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ any, opts ...service.Option) (*service.ListPluginsResult, error) {
			resolved := applyListPluginsOptions(t, opts)
			require.NotNil(t, resolved.Cursor)
			assert.Equal(t, "abc123", *resolved.Cursor)
			return &service.ListPluginsResult{Plugins: []*service.Plugin{}}, nil
		})

	router := pluginsRouterWithRegistryMount(mockSvc)
	req := httptest.NewRequest(http.MethodGet, "/myreg/v0.1/x/dev.toolhive/plugins?cursor=abc123", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestListPluginsNamespaceQueryParamIgnored(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	mockSvc := mocks.NewMockRegistryService(ctrl)

	// The handler passes exactly 2 options (registryName + limit) even when
	// ?namespace=foo is in the URL -- the namespace query param must be ignored.
	mockSvc.EXPECT().ListPlugins(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ any, opts ...service.Option) (*service.ListPluginsResult, error) {
			resolved := applyListPluginsOptions(t, opts)
			assert.Empty(t, resolved.Namespace, "namespace must not be forwarded from query param")
			return &service.ListPluginsResult{Plugins: []*service.Plugin{}}, nil
		})

	router := pluginsRouterWithRegistryMount(mockSvc)
	req := httptest.NewRequest(http.MethodGet, "/myreg/v0.1/x/dev.toolhive/plugins?namespace=io.github.stacklok", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestListPluginsRegistryNotFoundReturns404(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	mockSvc := mocks.NewMockRegistryService(ctrl)

	mockSvc.EXPECT().ListPlugins(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil, fmt.Errorf("%w: nosuchreg", service.ErrRegistryNotFound))

	router := pluginsRouterWithRegistryMount(mockSvc)
	req := httptest.NewRequest(http.MethodGet, "/nosuchreg/v0.1/x/dev.toolhive/plugins", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusNotFound, rr.Code)

	var body map[string]string
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&body))
	assert.Contains(t, body["error"], "registry not found")
}

func TestListPluginsInsufficientClaimsReturns403(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	mockSvc := mocks.NewMockRegistryService(ctrl)

	mockSvc.EXPECT().ListPlugins(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil, fmt.Errorf("%w: gated-reg", service.ErrClaimsInsufficient))

	router := pluginsRouterWithRegistryMount(mockSvc)
	req := httptest.NewRequest(http.MethodGet, "/gated-reg/v0.1/x/dev.toolhive/plugins", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusForbidden, rr.Code)

	var body map[string]string
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&body))
	assert.Contains(t, body["error"], "forbidden")
}

func TestGetLatestPluginVersion(t *testing.T) {
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
			path: "/myreg/v0.1/x/dev.toolhive/plugins/io.github.stacklok/auth-proxy",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().GetPluginVersion(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&service.Plugin{
						Namespace:   "io.github.stacklok",
						Name:        "auth-proxy",
						Version:     "1.0.0",
						Description: "OAuth proxy plugin",
					}, nil)
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "not found returns 404",
			path: "/myreg/v0.1/x/dev.toolhive/plugins/io.github.stacklok/nonexistent",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().GetPluginVersion(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Return(nil, fmt.Errorf("%w: nonexistent", service.ErrNotFound))
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "insufficient claims returns 403",
			path: "/gated/v0.1/x/dev.toolhive/plugins/io.github.stacklok/auth-proxy",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().GetPluginVersion(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Return(nil, fmt.Errorf("%w: gated", service.ErrClaimsInsufficient))
			},
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "empty namespace returns 400",
			path:       "/myreg/v0.1/x/dev.toolhive/plugins/%20/auth-proxy",
			wantStatus: http.StatusBadRequest,
			wantError:  "namespace cannot be empty",
		},
		{
			name:       "empty name returns 400",
			path:       "/myreg/v0.1/x/dev.toolhive/plugins/io.github.stacklok/%20",
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
			router := pluginsRouterWithRegistryMount(mockSvc)

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

func TestListPluginVersions(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	mockSvc := mocks.NewMockRegistryService(ctrl)

	mockSvc.EXPECT().ListPlugins(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(&service.ListPluginsResult{
			Plugins: []*service.Plugin{
				{Namespace: "io.github.stacklok", Name: "auth-proxy", Version: "1.0.0"},
				{Namespace: "io.github.stacklok", Name: "auth-proxy", Version: "0.9.0"},
			},
		}, nil)

	router := pluginsRouterWithRegistryMount(mockSvc)

	req := httptest.NewRequest(http.MethodGet, "/myreg/v0.1/x/dev.toolhive/plugins/io.github.stacklok/auth-proxy/versions", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)

	var resp PluginListResponse
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	assert.Equal(t, 2, resp.Metadata.Count)
}

func TestListPluginVersionsInsufficientClaimsReturns403(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	mockSvc := mocks.NewMockRegistryService(ctrl)

	mockSvc.EXPECT().ListPlugins(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil, fmt.Errorf("%w: gated-reg", service.ErrClaimsInsufficient))

	router := pluginsRouterWithRegistryMount(mockSvc)
	req := httptest.NewRequest(http.MethodGet, "/gated-reg/v0.1/x/dev.toolhive/plugins/io.github.stacklok/auth-proxy/versions", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusForbidden, rr.Code)

	var body map[string]string
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&body))
	assert.Contains(t, body["error"], "forbidden")
}

func TestGetPluginVersion(t *testing.T) {
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
			path: "/myreg/v0.1/x/dev.toolhive/plugins/io.github.stacklok/auth-proxy/versions/1.0.0",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().GetPluginVersion(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&service.Plugin{
						Namespace:   "io.github.stacklok",
						Name:        "auth-proxy",
						Version:     "1.0.0",
						Description: "OAuth proxy plugin",
					}, nil)
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "not found returns 404",
			path: "/myreg/v0.1/x/dev.toolhive/plugins/io.github.stacklok/auth-proxy/versions/9.9.9",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().GetPluginVersion(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Return(nil, fmt.Errorf("%w: auth-proxy@9.9.9", service.ErrNotFound))
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "insufficient claims returns 403",
			path: "/gated/v0.1/x/dev.toolhive/plugins/io.github.stacklok/auth-proxy/versions/1.0.0",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().GetPluginVersion(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Return(nil, fmt.Errorf("%w: gated", service.ErrClaimsInsufficient))
			},
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "empty version returns 400",
			path:       "/myreg/v0.1/x/dev.toolhive/plugins/io.github.stacklok/auth-proxy/versions/%20",
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
			router := pluginsRouterWithRegistryMount(mockSvc)

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
