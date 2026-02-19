package authz

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	thvauth "github.com/stacklok/toolhive/pkg/auth"

	"github.com/stacklok/toolhive-registry-server/internal/config"
)

// ForbiddenResponse is the JSON body returned when authorization is denied.
type ForbiddenResponse struct {
	Error   string           `json:"error"`
	Message string           `json:"message"`
	Details *ForbiddenDetail `json:"details,omitempty"`
}

// ForbiddenDetail provides additional context for authorization denials,
// helping callers understand why access was denied and what is required.
type ForbiddenDetail struct {
	RequiredAction string   `json:"required_action"`
	UserScopes     []string `json:"user_scopes"`
	Hint           string   `json:"hint"`
}

// Middleware creates an HTTP middleware that performs Cedar-based authorization.
// It requires the auth middleware to have already stored Identity in the context
// via auth.WithIdentity. If no identity is present (public/anonymous path),
// the request is passed through without authorization checks.
func Middleware(authorizer Authorizer, scopeMapping []config.ScopeMappingEntry) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			identity, ok := thvauth.IdentityFromContext(r.Context())
			if !ok {
				// No identity in context means the request arrived on a
				// public/anonymous path that bypassed authentication.
				// Pass through without authorization checks.
				next.ServeHTTP(w, r)
				return
			}

			scopes := ExtractScopes(identity.Claims)
			grantedActions := MapScopesToActions(scopes, scopeMapping)
			requiredAction := RouteAction(r.Method, r.URL.Path)
			registryName := extractRegistryName(r.URL.Path)

			req := Request{
				GrantedActions: grantedActions,
				Action:         requiredAction,
				ResourceType:   "Subregistry",
				ResourceID:     registryName,
			}

			decision, err := authorizer.Authorize(r.Context(), req)
			if err != nil {
				slog.Error("Authorization evaluation failed",
					"error", err,
					"action", requiredAction,
					"path", r.URL.Path,
					"method", r.Method,
					"subject", identity.Subject,
				)
				writeJSONError(w, http.StatusInternalServerError, "authorization evaluation failed")
				return
			}

			if !decision.Allowed {
				slog.Warn("Authorization denied",
					"action", requiredAction,
					"path", r.URL.Path,
					"method", r.Method,
					"subject", identity.Subject,
					"scopes", scopes,
					"granted_actions", grantedActions,
					"reasons", decision.Reasons,
				)
				writeForbidden(w, requiredAction, scopes, scopeMapping)
				return
			}

			slog.Debug("Authorization permitted",
				"action", requiredAction,
				"path", r.URL.Path,
				"method", r.Method,
				"subject", identity.Subject,
				"reasons", decision.Reasons,
			)

			next.ServeHTTP(w, r)
		})
	}
}

// NoopMiddleware returns a middleware that performs no authorization checks.
// Use this when authorization is disabled in the configuration.
func NoopMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return next
	}
}

// writeForbidden writes a 403 Forbidden JSON response with details about
// the required action and a hint indicating which scopes would grant access.
func writeForbidden(w http.ResponseWriter, requiredAction string, userScopes []string, scopeMapping []config.ScopeMappingEntry) {
	hint := buildHint(requiredAction, scopeMapping)

	resp := ForbiddenResponse{
		Error:   "forbidden",
		Message: "You do not have permission to perform this action.",
		Details: &ForbiddenDetail{
			RequiredAction: requiredAction,
			UserScopes:     userScopes,
			Hint:           hint,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("Failed to encode forbidden response", "error", err)
	}
}

// writeJSONError writes a generic JSON error response with the given status code.
func writeJSONError(w http.ResponseWriter, status int, message string) {
	resp := struct {
		Error string `json:"error"`
	}{
		Error: message,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("Failed to encode error response", "error", err)
	}
}

// buildHint examines the scope mapping to find which scopes grant the
// required action and returns a human-readable hint string.
func buildHint(requiredAction string, scopeMapping []config.ScopeMappingEntry) string {
	var matchingScopes []string

	for _, entry := range scopeMapping {
		if slices.Contains(entry.Actions, requiredAction) {
			matchingScopes = append(matchingScopes, entry.Scope)
		}
	}

	if len(matchingScopes) == 0 {
		return "No configured scopes grant the required action."
	}

	return "This operation requires one of the following scopes: " + strings.Join(matchingScopes, ", ")
}

// extractRegistryName extracts the registry name from the URL path.
// It handles two URL patterns:
//   - /registry/{name}/v0.1/...           (MCP Registry API)
//   - /extension/v0/registries/{name}/...  (Extension API)
//
// Returns an empty string if no registry name can be determined from the path.
func extractRegistryName(path string) string {
	segments := strings.Split(strings.TrimPrefix(path, "/"), "/")

	// Pattern: /registry/{name}/v0.1/...
	// segments: ["registry", "{name}", "v0.1", ...]
	if len(segments) >= 2 && segments[0] == "registry" {
		return segments[1]
	}

	// Pattern: /extension/v0/registries/{name}/...
	// segments: ["extension", "v0", "registries", "{name}", ...]
	if len(segments) >= 4 && segments[0] == "extension" && segments[1] == "v0" && segments[2] == "registries" {
		return segments[3]
	}

	return ""
}
