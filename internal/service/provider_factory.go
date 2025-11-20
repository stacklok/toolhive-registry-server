// Package service provides the business logic for the MCP registry API
package service

import (
	"fmt"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/sources"
)

//go:generate mockgen -destination=mocks/mock_provider_factory.go -package=mocks -source=provider_factory.go RegistryProviderFactory

// RegistryProviderFactory creates registry data providers based on configuration
type RegistryProviderFactory interface {
	// CreateProvider creates a registry data provider based on the provided configuration
	CreateProvider(cfg *config.Config) (RegistryDataProvider, error)
}

// defaultRegistryProviderFactory is the default implementation of RegistryProviderFactory
type defaultRegistryProviderFactory struct {
	storageManager sources.StorageManager
}

var _ RegistryProviderFactory = (*defaultRegistryProviderFactory)(nil)

// NewRegistryProviderFactory creates a new default registry provider factory
func NewRegistryProviderFactory(storageManager sources.StorageManager) RegistryProviderFactory {
	return &defaultRegistryProviderFactory{
		storageManager: storageManager,
	}
}

// CreateProvider implements RegistryProviderFactory.CreateProvider
func (f *defaultRegistryProviderFactory) CreateProvider(cfg *config.Config) (RegistryDataProvider, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	storageType := cfg.GetStorage()

	switch storageType {
	case config.StorageTypeFile:
		return NewFileRegistryDataProvider(f.storageManager, cfg), nil
	case config.StorageTypeDatabase:
		// Database provider is not yet implemented
		// When implemented, this will create a database-backed provider
		return nil, fmt.Errorf("database storage is not yet implemented")
	default:
		return nil, fmt.Errorf("unsupported storage type: %s", storageType)
	}
}
