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
		name       string
		path       string
		setupMocks func(*mocks.MockRegistryService)
		wantStatus int
	}{
		{
			name: "list servers - basic",
			path: "/v0.1/servers",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().ListServers(gomock.Any(), gomock.Any()).Return([]*upstreamv0.ServerJSON{}, nil).AnyTimes()
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "list servers - with cursor",
			path: "/v0.1/servers?cursor=abc123",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().ListServers(gomock.Any(), gomock.Any()).Return([]*upstreamv0.ServerJSON{}, nil).AnyTimes()
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "list servers - with limit",
			path: "/v0.1/servers?limit=10",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().ListServers(gomock.Any(), gomock.Any()).Return([]*upstreamv0.ServerJSON{}, nil).AnyTimes()
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "list servers - with search",
			path: "/v0.1/servers?search=test",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().ListServers(gomock.Any(), gomock.Any()).Return([]*upstreamv0.ServerJSON{}, nil).AnyTimes()
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "list servers - with updated_since",
			path: "/v0.1/servers?updated_since=2025-01-01T00:00:00Z",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().ListServers(gomock.Any(), gomock.Any()).Return([]*upstreamv0.ServerJSON{}, nil).AnyTimes()
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "list servers - with version",
			path: "/v0.1/servers?version=latest",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().ListServers(gomock.Any(), gomock.Any()).Return([]*upstreamv0.ServerJSON{}, nil).AnyTimes()
			},
			wantStatus: http.StatusOK,
		},
		{
			name:       "list servers - invalid limit",
			path:       "/v0.1/servers?limit=invalid",
			setupMocks: func(_ *mocks.MockRegistryService) {},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "list servers - invalid updated_since",
			path:       "/v0.1/servers?updated_since=invalid",
			setupMocks: func(_ *mocks.MockRegistryService) {},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "list servers with registry name - basic",
			path: "/foo/v0.1/servers",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().ListServers(gomock.Any(), gomock.Any()).Return([]*upstreamv0.ServerJSON{}, nil).AnyTimes()
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "list servers with registry name - with cursor",
			path: "/foo/v0.1/servers?cursor=abc123",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().ListServers(gomock.Any(), gomock.Any()).Return([]*upstreamv0.ServerJSON{}, nil).AnyTimes()
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "list servers with registry name - with limit",
			path: "/foo/v0.1/servers?limit=10",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().ListServers(gomock.Any(), gomock.Any()).Return([]*upstreamv0.ServerJSON{}, nil).AnyTimes()
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "list servers with registry name - with search",
			path: "/foo/v0.1/servers?search=test",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().ListServers(gomock.Any(), gomock.Any()).Return([]*upstreamv0.ServerJSON{}, nil).AnyTimes()
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "list servers with registry name - with updated_since",
			path: "/foo/v0.1/servers?updated_since=2025-01-01T00:00:00Z",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().ListServers(gomock.Any(), gomock.Any()).Return([]*upstreamv0.ServerJSON{}, nil).AnyTimes()
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "list servers with registry name - with version",
			path: "/foo/v0.1/servers?version=latest",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().ListServers(gomock.Any(), gomock.Any()).Return([]*upstreamv0.ServerJSON{}, nil).AnyTimes()
			},
			wantStatus: http.StatusOK,
		},
		{
			name:       "list servers with registry name - invalid limit",
			path:       "/foo/v0.1/servers?limit=invalid",
			setupMocks: func(_ *mocks.MockRegistryService) {},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "list servers with registry name - invalid updated_since",
			path:       "/foo/v0.1/servers?updated_since=invalid",
			setupMocks: func(_ *mocks.MockRegistryService) {},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req, err := http.NewRequest("GET", tt.path, nil)
			require.NoError(t, err)

			mockSvc := mocks.NewMockRegistryService(ctrl)
			tt.setupMocks(mockSvc)
			router := Router(mockSvc)

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
		name       string
		path       string
		setupMocks func(*mocks.MockRegistryService)
		wantStatus int
	}{
		{
			name: "list versions - valid server name",
			path: "/v0.1/servers/test-server/versions",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().ListServerVersions(gomock.Any(), gomock.Any()).Return([]*upstreamv0.ServerJSON{}, nil).AnyTimes()
			},
			wantStatus: http.StatusOK,
		},
		{
			name:       "list versions - empty server name",
			path:       "/v0.1/servers//versions",
			setupMocks: func(_ *mocks.MockRegistryService) {},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "list versions - empty server name",
			path:       "/v0.1/servers/%20/versions",
			setupMocks: func(_ *mocks.MockRegistryService) {},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "list versions with registry name - valid server name",
			path: "/foo/v0.1/servers/test-server/versions",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().ListServerVersions(gomock.Any(), gomock.Any()).Return([]*upstreamv0.ServerJSON{}, nil).AnyTimes()
			},
			wantStatus: http.StatusOK,
		},
		{
			name:       "list versions with registry name - empty server name",
			path:       "/foo/v0.1/servers//versions",
			setupMocks: func(_ *mocks.MockRegistryService) {},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "list versions with registry name - empty server name",
			path:       "/foo/v0.1/servers/%20/versions",
			setupMocks: func(_ *mocks.MockRegistryService) {},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req, err := http.NewRequest("GET", tt.path, nil)
			require.NoError(t, err)

			mockSvc := mocks.NewMockRegistryService(ctrl)
			tt.setupMocks(mockSvc)
			router := Router(mockSvc)

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
		name       string
		path       string
		setupMocks func(*mocks.MockRegistryService)
		wantStatus int
	}{
		{
			name: "get version - valid server and version",
			path: "/v0.1/servers/test-server/versions/1.0.0",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().GetServerVersion(gomock.Any(), gomock.Any()).Return(&upstreamv0.ServerJSON{}, nil).AnyTimes()
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "get version - latest",
			path: "/v0.1/servers/test-server/versions/latest",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().GetServerVersion(gomock.Any(), gomock.Any()).Return(&upstreamv0.ServerJSON{}, nil).AnyTimes()
			},
			wantStatus: http.StatusOK,
		},
		{
			name:       "get version - empty server name",
			path:       "/v0.1/servers//versions/1.0.0",
			setupMocks: func(_ *mocks.MockRegistryService) {},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "get version - empty version",
			path:       "/v0.1/servers/test-server/versions/",
			setupMocks: func(_ *mocks.MockRegistryService) {},
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "get version - empty server name",
			path:       "/v0.1/servers/%20/versions/1.0.0",
			setupMocks: func(_ *mocks.MockRegistryService) {},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "get version - empty version",
			path:       "/v0.1/servers/test-server/versions/%20",
			setupMocks: func(_ *mocks.MockRegistryService) {},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "get version with registry name - valid server and version",
			path: "/foo/v0.1/servers/test-server/versions/1.0.0",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().GetServerVersion(gomock.Any(), gomock.Any()).Return(&upstreamv0.ServerJSON{}, nil).AnyTimes()
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "get version with registry name - latest",
			path: "/foo/v0.1/servers/test-server/versions/latest",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().GetServerVersion(gomock.Any(), gomock.Any()).Return(&upstreamv0.ServerJSON{}, nil).AnyTimes()
			},
			wantStatus: http.StatusOK,
		},
		{
			name:       "get version with registry name - empty server name",
			path:       "/foo/v0.1/servers//versions/1.0.0",
			setupMocks: func(_ *mocks.MockRegistryService) {},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "get version with registry name - empty version",
			path:       "/foo/v0.1/servers/test-server/versions/",
			setupMocks: func(_ *mocks.MockRegistryService) {},
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "get version with registry name - empty server name",
			path:       "/foo/v0.1/servers/%20/versions/1.0.0",
			setupMocks: func(_ *mocks.MockRegistryService) {},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "get version with registry name - empty version",
			path:       "/foo/v0.1/servers/test-server/versions/%20",
			setupMocks: func(_ *mocks.MockRegistryService) {},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req, err := http.NewRequest("GET", tt.path, nil)
			require.NoError(t, err)

			mockSvc := mocks.NewMockRegistryService(ctrl)
			tt.setupMocks(mockSvc)
			router := Router(mockSvc)

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
		wantStatus    int
		expectedError string
	}{
		{
			name:          "publish - basic (deprecated)",
			path:          "/v0.1/publish",
			body:          `{"name":"test","version":"1.0.0"}`,
			setupMocks:    func(_ *mocks.MockRegistryService) {},
			wantStatus:    http.StatusBadRequest,
			expectedError: "Registry name is required. Use /{registryName}/v0.1/publish endpoint",
		},
		{
			name:          "publish with registry name - missing body",
			path:          "/foo/v0.1/publish",
			body:          ``,
			setupMocks:    func(_ *mocks.MockRegistryService) {},
			wantStatus:    http.StatusBadRequest,
			expectedError: "Invalid request body",
		},
		{
			name: "publish with registry name - success",
			path: "/foo/v0.1/publish",
			body: `{"name":"test/server","version":"1.0.0","description":"Test server"}`,
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().PublishServerVersion(gomock.Any(), gomock.Any()).
					Return(&upstreamv0.ServerJSON{
						Name:        "test/server",
						Version:     "1.0.0",
						Description: "Test server",
					}, nil)
			},
			wantStatus: http.StatusCreated,
		},
		{
			name: "publish - version already exists",
			path: "/foo/v0.1/publish",
			body: `{"name":"test/server","version":"1.0.0"}`,
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().PublishServerVersion(gomock.Any(), gomock.Any()).
					Return(nil, service.ErrVersionAlreadyExists)
			},
			wantStatus:    http.StatusConflict,
			expectedError: "version already exists",
		},
		{
			name: "publish - registry not found",
			path: "/nonexistent/v0.1/publish",
			body: `{"name":"test/server","version":"1.0.0"}`,
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().PublishServerVersion(gomock.Any(), gomock.Any()).
					Return(nil, service.ErrRegistryNotFound)
			},
			wantStatus:    http.StatusNotFound,
			expectedError: "registry not found",
		},
		{
			name: "publish - not a managed registry",
			path: "/remote-registry/v0.1/publish",
			body: `{"name":"test/server","version":"1.0.0"}`,
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().PublishServerVersion(gomock.Any(), gomock.Any()).
					Return(nil, service.ErrNotManagedRegistry)
			},
			wantStatus:    http.StatusForbidden,
			expectedError: "registry is not managed",
		},
		{
			name:          "publish - missing server name",
			path:          "/foo/v0.1/publish",
			body:          `{"version":"1.0.0"}`,
			setupMocks:    func(_ *mocks.MockRegistryService) {},
			wantStatus:    http.StatusBadRequest,
			expectedError: "Server name is required",
		},
		{
			name:          "publish - missing version",
			path:          "/foo/v0.1/publish",
			body:          `{"name":"test/server"}`,
			setupMocks:    func(_ *mocks.MockRegistryService) {},
			wantStatus:    http.StatusBadRequest,
			expectedError: "Server version is required",
		},
		{
			name:          "publish - empty server name",
			path:          "/foo/v0.1/publish",
			body:          `{"name":"   ","version":"1.0.0"}`,
			setupMocks:    func(_ *mocks.MockRegistryService) {},
			wantStatus:    http.StatusBadRequest,
			expectedError: "Server name is required",
		},
		{
			name:          "publish - empty version",
			path:          "/foo/v0.1/publish",
			body:          `{"name":"test/server","version":"   "}`,
			setupMocks:    func(_ *mocks.MockRegistryService) {},
			wantStatus:    http.StatusBadRequest,
			expectedError: "Server version is required",
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
			router := Router(mockSvc)

			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)

			assert.Equal(t, tt.wantStatus, rr.Code)

			if tt.wantStatus == http.StatusCreated {
				var response upstreamv0.ServerJSON
				err = json.Unmarshal(rr.Body.Bytes(), &response)
				require.NoError(t, err)
				assert.Equal(t, "test/server", response.Name)
				assert.Equal(t, "1.0.0", response.Version)
			} else {
				var response map[string]string
				err = json.Unmarshal(rr.Body.Bytes(), &response)
				require.NoError(t, err)
				assert.Contains(t, response, "error")
				if tt.expectedError != "" {
					assert.Contains(t, response["error"], tt.expectedError)
				}
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
			wantStatus: http.StatusNoContent,
		},
		{
			name: "delete - server not found",
			path: "/foo/v0.1/servers/nonexistent/versions/1.0.0",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().DeleteServerVersion(gomock.Any(), gomock.Any()).
					Return(service.ErrServerNotFound)
			},
			wantStatus:    http.StatusNotFound,
			expectedError: "server not found",
		},
		{
			name: "delete - registry not found",
			path: "/nonexistent/v0.1/servers/test/versions/1.0.0",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().DeleteServerVersion(gomock.Any(), gomock.Any()).
					Return(service.ErrRegistryNotFound)
			},
			wantStatus:    http.StatusNotFound,
			expectedError: "registry not found",
		},
		{
			name: "delete - not a managed registry",
			path: "/remote-registry/v0.1/servers/test/versions/1.0.0",
			setupMocks: func(m *mocks.MockRegistryService) {
				m.EXPECT().DeleteServerVersion(gomock.Any(), gomock.Any()).
					Return(service.ErrNotManagedRegistry)
			},
			wantStatus:    http.StatusForbidden,
			expectedError: "registry is not managed",
		},
		{
			name:          "delete - empty registry name",
			path:          "/%20/v0.1/servers/test/versions/1.0.0",
			setupMocks:    func(_ *mocks.MockRegistryService) {},
			wantStatus:    http.StatusBadRequest,
			expectedError: "Registry name is required",
		},
		{
			name:          "delete - empty server name",
			path:          "/foo/v0.1/servers/%20/versions/1.0.0",
			setupMocks:    func(_ *mocks.MockRegistryService) {},
			wantStatus:    http.StatusBadRequest,
			expectedError: "Server name is required",
		},
		{
			name:          "delete - empty version",
			path:          "/foo/v0.1/servers/test/versions/%20",
			setupMocks:    func(_ *mocks.MockRegistryService) {},
			wantStatus:    http.StatusBadRequest,
			expectedError: "Version is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req, err := http.NewRequest("DELETE", tt.path, nil)
			require.NoError(t, err)

			mockSvc := mocks.NewMockRegistryService(ctrl)
			tt.setupMocks(mockSvc)
			router := Router(mockSvc)

			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)

			assert.Equal(t, tt.wantStatus, rr.Code)

			if tt.wantStatus != http.StatusNoContent {
				var response map[string]string
				err = json.Unmarshal(rr.Body.Bytes(), &response)
				require.NoError(t, err)
				assert.Contains(t, response, "error")
				if tt.expectedError != "" {
					assert.Contains(t, response["error"], tt.expectedError)
				}
			}
		})
	}
}
