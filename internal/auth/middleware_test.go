package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
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

	_, err := newMultiProviderMiddleware(context.Background(), nil, "", "", DefaultValidatorFactory)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one provider must be configured")
}

func TestMultiProviderMiddleware_Middleware(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		authHeader string
		setupMock  func(*mocks.MocktokenValidatorInterface)
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
			setupMock: func(m *mocks.MocktokenValidatorInterface) {
				m.EXPECT().ValidateToken(gomock.Any(), "valid-token").
					Return(map[string]any{"sub": "user"}, nil)
			},
			wantStatus: http.StatusOK,
			wantCalled: true,
		},
		{
			name:       "invalid token",
			authHeader: "Bearer bad-token",
			setupMock: func(m *mocks.MocktokenValidatorInterface) {
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
			mockValidator := mocks.NewMocktokenValidatorInterface(ctrl)
			if tt.setupMock != nil {
				tt.setupMock(mockValidator)
			}

			m, err := newMultiProviderMiddleware(
				context.Background(),
				singleProviderConfig(),
				"https://api.example.com",
				"",
				func(_ context.Context, _ thvauth.TokenValidatorConfig) (tokenValidatorInterface, error) {
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
	keycloakMock := mocks.NewMocktokenValidatorInterface(ctrl)
	googleMock := mocks.NewMocktokenValidatorInterface(ctrl)

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
	validatorMocks := []*mocks.MocktokenValidatorInterface{keycloakMock, googleMock}

	m, err := newMultiProviderMiddleware(
		context.Background(),
		providers,
		"",
		"",
		func(_ context.Context, _ thvauth.TokenValidatorConfig) (tokenValidatorInterface, error) {
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
			mockValidator := mocks.NewMocktokenValidatorInterface(ctrl)
			mockValidator.EXPECT().ValidateToken(gomock.Any(), gomock.Any()).
				Return(nil, errors.New("fail")).AnyTimes()

			m, err := newMultiProviderMiddleware(
				context.Background(),
				singleProviderConfig(),
				tt.resourceURL,
				tt.realm,
				func(_ context.Context, _ thvauth.TokenValidatorConfig) (tokenValidatorInterface, error) {
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

func TestWrapWithPublicPaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		path           string
		publicPaths    []string
		expectAuthCall bool
	}{
		// Public paths bypass auth
		{"exact public path bypasses auth", "/health", []string{"/health"}, false},
		{"sub-path of public bypasses auth", "/health/check", []string{"/health"}, false},
		{"well-known bypasses auth", "/.well-known/oauth", []string{"/.well-known"}, false},

		// Protected paths require auth
		{"protected path requires auth", "/v0/servers", []string{"/health"}, true},
		{"similar prefix still requires auth", "/healthcheck", []string{"/health"}, true},
		{"empty public paths requires auth", "/health", []string{}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			authCalled := false
			mockAuthMw := func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					authCalled = true
					next.ServeHTTP(w, r)
				})
			}

			mw := WrapWithPublicPaths(mockAuthMw, tt.publicPaths)
			handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			req, _ := http.NewRequest(http.MethodGet, tt.path, nil)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			assert.Equal(t, tt.expectAuthCall, authCalled)
		})
	}
}

// TestMultiProviderMiddleware_LogsIdentity verifies that on successful
// authentication the middleware emits sub/user fields (not the legacy
// "subject" field that returned null in v1.1.2 — the symptom in #731).
// Also proves the identity holder is populated so an outer access logger
// can read it after the chain returns.
//
// Not t.Parallel: this test mutates slog.Default() globally to capture
// log output.
func TestMultiProviderMiddleware_LogsIdentity(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockValidator := mocks.NewMocktokenValidatorInterface(ctrl)
	mockValidator.EXPECT().ValidateToken(gomock.Any(), "valid-token").
		Return(map[string]any{
			"sub":                "f1c2d3e4-5678-90ab-cdef-1234567890ab",
			"name":               "Alice Example",
			"preferred_username": "alice",
			"email":              "alice@example.com",
		}, nil)

	m, err := newMultiProviderMiddleware(
		context.Background(),
		singleProviderConfig(),
		"",
		"",
		func(_ context.Context, _ thvauth.TokenValidatorConfig) (tokenValidatorInterface, error) {
			return mockValidator, nil
		},
	)
	require.NoError(t, err)

	// Capture both the slog output AND the post-chain identity from a
	// holder installed in the outer context.
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	wrapped := m.Middleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/registry/demo/v0.1/servers", nil)
	req.Header.Set("Authorization", "Bearer valid-token")

	// Install holder in the request's context — this stands in for the
	// outer LoggingMiddleware. After the chain returns, the holder must
	// be populated.
	outerCtx := WithIdentityHolder(req.Context())
	req = req.WithContext(outerCtx)

	rr := httptest.NewRecorder()
	wrapped.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)

	// Holder must reflect the authenticated identity, visible to the
	// outer scope despite the auth middleware attaching claims via a
	// child context.
	sub, user := IdentityFromContext(req.Context())
	assert.Equal(t, "f1c2d3e4-5678-90ab-cdef-1234567890ab", sub)
	assert.Equal(t, "Alice Example", user)

	// Slog line for "Authentication successful" must carry sub/user (not
	// the broken "subject" field with a null value).
	var found bool
	for _, line := range bytes.Split(bytes.TrimRight(buf.Bytes(), "\n"), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var rec map[string]any
		require.NoError(t, json.Unmarshal(line, &rec))
		if rec["msg"] != "Authentication successful" {
			continue
		}
		found = true
		assert.Equal(t, "f1c2d3e4-5678-90ab-cdef-1234567890ab", rec["sub"])
		assert.Equal(t, "Alice Example", rec["user"])
		assert.NotContains(t, rec, "subject", `legacy "subject" field should be gone`)
		assert.Equal(t, "test-provider", rec["provider"])
		assert.Equal(t, "/registry/demo/v0.1/servers", rec["path"])
	}
	assert.True(t, found, "Authentication successful log line not emitted")
}

// TestMultiProviderMiddleware_LogsIdentity_PreferredUsernameFallback verifies
// that when the JWT lacks `name` but has `preferred_username`, the latter is
// used as the user display name in the log line.
func TestMultiProviderMiddleware_LogsIdentity_PreferredUsernameFallback(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockValidator := mocks.NewMocktokenValidatorInterface(ctrl)
	mockValidator.EXPECT().ValidateToken(gomock.Any(), "tok").
		Return(map[string]any{
			"sub":                "u-1",
			"preferred_username": "alice",
		}, nil)

	m, err := newMultiProviderMiddleware(
		context.Background(),
		singleProviderConfig(),
		"",
		"",
		func(_ context.Context, _ thvauth.TokenValidatorConfig) (tokenValidatorInterface, error) {
			return mockValidator, nil
		},
	)
	require.NoError(t, err)

	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	wrapped := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer tok")
	wrapped.ServeHTTP(httptest.NewRecorder(), req)

	for _, line := range bytes.Split(bytes.TrimRight(buf.Bytes(), "\n"), []byte("\n")) {
		var rec map[string]any
		require.NoError(t, json.Unmarshal(line, &rec))
		if rec["msg"] == "Authentication successful" {
			assert.Equal(t, "u-1", rec["sub"])
			assert.Equal(t, "alice", rec["user"])
			return
		}
	}
	t.Fatal("Authentication successful log line not emitted")
}
