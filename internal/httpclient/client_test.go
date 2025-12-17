package httpclient_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/httpclient"
)

// newTestServer creates a new test server with keep-alives disabled.
// This prevents flaky tests when running in parallel, as closing a server
// with keep-alives enabled can affect other tests sharing the HTTP transport.
func newTestServer(handler http.Handler) *httptest.Server {
	server := httptest.NewServer(handler)
	server.Config.SetKeepAlivesEnabled(false)
	return server
}

func TestNewDefaultClient(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		timeout time.Duration
	}{
		{
			name:    "create client with custom timeout",
			timeout: 5 * time.Second,
		},
		{
			name:    "create client with zero timeout uses default",
			timeout: 0,
		},
		{
			name:    "create client with large timeout",
			timeout: 10 * time.Minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := httpclient.NewDefaultClient(tt.timeout)

			require.NotNil(t, client, "client should not be nil")
		})
	}
}

func TestDefaultClient_Get_SuccessfulRequests(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		responseBody     string
		expectedResponse []byte
	}{
		{
			name:             "successful JSON response",
			responseBody:     `{"message": "success"}`,
			expectedResponse: []byte(`{"message": "success"}`),
		},
		{
			name:             "successful plain text response",
			responseBody:     "plain text content",
			expectedResponse: []byte("plain text content"),
		},
		{
			name:             "successful response with special characters",
			responseBody:     `{"data": "value with \u0000 special chars"}`,
			expectedResponse: []byte(`{"data": "value with \u0000 special chars"}`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var receivedUserAgent string
			var receivedAccept string

			mockServer := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				receivedUserAgent = r.Header.Get("User-Agent")
				receivedAccept = r.Header.Get("Accept")

				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(tt.responseBody))
			}))
			defer mockServer.Close()

			client := httpclient.NewDefaultClient(30 * time.Second)
			ctx := context.Background()

			data, err := client.Get(ctx, mockServer.URL)

			require.NoError(t, err)
			assert.Equal(t, tt.expectedResponse, data)
			assert.Equal(t, "toolhive-registry-server/1.0", receivedUserAgent, "User-Agent header should be set correctly")
			assert.Equal(t, "application/json", receivedAccept, "Accept header should be set correctly")
		})
	}
}

func TestDefaultClient_Get_HTTPErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		statusCode    int
		responseBody  string
		errorContains string
	}{
		{
			name:          "404 Not Found",
			statusCode:    http.StatusNotFound,
			responseBody:  "Not Found",
			errorContains: "HTTP 404",
		},
		{
			name:          "500 Internal Server Error",
			statusCode:    http.StatusInternalServerError,
			responseBody:  "Internal Server Error",
			errorContains: "HTTP 500",
		},
		{
			name:          "401 Unauthorized",
			statusCode:    http.StatusUnauthorized,
			responseBody:  "Unauthorized",
			errorContains: "HTTP 401",
		},
		{
			name:          "403 Forbidden",
			statusCode:    http.StatusForbidden,
			responseBody:  "Forbidden",
			errorContains: "HTTP 403",
		},
		{
			name:          "502 Bad Gateway",
			statusCode:    http.StatusBadGateway,
			responseBody:  "Bad Gateway",
			errorContains: "HTTP 502",
		},
		{
			name:          "503 Service Unavailable",
			statusCode:    http.StatusServiceUnavailable,
			responseBody:  "Service Unavailable",
			errorContains: "HTTP 503",
		},
		{
			name:          "429 Too Many Requests",
			statusCode:    http.StatusTooManyRequests,
			responseBody:  "Too Many Requests",
			errorContains: "HTTP 429",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockServer := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.responseBody))
			}))
			defer mockServer.Close()

			client := httpclient.NewDefaultClient(30 * time.Second)
			ctx := context.Background()

			_, err := client.Get(ctx, mockServer.URL)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errorContains)
		})
	}
}

func TestDefaultClient_Get_NetworkErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		url           string
		errorContains string
	}{
		{
			name:          "invalid URL scheme",
			url:           "://invalid-url",
			errorContains: "failed to create request",
		},
		{
			name:          "unreachable host",
			url:           "http://invalid-host-does-not-exist.local:9999",
			errorContains: "failed to execute request",
		},
		{
			name:          "invalid URL format",
			url:           "not-a-valid-url",
			errorContains: "failed to execute request",
		},
		{
			name:          "empty URL",
			url:           "",
			errorContains: "failed to execute request",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := httpclient.NewDefaultClient(30 * time.Second)
			ctx := context.Background()

			_, err := client.Get(ctx, tt.url)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errorContains)
		})
	}
}

func TestDefaultClient_Get_ContextCancellation(t *testing.T) {
	t.Parallel()

	t.Run("should respect context cancellation", func(t *testing.T) {
		t.Parallel()

		mockServer := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			time.Sleep(2 * time.Second)
			w.WriteHeader(http.StatusOK)
		}))
		defer mockServer.Close()

		client := httpclient.NewDefaultClient(30 * time.Second)
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		_, err := client.Get(ctx, mockServer.URL)

		require.Error(t, err)
	})

	t.Run("should respect context timeout", func(t *testing.T) {
		t.Parallel()

		mockServer := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			time.Sleep(2 * time.Second)
			w.WriteHeader(http.StatusOK)
		}))
		defer mockServer.Close()

		client := httpclient.NewDefaultClient(30 * time.Second)
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		_, err := client.Get(ctx, mockServer.URL)

		require.Error(t, err)
	})

	t.Run("should succeed with sufficient timeout", func(t *testing.T) {
		t.Parallel()

		mockServer := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("success"))
		}))
		defer mockServer.Close()

		client := httpclient.NewDefaultClient(30 * time.Second)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		data, err := client.Get(ctx, mockServer.URL)

		require.NoError(t, err)
		assert.Equal(t, []byte("success"), data)
	})
}

func TestDefaultClient_Get_ResponseBodyHandling(t *testing.T) {
	t.Parallel()

	t.Run("should handle empty response body", func(t *testing.T) {
		t.Parallel()

		mockServer := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer mockServer.Close()

		client := httpclient.NewDefaultClient(30 * time.Second)
		ctx := context.Background()

		data, err := client.Get(ctx, mockServer.URL)

		require.NoError(t, err)
		assert.Empty(t, data)
	})

	t.Run("should handle large response body (1MB)", func(t *testing.T) {
		t.Parallel()

		largeData := make([]byte, 1024*1024) // 1MB
		for i := range largeData {
			largeData[i] = 'a'
		}

		mockServer := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(largeData)
		}))
		defer mockServer.Close()

		client := httpclient.NewDefaultClient(30 * time.Second)
		ctx := context.Background()

		data, err := client.Get(ctx, mockServer.URL)

		require.NoError(t, err)
		assert.Len(t, data, 1024*1024)
	})

	t.Run("should successfully handle response at exactly 100MB", func(t *testing.T) {
		t.Parallel()

		mockServer := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			// Write exactly 100MB in 1MB chunks
			chunk := make([]byte, 1024*1024)
			for i := 0; i < 100; i++ {
				_, _ = w.Write(chunk)
			}
		}))
		defer mockServer.Close()

		client := httpclient.NewDefaultClient(30 * time.Second)
		ctx := context.Background()

		data, err := client.Get(ctx, mockServer.URL)

		require.NoError(t, err)
		assert.Len(t, data, 100*1024*1024)
	})
}

func TestDefaultClient_Get_SizeLimitExceeded(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		setupServer   func() *httptest.Server
		errorContains []string
	}{
		{
			name: "reject response exceeding 100MB via Content-Length",
			setupServer: func() *httptest.Server {
				return newTestServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					// Set Content-Length to 101MB
					w.Header().Set("Content-Length", fmt.Sprintf("%d", 101*1024*1024))
					w.WriteHeader(http.StatusOK)
				}))
			},
			errorContains: []string{"exceeds maximum allowed size", "100.00 MB"},
		},
		{
			name: "reject response exceeding 100MB by actual content",
			setupServer: func() *httptest.Server {
				return newTestServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusOK)
					// Write 101MB of data in chunks
					chunk := make([]byte, 1024*1024)
					for i := 0; i < 101; i++ {
						_, _ = w.Write(chunk)
					}
				}))
			},
			errorContains: []string{"exceeds maximum allowed size"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockServer := tt.setupServer()
			defer mockServer.Close()

			client := httpclient.NewDefaultClient(30 * time.Second)
			ctx := context.Background()

			_, err := client.Get(ctx, mockServer.URL)

			require.Error(t, err)
			for _, contains := range tt.errorContains {
				assert.Contains(t, err.Error(), contains)
			}
		})
	}
}

func TestDefaultClient_Get_Headers(t *testing.T) {
	t.Parallel()

	t.Run("should set correct default headers", func(t *testing.T) {
		t.Parallel()

		var receivedHeaders http.Header

		mockServer := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedHeaders = r.Header.Clone()
			w.WriteHeader(http.StatusOK)
		}))
		defer mockServer.Close()

		client := httpclient.NewDefaultClient(30 * time.Second)
		ctx := context.Background()

		_, err := client.Get(ctx, mockServer.URL)

		require.NoError(t, err)
		assert.Equal(t, "toolhive-registry-server/1.0", receivedHeaders.Get("User-Agent"))
		assert.Equal(t, "application/json", receivedHeaders.Get("Accept"))
	})

	t.Run("should use GET method", func(t *testing.T) {
		t.Parallel()

		var receivedMethod string

		mockServer := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedMethod = r.Method
			w.WriteHeader(http.StatusOK)
		}))
		defer mockServer.Close()

		client := httpclient.NewDefaultClient(30 * time.Second)
		ctx := context.Background()

		_, err := client.Get(ctx, mockServer.URL)

		require.NoError(t, err)
		assert.Equal(t, http.MethodGet, receivedMethod)
	})
}
