// Package service provides the business logic for the MCP registry API
package service

import (
	"fmt"
)

// RegistryProviderConfig holds configuration for creating a file-based registry data provider
type RegistryProviderConfig struct {
	FilePath     string
	RegistryName string
}

// Validate validates the RegistryProviderConfig
func (c *RegistryProviderConfig) Validate() error {
	if c.FilePath == "" {
		return fmt.Errorf("file path is required")
	}
	if c.RegistryName == "" {
		return fmt.Errorf("registry name is required")
	}
	return nil
}

//go:generate mockgen -destination=mocks/mock_provider_factory.go -package=mocks -source=provider_factory.go RegistryProviderFactory

// RegistryProviderFactory creates registry data providers based on configuration
type RegistryProviderFactory interface {
	// CreateProvider creates a registry data provider based on the provided configuration
	CreateProvider(config *RegistryProviderConfig) (RegistryDataProvider, error)
}

// DefaultRegistryProviderFactory is the default implementation of RegistryProviderFactory
type DefaultRegistryProviderFactory struct{}

// NewRegistryProviderFactory creates a new default registry provider factory
func NewRegistryProviderFactory() RegistryProviderFactory {
	return &DefaultRegistryProviderFactory{}
}

// CreateProvider implements RegistryProviderFactory.CreateProvider
func (f *DefaultRegistryProviderFactory) CreateProvider(config *RegistryProviderConfig) (RegistryDataProvider, error) {
	if config == nil {
		return nil, fmt.Errorf("registry provider config cannot be nil")
	}

	if config.FilePath == "" {
		return nil, fmt.Errorf("file path is required")
	}
	if config.RegistryName == "" {
		return nil, fmt.Errorf("registry name is required")
	}

	return NewFileRegistryDataProvider(config.FilePath, config.RegistryName), nil
}
