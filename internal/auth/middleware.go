// Package auth provides authentication middleware for the registry API server.
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stacklok/toolhive/pkg/auth"
)

// errAllProvidersFailed indicates all providers failed during sequential fallback
var errAllProvidersFailed = errors.New("all providers failed to validate token")

// RFC 6750 Section 3 error codes
const (
	// errorCodeInvalidRequest indicates the request is missing a required parameter,
	// includes an unsupported parameter or parameter value, or is otherwise malformed.
	errorCodeInvalidRequest = "invalid_request"

	// errorCodeInvalidToken indicates the access token provided is expired, revoked,
	// malformed, or invalid for other reasons.
	errorCodeInvalidToken = "invalid_token"
)

// validationResult contains the outcome of token validation
type validationResult struct {
	// Provider is the name of the provider that validated the token
	Provider string

	// Error is set if validation failed
	Error error

	// Errors contains all errors from sequential fallback (for debugging)
	Errors []providerError

	// Claims contains the validated JWT claims (only set on success)
	Claims jwt.MapClaims
}

// providerError pairs a provider name with its validation error
type providerError struct {
	Provider string
	Error    error
}

// namedValidator pairs a validator with its provider metadata
type namedValidator struct {
	Name      string
	Validator tokenValidatorInterface
}

// defaultRealm is the default protection space identifier
const defaultRealm = "mcp-registry"

// validatorFactory creates token validators from configuration.
type validatorFactory func(ctx context.Context, cfg auth.TokenValidatorConfig) (tokenValidatorInterface, error)

// DefaultValidatorFactory uses the real ToolHive token validator.
var DefaultValidatorFactory validatorFactory = func(
	ctx context.Context,
	cfg auth.TokenValidatorConfig,
) (tokenValidatorInterface, error) {
	return auth.NewTokenValidator(ctx, cfg)
}

// multiProviderMiddleware handles authentication with multiple OAuth/OIDC providers.
type multiProviderMiddleware struct {
	validators  []namedValidator
	resourceURL string
	realm       string
}

// newMultiProviderMiddleware creates a new multi-provider authentication middleware.
func newMultiProviderMiddleware(
	ctx context.Context,
	providers []providerConfig,
	resourceURL string,
	realm string,
	factory validatorFactory,
) (*multiProviderMiddleware, error) {
	if len(providers) == 0 {
		return nil, errors.New("at least one provider must be configured")
	}

	// Apply default realm if not specified
	if realm == "" {
		realm = defaultRealm
	}

	m := &multiProviderMiddleware{
		validators:  make([]namedValidator, 0, len(providers)),
		resourceURL: resourceURL,
		realm:       realm,
	}

	for _, pc := range providers {
		validator, err := factory(ctx, pc.ValidatorConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create validator for provider %q: %w", pc.Name, err)
		}

		nv := namedValidator{
			Name:      pc.Name,
			Validator: validator,
		}
		m.validators = append(m.validators, nv)
	}

	return m, nil
}

// Middleware returns an HTTP middleware function that performs authentication.
func (m *multiProviderMiddleware) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, err := auth.ExtractBearerToken(r)
		if err != nil {
			slog.Warn("Token extraction failed",
				"error", err,
				"remote_addr", r.RemoteAddr,
				"path", r.URL.Path)
			m.writeError(w, http.StatusUnauthorized, errorCodeInvalidRequest, "missing or malformed authorization header")
			return
		}

		result := m.validateToken(r.Context(), token)
		if result.Error != nil {
			slog.Warn("Token validation failed",
				"error", result.Error,
				"remote_addr", r.RemoteAddr,
				"path", r.URL.Path)
			m.writeError(w, http.StatusUnauthorized, errorCodeInvalidToken, "token validation failed")
			return
		}

		slog.Info("Authentication successful",
			"provider", result.Provider,
			"subject", result.Claims["sub"],
			"remote_addr", r.RemoteAddr,
			"path", r.URL.Path)
		// TODO: Store claims in request context for downstream handlers (needed for authorization/scope enforcement)
		next.ServeHTTP(w, r)
	})
}

// validateToken attempts to validate the token by iterating through providers sequentially.
func (m *multiProviderMiddleware) validateToken(ctx context.Context, token string) validationResult {
	providerErrors := make([]providerError, 0, len(m.validators))

	for _, nv := range m.validators {
		claims, err := nv.Validator.ValidateToken(ctx, token)
		if err != nil {
			providerErrors = append(providerErrors, providerError{
				Provider: nv.Name,
				Error:    err,
			})
			slog.Debug("Provider failed to validate token", "provider", nv.Name, "error", err)
			continue
		}

		return validationResult{
			Provider: nv.Name,
			Claims:   claims,
			Errors:   providerErrors,
		}
	}

	return validationResult{
		Error:  errAllProvidersFailed,
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
// The errCode parameter should be one of the RFC 6750 error codes (invalid_request, invalid_token).
func (m *multiProviderMiddleware) writeError(w http.ResponseWriter, status int, errCode, description string) {
	w.Header().Set("Content-Type", "application/json")

	// Sanitize values to prevent header injection
	realm := sanitizeHeaderValue(m.realm)
	resourceURL := sanitizeHeaderValue(m.resourceURL)
	sanitizedDescription := sanitizeHeaderValue(description)

	// Build WWW-Authenticate header with error codes per RFC 6750 Section 3
	wwwAuth := fmt.Sprintf(`Bearer realm="%s", error="%s", error_description="%s"`,
		realm, errCode, sanitizedDescription)
	if resourceURL != "" {
		wwwAuth = fmt.Sprintf(
			`Bearer realm="%s", error="%s", error_description="%s", resource_metadata="%s/.well-known/oauth-protected-resource"`,
			realm, errCode, sanitizedDescription, resourceURL)
	}
	w.Header().Set("WWW-Authenticate", wwwAuth)
	w.WriteHeader(status)

	resp := struct {
		Error string `json:"error"`
	}{
		Error: description,
	}

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("Failed to encode error response", "error", err)
	}
}

// WrapWithPublicPaths wraps an auth middleware to bypass authentication for public paths.
// It checks each request path against the provided list of public paths using IsPublicPath.
// Requests to public paths are passed directly to the next handler without authentication,
// while all other requests go through the provided auth middleware.
func WrapWithPublicPaths(
	authMw func(http.Handler) http.Handler,
	publicPaths []string,
) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		// Pre-wrap the handler once during initialization, not per-request
		authWrappedNext := authMw(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !IsPublicPath(r.URL.Path, publicPaths) {
				authWrappedNext.ServeHTTP(w, r)
			} else {
				next.ServeHTTP(w, r)
			}
		})
	}
}
