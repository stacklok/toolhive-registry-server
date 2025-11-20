package auth

import (
	"encoding/json"
	"net/http"

	"github.com/stacklok/toolhive/pkg/logger"
)

// ProtectedResourceMetadata represents RFC 9728 OAuth 2.0 Protected Resource Metadata
type ProtectedResourceMetadata struct {
	Resource               string   `json:"resource"`
	AuthorizationServers   []string `json:"authorization_servers"`
	BearerMethodsSupported []string `json:"bearer_methods_supported,omitempty"`
	ScopesSupported        []string `json:"scopes_supported,omitempty"`
}

// DefaultScopes are the default OAuth scopes for the registry
var DefaultScopes = []string{"mcp-registry:read", "mcp-registry:write"}

// NewProtectedResourceHandler creates an RFC 9728 compliant handler
func NewProtectedResourceHandler(
	resourceURL string,
	authorizationServers []string,
	scopes []string,
) http.Handler {
	// Apply default scopes if not specified
	if len(scopes) == 0 {
		scopes = DefaultScopes
	}

	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		metadata := ProtectedResourceMetadata{
			Resource:               resourceURL,
			AuthorizationServers:   authorizationServers,
			BearerMethodsSupported: []string{"header"},
			ScopesSupported:        scopes,
		}

		if err := json.NewEncoder(w).Encode(metadata); err != nil {
			logger.Errorf("auth: failed to encode protected resource metadata: %v", err)
		}
	})
}
