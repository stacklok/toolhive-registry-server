// Package service provides the business logic for the MCP registry API
package service

import (
	"fmt"

	"github.com/stacklok/toolhive-registry-server/pkg/config"
	"github.com/stacklok/toolhive-registry-server/pkg/sources"
)

//go:generate mockgen -destination=mocks/mock_provider_factory.go -package=mocks -source=provider_factory.go RegistryProviderFactory

// RegistryProviderFactory creates registry data providers based on configuration
type RegistryProviderFactory interface {
	// CreateProvider creates a registry data provider based on the provided configuration
	CreateProvider(cfg *config.Config) (RegistryDataProvider, error)
}

// DefaultRegistryProviderFactory is the default implementation of RegistryProviderFactory
type DefaultRegistryProviderFactory struct {
	storageManager sources.StorageManager
}

// NewRegistryProviderFactory creates a new default registry provider factory
func NewRegistryProviderFactory(storageManager sources.StorageManager) RegistryProviderFactory {
	return &DefaultRegistryProviderFactory{
		storageManager: storageManager,
	}
}

// CreateProvider implements RegistryProviderFactory.CreateProvider
func (f *DefaultRegistryProviderFactory) CreateProvider(cfg *config.Config) (RegistryDataProvider, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	return NewFileRegistryDataProvider(f.storageManager, cfg), nil
}
