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

func TestNewFileSourceHandler(t *testing.T) {
	t.Parallel()

	handler := NewFileSourceHandler()
	assert.NotNil(t, handler)
	// Cast to concrete type to access fields in tests (same package)
	concreteHandler := handler.(*fileSourceHandler)
	assert.NotNil(t, concreteHandler.validator)
}

func TestFileSourceHandler_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		source  *config.SourceConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid_file_source",
			source: &config.SourceConfig{
				Type: config.SourceTypeFile,
				File: &config.FileConfig{
					Path: "/tmp/registry.json",
				},
			},
			wantErr: false,
		},
		{
			name: "invalid_source_type",
			source: &config.SourceConfig{
				Type: config.SourceTypeGit,
				File: &config.FileConfig{
					Path: "/tmp/registry.json",
				},
			},
			wantErr: true,
			errMsg:  "invalid source type",
		},
		{
			name: "missing_file_configuration",
			source: &config.SourceConfig{
				Type: config.SourceTypeFile,
			},
			wantErr: true,
			errMsg:  "file configuration is required",
		},
		{
			name: "empty_file_path",
			source: &config.SourceConfig{
				Type: config.SourceTypeFile,
				File: &config.FileConfig{
					Path: "",
				},
			},
			wantErr: true,
			errMsg:  "file path cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			handler := NewFileSourceHandler()
			err := handler.Validate(tt.source)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestFileSourceHandler_FetchRegistry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		setupFile   func(t *testing.T) string
		config      *config.Config
		wantErr     bool
		errMsg      string
		checkResult func(t *testing.T, result *FetchResult)
	}{
		{
			name: "successful_fetch_toolhive_format",
			setupFile: func(t *testing.T) string {
				t.Helper()
				tmpDir := t.TempDir()
				filePath := filepath.Join(tmpDir, "registry.json")
				registryData := `{
					"version": "1.0.0",
					"last_updated": "2024-01-01T00:00:00Z",
					"servers": {}
				}`
				err := os.WriteFile(filePath, []byte(registryData), 0644)
				require.NoError(t, err)
				return filePath
			},
			config: &config.Config{
				Source: config.SourceConfig{
					Type:   config.SourceTypeFile,
					Format: config.SourceFormatToolHive,
				},
			},
			wantErr: false,
			checkResult: func(t *testing.T, result *FetchResult) {
				t.Helper()
				assert.NotNil(t, result)
				assert.NotNil(t, result.Registry)
				assert.NotEmpty(t, result.Hash)
				assert.Equal(t, config.SourceFormatToolHive, result.Format)
			},
		},
		{
			name: "file_not_found",
			setupFile: func(_ *testing.T) string {
				return "/nonexistent/path/registry.json"
			},
			config: &config.Config{
				Source: config.SourceConfig{
					Type:   config.SourceTypeFile,
					Format: config.SourceFormatToolHive,
				},
			},
			wantErr: true,
			errMsg:  "file not found",
		},
		{
			name: "invalid_json",
			setupFile: func(t *testing.T) string {
				t.Helper()
				tmpDir := t.TempDir()
				filePath := filepath.Join(tmpDir, "invalid.json")
				err := os.WriteFile(filePath, []byte("invalid json {"), 0644)
				require.NoError(t, err)
				return filePath
			},
			config: &config.Config{
				Source: config.SourceConfig{
					Type:   config.SourceTypeFile,
					Format: config.SourceFormatToolHive,
				},
			},
			wantErr: true,
			errMsg:  "validation failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			filePath := tt.setupFile(t)
			tt.config.Source.File = &config.FileConfig{
				Path: filePath,
			}

			handler := NewFileSourceHandler()
			result, err := handler.FetchRegistry(context.Background(), tt.config)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
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

func TestFileSourceHandler_CurrentHash(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "registry.json")
	registryData := `{
		"version": "1.0.0",
		"last_updated": "2024-01-01T00:00:00Z",
		"servers": {}
	}`
	err := os.WriteFile(filePath, []byte(registryData), 0644)
	require.NoError(t, err)

	cfg := &config.Config{
		Source: config.SourceConfig{
			Type:   config.SourceTypeFile,
			Format: config.SourceFormatToolHive,
			File: &config.FileConfig{
				Path: filePath,
			},
		},
	}

	handler := NewFileSourceHandler()
	hash, err := handler.CurrentHash(context.Background(), cfg)

	require.NoError(t, err)
	assert.NotEmpty(t, hash)

	// Hash should be consistent
	hash2, err := handler.CurrentHash(context.Background(), cfg)
	require.NoError(t, err)
	assert.Equal(t, hash, hash2)
}
