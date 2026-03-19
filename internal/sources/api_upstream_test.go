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
	upstreamOpenapiPath = "/openapi.yaml"
	serversAPIPath      = "/v0.1/servers"
)

func TestUpstreamAPIHandler_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		setupServer   func() *httptest.Server
		expectError   bool
		errorContains string
	}{
		{
			name: "valid upstream MCP registry API",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == upstreamOpenapiPath {
						w.Header().Set("Content-Type", "application/x-yaml")
						w.WriteHeader(http.StatusOK)
						_, _ = w.Write([]byte(`
openapi: 3.1.0
info:
  title: Official MCP Registry
  description: |
    A community driven registry service for Model Context Protocol (MCP) servers.

    [GitHub repository](https://github.com/modelcontextprotocol/registry) | [Documentation](https://github.com/modelcontextprotocol/registry/tree/main/docs)
  version: 1.0.0
paths:
  /v0/servers:
    get:
      summary: List servers
`))
					} else {
						w.WriteHeader(http.StatusNotFound)
					}
				}))
			},
			expectError: false,
		},
		{
			name: "missing /openapi.yaml endpoint",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusNotFound)
				}))
			},
			expectError:   true,
			errorContains: "failed to fetch /openapi.yaml",
		},
		{
			name: "invalid YAML in /openapi.yaml",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == upstreamOpenapiPath {
						w.Header().Set("Content-Type", "application/x-yaml")
						w.WriteHeader(http.StatusOK)
						_, _ = w.Write([]byte(`{invalid: yaml: [unclosed`))
					}
				}))
			},
			expectError:   true,
			errorContains: "failed to parse /openapi.yaml",
		},
		{
			name: "missing info section in OpenAPI spec",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == upstreamOpenapiPath {
						w.Header().Set("Content-Type", "application/x-yaml")
						w.WriteHeader(http.StatusOK)
						_, _ = w.Write([]byte(`
openapi: 3.1.0
paths:
  /v0/servers:
    get:
      summary: List servers
`))
					}
				}))
			},
			expectError:   true,
			errorContains: "missing 'info' section",
		},
		{
			name: "missing version field in info section",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == upstreamOpenapiPath {
						w.Header().Set("Content-Type", "application/x-yaml")
						w.WriteHeader(http.StatusOK)
						_, _ = w.Write([]byte(`
openapi: 3.1.0
info:
  title: Some Registry
  description: A registry without version
`))
					}
				}))
			},
			expectError:   true,
			errorContains: "missing 'version' field",
		},
		{
			name: "wrong version (not 1.0.0)",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == upstreamOpenapiPath {
						w.Header().Set("Content-Type", "application/x-yaml")
						w.WriteHeader(http.StatusOK)
						_, _ = w.Write([]byte(`
openapi: 3.1.0
info:
  title: Some Registry
  description: Contains GitHub URL https://github.com/modelcontextprotocol/registry
  version: 2.0.0
`))
					}
				}))
			},
			expectError:   true,
			errorContains: "version is 2.0.0, expected 1.0.0",
		},
		{
			name: "missing description field in info section",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == upstreamOpenapiPath {
						w.Header().Set("Content-Type", "application/x-yaml")
						w.WriteHeader(http.StatusOK)
						_, _ = w.Write([]byte(`
openapi: 3.1.0
info:
  title: Some Registry
  version: 1.0.0
`))
					}
				}))
			},
			expectError:   true,
			errorContains: "missing 'description' field",
		},
		{
			name: "description without expected GitHub URL",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == upstreamOpenapiPath {
						w.Header().Set("Content-Type", "application/x-yaml")
						w.WriteHeader(http.StatusOK)
						_, _ = w.Write([]byte(`
openapi: 3.1.0
info:
  title: Some Registry
  description: A registry without the expected GitHub URL
  version: 1.0.0
`))
					}
				}))
			},
			expectError:   true,
			errorContains: "does not contain expected GitHub URL",
		},
		{
			name: "version as number instead of string (YAML parses 1.0 as float)",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == upstreamOpenapiPath {
						w.Header().Set("Content-Type", "application/x-yaml")
						w.WriteHeader(http.StatusOK)
						_, _ = w.Write([]byte(`
openapi: 3.1.0
info:
  title: Some Registry
  description: Contains https://github.com/modelcontextprotocol/registry
  version: 1.0
`))
					}
				}))
			},
			expectError:   true,
			errorContains: "missing 'version' field",
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

			err := handler.Validate(ctx, mockServer.URL)

			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestUpstreamAPIHandler_FetchRegistry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		setupServer        func() *httptest.Server
		expectError        bool
		errorContains      string
		expectedCount      int
		expectedFormat     string
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
			expectedFormat:     config.SourceFormatUpstream,
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

			registryConfig := &config.RegistryConfig{
				Name:   "test-registry",
				Format: config.SourceFormatUpstream,
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
			assert.Equal(t, tt.expectedFormat, result.Format)
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

	registryConfig := &config.RegistryConfig{
		Name:   "test-registry",
		Format: config.SourceFormatUpstream,
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

func TestUpstreamAPIHandler_CurrentHash(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		setupServer   func() *httptest.Server
		expectError   bool
		errorContains string
		verifyHash    bool
	}{
		{
			name: "successful hash calculation",
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
			expectError: false,
			verifyHash:  true,
		},
		{
			name: "error propagation on HTTP failure",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusInternalServerError)
				}))
			},
			expectError: true,
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

			registryConfig := &config.RegistryConfig{
				Name:   "test-registry",
				Format: config.SourceFormatUpstream,
				API: &config.APIConfig{
					Endpoint: mockServer.URL,
				},
			}

			hash, err := handler.CurrentHash(ctx, registryConfig)

			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
				return
			}

			require.NoError(t, err)
			if tt.verifyHash {
				assert.NotEmpty(t, hash)
				// Should be a valid SHA256 hex string (64 characters)
				assert.Len(t, hash, 64)
			}
		})
	}
}

func TestUpstreamAPIHandler_HashConsistency(t *testing.T) {
	t.Parallel()

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	defer mockServer.Close()

	httpClient := httpclient.NewDefaultClient(0)
	handler := NewUpstreamAPIHandler(httpClient)
	ctx := context.Background()

	registryConfig := &config.RegistryConfig{
		Name:   "test-registry",
		Format: config.SourceFormatUpstream,
		API: &config.APIConfig{
			Endpoint: mockServer.URL,
		},
	}

	// Get hash via CurrentHash
	hash1, err := handler.CurrentHash(ctx, registryConfig)
	require.NoError(t, err)

	// Get hash via FetchRegistry
	result, err := handler.FetchRegistry(ctx, registryConfig)
	require.NoError(t, err)

	// Both should return the same hash for the same data
	assert.Equal(t, hash1, result.Hash, "CurrentHash and FetchRegistry should return the same hash")
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

	registryConfig := &config.RegistryConfig{
		Name:   "test-registry",
		Format: config.SourceFormatUpstream,
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

	registryConfig := &config.RegistryConfig{
		Name:   "test-registry",
		Format: config.SourceFormatUpstream,
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

			registryConfig := &config.RegistryConfig{
				Name:   "test-registry",
				Format: config.SourceFormatUpstream,
				API: &config.APIConfig{
					Endpoint: mockServer.URL,
				},
			}

			_, err := handler.FetchRegistry(ctx, registryConfig)

			require.Error(t, err)
		})
	}
}
