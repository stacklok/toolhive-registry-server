package audit

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/stacklok/toolhive-core/audit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// readAuditLines parses every JSON line written to path.
func readAuditLines(t *testing.T, path string) []map[string]any {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // path is test-controlled
	require.NoError(t, err)
	var lines []map[string]any
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		var line map[string]any
		require.NoError(t, json.Unmarshal(scanner.Bytes(), &line), scanner.Text())
		lines = append(lines, line)
	}
	require.NoError(t, scanner.Err())
	return lines
}

func TestLogger_Admission(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		decision    string
		wantOutcome string
	}{
		{name: "allow is success", decision: "allow", wantOutcome: audit.OutcomeSuccess},
		{name: "deny is denied", decision: "deny", wantOutcome: audit.OutcomeDenied},
		{name: "error is error", decision: "error", wantOutcome: audit.OutcomeError},
		{name: "unknown decision fails closed as denied", decision: "maybe", wantOutcome: audit.OutcomeDenied},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			logFile := filepath.Join(t.TempDir(), "audit.log")
			logger, err := NewLogger(logFile)
			require.NoError(t, err)

			req := httptest.NewRequest(http.MethodGet, "/registry/main/v0.1/servers", nil)
			req.RemoteAddr = "192.0.2.10:4242"
			req.Header.Set("User-Agent", "thv-test/1.0")
			req = req.WithContext(context.WithValue(req.Context(), middleware.RequestIDKey, "req-123"))
			logger.Admission(req.Context(), req, `https://idp.example|alice`, "catalog.read",
				`Registry::View::"main"`, "sha256:abc", tt.decision)
			require.NoError(t, logger.Close())

			lines := readAuditLines(t, logFile)
			require.Len(t, lines, 1)
			event := lines[0]
			assert.Equal(t, "AUDIT", event["level"])
			assert.Equal(t, "authorization.admission", event["type"])
			assert.Equal(t, tt.wantOutcome, event["outcome"])
			assert.Equal(t, ComponentRegistryAPI, event["component"])
			assert.Equal(t, map[string]any{"actor": `https://idp.example|alice`}, event["subjects"])
			assert.Equal(t, map[string]any{"action": "catalog.read", "resource": `Registry::View::"main"`}, event["target"])
			source, ok := event["source"].(map[string]any)
			require.True(t, ok)
			assert.Equal(t, audit.SourceTypeNetwork, source["type"])
			assert.Equal(t, "192.0.2.10:4242", source["value"])
			assert.Equal(t, map[string]any{"user_agent": "thv-test/1.0"}, source["extra"])
			metadata, ok := event["metadata"].(map[string]any)
			require.True(t, ok)
			// The request ID joins this event to the request's own audit event.
			assert.Equal(t, map[string]any{"decision": tt.decision, "policy_revision": "sha256:abc", "request_id": "req-123"},
				metadata["extra"])
		})
	}
}

func TestMiddleware_MutationCommittedExtra(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		committed     bool
		wantCommitted bool
	}{
		{name: "committed mutation is marked", committed: true, wantCommitted: true},
		{name: "uncommitted mutation is not marked", committed: false, wantCommitted: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			logger := newTestLogger(&buf)

			info := &RouteInfo{
				EventType:         EventSourceCreate,
				MutationCommitted: tt.committed,
				Target:            map[string]string{"method": http.MethodPut, "path": "/v1/sources/s"},
			}
			// A committed mutation is recorded even when the response fails.
			inner := withRouteInfo(info, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusServiceUnavailable)
			}))
			Middleware(enabledConfig(), logger)(inner).ServeHTTP(httptest.NewRecorder(),
				httptest.NewRequest(http.MethodPut, "/v1/sources/s", nil))

			var event map[string]any
			require.NoError(t, json.Unmarshal(buf.Bytes(), &event), buf.String())
			assert.Equal(t, EventSourceCreate, event["type"])
			extra := event["metadata"].(map[string]any)["extra"].(map[string]any)
			if tt.wantCommitted {
				assert.Equal(t, true, extra["mutation_committed"])
			} else {
				assert.NotContains(t, extra, "mutation_committed")
			}
		})
	}
}
