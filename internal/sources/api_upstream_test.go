package sources

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/httpclient"
)

const (
	serversAPIPath = "/v0.1/servers"
)

func TestUpstreamAPIHandler_Validate(t *testing.T) {
	t.Parallel()

	// serveServers returns a handler that answers /v0.1/servers with body and
	// 404s everything else, recording the query of the probe request.
	serveServers := func(body string, gotQuery *string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != serversAPIPath {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			if gotQuery != nil {
				*gotQuery = r.URL.RawQuery
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		}
	}

	tests := []struct {
		name          string
		handler       http.HandlerFunc
		expectError   bool
		errorContains string
	}{
		{
			// No /openapi.yaml is served: non-official implementations (including
			// ToolHive Registry Server) must be accepted on API behavior alone.
			name: "valid v0.1 server list",
			handler: serveServers(`{
				"servers": [{"server": {"name": "io.example/a", "description": "d", "version": "1.0.0"}, "_meta": {}}],
				"metadata": {"nextCursor": "abc", "count": 1}
			}`, nil),
		},
		{
			name:    "empty server list",
			handler: serveServers(`{"servers": [], "metadata": {"count": 0}}`, nil),
		},
		{
			name: "endpoint returns 404",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			expectError:   true,
			errorContains: "failed to fetch /v0.1/servers",
		},
		{
			name: "endpoint returns 500",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			expectError:   true,
			errorContains: "HTTP 500",
		},
		{
			name:          "non-JSON response",
			handler:       serveServers(`<html>not a registry</html>`, nil),
			expectError:   true,
			errorContains: "did not return a JSON object",
		},
		{
			name:          "JSON array instead of object",
			handler:       serveServers(`[]`, nil),
			expectError:   true,
			errorContains: "did not return a JSON object",
		},
		{
			name:          "object without servers field",
			handler:       serveServers(`{"items": [], "metadata": {"count": 0}}`, nil),
			expectError:   true,
			errorContains: "missing a 'servers' array",
		},
		{
			name:          "servers is null",
			handler:       serveServers(`{"servers": null}`, nil),
			expectError:   true,
			errorContains: "missing a 'servers' array",
		},
		{
			name:          "servers is not an array",
			handler:       serveServers(`{"servers": {"name": "x"}}`, nil),
			expectError:   true,
			errorContains: "missing a 'servers' array",
		},
		{
			name:          "server entries have the wrong shape",
			handler:       serveServers(`{"servers": ["io.example/a"]}`, nil),
			expectError:   true,
			errorContains: "not a valid server list",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockServer := httptest.NewServer(tt.handler)
			defer mockServer.Close()

			handler := NewUpstreamAPIHandler(httpclient.NewDefaultClient(0))
			err := handler.Validate(context.Background(), mockServer.URL)

			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorContains)
			} else {
				require.NoError(t, err)
			}
		})
	}

	t.Run("probes a single-item page", func(t *testing.T) {
		t.Parallel()

		var gotQuery string
		mockServer := httptest.NewServer(serveServers(`{"servers": []}`, &gotQuery))
		defer mockServer.Close()

		handler := NewUpstreamAPIHandler(httpclient.NewDefaultClient(0))
		require.NoError(t, handler.Validate(context.Background(), mockServer.URL))
		assert.Equal(t, "limit=1", gotQuery)
	})
}

func TestUpstreamAPIHandler_FetchRegistry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		setupServer        func() *httptest.Server
		expectError        bool
		errorContains      string
		expectedCount      int
		expectedServerName string
		verifyHash         bool
	}{
		{
			name: "successful fetch with single page",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == serversAPIPath {
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusOK)
						_, _ = w.Write([]byte(`{
							"servers": [
								{
									"server": {
										"name": "test-server",
										"description": "A test server"
									},
									"_meta": {}
								}
							],
							"metadata": {
								"nextCursor": "",
								"count": 1
							}
						}`))
					} else {
						w.WriteHeader(http.StatusNotFound)
					}
				}))
			},
			expectError:        false,
			expectedCount:      1,
			expectedServerName: "test-server",
			verifyHash:         true,
		},
		{
			name: "HTTP error during fetch (500)",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusInternalServerError)
				}))
			},
			expectError:   true,
			errorContains: "failed to fetch servers",
		},
		{
			name: "invalid JSON response",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == serversAPIPath {
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusOK)
						_, _ = w.Write([]byte(`{invalid json`))
					}
				}))
			},
			expectError:   true,
			errorContains: "failed to parse response",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockServer := tt.setupServer()
			defer mockServer.Close()

			httpClient := httpclient.NewDefaultClient(0)
			handler := NewUpstreamAPIHandler(httpClient)
			ctx := context.Background()

			registryConfig := &config.SourceConfig{
				Name: "test-registry",
				API: &config.APIConfig{
					Endpoint: mockServer.URL,
				},
			}

			result, err := handler.FetchRegistry(ctx, registryConfig)

			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorContains)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, result)
			assert.Equal(t, tt.expectedCount, result.ServerCount)
			require.NotNil(t, result.Registry)
			require.Len(t, result.Registry.Data.Servers, tt.expectedCount)

			if tt.expectedServerName != "" {
				assert.Equal(t, tt.expectedServerName, result.Registry.Data.Servers[0].Name)
			}

			if tt.verifyHash {
				assert.NotEmpty(t, result.Hash)
			}
		})
	}
}

func TestUpstreamAPIHandler_FetchRegistry_Pagination(t *testing.T) {
	t.Parallel()

	var requestCount int
	var receivedCursors []string

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == serversAPIPath {
			requestCount++
			cursor := r.URL.Query().Get("cursor")
			receivedCursors = append(receivedCursors, cursor)

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			switch cursor {
			case "":
				_, _ = w.Write([]byte(`{
					"servers": [
						{
							"server": {
								"name": "server-1",
								"description": "First server"
							},
							"_meta": {}
						}
					],
					"metadata": {
						"nextCursor": "page2",
						"count": 1
					}
				}`))
			case "page2":
				_, _ = w.Write([]byte(`{
					"servers": [
						{
							"server": {
								"name": "server-2",
								"description": "Second server"
							},
							"_meta": {}
						}
					],
					"metadata": {
						"nextCursor": "",
						"count": 1
					}
				}`))
			}
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockServer.Close()

	httpClient := httpclient.NewDefaultClient(0)
	handler := NewUpstreamAPIHandler(httpClient)
	ctx := context.Background()

	registryConfig := &config.SourceConfig{
		Name: "test-registry",
		API: &config.APIConfig{
			Endpoint: mockServer.URL,
		},
	}

	result, err := handler.FetchRegistry(ctx, registryConfig)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 2, result.ServerCount)
	require.Len(t, result.Registry.Data.Servers, 2)
	assert.Equal(t, "server-1", result.Registry.Data.Servers[0].Name)
	assert.Equal(t, "server-2", result.Registry.Data.Servers[1].Name)

	// Verify pagination mechanics
	assert.Equal(t, 2, requestCount, "should make exactly 2 requests")
	assert.Equal(t, []string{"", "page2"}, receivedCursors, "should receive correct cursor sequence")
}

func TestUpstreamAPIHandler_EmptyServers(t *testing.T) {
	t.Parallel()

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == serversAPIPath {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"servers": [],
				"metadata": {
					"nextCursor": "",
					"count": 0
				}
			}`))
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockServer.Close()

	httpClient := httpclient.NewDefaultClient(0)
	handler := NewUpstreamAPIHandler(httpClient)
	ctx := context.Background()

	registryConfig := &config.SourceConfig{
		Name: "test-registry",
		API: &config.APIConfig{
			Endpoint: mockServer.URL,
		},
	}

	result, err := handler.FetchRegistry(ctx, registryConfig)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 0, result.ServerCount)
	assert.Empty(t, result.Registry.Data.Servers)
}

func TestUpstreamAPIHandler_MultiplePages(t *testing.T) {
	t.Parallel()

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == serversAPIPath {
			cursor := r.URL.Query().Get("cursor")

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			switch cursor {
			case "":
				_, _ = w.Write([]byte(`{
					"servers": [
						{"server": {"name": "server-1", "description": "First"}, "_meta": {}},
						{"server": {"name": "server-2", "description": "Second"}, "_meta": {}}
					],
					"metadata": {"nextCursor": "page2", "count": 2}
				}`))
			case "page2":
				_, _ = w.Write([]byte(`{
					"servers": [
						{"server": {"name": "server-3", "description": "Third"}, "_meta": {}},
						{"server": {"name": "server-4", "description": "Fourth"}, "_meta": {}}
					],
					"metadata": {"nextCursor": "page3", "count": 2}
				}`))
			case "page3":
				_, _ = w.Write([]byte(`{
					"servers": [
						{"server": {"name": "server-5", "description": "Fifth"}, "_meta": {}}
					],
					"metadata": {"nextCursor": "", "count": 1}
				}`))
			}
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockServer.Close()

	httpClient := httpclient.NewDefaultClient(0)
	handler := NewUpstreamAPIHandler(httpClient)
	ctx := context.Background()

	registryConfig := &config.SourceConfig{
		Name: "test-registry",
		API: &config.APIConfig{
			Endpoint: mockServer.URL,
		},
	}

	result, err := handler.FetchRegistry(ctx, registryConfig)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 5, result.ServerCount)
	require.Len(t, result.Registry.Data.Servers, 5)

	expectedNames := []string{"server-1", "server-2", "server-3", "server-4", "server-5"}
	for i, name := range expectedNames {
		assert.Equal(t, name, result.Registry.Data.Servers[i].Name)
	}
}

func TestNewUpstreamAPIHandler(t *testing.T) {
	t.Parallel()

	httpClient := httpclient.NewDefaultClient(0)
	handler := NewUpstreamAPIHandler(httpClient)

	require.NotNil(t, handler, "NewUpstreamAPIHandler should return a non-nil handler")
}

func TestUpstreamAPIHandler_HTTPErrorCodes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		statusCode int
	}{
		{"400 Bad Request", http.StatusBadRequest},
		{"401 Unauthorized", http.StatusUnauthorized},
		{"403 Forbidden", http.StatusForbidden},
		{"404 Not Found", http.StatusNotFound},
		{"500 Internal Server Error", http.StatusInternalServerError},
		{"502 Bad Gateway", http.StatusBadGateway},
		{"503 Service Unavailable", http.StatusServiceUnavailable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.statusCode)
			}))
			defer mockServer.Close()

			httpClient := httpclient.NewDefaultClient(0)
			handler := NewUpstreamAPIHandler(httpClient)
			ctx := context.Background()

			registryConfig := &config.SourceConfig{
				Name: "test-registry",
				API: &config.APIConfig{
					Endpoint: mockServer.URL,
				},
			}

			_, err := handler.FetchRegistry(ctx, registryConfig)

			require.Error(t, err)
		})
	}
}
