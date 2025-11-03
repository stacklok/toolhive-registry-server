package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// ConfigLoader defines the interface for loading configuration
type ConfigLoader interface {
	LoadConfig(path string) (*Config, error)
}

// Config represents the root configuration structure
type Config struct {
	Source     SourceConfig     `yaml:"source"`
	SyncPolicy SyncPolicyConfig `yaml:"syncPolicy"`
	Filter     FilterConfig     `yaml:"filter"`
}

// SourceConfig defines the data source configuration
type SourceConfig struct {
	Type      string           `yaml:"type"`
	ConfigMap *ConfigMapConfig `yaml:"configmap,omitempty"`
}

// ConfigMapConfig defines Kubernetes ConfigMap source settings
type ConfigMapConfig struct {
	Name string `yaml:"name"`
}

// SyncPolicyConfig defines synchronization settings
type SyncPolicyConfig struct {
	Interval string `yaml:"interval"`
}

// FilterConfig defines filtering rules for registry entries
type FilterConfig struct {
	Tags TagFilterConfig `yaml:"tags"`
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

// LoadConfig loads and parses configuration from a YAML file
func (c *configLoader) LoadConfig(path string) (*Config, error) {
	// Read the entire file into memory
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
