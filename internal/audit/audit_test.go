package audit

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stacklok/toolhive-core/audit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOutcomeFromStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		status   int
		expected string
	}{
		{name: "200 OK is success", status: http.StatusOK, expected: audit.OutcomeSuccess},
		{name: "201 Created is success", status: http.StatusCreated, expected: audit.OutcomeSuccess},
		{name: "204 No Content is success", status: http.StatusNoContent, expected: audit.OutcomeSuccess},
		{name: "299 is success", status: 299, expected: audit.OutcomeSuccess},
		{name: "400 Bad Request is failure", status: http.StatusBadRequest, expected: audit.OutcomeFailure},
		{name: "401 Unauthorized is failure", status: http.StatusUnauthorized, expected: audit.OutcomeFailure},
		{name: "403 Forbidden is denied", status: http.StatusForbidden, expected: audit.OutcomeDenied},
		{name: "404 Not Found is failure", status: http.StatusNotFound, expected: audit.OutcomeFailure},
		{name: "409 Conflict is failure", status: http.StatusConflict, expected: audit.OutcomeFailure},
		{name: "422 Unprocessable is failure", status: http.StatusUnprocessableEntity, expected: audit.OutcomeFailure},
		{name: "500 Internal Server Error is error", status: http.StatusInternalServerError, expected: audit.OutcomeError},
		{name: "502 Bad Gateway is error", status: http.StatusBadGateway, expected: audit.OutcomeError},
		{name: "503 Service Unavailable is error", status: http.StatusServiceUnavailable, expected: audit.OutcomeError},
		{name: "100 Continue is error (unexpected)", status: 100, expected: audit.OutcomeError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := OutcomeFromStatus(tt.status)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestSourceFromRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		xff         string // X-Forwarded-For header
		remoteAddr  string
		userAgent   string
		expectValue string
		expectXFF   string // expected x_forwarded_for in extra
		expectUA    string // expected user_agent in extra
	}{
		{
			name:        "RemoteAddr is always the primary source value",
			remoteAddr:  "192.168.1.1:12345",
			expectValue: "192.168.1.1:12345",
		},
		{
			name:        "X-Forwarded-For preserved in extra, RemoteAddr is value",
			xff:         "203.0.113.50, 70.41.3.18",
			remoteAddr:  "10.0.0.1:54321",
			expectValue: "10.0.0.1:54321",
			expectXFF:   "203.0.113.50, 70.41.3.18",
		},
		{
			name:        "User-Agent captured in extra",
			remoteAddr:  "10.0.0.1:54321",
			userAgent:   "curl/7.81.0",
			expectValue: "10.0.0.1:54321",
			expectUA:    "curl/7.81.0",
		},
		{
			name:        "User-Agent truncated at 512 bytes",
			remoteAddr:  "10.0.0.1:54321",
			userAgent:   strings.Repeat("A", 600),
			expectValue: "10.0.0.1:54321",
			expectUA:    strings.Repeat("A", maxUserAgentLen),
		},
		{
			name:        "no headers means no extra",
			remoteAddr:  "10.0.0.1:54321",
			expectValue: "10.0.0.1:54321",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req, _ := http.NewRequest(http.MethodGet, "/v1/sources", nil)
			req.RemoteAddr = tt.remoteAddr
			if tt.xff != "" {
				req.Header.Set("X-Forwarded-For", tt.xff)
			}
			if tt.userAgent != "" {
				req.Header.Set("User-Agent", tt.userAgent)
			}

			source := SourceFromRequest(req)
			assert.Equal(t, audit.SourceTypeNetwork, source.Type)
			assert.Equal(t, tt.expectValue, source.Value)

			if tt.expectXFF != "" {
				require.NotNil(t, source.Extra)
				assert.Equal(t, tt.expectXFF, source.Extra["x_forwarded_for"])
			}
			if tt.expectUA != "" {
				require.NotNil(t, source.Extra)
				assert.Equal(t, tt.expectUA, source.Extra["user_agent"])
			}
			if tt.expectXFF == "" && tt.expectUA == "" {
				assert.Nil(t, source.Extra)
			}
		})
	}
}
