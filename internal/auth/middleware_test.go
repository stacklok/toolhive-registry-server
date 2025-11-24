package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/internal/auth/mocks"
)

func TestNewMultiProviderMiddleware_EmptyProviders(t *testing.T) {
	t.Parallel()

	_, err := NewMultiProviderMiddleware(context.Background(), nil, "", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one provider must be configured")
}

func TestMultiProviderMiddleware_Middleware(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		authHeader    string
		setupMocks    func(*mocks.MockTokenValidatorInterface)
		numValidators int
		wantStatus    int
		wantCalled    bool
	}{
		{
			name:          "missing authorization header",
			authHeader:    "",
			numValidators: 1,
			wantStatus:    http.StatusUnauthorized,
			wantCalled:    false,
		},
		{
			name:          "invalid bearer format - Basic auth",
			authHeader:    "Basic xyz",
			numValidators: 1,
			wantStatus:    http.StatusUnauthorized,
			wantCalled:    false,
		},
		{
			name:          "empty bearer token",
			authHeader:    "Bearer ",
			numValidators: 1,
			wantStatus:    http.StatusUnauthorized,
			wantCalled:    false,
		},
		{
			name:       "valid token - first provider succeeds",
			authHeader: "Bearer valid-token",
			setupMocks: func(m *mocks.MockTokenValidatorInterface) {
				m.EXPECT().ValidateToken(gomock.Any(), "valid-token").
					Return(map[string]any{"sub": "user"}, nil)
			},
			numValidators: 1,
			wantStatus:    http.StatusOK,
			wantCalled:    true,
		},
		{
			name:       "all providers fail",
			authHeader: "Bearer bad-token",
			setupMocks: func(m *mocks.MockTokenValidatorInterface) {
				m.EXPECT().ValidateToken(gomock.Any(), "bad-token").
					Return(nil, errors.New("validation failed"))
			},
			numValidators: 1,
			wantStatus:    http.StatusUnauthorized,
			wantCalled:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)

			// Create validators with mocks
			validators := make([]NamedValidator, tt.numValidators)
			for i := range validators {
				mockValidator := mocks.NewMockTokenValidatorInterface(ctrl)
				if tt.setupMocks != nil {
					tt.setupMocks(mockValidator)
				}
				validators[i] = NamedValidator{
					Name:      "test-provider",
					Validator: mockValidator,
				}
			}

			m := NewMultiProviderMiddlewareWithValidators(validators, "https://api.example.com", "")

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

	mock1 := mocks.NewMockTokenValidatorInterface(ctrl)
	mock2 := mocks.NewMockTokenValidatorInterface(ctrl)

	// First validator fails, second succeeds
	gomock.InOrder(
		mock1.EXPECT().ValidateToken(gomock.Any(), "token").
			Return(nil, errors.New("invalid issuer")),
		mock2.EXPECT().ValidateToken(gomock.Any(), "token").
			Return(map[string]any{"sub": "user"}, nil),
	)

	validators := []NamedValidator{
		{Name: "provider1", Validator: mock1},
		{Name: "provider2", Validator: mock2},
	}

	m := NewMultiProviderMiddlewareWithValidators(validators, "", "")

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

			validators := []NamedValidator{
				{Name: "test", Validator: mockValidator},
			}

			m := NewMultiProviderMiddlewareWithValidators(validators, tt.resourceURL, tt.realm)

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
