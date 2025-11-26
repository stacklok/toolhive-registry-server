package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	thvauth "github.com/stacklok/toolhive/pkg/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/internal/auth/mocks"
	"github.com/stacklok/toolhive-registry-server/internal/config"
)

func TestNewAuthMiddleware(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)

	// Mock validator factory for OAuth tests
	mockValidatorFactory := func(_ context.Context, _ thvauth.TokenValidatorConfig) (tokenValidatorInterface, error) {
		return mocks.NewMocktokenValidatorInterface(ctrl), nil
	}

	// Note: These tests assume that auth mode has already been resolved by resolveAuthMode()
	// in serve.go before NewAuthMiddleware is called. This means:
	// - Mode is never empty (always resolved to anonymous or oauth)
	// - Only valid modes (anonymous, oauth) are passed
	tests := []struct {
		name             string
		config           *config.AuthConfig
		validatorFactory validatorFactory
		wantErr          string
		wantHandler      bool // whether handler should be non-nil
	}{
		{
			name:             "nil config returns error (auth required by default)",
			config:           nil,
			validatorFactory: DefaultValidatorFactory,
			wantErr:          "auth configuration is required",
		},
		{
			name:             "anonymous mode succeeds",
			config:           &config.AuthConfig{Mode: config.AuthModeAnonymous},
			validatorFactory: DefaultValidatorFactory,
			wantHandler:      false,
		},
		{
			name:             "unsupported mode returns error",
			config:           &config.AuthConfig{Mode: "custom"},
			validatorFactory: DefaultValidatorFactory,
			wantErr:          "invalid auth.mode",
		},
		{
			name:             "empty mode returns error (should be resolved before calling)",
			config:           &config.AuthConfig{Mode: ""},
			validatorFactory: DefaultValidatorFactory,
			wantErr:          "invalid auth.mode",
		},
		{
			name: "oauth mode with no providers returns error",
			config: &config.AuthConfig{
				Mode: config.AuthModeOAuth,
				OAuth: &config.OAuthConfig{
					Providers: nil,
				},
			},
			validatorFactory: mockValidatorFactory,
			wantErr:          "auth.oauth.providers is required",
		},
		{
			name: "oauth mode with valid provider succeeds",
			config: &config.AuthConfig{
				Mode: config.AuthModeOAuth,
				OAuth: &config.OAuthConfig{
					ResourceURL: "https://registry.example.com",
					Providers: []config.OAuthProviderConfig{{
						Name:      "test-provider",
						IssuerURL: "https://issuer.example.com",
						Audience:  "test-audience",
					}},
				},
			},
			validatorFactory: mockValidatorFactory,
			wantHandler:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			middleware, handler, err := NewAuthMiddleware(context.Background(), tt.config, tt.validatorFactory)

			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.NotNil(t, middleware)

			if tt.wantHandler {
				assert.NotNil(t, handler)
			} else {
				assert.Nil(t, handler)
			}
		})
	}
}

func TestNewAuthMiddleware_ClientSecretFile(t *testing.T) {
	t.Parallel()

	t.Run("reads client secret from file", func(t *testing.T) {
		t.Parallel()

		ctrl := gomock.NewController(t)
		var capturedConfig thvauth.TokenValidatorConfig
		capturingFactory := func(_ context.Context, cfg thvauth.TokenValidatorConfig) (tokenValidatorInterface, error) {
			capturedConfig = cfg
			return mocks.NewMocktokenValidatorInterface(ctrl), nil
		}

		// Create temp file with secret
		tempDir := t.TempDir()
		secretFile := filepath.Join(tempDir, "secret.txt")
		err := os.WriteFile(secretFile, []byte("my-test-secret"), 0600)
		require.NoError(t, err)

		cfg := &config.AuthConfig{
			Mode: config.AuthModeOAuth,
			OAuth: &config.OAuthConfig{
				ResourceURL: "https://registry.example.com",
				Providers: []config.OAuthProviderConfig{{
					Name:             "test-provider",
					IssuerURL:        "https://issuer.example.com",
					Audience:         "test-audience",
					ClientSecretFile: secretFile,
				}},
			},
		}

		middleware, handler, err := NewAuthMiddleware(context.Background(), cfg, capturingFactory)
		require.NoError(t, err)
		assert.NotNil(t, middleware)
		assert.NotNil(t, handler)
		assert.Equal(t, "my-test-secret", capturedConfig.ClientSecret)
	})

	t.Run("returns error for missing secret file", func(t *testing.T) {
		t.Parallel()

		ctrl := gomock.NewController(t)
		mockFactory := func(_ context.Context, _ thvauth.TokenValidatorConfig) (tokenValidatorInterface, error) {
			return mocks.NewMocktokenValidatorInterface(ctrl), nil
		}

		cfg := &config.AuthConfig{
			Mode: config.AuthModeOAuth,
			OAuth: &config.OAuthConfig{
				ResourceURL: "https://registry.example.com",
				Providers: []config.OAuthProviderConfig{{
					Name:             "test-provider",
					IssuerURL:        "https://issuer.example.com",
					Audience:         "test-audience",
					ClientSecretFile: "/nonexistent/secret.txt",
				}},
			},
		}

		_, _, err := NewAuthMiddleware(context.Background(), cfg, mockFactory)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to read client secret")
		assert.Contains(t, err.Error(), "test-provider")
	})
}

func TestAnonymousMiddleware(t *testing.T) {
	t.Parallel()

	called := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	wrapped := anonymousMiddleware(handler)

	req := httptest.NewRequest("GET", "/test", nil)
	rr := httptest.NewRecorder()
	wrapped.ServeHTTP(rr, req)

	assert.True(t, called, "handler should be called")
	assert.Equal(t, http.StatusOK, rr.Code)
}
