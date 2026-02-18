package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/internal/api"
	"github.com/stacklok/toolhive-registry-server/internal/service/mocks"
)

func TestHealthEndpoint(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	mockSvc := mocks.NewMockRegistryService(ctrl)
	// No expectations needed - health check doesn't call service
	server := api.NewServer(mockSvc)

	req, err := http.NewRequest("GET", "/health", nil)
	require.NoError(t, err)

	rr := httptest.NewRecorder()
	server.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))

	var response map[string]string
	err = json.Unmarshal(rr.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.Equal(t, "healthy", response["status"])
}

func TestReadinessEndpoint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		setupMock      func(*mocks.MockRegistryService)
		expectedStatus int
		expectedBody   string
	}{
		{
			name: "service ready",
			setupMock: func(m *mocks.MockRegistryService) {
				m.EXPECT().CheckReadiness(gomock.Any()).Return(nil)
			},
			expectedStatus: http.StatusOK,
			expectedBody:   "ready",
		},
		{
			name: "service not ready",
			setupMock: func(m *mocks.MockRegistryService) {
				m.EXPECT().CheckReadiness(gomock.Any()).Return(fmt.Errorf("service not initialized"))
			},
			expectedStatus: http.StatusServiceUnavailable,
			expectedBody:   "error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			t.Cleanup(ctrl.Finish)

			mockSvc := mocks.NewMockRegistryService(ctrl)
			tt.setupMock(mockSvc)

			server := api.NewServer(mockSvc)

			req, err := http.NewRequest("GET", "/readiness", nil)
			require.NoError(t, err)

			rr := httptest.NewRecorder()
			server.ServeHTTP(rr, req)

			assert.Equal(t, tt.expectedStatus, rr.Code)
			assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))

			var response map[string]string
			err = json.Unmarshal(rr.Body.Bytes(), &response)
			require.NoError(t, err)

			if tt.expectedStatus == http.StatusOK {
				assert.Equal(t, tt.expectedBody, response["status"])
			} else {
				assert.Contains(t, response, tt.expectedBody)
			}
		})
	}
}

func TestVersionEndpoint(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	mockSvc := mocks.NewMockRegistryService(ctrl)
	// No expectations needed - version check doesn't call service
	server := api.NewServer(mockSvc)

	req, err := http.NewRequest("GET", "/version", nil)
	require.NoError(t, err)

	rr := httptest.NewRecorder()
	server.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))

	var response map[string]string
	err = json.Unmarshal(rr.Body.Bytes(), &response)
	require.NoError(t, err)

	// Version info should contain these fields
	assert.Contains(t, response, "version")
	assert.Contains(t, response, "commit")
	assert.Contains(t, response, "build_date")
	assert.Contains(t, response, "go_version")
	assert.Contains(t, response, "platform")
}

func TestOpenAPIEndpoint(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	mockSvc := mocks.NewMockRegistryService(ctrl)
	server := api.NewServer(mockSvc)

	req, err := http.NewRequest("GET", "/openapi.json", nil)
	require.NoError(t, err)

	rr := httptest.NewRecorder()
	server.ServeHTTP(rr, req)

	// Should return 200 OK with OpenAPI spec
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))

	// Validate the response is valid JSON and contains OpenAPI spec fields
	var spec map[string]any
	err = json.Unmarshal(rr.Body.Bytes(), &spec)
	require.NoError(t, err)

	// Check for required OpenAPI 3.1.0 fields
	assert.Equal(t, "3.1.0", spec["openapi"])
	assert.Contains(t, spec, "info")
	assert.Contains(t, spec, "paths")

	// Verify info section
	info, ok := spec["info"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "ToolHive Registry API", info["title"])
	assert.Equal(t, "0.1", info["version"])

	// Verify paths section contains our endpoint
	paths, ok := spec["paths"].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, paths, "/openapi.json")
}
