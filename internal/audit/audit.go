// Package audit provides audit logging for the ToolHive Registry Server.
// It uses the shared audit library from toolhive-core to emit structured
// audit events for API operations.
package audit

import (
	"net/http"

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
	ResourceTypeUser     = "user"
	ResourceTypeServer   = "server"
	ResourceTypeSkill    = "skill"
)

// Target field keys.
const (
	targetFieldMethod       = "method"
	targetFieldPath         = "path"
	targetFieldResourceType = "resource_type"
	targetFieldResourceName = "resource_name"
	targetFieldRegistryName = "registry_name"
	targetFieldNamespace    = "namespace"
	targetFieldEntryType    = "entry_type"
	targetFieldVersion      = "version"
)

// Event types for the MCP registry v0.1 discovery API.
const (
	EventServerList         = "server.list"
	EventServerVersionsList = "server.versions.list"
	EventServerVersionRead  = "server.version.read"
)

// Event types for the skills extension API.
const (
	EventSkillList         = "skill.list"
	EventSkillRead         = "skill.read"
	EventSkillVersionsList = "skill.versions.list"
	EventSkillVersionRead  = "skill.version.read"
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
