package auth

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/stacklok/toolhive/pkg/logger"

	"github.com/stacklok/toolhive-registry-server/internal/config"
)

// protectedResourceMetadata represents RFC 9728 OAuth 2.0 Protected Resource Metadata
type protectedResourceMetadata struct {
	Resource               string   `json:"resource"`
	AuthorizationServers   []string `json:"authorization_servers"`
	BearerMethodsSupported []string `json:"bearer_methods_supported,omitempty"`
	ScopesSupported        []string `json:"scopes_supported,omitempty"`
}

// newProtectedResourceHandler creates an RFC 9728 compliant handler.
// Returns an error if required parameters are missing per RFC 9728:
// - resourceURL must not be empty
// - authorizationServers must contain at least one entry
func newProtectedResourceHandler(
	resourceURL string,
	authorizationServers []string,
	scopes []string,
) (http.Handler, error) {
	// Validate required parameters per RFC 9728
	if resourceURL == "" {
		return nil, errors.New("resourceURL is required")
	}
	if len(authorizationServers) == 0 {
		return nil, errors.New("at least one authorization server is required")
	}

	// Apply default scopes if not specified (defensive copy to avoid aliasing)
	if len(scopes) == 0 {
		scopes = append([]string{}, config.DefaultScopes...)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		metadata := protectedResourceMetadata{
			Resource:               resourceURL,
			AuthorizationServers:   authorizationServers,
			BearerMethodsSupported: []string{"header"},
			ScopesSupported:        scopes,
		}

		data, err := json.Marshal(metadata)
		if err != nil {
			logger.Errorf("auth: failed to encode protected resource metadata: %v", err)
			http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
	}), nil
}
