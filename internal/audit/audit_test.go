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

func TestEventTypeFromRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		method   string
		path     string
		status   int
		expected string
	}{
		// GET methods — read operations
		{
			name:     "GET /v1/sources returns source.list",
			method:   http.MethodGet,
			path:     "/v1/sources",
			status:   http.StatusOK,
			expected: EventSourceList,
		},
		{
			name:     "GET /v1/sources/{name} returns source.read",
			method:   http.MethodGet,
			path:     "/v1/sources/my-source",
			status:   http.StatusOK,
			expected: EventSourceRead,
		},
		{
			name:     "GET /v1/sources/{name}/entries returns source.entries.list",
			method:   http.MethodGet,
			path:     "/v1/sources/my-source/entries",
			status:   http.StatusOK,
			expected: EventSourceEntriesList,
		},
		{
			name:     "GET /v1/registries returns registry.list",
			method:   http.MethodGet,
			path:     "/v1/registries",
			status:   http.StatusOK,
			expected: EventRegistryList,
		},
		{
			name:     "GET /v1/registries/{name} returns registry.read",
			method:   http.MethodGet,
			path:     "/v1/registries/my-reg",
			status:   http.StatusOK,
			expected: EventRegistryRead,
		},
		{
			name:     "GET /v1/registries/{name}/entries returns registry.entries.list",
			method:   http.MethodGet,
			path:     "/v1/registries/my-reg/entries",
			status:   http.StatusOK,
			expected: EventRegistryEntriesList,
		},
		{
			name:     "GET /v1/me returns user.info",
			method:   http.MethodGet,
			path:     "/v1/me",
			status:   http.StatusOK,
			expected: EventUserInfo,
		},
		{
			name:     "GET unknown v1 path returns empty",
			method:   http.MethodGet,
			path:     "/v1/unknown",
			status:   http.StatusOK,
			expected: "",
		},

		// PUT methods — create vs update via status code
		{
			name:     "PUT source with 200 is source.update",
			method:   http.MethodPut,
			path:     "/v1/sources/my-source",
			status:   http.StatusOK,
			expected: EventSourceUpdate,
		},
		{
			name:     "PUT source with 201 is source.create",
			method:   http.MethodPut,
			path:     "/v1/sources/my-source",
			status:   http.StatusCreated,
			expected: EventSourceCreate,
		},
		{
			name:     "PUT registry with 200 is registry.update",
			method:   http.MethodPut,
			path:     "/v1/registries/my-registry",
			status:   http.StatusOK,
			expected: EventRegistryUpdate,
		},
		{
			name:     "PUT registry with 201 is registry.create",
			method:   http.MethodPut,
			path:     "/v1/registries/my-registry",
			status:   http.StatusCreated,
			expected: EventRegistryCreate,
		},
		{
			name:     "PUT source with 400 is source.update (error path)",
			method:   http.MethodPut,
			path:     "/v1/sources/my-source",
			status:   http.StatusBadRequest,
			expected: EventSourceUpdate,
		},
		{
			name:     "PUT entry claims is always entry.claims.update",
			method:   http.MethodPut,
			path:     "/v1/entries/tool/my-tool/claims",
			status:   http.StatusOK,
			expected: EventEntryClaims,
		},
		{
			name:     "PUT unknown path returns empty",
			method:   http.MethodPut,
			path:     "/v1/unknown",
			status:   http.StatusOK,
			expected: "",
		},

		// POST methods
		{
			name:     "POST entries creates entry.publish event",
			method:   http.MethodPost,
			path:     "/v1/entries",
			status:   http.StatusCreated,
			expected: EventEntryPublish,
		},
		{
			name:     "POST unknown path returns empty",
			method:   http.MethodPost,
			path:     "/v1/sources/my-source",
			status:   http.StatusOK,
			expected: "",
		},

		// DELETE methods
		{
			name:     "DELETE source creates source.delete event",
			method:   http.MethodDelete,
			path:     "/v1/sources/my-source",
			status:   http.StatusNoContent,
			expected: EventSourceDelete,
		},
		{
			name:     "DELETE registry creates registry.delete event",
			method:   http.MethodDelete,
			path:     "/v1/registries/my-registry",
			status:   http.StatusNoContent,
			expected: EventRegistryDelete,
		},
		{
			name:     "DELETE entry version creates entry.delete event",
			method:   http.MethodDelete,
			path:     "/v1/entries/tool/my-tool/versions/1.0.0",
			status:   http.StatusNoContent,
			expected: EventEntryDelete,
		},
		{
			name:     "DELETE unknown path returns empty",
			method:   http.MethodDelete,
			path:     "/v1/unknown",
			status:   http.StatusOK,
			expected: "",
		},

		// Non-v1 paths
		{
			name:     "PUT on non-v1 path returns empty event type",
			method:   http.MethodPut,
			path:     "/health",
			status:   http.StatusOK,
			expected: "",
		},
		// Unrecognised v1 path
		{
			name:     "POST on unrecognised v1 path returns empty event type",
			method:   http.MethodPost,
			path:     "/v1/unknown",
			status:   http.StatusOK,
			expected: "",
		},
		// Edge cases: paths with trailing slashes
		{
			name:     "PUT source with trailing slash",
			method:   http.MethodPut,
			path:     "/v1/sources/my-source/",
			status:   http.StatusOK,
			expected: EventSourceUpdate,
		},
		{
			name:     "GET sources with trailing slash",
			method:   http.MethodGet,
			path:     "/v1/sources/",
			status:   http.StatusOK,
			expected: EventSourceList,
		},
		// Edge case: subpath should not match for delete
		{
			name:     "DELETE registries entries subpath returns empty event type",
			method:   http.MethodDelete,
			path:     "/v1/registries/my-reg/entries",
			status:   http.StatusOK,
			expected: "",
		},
		// Edge case: PATCH method should not produce events
		{
			name:     "PATCH method returns empty event type",
			method:   http.MethodPatch,
			path:     "/v1/sources/my-source",
			status:   http.StatusOK,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := EventTypeFromRequest(tt.method, tt.path, tt.status)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestTargetFromRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		method string
		path   string
		expect map[string]string
	}{
		{
			name:   "source path extracts resource details",
			method: http.MethodPut,
			path:   "/v1/sources/my-source",
			expect: map[string]string{
				"method":        http.MethodPut,
				"path":          "/v1/sources/my-source",
				"resource_type": "source",
				"resource_name": "my-source",
			},
		},
		{
			name:   "registry path extracts resource details",
			method: http.MethodDelete,
			path:   "/v1/registries/my-reg",
			expect: map[string]string{
				"method":        http.MethodDelete,
				"path":          "/v1/registries/my-reg",
				"resource_type": "registry",
				"resource_name": "my-reg",
			},
		},
		{
			name:   "entry claims path extracts entry details",
			method: http.MethodPut,
			path:   "/v1/entries/tool/my-tool/claims",
			expect: map[string]string{
				"method":        http.MethodPut,
				"path":          "/v1/entries/tool/my-tool/claims",
				"resource_type": "entry",
				"entry_type":    "tool",
				"resource_name": "my-tool",
			},
		},
		{
			name:   "entry version path extracts full details",
			method: http.MethodDelete,
			path:   "/v1/entries/server/my-server/versions/1.2.3",
			expect: map[string]string{
				"method":        http.MethodDelete,
				"path":          "/v1/entries/server/my-server/versions/1.2.3",
				"resource_type": "entry",
				"entry_type":    "server",
				"resource_name": "my-server",
				"version":       "1.2.3",
			},
		},
		{
			name:   "entries collection path sets resource_type only",
			method: http.MethodPost,
			path:   "/v1/entries",
			expect: map[string]string{
				"method":        http.MethodPost,
				"path":          "/v1/entries",
				"resource_type": "entry",
			},
		},
		{
			name:   "sources list path sets resource_type only",
			method: http.MethodGet,
			path:   "/v1/sources",
			expect: map[string]string{
				"method":        http.MethodGet,
				"path":          "/v1/sources",
				"resource_type": "source",
			},
		},
		{
			name:   "registries list path sets resource_type only",
			method: http.MethodGet,
			path:   "/v1/registries",
			expect: map[string]string{
				"method":        http.MethodGet,
				"path":          "/v1/registries",
				"resource_type": "registry",
			},
		},
		{
			name:   "source entries path extracts resource details",
			method: http.MethodGet,
			path:   "/v1/sources/my-source/entries",
			expect: map[string]string{
				"method":        http.MethodGet,
				"path":          "/v1/sources/my-source/entries",
				"resource_type": "source",
				"resource_name": "my-source",
			},
		},
		{
			name:   "registry entries path extracts resource details",
			method: http.MethodGet,
			path:   "/v1/registries/my-reg/entries",
			expect: map[string]string{
				"method":        http.MethodGet,
				"path":          "/v1/registries/my-reg/entries",
				"resource_type": "registry",
				"resource_name": "my-reg",
			},
		},
		{
			name:   "/me path sets resource_type to user",
			method: http.MethodGet,
			path:   "/v1/me",
			expect: map[string]string{
				"method":        http.MethodGet,
				"path":          "/v1/me",
				"resource_type": "user",
			},
		},
		{
			name:   "unknown path has only method and path",
			method: http.MethodPost,
			path:   "/v1/unknown",
			expect: map[string]string{
				"method": http.MethodPost,
				"path":   "/v1/unknown",
			},
		},
		{
			name:   "source with trailing slash",
			method: http.MethodPut,
			path:   "/v1/sources/my-source/",
			expect: map[string]string{
				"method":        http.MethodPut,
				"path":          "/v1/sources/my-source/",
				"resource_type": "source",
				"resource_name": "my-source",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := TargetFromRequest(tt.method, tt.path)
			assert.Equal(t, tt.expect, got)
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

func TestIsSourceEntriesPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{name: "valid source entries path", path: "/sources/my-source/entries", expected: true},
		{name: "source path without entries", path: "/sources/my-source", expected: false},
		{name: "source list path", path: "/sources", expected: false},
		{name: "source with extra segment after entries", path: "/sources/my-source/entries/extra", expected: false},
		{name: "empty path", path: "", expected: false},
		{name: "unrelated path", path: "/registries/foo/entries", expected: false},
		{name: "sources prefix only", path: "/sources/", expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, isSourceEntriesPath(tt.path))
		})
	}
}

func TestIsRegistryEntriesPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{name: "valid registry entries path", path: "/registries/my-reg/entries", expected: true},
		{name: "registry path without entries", path: "/registries/my-reg", expected: false},
		{name: "registry list path", path: "/registries", expected: false},
		{name: "registry with extra segment after entries", path: "/registries/my-reg/entries/extra", expected: false},
		{name: "empty path", path: "", expected: false},
		{name: "unrelated path", path: "/sources/foo/entries", expected: false},
		{name: "registries prefix only", path: "/registries/", expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, isRegistryEntriesPath(tt.path))
		})
	}
}

func TestIsSourcePath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{name: "valid source path", path: "/sources/my-source", expected: true},
		{name: "source list path", path: "/sources/", expected: false},
		{name: "source with sub-path", path: "/sources/my-source/entries", expected: false},
		{name: "empty path", path: "", expected: false},
		{name: "unrelated path", path: "/registries/foo", expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, isSourcePath(tt.path))
		})
	}
}

func TestIsRegistryPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{name: "valid registry path", path: "/registries/my-reg", expected: true},
		{name: "registry list path", path: "/registries/", expected: false},
		{name: "registry with sub-path", path: "/registries/my-reg/entries", expected: false},
		{name: "empty path", path: "", expected: false},
		{name: "unrelated path", path: "/sources/foo", expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, isRegistryPath(tt.path))
		})
	}
}

func TestIsEntryVersionPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{name: "valid entry version path", path: "/entries/tool/my-tool/versions/1.0.0", expected: true},
		{name: "missing version segment", path: "/entries/tool/my-tool/versions", expected: false},
		{name: "wrong middle segment", path: "/entries/tool/my-tool/claims/1.0.0", expected: false},
		{name: "entry list path", path: "/entries", expected: false},
		{name: "too few segments", path: "/entries/tool", expected: false},
		{name: "empty path", path: "", expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, isEntryVersionPath(tt.path))
		})
	}
}

func TestIsEntryClaimsPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{name: "valid entry claims path", path: "/entries/tool/my-tool/claims", expected: true},
		{name: "wrong suffix", path: "/entries/tool/my-tool/versions", expected: false},
		{name: "too few segments", path: "/entries/tool/claims", expected: false},
		{name: "too many segments", path: "/entries/tool/my-tool/claims/extra", expected: false},
		{name: "empty path", path: "", expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, isEntryClaimsPath(tt.path))
		})
	}
}
