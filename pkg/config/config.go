package config

import (
	"fmt"
	"os"

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
