package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfig(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		yamlContent string
		wantConfig  *Config
		wantErr     bool
	}{
		{
			name: "valid_config_matching_spec",
			yamlContent: `source:
  type: file
  file:
    path: /data/registry.json
syncPolicy:
  interval: "30m"
filter:
  tags:
    include: ["database", "production"]
    exclude: ["experimental", "deprecated", "beta"]`,
			wantConfig: &Config{
				Source: SourceConfig{
					Type: "file",
					File: &FileConfig{
						Path: "/data/registry.json",
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
  type: file
  file:
    path: /data/registry.json
syncPolicy:
  interval: "1h"
filter:
  tags:
    include: []
    exclude: []`,
			wantConfig: &Config{
				Source: SourceConfig{
					Type: "file",
					File: &FileConfig{
						Path: "/data/registry.json",
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
  type: file
  file:
    path: /data/registry.json
syncPolicy:
  interval: "15m"
filter:
  tags:
    include: ["api", "backend", "frontend"]`,
			wantConfig: &Config{
				Source: SourceConfig{
					Type: "file",
					File: &FileConfig{
						Path: "/data/registry.json",
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
  type: file
  file:
    path: /data/registry.json
syncPolicy:
  interval: "5m"
filter:
  tags:
    exclude: ["test", "debug", "experimental"]`,
			wantConfig: &Config{
				Source: SourceConfig{
					Type: "file",
					File: &FileConfig{
						Path: "/data/registry.json",
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
		},
		{
			name:        "file_not_found",
			yamlContent: "",
			wantConfig:  nil,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Create a temporary directory for test files
			tmpDir := t.TempDir()

			// TODO: this is typical Claude Code pattern, we must fix this
			if tt.name == "file_not_found" {
				// Test with non-existent file
				_, err := LoadConfig(WithConfigPath(filepath.Join(tmpDir, "non-existent.yaml")))
				require.Error(t, err)
				return
			}

			// Create test config file
			configPath := filepath.Join(tmpDir, "config.yaml")
			err := os.WriteFile(configPath, []byte(tt.yamlContent), 0600)
			require.NoError(t, err)

			// Load the config
			config, err := LoadConfig(WithConfigPath(configPath))

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantConfig, config)
		})
	}
}

func TestConfigStructure(t *testing.T) {
	t.Parallel()
	// Test that the Config struct can be properly marshaled and unmarshaled
	originalConfig := &Config{
		Source: SourceConfig{
			Type: "file",
			File: &FileConfig{
				Path: "/data/registry.json",
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
  type: file
  file:
    path: /data/registry.json
syncPolicy:
  interval: "45m"
filter:
  tags:
    include: ["prod", "stable"]
    exclude: ["beta", "alpha"]`

	err := os.WriteFile(configPath, []byte(yamlContent), 0600)
	require.NoError(t, err)

	// Load it back
	loadedConfig, err := LoadConfig(WithConfigPath(configPath))
	require.NoError(t, err)

	// Compare the structures
	assert.Equal(t, originalConfig, loadedConfig)
}

func TestConfigValidate(t *testing.T) {
	t.Parallel()
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
					Type: "file",
					File: &FileConfig{
						Path: "/data/registry.json",
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
					File: &FileConfig{
						Path: "/data/registry.json",
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
			name: "missing_file_when_type_is_file",
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
			name: "missing_sync_interval",
			config: &Config{
				Source: SourceConfig{
					Type: "file",
					File: &FileConfig{
						Path: "/data/registry.json",
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
					Type: "file",
					File: &FileConfig{
						Path: "/data/registry.json",
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
			t.Parallel()
			err := tt.config.validate()

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
	t.Parallel()
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
			t.Parallel()
			result := tt.config.GetRegistryName()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestWithConfigPath(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	err := os.MkdirAll(filepath.Join(tmpDir, "configs"), 0755)
	require.NoError(t, err, "failed to create subdir")

	configPath := filepath.Join(tmpDir, "config.yaml")
	err = os.WriteFile(configPath, []byte("source: type: file path: /data/registry.json"), 0600)
	require.NoError(t, err, "failed to write config file")

	configPath = filepath.Join(tmpDir, "configs", "app.yaml")
	err = os.WriteFile(configPath, []byte("source: type: file path: /data/registry.json"), 0600)
	require.NoError(t, err, "failed to write config file")

	err = os.Chdir(tmpDir)
	require.NoError(t, err, "failed to change directory")

	tests := []struct {
		name     string
		path     string
		wantPath string
		wantErr  bool
	}{
		{
			name:    "empty path",
			path:    "",
			wantErr: true,
		},
		{
			name:    "path traversal at start",
			path:    "../etc/passwd",
			wantErr: true,
		},
		{
			name:    "path traversal in middle",
			path:    "config/../../etc/passwd",
			wantErr: true,
		},
		{
			name:    "path traversal multiple",
			path:    "a/b/../../../etc/passwd",
			wantErr: true,
		},
		{
			name:    "path traversal with dot",
			path:    "./../etc/passwd",
			wantErr: true,
		},
		{
			name:     "valid relative path",
			path:     "config.yaml",
			wantPath: "config.yaml",
			wantErr:  false,
		},
		{
			name:     "valid relative path with subdir",
			path:     "configs/app.yaml",
			wantPath: "configs/app.yaml",
			wantErr:  false,
		},
		{
			name:    "valid absolute path with subdir",
			path:    "/foo/bar/../../../configs/app.yaml",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Test WithConfigPath directly
			opt := WithConfigPath(tt.path)
			cfg := &loaderConfig{}
			err := opt(cfg)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantPath, cfg.path)
			}
		})
	}
}
