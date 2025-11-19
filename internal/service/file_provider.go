// Package service provides the business logic for the MCP registry API
package service

import (
	"context"
	"fmt"

	toolhivetypes "github.com/stacklok/toolhive/pkg/registry/registry"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/sources"
)

// FileRegistryDataProvider implements RegistryDataProvider by delegating to StorageManager.
// This implementation uses the adapter pattern to reuse storage infrastructure instead of
// duplicating file reading logic. It delegates to StorageManager for all file operations.
type FileRegistryDataProvider struct {
	storageManager sources.StorageManager
	config         *config.Config
	registryName   string
}

// NewFileRegistryDataProvider creates a new file-based registry data provider.
// It accepts a StorageManager to delegate file operations and a Config for registry metadata.
// This design eliminates code duplication and improves testability through dependency injection.
func NewFileRegistryDataProvider(storageManager sources.StorageManager, cfg *config.Config) *FileRegistryDataProvider {
	return &FileRegistryDataProvider{
		storageManager: storageManager,
		config:         cfg,
		registryName:   cfg.GetRegistryName(),
	}
}

// GetRegistryData implements RegistryDataProvider.GetRegistryData.
// It delegates to the StorageManager to retrieve and parse registry data.
// This eliminates code duplication and provides a single source of truth for file operations.
//
// NOTE: In PR 1, StorageManager returns UpstreamRegistry but RegistryDataProvider interface
// still expects toolhive Registry. This method converts at the boundary to maintain
// backward compatibility until PR 2.
func (p *FileRegistryDataProvider) GetRegistryData(ctx context.Context) (*toolhivetypes.UpstreamRegistry, error) {
	// Get UpstreamRegistry from storage manager (new format)
	registry, err := p.storageManager.Get(ctx, p.config)
	if err != nil {
		return nil, fmt.Errorf("failed to get registry data: %w", err)
	}

	return registry, nil
}

// GetSource implements RegistryDataProvider.GetSource.
// It returns a descriptive string indicating the file source from the configuration.
func (p *FileRegistryDataProvider) GetSource() string {
	if p.config.Source.File == nil || p.config.Source.File.Path == "" {
		return "file:<not-configured>"
	}
	return fmt.Sprintf("file:%s", p.config.Source.File.Path)
}

// GetRegistryName implements RegistryDataProvider.GetRegistryName.
// It returns the injected registry name identifier.
func (p *FileRegistryDataProvider) GetRegistryName() string {
	return p.registryName
}
