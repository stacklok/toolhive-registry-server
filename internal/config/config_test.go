package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/aws/smithy-go/ptr"
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
			yamlContent: `sources:
  - name: test-registry
    type: file
    file:
      path: /data/registry.json
    syncPolicy:
      interval: "30m"
    filter:
      tags:
        include: ["database", "production"]
        exclude: ["experimental", "deprecated", "beta"]
registries:
  - name: default
    sources: ["test-registry"]
database:
  host: localhost
  port: 5432
  user: testuser
  database: testdb
auth:
  mode: anonymous
`,
			wantConfig: &Config{
				Sources: []SourceConfig{
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
				Registries: []RegistryConfig{
					{Name: "default", Sources: []string{"test-registry"}},
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
		{
			name: "minimal_config",
			yamlContent: `sources:
  - name: minimal-registry
    type: file
    file:
      path: /data/registry.json
    syncPolicy:
      interval: "1h"
    filter:
      tags:
        include: []
        exclude: []
registries:
  - name: default
    sources: ["minimal-registry"]
database:
  host: localhost
  port: 5432
  user: testuser
  database: testdb
auth:
  mode: anonymous
`,
			wantConfig: &Config{
				Sources: []SourceConfig{
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
				Registries: []RegistryConfig{
					{Name: "default", Sources: []string{"minimal-registry"}},
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
		{
			name: "config_with_only_include_tags",
			yamlContent: `sources:
  - name: include-tags-registry
    type: file
    file:
      path: /data/registry.json
    syncPolicy:
      interval: "15m"
    filter:
      tags:
        include: ["api", "backend", "frontend"]
registries:
  - name: default
    sources: ["include-tags-registry"]
database:
  host: localhost
  port: 5432
  user: testuser
  database: testdb
auth:
  mode: anonymous
`,
			wantConfig: &Config{
				Sources: []SourceConfig{
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
				Registries: []RegistryConfig{
					{Name: "default", Sources: []string{"include-tags-registry"}},
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
		{
			name: "config_with_only_exclude_tags",
			yamlContent: `sources:
  - name: exclude-tags-registry
    type: file
    file:
      path: /data/registry.json
    syncPolicy:
      interval: "5m"
    filter:
      tags:
        exclude: ["test", "debug", "experimental"]
registries:
  - name: default
    sources: ["exclude-tags-registry"]
database:
  host: localhost
  port: 5432
  user: testuser
  database: testdb
auth:
  mode: anonymous
`,
			wantConfig: &Config{
				Sources: []SourceConfig{
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
				Registries: []RegistryConfig{
					{Name: "default", Sources: []string{"exclude-tags-registry"}},
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
		{
			name:        "invalid_yaml",
			yamlContent: `sources: [invalid yaml`,
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
		Sources: []SourceConfig{
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
		Registries: []RegistryConfig{
			{Name: "default", Sources: []string{"test-registry"}},
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
	}

	// Create a temporary directory and file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test-config.yaml")

	// Write the config using YAML
	yamlContent := `sources:
  - name: test-registry
    type: file
    file:
      path: /data/registry.json
    syncPolicy:
      interval: "45m"
    filter:
      tags:
        include: ["prod", "stable"]
        exclude: ["beta", "alpha"]
registries:
  - name: default
    sources: ["test-registry"]
database:
  host: localhost
  port: 5432
  user: testuser
  database: testdb
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
				Sources: []SourceConfig{
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
				Registries: []RegistryConfig{
					{Name: "default", Sources: []string{"test-registry"}},
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
		{
			name: "missing_registry_name",
			config: &Config{
				Sources: []SourceConfig{
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
			name: "missing_source_type",
			config: &Config{
				Sources: []SourceConfig{
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
				Sources: []SourceConfig{
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
				Sources: []SourceConfig{
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
				Sources: []SourceConfig{
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
				Registries: []RegistryConfig{
					{Name: "default", Sources: []string{"test-registry"}},
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
		{
			name: "valid_file_url_with_timeout",
			config: &Config{
				Sources: []SourceConfig{
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
				Registries: []RegistryConfig{
					{Name: "default", Sources: []string{"test-registry"}},
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
		{
			name: "invalid_file_url_timeout",
			config: &Config{
				Sources: []SourceConfig{
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
				Sources: []SourceConfig{
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
				Sources: []SourceConfig{
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
				Sources: []SourceConfig{
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
				Sources: []SourceConfig{
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
				Sources: []SourceConfig{},
				Auth: &AuthConfig{
					Mode: AuthModeAnonymous,
				},
			},
			wantErr: true,
			errMsg:  "at least one source must be configured",
		},
		{
			name: "valid_file_source",
			config: &Config{
				Sources: []SourceConfig{
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
				Registries: []RegistryConfig{
					{Name: "default", Sources: []string{"test-registry"}},
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
		{
			name: "duplicate_registry_names",
			config: &Config{
				Sources: []SourceConfig{
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
			errMsg:  "duplicate source name",
		},
		{
			name: "multiple_managed_sources_rejected",
			config: &Config{
				Sources: []SourceConfig{
					{
						Name:    "managed-1",
						Managed: &ManagedConfig{},
					},
					{
						Name:    "managed-2",
						Managed: &ManagedConfig{},
					},
				},
				Registries: []RegistryConfig{
					{Name: "default", Sources: []string{"managed-1", "managed-2"}},
				},
				Auth: &AuthConfig{
					Mode: AuthModeAnonymous,
				},
			},
			wantErr: true,
			errMsg:  "at most one managed source is allowed",
		},
		{
			name: "invalid_format_when_using_api",
			config: &Config{
				Sources: []SourceConfig{
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
				Sources: []SourceConfig{
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
				Sources: []SourceConfig{
					{
						Name:    "managed-registry",
						Managed: &ManagedConfig{},
						// No SyncPolicy required for managed registries
					},
				},
				Registries: []RegistryConfig{
					{Name: "default", Sources: []string{"managed-registry"}},
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
		{
			name: "valid_managed_registry_with_ignored_sync_policy",
			config: &Config{
				Sources: []SourceConfig{
					{
						Name:    "managed-registry",
						Managed: &ManagedConfig{},
						// SyncPolicy is silently ignored for managed registries
						SyncPolicy: &SyncPolicyConfig{
							Interval: "30m",
						},
					},
				},
				Registries: []RegistryConfig{
					{Name: "default", Sources: []string{"managed-registry"}},
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
		{
			name: "valid_managed_registry_with_ignored_filter",
			config: &Config{
				Sources: []SourceConfig{
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
				Registries: []RegistryConfig{
					{Name: "default", Sources: []string{"managed-registry"}},
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
		{
			name: "valid_kubernetes_registry_no_sync_policy",
			config: &Config{
				Sources: []SourceConfig{
					{
						Name:       "kubernetes-registry",
						Kubernetes: &KubernetesConfig{},
						// No SyncPolicy required for kubernetes registries
					},
				},
				Registries: []RegistryConfig{
					{Name: "default", Sources: []string{"kubernetes-registry"}},
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
		{
			name: "valid_kubernetes_registry_with_ignored_sync_policy",
			config: &Config{
				Sources: []SourceConfig{
					{
						Name:       "kubernetes-registry",
						Kubernetes: &KubernetesConfig{},
						// SyncPolicy is silently ignored for kubernetes registries
						SyncPolicy: &SyncPolicyConfig{
							Interval: "30m",
						},
					},
				},
				Registries: []RegistryConfig{
					{Name: "default", Sources: []string{"kubernetes-registry"}},
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
		{
			name: "valid_kubernetes_registry_with_ignored_filter",
			config: &Config{
				Sources: []SourceConfig{
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
				Registries: []RegistryConfig{
					{Name: "default", Sources: []string{"kubernetes-registry"}},
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
		{
			name: "managed_and_file_source_specified",
			config: &Config{
				Sources: []SourceConfig{
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
				Sources: []SourceConfig{
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
				Sources: []SourceConfig{
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
				Sources: []SourceConfig{
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
				Registries: []RegistryConfig{
					{Name: "default", Sources: []string{"file-registry", "managed-registry", "git-registry"}},
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
		{
			name: "valid_config_with_database_storage",
			config: &Config{
				Sources: []SourceConfig{
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
				Registries: []RegistryConfig{
					{Name: "default", Sources: []string{"test-registry"}},
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
		{
			name: "missing_database_configuration",
			config: &Config{
				Sources: []SourceConfig{
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
				Registries: []RegistryConfig{
					{Name: "default", Sources: []string{"test-registry"}},
				},
				Auth: &AuthConfig{
					Mode: AuthModeAnonymous,
				},
			},
			wantErr: true,
			errMsg:  "database configuration is required",
		},
		{
			name: "database_storage_missing_host",
			config: &Config{
				Sources: []SourceConfig{
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
				Registries: []RegistryConfig{
					{Name: "default", Sources: []string{"test-registry"}},
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
				Sources: []SourceConfig{
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
				Registries: []RegistryConfig{
					{Name: "default", Sources: []string{"test-registry"}},
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
				Sources: []SourceConfig{
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
				Registries: []RegistryConfig{
					{Name: "default", Sources: []string{"test-registry"}},
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
				Sources: []SourceConfig{
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
				Registries: []RegistryConfig{
					{Name: "default", Sources: []string{"test-registry"}},
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

// TestGetRegistryName removed - GetRegistryName and RegistryName field have been removed

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
			yamlContent: `sources:
  - name: test-registry
    type: file
    file:
      path: /data/registry.json
    syncPolicy:
      interval: "30m"
registries:
  - name: default
    sources: ["test-registry"]
auth:
  mode: anonymous
database:
  host: localhost
  port: 5432
  user: testuser
  database: testdb`,
			wantConfig: &Config{
				Sources: []SourceConfig{
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
				Registries: []RegistryConfig{
					{Name: "default", Sources: []string{"test-registry"}},
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
			yamlContent: `sources:
  - name: production-registry
    type: file
    file:
      path: /data/registry.json
    syncPolicy:
      interval: "1h"
registries:
  - name: default
    sources: ["production-registry"]
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
				Sources: []SourceConfig{
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
				Registries: []RegistryConfig{
					{Name: "default", Sources: []string{"production-registry"}},
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
			yamlContent: `sources:
  - name: simple-registry
    type: file
    file:
      path: /data/registry.json
    syncPolicy:
      interval: "30m"
registries:
  - name: default
    sources: ["simple-registry"]
auth:
  mode: anonymous
`,
			wantConfig: nil,
			wantErr:    true,
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
			yaml: `sources:
  - name: test-registry
    type: file
    file:
      path: /data/registry.json
    syncPolicy:
      interval: "30m"
registries:
  - name: default
    sources: ["test-registry"]`,
			check: nil,
		},
		{
			name: "oauth with providers is parsed correctly",
			yaml: `sources:
  - name: test-registry
    type: file
    file:
      path: /data/registry.json
    syncPolicy:
      interval: "30m"
registries:
  - name: default
    sources: ["test-registry"]
database:
  host: localhost
  port: 5432
  user: testuser
  database: testdb
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
			name: "oauth provider with introspection url",
			yaml: `sources:
  - name: test-registry
    type: file
    file:
      path: /data/registry.json
    syncPolicy:
      interval: "30m"
registries:
  - name: default
    sources: ["test-registry"]
database:
  host: localhost
  port: 5432
  user: testuser
  database: testdb
auth:
  mode: oauth
  oauth:
    resourceUrl: https://registry.example.com
    providers:
      - name: google
        issuerUrl: https://accounts.google.com
        audience: my-google-client-id
        introspectionUrl: https://oauth2.googleapis.com/tokeninfo`,
			//nolint:thelper // We want to see errors here
			check: func(t *testing.T, cfg *Config) {
				require.NotNil(t, cfg.Auth)
				assert.Equal(t, AuthModeOAuth, cfg.Auth.Mode)
				require.NotNil(t, cfg.Auth.OAuth)
				require.Len(t, cfg.Auth.OAuth.Providers, 1)
				p := cfg.Auth.OAuth.Providers[0]
				assert.Equal(t, "google", p.Name)
				assert.Equal(t, "https://accounts.google.com", p.IssuerURL)
				assert.Equal(t, "my-google-client-id", p.Audience)
				assert.Equal(t, "https://oauth2.googleapis.com/tokeninfo", p.IntrospectionURL)
			},
		},
		{
			name: "explicit anonymous mode is parsed correctly",
			yaml: `sources:
  - name: test-registry
    type: file
    file:
      path: /data/registry.json
    syncPolicy:
      interval: "30m"
registries:
  - name: default
    sources: ["test-registry"]
database:
  host: localhost
  port: 5432
  user: testuser
  database: testdb
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
			yaml: `sources:
  - name: test-registry
    type: file
    file:
      path: /data/registry.json
    syncPolicy:
      interval: "30m"
registries:
  - name: default
    sources: ["test-registry"]
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
		registryConf *SourceConfig
		expectedType SourceType
	}{
		{
			name: "git type",
			registryConf: &SourceConfig{
				Name: "git-registry",
				Git: &GitConfig{
					Repository: "https://github.com/example/repo.git",
				},
			},
			expectedType: SourceTypeGit,
		},
		{
			name: "api type",
			registryConf: &SourceConfig{
				Name: "api-registry",
				API: &APIConfig{
					Endpoint: "https://api.example.com",
				},
			},
			expectedType: SourceTypeAPI,
		},
		{
			name: "file type",
			registryConf: &SourceConfig{
				Name: "file-registry",
				File: &FileConfig{
					Path: "/data/registry.json",
				},
			},
			expectedType: SourceTypeFile,
		},
		{
			name: "managed type",
			registryConf: &SourceConfig{
				Name:    "managed-registry",
				Managed: &ManagedConfig{},
			},
			expectedType: SourceTypeManaged,
		},
		{
			name: "kubernetes type",
			registryConf: &SourceConfig{
				Name:       "kubernetes-registry",
				Kubernetes: &KubernetesConfig{},
			},
			expectedType: SourceTypeKubernetes,
		},
		{
			name: "no type configured returns empty string",
			registryConf: &SourceConfig{
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

func TestRegistryConfig_IsNonSyncedSource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		registryConf *SourceConfig
		expected     bool
	}{
		{
			name: "git type is synced",
			registryConf: &SourceConfig{
				Name: "git-registry",
				Git: &GitConfig{
					Repository: "https://github.com/example/repo.git",
				},
			},
			expected: false,
		},
		{
			name: "api type is synced",
			registryConf: &SourceConfig{
				Name: "api-registry",
				API: &APIConfig{
					Endpoint: "https://api.example.com",
				},
			},
			expected: false,
		},
		{
			name: "file type is synced",
			registryConf: &SourceConfig{
				Name: "file-registry",
				File: &FileConfig{
					Path: "/data/registry.json",
				},
			},
			expected: false,
		},
		{
			name: "managed type is non-synced",
			registryConf: &SourceConfig{
				Name:    "managed-registry",
				Managed: &ManagedConfig{},
			},
			expected: true,
		},
		{
			name: "kubernetes type is non-synced",
			registryConf: &SourceConfig{
				Name:       "kubernetes-registry",
				Kubernetes: &KubernetesConfig{},
			},
			expected: true,
		},
		{
			name: "no type configured is synced (needs validation to catch)",
			registryConf: &SourceConfig{
				Name: "invalid-registry",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := tt.registryConf.IsNonSyncedSource()
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

// TestViperEnvOverrideRegistryName removed - RegistryName field has been removed

// TestViperEnvOverrideDatabaseHost tests that THV_REGISTRY_DATABASE_HOST can override database.host
func TestViperEnvOverrideDatabaseHost(t *testing.T) {
	// Create a temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	yamlContent := `sources:
  - name: test-registry
    type: file
    file:
      path: /data/registry.json
    syncPolicy:
      interval: "30m"
registries:
  - name: default
    sources: ["test-registry"]
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
	yamlContent := `sources:
  - name: test-registry
    type: file
    file:
      path: /data/registry.json
    syncPolicy:
      interval: "30m"
registries:
  - name: default
    sources: ["test-registry"]
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
	yamlContent := `sources:
  - name: test-registry
    type: file
    file:
      path: /data/registry.json
    syncPolicy:
      interval: "30m"
registries:
  - name: default
    sources: ["test-registry"]
database:
  host: localhost
  port: 5432
  user: testuser
  database: testdb
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
	yamlContent := `
sources:
  - name: test-registry
    type: file
    file:
      path: /data/registry.json
    syncPolicy:
      interval: "30m"
registries:
  - name: default
    sources: ["test-registry"]
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
	t.Setenv("THV_REGISTRY_DATABASE_HOST", "env-host")
	t.Setenv("THV_REGISTRY_DATABASE_USER", "env-user")

	// Load config
	cfg, err := LoadConfig(WithConfigPath(configPath))
	require.NoError(t, err)

	// Verify that environment variable overrides took precedence
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
	yamlContent := `
sources: []
auth:
  mode: anonymous
`
	err := os.WriteFile(configPath, []byte(yamlContent), 0600)
	require.NoError(t, err)

	// Load config - should fail validation
	_, err = LoadConfig(WithConfigPath(configPath))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one source must be configured")
}

// TestViperInvalidYAML tests that invalid YAML still returns an error
func TestViperInvalidYAML(t *testing.T) {
	t.Parallel()
	// Create a temporary config file with invalid YAML
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	yamlContent := `sources: [invalid yaml`
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

// TestOAuthProviderAllowPrivateIP tests that allowPrivateIP is correctly parsed for OAuth providers
func TestOAuthProviderAllowPrivateIP(t *testing.T) {
	t.Parallel()

	yaml := `sources:
  - name: test-registry
    type: file
    file:
      path: /data/registry.json
    syncPolicy:
      interval: "30m"
registries:
  - name: default
    sources: ["test-registry"]
database:
  host: localhost
  port: 5432
  user: testuser
  database: testdb
auth:
  mode: oauth
  oauth:
    providers:
      - name: kubernetes
        issuerUrl: https://kubernetes.default.svc
        audience: https://kubernetes.default.svc
        allowPrivateIP: true`

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	err := os.WriteFile(configPath, []byte(yaml), 0600)
	require.NoError(t, err)

	cfg, err := LoadConfig(WithConfigPath(configPath))
	require.NoError(t, err)
	require.NotNil(t, cfg.Auth)
	require.NotNil(t, cfg.Auth.OAuth)
	require.Len(t, cfg.Auth.OAuth.Providers, 1)
	assert.True(t, cfg.Auth.OAuth.Providers[0].AllowPrivateIP)
}

// TestOAuthProviderJwksUrl tests that jwksUrl is correctly parsed for OAuth providers
func TestOAuthProviderJwksUrl(t *testing.T) {
	t.Parallel()

	yaml := `sources:
  - name: test-registry
    type: file
    file:
      path: /data/registry.json
    syncPolicy:
      interval: "30m"
registries:
  - name: default
    sources: ["test-registry"]
database:
  host: localhost
  port: 5432
  user: testuser
  database: testdb
auth:
  mode: oauth
  oauth:
    providers:
      - name: kubernetes
        issuerUrl: https://kubernetes.default.svc
        jwksUrl: https://kubernetes.default.svc/openid/v1/jwks
        audience: https://kubernetes.default.svc`

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	err := os.WriteFile(configPath, []byte(yaml), 0600)
	require.NoError(t, err)

	cfg, err := LoadConfig(WithConfigPath(configPath))
	require.NoError(t, err)
	require.NotNil(t, cfg.Auth)
	require.NotNil(t, cfg.Auth.OAuth)
	require.Len(t, cfg.Auth.OAuth.Providers, 1)
	assert.Equal(t, "https://kubernetes.default.svc/openid/v1/jwks", cfg.Auth.OAuth.Providers[0].JwksUrl)
}

// TestOAuthProviderAuthTokenFile tests that authTokenFile is correctly parsed for OAuth providers
func TestOAuthProviderAuthTokenFile(t *testing.T) {
	t.Parallel()

	yaml := `sources:
  - name: test-registry
    type: file
    file:
      path: /data/registry.json
    syncPolicy:
      interval: "30m"
registries:
  - name: default
    sources: ["test-registry"]
database:
  host: localhost
  port: 5432
  user: testuser
  database: testdb
auth:
  mode: oauth
  oauth:
    providers:
      - name: kubernetes
        issuerUrl: https://kubernetes.default.svc
        audience: https://kubernetes.default.svc
        authTokenFile: /var/run/secrets/kubernetes.io/serviceaccount/token`

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	err := os.WriteFile(configPath, []byte(yaml), 0600)
	require.NoError(t, err)

	cfg, err := LoadConfig(WithConfigPath(configPath))
	require.NoError(t, err)
	require.NotNil(t, cfg.Auth)
	require.NotNil(t, cfg.Auth.OAuth)
	require.Len(t, cfg.Auth.OAuth.Providers, 1)
	assert.Equal(t, "/var/run/secrets/kubernetes.io/serviceaccount/token", cfg.Auth.OAuth.Providers[0].AuthTokenFile)
}

// TestGitAuthConfigGetPassword tests the GetPassword() method of GitAuthConfig
func TestGitAuthConfigGetPassword(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		auth        *GitAuthConfig
		setupFile   func(t *testing.T) string
		wantPass    string
		wantErr     bool
		errContains string
	}{
		{
			name: "reads password from file with whitespace trimming",
			auth: &GitAuthConfig{
				Username: "testuser",
			},
			setupFile: func(t *testing.T) string {
				t.Helper()
				tmpDir := t.TempDir()
				passwordFile := filepath.Join(tmpDir, "password.txt")
				err := os.WriteFile(passwordFile, []byte("  my-secret-password\n\t  "), 0600)
				require.NoError(t, err)
				return passwordFile
			},
			wantPass: "my-secret-password",
			wantErr:  false,
		},
		{
			name:     "nil receiver returns empty string",
			auth:     nil,
			wantPass: "",
			wantErr:  false,
		},
		{
			name: "missing file returns error",
			auth: &GitAuthConfig{
				Username:     "testuser",
				PasswordFile: "/nonexistent/path/to/password.txt",
			},
			wantPass:    "",
			wantErr:     true,
			errContains: "failed to read git password",
		},
		{
			name: "empty password file path returns empty string",
			auth: &GitAuthConfig{
				Username:     "testuser",
				PasswordFile: "",
			},
			wantPass: "",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Set up password file if needed
			if tt.setupFile != nil && tt.auth != nil {
				tt.auth.PasswordFile = tt.setupFile(t)
			}

			password, err := tt.auth.GetPassword()

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantPass, password)
			}
		})
	}
}

// TestConfigValidateGitAuth tests git auth validation in TestConfigValidate
func TestConfigValidateGitAuth(t *testing.T) {
	t.Parallel()

	// Create a temporary password file for valid auth tests
	tmpDir := t.TempDir()
	passwordFile := filepath.Join(tmpDir, "password.txt")
	err := os.WriteFile(passwordFile, []byte("secret"), 0600)
	require.NoError(t, err)

	tests := []struct {
		name    string
		config  *Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid git config with auth (both username and passwordFile set)",
			config: &Config{
				Sources: []SourceConfig{
					{
						Name: "git-registry",
						Git: &GitConfig{
							Repository: "https://github.com/example/repo.git",
							Auth: &GitAuthConfig{
								Username:     "testuser",
								PasswordFile: passwordFile,
							},
						},
						SyncPolicy: &SyncPolicyConfig{
							Interval: "30m",
						},
					},
				},
				Registries: []RegistryConfig{
					{Name: "default", Sources: []string{"git-registry"}},
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
		{
			name: "git.auth.username only (missing passwordFile) should fail",
			config: &Config{
				Sources: []SourceConfig{
					{
						Name: "git-registry",
						Git: &GitConfig{
							Repository: "https://github.com/example/repo.git",
							Auth: &GitAuthConfig{
								Username: "testuser",
							},
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
			errMsg:  "git.auth.username and git.auth.passwordFile must both be specified",
		},
		{
			name: "git.auth.passwordFile only (missing username) should fail",
			config: &Config{
				Sources: []SourceConfig{
					{
						Name: "git-registry",
						Git: &GitConfig{
							Repository: "https://github.com/example/repo.git",
							Auth: &GitAuthConfig{
								PasswordFile: passwordFile,
							},
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
			errMsg:  "git.auth.username and git.auth.passwordFile must both be specified",
		},
		{
			name: "git.auth.passwordFile with relative path should fail",
			config: &Config{
				Sources: []SourceConfig{
					{
						Name: "git-registry",
						Git: &GitConfig{
							Repository: "https://github.com/example/repo.git",
							Auth: &GitAuthConfig{
								Username:     "testuser",
								PasswordFile: "relative/path/to/password.txt",
							},
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
			errMsg:  "git.auth.passwordFile must be an absolute path",
		},
		{
			name: "git config without auth is valid",
			config: &Config{
				Sources: []SourceConfig{
					{
						Name: "git-registry",
						Git: &GitConfig{
							Repository: "https://github.com/example/repo.git",
						},
						SyncPolicy: &SyncPolicyConfig{
							Interval: "30m",
						},
					},
				},
				Registries: []RegistryConfig{
					{Name: "default", Sources: []string{"git-registry"}},
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
		{
			name: "git config with empty auth struct is valid",
			config: &Config{
				Sources: []SourceConfig{
					{
						Name: "git-registry",
						Git: &GitConfig{
							Repository: "https://github.com/example/repo.git",
							Auth:       &GitAuthConfig{},
						},
						SyncPolicy: &SyncPolicyConfig{
							Interval: "30m",
						},
					},
				},
				Registries: []RegistryConfig{
					{Name: "default", Sources: []string{"git-registry"}},
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

func TestDatabaseConfigGetMaxMetaSize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		dbConfig *DatabaseConfig
		want     int
	}{
		{
			name:     "nil DatabaseConfig returns default",
			dbConfig: nil,
			want:     DefaultMaxMetaSize,
		},
		{
			name:     "MaxMetaSize not set returns default",
			dbConfig: &DatabaseConfig{},
			want:     DefaultMaxMetaSize,
		},
		{
			name: "MaxMetaSize set to custom value",
			dbConfig: &DatabaseConfig{
				MaxMetaSize: ptr.Int(4096),
			},
			want: 4096,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.dbConfig.GetMaxMetaSize()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDatabaseConfigBuildConnectionStringWithAuth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		dbConfig    *DatabaseConfig
		user        string
		password    string
		wantConnStr string
	}{
		{
			name: "empty password produces passwordless URL",
			dbConfig: &DatabaseConfig{
				Host:     "localhost",
				Port:     5432,
				Database: "testdb",
			},
			user:        "appuser",
			password:    "",
			wantConnStr: "postgres://appuser@localhost:5432/testdb?sslmode=require",
		},
		{
			name: "non-empty password is embedded in URL",
			dbConfig: &DatabaseConfig{
				Host:     "localhost",
				Port:     5432,
				Database: "testdb",
			},
			user:        "appuser",
			password:    "secret123",
			wantConnStr: "postgres://appuser:secret123@localhost:5432/testdb?sslmode=require",
		},
		{
			name: "password with special characters is URL-escaped",
			dbConfig: &DatabaseConfig{
				Host:     "localhost",
				Port:     5432,
				Database: "testdb",
			},
			user:     "appuser",
			password: "p@ss/word=",
			// url.UserPassword escapes @ and / but not =
			wantConnStr: "postgres://appuser:p%40ss%2Fword=@localhost:5432/testdb?sslmode=require",
		},
		{
			name: "custom sslmode is respected",
			dbConfig: &DatabaseConfig{
				Host:     "db.example.com",
				Port:     5433,
				Database: "production",
				SSLMode:  "verify-full",
			},
			user:        "migratoruser",
			password:    "token",
			wantConnStr: "postgres://migratoruser:token@db.example.com:5433/production?sslmode=verify-full",
		},
		{
			name: "migration user differs from app user",
			dbConfig: &DatabaseConfig{
				Host:          "localhost",
				Port:          5432,
				User:          "appuser",
				MigrationUser: "migratoruser",
				Database:      "testdb",
			},
			user:        "migratoruser",
			password:    "",
			wantConnStr: "postgres://migratoruser@localhost:5432/testdb?sslmode=require",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			connStr := tt.dbConfig.BuildConnectionStringWithAuth(tt.user, tt.password)
			assert.Equal(t, tt.wantConnStr, connStr)
		})
	}
}

func TestValidateStorageConfigRejectsInvalidMaxMetaSize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		maxMetaSize int
	}{
		{name: "zero", maxMetaSize: 0},
		{name: "negative", maxMetaSize: -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := &Config{
				Database: &DatabaseConfig{
					Host:        "localhost",
					Port:        5432,
					User:        "test",
					Database:    "testdb",
					MaxMetaSize: ptr.Int(tt.maxMetaSize),
				},
			}
			err := cfg.validateStorageConfig()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "database.maxMetaSize must be greater than zero")
		})
	}
}
