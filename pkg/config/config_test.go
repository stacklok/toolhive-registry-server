package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfig(t *testing.T) {
	tests := []struct {
		name        string
		yamlContent string
		wantConfig  *Config
		wantErr     bool
		errMsg      string
	}{
		{
			name: "valid_config_matching_spec",
			yamlContent: `source:
  type: configmap
  configmap:
    name: minimal-registry-data
syncPolicy:
  interval: "30m"
filter:
  tags:
    include: ["database", "production"]
    exclude: ["experimental", "deprecated", "beta"]`,
			wantConfig: &Config{
				Source: SourceConfig{
					Type: "configmap",
					ConfigMap: &ConfigMapConfig{
						Name: "minimal-registry-data",
					},
				},
				SyncPolicy: &SyncPolicyConfig{
					Interval: "30m",
				},
				Filter: &FilterConfig{
					Tags: &TagFilterConfig{
						Include: []string{"database", "production"},
						Exclude: []string{"experimental", "deprecated", "beta"},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "minimal_config",
			yamlContent: `source:
  type: configmap
  configmap:
    name: test-registry
syncPolicy:
  interval: "1h"
filter:
  tags:
    include: []
    exclude: []`,
			wantConfig: &Config{
				Source: SourceConfig{
					Type: "configmap",
					ConfigMap: &ConfigMapConfig{
						Name: "test-registry",
					},
				},
				SyncPolicy: &SyncPolicyConfig{
					Interval: "1h",
				},
				Filter: &FilterConfig{
					Tags: &TagFilterConfig{
						Include: []string{},
						Exclude: []string{},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "config_with_only_include_tags",
			yamlContent: `source:
  type: configmap
  configmap:
    name: prod-registry
syncPolicy:
  interval: "15m"
filter:
  tags:
    include: ["api", "backend", "frontend"]`,
			wantConfig: &Config{
				Source: SourceConfig{
					Type: "configmap",
					ConfigMap: &ConfigMapConfig{
						Name: "prod-registry",
					},
				},
				SyncPolicy: &SyncPolicyConfig{
					Interval: "15m",
				},
				Filter: &FilterConfig{
					Tags: &TagFilterConfig{
						Include: []string{"api", "backend", "frontend"},
						Exclude: nil,
					},
				},
			},
			wantErr: false,
		},
		{
			name: "config_with_only_exclude_tags",
			yamlContent: `source:
  type: configmap
  configmap:
    name: dev-registry
syncPolicy:
  interval: "5m"
filter:
  tags:
    exclude: ["test", "debug", "experimental"]`,
			wantConfig: &Config{
				Source: SourceConfig{
					Type: "configmap",
					ConfigMap: &ConfigMapConfig{
						Name: "dev-registry",
					},
				},
				SyncPolicy: &SyncPolicyConfig{
					Interval: "5m",
				},
				Filter: &FilterConfig{
					Tags: &TagFilterConfig{
						Include: nil,
						Exclude: []string{"test", "debug", "experimental"},
					},
				},
			},
			wantErr: false,
		},
		{
			name:        "invalid_yaml",
			yamlContent: `source: [invalid yaml`,
			wantConfig:  nil,
			wantErr:     true,
			errMsg:      "failed to parse YAML config",
		},
		{
			name:        "file_not_found",
			yamlContent: "",
			wantConfig:  nil,
			wantErr:     true,
			errMsg:      "failed to read config file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary directory for test files
			tmpDir := t.TempDir()

			// Create the ConfigLoader
			loader := NewConfigLoader()

			if tt.name == "file_not_found" {
				// Test with non-existent file
				_, err := loader.LoadConfig(filepath.Join(tmpDir, "non-existent.yaml"))
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
				return
			}

			// Create test config file
			configPath := filepath.Join(tmpDir, "config.yaml")
			err := os.WriteFile(configPath, []byte(tt.yamlContent), 0644)
			require.NoError(t, err)

			// Load the config
			config, err := loader.LoadConfig(configPath)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantConfig, config)
		})
	}
}

func TestConfigStructure(t *testing.T) {
	// Test that the Config struct can be properly marshaled and unmarshaled
	originalConfig := &Config{
		Source: SourceConfig{
			Type: "configmap",
			ConfigMap: &ConfigMapConfig{
				Name: "test-configmap",
			},
		},
		SyncPolicy: &SyncPolicyConfig{
			Interval: "45m",
		},
		Filter: &FilterConfig{
			Tags: &TagFilterConfig{
				Include: []string{"prod", "stable"},
				Exclude: []string{"beta", "alpha"},
			},
		},
	}

	// Create a temporary directory and file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test-config.yaml")

	// Write the config using YAML
	yamlContent := `source:
  type: configmap
  configmap:
    name: test-configmap
syncPolicy:
  interval: "45m"
filter:
  tags:
    include: ["prod", "stable"]
    exclude: ["beta", "alpha"]`

	err := os.WriteFile(configPath, []byte(yamlContent), 0644)
	require.NoError(t, err)

	// Load it back
	loader := NewConfigLoader()
	loadedConfig, err := loader.LoadConfig(configPath)
	require.NoError(t, err)

	// Compare the structures
	assert.Equal(t, originalConfig, loadedConfig)
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid_config",
			config: &Config{
				Source: SourceConfig{
					Type: "configmap",
					ConfigMap: &ConfigMapConfig{
						Name: "test-configmap",
					},
				},
				SyncPolicy: &SyncPolicyConfig{
					Interval: "30m",
				},
			},
			wantErr: false,
		},
		{
			name: "missing_source_type",
			config: &Config{
				Source: SourceConfig{
					ConfigMap: &ConfigMapConfig{
						Name: "test",
					},
				},
				SyncPolicy: &SyncPolicyConfig{
					Interval: "30m",
				},
			},
			wantErr: true,
			errMsg:  "source.type is required",
		},
		{
			name: "missing_configmap_when_type_is_configmap",
			config: &Config{
				Source: SourceConfig{
					Type: "configmap",
				},
				SyncPolicy: &SyncPolicyConfig{
					Interval: "30m",
				},
			},
			wantErr: true,
			errMsg:  "source.configmap is required",
		},
		{
			name: "missing_configmap_name",
			config: &Config{
				Source: SourceConfig{
					Type:      "configmap",
					ConfigMap: &ConfigMapConfig{},
				},
				SyncPolicy: &SyncPolicyConfig{
					Interval: "30m",
				},
			},
			wantErr: true,
			errMsg:  "source.configmap.name is required",
		},
		{
			name: "missing_sync_interval",
			config: &Config{
				Source: SourceConfig{
					Type: "configmap",
					ConfigMap: &ConfigMapConfig{
						Name: "test",
					},
				},
				SyncPolicy: &SyncPolicyConfig{},
			},
			wantErr: true,
			errMsg:  "syncPolicy.interval is required",
		},
		{
			name: "invalid_sync_interval",
			config: &Config{
				Source: SourceConfig{
					Type: "configmap",
					ConfigMap: &ConfigMapConfig{
						Name: "test",
					},
				},
				SyncPolicy: &SyncPolicyConfig{
					Interval: "invalid",
				},
			},
			wantErr: true,
			errMsg:  "syncPolicy.interval must be a valid duration",
		},
		{
			name:    "nil_config",
			config:  nil,
			wantErr: true,
			errMsg:  "config cannot be nil",
		},
		{
			name: "valid_file_source",
			config: &Config{
				RegistryName: "test-registry",
				Source: SourceConfig{
					Type: "file",
					File: &FileConfig{
						Path: "/tmp/registry.json",
					},
				},
				SyncPolicy: &SyncPolicyConfig{
					Interval: "30m",
				},
			},
			wantErr: false,
		},
		{
			name: "missing_file_config",
			config: &Config{
				Source: SourceConfig{
					Type: "file",
				},
				SyncPolicy: &SyncPolicyConfig{
					Interval: "30m",
				},
			},
			wantErr: true,
			errMsg:  "source.file is required",
		},
		{
			name: "missing_file_path",
			config: &Config{
				Source: SourceConfig{
					Type: "file",
					File: &FileConfig{},
				},
				SyncPolicy: &SyncPolicyConfig{
					Interval: "30m",
				},
			},
			wantErr: true,
			errMsg:  "source.file.path is required",
		},
		{
			name: "unsupported_source_type",
			config: &Config{
				Source: SourceConfig{
					Type: "unknown",
				},
				SyncPolicy: &SyncPolicyConfig{
					Interval: "30m",
				},
			},
			wantErr: true,
			errMsg:  "unsupported source type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()

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

func TestGetRegistryName(t *testing.T) {
	tests := []struct {
		name     string
		config   *Config
		expected string
	}{
		{
			name: "with_registry_name",
			config: &Config{
				RegistryName: "my-registry",
			},
			expected: "my-registry",
		},
		{
			name:     "without_registry_name",
			config:   &Config{},
			expected: "default",
		},
		{
			name: "empty_registry_name",
			config: &Config{
				RegistryName: "",
			},
			expected: "default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.GetRegistryName()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestStrictYAMLUnmarshaling(t *testing.T) {
	tests := []struct {
		name        string
		yamlContent string
		wantErr     bool
		errContains string
	}{
		{
			name: "valid_config_all_fields_correct",
			yamlContent: `registryName: test-registry
source:
  type: file
  format: toolhive
  file:
    path: /tmp/registry.json
syncPolicy:
  interval: "30m"`,
			wantErr: false,
		},
		{
			name: "typo_in_top_level_field",
			yamlContent: `oooregistryName: test-registry
source:
  type: file
  file:
    path: /tmp/registry.json
syncPolicy:
  interval: "30m"`,
			wantErr:     true,
			errContains: "field oooregistryName not found",
		},
		{
			name: "typo_in_source_type",
			yamlContent: `registryName: test-registry
source:
  typeeee: file
  file:
    path: /tmp/registry.json
syncPolicy:
  interval: "30m"`,
			wantErr:     true,
			errContains: "field typeeee not found",
		},
		{
			name: "unknown_field_in_file_config",
			yamlContent: `registryName: test-registry
source:
  type: file
  file:
    path: /tmp/registry.json
    unknownField: value
syncPolicy:
  interval: "30m"`,
			wantErr:     true,
			errContains: "field unknownField not found",
		},
		{
			name: "unknown_field_in_sync_policy",
			yamlContent: `registryName: test-registry
source:
  type: file
  file:
    path: /tmp/registry.json
syncPolicy:
  interval: "30m"
  invalidField: true`,
			wantErr:     true,
			errContains: "field invalidField not found",
		},
		{
			name: "unknown_field_in_git_config",
			yamlContent: `registryName: test-registry
source:
  type: git
  git:
    repository: https://github.com/example/repo.git
    branch: main
    path: registry.json
    unknownGitField: value
syncPolicy:
  interval: "30m"`,
			wantErr:     true,
			errContains: "field unknownGitField not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "config.yaml")
			err := os.WriteFile(configPath, []byte(tt.yamlContent), 0644)
			require.NoError(t, err)

			loader := NewConfigLoader()
			_, err = loader.LoadConfig(configPath)

			if tt.wantErr {
				require.Error(t, err, "Expected error for config with unknown fields")
				assert.Contains(t, err.Error(), tt.errContains,
					"Error message should mention the unknown field")
			} else {
				require.NoError(t, err, "Valid config should load without error")
			}
		})
	}
}
