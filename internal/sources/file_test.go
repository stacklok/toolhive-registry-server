package sources

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/config"
)

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
			name: "empty file path",
			registryConfig: &config.RegistryConfig{
				Name: "test-file",
				File: &config.FileConfig{
					Path: "",
				},
			},
			expectError:   true,
			errorContains: "file path cannot be empty",
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
				registryData := `{
					"version": "1.0.0",
					"last_updated": "2024-01-01T00:00:00Z",
					"servers": {}
				}`
				err := os.WriteFile(filePath, []byte(registryData), 0600)
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
				registryData := `{
					"version": "1.0.0",
					"last_updated": "2024-01-01T00:00:00Z",
					"servers": {}
				}`
				err := os.WriteFile(filePath, []byte(registryData), 0600)
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
	registryData := `{
		"version": "1.0.0",
		"last_updated": "2024-01-01T00:00:00Z",
		"servers": {}
	}`
	err := os.WriteFile(filePath, []byte(registryData), 0600)
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
