package v0_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	toolhivetypes "github.com/stacklok/toolhive/pkg/registry/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	v0 "github.com/stacklok/toolhive-registry-server/internal/api/v0"
	"github.com/stacklok/toolhive-registry-server/internal/service/mocks"
)

func TestHealthRouter(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	mockSvc := mocks.NewMockRegistryService(ctrl)
	// Set up expectations for readiness check
	mockSvc.EXPECT().CheckReadiness(gomock.Any()).Return(nil).AnyTimes()

	router := v0.HealthRouter(mockSvc)

	tests := []struct {
		name       string
		path       string
		method     string
		wantStatus int
	}{
		{
			name:       "health endpoint",
			path:       "/health",
			method:     "GET",
			wantStatus: http.StatusOK,
		},
		{
			name:       "readiness endpoint - ready",
			path:       "/readiness",
			method:     "GET",
			wantStatus: http.StatusOK,
		},
		{
			name:       "version endpoint",
			path:       "/version",
			method:     "GET",
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req, err := http.NewRequest(tt.method, tt.path, nil)
			require.NoError(t, err)

			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)

			assert.Equal(t, tt.wantStatus, rr.Code)
		})
	}
}

func TestRegistryRouter(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	mockSvc := mocks.NewMockRegistryService(ctrl)
	// Set up expectations for all routes
	mockSvc.EXPECT().GetRegistry(gomock.Any()).Return(&toolhivetypes.UpstreamRegistry{
		Version:     "1.0.0",
		LastUpdated: time.Now().Format(time.RFC3339),
		Servers:     []upstreamv0.ServerJSON{},
	}, "test", nil).AnyTimes()
	router := v0.Router(mockSvc)

	tests := []struct {
		name       string
		path       string
		method     string
		wantStatus int
	}{
		{
			name:       "registry info",
			path:       "/info",
			method:     "GET",
			wantStatus: http.StatusOK,
		},
		{
			name:       "Open API YAML",
			path:       "/openapi.yaml",
			method:     "GET",
			wantStatus: http.StatusOK,
		},
		{
			name:       "list servers",
			path:       "/servers",
			method:     "GET",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "get server",
			path:       "/servers/test-server",
			method:     "GET",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "list deployed servers",
			path:       "/servers/deployed",
			method:     "GET",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "get deployed server",
			path:       "/servers/deployed/test-server",
			method:     "GET",
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req, err := http.NewRequest(tt.method, tt.path, nil)
			require.NoError(t, err)

			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)

			assert.Equal(t, tt.wantStatus, rr.Code)
		})
	}
}

// TestOpenAPIEndpoint tests the OpenAPI YAML endpoint
func TestOpenAPIEndpoint(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	mockSvc := mocks.NewMockRegistryService(ctrl)
	router := v0.Router(mockSvc)

	req, err := http.NewRequest("GET", "/openapi.yaml", nil)
	require.NoError(t, err)

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "application/x-yaml", rr.Header().Get("Content-Type"))
	assert.Greater(t, len(rr.Body.String()), 0, "OpenAPI YAML should not be empty")
}

// TestReadinessWithServiceError tests readiness endpoint when service has errors
func TestReadinessWithServiceError(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	mockSvc := mocks.NewMockRegistryService(ctrl)
	mockSvc.EXPECT().CheckReadiness(gomock.Any()).Return(assert.AnError)

	router := v0.HealthRouter(mockSvc)
	req, err := http.NewRequest("GET", "/readiness", nil)
	require.NoError(t, err)

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusServiceUnavailable, rr.Code)

	var response map[string]interface{}
	err = json.Unmarshal(rr.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.Contains(t, response, "error")
}
