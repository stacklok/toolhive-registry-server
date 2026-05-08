package api_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/internal/api"
	"github.com/stacklok/toolhive-registry-server/internal/auth"
	"github.com/stacklok/toolhive-registry-server/internal/service/mocks"
)

func TestHealthEndpoint(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	mockSvc := mocks.NewMockRegistryService(ctrl)
	// No expectations needed - health check doesn't call service
	server := api.NewInternalServer(mockSvc)

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

			server := api.NewInternalServer(mockSvc)

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
	server := api.NewInternalServer(mockSvc)

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

// captureSlog redirects slog output to a buffer for the duration of fn,
// then returns parsed JSON log records (one per emitted line).
func captureSlog(t *testing.T, fn func()) []map[string]any {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(prev)

	fn()

	var records []map[string]any
	for _, line := range bytes.Split(bytes.TrimRight(buf.Bytes(), "\n"), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var rec map[string]any
		require.NoError(t, json.Unmarshal(line, &rec))
		records = append(records, rec)
	}
	return records
}

// findRecord returns the first slog record whose msg matches.
func findRecord(t *testing.T, records []map[string]any, msg string) map[string]any {
	t.Helper()
	for _, r := range records {
		if r["msg"] == msg {
			return r
		}
	}
	t.Fatalf("no log record with msg=%q found; got %+v", msg, records)
	return nil
}

// TestLoggingMiddleware_AnonymousRequest verifies the access log emits
// sub: "anonymous" (with empty user) when no auth middleware populates
// the identity holder, and that all pre-existing fields are preserved.
//
// Not t.Parallel: captureSlog mutates the process-global slog.Default(),
// so running these tests concurrently would cross-pollute their buffers.
func TestLoggingMiddleware_AnonymousRequest(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	wrapped := api.LoggingMiddleware(handler)

	records := captureSlog(t, func() {
		req := httptest.NewRequest(http.MethodGet, "/registry/demo/v0.1/servers", nil)
		req.RemoteAddr = "10.0.0.1:5555"
		rr := httptest.NewRecorder()
		wrapped.ServeHTTP(rr, req)
	})

	rec := findRecord(t, records, "HTTP request")
	assert.Equal(t, "anonymous", rec["sub"])
	assert.Equal(t, "", rec["user"])
	assert.Equal(t, "GET", rec["method"])
	assert.Equal(t, "/registry/demo/v0.1/servers", rec["path"])
	assert.EqualValues(t, http.StatusOK, rec["status"])
	assert.Equal(t, "10.0.0.1:5555", rec["remote_addr"])
	require.Contains(t, rec, "duration_ms")
	require.Contains(t, rec, "response_bytes")
}

// TestLoggingMiddleware_AuthenticatedRequest proves the holder pattern:
// an inner handler (standing in for the auth middleware) calls
// auth.SetIdentity on the request context, and the outer access log
// reads those values back even though the inner ctx is a child branch.
// This is the exact middleware-ordering wrinkle the change is meant to
// solve.
func TestLoggingMiddleware_AuthenticatedRequest(t *testing.T) {
	innerHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Stand-in for the auth middleware: write identity into the holder
		// installed by LoggingMiddleware.
		auth.SetIdentity(r.Context(), "user-uuid-1234", "Alice Example")
		w.WriteHeader(http.StatusOK)
	})
	wrapped := api.LoggingMiddleware(innerHandler)

	records := captureSlog(t, func() {
		req := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
		rr := httptest.NewRecorder()
		wrapped.ServeHTTP(rr, req)
	})

	rec := findRecord(t, records, "HTTP request")
	assert.Equal(t, "user-uuid-1234", rec["sub"])
	assert.Equal(t, "Alice Example", rec["user"])
}

// TestLoggingMiddleware_ErrorStatus verifies sub/user are still populated
// when the handler returns 4xx/5xx — the log level changes but identity
// fields remain on the line.
func TestLoggingMiddleware_ErrorStatus(t *testing.T) {
	innerHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth.SetIdentity(r.Context(), "user-1", "Bob")
		w.WriteHeader(http.StatusForbidden)
	})
	wrapped := api.LoggingMiddleware(innerHandler)

	records := captureSlog(t, func() {
		req := httptest.NewRequest(http.MethodPost, "/v1/sources", nil)
		rr := httptest.NewRecorder()
		wrapped.ServeHTTP(rr, req)
	})

	rec := findRecord(t, records, "HTTP request")
	assert.EqualValues(t, http.StatusForbidden, rec["status"])
	assert.Equal(t, "WARN", rec["level"])
	assert.Equal(t, "user-1", rec["sub"])
	assert.Equal(t, "Bob", rec["user"])
}
