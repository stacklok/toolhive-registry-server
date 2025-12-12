package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	thvauth "github.com/stacklok/toolhive/pkg/auth"

	"github.com/stacklok/toolhive-registry-server/internal/config"
)

// NewAuthMiddleware creates authentication middleware based on config.
// Returns: (middleware, authInfoHandler, error)
//
// By default, authentication is ENABLED and requires OAuth configuration.
// To disable authentication for development, either:
//   - Use --auth-mode=anonymous flag
//   - Set auth.mode: anonymous in the config file
//
// This function validates the auth configuration before creating the middleware.
func NewAuthMiddleware(
	ctx context.Context,
	cfg *config.AuthConfig,
	factory validatorFactory,
) (func(http.Handler) http.Handler, http.Handler, error) {
	// Handle nil config - authentication is required by default
	if cfg == nil {
		return nil, nil, errors.New("auth configuration is required")
	}

	// Validate the auth configuration
	// Pass false for insecureAllowHTTP since this is already validated during config load
	if err := cfg.Validate(false); err != nil {
		return nil, nil, fmt.Errorf("invalid auth configuration: %w", err)
	}

	switch cfg.Mode {
	case config.AuthModeAnonymous:
		slog.Info("Auth mode configured", "mode", "anonymous")
		return anonymousMiddleware, nil, nil
	case config.AuthModeOAuth:
		return createOAuthMiddleware(ctx, cfg, factory)
	default:
		return nil, nil, fmt.Errorf("unsupported auth mode: %s", cfg.Mode)
	}
}

// createOAuthMiddleware creates OAuth/OIDC multi-provider middleware from config
func createOAuthMiddleware(
	ctx context.Context,
	cfg *config.AuthConfig,
	factory validatorFactory,
) (func(http.Handler) http.Handler, http.Handler, error) {
	if cfg.OAuth == nil {
		return nil, nil, errors.New("oauth configuration is required for oauth mode")
	}
	oauth := cfg.OAuth
	providers := make([]providerConfig, len(oauth.Providers))
	issuerURLs := make([]string, len(oauth.Providers))

	for i, p := range oauth.Providers {
		// Read client secret from file if configured (read immediately before creating provider)
		var clientSecret string
		if p.ClientSecretFile != "" {
			secret, err := p.GetClientSecret()
			if err != nil {
				return nil, nil, fmt.Errorf("failed to read client secret for provider %q: %w", p.Name, err)
			}
			clientSecret = secret
		}

		providers[i] = providerConfig{
			Name:      p.Name,
			IssuerURL: p.IssuerURL,
			ValidatorConfig: thvauth.TokenValidatorConfig{
				Issuer:           p.IssuerURL,
				JWKSURL:          p.JwksUrl,
				Audience:         p.Audience,
				ClientID:         p.ClientID,
				ClientSecret:     clientSecret,
				CACertPath:       p.CACertPath,
				IntrospectionURL: p.IntrospectionURL,
				AllowPrivateIP:   p.AllowPrivateIP,
			},
		}
		issuerURLs[i] = p.IssuerURL
	}

	m, err := newMultiProviderMiddleware(ctx, providers, oauth.ResourceURL, oauth.Realm, factory)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create multi-provider middleware: %w", err)
	}

	// Create RFC 9728 compliant protected resource handler
	handler, err := newProtectedResourceHandler(oauth.ResourceURL, issuerURLs, oauth.ScopesSupported)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create protected resource handler: %w", err)
	}

	slog.Info("Auth mode configured", "mode", "OAuth")

	return m.Middleware, handler, nil
}

// anonymousMiddleware is a no-op middleware that passes requests through without authentication.
func anonymousMiddleware(next http.Handler) http.Handler {
	return next
}
