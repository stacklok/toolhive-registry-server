package sources

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/stacklok/toolhive-registry-server/internal/config"
)

func TestNewFileRegistryHandler(t *testing.T) {
	t.Parallel()

	handler := NewFileRegistryHandler()
	assert.NotNil(t, handler, "NewFileRegistryHandler should return a non-nil handler")
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
					Path: "/absolute/path/to/registry.json",
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
