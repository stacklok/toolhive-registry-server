package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stacklok/toolhive/pkg/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testAudience = "test-audience"
)

// testJWKSServer provides a mock OIDC provider for integration testing.
type testJWKSServer struct {
	*httptest.Server
	privateKey *rsa.PrivateKey
	keyID      string
	issuerURL  string
}

// newTestJWKSServer creates a new test JWKS server with generated RSA keys.
func newTestJWKSServer(t *testing.T) *testJWKSServer {
	t.Helper()

	// Generate RSA key pair
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	keyID := "test-key-1"

	ts := &testJWKSServer{
		privateKey: privateKey,
		keyID:      keyID,
	}

	mux := http.NewServeMux()

	// OIDC discovery endpoint
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		config := map[string]any{
			"issuer":                 ts.issuerURL,
			"jwks_uri":               ts.issuerURL + "/.well-known/jwks.json",
			"authorization_endpoint": ts.issuerURL + "/authorize",
			"token_endpoint":         ts.issuerURL + "/token",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(config)
	})

	// JWKS endpoint
	mux.HandleFunc("/.well-known/jwks.json", func(w http.ResponseWriter, _ *http.Request) {
		jwks := ts.generateJWKS()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(jwks)
	})

	ts.Server = httptest.NewServer(mux)
	ts.issuerURL = ts.URL

	return ts
}

// generateJWKS creates a JWKS JSON response with the server's public key.
func (ts *testJWKSServer) generateJWKS() map[string]any {
	pub := &ts.privateKey.PublicKey

	return map[string]any{
		"keys": []map[string]any{
			{
				"kty": "RSA",
				"use": "sig",
				"alg": "RS256",
				"kid": ts.keyID,
				"n":   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
				"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
			},
		},
	}
}

// generateToken creates a signed JWT with the given claims.
func (ts *testJWKSServer) generateToken(claims jwt.MapClaims) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = ts.keyID
	return token.SignedString(ts.privateKey)
}

// generateValidToken creates a valid token with standard claims.
func (ts *testJWKSServer) generateValidToken(audience string) (string, error) {
	claims := jwt.MapClaims{
		"iss": ts.issuerURL,
		"sub": "user-123",
		"aud": audience,
		"exp": time.Now().Add(time.Hour).Unix(),
		"iat": time.Now().Unix(),
	}
	return ts.generateToken(claims)
}

// generateExpiredToken creates a token that has already expired.
func (ts *testJWKSServer) generateExpiredToken(audience string) (string, error) {
	claims := jwt.MapClaims{
		"iss": ts.issuerURL,
		"sub": "user-123",
		"aud": audience,
		"exp": time.Now().Add(-time.Hour).Unix(),
		"iat": time.Now().Add(-2 * time.Hour).Unix(),
	}
	return ts.generateToken(claims)
}

// middlewareTestConfig holds configuration for setting up test middleware.
type middlewareTestConfig struct {
	audience    string
	resourceURL string
	realm       string
}

// setupMiddleware creates a multiProviderMiddleware with standard test configuration.
func setupMiddleware(t *testing.T, jwksServer *testJWKSServer, cfg middlewareTestConfig) *MultiProviderMiddleware {
	t.Helper()

	ctx := context.Background()
	providers := []providerConfig{
		{
			Name:      "test-provider",
			IssuerURL: jwksServer.issuerURL,
			ValidatorConfig: auth.TokenValidatorConfig{
				Issuer:            jwksServer.issuerURL,
				Audience:          cfg.audience,
				InsecureAllowHTTP: true,
				AllowPrivateIP:    true,
			},
		},
	}

	middleware, err := NewMultiProviderMiddleware(ctx, providers, cfg.resourceURL, cfg.realm, DefaultValidatorFactory)
	require.NoError(t, err)

	return middleware
}

// executeAuthRequest creates and executes an HTTP request with optional Bearer token authentication.
func executeAuthRequest(t *testing.T, handler http.Handler, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

// setupMultiProviderMiddleware creates a multiProviderMiddleware with multiple test JWKS servers.
func setupMultiProviderMiddleware(t *testing.T, servers ...*testJWKSServer) *MultiProviderMiddleware {
	t.Helper()

	ctx := context.Background()
	providers := make([]providerConfig, len(servers))
	for i, server := range servers {
		providers[i] = providerConfig{
			Name:      fmt.Sprintf("provider-%d", i+1),
			IssuerURL: server.issuerURL,
			ValidatorConfig: auth.TokenValidatorConfig{
				Issuer:            server.issuerURL,
				Audience:          testAudience,
				InsecureAllowHTTP: true,
				AllowPrivateIP:    true,
			},
		}
	}

	middleware, err := NewMultiProviderMiddleware(ctx, providers, "", "", DefaultValidatorFactory)
	require.NoError(t, err)

	return middleware
}

// newTestHandler creates a simple handler for testing that tracks if it was called.
// Returns the handler and a pointer to the called flag.
func newTestHandler() (http.Handler, *bool) {
	called := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	return handler, &called
}

func TestIntegration_AuthMiddleware(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		setupToken   func(jwksServer *testJWKSServer) (string, error)
		wantStatus   int
		wantCalled   bool
		checkWWWAuth func(t *testing.T, header string)
	}{
		{
			name: "valid token succeeds",
			setupToken: func(jwksServer *testJWKSServer) (string, error) {
				return jwksServer.generateValidToken(testAudience)
			},
			wantStatus: http.StatusOK,
			wantCalled: true,
		},
		{
			name: "expired token returns 401 with invalid_token",
			setupToken: func(jwksServer *testJWKSServer) (string, error) {
				return jwksServer.generateExpiredToken(testAudience)
			},
			wantStatus: http.StatusUnauthorized,
			wantCalled: false,
			checkWWWAuth: func(t *testing.T, header string) {
				t.Helper()
				assert.Contains(t, header, `error="invalid_token"`)
			},
		},
		{
			name: "wrong issuer returns 401",
			setupToken: func(jwksServer *testJWKSServer) (string, error) {
				claims := jwt.MapClaims{
					"iss": "https://wrong-issuer.example.com",
					"sub": "user-123",
					"aud": testAudience,
					"exp": time.Now().Add(time.Hour).Unix(),
					"iat": time.Now().Unix(),
				}
				return jwksServer.generateToken(claims)
			},
			wantStatus: http.StatusUnauthorized,
			wantCalled: false,
		},
		{
			name: "wrong audience returns 401",
			setupToken: func(jwksServer *testJWKSServer) (string, error) {
				return jwksServer.generateValidToken("wrong-audience")
			},
			wantStatus: http.StatusUnauthorized,
			wantCalled: false,
		},
		{
			name: "missing authorization header returns 401 with invalid_request",
			setupToken: func(_ *testJWKSServer) (string, error) {
				return "", nil
			},
			wantStatus: http.StatusUnauthorized,
			wantCalled: false,
			checkWWWAuth: func(t *testing.T, header string) {
				t.Helper()
				assert.Contains(t, header, `error="invalid_request"`)
			},
		},
		{
			name: "malformed token returns 401",
			setupToken: func(_ *testJWKSServer) (string, error) {
				return "not.a.valid.jwt.token", nil
			},
			wantStatus: http.StatusUnauthorized,
			wantCalled: false,
		},
		{
			name: "token with invalid signature returns 401",
			setupToken: func(jwksServer *testJWKSServer) (string, error) {
				// Generate a token with a different key than what the JWKS server exposes
				differentKey, err := rsa.GenerateKey(rand.Reader, 2048)
				if err != nil {
					return "", err
				}
				claims := jwt.MapClaims{
					"iss": jwksServer.issuerURL,
					"sub": "user-123",
					"aud": testAudience,
					"exp": time.Now().Add(time.Hour).Unix(),
					"iat": time.Now().Unix(),
				}
				token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
				token.Header["kid"] = jwksServer.keyID // Use correct kid but wrong key
				return token.SignedString(differentKey)
			},
			wantStatus: http.StatusUnauthorized,
			wantCalled: false,
			checkWWWAuth: func(t *testing.T, header string) {
				t.Helper()
				assert.Contains(t, header, `error="invalid_token"`)
			},
		},
		{
			name: "token with alg none attack returns 401",
			setupToken: func(jwksServer *testJWKSServer) (string, error) {
				// Craft a token with alg: none (algorithm confusion attack)
				// This should be rejected by the validator
				header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
				payload := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(
					`{"iss":"%s","sub":"attacker","aud":"%s","exp":%d}`,
					jwksServer.issuerURL, testAudience, time.Now().Add(time.Hour).Unix(),
				)))
				// alg:none tokens have empty signature
				return header + "." + payload + ".", nil
			},
			wantStatus: http.StatusUnauthorized,
			wantCalled: false,
		},
		{
			name: "token with unknown key ID returns 401",
			setupToken: func(jwksServer *testJWKSServer) (string, error) {
				// Generate a valid token but with a kid that doesn't exist in JWKS
				claims := jwt.MapClaims{
					"iss": jwksServer.issuerURL,
					"sub": "user-123",
					"aud": testAudience,
					"exp": time.Now().Add(time.Hour).Unix(),
					"iat": time.Now().Unix(),
				}
				token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
				token.Header["kid"] = "unknown-key-id" // Key ID not in JWKS
				return token.SignedString(jwksServer.privateKey)
			},
			wantStatus: http.StatusUnauthorized,
			wantCalled: false,
		},
		{
			name: "token with future nbf claim returns 401",
			setupToken: func(jwksServer *testJWKSServer) (string, error) {
				// Token with nbf (not before) set in the future
				claims := jwt.MapClaims{
					"iss": jwksServer.issuerURL,
					"sub": "user-123",
					"aud": testAudience,
					"exp": time.Now().Add(2 * time.Hour).Unix(),
					"iat": time.Now().Unix(),
					"nbf": time.Now().Add(time.Hour).Unix(), // Not valid until 1 hour from now
				}
				return jwksServer.generateToken(claims)
			},
			wantStatus: http.StatusUnauthorized,
			wantCalled: false,
			checkWWWAuth: func(t *testing.T, header string) {
				t.Helper()
				assert.Contains(t, header, `error="invalid_token"`)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create test JWKS server per subtest
			jwksServer := newTestJWKSServer(t)
			t.Cleanup(func() { jwksServer.Close() })

			// Create the middleware with real validators
			middleware := setupMiddleware(t, jwksServer, middlewareTestConfig{
				audience:    testAudience,
				resourceURL: "https://api.example.com",
				realm:       "test-realm",
			})

			// Test handler that tracks if it was called
			handler, handlerCalled := newTestHandler()

			wrapped := middleware.Middleware(handler)

			token, err := tt.setupToken(jwksServer)
			require.NoError(t, err, "setupToken should not fail")

			req := httptest.NewRequest("GET", "/api/v1/servers", nil)
			if token != "" {
				req.Header.Set("Authorization", "Bearer "+token)
			}
			rr := httptest.NewRecorder()

			wrapped.ServeHTTP(rr, req)

			assert.Equal(t, tt.wantStatus, rr.Code)
			assert.Equal(t, tt.wantCalled, *handlerCalled, "handler called status mismatch")

			if tt.wantStatus == http.StatusUnauthorized {
				wwwAuth := rr.Header().Get("WWW-Authenticate")
				assert.NotEmpty(t, wwwAuth)
				if tt.checkWWWAuth != nil {
					tt.checkWWWAuth(t, wwwAuth)
				}
			}
		})
	}
}

func TestIntegration_MultiProvider_Fallback(t *testing.T) {
	t.Parallel()

	// Create two test JWKS servers (simulating different providers)
	server1 := newTestJWKSServer(t)
	t.Cleanup(func() { server1.Close() })

	server2 := newTestJWKSServer(t)
	t.Cleanup(func() { server2.Close() })

	middleware := setupMultiProviderMiddleware(t, server1, server2)

	tests := []struct {
		name       string
		setupToken func(t *testing.T) string
		wantStatus int
		wantCalled bool
	}{
		{
			name: "token from second provider succeeds when first fails",
			setupToken: func(_ *testing.T) string {
				// Generate token from server2 (second provider)
				// This token's issuer won't match server1, so it will fail there
				// but should succeed on server2
				token, err := server2.generateValidToken(testAudience)
				require.NoError(t, err)
				return token
			},
			wantStatus: http.StatusOK,
			wantCalled: true,
		},
		{
			name: "token from unknown provider fails",
			setupToken: func(t *testing.T) string {
				t.Helper()
				// Create a third server that's not in the provider list
				unknownServer := newTestJWKSServer(t)
				t.Cleanup(func() { unknownServer.Close() })

				token, err := unknownServer.generateValidToken(testAudience)
				require.NoError(t, err)
				return token
			},
			wantStatus: http.StatusUnauthorized,
			wantCalled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			handler, handlerCalled := newTestHandler()
			wrapped := middleware.Middleware(handler)

			token := tt.setupToken(t)
			rr := executeAuthRequest(t, wrapped, "/api/v1/servers", token)

			assert.Equal(t, tt.wantStatus, rr.Code)
			assert.Equal(t, tt.wantCalled, *handlerCalled, "handler called status mismatch")
		})
	}
}

func TestIntegration_TokenClaims(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		claims     func(issuerURL string) jwt.MapClaims
		wantStatus int
		wantCalled bool
	}{
		{
			name: "token missing exp claim fails",
			claims: func(issuerURL string) jwt.MapClaims {
				return jwt.MapClaims{
					"iss": issuerURL,
					"sub": "user-123",
					"aud": testAudience,
					"iat": time.Now().Unix(),
					// No exp claim
				}
			},
			wantStatus: http.StatusUnauthorized,
			wantCalled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			jwksServer := newTestJWKSServer(t)
			t.Cleanup(func() { jwksServer.Close() })

			middleware := setupMiddleware(t, jwksServer, middlewareTestConfig{
				audience: testAudience,
			})

			handler, handlerCalled := newTestHandler()
			wrapped := middleware.Middleware(handler)

			claims := tt.claims(jwksServer.issuerURL)
			token, err := jwksServer.generateToken(claims)
			require.NoError(t, err)

			rr := executeAuthRequest(t, wrapped, "/test", token)

			assert.Equal(t, tt.wantStatus, rr.Code)
			assert.Equal(t, tt.wantCalled, *handlerCalled, "handler called status mismatch")
		})
	}
}

func TestIntegration_ConcurrentRequests(t *testing.T) {
	t.Parallel()

	jwksServer := newTestJWKSServer(t)
	t.Cleanup(func() { jwksServer.Close() })

	middleware := setupMiddleware(t, jwksServer, middlewareTestConfig{
		audience:    testAudience,
		resourceURL: "https://api.example.com",
	})

	// Use a simple handler without tracking for concurrent tests to avoid data races
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	wrapped := middleware.Middleware(handler)

	// Generate valid and invalid tokens
	validToken, err := jwksServer.generateValidToken(testAudience)
	require.NoError(t, err)

	expiredToken, err := jwksServer.generateExpiredToken(testAudience)
	require.NoError(t, err)

	const numRequests = 50
	results := make(chan struct {
		token  string
		status int
	}, numRequests*2)

	// Send concurrent requests with valid and invalid tokens
	var wg sync.WaitGroup
	for i := 0; i < numRequests; i++ {
		wg.Add(2)

		// Valid token request
		go func() {
			defer wg.Done()
			req := httptest.NewRequest("GET", "/api/v1/servers", nil)
			req.Header.Set("Authorization", "Bearer "+validToken)
			rr := httptest.NewRecorder()
			wrapped.ServeHTTP(rr, req)
			results <- struct {
				token  string
				status int
			}{"valid", rr.Code}
		}()

		// Invalid token request
		go func() {
			defer wg.Done()
			req := httptest.NewRequest("GET", "/api/v1/servers", nil)
			req.Header.Set("Authorization", "Bearer "+expiredToken)
			rr := httptest.NewRecorder()
			wrapped.ServeHTTP(rr, req)
			results <- struct {
				token  string
				status int
			}{"expired", rr.Code}
		}()
	}

	wg.Wait()
	close(results)

	// Verify all responses
	var validOK, expiredUnauth int
	for r := range results {
		if r.token == "valid" && r.status == http.StatusOK {
			validOK++
		} else if r.token == "expired" && r.status == http.StatusUnauthorized {
			expiredUnauth++
		}
	}

	assert.Equal(t, numRequests, validOK, "all valid tokens should succeed")
	assert.Equal(t, numRequests, expiredUnauth, "all expired tokens should fail")
}
