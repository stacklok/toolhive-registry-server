package sources

import (
	"context"
	"fmt"

	"github.com/stacklok/toolhive-registry-server/pkg/config"
	"github.com/stacklok/toolhive/pkg/registry"
)

const (
	// ConfigMapStorageDataKey is the key used to store registry data in ConfigMaps by the storage manager
	ConfigMapStorageDataKey = "registry.json"
	// RegistryStorageComponent is the component label for the registry storage
	RegistryStorageComponent = "registry-storage"

	// StorageTypeConfigMap identifies the ConfigMap storage manager implementation
	StorageTypeConfigMap = "configmap"
)

//go:generate mockgen -destination=mocks/mock_storage_manager.go -package=mocks -source=storage_manager.go StorageManager

// StorageManager defines the interface for registry data persistence
type StorageManager interface {
	// Store saves a Registry instance to persistent storage
	Store(ctx context.Context, config *config.Config, reg *registry.Registry) error

	// Get retrieves and parses registry data from persistent storage
	Get(ctx context.Context, config *config.Config) (*registry.Registry, error)

	// Delete removes registry data from persistent storage
	Delete(ctx context.Context, config *config.Config) error
}

// NewStorageManager creates a new storage manager
func NewStorageManager() (StorageManager, error) {
	return nil, fmt.Errorf("storage manager not yet implemented")
}
