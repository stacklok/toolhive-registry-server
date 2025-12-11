// Package inmemory provides the business logic for the MCP registry API
package inmemory

import (
	"context"
	"fmt"

	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	toolhivetypes "github.com/stacklok/toolhive/pkg/registry/registry"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/registry"
	"github.com/stacklok/toolhive-registry-server/internal/sources"
)

// fileRegistryDataProvider implements RegistryDataProvider by delegating to StorageManager.
// This implementation uses the adapter pattern to reuse storage infrastructure instead of
// duplicating file reading logic. It delegates to StorageManager for all file operations.
type fileRegistryDataProvider struct {
	storageManager sources.StorageManager
	config         *config.Config
	registryName   string
}

// NewFileRegistryDataProvider creates a new file-based registry data provider.
// It accepts a StorageManager to delegate file operations and a Config for registry metadata.
// This design eliminates code duplication and improves testability through dependency injection.
func NewFileRegistryDataProvider(storageManager sources.StorageManager, cfg *config.Config) RegistryDataProvider {
	return &fileRegistryDataProvider{
		storageManager: storageManager,
		config:         cfg,
		registryName:   cfg.GetRegistryName(),
	}
}

// GetRegistryData implements RegistryDataProvider.GetRegistryData.
// It delegates to the StorageManager to retrieve and parse registry data from all registries,
// then merges them into a single UpstreamRegistry for the API response.
func (p *fileRegistryDataProvider) GetRegistryData(ctx context.Context) (*toolhivetypes.UpstreamRegistry, error) {
	// Get all registry data from storage manager
	allRegistries, err := p.storageManager.GetAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get registry data: %w", err)
	}

	// Merge all registries into a single UpstreamRegistry
	merged := &toolhivetypes.UpstreamRegistry{
		Schema:  registry.UpstreamRegistrySchemaURL, // Use default schema URL
		Version: registry.UpstreamRegistryVersion,   // Use default version
		Meta:    toolhivetypes.UpstreamMeta{},
		Data: toolhivetypes.UpstreamData{
			Servers: make([]upstreamv0.ServerJSON, 0),
			Groups:  make([]toolhivetypes.UpstreamGroup, 0),
		},
	}

	firstRegistry := true
	for registryName, reg := range allRegistries {
		if reg == nil {
			continue
		}

		// Copy metadata from first non-nil registry, but use defaults if empty
		if firstRegistry {
			// Use registry's schema if provided, otherwise keep default
			if reg.Schema != "" {
				merged.Schema = reg.Schema
			}
			// Use registry's version if provided, otherwise keep default
			if reg.Version != "" {
				merged.Version = reg.Version
			}
			merged.Meta = reg.Meta
			firstRegistry = false
		}

		// Add all servers from this registry
		// TODO: Consider adding registry source metadata to each server entry
		_ = registryName // Will be used for metadata in future
		merged.Data.Servers = append(merged.Data.Servers, reg.Data.Servers...)

		// Merge groups if any
		merged.Data.Groups = append(merged.Data.Groups, reg.Data.Groups...)
	}

	return merged, nil
}

// GetSource implements RegistryDataProvider.GetSource.
// It returns a descriptive string indicating all configured registries.
func (p *fileRegistryDataProvider) GetSource() string {
	if len(p.config.Registries) == 0 {
		return "multi-registry:<not-configured>"
	}
	if len(p.config.Registries) == 1 {
		regCfg := &p.config.Registries[0]
		if regCfg.File != nil {
			return fmt.Sprintf("file:%s", regCfg.File.Path)
		}
		if regCfg.Git != nil {
			return fmt.Sprintf("git:%s", regCfg.Git.Repository)
		}
		if regCfg.API != nil {
			return fmt.Sprintf("api:%s", regCfg.API.Endpoint)
		}
		return fmt.Sprintf("registry:%s", regCfg.Name)
	}
	// Multiple registries
	return fmt.Sprintf("multi-registry:%d-sources", len(p.config.Registries))
}

// GetRegistryName implements RegistryDataProvider.GetRegistryName.
// It returns the injected registry name identifier.
func (p *fileRegistryDataProvider) GetRegistryName() string {
	return p.registryName
}
