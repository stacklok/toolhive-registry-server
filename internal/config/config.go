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
	RegistryName string           `yaml:"registryName,omitempty"`
	Registries   []RegistryConfig `yaml:"registries"`
	Database     *DatabaseConfig  `yaml:"database,omitempty"`
}

// RegistryConfig defines a single registry data source configuration
type RegistryConfig struct {
	// Name is the identifier for this registry
	Name string `yaml:"name"`

	// Format specifies the data format (toolhive or upstream)
	Format string `yaml:"format"`

	// Type-specific configurations (only one should be set)
	Git  *GitConfig  `yaml:"git,omitempty"`
	API  *APIConfig  `yaml:"api,omitempty"`
	File *FileConfig `yaml:"file,omitempty"`

	// Per-registry sync policy
	SyncPolicy *SyncPolicyConfig `yaml:"syncPolicy,omitempty"`

	// Per-registry filtering rules
	Filter *FilterConfig `yaml:"filter,omitempty"`
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
	// The registry handler will append the appropriate paths, for instance:
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

	// Validate at least one registry is configured
	if len(c.Registries) == 0 {
		return fmt.Errorf("at least one registry must be configured")
	}

	// Validate each registry configuration
	registryNames := make(map[string]bool)
	for i, reg := range c.Registries {
		// Validate registry name
		if reg.Name == "" {
			return fmt.Errorf("registry[%d]: name is required", i)
		}

		// Check for duplicate registry names
		if registryNames[reg.Name] {
			return fmt.Errorf("registry[%d]: duplicate registry name '%s'", i, reg.Name)
		}
		registryNames[reg.Name] = true

		// Validate registry-specific configuration
		if err := c.validateRegistryConfig(&reg, i); err != nil {
			return err
		}
	}

	return nil
}

// validateRegistryConfig validates a single registry configuration
func (*Config) validateRegistryConfig(reg *RegistryConfig, index int) error {
	prefix := fmt.Sprintf("registry[%d] (%s)", index, reg.Name)

	// Validate sync policy
	if err := validateSyncPolicy(reg.SyncPolicy, prefix); err != nil {
		return err
	}

	// Validate exactly one source type is configured
	if err := validateSourceTypeCount(reg, prefix); err != nil {
		return err
	}

	// Validate type-specific settings
	return validateSourceSpecificConfig(reg, prefix)
}

// validateSyncPolicy validates the sync policy configuration
func validateSyncPolicy(policy *SyncPolicyConfig, prefix string) error {
	if policy == nil || policy.Interval == "" {
		return fmt.Errorf("%s: syncPolicy.interval is required", prefix)
	}

	// Try to parse the interval to ensure it's valid
	if _, err := time.ParseDuration(policy.Interval); err != nil {
		return fmt.Errorf("%s: syncPolicy.interval must be a valid duration (e.g., '30m', '1h'): %w", prefix, err)
	}

	return nil
}

// validateSourceTypeCount ensures exactly one source type is configured
func validateSourceTypeCount(reg *RegistryConfig, prefix string) error {
	configCount := 0
	if reg.Git != nil {
		configCount++
	}
	if reg.API != nil {
		configCount++
	}
	if reg.File != nil {
		configCount++
	}

	if configCount == 0 {
		return fmt.Errorf("%s: one of git, api, or file configuration must be specified", prefix)
	}
	if configCount > 1 {
		return fmt.Errorf("%s: only one of git, api, or file configuration may be specified", prefix)
	}

	return nil
}

// validateSourceSpecificConfig validates the configuration for each source type
func validateSourceSpecificConfig(reg *RegistryConfig, prefix string) error {
	if reg.Git != nil {
		return validateGitConfig(reg.Git, prefix)
	}

	if reg.API != nil {
		return validateAPIConfig(reg.API, reg.Format, prefix)
	}

	if reg.File != nil {
		return validateFileConfig(reg.File, prefix)
	}

	return nil
}

// validateGitConfig validates Git-specific configuration
func validateGitConfig(git *GitConfig, prefix string) error {
	if git.Repository == "" {
		return fmt.Errorf("%s: git.repository is required", prefix)
	}
	return nil
}

// validateAPIConfig validates API-specific configuration
func validateAPIConfig(api *APIConfig, format string, prefix string) error {
	if api.Endpoint == "" {
		return fmt.Errorf("%s: api.endpoint is required", prefix)
	}
	if format != "" && format != SourceFormatUpstream {
		return fmt.Errorf("%s: format must be either empty or %s when using api, got %s", prefix, SourceFormatUpstream, format)
	}
	return nil
}

// validateFileConfig validates File-specific configuration
func validateFileConfig(file *FileConfig, prefix string) error {
	if file.Path == "" {
		return fmt.Errorf("%s: file.path is required", prefix)
	}
	return nil
}

// GetType returns the inferred type of the registry config based on which field is present
func (r *RegistryConfig) GetType() string {
	if r.Git != nil {
		return SourceTypeGit
	}
	if r.API != nil {
		return SourceTypeAPI
	}
	if r.File != nil {
		return SourceTypeFile
	}
	return ""
}
