package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	thvauth "github.com/stacklok/toolhive/pkg/auth"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/internal/auth/mocks"
	"github.com/stacklok/toolhive-registry-server/internal/config"
)

func TestAdmissionIdentityEvidence(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, mode, header                   string
		allow, valid, established, anonymous bool
		status                               int
	}{
		{name: "anonymous established", mode: "anonymous", allow: true, established: true, anonymous: true, status: 200},
		{name: "anonymous rejects credentials", mode: "anonymous", allow: true, header: "Bearer invalid", status: 401},
		{name: "private missing", mode: "oauth", status: 401},
		{name: "optional absent", mode: "oauth", allow: true, established: true, anonymous: true, status: 200},
		{name: "optional bad credentials", mode: "oauth", allow: true, header: "Bearer invalid", status: 401},
		{name: "verified", mode: "oauth", header: "Bearer valid", valid: true, established: true, status: 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			validator := mocks.NewMocktokenValidatorInterface(ctrl)
			if tc.mode == "oauth" && tc.header != "" {
				if tc.valid {
					validator.EXPECT().ValidateToken(gomock.Any(), "valid").Return(map[string]any{"iss": "https://issuer", "sub": "user"}, nil)
				} else {
					validator.EXPECT().ValidateToken(gomock.Any(), "invalid").Return(nil, errors.New("SECRET provider diagnostic"))
				}
			}
			cfg := &config.AuthConfig{Mode: config.AuthMode(tc.mode), OAuth: &config.OAuthConfig{Providers: []config.OAuthProviderConfig{{Name: "one", IssuerURL: "https://issuer", Audience: "registry"}}}}
			cfg.OAuth.Realm = "admission-test"
			cfg.OAuth.ResourceURL = "https://registry.example.org"
			mw, _, err := NewAdmissionAuthMiddleware(context.Background(), cfg, tc.allow, func(context.Context, thvauth.TokenValidatorConfig) (tokenValidatorInterface, error) {
				return validator, nil
			})
			require.NoError(t, err)
			called := false
			handler := mw(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				called = true
				identity, ok := VerifiedIdentityFromContext(r.Context())
				require.Equal(t, tc.established, ok)
				require.Equal(t, tc.anonymous, identity.Anonymous)
				if !identity.Anonymous {
					require.Equal(t, "https://issuer", identity.Issuer)
					require.Equal(t, "user", identity.Subject)
				}
			}))
			request := httptest.NewRequest("GET", "/", nil)
			if tc.header != "" {
				request.Header.Set("Authorization", tc.header)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			require.Equal(t, tc.status, response.Code)
			require.Equal(t, tc.status == 200, called)
			require.NotContains(t, response.Body.String(), "SECRET")
			if response.Code == http.StatusUnauthorized {
				require.Contains(t, response.Header().Get("WWW-Authenticate"), "Bearer ")
				if tc.mode == "oauth" {
					require.Contains(t, response.Header().Get("WWW-Authenticate"), `realm="admission-test"`)
					require.Contains(t, response.Header().Get("WWW-Authenticate"), `resource_metadata="https://registry.example.org/.well-known/oauth-protected-resource"`)
				}
			}
		})
	}
	_, ok := VerifiedIdentityFromContext(ContextWithClaims(context.Background(), map[string]any{"sub": "anonymous", "iss": "https://issuer"}))
	require.False(t, ok)
}

func TestAdmissionInsufficientVerifiedIdentity(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		claims map[string]any
	}{
		{"missing issuer", map[string]any{"sub": "user"}},
		{"wrong issuer", map[string]any{"iss": "https://other", "sub": "user"}},
		{"missing subject", map[string]any{"iss": "https://issuer"}},
		{"bad subject", map[string]any{"iss": "https://issuer", "sub": []string{"user"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			validator := mocks.NewMocktokenValidatorInterface(gomock.NewController(t))
			validator.EXPECT().ValidateToken(gomock.Any(), "opaque").Return(tc.claims, nil)
			cfg := &config.AuthConfig{Mode: config.AuthModeOAuth, OAuth: &config.OAuthConfig{
				Providers: []config.OAuthProviderConfig{{Name: "one", IssuerURL: "https://issuer", Audience: "registry"}},
			}}
			mw, _, err := NewAdmissionAuthMiddleware(context.Background(), cfg, true,
				func(context.Context, thvauth.TokenValidatorConfig) (tokenValidatorInterface, error) {
					return validator, nil
				})
			require.NoError(t, err)
			request := httptest.NewRequest("GET", "/", nil)
			request.Header.Set("Authorization", "Bearer opaque")
			response := httptest.NewRecorder()
			mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("identity reached downstream") })).ServeHTTP(response, request)
			require.Equal(t, http.StatusUnauthorized, response.Code)
		})
	}
}
