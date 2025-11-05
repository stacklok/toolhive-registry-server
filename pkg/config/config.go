package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	// RegistrySourceTypeConfigMap is the type for registry data stored in ConfigMaps
	SourceTypeConfigMap = "configmap"

	// RegistrySourceTypeGit is the type for registry data stored in Git repositories
	SourceTypeGit = "git"

	// SourceTypeAPI is the type for registry data fetched from API endpoints
	SourceTypeAPI = "api"

	// RegistryFormatToolHive is the native ToolHive registry format
	SourceFormatToolHive = "toolhive"

	// RegistryFormatUpstream is the upstream MCP registry format
	SourceFormatUpstream = "upstream"
)

type ConfigLoader interface {
	// LoadConfig reads and parses a configuration file from the given path.
	// The file is only read, never modified.
	LoadConfig(path string) (*Config, error)
}

// Config represents the root configuration structure
type Config struct {
	Source     SourceConfig      `yaml:"source"`
	SyncPolicy *SyncPolicyConfig `yaml:"syncPolicy,omitempty"`
	Filter     *FilterConfig     `yaml:"filter,omitempty"`
}

// SourceConfig defines the data source configuration
type SourceConfig struct {
	Type      string           `yaml:"type"`
	Format    string           `yaml:"format"`
	ConfigMap *ConfigMapConfig `yaml:"configmap,omitempty"`
	Git       *GitConfig       `yaml:"git,omitempty"`
	API       *APIConfig       `yaml:"api,omitempty"`
}

// ConfigMapConfig defines Kubernetes ConfigMap source settings
type ConfigMapConfig struct {
	Namespace string `yaml:"namespace"`
	Name      string `yaml:"name"`
	Key       string `yaml:"key,omitempty"`
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

// configLoader implements the ConfigLoader interface
type configLoader struct{}

// NewConfigLoader creates a new ConfigLoader instance
func NewConfigLoader() ConfigLoader {
	return &configLoader{}
}

// LoadConfig reads and parses a YAML configuration file.
// This is a read-only operation - the file is never modified.
func (c *configLoader) LoadConfig(path string) (*Config, error) {
	// Read-only file access
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Parse the YAML content
	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	return &config, nil
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

	// Validate ConfigMap settings if type is configmap
	if c.Source.Type == "configmap" {
		if c.Source.ConfigMap == nil {
			return fmt.Errorf("source.configmap is required when type is configmap")
		}
		if c.Source.ConfigMap.Name == "" {
			return fmt.Errorf("source.configmap.name is required")
		}
	}

	// Validate sync policy
	if c.SyncPolicy.Interval == "" {
		return fmt.Errorf("syncPolicy.interval is required")
	}

	// Try to parse the interval to ensure it's valid
	if _, err := time.ParseDuration(c.SyncPolicy.Interval); err != nil {
		return fmt.Errorf("syncPolicy.interval must be a valid duration (e.g., '30m', '1h'): %w", err)
	}

	return nil
}
