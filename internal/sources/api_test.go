package sources_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/sources"
)

const (
	openapiPath = "/openapi.yaml"
)

func TestAPIRegistryHandler_FetchRegistry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		setupServer        func() *httptest.Server
		registryConfig     *config.SourceConfig
		expectError        bool
		errorContains      string
		expectedCount      int
		expectedServerName string
	}{
		{
			name: "successful fetch with upstream format",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case openapiPath:
						w.Header().Set("Content-Type", "application/x-yaml")
						w.WriteHeader(http.StatusOK)
						_, _ = w.Write([]byte(`
info:
  title: Official MCP Registry
  description: |
    A community driven registry service for Model Context Protocol (MCP) servers.

    [GitHub repository](https://github.com/modelcontextprotocol/registry)
  version: 1.0.0
openapi: 3.1.0
`))
					case "/v0.1/servers":
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusOK)
						_, _ = w.Write([]byte(`{
							"servers": [
								{
									"server": {
										"name": "test-server",
										"description": "A test MCP server"
									},
									"_meta": {}
								}
							],
							"metadata": {
								"nextCursor": "",
								"count": 1
							}
						}`))
					default:
						w.WriteHeader(http.StatusNotFound)
					}
				}))
			},
			registryConfig: &config.SourceConfig{
				Name: "test-registry",
			},
			expectError:        false,
			expectedCount:      1,
			expectedServerName: "test-server",
		},
		{
			name: "fail when API endpoint returns 404",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusNotFound)
				}))
			},
			registryConfig: &config.SourceConfig{
				Name: "test-registry",
			},
			expectError:   true,
			errorContains: "validation failed",
		},
		{
			name: "fail when API endpoint returns 500",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusInternalServerError)
				}))
			},
			registryConfig: &config.SourceConfig{
				Name: "test-registry",
			},
			expectError:   true,
			errorContains: "validation failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockServer := tt.setupServer()
			defer mockServer.Close()

			handler := sources.NewAPIRegistryHandler()
			ctx := context.Background()

			// Set the endpoint from the mock server
			tt.registryConfig.API = &config.APIConfig{
				Endpoint: mockServer.URL,
			}

			result, err := handler.FetchRegistry(ctx, tt.registryConfig)

			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorContains)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, result)
			assert.Equal(t, tt.expectedCount, result.ServerCount)

			if tt.expectedServerName != "" {
				require.NotNil(t, result.Registry)
				require.NotEmpty(t, result.Registry.Data.Servers)
				assert.Equal(t, tt.expectedServerName, result.Registry.Data.Servers[0].Name)
			}
		})
	}
}

func TestNewAPIRegistryHandler(t *testing.T) {
	t.Parallel()

	handler := sources.NewAPIRegistryHandler()

	require.NotNil(t, handler, "NewAPIRegistryHandler should return a non-nil handler")
}

func TestAPIRegistryHandler_MultipleServers(t *testing.T) {
	t.Parallel()

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case openapiPath:
			w.Header().Set("Content-Type", "application/x-yaml")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`
info:
  title: Official MCP Registry
  description: |
    A community driven registry service for Model Context Protocol (MCP) servers.

    [GitHub repository](https://github.com/modelcontextprotocol/registry)
  version: 1.0.0
openapi: 3.1.0
`))
		case "/v0.1/servers":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"servers": [
					{
						"server": {
							"name": "server-1",
							"description": "First server"
						},
						"_meta": {}
					},
					{
						"server": {
							"name": "server-2",
							"description": "Second server"
						},
						"_meta": {}
					},
					{
						"server": {
							"name": "server-3",
							"description": "Third server"
						},
						"_meta": {}
					}
				],
				"metadata": {
					"nextCursor": "",
					"count": 3
				}
			}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockServer.Close()

	handler := sources.NewAPIRegistryHandler()
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
	assert.Equal(t, 3, result.ServerCount)
	require.Len(t, result.Registry.Data.Servers, 3)
	assert.Equal(t, "server-1", result.Registry.Data.Servers[0].Name)
	assert.Equal(t, "server-2", result.Registry.Data.Servers[1].Name)
	assert.Equal(t, "server-3", result.Registry.Data.Servers[2].Name)
}

func TestAPIRegistryHandler_NilConfig(t *testing.T) {
	t.Parallel()

	handler := sources.NewAPIRegistryHandler()
	ctx := context.Background()

	_, err := handler.FetchRegistry(ctx, nil)

	require.Error(t, err)
}

func TestAPIRegistryHandler_NilAPIConfig(t *testing.T) {
	t.Parallel()

	handler := sources.NewAPIRegistryHandler()
	ctx := context.Background()

	registryConfig := &config.SourceConfig{
		Name: "test-registry",
		API:  nil,
	}

	_, err := handler.FetchRegistry(ctx, registryConfig)

	require.Error(t, err)
}

// newUpstreamMockServer returns an httptest.Server that speaks the upstream MCP
// Registry format with a single server, optionally delaying every response by
// delay to exercise client timeout behavior.
func newUpstreamMockServer(delay time.Duration) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if delay > 0 {
			time.Sleep(delay)
		}
		switch r.URL.Path {
		case openapiPath:
			w.Header().Set("Content-Type", "application/x-yaml")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`
info:
  title: Official MCP Registry
  description: |
    A community driven registry service for Model Context Protocol (MCP) servers.

    [GitHub repository](https://github.com/modelcontextprotocol/registry)
  version: 1.0.0
openapi: 3.1.0
`))
		case "/v0.1/servers":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"servers": [
					{
						"server": {
							"name": "test-server",
							"description": "A test MCP server"
						},
						"_meta": {}
					}
				],
				"metadata": {"nextCursor": "", "count": 1}
			}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestAPIRegistryHandler_FetchRegistry_WithConfiguredTimeout(t *testing.T) {
	t.Parallel()

	mockServer := newUpstreamMockServer(0)
	defer mockServer.Close()

	handler := sources.NewAPIRegistryHandler()
	ctx := context.Background()

	registryConfig := &config.SourceConfig{
		Name: "test-registry",
		API: &config.APIConfig{
			Endpoint: mockServer.URL,
			Timeout:  "1m",
		},
	}

	result, err := handler.FetchRegistry(ctx, registryConfig)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 1, result.ServerCount)
}

func TestAPIRegistryHandler_FetchRegistry_InvalidTimeout(t *testing.T) {
	t.Parallel()

	handler := sources.NewAPIRegistryHandler()
	ctx := context.Background()

	registryConfig := &config.SourceConfig{
		Name: "test-registry",
		API: &config.APIConfig{
			Endpoint: "https://example.com",
			Timeout:  "not-a-duration",
		},
	}

	_, err := handler.FetchRegistry(ctx, registryConfig)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid api timeout")
}

func TestAPIRegistryHandler_FetchRegistry_TimeoutEnforced(t *testing.T) {
	t.Parallel()

	// Server delays longer than the configured per-request timeout, so the
	// request should fail rather than hang until the default timeout.
	mockServer := newUpstreamMockServer(200 * time.Millisecond)
	defer mockServer.Close()

	handler := sources.NewAPIRegistryHandler()
	ctx := context.Background()

	registryConfig := &config.SourceConfig{
		Name: "test-registry",
		API: &config.APIConfig{
			Endpoint: mockServer.URL,
			Timeout:  "50ms",
		},
	}

	_, err := handler.FetchRegistry(ctx, registryConfig)

	require.Error(t, err)
}
