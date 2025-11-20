package auth

import (
	"context"
	"fmt"
	"net/http"

	"github.com/stacklok/toolhive/pkg/auth"

	"github.com/stacklok/toolhive-registry-server/internal/config"
)

// NewAuthMiddleware creates authentication middleware based on config.
// Returns: (middleware, authInfoHandler, error)
func NewAuthMiddleware(ctx context.Context, cfg *config.AuthConfig) (func(http.Handler) http.Handler, http.Handler, error) {
	// Handle nil config - defaults to anonymous
	if cfg == nil {
		return AnonymousMiddleware, nil, nil
	}

	switch cfg.Mode {
	case config.AuthModeAnonymous, "":
		return AnonymousMiddleware, nil, nil
	case config.AuthModeOAuth:
		return createOAuthMiddleware(ctx, cfg)
	default:
		return nil, nil, fmt.Errorf("unsupported auth mode: %s", cfg.Mode)
	}
}

// createOAuthMiddleware creates OAuth/OIDC multi-provider middleware from config
func createOAuthMiddleware(ctx context.Context, cfg *config.AuthConfig) (func(http.Handler) http.Handler, http.Handler, error) {
	providers := make([]providerConfig, len(cfg.Providers))
	issuerURLs := make([]string, len(cfg.Providers))

	for i, p := range cfg.Providers {
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
			ValidatorConfig: auth.TokenValidatorConfig{
				Issuer:       p.IssuerURL,
				Audience:     p.Audience,
				ClientID:     p.ClientID,
				ClientSecret: clientSecret,
				CACertPath:   p.CACertPath,
			},
		}
		issuerURLs[i] = p.IssuerURL
	}

	m, err := NewMultiProviderMiddleware(ctx, providers, cfg.ResourceURL, cfg.Realm)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create multi-provider middleware: %w", err)
	}

	// Create RFC 9728 compliant protected resource handler
	handler := NewProtectedResourceHandler(cfg.ResourceURL, issuerURLs, cfg.ScopesSupported)

	return m.Middleware, handler, nil
}

// AnonymousMiddleware is a no-op middleware that passes requests through without authentication.
func AnonymousMiddleware(next http.Handler) http.Handler {
	return next
}
