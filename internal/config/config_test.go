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
			name: "invalid_format_when_type_is_api",
			config: &Config{
				Source: SourceConfig{
					Type:   "api",
					Format: "toolhive",
					API: &APIConfig{
						Endpoint: "http://example.com",
					},
				},
			},
			wantErr: true,
			errMsg:  "source.format must be either empty or upstream",
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
		{
			name: "valid_config_with_file_storage",
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
				FileStorage: &FileStorageConfig{
					BaseDir: "/custom/data",
				},
			},
			wantErr: false,
		},
		{
			name: "valid_config_with_database_storage",
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
				Database: &DatabaseConfig{
					Host:     "localhost",
					Port:     5432,
					User:     "testuser",
					Database: "testdb",
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
				Source: SourceConfig{
					Type: "file",
					File: &FileConfig{
						Path: "/data/registry.json",
					},
				},
				SyncPolicy: &SyncPolicyConfig{
					Interval: "30m",
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
				Source: SourceConfig{
					Type: "file",
					File: &FileConfig{
						Path: "/data/registry.json",
					},
				},
				SyncPolicy: &SyncPolicyConfig{
					Interval: "30m",
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
				Source: SourceConfig{
					Type: "file",
					File: &FileConfig{
						Path: "/data/registry.json",
					},
				},
				SyncPolicy: &SyncPolicyConfig{
					Interval: "30m",
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
				Source: SourceConfig{
					Type: "file",
					File: &FileConfig{
						Path: "/data/registry.json",
					},
				},
				SyncPolicy: &SyncPolicyConfig{
					Interval: "30m",
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

	tests := []struct {
		name         string
		dbConfig     *DatabaseConfig
		setupFile    func(t *testing.T) string
		wantPassword string
		wantErr      bool
		errMsg       string
	}{
		{
			name: "password_from_file",
			dbConfig: &DatabaseConfig{
				Host:     "localhost",
				Port:     5432,
				User:     "testuser",
				Database: "testdb",
			},
			setupFile: func(t *testing.T) string {
				t.Helper()
				tmpDir := t.TempDir()
				passwordFile := filepath.Join(tmpDir, "password.txt")
				err := os.WriteFile(passwordFile, []byte("mypassword"), 0600)
				require.NoError(t, err)
				return passwordFile
			},
			wantPassword: "mypassword",
			wantErr:      false,
		},
		{
			name: "password_from_file_with_whitespace",
			dbConfig: &DatabaseConfig{
				Host:     "localhost",
				Port:     5432,
				User:     "testuser",
				Database: "testdb",
			},
			setupFile: func(t *testing.T) string {
				t.Helper()
				tmpDir := t.TempDir()
				passwordFile := filepath.Join(tmpDir, "password.txt")
				err := os.WriteFile(passwordFile, []byte("  mypassword\n\t"), 0600)
				require.NoError(t, err)
				return passwordFile
			},
			wantPassword: "mypassword",
			wantErr:      false,
		},
		{
			name: "password_file_not_found",
			dbConfig: &DatabaseConfig{
				Host:         "localhost",
				Port:         5432,
				User:         "testuser",
				Database:     "testdb",
				PasswordFile: "/nonexistent/password.txt",
			},
			wantErr: true,
			errMsg:  "failed to read password from file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Setup password file if needed
			if tt.setupFile != nil {
				tt.dbConfig.PasswordFile = tt.setupFile(t)
			}

			password, err := tt.dbConfig.GetPassword()

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantPassword, password)
			}
		})
	}
}

func TestDatabaseConfigGetConnectionString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		dbConfig    *DatabaseConfig
		setupFile   func(t *testing.T) string
		wantConnStr string
		wantErr     bool
		errMsg      string
	}{
		{
			name: "valid_connection_string_with_default_sslmode",
			dbConfig: &DatabaseConfig{
				Host:     "localhost",
				Port:     5432,
				User:     "testuser",
				Database: "testdb",
			},
			setupFile: func(t *testing.T) string {
				t.Helper()
				tmpDir := t.TempDir()
				passwordFile := filepath.Join(tmpDir, "password.txt")
				err := os.WriteFile(passwordFile, []byte("mypassword"), 0600)
				require.NoError(t, err)
				return passwordFile
			},
			wantConnStr: "postgres://testuser:mypassword@localhost:5432/testdb?sslmode=require",
			wantErr:     false,
		},
		{
			name: "valid_connection_string_with_custom_sslmode",
			dbConfig: &DatabaseConfig{
				Host:     "db.example.com",
				Port:     5433,
				User:     "admin",
				Database: "production",
				SSLMode:  "verify-full",
			},
			setupFile: func(t *testing.T) string {
				t.Helper()
				tmpDir := t.TempDir()
				passwordFile := filepath.Join(tmpDir, "password.txt")
				err := os.WriteFile(passwordFile, []byte("securepass"), 0600)
				require.NoError(t, err)
				return passwordFile
			},
			wantConnStr: "postgres://admin:securepass@db.example.com:5433/production?sslmode=verify-full",
			wantErr:     false,
		},
		{
			name: "connection_string_with_special_characters_in_password",
			dbConfig: &DatabaseConfig{
				Host:     "localhost",
				Port:     5432,
				User:     "testuser",
				Database: "testdb",
			},
			setupFile: func(t *testing.T) string {
				t.Helper()
				tmpDir := t.TempDir()
				passwordFile := filepath.Join(tmpDir, "password.txt")
				err := os.WriteFile(passwordFile, []byte("p@ss&w0rd!#$%"), 0600)
				require.NoError(t, err)
				return passwordFile
			},
			wantConnStr: "postgres://testuser:p%40ss%26w0rd%21%23%24%25@localhost:5432/testdb?sslmode=require",
			wantErr:     false,
		},
		{
			name: "connection_string_from_password_file",
			dbConfig: &DatabaseConfig{
				Host:     "localhost",
				Port:     5432,
				User:     "testuser",
				Database: "testdb",
				SSLMode:  "disable",
			},
			setupFile: func(t *testing.T) string {
				t.Helper()
				tmpDir := t.TempDir()
				passwordFile := filepath.Join(tmpDir, "password.txt")
				err := os.WriteFile(passwordFile, []byte("filepassword"), 0600)
				require.NoError(t, err)
				return passwordFile
			},
			wantConnStr: "postgres://testuser:filepassword@localhost:5432/testdb?sslmode=disable",
			wantErr:     false,
		},
		{
			name: "error_when_password_file_not_found",
			dbConfig: &DatabaseConfig{
				Host:         "localhost",
				Port:         5432,
				User:         "testuser",
				Database:     "testdb",
				PasswordFile: "/nonexistent/password.txt",
			},
			wantErr: true,
			errMsg:  "failed to read password from file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Setup password file if needed
			if tt.setupFile != nil {
				tt.dbConfig.PasswordFile = tt.setupFile(t)
			}

			connStr, err := tt.dbConfig.GetConnectionString()

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantConnStr, connStr)
			}
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
			yamlContent: `source:
  type: file
  file:
    path: /data/registry.json
syncPolicy:
  interval: "30m"
database:
  host: localhost
  port: 5432
  user: testuser
  database: testdb`,
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
			yamlContent: `source:
  type: file
  file:
    path: /data/registry.json
syncPolicy:
  interval: "1h"
database:
  host: db.example.com
  port: 5433
  user: admin
  passwordFile: /secrets/db-password
  database: production
  sslMode: verify-full
  maxOpenConns: 25
  maxIdleConns: 10
  connMaxLifetime: "1h"`,
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
				Database: &DatabaseConfig{
					Host:            "db.example.com",
					Port:            5433,
					User:            "admin",
					PasswordFile:    "/secrets/db-password",
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
			yamlContent: `source:
  type: file
  file:
    path: /data/registry.json
syncPolicy:
  interval: "30m"`,
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

func TestAuthConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		yaml    string
		wantErr bool
		errMsg  string
		check   func(t *testing.T, cfg *Config)
	}{
		{
			name: "auth defaults to anonymous",
			yaml: `source:
  type: file
  file:
    path: /data/registry.json
syncPolicy:
  interval: "30m"`,
			wantErr: false,
			check: func(t *testing.T, cfg *Config) {
				t.Helper()
				// Source should be parsed correctly
				assert.Equal(t, "file", cfg.Source.Type)
				assert.Equal(t, "/data/registry.json", cfg.Source.File.Path)
				// Auth should be nil (defaults to anonymous behavior)
				assert.Nil(t, cfg.Auth)
			},
		},
		{
			name: "oauth with k8s and okta providers",
			yaml: `source:
  type: file
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
			wantErr: false,
			check: func(t *testing.T, cfg *Config) {
				t.Helper()
				require.NotNil(t, cfg.Auth)
				assert.Equal(t, AuthModeOAuth, cfg.Auth.Mode)
				require.NotNil(t, cfg.Auth.OAuth)
				assert.Len(t, cfg.Auth.OAuth.Providers, 2)
				assert.Equal(t, "https://kubernetes.default.svc.cluster.local", cfg.Auth.OAuth.Providers[0].IssuerURL)
				assert.Equal(t, "https://dev-12345.okta.com", cfg.Auth.OAuth.Providers[1].IssuerURL)
			},
		},
		{
			name: "explicit anonymous mode",
			yaml: `source:
  type: file
  file:
    path: /data/registry.json
syncPolicy:
  interval: "30m"
auth:
  mode: anonymous`,
			wantErr: false,
			check: func(t *testing.T, cfg *Config) {
				t.Helper()
				require.NotNil(t, cfg.Auth)
				assert.Equal(t, AuthModeAnonymous, cfg.Auth.Mode)
			},
		},
		{
			name: "oauth requires oauth config",
			yaml: `source:
  type: file
  file:
    path: /data/registry.json
syncPolicy:
  interval: "30m"
auth:
  mode: oauth`,
			wantErr: true,
			errMsg:  "auth.oauth is required",
		},
		{
			name: "oauth requires providers",
			yaml: `source:
  type: file
  file:
    path: /data/registry.json
syncPolicy:
  interval: "30m"
auth:
  mode: oauth
  oauth:
    providers: []`,
			wantErr: true,
			errMsg:  "providers",
		},
		{
			name: "provider requires issuerUrl",
			yaml: `source:
  type: file
  file:
    path: /data/registry.json
syncPolicy:
  interval: "30m"
auth:
  mode: oauth
  oauth:
    providers:
      - name: kubernetes`,
			wantErr: true,
			errMsg:  "issuerUrl",
		},
		{
			name: "provider requires audience",
			yaml: `source:
  type: file
  file:
    path: /data/registry.json
syncPolicy:
  interval: "30m"
auth:
  mode: oauth
  oauth:
    providers:
      - name: kubernetes
        issuerUrl: https://kubernetes.default.svc.cluster.local`,
			wantErr: true,
			errMsg:  "audience is required",
		},
		{
			name: "issuerUrl must be HTTPS",
			yaml: `source:
  type: file
  file:
    path: /data/registry.json
syncPolicy:
  interval: "30m"
auth:
  mode: oauth
  oauth:
    providers:
      - name: test
        issuerUrl: http://example.com
        audience: api://test`,
			wantErr: true,
			errMsg:  "must use HTTPS",
		},
		{
			name: "issuerUrl must be valid URL",
			yaml: `source:
  type: file
  file:
    path: /data/registry.json
syncPolicy:
  interval: "30m"
auth:
  mode: oauth
  oauth:
    providers:
      - name: test
        issuerUrl: "not-a-valid-url"
        audience: api://test`,
			wantErr: true,
			errMsg:  "must be an absolute URL with host",
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

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
				return
			}

			require.NoError(t, err)
			if tt.check != nil {
				tt.check(t, cfg)
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
