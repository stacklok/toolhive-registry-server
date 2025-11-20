// Package config provides configuration loading and management for the registry server.
package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	// SourceTypeGit is the type for registry data stored in Git repositories
	SourceTypeGit = "git"

	// SourceTypeAPI is the type for registry data fetched from API endpoints
	SourceTypeAPI = "api"

	// SourceTypeFile is the type for registry data stored in local files
	SourceTypeFile = "file"
)

const (
	// SourceFormatToolHive is the native ToolHive registry format
	SourceFormatToolHive = "toolhive"

	// SourceFormatUpstream is the upstream MCP registry format
	SourceFormatUpstream = "upstream"
)

// Option defines the interface for configuration options
type Option func(*loaderConfig) error

// loaderConfig defines the configuration for loading a configuration
type loaderConfig struct {
	path string
}

// WithConfigPath loads configuration from a YAML file
func WithConfigPath(path string) Option {
	return func(cfg *loaderConfig) error {
		if path == "" {
			return fmt.Errorf("path is required")
		}

		// Resolve symlinks to prevent symlink attacks.
		// Note that this calls filepath.Clean internally.
		realPath, err := filepath.EvalSymlinks(path)
		if err != nil {
			return fmt.Errorf("failed to evaluate symlinks: %w", err)
		}

		// Validate the path to prevent path traversal attacks
		if !filepath.IsAbs(realPath) {
			if !filepath.IsLocal(realPath) {
				return fmt.Errorf("path is not local or contains invalid traversal: %s", path)
			}
		}

		cfg.path = realPath
		return nil
	}
}

// Config represents the root configuration structure
type Config struct {
	// RegistryName is the name/identifier for this registry instance
	// Defaults to "default" if not specified
	RegistryName string            `yaml:"registryName,omitempty"`
	Source       SourceConfig      `yaml:"source"`
	SyncPolicy   *SyncPolicyConfig `yaml:"syncPolicy,omitempty"`
	Filter       *FilterConfig     `yaml:"filter,omitempty"`
	Database     *DatabaseConfig   `yaml:"database,omitempty"`
}

// SourceConfig defines the data source configuration
type SourceConfig struct {
	Type   string      `yaml:"type"`
	Format string      `yaml:"format"`
	Git    *GitConfig  `yaml:"git,omitempty"`
	API    *APIConfig  `yaml:"api,omitempty"`
	File   *FileConfig `yaml:"file,omitempty"`
}

// GitConfig defines Git source settings
type GitConfig struct {
	// Repository is the Git repository URL (HTTP/HTTPS/SSH)
	Repository string `yaml:"repository"`

	// Branch is the Git branch to use (mutually exclusive with Tag and Commit)
	Branch string `yaml:"branch,omitempty"`

	// Tag is the Git tag to use (mutually exclusive with Branch and Commit)
	Tag string `yaml:"tag,omitempty"`

	// Commit is the Git commit SHA to use (mutually exclusive with Branch and Tag)
	Commit string `yaml:"commit,omitempty"`

	// Path is the path to the registry file within the repository
	Path string `yaml:"path,omitempty"`
}

// APIConfig defines API source configuration for ToolHive Registry APIs
type APIConfig struct {
	// Endpoint is the base API URL (without path)
	// The source handler will append the appropriate paths, for instance:
	//   - /v0/servers - List all servers (single response, no pagination)
	//   - /v0/servers/{name} - Get specific server (future)
	//   - /v0/info - Get registry metadata (future)
	// Example: "http://my-registry-api.default.svc.cluster.local/api"
	Endpoint string `yaml:"endpoint"`
}

// FileConfig defines local file source configuration
type FileConfig struct {
	// Path is the path to the registry.json file on the local filesystem
	// Can be absolute or relative to the working directory
	Path string `yaml:"path"`
}

// SyncPolicyConfig defines synchronization settings
type SyncPolicyConfig struct {
	Interval string `yaml:"interval"`
}

// FilterConfig defines filtering rules for registry entries
type FilterConfig struct {
	Names *NameFilterConfig `yaml:"names,omitempty"`
	Tags  *TagFilterConfig  `yaml:"tags,omitempty"`
}

// NameFilterConfig defines name-based filtering
type NameFilterConfig struct {
	Include []string `yaml:"include,omitempty"`
	Exclude []string `yaml:"exclude,omitempty"`
}

// TagFilterConfig defines tag-based filtering
type TagFilterConfig struct {
	Include []string `yaml:"include,omitempty"`
	Exclude []string `yaml:"exclude,omitempty"`
}

// DatabaseConfig defines database connection settings
type DatabaseConfig struct {
	// Host is the database server hostname or IP address
	Host string `yaml:"host"`

	// Port is the database server port
	Port int `yaml:"port"`

	// User is the database username
	User string `yaml:"user"`

	// PasswordFile is the path to a file containing the database password
	// This is the recommended approach for production deployments
	// The file should contain only the password with optional trailing whitespace
	PasswordFile string `yaml:"passwordFile,omitempty"`

	// Database is the database name
	Database string `yaml:"database"`

	// SSLMode is the SSL mode for the connection (disable, require, verify-ca, verify-full)
	SSLMode string `yaml:"sslMode,omitempty"`

	// MaxOpenConns is the maximum number of open connections to the database
	MaxOpenConns int32 `yaml:"maxOpenConns,omitempty"`

	// MaxIdleConns is the maximum number of idle connections in the pool
	MaxIdleConns int32 `yaml:"maxIdleConns,omitempty"`

	// ConnMaxLifetime is the maximum lifetime of a connection (e.g., "1h", "30m")
	ConnMaxLifetime string `yaml:"connMaxLifetime,omitempty"`
}

// GetPassword returns the database password using the following priority:
// 1. Read from PasswordFile if specified
// 2. Read from THV_DATABASE_PASSWORD environment variable
//
// The password from file will have leading/trailing whitespace trimmed.
func (d *DatabaseConfig) GetPassword() (string, error) {
	// Priority 1: Read from file if specified
	if d.PasswordFile != "" {
		// Use filepath.Clean to prevent path traversal attacks
		cleanPath := filepath.Clean(d.PasswordFile)

		data, err := os.ReadFile(cleanPath)
		if err != nil {
			return "", fmt.Errorf("failed to read password from file %s: %w", d.PasswordFile, err)
		}

		// Trim whitespace (including newlines) from file content
		password := strings.TrimSpace(string(data))
		return password, nil
	}

	// Priority 2: Check environment variable
	if envPassword := os.Getenv("THV_DATABASE_PASSWORD"); envPassword != "" {
		return envPassword, nil
	}

	return "", fmt.Errorf(
		"no database password configured: set passwordFile or THV_DATABASE_PASSWORD environment variable",
	)
}

// GetConnectionString builds a PostgreSQL connection string with proper password handling.
// The password is URL-escaped to handle special characters safely.
func (d *DatabaseConfig) GetConnectionString() (string, error) {
	password, err := d.GetPassword()
	if err != nil {
		return "", err
	}

	sslMode := d.SSLMode
	if sslMode == "" {
		sslMode = "require"
	}

	// URL-escape the password to handle special characters
	escapedPassword := url.QueryEscape(password)

	connString := fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=%s",
		d.User,
		escapedPassword,
		d.Host,
		d.Port,
		d.Database,
		sslMode,
	)

	return connString, nil
}

// LoadConfig loads and parses configuration from a YAML file
func LoadConfig(opts ...Option) (*Config, error) {
	loaderCfg := &loaderConfig{}
	for _, opt := range opts {
		if err := opt(loaderCfg); err != nil {
			return nil, err
		}
	}

	// As of now, this is required because there's no other options to load
	// configuration. Once we add more options, we can remove this check.
	if loaderCfg.path == "" {
		return nil, fmt.Errorf("path is required")
	}

	// Read the entire file into memory
	data, err := os.ReadFile(loaderCfg.path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Parse YAML content
	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse YAML config: %w", err)
	}

	// Validate the config
	if err := config.validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return &config, nil
}

// GetRegistryName returns the registry name, using "default" if not specified
func (c *Config) GetRegistryName() string {
	if c.RegistryName == "" {
		return "default"
	}
	return c.RegistryName
}

// Validate performs validation on the configuration
func (c *Config) validate() error {
	if c == nil {
		return fmt.Errorf("config cannot be nil")
	}

	// Validate source configuration
	if c.Source.Type == "" {
		return fmt.Errorf("source.type is required")
	}

	if err := c.validateSourceConfigByType(); err != nil {
		return err
	}

	// Validate sync policy
	if c.SyncPolicy == nil || c.SyncPolicy.Interval == "" {
		return fmt.Errorf("syncPolicy.interval is required")
	}

	// Try to parse the interval to ensure it's valid
	if _, err := time.ParseDuration(c.SyncPolicy.Interval); err != nil {
		return fmt.Errorf("syncPolicy.interval must be a valid duration (e.g., '30m', '1h'): %w", err)
	}

	return nil
}

// validateSourceConfigByType validates the source configuration by the source type
func (c *Config) validateSourceConfigByType() error {
	// Validate source-specific settings
	switch c.Source.Type {
	case SourceTypeGit:
		if c.Source.Git == nil {
			return fmt.Errorf("source.git is required when type is git")
		}
		if c.Source.Git.Repository == "" {
			return fmt.Errorf("source.git.repository is required")
		}

	case SourceTypeAPI:
		if c.Source.API == nil {
			return fmt.Errorf("source.api is required when type is api")
		}
		if c.Source.API.Endpoint == "" {
			return fmt.Errorf("source.api.endpoint is required")
		}
		if c.Source.Format != "" && c.Source.Format != SourceFormatUpstream {
			return fmt.Errorf("source.format must be either empty or %s when type is api, got %s", SourceFormatUpstream, c.Source.Format)
		}

	case SourceTypeFile:
		if c.Source.File == nil {
			return fmt.Errorf("source.file is required when type is file")
		}
		if c.Source.File.Path == "" {
			return fmt.Errorf("source.file.path is required")
		}

	default:
		return fmt.Errorf("unsupported source type: %s", c.Source.Type)
	}

	return nil
}
