package sources_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

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
		registryConfig     *config.RegistryConfig
		expectError        bool
		errorContains      string
		expectedFormat     string
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
			registryConfig: &config.RegistryConfig{
				Name:   "test-registry",
				Format: config.SourceFormatUpstream,
			},
			expectError:        false,
			expectedFormat:     config.SourceFormatUpstream,
			expectedCount:      1,
			expectedServerName: "test-server",
		},
		{
			name: "fail with unsupported format (toolhive format)",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusNotFound)
				}))
			},
			registryConfig: &config.RegistryConfig{
				Name:   "test-registry",
				Format: config.SourceFormatToolHive,
			},
			expectError:   true,
			errorContains: "unsupported format",
		},
		{
			name: "fail when API endpoint returns 404",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusNotFound)
				}))
			},
			registryConfig: &config.RegistryConfig{
				Name:   "test-registry",
				Format: config.SourceFormatUpstream,
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
			registryConfig: &config.RegistryConfig{
				Name:   "test-registry",
				Format: config.SourceFormatUpstream,
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
			assert.Equal(t, tt.expectedFormat, result.Format)

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

	registryConfig := &config.RegistryConfig{
		Name:   "test-registry",
		Format: config.SourceFormatUpstream,
		API:    nil,
	}

	_, err := handler.FetchRegistry(ctx, registryConfig)

	require.Error(t, err)
}
