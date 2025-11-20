// Package auth provides authentication middleware for the registry API server.
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/stacklok/toolhive/pkg/auth"
	"github.com/stacklok/toolhive/pkg/logger"
)

// namedValidator pairs a validator with its provider metadata
type namedValidator struct {
	name      string
	validator *auth.TokenValidator
}

// DefaultRealm is the default protection space identifier
const DefaultRealm = "mcp-registry"

// MultiProviderMiddleware handles authentication with multiple OAuth/OIDC providers.
type MultiProviderMiddleware struct {
	validators  []namedValidator
	resourceURL string
	realm       string
}

// NewMultiProviderMiddleware creates a new multi-provider authentication middleware.
func NewMultiProviderMiddleware(
	ctx context.Context,
	providers []providerConfig,
	resourceURL string,
	realm string,
) (*MultiProviderMiddleware, error) {
	if len(providers) == 0 {
		return nil, errors.New("at least one provider must be configured")
	}

	// Apply default realm if not specified
	if realm == "" {
		realm = DefaultRealm
	}

	m := &MultiProviderMiddleware{
		validators:  make([]namedValidator, 0, len(providers)),
		resourceURL: resourceURL,
		realm:       realm,
	}

	for _, pc := range providers {
		validator, err := auth.NewTokenValidator(ctx, pc.ValidatorConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create validator for provider %q: %w", pc.Name, err)
		}

		nv := namedValidator{
			name:      pc.Name,
			validator: validator,
		}
		m.validators = append(m.validators, nv)
	}

	return m, nil
}

// Middleware returns an HTTP middleware function that performs authentication.
func (m *MultiProviderMiddleware) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, err := auth.ExtractBearerToken(r)
		if err != nil {
			m.writeError(w, http.StatusUnauthorized, err.Error())
			return
		}

		result := m.validateToken(r.Context(), token)
		if result.Error != nil {
			m.writeError(w, http.StatusUnauthorized, result.Error.Error())
			return
		}

		logger.Debugf("auth: token validated using provider %q", result.Provider)
		next.ServeHTTP(w, r)
	})
}

// validateToken attempts to validate the token by iterating through providers sequentially.
func (m *MultiProviderMiddleware) validateToken(ctx context.Context, token string) ValidationResult {
	var providerErrors []ProviderError

	for _, nv := range m.validators {
		_, err := nv.validator.ValidateToken(ctx, token)
		if err != nil {
			providerErrors = append(providerErrors, ProviderError{
				Provider: nv.name,
				Error:    err,
			})
			logger.Debugf("auth: provider %q failed to validate token: %v", nv.name, err)
			continue
		}

		return ValidationResult{
			Provider: nv.name,
			Errors:   providerErrors,
		}
	}

	return ValidationResult{
		Error:  ErrAllProvidersFailed,
		Errors: providerErrors,
	}
}

// writeError writes a JSON error response
func (m *MultiProviderMiddleware) writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")

	// Build WWW-Authenticate header with resource_metadata per RFC 9728
	wwwAuth := fmt.Sprintf(`Bearer realm="%s"`, m.realm)
	if m.resourceURL != "" {
		wwwAuth = fmt.Sprintf(`Bearer realm="%s", resource_metadata="%s/.well-known/oauth-protected-resource"`,
			m.realm, m.resourceURL)
	}
	w.Header().Set("WWW-Authenticate", wwwAuth)
	w.WriteHeader(status)

	resp := struct {
		Error string `json:"error"`
	}{
		Error: message,
	}

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		logger.Errorf("auth: failed to encode error response: %v", err)
	}
}
