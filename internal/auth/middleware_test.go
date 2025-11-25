package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	thvauth "github.com/stacklok/toolhive/pkg/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/internal/auth/mocks"
)

// singleProviderConfig returns a minimal provider config for testing.
func singleProviderConfig() []providerConfig {
	return []providerConfig{{
		Name:      "test-provider",
		IssuerURL: "https://issuer.example.com",
		ValidatorConfig: thvauth.TokenValidatorConfig{
			Issuer:   "https://issuer.example.com",
			Audience: "test-audience",
		},
	}}
}

func TestNewMultiProviderMiddleware_EmptyProviders(t *testing.T) {
	t.Parallel()

	_, err := NewMultiProviderMiddleware(context.Background(), nil, "", "", DefaultValidatorFactory)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one provider must be configured")
}

func TestMultiProviderMiddleware_Middleware(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		authHeader string
		setupMock  func(*mocks.MockTokenValidatorInterface)
		wantStatus int
		wantCalled bool
	}{
		{
			name:       "missing authorization header",
			authHeader: "",
			wantStatus: http.StatusUnauthorized,
			wantCalled: false,
		},
		{
			name:       "invalid bearer format - Basic auth",
			authHeader: "Basic xyz",
			wantStatus: http.StatusUnauthorized,
			wantCalled: false,
		},
		{
			name:       "empty bearer token",
			authHeader: "Bearer ",
			wantStatus: http.StatusUnauthorized,
			wantCalled: false,
		},
		{
			name:       "valid token",
			authHeader: "Bearer valid-token",
			setupMock: func(m *mocks.MockTokenValidatorInterface) {
				m.EXPECT().ValidateToken(gomock.Any(), "valid-token").
					Return(map[string]any{"sub": "user"}, nil)
			},
			wantStatus: http.StatusOK,
			wantCalled: true,
		},
		{
			name:       "invalid token",
			authHeader: "Bearer bad-token",
			setupMock: func(m *mocks.MockTokenValidatorInterface) {
				m.EXPECT().ValidateToken(gomock.Any(), "bad-token").
					Return(nil, errors.New("validation failed"))
			},
			wantStatus: http.StatusUnauthorized,
			wantCalled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			mockValidator := mocks.NewMockTokenValidatorInterface(ctrl)
			if tt.setupMock != nil {
				tt.setupMock(mockValidator)
			}

			m, err := NewMultiProviderMiddleware(
				context.Background(),
				singleProviderConfig(),
				"https://api.example.com",
				"",
				func(_ context.Context, _ thvauth.TokenValidatorConfig) (TokenValidatorInterface, error) {
					return mockValidator, nil
				},
			)
			require.NoError(t, err)

			called := false
			handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				called = true
				w.WriteHeader(http.StatusOK)
			})

			wrapped := m.Middleware(handler)

			req := httptest.NewRequest("GET", "/test", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			rr := httptest.NewRecorder()

			wrapped.ServeHTTP(rr, req)

			assert.Equal(t, tt.wantStatus, rr.Code)
			assert.Equal(t, tt.wantCalled, called)

			if tt.wantStatus == http.StatusUnauthorized {
				assert.NotEmpty(t, rr.Header().Get("WWW-Authenticate"))
			}
		})
	}
}

func TestMultiProviderMiddleware_SequentialFallback(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)

	// Two different providers with different issuers
	keycloakMock := mocks.NewMockTokenValidatorInterface(ctrl)
	googleMock := mocks.NewMockTokenValidatorInterface(ctrl)

	// First provider (Keycloak) fails, second (Google) succeeds
	gomock.InOrder(
		keycloakMock.EXPECT().ValidateToken(gomock.Any(), "token").
			Return(nil, errors.New("invalid issuer")),
		googleMock.EXPECT().ValidateToken(gomock.Any(), "token").
			Return(map[string]any{"sub": "user@google.com"}, nil),
	)

	providers := []providerConfig{
		{
			Name:      "keycloak",
			IssuerURL: "https://keycloak.example.com",
			ValidatorConfig: thvauth.TokenValidatorConfig{
				Issuer:   "https://keycloak.example.com",
				Audience: "my-app",
			},
		},
		{
			Name:      "google",
			IssuerURL: "https://accounts.google.com",
			ValidatorConfig: thvauth.TokenValidatorConfig{
				Issuer:   "https://accounts.google.com",
				Audience: "my-app.apps.googleusercontent.com",
			},
		},
	}

	// Factory returns the correct mock based on call order
	callIdx := 0
	validatorMocks := []*mocks.MockTokenValidatorInterface{keycloakMock, googleMock}

	m, err := NewMultiProviderMiddleware(
		context.Background(),
		providers,
		"",
		"",
		func(_ context.Context, _ thvauth.TokenValidatorConfig) (TokenValidatorInterface, error) {
			mock := validatorMocks[callIdx]
			callIdx++
			return mock, nil
		},
	)
	require.NoError(t, err)

	called := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	wrapped := m.Middleware(handler)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer token")
	rr := httptest.NewRecorder()

	wrapped.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.True(t, called)
}

func TestSanitizeHeaderValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"clean value", "mcp-registry", "mcp-registry"},
		{"removes newline", "realm\ninjected: evil", "realminjected: evil"},
		{"removes carriage return", "realm\rinjected", "realminjected"},
		{"removes CRLF", "realm\r\nX-Injected: evil", "realmX-Injected: evil"},
		{"escapes quotes", `realm"with"quotes`, `realm\"with\"quotes`},
		{"handles multiple issues", "bad\r\n\"value\"", `bad\"value\"`},
		{"empty string", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := sanitizeHeaderValue(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMultiProviderMiddleware_WWWAuthenticate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		resourceURL string
		realm       string
		wantContain string
	}{
		{
			name:        "with resource URL",
			resourceURL: "https://api.example.com",
			realm:       "test-realm",
			wantContain: `resource_metadata="https://api.example.com/.well-known/oauth-protected-resource"`,
		},
		{
			name:        "without resource URL",
			resourceURL: "",
			realm:       "test-realm",
			wantContain: `realm="test-realm"`,
		},
		{
			name:        "default realm",
			resourceURL: "",
			realm:       "",
			wantContain: `realm="mcp-registry"`,
		},
		{
			name:        "sanitizes realm with injection attempt",
			resourceURL: "",
			realm:       "evil\r\nX-Injected: header",
			wantContain: `realm="evilX-Injected: header"`,
		},
		{
			name:        "sanitizes resource URL with injection attempt",
			resourceURL: "https://evil.com\r\nX-Injected: header",
			realm:       "test",
			wantContain: `resource_metadata="https://evil.comX-Injected: header/.well-known/oauth-protected-resource"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			mockValidator := mocks.NewMockTokenValidatorInterface(ctrl)
			mockValidator.EXPECT().ValidateToken(gomock.Any(), gomock.Any()).
				Return(nil, errors.New("fail")).AnyTimes()

			m, err := NewMultiProviderMiddleware(
				context.Background(),
				singleProviderConfig(),
				tt.resourceURL,
				tt.realm,
				func(_ context.Context, _ thvauth.TokenValidatorConfig) (TokenValidatorInterface, error) {
					return mockValidator, nil
				},
			)
			require.NoError(t, err)

			handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			wrapped := m.Middleware(handler)

			req := httptest.NewRequest("GET", "/test", nil)
			req.Header.Set("Authorization", "Bearer test-token")
			rr := httptest.NewRecorder()

			wrapped.ServeHTTP(rr, req)

			wwwAuth := rr.Header().Get("WWW-Authenticate")
			assert.Contains(t, wwwAuth, tt.wantContain)
		})
	}
}
