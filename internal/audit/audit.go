// Package audit provides audit logging for the ToolHive Registry Server.
// It uses the shared audit library from toolhive-core to emit structured
// audit events for API operations.
package audit

import (
	"net/http"
	"strings"

	"github.com/stacklok/toolhive-core/audit"
)

// ComponentRegistryAPI is the component name for the registry server
// (distinct from toolhive's "toolhive-api").
const ComponentRegistryAPI = "toolhive-registry-api"

// Target field values for resource types.
const (
	ResourceTypeSource   = "source"
	ResourceTypeRegistry = "registry"
	ResourceTypeEntry    = "entry"
)

// Event types for audit logging — write operations.
const (
	EventSourceCreate   = "source.create"
	EventSourceUpdate   = "source.update"
	EventSourceDelete   = "source.delete"
	EventRegistryCreate = "registry.create"
	EventRegistryUpdate = "registry.update"
	EventRegistryDelete = "registry.delete"
	EventEntryPublish   = "entry.publish"
	EventEntryDelete    = "entry.delete"
	EventEntryClaims    = "entry.claims.update"
)

// Event types for audit logging — read operations.
const (
	EventSourceList          = "source.list"
	EventSourceRead          = "source.read"
	EventSourceEntriesList   = "source.entries.list"
	EventRegistryList        = "registry.list"
	EventRegistryRead        = "registry.read"
	EventRegistryEntriesList = "registry.entries.list"
	EventUserInfo            = "user.info"
)

// Event types for audit logging — security events.
const (
	// EventAuthUnauthenticated is emitted when a request fails authentication
	// (HTTP 401). Captured by the pre-auth middleware so that auth failures
	// are visible to SIEM systems even though the post-auth audit middleware
	// never fires for rejected requests.
	EventAuthUnauthenticated = "auth.unauthenticated"
)

// OutcomeFromStatus maps an HTTP status code to an audit outcome string.
func OutcomeFromStatus(status int) string {
	switch {
	case status >= 200 && status < 300:
		return audit.OutcomeSuccess
	case status == http.StatusForbidden:
		return audit.OutcomeDenied
	case status >= 400 && status < 500:
		return audit.OutcomeFailure
	default:
		return audit.OutcomeError
	}
}

// EventTypeFromRequest determines the audit event type from the HTTP method,
// request path, and response status code. The status code is used to
// distinguish creates (201) from updates (200) for PUT upsert endpoints.
// It returns an empty string if the request does not map to a known
// auditable operation.
func EventTypeFromRequest(method, path string, status int) string {
	// Normalise the path by trimming the /v1 prefix and any trailing slash.
	trimmed := strings.TrimPrefix(path, "/v1")
	trimmed = strings.TrimSuffix(trimmed, "/")

	switch method {
	case http.MethodGet:
		return eventTypeForGet(trimmed)
	case http.MethodPut:
		return eventTypeForPut(trimmed, status)
	case http.MethodPost:
		return eventTypeForPost(trimmed)
	case http.MethodDelete:
		return eventTypeForDelete(trimmed)
	default:
		return ""
	}
}

// TargetFromRequest extracts a rich target map from the request path,
// including explicit resource_type and resource_name fields alongside
// the raw method and path. This gives SIEM systems structured fields
// to query without needing to regex-parse the URL.
func TargetFromRequest(method, path string) map[string]string {
	target := map[string]string{
		"method": method,
		"path":   path,
	}

	trimmed := strings.TrimPrefix(path, "/v1")
	trimmed = strings.TrimSuffix(trimmed, "/")

	switch {
	case isSourceEntriesPath(trimmed):
		target["resource_type"] = ResourceTypeSource
		target["resource_name"] = extractSegment(trimmed, "/sources/")
	case isSourcePath(trimmed):
		target["resource_type"] = ResourceTypeSource
		target["resource_name"] = extractSegment(trimmed, "/sources/")
	case trimmed == "/sources":
		target["resource_type"] = ResourceTypeSource
	case isRegistryEntriesPath(trimmed):
		target["resource_type"] = ResourceTypeRegistry
		target["resource_name"] = extractSegment(trimmed, "/registries/")
	case isRegistryPath(trimmed):
		target["resource_type"] = ResourceTypeRegistry
		target["resource_name"] = extractSegment(trimmed, "/registries/")
	case trimmed == "/registries":
		target["resource_type"] = ResourceTypeRegistry
	case isEntryClaimsPath(trimmed):
		parts := strings.Split(strings.TrimPrefix(trimmed, "/entries/"), "/")
		target["resource_type"] = ResourceTypeEntry
		if len(parts) >= 2 {
			target["entry_type"] = parts[0]
			target["resource_name"] = parts[1]
		}
	case isEntryVersionPath(trimmed):
		parts := strings.Split(strings.TrimPrefix(trimmed, "/entries/"), "/")
		target["resource_type"] = ResourceTypeEntry
		if len(parts) >= 4 {
			target["entry_type"] = parts[0]
			target["resource_name"] = parts[1]
			target["version"] = parts[3]
		}
	case trimmed == "/entries":
		target["resource_type"] = ResourceTypeEntry
	case trimmed == "/me":
		target["resource_type"] = "user"
	}

	return target
}

// maxUserAgentLen is the maximum length for captured User-Agent values
// to prevent log bloat from malicious clients.
const maxUserAgentLen = 512

// SourceFromRequest extracts the client IP and User-Agent for audit events.
//
// For the IP address, it uses r.RemoteAddr as the primary source because
// Chi's middleware.RealIP runs earlier in the middleware chain and already
// resolves X-Forwarded-For / X-Real-IP into RemoteAddr. The raw
// X-Forwarded-For header is preserved in Extra for forensic context.
func SourceFromRequest(r *http.Request) audit.EventSource {
	source := audit.EventSource{
		Type:  audit.SourceTypeNetwork,
		Value: r.RemoteAddr,
	}

	extra := make(map[string]any, 2)

	// Preserve raw X-Forwarded-For for forensic context (may differ from
	// RemoteAddr after RealIP middleware processing).
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		extra["x_forwarded_for"] = xff
	}

	// Include User-Agent for forensics, truncated to prevent log bloat.
	if ua := r.Header.Get("User-Agent"); ua != "" {
		if len(ua) > maxUserAgentLen {
			ua = ua[:maxUserAgentLen]
		}
		extra["user_agent"] = ua
	}

	if len(extra) > 0 {
		source.Extra = extra
	}

	return source
}

// extractSegment returns the first path segment after the given prefix.
func extractSegment(path, prefix string) string {
	after := strings.TrimPrefix(path, prefix)
	if idx := strings.Index(after, "/"); idx != -1 {
		return after[:idx]
	}
	return after
}

// eventTypeForGet returns the event type for GET (read) requests.
func eventTypeForGet(path string) string {
	switch {
	case path == "/sources":
		return EventSourceList
	case isSourceEntriesPath(path):
		return EventSourceEntriesList
	case isSourcePath(path):
		return EventSourceRead
	case path == "/registries":
		return EventRegistryList
	case isRegistryEntriesPath(path):
		return EventRegistryEntriesList
	case isRegistryPath(path):
		return EventRegistryRead
	case path == "/me":
		return EventUserInfo
	default:
		return ""
	}
}

// eventTypeForPut returns the event type for PUT requests.
// Uses the response status to distinguish create (201) from update (200)
// for upsert endpoints (sources, registries).
func eventTypeForPut(path string, status int) string {
	switch {
	case isSourcePath(path):
		if status == http.StatusCreated {
			return EventSourceCreate
		}
		return EventSourceUpdate
	case isRegistryPath(path):
		if status == http.StatusCreated {
			return EventRegistryCreate
		}
		return EventRegistryUpdate
	case isEntryClaimsPath(path):
		return EventEntryClaims
	default:
		return ""
	}
}

// eventTypeForPost returns the event type for POST requests.
func eventTypeForPost(path string) string {
	if path == "/entries" {
		return EventEntryPublish
	}
	return ""
}

// eventTypeForDelete returns the event type for DELETE requests.
func eventTypeForDelete(path string) string {
	switch {
	case isSourcePath(path):
		return EventSourceDelete
	case isRegistryPath(path):
		return EventRegistryDelete
	case isEntryVersionPath(path):
		return EventEntryDelete
	default:
		return ""
	}
}

// isSourcePath checks if the path matches /sources/{name}.
func isSourcePath(path string) bool {
	return matchesPattern(path, "/sources/")
}

// isSourceEntriesPath checks if the path matches /sources/{name}/entries.
func isSourceEntriesPath(path string) bool {
	trimmed := strings.TrimPrefix(path, "/sources/")
	if trimmed == path || trimmed == "" {
		return false
	}
	return strings.HasSuffix(trimmed, "/entries") && strings.Count(trimmed, "/") == 1
}

// isRegistryPath checks if the path matches /registries/{name}.
func isRegistryPath(path string) bool {
	return matchesPattern(path, "/registries/")
}

// isRegistryEntriesPath checks if the path matches /registries/{name}/entries.
func isRegistryEntriesPath(path string) bool {
	trimmed := strings.TrimPrefix(path, "/registries/")
	if trimmed == path || trimmed == "" {
		return false
	}
	return strings.HasSuffix(trimmed, "/entries") && strings.Count(trimmed, "/") == 1
}

// isEntryVersionPath checks if the path matches /entries/{type}/{name}/versions/{version}.
func isEntryVersionPath(path string) bool {
	trimmed := strings.TrimPrefix(path, "/entries/")
	if trimmed == path || trimmed == "" {
		return false
	}
	// Expect: {type}/{name}/versions/{version}
	parts := strings.Split(trimmed, "/")
	return len(parts) == 4 && parts[2] == "versions"
}

// isEntryClaimsPath checks if the path matches /entries/{type}/{name}/claims.
func isEntryClaimsPath(path string) bool {
	trimmed := strings.TrimPrefix(path, "/entries/")
	if trimmed == path || trimmed == "" {
		return false
	}
	// Expect: {type}/{name}/claims
	parts := strings.Split(trimmed, "/")
	return len(parts) == 3 && parts[2] == "claims"
}

// matchesPattern is a helper that checks if the path starts with the given
// prefix and has exactly one segment after it (i.e. /prefix/{name}).
func matchesPattern(path, prefix string) bool {
	trimmed := strings.TrimPrefix(path, prefix)
	if trimmed == path || trimmed == "" {
		return false
	}
	return !strings.Contains(trimmed, "/")
}
