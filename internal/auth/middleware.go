// Package auth provides authentication middleware for the registry API server.
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/stacklok/toolhive/pkg/auth"
	"github.com/stacklok/toolhive/pkg/logger"
)

// Domain errors for authentication
var (
	// ErrAllProvidersFailed indicates all providers failed during sequential fallback
	ErrAllProvidersFailed = errors.New("all providers failed to validate token")

	// ErrMissingToken indicates the authorization header is missing
	ErrMissingToken = errors.New("authorization header missing")

	// ErrInvalidTokenFormat indicates the token format is invalid (not Bearer)
	ErrInvalidTokenFormat = errors.New("invalid bearer token format")
)

// RFC 6750 Section 3 error codes
const (
	// ErrorCodeInvalidRequest indicates the request is missing a required parameter,
	// includes an unsupported parameter or parameter value, or is otherwise malformed.
	ErrorCodeInvalidRequest = "invalid_request"

	// ErrorCodeInvalidToken indicates the access token provided is expired, revoked,
	// malformed, or invalid for other reasons.
	ErrorCodeInvalidToken = "invalid_token"
)

// ValidationResult contains the outcome of token validation
type ValidationResult struct {
	// Provider is the name of the provider that validated the token
	Provider string

	// Error is set if validation failed
	Error error

	// Errors contains all errors from sequential fallback (for debugging)
	Errors []ProviderError
}

// ProviderError pairs a provider name with its validation error
type ProviderError struct {
	Provider string
	Error    error
}

// NamedValidator pairs a validator with its provider metadata
type NamedValidator struct {
	Name      string
	Validator TokenValidatorInterface
}

// DefaultRealm is the default protection space identifier
const DefaultRealm = "mcp-registry"

// MultiProviderMiddleware handles authentication with multiple OAuth/OIDC providers.
type MultiProviderMiddleware struct {
	validators  []NamedValidator
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
		validators:  make([]NamedValidator, 0, len(providers)),
		resourceURL: resourceURL,
		realm:       realm,
	}

	for _, pc := range providers {
		validator, err := auth.NewTokenValidator(ctx, pc.ValidatorConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create validator for provider %q: %w", pc.Name, err)
		}

		nv := NamedValidator{
			Name:      pc.Name,
			Validator: validator,
		}
		m.validators = append(m.validators, nv)
	}

	return m, nil
}

// NewMultiProviderMiddlewareWithValidators creates middleware with pre-built validators.
// This is primarily for testing with mock validators.
func NewMultiProviderMiddlewareWithValidators(
	validators []NamedValidator,
	resourceURL string,
	realm string,
) *MultiProviderMiddleware {
	if realm == "" {
		realm = DefaultRealm
	}
	return &MultiProviderMiddleware{
		validators:  validators,
		resourceURL: resourceURL,
		realm:       realm,
	}
}

// Middleware returns an HTTP middleware function that performs authentication.
func (m *MultiProviderMiddleware) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, err := auth.ExtractBearerToken(r)
		if err != nil {
			logger.Debugf("auth: token extraction failed: %v", err)
			m.writeError(w, http.StatusUnauthorized, ErrorCodeInvalidRequest, "missing or malformed authorization header")
			return
		}

		result := m.validateToken(r.Context(), token)
		if result.Error != nil {
			logger.Debugf("auth: token validation failed: %v", result.Error)
			m.writeError(w, http.StatusUnauthorized, ErrorCodeInvalidToken, "token validation failed")
			return
		}

		logger.Debugf("auth: token validated using provider %q", result.Provider)
		// TODO: Store claims in request context for downstream handlers (needed for authorization/scope enforcement)
		next.ServeHTTP(w, r)
	})
}

// validateToken attempts to validate the token by iterating through providers sequentially.
func (m *MultiProviderMiddleware) validateToken(ctx context.Context, token string) ValidationResult {
	providerErrors := make([]ProviderError, 0, len(m.validators))

	for _, nv := range m.validators {
		_, err := nv.Validator.ValidateToken(ctx, token)
		if err != nil {
			providerErrors = append(providerErrors, ProviderError{
				Provider: nv.Name,
				Error:    err,
			})
			logger.Debugf("auth: provider %q failed to validate token: %v", nv.Name, err)
			continue
		}

		return ValidationResult{
			Provider: nv.Name,
			Errors:   providerErrors,
		}
	}

	return ValidationResult{
		Error:  ErrAllProvidersFailed,
		Errors: providerErrors,
	}
}

// sanitizeHeaderValue removes characters that could enable header injection attacks.
// This includes newlines, carriage returns, and unescaped quotes.
func sanitizeHeaderValue(s string) string {
	// Fast path: no sanitization needed
	if !strings.ContainsAny(s, "\r\n\"") {
		return s
	}
	// Remove CR and LF to prevent header injection
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\n", "")
	// Escape quotes for use in quoted-string (RFC 7230)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}

// writeError writes a JSON error response with RFC 6750 compliant WWW-Authenticate header.
// The errorCode parameter should be one of the RFC 6750 error codes (invalid_request, invalid_token).
func (m *MultiProviderMiddleware) writeError(w http.ResponseWriter, status int, errorCode, description string) {
	w.Header().Set("Content-Type", "application/json")

	// Sanitize values to prevent header injection
	realm := sanitizeHeaderValue(m.realm)
	resourceURL := sanitizeHeaderValue(m.resourceURL)
	sanitizedDescription := sanitizeHeaderValue(description)

	// Build WWW-Authenticate header with error codes per RFC 6750 Section 3
	wwwAuth := fmt.Sprintf(`Bearer realm="%s", error="%s", error_description="%s"`,
		realm, errorCode, sanitizedDescription)
	if resourceURL != "" {
		wwwAuth = fmt.Sprintf(
			`Bearer realm="%s", error="%s", error_description="%s", resource_metadata="%s/.well-known/oauth-protected-resource"`,
			realm, errorCode, sanitizedDescription, resourceURL)
	}
	w.Header().Set("WWW-Authenticate", wwwAuth)
	w.WriteHeader(status)

	resp := struct {
		Error string `json:"error"`
	}{
		Error: description,
	}

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		logger.Errorf("auth: failed to encode error response: %v", err)
	}
}
