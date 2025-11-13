package v01

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/internal/service/mocks"
)

func TestListServers(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	mockSvc := mocks.NewMockRegistryService(ctrl)
	router := Router(mockSvc)

	tests := []struct {
		name       string
		path       string
		wantStatus int
	}{
		{
			name:       "list servers - basic",
			path:       "/v0.1/servers",
			wantStatus: http.StatusOK,
		},
		{
			name:       "list servers - with cursor",
			path:       "/v0.1/servers?cursor=abc123",
			wantStatus: http.StatusOK,
		},
		{
			name:       "list servers - with limit",
			path:       "/v0.1/servers?limit=10",
			wantStatus: http.StatusOK,
		},
		{
			name:       "list servers - with search",
			path:       "/v0.1/servers?search=test",
			wantStatus: http.StatusOK,
		},
		{
			name:       "list servers - with updated_since",
			path:       "/v0.1/servers?updated_since=2025-01-01T00:00:00Z",
			wantStatus: http.StatusOK,
		},
		{
			name:       "list servers - with version",
			path:       "/v0.1/servers?version=latest",
			wantStatus: http.StatusOK,
		},
		{
			name:       "list servers - invalid limit",
			path:       "/v0.1/servers?limit=invalid",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "list servers - invalid updated_since",
			path:       "/v0.1/servers?updated_since=invalid",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "list servers with registry name - basic",
			path:       "/foo/v0.1/servers",
			wantStatus: http.StatusOK,
		},
		{
			name:       "list servers with registry name - with cursor",
			path:       "/foo/v0.1/servers?cursor=abc123",
			wantStatus: http.StatusOK,
		},
		{
			name:       "list servers with registry name - with limit",
			path:       "/foo/v0.1/servers?limit=10",
			wantStatus: http.StatusOK,
		},
		{
			name:       "list servers with registry name - with search",
			path:       "/foo/v0.1/servers?search=test",
			wantStatus: http.StatusOK,
		},
		{
			name:       "list servers with registry name - with updated_since",
			path:       "/foo/v0.1/servers?updated_since=2025-01-01T00:00:00Z",
			wantStatus: http.StatusOK,
		},
		{
			name:       "list servers with registry name - with version",
			path:       "/foo/v0.1/servers?version=latest",
			wantStatus: http.StatusOK,
		},
		{
			name:       "list servers with registry name - invalid limit",
			path:       "/foo/v0.1/servers?limit=invalid",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "list servers with registry name - invalid updated_since",
			path:       "/foo/v0.1/servers?updated_since=invalid",
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req, err := http.NewRequest("GET", tt.path, nil)
			require.NoError(t, err)

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

	mockSvc := mocks.NewMockRegistryService(ctrl)
	router := Router(mockSvc)

	tests := []struct {
		name       string
		path       string
		wantStatus int
	}{
		{
			name:       "list versions - valid server name",
			path:       "/v0.1/servers/test-server/versions",
			wantStatus: http.StatusOK,
		},
		{
			name:       "list versions - empty server name",
			path:       "/v0.1/servers//versions",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "list versions - empty server name",
			path:       "/v0.1/servers/%20/versions",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "list versions with registry name - valid server name",
			path:       "/foo/v0.1/servers/test-server/versions",
			wantStatus: http.StatusOK,
		},
		{
			name:       "list versions with registry name - empty server name",
			path:       "/foo/v0.1/servers//versions",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "list versions with registry name - empty server name",
			path:       "/foo/v0.1/servers/%20/versions",
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req, err := http.NewRequest("GET", tt.path, nil)
			require.NoError(t, err)

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

	mockSvc := mocks.NewMockRegistryService(ctrl)
	router := Router(mockSvc)

	tests := []struct {
		name       string
		path       string
		wantStatus int
	}{
		{
			name:       "get version - valid server and version",
			path:       "/v0.1/servers/test-server/versions/1.0.0",
			wantStatus: http.StatusOK,
		},
		{
			name:       "get version - latest",
			path:       "/v0.1/servers/test-server/versions/latest",
			wantStatus: http.StatusOK,
		},
		{
			name:       "get version - empty server name",
			path:       "/v0.1/servers//versions/1.0.0",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "get version - empty version",
			path:       "/v0.1/servers/test-server/versions/",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "get version - empty server name",
			path:       "/v0.1/servers/%20/versions/1.0.0",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "get version - empty version",
			path:       "/v0.1/servers/test-server/versions/%20",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "get version with registry name - valid server and version",
			path:       "/foo/v0.1/servers/test-server/versions/1.0.0",
			wantStatus: http.StatusOK,
		},
		{
			name:       "get version with registry name - latest",
			path:       "/foo/v0.1/servers/test-server/versions/latest",
			wantStatus: http.StatusOK,
		},
		{
			name:       "get version with registry name - empty server name",
			path:       "/foo/v0.1/servers//versions/1.0.0",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "get version with registry name - empty version",
			path:       "/foo/v0.1/servers/test-server/versions/",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "get version with registry name - empty server name",
			path:       "/foo/v0.1/servers/%20/versions/1.0.0",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "get version with registry name - empty version",
			path:       "/foo/v0.1/servers/test-server/versions/%20",
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req, err := http.NewRequest("GET", tt.path, nil)
			require.NoError(t, err)

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

	mockSvc := mocks.NewMockRegistryService(ctrl)
	router := Router(mockSvc)

	tests := []struct {
		name       string
		path       string
		wantStatus int
	}{
		{
			name:       "publish - basic",
			path:       "/v0.1/publish",
			wantStatus: http.StatusNotImplemented,
		},
		{
			name:       "publish with registry name - basic",
			path:       "/foo/v0.1/publish",
			wantStatus: http.StatusNotImplemented,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req, err := http.NewRequest("POST", tt.path, nil)
			require.NoError(t, err)

			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)

			assert.Equal(t, tt.wantStatus, rr.Code)

			var response map[string]string
			err = json.Unmarshal(rr.Body.Bytes(), &response)
			require.NoError(t, err)
			assert.Contains(t, response, "error")
			assert.Equal(t, "Publishing is not supported", response["error"])
		})
	}
}
