package v01

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/internal/service"
	"github.com/stacklok/toolhive-registry-server/internal/service/mocks"
)

func TestListServers(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	tests := []struct {
		name        string
		path        string
		setupMocks  func(*mocks.MockRegistryService)
		setupRouter func(*mocks.MockRegistryService) http.Handler
		wantStatus  int
	}{
		{
			name: "list servers - basic",
			path: "/v0.1/servers",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().ListServers(gomock.Any(), gomock.Any()).Return(&service.ListServersResult{
					Servers:    []*upstreamv0.ServerJSON{},
					NextCursor: "",
				}, nil).AnyTimes()
			},
			setupRouter: func(mockSvc *mocks.MockRegistryService) http.Handler {
				return Router(mockSvc, true)
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "list servers - with cursor",
			path: "/v0.1/servers?cursor=abc123",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().ListServers(gomock.Any(), gomock.Any()).Return(&service.ListServersResult{
					Servers:    []*upstreamv0.ServerJSON{},
					NextCursor: "",
				}, nil).AnyTimes()
			},
			setupRouter: func(mockSvc *mocks.MockRegistryService) http.Handler {
				return Router(mockSvc, true)
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "list servers - with limit",
			path: "/v0.1/servers?limit=10",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().ListServers(gomock.Any(), gomock.Any()).Return(&service.ListServersResult{
					Servers:    []*upstreamv0.ServerJSON{},
					NextCursor: "",
				}, nil).AnyTimes()
			},
			setupRouter: func(mockSvc *mocks.MockRegistryService) http.Handler {
				return Router(mockSvc, true)
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "list servers - with search",
			path: "/v0.1/servers?search=test",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().ListServers(gomock.Any(), gomock.Any()).Return(&service.ListServersResult{
					Servers:    []*upstreamv0.ServerJSON{},
					NextCursor: "",
				}, nil).AnyTimes()
			},
			setupRouter: func(mockSvc *mocks.MockRegistryService) http.Handler {
				return Router(mockSvc, true)
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "list servers - with updated_since",
			path: "/v0.1/servers?updated_since=2025-01-01T00:00:00Z",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().ListServers(gomock.Any(), gomock.Any()).Return(&service.ListServersResult{
					Servers:    []*upstreamv0.ServerJSON{},
					NextCursor: "",
				}, nil).AnyTimes()
			},
			setupRouter: func(mockSvc *mocks.MockRegistryService) http.Handler {
				return Router(mockSvc, true)
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "list servers - with version",
			path: "/v0.1/servers?version=latest",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().ListServers(gomock.Any(), gomock.Any()).Return(&service.ListServersResult{
					Servers:    []*upstreamv0.ServerJSON{},
					NextCursor: "",
				}, nil).AnyTimes()
			},
			setupRouter: func(mockSvc *mocks.MockRegistryService) http.Handler {
				return Router(mockSvc, true)
			},
			wantStatus: http.StatusOK,
		},
		{
			name:       "list servers - invalid limit",
			path:       "/v0.1/servers?limit=invalid",
			setupMocks: func(_ *mocks.MockRegistryService) {},
			setupRouter: func(mockSvc *mocks.MockRegistryService) http.Handler {
				return Router(mockSvc, true)
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "list servers - invalid updated_since",
			path:       "/v0.1/servers?updated_since=invalid",
			setupMocks: func(_ *mocks.MockRegistryService) {},
			setupRouter: func(mockSvc *mocks.MockRegistryService) http.Handler {
				return Router(mockSvc, true)
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "list servers with registry name - basic",
			path: "/foo/v0.1/servers",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().ListServers(gomock.Any(), gomock.Any()).Return(&service.ListServersResult{
					Servers:    []*upstreamv0.ServerJSON{},
					NextCursor: "",
				}, nil).AnyTimes()
			},
			setupRouter: func(mockSvc *mocks.MockRegistryService) http.Handler {
				return Router(mockSvc, true)
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "list servers with registry name - with cursor",
			path: "/foo/v0.1/servers?cursor=abc123",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().ListServers(gomock.Any(), gomock.Any()).Return(&service.ListServersResult{
					Servers:    []*upstreamv0.ServerJSON{},
					NextCursor: "",
				}, nil).AnyTimes()
			},
			setupRouter: func(mockSvc *mocks.MockRegistryService) http.Handler {
				return Router(mockSvc, false)
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "list servers with registry name - with limit",
			path: "/foo/v0.1/servers?limit=10",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().ListServers(gomock.Any(), gomock.Any()).Return(&service.ListServersResult{
					Servers:    []*upstreamv0.ServerJSON{},
					NextCursor: "",
				}, nil).AnyTimes()
			},
			setupRouter: func(mockSvc *mocks.MockRegistryService) http.Handler {
				return Router(mockSvc, false)
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "list servers with registry name - with search",
			path: "/foo/v0.1/servers?search=test",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().ListServers(gomock.Any(), gomock.Any()).Return(&service.ListServersResult{
					Servers:    []*upstreamv0.ServerJSON{},
					NextCursor: "",
				}, nil).AnyTimes()
			},
			setupRouter: func(mockSvc *mocks.MockRegistryService) http.Handler {
				return Router(mockSvc, false)
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "list servers with registry name - with updated_since",
			path: "/foo/v0.1/servers?updated_since=2025-01-01T00:00:00Z",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().ListServers(gomock.Any(), gomock.Any()).Return(&service.ListServersResult{
					Servers:    []*upstreamv0.ServerJSON{},
					NextCursor: "",
				}, nil).AnyTimes()
			},
			setupRouter: func(mockSvc *mocks.MockRegistryService) http.Handler {
				return Router(mockSvc, false)
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "list servers with registry name - with version",
			path: "/foo/v0.1/servers?version=latest",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().ListServers(gomock.Any(), gomock.Any()).Return(&service.ListServersResult{
					Servers:    []*upstreamv0.ServerJSON{},
					NextCursor: "",
				}, nil).AnyTimes()
			},
			setupRouter: func(mockSvc *mocks.MockRegistryService) http.Handler {
				return Router(mockSvc, false)
			},
			wantStatus: http.StatusOK,
		},
		{
			name:       "list servers with registry name - invalid limit",
			path:       "/foo/v0.1/servers?limit=invalid",
			setupMocks: func(_ *mocks.MockRegistryService) {},
			setupRouter: func(mockSvc *mocks.MockRegistryService) http.Handler {
				return Router(mockSvc, false)
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "list servers with registry name - invalid updated_since",
			path:       "/foo/v0.1/servers?updated_since=invalid",
			setupMocks: func(_ *mocks.MockRegistryService) {},
			setupRouter: func(mockSvc *mocks.MockRegistryService) http.Handler {
				return Router(mockSvc, false)
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "list servers - disabled aggregated endpoints",
			path:       "/v0.1/servers",
			setupMocks: func(_ *mocks.MockRegistryService) {},
			setupRouter: func(mockSvc *mocks.MockRegistryService) http.Handler {
				return Router(mockSvc, false)
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "list servers with registry name - registry not found",
			path: "/nonexistent/v0.1/servers",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().ListServers(gomock.Any(), gomock.Any()).
					Return(nil, service.ErrRegistryNotFound)
			},
			setupRouter: func(mockSvc *mocks.MockRegistryService) http.Handler {
				return Router(mockSvc, false)
			},
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req, err := http.NewRequest("GET", tt.path, nil)
			require.NoError(t, err)

			mockSvc := mocks.NewMockRegistryService(ctrl)
			tt.setupMocks(mockSvc)
			router := tt.setupRouter(mockSvc)

			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)

			assert.Equal(t, tt.wantStatus, rr.Code)

			if tt.wantStatus == http.StatusOK {
				var response upstreamv0.ServerListResponse
				err = json.Unmarshal(rr.Body.Bytes(), &response)
				require.NoError(t, err)
				assert.NotNil(t, response.Servers)
				assert.NotNil(t, response.Metadata)
			}
		})
	}
}

func TestListVersions(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	tests := []struct {
		name        string
		path        string
		setupMocks  func(*mocks.MockRegistryService)
		setupRouter func(*mocks.MockRegistryService) http.Handler
		wantStatus  int
	}{
		{
			name: "list versions - valid server name",
			path: "/v0.1/servers/com.example%2Ftest-server/versions",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().ListServerVersions(gomock.Any(), gomock.Any()).Return([]*upstreamv0.ServerJSON{}, nil).AnyTimes()
			},
			setupRouter: func(mockSvc *mocks.MockRegistryService) http.Handler {
				return Router(mockSvc, true)
			},
			wantStatus: http.StatusOK,
		},
		{
			name:       "list versions - empty server name",
			path:       "/v0.1/servers//versions",
			setupMocks: func(_ *mocks.MockRegistryService) {},
			setupRouter: func(mockSvc *mocks.MockRegistryService) http.Handler {
				return Router(mockSvc, true)
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "list versions - empty server name",
			path:       "/v0.1/servers/%20/versions",
			setupMocks: func(_ *mocks.MockRegistryService) {},
			setupRouter: func(mockSvc *mocks.MockRegistryService) http.Handler {
				return Router(mockSvc, true)
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "list versions with registry name - valid server name",
			path: "/foo/v0.1/servers/com.example%2Ftest-server/versions",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().ListServerVersions(gomock.Any(), gomock.Any()).Return([]*upstreamv0.ServerJSON{}, nil).AnyTimes()
			},
			setupRouter: func(mockSvc *mocks.MockRegistryService) http.Handler {
				return Router(mockSvc, false)
			},
			wantStatus: http.StatusOK,
		},
		{
			name:       "list versions with registry name - empty server name",
			path:       "/foo/v0.1/servers//versions",
			setupMocks: func(_ *mocks.MockRegistryService) {},
			setupRouter: func(mockSvc *mocks.MockRegistryService) http.Handler {
				return Router(mockSvc, false)
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "list versions with registry name - empty server name",
			path:       "/foo/v0.1/servers/%20/versions",
			setupMocks: func(_ *mocks.MockRegistryService) {},
			setupRouter: func(mockSvc *mocks.MockRegistryService) http.Handler {
				return Router(mockSvc, false)
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "list versions - disabled aggregated endpoints",
			path:       "/v0.1/servers/foo/versions",
			setupMocks: func(_ *mocks.MockRegistryService) {},
			setupRouter: func(mockSvc *mocks.MockRegistryService) http.Handler {
				return Router(mockSvc, false)
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "list versions with registry name - registry not found",
			path: "/nonexistent/v0.1/servers/com.example%2Ftest-server/versions",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().ListServerVersions(gomock.Any(), gomock.Any()).
					Return(nil, service.ErrRegistryNotFound)
			},
			setupRouter: func(mockSvc *mocks.MockRegistryService) http.Handler {
				return Router(mockSvc, false)
			},
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req, err := http.NewRequest("GET", tt.path, nil)
			require.NoError(t, err)

			mockSvc := mocks.NewMockRegistryService(ctrl)
			tt.setupMocks(mockSvc)
			router := tt.setupRouter(mockSvc)

			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)

			assert.Equal(t, tt.wantStatus, rr.Code)

			if tt.wantStatus == http.StatusOK {
				var response upstreamv0.ServerListResponse
				err = json.Unmarshal(rr.Body.Bytes(), &response)
				require.NoError(t, err)
				assert.NotNil(t, response.Servers)
				assert.NotNil(t, response.Metadata)
			}
		})
	}
}

func TestGetVersion(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	tests := []struct {
		name        string
		path        string
		setupMocks  func(*mocks.MockRegistryService)
		setupRouter func(*mocks.MockRegistryService) http.Handler
		wantStatus  int
	}{
		{
			name: "get version - valid server and version",
			path: "/v0.1/servers/com.example%2Ftest-server/versions/1.0.0",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().GetServerVersion(gomock.Any(), gomock.Any()).Return(&upstreamv0.ServerJSON{}, nil).AnyTimes()
			},
			setupRouter: func(mockSvc *mocks.MockRegistryService) http.Handler {
				return Router(mockSvc, true)
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "get version - latest",
			path: "/v0.1/servers/com.example%2Ftest-server/versions/latest",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().GetServerVersion(gomock.Any(), gomock.Any()).Return(&upstreamv0.ServerJSON{}, nil).AnyTimes()
			},
			setupRouter: func(mockSvc *mocks.MockRegistryService) http.Handler {
				return Router(mockSvc, true)
			},
			wantStatus: http.StatusOK,
		},
		{
			name:       "get version - empty server name",
			path:       "/v0.1/servers//versions/1.0.0",
			setupMocks: func(_ *mocks.MockRegistryService) {},
			setupRouter: func(mockSvc *mocks.MockRegistryService) http.Handler {
				return Router(mockSvc, true)
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "get version - empty version",
			path:       "/v0.1/servers/com.example%2Ftest-server/versions/",
			setupMocks: func(_ *mocks.MockRegistryService) {},
			setupRouter: func(mockSvc *mocks.MockRegistryService) http.Handler {
				return Router(mockSvc, true)
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "get version - empty server name",
			path:       "/v0.1/servers/%20/versions/1.0.0",
			setupMocks: func(_ *mocks.MockRegistryService) {},
			setupRouter: func(mockSvc *mocks.MockRegistryService) http.Handler {
				return Router(mockSvc, true)
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "get version - empty version",
			path:       "/v0.1/servers/com.example%2Ftest-server/versions/%20",
			setupMocks: func(_ *mocks.MockRegistryService) {},
			setupRouter: func(mockSvc *mocks.MockRegistryService) http.Handler {
				return Router(mockSvc, true)
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "get version with registry name - valid server and version",
			path: "/foo/v0.1/servers/com.example%2Ftest-server/versions/1.0.0",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().GetServerVersion(gomock.Any(), gomock.Any()).Return(&upstreamv0.ServerJSON{}, nil).AnyTimes()
			},
			setupRouter: func(mockSvc *mocks.MockRegistryService) http.Handler {
				return Router(mockSvc, false)
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "get version with registry name - latest",
			path: "/foo/v0.1/servers/com.example%2Ftest-server/versions/latest",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().GetServerVersion(gomock.Any(), gomock.Any()).Return(&upstreamv0.ServerJSON{}, nil).AnyTimes()
			},
			setupRouter: func(mockSvc *mocks.MockRegistryService) http.Handler {
				return Router(mockSvc, false)
			},
			wantStatus: http.StatusOK,
		},
		{
			name:       "get version with registry name - empty server name",
			path:       "/foo/v0.1/servers//versions/1.0.0",
			setupMocks: func(_ *mocks.MockRegistryService) {},
			setupRouter: func(mockSvc *mocks.MockRegistryService) http.Handler {
				return Router(mockSvc, false)
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "get version with registry name - empty version",
			path:       "/foo/v0.1/servers/com.example%2Ftest-server/versions/",
			setupMocks: func(_ *mocks.MockRegistryService) {},
			setupRouter: func(mockSvc *mocks.MockRegistryService) http.Handler {
				return Router(mockSvc, false)
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "get version with registry name - empty server name",
			path:       "/foo/v0.1/servers/%20/versions/1.0.0",
			setupMocks: func(_ *mocks.MockRegistryService) {},
			setupRouter: func(mockSvc *mocks.MockRegistryService) http.Handler {
				return Router(mockSvc, false)
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "get version with registry name - empty version",
			path:       "/foo/v0.1/servers/com.example%2Ftest-server/versions/%20",
			setupMocks: func(_ *mocks.MockRegistryService) {},
			setupRouter: func(mockSvc *mocks.MockRegistryService) http.Handler {
				return Router(mockSvc, false)
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "get version - disabled aggregated endpoints",
			path:       "/v0.1/servers/foo/versions/1.0.0",
			setupMocks: func(_ *mocks.MockRegistryService) {},
			setupRouter: func(mockSvc *mocks.MockRegistryService) http.Handler {
				return Router(mockSvc, false)
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "get version with registry name - registry not found",
			path: "/nonexistent/v0.1/servers/com.example%2Ftest-server/versions/1.0.0",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().GetServerVersion(gomock.Any(), gomock.Any()).
					Return(nil, service.ErrRegistryNotFound)
			},
			setupRouter: func(mockSvc *mocks.MockRegistryService) http.Handler {
				return Router(mockSvc, false)
			},
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req, err := http.NewRequest("GET", tt.path, nil)
			require.NoError(t, err)

			mockSvc := mocks.NewMockRegistryService(ctrl)
			tt.setupMocks(mockSvc)
			router := tt.setupRouter(mockSvc)

			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)

			assert.Equal(t, tt.wantStatus, rr.Code)

			if tt.wantStatus == http.StatusOK {
				var response upstreamv0.ServerResponse
				err = json.Unmarshal(rr.Body.Bytes(), &response)
				require.NoError(t, err)
			}
		})
	}
}

func TestPublish(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	tests := []struct {
		name          string
		path          string
		body          string
		setupMocks    func(*mocks.MockRegistryService)
		setupRouter   func(*mocks.MockRegistryService) http.Handler
		wantStatus    int
		expectedError string
	}{
		{
			name:       "publish - not implemented",
			path:       "/v0.1/publish",
			body:       `{"name":"test","version":"1.0.0"}`,
			setupMocks: func(_ *mocks.MockRegistryService) {},
			setupRouter: func(mockSvc *mocks.MockRegistryService) http.Handler {
				return Router(mockSvc, true)
			},
			wantStatus:    http.StatusNotImplemented,
			expectedError: "Publishing servers via this endpoint is not supported. Use /{registryName}/v0.1/publish endpoint instead",
		},
		{
			name:       "publish with registry name - missing body",
			path:       "/foo/v0.1/publish",
			body:       ``,
			setupMocks: func(_ *mocks.MockRegistryService) {},
			setupRouter: func(mockSvc *mocks.MockRegistryService) http.Handler {
				return Router(mockSvc, false)
			},
			wantStatus:    http.StatusBadRequest,
			expectedError: "Invalid request body",
		},
		{
			name: "publish with registry name - success",
			path: "/foo/v0.1/publish",
			body: `{"name":"com.example/test-server","version":"1.0.0","description":"Test server"}`,
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().PublishServerVersion(gomock.Any(), gomock.Any()).
					Return(&upstreamv0.ServerJSON{
						Name:        "com.example/test-server",
						Version:     "1.0.0",
						Description: "Test server",
					}, nil)
			},
			setupRouter: func(mockSvc *mocks.MockRegistryService) http.Handler {
				return Router(mockSvc, false)
			},
			wantStatus: http.StatusCreated,
		},
		{
			name: "publish - version already exists",
			path: "/foo/v0.1/publish",
			body: `{"name":"com.example/test-server","version":"1.0.0"}`,
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().PublishServerVersion(gomock.Any(), gomock.Any()).
					Return(nil, service.ErrVersionAlreadyExists)
			},
			setupRouter: func(mockSvc *mocks.MockRegistryService) http.Handler {
				return Router(mockSvc, false)
			},
			wantStatus:    http.StatusConflict,
			expectedError: "version already exists",
		},
		{
			name: "publish - registry not found",
			path: "/nonexistent/v0.1/publish",
			body: `{"name":"com.example/test-server","version":"1.0.0"}`,
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().PublishServerVersion(gomock.Any(), gomock.Any()).
					Return(nil, service.ErrRegistryNotFound)
			},
			setupRouter: func(mockSvc *mocks.MockRegistryService) http.Handler {
				return Router(mockSvc, false)
			},
			wantStatus:    http.StatusNotFound,
			expectedError: "registry not found",
		},
		{
			name: "publish - not a managed registry",
			path: "/remote-registry/v0.1/publish",
			body: `{"name":"com.example/test-server","version":"1.0.0"}`,
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().PublishServerVersion(gomock.Any(), gomock.Any()).
					Return(nil, service.ErrNotManagedRegistry)
			},
			setupRouter: func(mockSvc *mocks.MockRegistryService) http.Handler {
				return Router(mockSvc, false)
			},
			wantStatus:    http.StatusForbidden,
			expectedError: "registry is not managed",
		},
		{
			name:       "publish - missing server name",
			path:       "/foo/v0.1/publish",
			body:       `{"version":"1.0.0"}`,
			setupMocks: func(_ *mocks.MockRegistryService) {},
			setupRouter: func(mockSvc *mocks.MockRegistryService) http.Handler {
				return Router(mockSvc, false)
			},
			wantStatus:    http.StatusBadRequest,
			expectedError: "server name cannot be empty",
		},
		{
			name:       "publish - missing version",
			path:       "/foo/v0.1/publish",
			body:       `{"name":"com.example/test-server"}`,
			setupMocks: func(_ *mocks.MockRegistryService) {},
			setupRouter: func(mockSvc *mocks.MockRegistryService) http.Handler {
				return Router(mockSvc, false)
			},
			wantStatus:    http.StatusBadRequest,
			expectedError: "Server version is required",
		},
		{
			name:       "publish - empty server name",
			path:       "/foo/v0.1/publish",
			body:       `{"name":"   ","version":"1.0.0"}`,
			setupMocks: func(_ *mocks.MockRegistryService) {},
			setupRouter: func(mockSvc *mocks.MockRegistryService) http.Handler {
				return Router(mockSvc, false)
			},
			wantStatus:    http.StatusBadRequest,
			expectedError: "server name cannot be empty",
		},
		{
			name:       "publish - empty version",
			path:       "/foo/v0.1/publish",
			body:       `{"name":"com.example/test-server","version":"   "}`,
			setupMocks: func(_ *mocks.MockRegistryService) {},
			setupRouter: func(mockSvc *mocks.MockRegistryService) http.Handler {
				return Router(mockSvc, false)
			},
			wantStatus:    http.StatusBadRequest,
			expectedError: "Server version is required",
		},
		{
			name:       "publish - disabled aggregated endpoints",
			path:       "/v0.1/publish",
			body:       `{"name":"test","version":"1.0.0"}`,
			setupMocks: func(_ *mocks.MockRegistryService) {},
			setupRouter: func(mockSvc *mocks.MockRegistryService) http.Handler {
				return Router(mockSvc, false)
			},
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var body *strings.Reader
			if tt.body != "" {
				body = strings.NewReader(tt.body)
			} else {
				body = strings.NewReader("")
			}
			req, err := http.NewRequest("POST", tt.path, body)
			require.NoError(t, err)

			mockSvc := mocks.NewMockRegistryService(ctrl)
			tt.setupMocks(mockSvc)
			router := tt.setupRouter(mockSvc)

			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)

			assert.Equal(t, tt.wantStatus, rr.Code)

			if tt.wantStatus == http.StatusCreated {
				var response upstreamv0.ServerJSON
				err = json.Unmarshal(rr.Body.Bytes(), &response)
				require.NoError(t, err)
				assert.Equal(t, "com.example/test-server", response.Name)
				assert.Equal(t, "1.0.0", response.Version)
				return
			}

			if tt.wantStatus != http.StatusNotFound {
				var response map[string]string
				err = json.Unmarshal(rr.Body.Bytes(), &response)
				require.NoError(t, err)
				assert.Contains(t, response, "error")
				if tt.expectedError != "" {
					assert.Contains(t, response["error"], tt.expectedError)
				}
			}

			assert.Equal(t, tt.wantStatus, rr.Code)
		})
	}
}

func TestURLEncodingInRoutes(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	tests := []struct {
		name        string
		path        string
		setupMocks  func(*mocks.MockRegistryService)
		wantStatus  int
		wantError   string
		description string
	}{
		// ListVersions with URL encoding
		{
			name:        "list versions - URL encoded slash in server name",
			path:        "/v0.1/servers/test%2Fserver/versions",
			description: "Should decode test%2Fserver to test/server and pass to service",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().ListServerVersions(gomock.Any(), gomock.Any()).
					Return([]*upstreamv0.ServerJSON{}, nil)
			},
			wantStatus: http.StatusOK,
		},
		{
			name:        "list versions - URL encoded at symbol (invalid)",
			path:        "/v0.1/servers/test%40v1/versions",
			description: "Should reject @ symbol in server name",
			setupMocks:  func(_ *mocks.MockRegistryService) {},
			wantStatus:  http.StatusBadRequest,
			wantError:   "invalid server name format: server name must be in format 'dns-namespace/name' (e.g., 'com.example.api/server')",
		},
		{
			name:        "list versions - URL encoded colon (invalid)",
			path:        "/v0.1/servers/test%3Aserver/versions",
			description: "Should reject colon in server name",
			setupMocks:  func(_ *mocks.MockRegistryService) {},
			wantStatus:  http.StatusBadRequest,
			wantError:   "invalid server name format: server name must be in format 'dns-namespace/name' (e.g., 'com.example.api/server')",
		},
		{
			name:        "list versions - multiple URL encoded chars (invalid)",
			path:        "/v0.1/servers/test%2Fserver%40v1%2B2/versions",
			description: "Should reject invalid characters in name part",
			setupMocks:  func(_ *mocks.MockRegistryService) {},
			wantStatus:  http.StatusBadRequest,
			wantError:   "invalid server name format: name 'server@v1+2' is invalid. Name must start and end with alphanumeric characters, and may contain dots, underscores, and hyphens in the middle",
		},
		{
			name:        "list versions - URL encoded space in middle",
			path:        "/v0.1/servers/test%20server/versions",
			description: "Should reject whitespace in parameter",
			setupMocks:  func(_ *mocks.MockRegistryService) {},
			wantStatus:  http.StatusBadRequest,
			wantError:   "serverName cannot contain whitespace",
		},
		{
			name:        "list versions - URL encoded tab",
			path:        "/v0.1/servers/test%09server/versions",
			description: "Should reject tab character",
			setupMocks:  func(_ *mocks.MockRegistryService) {},
			wantStatus:  http.StatusBadRequest,
			wantError:   "serverName cannot contain whitespace",
		},
		{
			name:        "list versions - URL encoded newline",
			path:        "/v0.1/servers/test%0Aserver/versions",
			description: "Should reject newline character",
			setupMocks:  func(_ *mocks.MockRegistryService) {},
			wantStatus:  http.StatusBadRequest,
			wantError:   "serverName cannot contain whitespace",
		},
		{
			name:        "list versions - URL encoded space only",
			path:        "/v0.1/servers/%20/versions",
			description: "Should reject empty parameter",
			setupMocks:  func(_ *mocks.MockRegistryService) {},
			wantStatus:  http.StatusBadRequest,
			wantError:   "serverName cannot be empty",
		},

		// GetVersion with URL encoding
		{
			name:        "get version - URL encoded slash in server name",
			path:        "/v0.1/servers/test%2Fserver/versions/1.0.0",
			description: "Should decode server name properly",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().GetServerVersion(gomock.Any(), gomock.Any()).
					Return(&upstreamv0.ServerJSON{}, nil)
			},
			wantStatus: http.StatusOK,
		},
		{
			name:        "get version - URL encoded slash in version",
			path:        "/v0.1/servers/com.example%2Ftest-server/versions/1.0%2F0",
			description: "Should decode version properly",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().GetServerVersion(gomock.Any(), gomock.Any()).
					Return(&upstreamv0.ServerJSON{}, nil)
			},
			wantStatus: http.StatusOK,
		},
		{
			name:        "get version - URL encoded at in version",
			path:        "/v0.1/servers/com.example%2Ftest-server/versions/v1%40latest",
			description: "Should decode version with @ symbol",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().GetServerVersion(gomock.Any(), gomock.Any()).
					Return(&upstreamv0.ServerJSON{}, nil)
			},
			wantStatus: http.StatusOK,
		},
		{
			name:        "get version - URL encoded space in server",
			path:        "/v0.1/servers/test%20server/versions/1.0.0",
			description: "Should reject whitespace",
			setupMocks:  func(_ *mocks.MockRegistryService) {},
			wantStatus:  http.StatusBadRequest,
			wantError:   "serverName cannot contain whitespace",
		},
		{
			name:        "get version - URL encoded space in version",
			path:        "/v0.1/servers/com.example%2Ftest-server/versions/1.0%20.0",
			description: "Should reject whitespace in version",
			setupMocks:  func(_ *mocks.MockRegistryService) {},
			wantStatus:  http.StatusBadRequest,
			wantError:   "version cannot contain whitespace",
		},
		{
			name:        "get version - empty encoded server",
			path:        "/v0.1/servers/%20%20/versions/1.0.0",
			description: "Should reject empty server name",
			setupMocks:  func(_ *mocks.MockRegistryService) {},
			wantStatus:  http.StatusBadRequest,
			wantError:   "serverName cannot be empty",
		},
		{
			name:        "get version - empty encoded version",
			path:        "/v0.1/servers/com.example%2Ftest-server/versions/%09",
			description: "Should reject empty version",
			setupMocks:  func(_ *mocks.MockRegistryService) {},
			wantStatus:  http.StatusBadRequest,
			wantError:   "version cannot be empty",
		},

		// With registry name prefix
		{
			name:        "with registry - URL encoded slash in registry name",
			path:        "/test%2Fregistry/v0.1/servers/org.example%2Fmy-server/versions",
			description: "Should decode registry name properly",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().ListServerVersions(gomock.Any(), gomock.Any()).
					Return([]*upstreamv0.ServerJSON{}, nil)
			},
			wantStatus: http.StatusOK,
		},
		{
			name:        "with registry - URL encoded space in registry",
			path:        "/test%20registry/v0.1/servers/org.example%2Fmy-server/versions",
			description: "Should reject whitespace in registry name",
			setupMocks:  func(_ *mocks.MockRegistryService) {},
			wantStatus:  http.StatusBadRequest,
			wantError:   "registryName cannot contain whitespace",
		},
		{
			name:        "with registry - empty encoded registry",
			path:        "/%20/v0.1/servers/org.example%2Fmy-server/versions",
			description: "Should reject empty registry name",
			setupMocks:  func(_ *mocks.MockRegistryService) {},
			wantStatus:  http.StatusBadRequest,
			wantError:   "registryName cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req, err := http.NewRequest("GET", tt.path, nil)
			require.NoError(t, err)

			mockSvc := mocks.NewMockRegistryService(ctrl)
			tt.setupMocks(mockSvc)
			router := Router(mockSvc, true)

			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)

			assert.Equal(t, tt.wantStatus, rr.Code, "Status code mismatch for %s", tt.description)

			if tt.wantStatus == http.StatusBadRequest && tt.wantError != "" {
				var response map[string]interface{}
				err = json.Unmarshal(rr.Body.Bytes(), &response)
				require.NoError(t, err)
				assert.Contains(t, response, "error")
				assert.Equal(t, tt.wantError, response["error"], "Error message mismatch")
			}

			if tt.wantStatus == http.StatusOK {
				// Verify we got a valid response
				var response interface{}
				err = json.Unmarshal(rr.Body.Bytes(), &response)
				require.NoError(t, err, "Response should be valid JSON")
			}
		})
	}
}

func TestDeleteVersion(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	tests := []struct {
		name          string
		path          string
		setupMocks    func(*mocks.MockRegistryService)
		setupRouter   func(*mocks.MockRegistryService) http.Handler
		wantStatus    int
		expectedError string
	}{
		{
			name: "delete - success",
			path: "/foo/v0.1/servers/test%2Fserver/versions/1.0.0",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().DeleteServerVersion(gomock.Any(), gomock.Any()).
					Return(nil)
			},
			setupRouter: func(mockSvc *mocks.MockRegistryService) http.Handler {
				return Router(mockSvc, false)
			},
			wantStatus: http.StatusNoContent,
		},
		{
			name: "delete - server not found",
			path: "/foo/v0.1/servers/com.example%2Fnonexistent/versions/1.0.0",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().DeleteServerVersion(gomock.Any(), gomock.Any()).
					Return(service.ErrServerNotFound)
			},
			setupRouter: func(mockSvc *mocks.MockRegistryService) http.Handler {
				return Router(mockSvc, false)
			},
			wantStatus:    http.StatusNotFound,
			expectedError: "server not found",
		},
		{
			name: "delete - registry not found",
			path: "/nonexistent/v0.1/servers/com.example%2Ftest-server/versions/1.0.0",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().DeleteServerVersion(gomock.Any(), gomock.Any()).
					Return(service.ErrRegistryNotFound)
			},
			setupRouter: func(mockSvc *mocks.MockRegistryService) http.Handler {
				return Router(mockSvc, false)
			},
			wantStatus:    http.StatusNotFound,
			expectedError: "registry not found",
		},
		{
			name: "delete - not a managed registry",
			path: "/remote-registry/v0.1/servers/com.example%2Ftest-server/versions/1.0.0",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().DeleteServerVersion(gomock.Any(), gomock.Any()).
					Return(service.ErrNotManagedRegistry)
			},
			setupRouter: func(mockSvc *mocks.MockRegistryService) http.Handler {
				return Router(mockSvc, false)
			},
			wantStatus:    http.StatusForbidden,
			expectedError: "registry is not managed",
		},
		{
			name:       "delete - empty registry name",
			path:       "/%20/v0.1/servers/com.example%2Ftest-server/versions/1.0.0",
			setupMocks: func(_ *mocks.MockRegistryService) {},
			setupRouter: func(mockSvc *mocks.MockRegistryService) http.Handler {
				return Router(mockSvc, false)
			},
			wantStatus:    http.StatusBadRequest,
			expectedError: "registryName cannot be empty",
		},
		{
			name:       "delete - empty server name",
			path:       "/foo/v0.1/servers/%20/versions/1.0.0",
			setupMocks: func(_ *mocks.MockRegistryService) {},
			setupRouter: func(mockSvc *mocks.MockRegistryService) http.Handler {
				return Router(mockSvc, false)
			},
			wantStatus:    http.StatusBadRequest,
			expectedError: "serverName cannot be empty",
		},
		{
			name:       "delete - empty version",
			path:       "/foo/v0.1/servers/com.example%2Ftest-server/versions/%20",
			setupMocks: func(_ *mocks.MockRegistryService) {},
			setupRouter: func(mockSvc *mocks.MockRegistryService) http.Handler {
				return Router(mockSvc, false)
			},
			wantStatus:    http.StatusBadRequest,
			expectedError: "version cannot be empty",
		},
		{
			name:       "delete - disabled aggregated endpoints",
			path:       "/v0.1/servers/foo/versions/1.0.0",
			setupMocks: func(_ *mocks.MockRegistryService) {},
			setupRouter: func(mockSvc *mocks.MockRegistryService) http.Handler {
				return Router(mockSvc, false)
			},
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req, err := http.NewRequest("DELETE", tt.path, nil)
			require.NoError(t, err)

			mockSvc := mocks.NewMockRegistryService(ctrl)
			tt.setupMocks(mockSvc)
			router := tt.setupRouter(mockSvc)

			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)

			assert.Equal(t, tt.wantStatus, rr.Code)

			if tt.wantStatus != http.StatusNoContent && tt.wantStatus != http.StatusNotFound {
				var response map[string]string
				err = json.Unmarshal(rr.Body.Bytes(), &response)
				require.NoError(t, err)
				assert.Contains(t, response, "error")
				if tt.expectedError != "" {
					assert.Contains(t, response["error"], tt.expectedError)
				}
			}

			assert.Equal(t, tt.wantStatus, rr.Code)
		})
	}
}
