package sources

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/httpclient"
)

// testToolhiveRegistryData is a test fixture for ToolHive format registry data
const testToolhiveRegistryData = `{
	"version": "1.0.0",
	"last_updated": "2024-01-01T00:00:00Z",
	"servers": {}
}`

// testUpstreamRegistryData is a test fixture for upstream MCP registry format
const testUpstreamRegistryData = `{
	"$schema": "https://raw.githubusercontent.com/stacklok/toolhive/main/pkg/registry/data/upstream-registry.schema.json",
	"version": "1.0.0",
	"meta": {
		"last_updated": "2025-01-15T10:30:00Z"
	},
	"data": {
		"servers": [{
			"$schema": "https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json",
			"name": "io.github.test/test-server",
			"description": "A test server for URL validation",
			"title": "test-server",
			"version": "1.0.0",
			"packages": [{
				"registryType": "oci",
				"identifier": "test/image:latest",
				"transport": {
					"type": "stdio"
				}
			}],
			"_meta": {
				"io.modelcontextprotocol.registry/publisher-provided": {
					"io.github.test": {
						"test/image:latest": {
							"tier": "Community",
							"status": "Active",
							"tools": ["test_tool"]
						}
					}
				}
			}
		}]
	}
}`

func TestNewFileRegistryHandler(t *testing.T) {
	t.Parallel()

	handler := NewFileRegistryHandler()
	assert.NotNil(t, handler)
	// Cast to concrete type to access fields in tests (same package)
	concreteHandler := handler.(*fileRegistryHandler)
	assert.NotNil(t, concreteHandler.validator)
}

func TestFileRegistryHandler_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		registryConfig *config.RegistryConfig
		expectError    bool
		errorContains  string
	}{
		{
			name: "valid file config with absolute path",
			registryConfig: &config.RegistryConfig{
				Name: "test-file",
				File: &config.FileConfig{
					Path: "/tmp/registry.json",
				},
			},
			expectError: false,
		},
		{
			name: "valid file config with relative path",
			registryConfig: &config.RegistryConfig{
				Name: "test-file",
				File: &config.FileConfig{
					Path: "./data/registry.json",
				},
			},
			expectError: false,
		},
		{
			name:           "nil registry config",
			registryConfig: nil,
			expectError:    true,
			errorContains:  "registry configuration cannot be nil",
		},
		{
			name: "nil file config",
			registryConfig: &config.RegistryConfig{
				Name: "test-file",
				File: nil,
			},
			expectError:   true,
			errorContains: "file configuration is required",
		},
		{
			name: "empty file path and url",
			registryConfig: &config.RegistryConfig{
				Name: "test-file",
				File: &config.FileConfig{
					Path: "",
				},
			},
			expectError:   true,
			errorContains: "file path or url cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			handler := NewFileRegistryHandler()
			err := handler.Validate(tt.registryConfig)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestFileRegistryHandler_FetchRegistry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		setupFile      func(t *testing.T) string
		registryConfig func(filePath string) *config.RegistryConfig
		expectError    bool
		errorContains  string
		checkResult    func(t *testing.T, result *FetchResult)
	}{
		{
			name: "successful fetch toolhive format",
			setupFile: func(t *testing.T) string {
				t.Helper()
				tmpDir := t.TempDir()
				filePath := filepath.Join(tmpDir, "registry.json")
				err := os.WriteFile(filePath, []byte(testToolhiveRegistryData), 0600)
				require.NoError(t, err)
				return filePath
			},
			registryConfig: func(filePath string) *config.RegistryConfig {
				return &config.RegistryConfig{
					Name:   "test-file",
					Format: config.SourceFormatToolHive,
					File: &config.FileConfig{
						Path: filePath,
					},
				}
			},
			expectError: false,
			checkResult: func(t *testing.T, result *FetchResult) {
				t.Helper()
				assert.NotNil(t, result)
				assert.NotNil(t, result.Registry)
				assert.NotEmpty(t, result.Hash)
				assert.Equal(t, config.SourceFormatToolHive, result.Format)
			},
		},
		{
			name: "successful fetch with empty servers",
			setupFile: func(t *testing.T) string {
				t.Helper()
				tmpDir := t.TempDir()
				filePath := filepath.Join(tmpDir, "registry.json")
				err := os.WriteFile(filePath, []byte(testToolhiveRegistryData), 0600)
				require.NoError(t, err)
				return filePath
			},
			registryConfig: func(filePath string) *config.RegistryConfig {
				return &config.RegistryConfig{
					Name:   "test-file",
					Format: config.SourceFormatToolHive,
					File: &config.FileConfig{
						Path: filePath,
					},
				}
			},
			expectError: false,
			checkResult: func(t *testing.T, result *FetchResult) {
				t.Helper()
				assert.NotNil(t, result)
				assert.NotNil(t, result.Registry)
				assert.NotEmpty(t, result.Hash)
				assert.Equal(t, config.SourceFormatToolHive, result.Format)
			},
		},
		{
			name: "file not found",
			setupFile: func(_ *testing.T) string {
				return "/nonexistent/path/registry.json"
			},
			registryConfig: func(filePath string) *config.RegistryConfig {
				return &config.RegistryConfig{
					Name:   "test-file",
					Format: config.SourceFormatToolHive,
					File: &config.FileConfig{
						Path: filePath,
					},
				}
			},
			expectError:   true,
			errorContains: "file not found",
		},
		{
			name: "invalid json",
			setupFile: func(t *testing.T) string {
				t.Helper()
				tmpDir := t.TempDir()
				filePath := filepath.Join(tmpDir, "invalid.json")
				err := os.WriteFile(filePath, []byte("invalid json {"), 0600)
				require.NoError(t, err)
				return filePath
			},
			registryConfig: func(filePath string) *config.RegistryConfig {
				return &config.RegistryConfig{
					Name:   "test-file",
					Format: config.SourceFormatToolHive,
					File: &config.FileConfig{
						Path: filePath,
					},
				}
			},
			expectError:   true,
			errorContains: "validation failed",
		},
		{
			name: "validation error - nil config",
			setupFile: func(_ *testing.T) string {
				return "/tmp/registry.json"
			},
			registryConfig: func(_ string) *config.RegistryConfig {
				return &config.RegistryConfig{
					Name: "test-file",
					File: &config.FileConfig{
						Path: "", // Invalid - empty path
					},
				}
			},
			expectError:   true,
			errorContains: "registry validation failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			filePath := tt.setupFile(t)
			regCfg := tt.registryConfig(filePath)

			handler := NewFileRegistryHandler()
			result, err := handler.FetchRegistry(context.Background(), regCfg)

			if tt.expectError {
				require.Error(t, err)
				assert.Nil(t, result)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				require.NoError(t, err)
				if tt.checkResult != nil {
					tt.checkResult(t, result)
				}
			}
		})
	}
}

func TestFileRegistryHandler_CurrentHash(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "registry.json")
	err := os.WriteFile(filePath, []byte(testToolhiveRegistryData), 0600)
	require.NoError(t, err)

	regCfg := &config.RegistryConfig{
		Name:   "test-file",
		Format: config.SourceFormatToolHive,
		File: &config.FileConfig{
			Path: filePath,
		},
	}

	handler := NewFileRegistryHandler()
	hash, err := handler.CurrentHash(context.Background(), regCfg)

	require.NoError(t, err)
	assert.NotEmpty(t, hash)

	// Hash should be consistent
	hash2, err := handler.CurrentHash(context.Background(), regCfg)
	require.NoError(t, err)
	assert.Equal(t, hash, hash2, "Hash should be deterministic for same file content")
}

func TestFileRegistryHandler_CurrentHash_FileNotFound(t *testing.T) {
	t.Parallel()

	regCfg := &config.RegistryConfig{
		Name:   "test-file",
		Format: config.SourceFormatToolHive,
		File: &config.FileConfig{
			Path: "/nonexistent/path/registry.json",
		},
	}

	handler := NewFileRegistryHandler()
	hash, err := handler.CurrentHash(context.Background(), regCfg)

	require.Error(t, err)
	assert.Empty(t, hash)
	assert.Contains(t, err.Error(), "file not found")
}

func TestFileRegistryHandler_CurrentHash_ValidationFailure(t *testing.T) {
	t.Parallel()

	regCfg := &config.RegistryConfig{
		Name:   "test-file",
		Format: config.SourceFormatToolHive,
		File: &config.FileConfig{
			Path: "", // Invalid - empty path
		},
	}

	handler := NewFileRegistryHandler()
	hash, err := handler.CurrentHash(context.Background(), regCfg)

	require.Error(t, err)
	assert.Empty(t, hash)
	assert.Contains(t, err.Error(), "registry validation failed")
}

func TestFileRegistryHandler_Validate_URL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		registryConfig *config.RegistryConfig
		expectError    bool
		errorContains  string
	}{
		{
			name: "valid url config",
			registryConfig: &config.RegistryConfig{
				Name: "test-url",
				File: &config.FileConfig{
					URL: "https://example.com/registry.json",
				},
			},
			expectError: false,
		},
		{
			name: "valid url config with timeout",
			registryConfig: &config.RegistryConfig{
				Name: "test-url",
				File: &config.FileConfig{
					URL:     "https://example.com/registry.json",
					Timeout: "30s",
				},
			},
			expectError: false,
		},
		{
			name: "both path and url specified",
			registryConfig: &config.RegistryConfig{
				Name: "test-file",
				File: &config.FileConfig{
					Path: "/tmp/registry.json",
					URL:  "https://example.com/registry.json",
				},
			},
			expectError:   true,
			errorContains: "mutually exclusive",
		},
		{
			name: "neither path nor url specified",
			registryConfig: &config.RegistryConfig{
				Name: "test-file",
				File: &config.FileConfig{},
			},
			expectError:   true,
			errorContains: "path or url cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			handler := NewFileRegistryHandler()
			err := handler.Validate(tt.registryConfig)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestFileRegistryHandler_FetchRegistry_URL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		format        string
		serverHandler func(w http.ResponseWriter, r *http.Request)
		expectError   bool
		errorContains string
		checkResult   func(t *testing.T, result *FetchResult)
	}{
		{
			name:   "successful fetch from URL - toolhive format",
			format: config.SourceFormatToolHive,
			serverHandler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(testToolhiveRegistryData))
			},
			expectError: false,
			checkResult: func(t *testing.T, result *FetchResult) {
				t.Helper()
				assert.NotNil(t, result)
				assert.NotNil(t, result.Registry)
				assert.NotEmpty(t, result.Hash)
				assert.Equal(t, config.SourceFormatToolHive, result.Format)
			},
		},
		{
			name:   "successful fetch from URL - upstream format",
			format: config.SourceFormatUpstream,
			serverHandler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(testUpstreamRegistryData))
			},
			expectError: false,
			checkResult: func(t *testing.T, result *FetchResult) {
				t.Helper()
				assert.NotNil(t, result)
				assert.NotNil(t, result.Registry)
				assert.NotEmpty(t, result.Hash)
				assert.Equal(t, config.SourceFormatUpstream, result.Format)
			},
		},
		{
			name:   "HTTP 404 error",
			format: config.SourceFormatToolHive,
			serverHandler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte("Not Found"))
			},
			expectError:   true,
			errorContains: "failed to fetch URL",
		},
		{
			name:   "HTTP 500 error",
			format: config.SourceFormatToolHive,
			serverHandler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte("Internal Server Error"))
			},
			expectError:   true,
			errorContains: "failed to fetch URL",
		},
		{
			name:   "invalid JSON response",
			format: config.SourceFormatToolHive,
			serverHandler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("invalid json {"))
			},
			expectError:   true,
			errorContains: "validation failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create test server
			server := httptest.NewServer(http.HandlerFunc(tt.serverHandler))
			defer server.Close()

			// Create handler with custom HTTP client
			client := httpclient.NewDefaultClient(DefaultURLTimeout)
			handler := NewFileRegistryHandlerWithClient(client)

			regCfg := &config.RegistryConfig{
				Name:   "test-url",
				Format: tt.format,
				File: &config.FileConfig{
					URL: server.URL,
				},
			}

			result, err := handler.FetchRegistry(context.Background(), regCfg)

			if tt.expectError {
				require.Error(t, err)
				assert.Nil(t, result)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				require.NoError(t, err)
				if tt.checkResult != nil {
					tt.checkResult(t, result)
				}
			}
		})
	}
}

func TestFileRegistryHandler_CurrentHash_URL(t *testing.T) {
	t.Parallel()

	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(testToolhiveRegistryData))
	}))
	defer server.Close()

	// Create handler with custom HTTP client
	client := httpclient.NewDefaultClient(DefaultURLTimeout)
	handler := NewFileRegistryHandlerWithClient(client)

	regCfg := &config.RegistryConfig{
		Name:   "test-url",
		Format: config.SourceFormatToolHive,
		File: &config.FileConfig{
			URL: server.URL,
		},
	}

	hash, err := handler.CurrentHash(context.Background(), regCfg)

	require.NoError(t, err)
	assert.NotEmpty(t, hash)

	// Hash should be consistent for same content
	hash2, err := handler.CurrentHash(context.Background(), regCfg)
	require.NoError(t, err)
	assert.Equal(t, hash, hash2, "Hash should be deterministic for same content")
}

func TestFileRegistryHandler_FetchRegistry_URL_WithTimeout(t *testing.T) {
	t.Parallel()

	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(testToolhiveRegistryData))
	}))
	defer server.Close()

	// Create handler
	client := httpclient.NewDefaultClient(DefaultURLTimeout)
	handler := NewFileRegistryHandlerWithClient(client)

	regCfg := &config.RegistryConfig{
		Name:   "test-url",
		Format: config.SourceFormatToolHive,
		File: &config.FileConfig{
			URL:     server.URL,
			Timeout: "10s", // Custom timeout
		},
	}

	result, err := handler.FetchRegistry(context.Background(), regCfg)

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.NotNil(t, result.Registry)
	assert.NotEmpty(t, result.Hash)
}

func TestNewFileRegistryHandlerWithClient(t *testing.T) {
	t.Parallel()

	client := httpclient.NewDefaultClient(DefaultURLTimeout)
	handler := NewFileRegistryHandlerWithClient(client)

	assert.NotNil(t, handler)
	concreteHandler := handler.(*fileRegistryHandler)
	assert.NotNil(t, concreteHandler.validator)
	assert.NotNil(t, concreteHandler.httpClient)
}
