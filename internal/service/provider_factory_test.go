package service

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegistryProviderConfig_Validate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		config      *RegistryProviderConfig
		wantErr     bool
		errContains string
	}{
		{
			name: "valid file provider",
			config: &RegistryProviderConfig{
				FilePath:     "/data/registry.json",
				RegistryName: "test-registry",
			},
			wantErr: false,
		},
		{
			name: "empty file path",
			config: &RegistryProviderConfig{
				FilePath:     "",
				RegistryName: "test-registry",
			},
			wantErr:     true,
			errContains: "file path is required",
		},
		{
			name: "empty registry name",
			config: &RegistryProviderConfig{
				FilePath:     "/data/registry.json",
				RegistryName: "",
			},
			wantErr:     true,
			errContains: "registry name is required",
		},
		{
			name: "both fields empty",
			config: &RegistryProviderConfig{
				FilePath:     "",
				RegistryName: "",
			},
			wantErr:     true,
			errContains: "file path is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.config.Validate()

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestDefaultRegistryProviderFactory_CreateProvider(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test-registry.json")

	// Create a test file
	err := os.WriteFile(tmpFile, []byte(`{
		"version": "1.0",
		"last_updated": "",
		"servers": {},
		"remote_servers": {}
	}`), 0644)
	require.NoError(t, err)

	tests := []struct {
		name        string
		config      *RegistryProviderConfig
		wantErr     bool
		errContains string
		checkType   func(*testing.T, RegistryDataProvider)
	}{
		{
			name: "create file provider",
			config: &RegistryProviderConfig{
				FilePath:     tmpFile,
				RegistryName: "test-file-registry",
			},
			wantErr: false,
			checkType: func(t *testing.T, provider RegistryDataProvider) {
				t.Helper()
				assert.IsType(t, &FileRegistryDataProvider{}, provider)
				assert.Equal(t, "file:"+tmpFile, provider.GetSource())
				assert.Equal(t, "test-file-registry", provider.GetRegistryName())
			},
		},
		{
			name:        "nil config",
			config:      nil,
			wantErr:     true,
			errContains: "registry provider config cannot be nil",
		},
		{
			name: "missing file path",
			config: &RegistryProviderConfig{
				FilePath:     "",
				RegistryName: "test-registry",
			},
			wantErr:     true,
			errContains: "file path is required",
		},
		{
			name: "missing registry name",
			config: &RegistryProviderConfig{
				FilePath:     tmpFile,
				RegistryName: "",
			},
			wantErr:     true,
			errContains: "registry name is required",
		},
	}

	factory := NewRegistryProviderFactory()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			provider, err := factory.CreateProvider(tt.config)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				assert.Nil(t, provider)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, provider)
				if tt.checkType != nil {
					tt.checkType(t, provider)
				}
			}
		})
	}
}

func TestNewRegistryProviderFactory(t *testing.T) {
	t.Parallel()
	factory := NewRegistryProviderFactory()
	assert.NotNil(t, factory)
	assert.IsType(t, &DefaultRegistryProviderFactory{}, factory)
}
