// Package config provides configuration loading and management for the registry server.
package config

import (
	"fmt"
	"os"
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

// Loader defines the interface for loading configuration
type Loader interface {
	LoadConfig(path string) (*Config, error)
}

// Config represents the root configuration structure
type Config struct {
	// RegistryName is the name/identifier for this registry instance
	// Defaults to "default" if not specified
	RegistryName string            `yaml:"registryName,omitempty"`
	Source       SourceConfig      `yaml:"source"`
	SyncPolicy   *SyncPolicyConfig `yaml:"syncPolicy,omitempty"`
	Filter       *FilterConfig     `yaml:"filter,omitempty"`
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

// configLoader implements the Loader interface
type configLoader struct{}

// NewConfigLoader creates a new Loader instance
func NewConfigLoader() Loader {
	return &configLoader{}
}

// LoadConfig loads and parses configuration from a YAML file
func (*configLoader) LoadConfig(path string) (*Config, error) {
	// Read the entire file into memory
	//nolint:gosec // Config file path is provided by user, this is expected behavior
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Parse YAML content
	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse YAML config: %w", err)
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
func (c *Config) Validate() error {
	if c == nil {
		return fmt.Errorf("config cannot be nil")
	}

	// Validate source configuration
	if c.Source.Type == "" {
		return fmt.Errorf("source.type is required")
	}

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
