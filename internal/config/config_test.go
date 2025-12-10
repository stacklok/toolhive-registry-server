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
		name             string
		yamlContent      string
		skipFileCreation bool
		wantConfig       *Config
		wantErr          bool
	}{
		{
			name: "valid_config_matching_spec",
			yamlContent: `registries:
  - name: test-registry
    file:
      path: /data/registry.json
    syncPolicy:
      interval: "30m"
    filter:
      tags:
        include: ["database", "production"]
        exclude: ["experimental", "deprecated", "beta"]
auth:
  mode: anonymous
`,
			wantConfig: &Config{
				Registries: []RegistryConfig{
					{
						Name: "test-registry",
						File: &FileConfig{
							Path: "/data/registry.json",
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
				},
				Auth: &AuthConfig{
					Mode: AuthModeAnonymous,
				},
			},
			wantErr: false,
		},
		{
			name: "minimal_config",
			yamlContent: `registries:
  - name: minimal-registry
    file:
      path: /data/registry.json
    syncPolicy:
      interval: "1h"
    filter:
      tags:
        include: []
        exclude: []
auth:
  mode: anonymous
`,
			wantConfig: &Config{
				Registries: []RegistryConfig{
					{
						Name: "minimal-registry",
						File: &FileConfig{
							Path: "/data/registry.json",
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
				},
				Auth: &AuthConfig{
					Mode: AuthModeAnonymous,
				},
			},
			wantErr: false,
		},
		{
			name: "config_with_only_include_tags",
			yamlContent: `registries:
  - name: include-tags-registry
    file:
      path: /data/registry.json
    syncPolicy:
      interval: "15m"
    filter:
      tags:
        include: ["api", "backend", "frontend"]
auth:
  mode: anonymous
`,
			wantConfig: &Config{
				Registries: []RegistryConfig{
					{
						Name: "include-tags-registry",
						File: &FileConfig{
							Path: "/data/registry.json",
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
				},
				Auth: &AuthConfig{
					Mode: AuthModeAnonymous,
				},
			},
			wantErr: false,
		},
		{
			name: "config_with_only_exclude_tags",
			yamlContent: `registries:
  - name: exclude-tags-registry
    file:
      path: /data/registry.json
    syncPolicy:
      interval: "5m"
    filter:
      tags:
        exclude: ["test", "debug", "experimental"]
auth:
  mode: anonymous
`,
			wantConfig: &Config{
				Registries: []RegistryConfig{
					{
						Name: "exclude-tags-registry",
						File: &FileConfig{
							Path: "/data/registry.json",
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
				},
				Auth: &AuthConfig{
					Mode: AuthModeAnonymous,
				},
			},
			wantErr: false,
		},
		{
			name:        "invalid_yaml",
			yamlContent: `registries: [invalid yaml`,
			wantConfig:  nil,
			wantErr:     true,
		},
		{
			name:             "file_not_found",
			yamlContent:      "",
			skipFileCreation: true,
			wantConfig:       nil,
			wantErr:          true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Create a temporary directory for test files
			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "config.yaml")

			if tt.skipFileCreation {
				// Test with non-existent file
				configPath = filepath.Join(tmpDir, "non-existent.yaml")
			} else {
				// Create test config file
				err := os.WriteFile(configPath, []byte(tt.yamlContent), 0600)
				require.NoError(t, err)
			}

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
		Registries: []RegistryConfig{
			{
				Name: "test-registry",
				File: &FileConfig{
					Path: "/data/registry.json",
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
			},
		},
		Auth: &AuthConfig{
			Mode: AuthModeAnonymous,
		},
	}

	// Create a temporary directory and file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test-config.yaml")

	// Write the config using YAML
	yamlContent := `registries:
  - name: test-registry
    file:
      path: /data/registry.json
    syncPolicy:
      interval: "45m"
    filter:
      tags:
        include: ["prod", "stable"]
        exclude: ["beta", "alpha"]
auth:
  mode: anonymous
`

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
				Registries: []RegistryConfig{
					{
						Name: "test-registry",
						File: &FileConfig{
							Path: "/data/registry.json",
						},
						SyncPolicy: &SyncPolicyConfig{
							Interval: "30m",
						},
					},
				},
				Auth: &AuthConfig{
					Mode: AuthModeAnonymous,
				},
			},
			wantErr: false,
		},
		{
			name: "missing_registry_name",
			config: &Config{
				Registries: []RegistryConfig{
					{
						File: &FileConfig{
							Path: "/data/registry.json",
						},
						SyncPolicy: &SyncPolicyConfig{
							Interval: "30m",
						},
					},
				},
				Auth: &AuthConfig{
					Mode: AuthModeAnonymous,
				},
			},
			wantErr: true,
			errMsg:  "name is required",
		},
		{
			name: "missing_file_when_no_source_configured",
			config: &Config{
				Registries: []RegistryConfig{
					{
						Name: "test-registry",
						SyncPolicy: &SyncPolicyConfig{
							Interval: "30m",
						},
					},
				},
				Auth: &AuthConfig{
					Mode: AuthModeAnonymous,
				},
			},
			wantErr: true,
			errMsg:  "one of git, api, file, managed, or kubernetes configuration must be specified",
		},
		{
			name: "missing_file_path_or_url",
			config: &Config{
				Registries: []RegistryConfig{
					{
						Name: "test-registry",
						File: &FileConfig{},
						SyncPolicy: &SyncPolicyConfig{
							Interval: "30m",
						},
					},
				},
				Auth: &AuthConfig{
					Mode: AuthModeAnonymous,
				},
			},
			wantErr: true,
			errMsg:  "file.path or file.url is required",
		},
		{
			name: "file_path_and_url_mutually_exclusive",
			config: &Config{
				Registries: []RegistryConfig{
					{
						Name: "test-registry",
						File: &FileConfig{
							Path: "/data/registry.json",
							URL:  "https://example.com/registry.json",
						},
						SyncPolicy: &SyncPolicyConfig{
							Interval: "30m",
						},
					},
				},
				Auth: &AuthConfig{
					Mode: AuthModeAnonymous,
				},
			},
			wantErr: true,
			errMsg:  "file.path and file.url are mutually exclusive",
		},
		{
			name: "valid_file_url",
			config: &Config{
				Registries: []RegistryConfig{
					{
						Name: "test-registry",
						File: &FileConfig{
							URL: "https://example.com/registry.json",
						},
						SyncPolicy: &SyncPolicyConfig{
							Interval: "30m",
						},
					},
				},
				Auth: &AuthConfig{
					Mode: AuthModeAnonymous,
				},
			},
			wantErr: false,
		},
		{
			name: "valid_file_url_with_timeout",
			config: &Config{
				Registries: []RegistryConfig{
					{
						Name: "test-registry",
						File: &FileConfig{
							URL:     "https://example.com/registry.json",
							Timeout: "1m",
						},
						SyncPolicy: &SyncPolicyConfig{
							Interval: "30m",
						},
					},
				},
				Auth: &AuthConfig{
					Mode: AuthModeAnonymous,
				},
			},
			wantErr: false,
		},
		{
			name: "invalid_file_url_timeout",
			config: &Config{
				Registries: []RegistryConfig{
					{
						Name: "test-registry",
						File: &FileConfig{
							URL:     "https://example.com/registry.json",
							Timeout: "invalid",
						},
						SyncPolicy: &SyncPolicyConfig{
							Interval: "30m",
						},
					},
				},
				Auth: &AuthConfig{
					Mode: AuthModeAnonymous,
				},
			},
			wantErr: true,
			errMsg:  "file.timeout must be a valid duration",
		},
		{
			name: "invalid_file_url_scheme",
			config: &Config{
				Registries: []RegistryConfig{
					{
						Name: "test-registry",
						File: &FileConfig{
							URL: "ftp://example.com/registry.json",
						},
						SyncPolicy: &SyncPolicyConfig{
							Interval: "30m",
						},
					},
				},
				Auth: &AuthConfig{
					Mode: AuthModeAnonymous,
				},
			},
			wantErr: true,
			errMsg:  "file.url must use http or https scheme",
		},
		{
			name: "invalid_file_url_no_host",
			config: &Config{
				Registries: []RegistryConfig{
					{
						Name: "test-registry",
						File: &FileConfig{
							URL: "/path/to/file",
						},
						SyncPolicy: &SyncPolicyConfig{
							Interval: "30m",
						},
					},
				},
				Auth: &AuthConfig{
					Mode: AuthModeAnonymous,
				},
			},
			wantErr: true,
			errMsg:  "file.url must be an absolute URL with host",
		},
		{
			name: "missing_sync_interval",
			config: &Config{
				Registries: []RegistryConfig{
					{
						Name: "test-registry",
						File: &FileConfig{
							Path: "/data/registry.json",
						},
						SyncPolicy: &SyncPolicyConfig{},
					},
				},
				Auth: &AuthConfig{
					Mode: AuthModeAnonymous,
				},
			},
			wantErr: true,
			errMsg:  "syncPolicy.interval is required",
		},
		{
			name: "invalid_sync_interval",
			config: &Config{
				Registries: []RegistryConfig{
					{
						Name: "test-registry",
						File: &FileConfig{
							Path: "/data/registry.json",
						},
						SyncPolicy: &SyncPolicyConfig{
							Interval: "invalid",
						},
					},
				},
				Auth: &AuthConfig{
					Mode: AuthModeAnonymous,
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
			name: "empty_registries",
			config: &Config{
				Registries: []RegistryConfig{},
				Auth: &AuthConfig{
					Mode: AuthModeAnonymous,
				},
			},
			wantErr: true,
			errMsg:  "at least one registry must be configured",
		},
		{
			name: "valid_file_source",
			config: &Config{
				Registries: []RegistryConfig{
					{
						Name: "test-registry",
						File: &FileConfig{
							Path: "/tmp/registry.json",
						},
						SyncPolicy: &SyncPolicyConfig{
							Interval: "30m",
						},
					},
				},
				Auth: &AuthConfig{
					Mode: AuthModeAnonymous,
				},
			},
			wantErr: false,
		},
		{
			name: "duplicate_registry_names",
			config: &Config{
				Registries: []RegistryConfig{
					{
						Name: "duplicate",
						File: &FileConfig{
							Path: "/tmp/registry1.json",
						},
						SyncPolicy: &SyncPolicyConfig{
							Interval: "30m",
						},
					},
					{
						Name: "duplicate",
						File: &FileConfig{
							Path: "/tmp/registry2.json",
						},
						SyncPolicy: &SyncPolicyConfig{
							Interval: "30m",
						},
					},
				},
				Auth: &AuthConfig{
					Mode: AuthModeAnonymous,
				},
			},
			wantErr: true,
			errMsg:  "duplicate registry name",
		},
		{
			name: "invalid_format_when_using_api",
			config: &Config{
				Registries: []RegistryConfig{
					{
						Name:   "api-registry",
						Format: "toolhive",
						API: &APIConfig{
							Endpoint: "http://example.com",
						},
						SyncPolicy: &SyncPolicyConfig{
							Interval: "30m",
						},
					},
				},
				Auth: &AuthConfig{
					Mode: AuthModeAnonymous,
				},
			},
			wantErr: true,
			errMsg:  "format must be either empty or upstream",
		},
		{
			name: "multiple_source_types_specified",
			config: &Config{
				Registries: []RegistryConfig{
					{
						Name: "multi-source",
						File: &FileConfig{
							Path: "/tmp/registry.json",
						},
						Git: &GitConfig{
							Repository: "https://github.com/example/repo.git",
						},
						SyncPolicy: &SyncPolicyConfig{
							Interval: "30m",
						},
					},
				},
				Auth: &AuthConfig{
					Mode: AuthModeAnonymous,
				},
			},
			wantErr: true,
			errMsg:  "only one of git, api, file, managed, or kubernetes configuration may be specified",
		},
		{
			name: "valid_managed_registry_no_sync_policy",
			config: &Config{
				Registries: []RegistryConfig{
					{
						Name:    "managed-registry",
						Managed: &ManagedConfig{},
						// No SyncPolicy required for managed registries
					},
				},
				Auth: &AuthConfig{
					Mode: AuthModeAnonymous,
				},
			},
			wantErr: false,
		},
		{
			name: "valid_managed_registry_with_ignored_sync_policy",
			config: &Config{
				Registries: []RegistryConfig{
					{
						Name:    "managed-registry",
						Managed: &ManagedConfig{},
						// SyncPolicy is silently ignored for managed registries
						SyncPolicy: &SyncPolicyConfig{
							Interval: "30m",
						},
					},
				},
				Auth: &AuthConfig{
					Mode: AuthModeAnonymous,
				},
			},
			wantErr: false,
		},
		{
			name: "valid_managed_registry_with_ignored_filter",
			config: &Config{
				Registries: []RegistryConfig{
					{
						Name:    "managed-registry",
						Managed: &ManagedConfig{},
						// Filter is silently ignored for managed registries
						Filter: &FilterConfig{
							Tags: &TagFilterConfig{
								Include: []string{"test"},
							},
						},
					},
				},
				Auth: &AuthConfig{
					Mode: AuthModeAnonymous,
				},
			},
			wantErr: false,
		},
		{
			name: "valid_kubernetes_registry_no_sync_policy",
			config: &Config{
				Registries: []RegistryConfig{
					{
						Name:       "kubernetes-registry",
						Kubernetes: &KubernetesConfig{},
						// No SyncPolicy required for kubernetes registries
					},
				},
				Auth: &AuthConfig{
					Mode: AuthModeAnonymous,
				},
			},
			wantErr: false,
		},
		{
			name: "valid_kubernetes_registry_with_ignored_sync_policy",
			config: &Config{
				Registries: []RegistryConfig{
					{
						Name:       "kubernetes-registry",
						Kubernetes: &KubernetesConfig{},
						// SyncPolicy is silently ignored for kubernetes registries
						SyncPolicy: &SyncPolicyConfig{
							Interval: "30m",
						},
					},
				},
				Auth: &AuthConfig{
					Mode: AuthModeAnonymous,
				},
			},
			wantErr: false,
		},
		{
			name: "valid_kubernetes_registry_with_ignored_filter",
			config: &Config{
				Registries: []RegistryConfig{
					{
						Name:       "kubernetes-registry",
						Kubernetes: &KubernetesConfig{},
						// Filter is silently ignored for kubernetes registries
						Filter: &FilterConfig{
							Tags: &TagFilterConfig{
								Include: []string{"test"},
							},
						},
					},
				},
				Auth: &AuthConfig{
					Mode: AuthModeAnonymous,
				},
			},
			wantErr: false,
		},
		{
			name: "managed_and_file_source_specified",
			config: &Config{
				Registries: []RegistryConfig{
					{
						Name:    "multi-source",
						Managed: &ManagedConfig{},
						File: &FileConfig{
							Path: "/tmp/registry.json",
						},
						SyncPolicy: &SyncPolicyConfig{
							Interval: "30m",
						},
					},
				},
			},
			wantErr: true,
			errMsg:  "only one of git, api, file, managed, or kubernetes configuration may be specified",
		},
		{
			name: "managed_and_git_source_specified",
			config: &Config{
				Registries: []RegistryConfig{
					{
						Name:    "multi-source",
						Managed: &ManagedConfig{},
						Git: &GitConfig{
							Repository: "https://github.com/example/repo.git",
						},
						SyncPolicy: &SyncPolicyConfig{
							Interval: "30m",
						},
					},
				},
			},
			wantErr: true,
			errMsg:  "only one of git, api, file, managed, or kubernetes configuration may be specified",
		},
		{
			name: "managed_and_api_source_specified",
			config: &Config{
				Registries: []RegistryConfig{
					{
						Name:    "multi-source",
						Managed: &ManagedConfig{},
						API: &APIConfig{
							Endpoint: "http://example.com",
						},
						SyncPolicy: &SyncPolicyConfig{
							Interval: "30m",
						},
					},
				},
			},
			wantErr: true,
			errMsg:  "only one of git, api, file, managed, or kubernetes configuration may be specified",
		},
		{
			name: "mixed_managed_and_synced_registries",
			config: &Config{
				Registries: []RegistryConfig{
					{
						Name: "file-registry",
						File: &FileConfig{
							Path: "/data/registry.json",
						},
						SyncPolicy: &SyncPolicyConfig{
							Interval: "30m",
						},
					},
					{
						Name:    "managed-registry",
						Managed: &ManagedConfig{},
					},
					{
						Name: "git-registry",
						Git: &GitConfig{
							Repository: "https://github.com/example/repo.git",
						},
						SyncPolicy: &SyncPolicyConfig{
							Interval: "1h",
						},
					},
				},
				Auth: &AuthConfig{
					Mode: AuthModeAnonymous,
				},
			},
			wantErr: false,
		},
		{
			name: "valid_config_with_file_storage",
			config: &Config{
				Registries: []RegistryConfig{
					{
						Name: "test-registry",
						File: &FileConfig{
							Path: "/data/registry.json",
						},
						SyncPolicy: &SyncPolicyConfig{
							Interval: "30m",
						},
					},
				},
				FileStorage: &FileStorageConfig{
					BaseDir: "/custom/data",
				},
				Auth: &AuthConfig{
					Mode: AuthModeAnonymous,
				},
			},
			wantErr: false,
		},
		{
			name: "valid_config_with_database_storage",
			config: &Config{
				Registries: []RegistryConfig{
					{
						Name: "test-registry",
						File: &FileConfig{
							Path: "/data/registry.json",
						},
						SyncPolicy: &SyncPolicyConfig{
							Interval: "30m",
						},
					},
				},
				Database: &DatabaseConfig{
					Host:     "localhost",
					Port:     5432,
					User:     "testuser",
					Database: "testdb",
				},
				Auth: &AuthConfig{
					Mode: AuthModeAnonymous,
				},
			},
			wantErr: false,
		},
		// TODO: Reinstate this test case once database is fully wired in
		// {
		// 	name: "both_file_storage_and_database_configured",
		// 	config: &Config{
		// 		Source: SourceConfig{
		// 			Type: "file",
		// 			File: &FileConfig{
		// 				Path: "/data/registry.json",
		// 			},
		// 		},
		// 		SyncPolicy: &SyncPolicyConfig{
		// 			Interval: "30m",
		// 		},
		// 		FileStorage: &FileStorageConfig{
		// 			BaseDir: "/custom/data",
		// 		},
		// 		Database: &DatabaseConfig{
		// 			Host:     "localhost",
		// 			Port:     5432,
		// 			User:     "testuser",
		// 			Database: "testdb",
		// 		},
		// 	},
		// 	wantErr: true,
		// 	errMsg:  "cannot configure both database and fileStorage",
		// },
		{
			name: "database_storage_missing_host",
			config: &Config{
				Registries: []RegistryConfig{
					{
						Name: "test-registry",
						File: &FileConfig{
							Path: "/data/registry.json",
						},
						SyncPolicy: &SyncPolicyConfig{
							Interval: "30m",
						},
					},
				},
				Database: &DatabaseConfig{
					Port:     5432,
					User:     "testuser",
					Database: "testdb",
				},
			},
			wantErr: true,
			errMsg:  "database.host is required",
		},
		{
			name: "database_storage_missing_port",
			config: &Config{
				Registries: []RegistryConfig{
					{
						Name: "test-registry",
						File: &FileConfig{
							Path: "/data/registry.json",
						},
						SyncPolicy: &SyncPolicyConfig{
							Interval: "30m",
						},
					},
				},
				Database: &DatabaseConfig{
					Host:     "localhost",
					User:     "testuser",
					Database: "testdb",
				},
			},
			wantErr: true,
			errMsg:  "database.port is required",
		},
		{
			name: "database_storage_missing_user",
			config: &Config{
				Registries: []RegistryConfig{
					{
						Name: "test-registry",
						File: &FileConfig{
							Path: "/data/registry.json",
						},
						SyncPolicy: &SyncPolicyConfig{
							Interval: "30m",
						},
					},
				},
				Database: &DatabaseConfig{
					Host:     "localhost",
					Port:     5432,
					Database: "testdb",
				},
			},
			wantErr: true,
			errMsg:  "database.user is required",
		},
		{
			name: "database_storage_missing_database",
			config: &Config{
				Registries: []RegistryConfig{
					{
						Name: "test-registry",
						File: &FileConfig{
							Path: "/data/registry.json",
						},
						SyncPolicy: &SyncPolicyConfig{
							Interval: "30m",
						},
					},
				},
				Database: &DatabaseConfig{
					Host: "localhost",
					Port: 5432,
					User: "testuser",
				},
			},
			wantErr: true,
			errMsg:  "database.database is required",
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

func TestGetStorageType(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		config   *Config
		expected StorageType
	}{
		{
			name: "with_file_storage",
			config: &Config{
				FileStorage: &FileStorageConfig{
					BaseDir: "/custom/data",
				},
			},
			expected: StorageTypeFile,
		},
		{
			name: "with_database_storage",
			config: &Config{
				Database: &DatabaseConfig{
					Host:     "localhost",
					Port:     5432,
					User:     "testuser",
					Database: "testdb",
				},
			},
			expected: StorageTypeDatabase,
		},
		{
			name:     "without_storage_defaults_to_file",
			config:   &Config{},
			expected: StorageTypeFile,
		},
		{
			name: "database_takes_precedence",
			config: &Config{
				Database: &DatabaseConfig{
					Host:     "localhost",
					Port:     5432,
					User:     "testuser",
					Database: "testdb",
				},
			},
			expected: StorageTypeDatabase,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := tt.config.GetStorageType()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetFileStorageBaseDir(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		config   *Config
		expected string
	}{
		{
			name: "with_custom_base_dir",
			config: &Config{
				FileStorage: &FileStorageConfig{
					BaseDir: "/custom/data",
				},
			},
			expected: "/custom/data",
		},
		{
			name: "with_empty_base_dir",
			config: &Config{
				FileStorage: &FileStorageConfig{
					BaseDir: "",
				},
			},
			expected: "./data",
		},
		{
			name:     "without_file_storage_config",
			config:   &Config{},
			expected: "./data",
		},
		{
			name: "nil_file_storage",
			config: &Config{
				FileStorage: nil,
			},
			expected: "./data",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := tt.config.GetFileStorageBaseDir()
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

func TestDatabaseConfigGetPassword(t *testing.T) {
	t.Parallel()

	// GetPassword now always returns empty string to delegate to pgpass
	dbConfig := &DatabaseConfig{
		Host:     "localhost",
		Port:     5432,
		User:     "testuser",
		Database: "testdb",
	}

	password := dbConfig.GetPassword()
	assert.Equal(t, "", password, "GetPassword should return empty string to delegate to pgpass")
}

func TestDatabaseConfigGetMigrationUser(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		dbConfig *DatabaseConfig
		wantUser string
	}{
		{
			name: "migration_user_set",
			dbConfig: &DatabaseConfig{
				User:          "appuser",
				MigrationUser: "migratoruser",
			},
			wantUser: "migratoruser",
		},
		{
			name: "migration_user_not_set_defaults_to_user",
			dbConfig: &DatabaseConfig{
				User: "appuser",
			},
			wantUser: "appuser",
		},
		{
			name: "migration_user_empty_defaults_to_user",
			dbConfig: &DatabaseConfig{
				User:          "appuser",
				MigrationUser: "",
			},
			wantUser: "appuser",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			user := tt.dbConfig.GetMigrationUser()
			assert.Equal(t, tt.wantUser, user)
		})
	}
}

func TestDatabaseConfigGetMigrationPassword(t *testing.T) {
	t.Parallel()

	// GetMigrationPassword always returns empty string to delegate to pgpass
	dbConfig := &DatabaseConfig{
		Host:          "localhost",
		Port:          5432,
		User:          "appuser",
		MigrationUser: "migratoruser",
		Database:      "testdb",
	}

	password := dbConfig.GetMigrationPassword()
	assert.Equal(t, "", password, "GetMigrationPassword should return empty string to delegate to pgpass")
}

func TestDatabaseConfigGetConnectionString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		dbConfig    *DatabaseConfig
		wantConnStr string
	}{
		{
			name: "connection_string_with_default_sslmode",
			dbConfig: &DatabaseConfig{
				Host:     "localhost",
				Port:     5432,
				User:     "testuser",
				Database: "testdb",
			},
			// Password omitted - pgx will use pgpass file
			wantConnStr: "postgres://testuser@localhost:5432/testdb?sslmode=require",
		},
		{
			name: "connection_string_with_custom_sslmode",
			dbConfig: &DatabaseConfig{
				Host:     "db.example.com",
				Port:     5433,
				User:     "admin",
				Database: "production",
				SSLMode:  "verify-full",
			},
			wantConnStr: "postgres://admin@db.example.com:5433/production?sslmode=verify-full",
		},
		{
			name: "connection_string_with_disable_sslmode",
			dbConfig: &DatabaseConfig{
				Host:     "localhost",
				Port:     5432,
				User:     "testuser",
				Database: "testdb",
				SSLMode:  "disable",
			},
			wantConnStr: "postgres://testuser@localhost:5432/testdb?sslmode=disable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			connStr := tt.dbConfig.GetConnectionString()
			assert.Equal(t, tt.wantConnStr, connStr)
		})
	}
}

func TestDatabaseConfigGetMigrationConnectionString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		dbConfig    *DatabaseConfig
		wantConnStr string
	}{
		{
			name: "migration_connection_string_with_migration_user",
			dbConfig: &DatabaseConfig{
				Host:          "localhost",
				Port:          5432,
				User:          "appuser",
				MigrationUser: "migratoruser",
				Database:      "testdb",
			},
			// Uses migration user, password omitted - pgx will use pgpass file
			wantConnStr: "postgres://migratoruser@localhost:5432/testdb?sslmode=require",
		},
		{
			name: "migration_connection_string_defaults_to_user",
			dbConfig: &DatabaseConfig{
				Host:     "localhost",
				Port:     5432,
				User:     "appuser",
				Database: "testdb",
			},
			// Falls back to regular user when MigrationUser not set
			wantConnStr: "postgres://appuser@localhost:5432/testdb?sslmode=require",
		},
		{
			name: "migration_connection_string_with_custom_sslmode",
			dbConfig: &DatabaseConfig{
				Host:          "db.example.com",
				Port:          5433,
				User:          "appuser",
				MigrationUser: "migratoruser",
				Database:      "production",
				SSLMode:       "verify-full",
			},
			wantConnStr: "postgres://migratoruser@db.example.com:5433/production?sslmode=verify-full",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			connStr := tt.dbConfig.GetMigrationConnectionString()
			assert.Equal(t, tt.wantConnStr, connStr)
		})
	}
}

func TestLoadConfigWithDatabase(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		yamlContent string
		wantConfig  *Config
		wantErr     bool
	}{
		{
			name: "config_with_database_minimal",
			yamlContent: `registries:
  - name: test-registry
    file:
      path: /data/registry.json
    syncPolicy:
      interval: "30m"
auth:
  mode: anonymous
database:
  host: localhost
  port: 5432
  user: testuser
  database: testdb`,
			wantConfig: &Config{
				Registries: []RegistryConfig{
					{
						Name: "test-registry",
						File: &FileConfig{
							Path: "/data/registry.json",
						},
						SyncPolicy: &SyncPolicyConfig{
							Interval: "30m",
						},
					},
				},
				Auth: &AuthConfig{
					Mode: AuthModeAnonymous,
				},
				Database: &DatabaseConfig{
					Host:     "localhost",
					Port:     5432,
					User:     "testuser",
					Database: "testdb",
				},
			},
			wantErr: false,
		},
		{
			name: "config_with_database_full",
			yamlContent: `registries:
  - name: production-registry
    file:
      path: /data/registry.json
    syncPolicy:
      interval: "1h"
auth:
  mode: anonymous
database:
  host: db.example.com
  port: 5433
  user: admin
  migrationUser: admin_migrator
  database: production
  sslMode: verify-full
  maxOpenConns: 25
  maxIdleConns: 10
  connMaxLifetime: "1h"`,
			wantConfig: &Config{
				Registries: []RegistryConfig{
					{
						Name: "production-registry",
						File: &FileConfig{
							Path: "/data/registry.json",
						},
						SyncPolicy: &SyncPolicyConfig{
							Interval: "1h",
						},
					},
				},
				Auth: &AuthConfig{
					Mode: AuthModeAnonymous,
				},
				Database: &DatabaseConfig{
					Host:            "db.example.com",
					Port:            5433,
					User:            "admin",
					MigrationUser:   "admin_migrator",
					Database:        "production",
					SSLMode:         "verify-full",
					MaxOpenConns:    25,
					MaxIdleConns:    10,
					ConnMaxLifetime: "1h",
				},
			},
			wantErr: false,
		},
		{
			name: "config_without_database",
			yamlContent: `registries:
  - name: simple-registry
    file:
      path: /data/registry.json
    syncPolicy:
      interval: "30m"
auth:
  mode: anonymous
`,
			wantConfig: &Config{
				Registries: []RegistryConfig{
					{
						Name: "simple-registry",
						File: &FileConfig{
							Path: "/data/registry.json",
						},
						SyncPolicy: &SyncPolicyConfig{
							Interval: "30m",
						},
					},
				},
				Auth: &AuthConfig{
					Mode: AuthModeAnonymous,
				},
				Database: nil,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create a temporary directory for test files
			tmpDir := t.TempDir()

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

func TestAuthConfigLoading(t *testing.T) {
	t.Parallel()

	// These tests verify that auth configuration is correctly parsed during LoadConfig.
	// Note: Auth validation is deferred to serve.go after mode resolution,
	// so these tests focus on parsing behavior, not validation.
	tests := []struct {
		name  string
		yaml  string
		check func(t *testing.T, cfg *Config)
	}{
		{
			name: "no auth section results in nil Auth",
			yaml: `registries:
  - name: test-registry
    file:
      path: /data/registry.json
    syncPolicy:
      interval: "30m"`,
			check: nil,
		},
		{
			name: "oauth with providers is parsed correctly",
			yaml: `registries:
  - name: test-registry
    file:
      path: /data/registry.json
    syncPolicy:
      interval: "30m"
auth:
  mode: oauth
  oauth:
    providers:
      - name: kubernetes
        issuerUrl: https://kubernetes.default.svc.cluster.local
        audience: https://kubernetes.default.svc.cluster.local
      - name: okta
        issuerUrl: https://dev-12345.okta.com
        audience: api://mcp-registry`,
			//nolint:thelper // We want to see errors here
			check: func(t *testing.T, cfg *Config) {
				require.NotNil(t, cfg.Auth)
				assert.Equal(t, AuthModeOAuth, cfg.Auth.Mode)
				require.NotNil(t, cfg.Auth.OAuth)
				assert.Len(t, cfg.Auth.OAuth.Providers, 2)
				assert.Equal(t, "https://kubernetes.default.svc.cluster.local", cfg.Auth.OAuth.Providers[0].IssuerURL)
				assert.Equal(t, "https://dev-12345.okta.com", cfg.Auth.OAuth.Providers[1].IssuerURL)
			},
		},
		{
			name: "explicit anonymous mode is parsed correctly",
			yaml: `registries:
  - name: test-registry
    file:
      path: /data/registry.json
    syncPolicy:
      interval: "30m"
auth:
  mode: anonymous`,
			//nolint:thelper // We want to see errors here
			check: func(t *testing.T, cfg *Config) {
				require.NotNil(t, cfg.Auth)
				assert.Equal(t, AuthModeAnonymous, cfg.Auth.Mode)
			},
		},
		{
			name: "empty mode is parsed as empty string",
			yaml: `registries:
  - name: test-registry
    file:
      path: /data/registry.json
    syncPolicy:
      interval: "30m"
auth:
  mode: ""`,
			check: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "config.yaml")
			err := os.WriteFile(configPath, []byte(tt.yaml), 0600)
			require.NoError(t, err)

			cfg, err := LoadConfig(WithConfigPath(configPath))
			if tt.check == nil {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			tt.check(t, cfg)
		})
	}
}

func TestAuthConfigValidate(t *testing.T) {
	t.Parallel()

	// These tests verify AuthConfig.Validate() behavior.
	// The method assumes Mode has already been resolved to a valid value.
	tests := []struct {
		name    string
		config  *AuthConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "anonymous mode is valid",
			config: &AuthConfig{
				Mode: AuthModeAnonymous,
			},
			wantErr: false,
		},
		{
			name: "oauth mode requires oauth config",
			config: &AuthConfig{
				Mode: AuthModeOAuth,
			},
			wantErr: true,
			errMsg:  "auth.oauth is required",
		},
		{
			name: "oauth mode requires providers",
			config: &AuthConfig{
				Mode:  AuthModeOAuth,
				OAuth: &OAuthConfig{},
			},
			wantErr: true,
			errMsg:  "auth.oauth.providers is required",
		},
		{
			name: "oauth provider requires name",
			config: &AuthConfig{
				Mode: AuthModeOAuth,
				OAuth: &OAuthConfig{
					Providers: []OAuthProviderConfig{
						{
							IssuerURL: "https://example.com",
							Audience:  "api://test",
						},
					},
				},
			},
			wantErr: true,
			errMsg:  "name is required",
		},
		{
			name: "oauth provider requires issuerUrl",
			config: &AuthConfig{
				Mode: AuthModeOAuth,
				OAuth: &OAuthConfig{
					Providers: []OAuthProviderConfig{
						{
							Name:     "test",
							Audience: "api://test",
						},
					},
				},
			},
			wantErr: true,
			errMsg:  "issuerUrl is required",
		},
		{
			name: "oauth provider requires audience",
			config: &AuthConfig{
				Mode: AuthModeOAuth,
				OAuth: &OAuthConfig{
					Providers: []OAuthProviderConfig{
						{
							Name:      "test",
							IssuerURL: "https://example.com",
						},
					},
				},
			},
			wantErr: true,
			errMsg:  "audience is required",
		},
		{
			name: "oauth provider issuerUrl must be HTTPS",
			config: &AuthConfig{
				Mode: AuthModeOAuth,
				OAuth: &OAuthConfig{
					Providers: []OAuthProviderConfig{
						{
							Name:      "test",
							IssuerURL: "http://example.com",
							Audience:  "api://test",
						},
					},
				},
			},
			wantErr: true,
			errMsg:  "must use HTTPS",
		},
		{
			name: "oauth provider issuerUrl must be valid URL",
			config: &AuthConfig{
				Mode: AuthModeOAuth,
				OAuth: &OAuthConfig{
					Providers: []OAuthProviderConfig{
						{
							Name:      "test",
							IssuerURL: "not-a-valid-url",
							Audience:  "api://test",
						},
					},
				},
			},
			wantErr: true,
			errMsg:  "must be an absolute URL with host",
		},
		{
			name: "valid oauth configuration",
			config: &AuthConfig{
				Mode: AuthModeOAuth,
				OAuth: &OAuthConfig{
					Providers: []OAuthProviderConfig{
						{
							Name:      "kubernetes",
							IssuerURL: "https://kubernetes.default.svc.cluster.local",
							Audience:  "https://kubernetes.default.svc.cluster.local",
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid mode returns error",
			config: &AuthConfig{
				Mode: AuthMode("invalid"),
			},
			wantErr: true,
			errMsg:  "invalid auth.mode",
		},
		{
			name: "empty mode returns error (should be resolved before validation)",
			config: &AuthConfig{
				Mode: AuthMode(""),
			},
			wantErr: true,
			errMsg:  "invalid auth.mode",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Pass false for insecureAllowHTTP in these tests
			// Tests for insecure URL handling should use true
			err := tt.config.Validate(false)

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

func TestGetClientSecret(t *testing.T) {
	t.Parallel()

	t.Run("reads secret from file", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		secretFile := filepath.Join(tmpDir, "secret.txt")
		err := os.WriteFile(secretFile, []byte("  my-secret-value\n"), 0600)
		require.NoError(t, err)

		provider := &OAuthProviderConfig{
			Name:             "test",
			IssuerURL:        "https://example.com",
			ClientSecretFile: secretFile,
		}

		secret, err := provider.GetClientSecret()
		require.NoError(t, err)
		assert.Equal(t, "my-secret-value", secret)
	})

	t.Run("returns empty for no file configured", func(t *testing.T) {
		t.Parallel()

		provider := &OAuthProviderConfig{
			Name:      "test",
			IssuerURL: "https://example.com",
		}

		secret, err := provider.GetClientSecret()
		require.NoError(t, err)
		assert.Equal(t, "", secret)
	})

	t.Run("returns error for missing file", func(t *testing.T) {
		t.Parallel()

		provider := &OAuthProviderConfig{
			Name:             "test",
			IssuerURL:        "https://example.com",
			ClientSecretFile: "/nonexistent/secret.txt",
		}

		_, err := provider.GetClientSecret()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to read client secret")
	})
}

func TestRegistryConfig_GetType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		registryConf *RegistryConfig
		expectedType string
	}{
		{
			name: "git type",
			registryConf: &RegistryConfig{
				Name: "git-registry",
				Git: &GitConfig{
					Repository: "https://github.com/example/repo.git",
				},
			},
			expectedType: SourceTypeGit,
		},
		{
			name: "api type",
			registryConf: &RegistryConfig{
				Name: "api-registry",
				API: &APIConfig{
					Endpoint: "https://api.example.com",
				},
			},
			expectedType: SourceTypeAPI,
		},
		{
			name: "file type",
			registryConf: &RegistryConfig{
				Name: "file-registry",
				File: &FileConfig{
					Path: "/data/registry.json",
				},
			},
			expectedType: SourceTypeFile,
		},
		{
			name: "managed type",
			registryConf: &RegistryConfig{
				Name:    "managed-registry",
				Managed: &ManagedConfig{},
			},
			expectedType: SourceTypeManaged,
		},
		{
			name: "kubernetes type",
			registryConf: &RegistryConfig{
				Name:       "kubernetes-registry",
				Kubernetes: &KubernetesConfig{},
			},
			expectedType: SourceTypeKubernetes,
		},
		{
			name: "no type configured returns empty string",
			registryConf: &RegistryConfig{
				Name: "invalid-registry",
			},
			expectedType: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := tt.registryConf.GetType()
			assert.Equal(t, tt.expectedType, result)
		})
	}
}

func TestRegistryConfig_IsNonSyncedRegistry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		registryConf *RegistryConfig
		expected     bool
	}{
		{
			name: "git type is synced",
			registryConf: &RegistryConfig{
				Name: "git-registry",
				Git: &GitConfig{
					Repository: "https://github.com/example/repo.git",
				},
			},
			expected: false,
		},
		{
			name: "api type is synced",
			registryConf: &RegistryConfig{
				Name: "api-registry",
				API: &APIConfig{
					Endpoint: "https://api.example.com",
				},
			},
			expected: false,
		},
		{
			name: "file type is synced",
			registryConf: &RegistryConfig{
				Name: "file-registry",
				File: &FileConfig{
					Path: "/data/registry.json",
				},
			},
			expected: false,
		},
		{
			name: "managed type is non-synced",
			registryConf: &RegistryConfig{
				Name:    "managed-registry",
				Managed: &ManagedConfig{},
			},
			expected: true,
		},
		{
			name: "kubernetes type is non-synced",
			registryConf: &RegistryConfig{
				Name:       "kubernetes-registry",
				Kubernetes: &KubernetesConfig{},
			},
			expected: true,
		},
		{
			name: "no type configured is synced (needs validation to catch)",
			registryConf: &RegistryConfig{
				Name: "invalid-registry",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := tt.registryConf.IsNonSyncedRegistry()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDatabaseConfigPgpassDelegation(t *testing.T) {
	t.Parallel()

	dbConfig := &DatabaseConfig{
		Host:          "localhost",
		Port:          5432,
		User:          "testuser",
		MigrationUser: "migratoruser",
		Database:      "testdb",
	}

	// Test that password methods always return empty string (delegate to pgpass)
	password := dbConfig.GetPassword()
	assert.Equal(t, "", password, "GetPassword should delegate to pgpass")

	migrationPassword := dbConfig.GetMigrationPassword()
	assert.Equal(t, "", migrationPassword, "GetMigrationPassword should delegate to pgpass")

	// Verify connection string format (no password - pgpass will provide it)
	connStr := dbConfig.GetConnectionString()
	assert.Equal(t, "postgres://testuser@localhost:5432/testdb?sslmode=require", connStr)

	// Verify migration connection string uses migration user
	migrationConnStr := dbConfig.GetMigrationConnectionString()
	assert.Equal(t, "postgres://migratoruser@localhost:5432/testdb?sslmode=require", migrationConnStr)
}

// TestEnvPrefix verifies the environment variable prefix constant is correctly defined
func TestEnvPrefix(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "THV_REGISTRY", EnvPrefix, "EnvPrefix should be THV_REGISTRY")
}

// TestViperEnvOverrideRegistryName tests that THV_REGISTRY_REGISTRYNAME can override the registryName
// Note: Environment variable tests cannot be run in parallel because they share the same
// environment namespace, even when using different prefixes
func TestViperEnvOverrideRegistryName(t *testing.T) {
	// Create a temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	yamlContent := `registryName: original-name
registries:
  - name: test-registry
    file:
      path: /data/registry.json
    syncPolicy:
      interval: "30m"
auth:
  mode: anonymous
`
	err := os.WriteFile(configPath, []byte(yamlContent), 0600)
	require.NoError(t, err)

	// Set environment variable override
	t.Setenv("THV_REGISTRY_REGISTRYNAME", "overridden-name")

	// Load config
	cfg, err := LoadConfig(WithConfigPath(configPath))
	require.NoError(t, err)

	// Verify the environment variable override took effect
	assert.Equal(t, "overridden-name", cfg.RegistryName)
}

// TestViperEnvOverrideDatabaseHost tests that THV_REGISTRY_DATABASE_HOST can override database.host
func TestViperEnvOverrideDatabaseHost(t *testing.T) {
	// Create a temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	yamlContent := `registries:
  - name: test-registry
    file:
      path: /data/registry.json
    syncPolicy:
      interval: "30m"
auth:
  mode: anonymous
database:
  host: original-host
  port: 5432
  user: testuser
  database: testdb
`
	err := os.WriteFile(configPath, []byte(yamlContent), 0600)
	require.NoError(t, err)

	// Set environment variable override
	t.Setenv("THV_REGISTRY_DATABASE_HOST", "overridden-host")

	// Load config
	cfg, err := LoadConfig(WithConfigPath(configPath))
	require.NoError(t, err)

	// Verify the environment variable override took effect
	require.NotNil(t, cfg.Database)
	assert.Equal(t, "overridden-host", cfg.Database.Host)
}

// TestViperEnvOverrideDatabasePort tests that THV_REGISTRY_DATABASE_PORT can override database.port
func TestViperEnvOverrideDatabasePort(t *testing.T) {
	// Create a temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	yamlContent := `registries:
  - name: test-registry
    file:
      path: /data/registry.json
    syncPolicy:
      interval: "30m"
auth:
  mode: anonymous
database:
  host: localhost
  port: 5432
  user: testuser
  database: testdb
`
	err := os.WriteFile(configPath, []byte(yamlContent), 0600)
	require.NoError(t, err)

	// Set environment variable override
	t.Setenv("THV_REGISTRY_DATABASE_PORT", "5433")

	// Load config
	cfg, err := LoadConfig(WithConfigPath(configPath))
	require.NoError(t, err)

	// Verify the environment variable override took effect
	require.NotNil(t, cfg.Database)
	assert.Equal(t, 5433, cfg.Database.Port)
}

// TestViperEnvOverrideAuthMode tests that THV_REGISTRY_AUTH_MODE can override auth.mode
func TestViperEnvOverrideAuthMode(t *testing.T) {
	// Create a temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	yamlContent := `registries:
  - name: test-registry
    file:
      path: /data/registry.json
    syncPolicy:
      interval: "30m"
auth:
  mode: oauth
  oauth:
    providers:
      - name: test
        issuerUrl: https://example.com
        audience: api://test
`
	err := os.WriteFile(configPath, []byte(yamlContent), 0600)
	require.NoError(t, err)

	// Set environment variable override to anonymous
	t.Setenv("THV_REGISTRY_AUTH_MODE", "anonymous")

	// Load config
	cfg, err := LoadConfig(WithConfigPath(configPath))
	require.NoError(t, err)

	// Verify the environment variable override took effect
	require.NotNil(t, cfg.Auth)
	assert.Equal(t, AuthModeAnonymous, cfg.Auth.Mode)
}

// TestViperConfigPrecedence tests that environment variables take precedence over config file values
func TestViperConfigPrecedence(t *testing.T) {
	// Create a temporary config file with all values set
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	yamlContent := `registryName: file-registry-name
registries:
  - name: test-registry
    file:
      path: /data/registry.json
    syncPolicy:
      interval: "30m"
auth:
  mode: anonymous
database:
  host: file-host
  port: 5432
  user: file-user
  database: file-db
`
	err := os.WriteFile(configPath, []byte(yamlContent), 0600)
	require.NoError(t, err)

	// Set multiple environment variable overrides
	t.Setenv("THV_REGISTRY_REGISTRYNAME", "env-registry-name")
	t.Setenv("THV_REGISTRY_DATABASE_HOST", "env-host")
	t.Setenv("THV_REGISTRY_DATABASE_USER", "env-user")

	// Load config
	cfg, err := LoadConfig(WithConfigPath(configPath))
	require.NoError(t, err)

	// Verify that environment variable overrides took precedence
	assert.Equal(t, "env-registry-name", cfg.RegistryName)
	require.NotNil(t, cfg.Database)
	assert.Equal(t, "env-host", cfg.Database.Host)
	assert.Equal(t, "env-user", cfg.Database.User)

	// Values not overridden should keep their file values
	assert.Equal(t, 5432, cfg.Database.Port)
	assert.Equal(t, "file-db", cfg.Database.Database)
}

// TestViperValidationStillWorks tests that validation errors still occur even with Viper
func TestViperValidationStillWorks(t *testing.T) {
	t.Parallel()
	// Create a temporary config file with invalid config (missing registries)
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	yamlContent := `registryName: test
registries: []
auth:
  mode: anonymous
`
	err := os.WriteFile(configPath, []byte(yamlContent), 0600)
	require.NoError(t, err)

	// Load config - should fail validation
	_, err = LoadConfig(WithConfigPath(configPath))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one registry must be configured")
}

// TestViperInvalidYAML tests that invalid YAML still returns an error
func TestViperInvalidYAML(t *testing.T) {
	t.Parallel()
	// Create a temporary config file with invalid YAML
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	yamlContent := `registries: [invalid yaml`
	err := os.WriteFile(configPath, []byte(yamlContent), 0600)
	require.NoError(t, err)

	// Load config - should fail to parse
	_, err = LoadConfig(WithConfigPath(configPath))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read config file")
}

// TestViperConfigFileNotFound tests error handling when config file doesn't exist
func TestViperConfigFileNotFound(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "nonexistent.yaml")

	// Load config - should fail to find file
	_, err := LoadConfig(WithConfigPath(configPath))
	require.Error(t, err)
	// The error should indicate the file couldn't be read
	assert.Contains(t, err.Error(), "failed to evaluate symlinks")
}
