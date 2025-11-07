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
			path:       "/servers",
			wantStatus: http.StatusOK,
		},
		{
			name:       "list servers - with cursor",
			path:       "/servers?cursor=abc123",
			wantStatus: http.StatusOK,
		},
		{
			name:       "list servers - with limit",
			path:       "/servers?limit=10",
			wantStatus: http.StatusOK,
		},
		{
			name:       "list servers - with search",
			path:       "/servers?search=test",
			wantStatus: http.StatusOK,
		},
		{
			name:       "list servers - with updated_since",
			path:       "/servers?updated_since=2025-01-01T00:00:00Z",
			wantStatus: http.StatusOK,
		},
		{
			name:       "list servers - with version",
			path:       "/servers?version=latest",
			wantStatus: http.StatusOK,
		},
		{
			name:       "list servers - invalid limit",
			path:       "/servers?limit=invalid",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "list servers - invalid updated_since",
			path:       "/servers?updated_since=invalid",
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
			path:       "/servers/test-server/versions",
			wantStatus: http.StatusOK,
		},
		{
			name:       "list versions - empty server name",
			path:       "/servers//versions",
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
			path:       "/servers/test-server/versions/1.0.0",
			wantStatus: http.StatusOK,
		},
		{
			name:       "get version - latest",
			path:       "/servers/test-server/versions/latest",
			wantStatus: http.StatusOK,
		},
		{
			name:       "get version - empty server name",
			path:       "/servers//versions/1.0.0",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "get version - empty version",
			path:       "/servers/test-server/versions/",
			wantStatus: http.StatusNotFound,
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

	req, err := http.NewRequest("POST", "/publish", nil)
	require.NoError(t, err)

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNotImplemented, rr.Code)

	var response map[string]string
	err = json.Unmarshal(rr.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.Contains(t, response, "error")
	assert.Equal(t, "Publishing is not supported", response["error"])
}
