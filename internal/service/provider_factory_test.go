package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	sourcesmocks "github.com/stacklok/toolhive-registry-server/internal/sources/mocks"
)

func TestNewRegistryProviderFactory(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockStorageManager := sourcesmocks.NewMockStorageManager(ctrl)
	factory := NewRegistryProviderFactory(mockStorageManager)

	require.NotNil(t, factory)

	// Verify that the factory has the storage manager injected
	concreteFactory, ok := factory.(*defaultRegistryProviderFactory)
	require.True(t, ok)
	assert.NotNil(t, concreteFactory.storageManager)
}

func TestRegistryProviderFactory_CreateProvider(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		config        *config.Config
		expectError   bool
		errorContains string
	}{
		{
			name: "success with single file registry",
			config: &config.Config{
				RegistryName: "test-registry",
				Registries: []config.RegistryConfig{
					{
						Name: "registry-1",
						File: &config.FileConfig{Path: "/path/to/file.json"},
					},
				},
			},
			expectError: false,
		},
		{
			name: "success with multiple registries",
			config: &config.Config{
				RegistryName: "test-registry",
				Registries: []config.RegistryConfig{
					{
						Name: "registry-1",
						File: &config.FileConfig{Path: "/path/to/file1.json"},
					},
					{
						Name: "registry-2",
						Git:  &config.GitConfig{Repository: "https://github.com/test/repo.git"},
					},
				},
			},
			expectError: false,
		},
		{
			name: "success with git registry",
			config: &config.Config{
				RegistryName: "test-registry",
				Registries: []config.RegistryConfig{
					{
						Name: "registry-1",
						Git: &config.GitConfig{
							Repository: "https://github.com/test/repo.git",
							Branch:     "main",
						},
					},
				},
			},
			expectError: false,
		},
		{
			name: "success with api registry",
			config: &config.Config{
				RegistryName: "test-registry",
				Registries: []config.RegistryConfig{
					{
						Name: "registry-1",
						API:  &config.APIConfig{Endpoint: "https://api.example.com"},
					},
				},
			},
			expectError: false,
		},
		{
			name: "success with empty registries",
			config: &config.Config{
				RegistryName: "test-registry",
				Registries:   []config.RegistryConfig{},
			},
			expectError: false,
		},
		{
			name:          "error with nil config",
			config:        nil,
			expectError:   true,
			errorContains: "config cannot be nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockStorageManager := sourcesmocks.NewMockStorageManager(ctrl)
			factory := NewRegistryProviderFactory(mockStorageManager)

			provider, err := factory.CreateProvider(tt.config)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
				assert.Nil(t, provider)
			} else {
				assert.NoError(t, err)
				require.NotNil(t, provider)
				// Verify the provider is a file registry data provider
				_, ok := provider.(*fileRegistryDataProvider)
				assert.True(t, ok, "provider should be a fileRegistryDataProvider")
			}
		})
	}
}
