package sources

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/stacklok/toolhive-registry-server/internal/config"
)

func TestNewRegistryHandlerFactory(t *testing.T) {
	t.Parallel()

	factory := NewRegistryHandlerFactory()
	assert.NotNil(t, factory)
}

func TestDefaultRegistryHandlerFactory_CreateHandler(t *testing.T) {
	t.Parallel()

	factory := NewRegistryHandlerFactory()

	tests := []struct {
		name           string
		registryConfig *config.RegistryConfig
		expectError    bool
		expectedType   any
		errorContains  string
	}{
		{
			name: "file source type",
			registryConfig: &config.RegistryConfig{
				Name: "test-file",
				File: &config.FileConfig{Path: "/path/to/file"},
			},
			expectError:  false,
			expectedType: &fileRegistryHandler{},
		},
		{
			name: "git source type",
			registryConfig: &config.RegistryConfig{
				Name: "test-git",
				Git:  &config.GitConfig{Repository: "https://github.com/test/repo.git"},
			},
			expectError:  false,
			expectedType: &gitRegistryHandler{},
		},
		{
			name: "api source type",
			registryConfig: &config.RegistryConfig{
				Name: "test-api",
				API:  &config.APIConfig{Endpoint: "https://api.example.com"},
			},
			expectError:  false,
			expectedType: &apiRegistryHandler{},
		},
		{
			name: "kubernetes source type not yet implemented",
			registryConfig: &config.RegistryConfig{
				Name:       "test-kubernetes",
				Kubernetes: &config.KubernetesConfig{},
			},
			expectError:   true,
			errorContains: "kubernetes source type is not yet implemented",
		},
		{
			name: "no source type configured",
			registryConfig: &config.RegistryConfig{
				Name: "test-invalid",
			},
			expectError:   true,
			errorContains: "unable to determine source type",
		},
		{
			name:           "nil registry config",
			registryConfig: nil,
			expectError:    true,
			errorContains:  "registry configuration cannot be nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			handler, err := factory.CreateHandler(tt.registryConfig)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, handler)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, handler)
				assert.IsType(t, tt.expectedType, handler)
			}
		})
	}
}
