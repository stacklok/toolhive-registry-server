package sources

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/stacklok/toolhive-registry-server/internal/config"
)

func TestNewGitRegistryHandler(t *testing.T) {
	t.Parallel()

	handler := NewGitRegistryHandler()
	assert.NotNil(t, handler, "NewGitRegistryHandler should return a non-nil handler")
}

func TestGitRegistryHandler_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		registryConfig *config.RegistryConfig
		expectError    bool
		errorContains  string
	}{
		{
			name: "valid git config with branch",
			registryConfig: &config.RegistryConfig{
				Name: "test-git",
				Git: &config.GitConfig{
					Repository: "https://github.com/test/repo.git",
					Branch:     "main",
					Path:       "registry.json",
				},
			},
			expectError: false,
		},
		{
			name: "valid git config with tag",
			registryConfig: &config.RegistryConfig{
				Name: "test-git",
				Git: &config.GitConfig{
					Repository: "https://github.com/test/repo.git",
					Tag:        "v1.0.0",
					Path:       "registry.json",
				},
			},
			expectError: false,
		},
		{
			name: "valid git config with commit",
			registryConfig: &config.RegistryConfig{
				Name: "test-git",
				Git: &config.GitConfig{
					Repository: "https://github.com/test/repo.git",
					Commit:     "abc123",
					Path:       "registry.json",
				},
			},
			expectError: false,
		},
		{
			name: "valid git config with default path",
			registryConfig: &config.RegistryConfig{
				Name: "test-git",
				Git: &config.GitConfig{
					Repository: "https://github.com/test/repo.git",
					Branch:     "main",
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
			name: "nil git config",
			registryConfig: &config.RegistryConfig{
				Name: "test-git",
				Git:  nil,
			},
			expectError:   true,
			errorContains: "git configuration is required",
		},
		{
			name: "empty repository URL",
			registryConfig: &config.RegistryConfig{
				Name: "test-git",
				Git: &config.GitConfig{
					Repository: "",
					Branch:     "main",
				},
			},
			expectError:   true,
			errorContains: "git repository URL cannot be empty",
		},
		{
			name: "both branch and tag specified",
			registryConfig: &config.RegistryConfig{
				Name: "test-git",
				Git: &config.GitConfig{
					Repository: "https://github.com/test/repo.git",
					Branch:     "main",
					Tag:        "v1.0.0",
				},
			},
			expectError:   true,
			errorContains: "only one of branch, tag, or commit may be specified",
		},
		{
			name: "both branch and commit specified",
			registryConfig: &config.RegistryConfig{
				Name: "test-git",
				Git: &config.GitConfig{
					Repository: "https://github.com/test/repo.git",
					Branch:     "main",
					Commit:     "abc123",
				},
			},
			expectError:   true,
			errorContains: "only one of branch, tag, or commit may be specified",
		},
		{
			name: "all three specified",
			registryConfig: &config.RegistryConfig{
				Name: "test-git",
				Git: &config.GitConfig{
					Repository: "https://github.com/test/repo.git",
					Branch:     "main",
					Tag:        "v1.0.0",
					Commit:     "abc123",
				},
			},
			expectError:   true,
			errorContains: "only one of branch, tag, or commit may be specified",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			handler := NewGitRegistryHandler()
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
