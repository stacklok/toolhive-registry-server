// Package inmemory provides an in-memory implementation of the RegistryService interface
package inmemory

import (
	"context"
	"fmt"
	"time"

	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/stacklok/toolhive/pkg/logger"
	toolhivetypes "github.com/stacklok/toolhive/pkg/registry/registry"

	"github.com/stacklok/toolhive-registry-server/internal/registry"
	"github.com/stacklok/toolhive-registry-server/internal/service"
)

// regSvc implements the RegistryService interface
type regSvc struct {
	registryProvider service.RegistryDataProvider

	registryData *toolhivetypes.UpstreamRegistry

	lastFetch     time.Time
	cacheDuration time.Duration
}

var _ service.RegistryService = (*regSvc)(nil)

// Option is a functional option for configuring the regSvc
type Option func(*regSvc)

// WithCacheDuration sets a custom cache duration for registry data
func WithCacheDuration(duration time.Duration) Option {
	return func(s *regSvc) {
		s.cacheDuration = duration
	}
}

// New creates a new registry regSvc with the given providers and options.
// registryProvider is required for registry data access.
// deploymentProvider can be nil if deployed servers functionality is not needed.
func New(
	ctx context.Context,
	registryProvider service.RegistryDataProvider,
	opts ...Option,
) (service.RegistryService, error) {
	if registryProvider == nil {
		return nil, fmt.Errorf("registry data provider is required")
	}

	s := &regSvc{
		registryProvider: registryProvider,
		cacheDuration:    30 * time.Second, // Default cache duration
	}

	// Apply functional options
	for _, opt := range opts {
		opt(s)
	}

	// Load initial data
	if err := s.loadRegistryData(ctx); err != nil {
		logger.Warnf("Failed to load initial registry data: %v", err)
		// Don't fail regSvc creation, allow it to retry later
	}

	return s, nil
}

// loadRegistryData loads registry data using the configured provider
func (s *regSvc) loadRegistryData(ctx context.Context) error {
	if s.registryProvider == nil {
		return fmt.Errorf("registry data provider not initialized")
	}

	data, err := s.registryProvider.GetRegistryData(ctx)
	if err != nil {
		return fmt.Errorf("failed to get registry data: %w", err)
	}

	s.registryData = data
	s.lastFetch = time.Now()

	// Count total servers (both container and remote)
	totalServers := len(data.Data.Servers)
	logger.Infof("Loaded registry data: %d servers", totalServers)
	return nil
}

// refreshDataIfNeeded refreshes the registry data if cache has expired
func (s *regSvc) refreshDataIfNeeded(ctx context.Context) error {
	if s.registryProvider == nil {
		return nil // No registry provider configured
	}

	if time.Since(s.lastFetch) > s.cacheDuration {
		if err := s.loadRegistryData(ctx); err != nil {
			logger.Warnf("Failed to refresh registry data: %v", err)
			// Continue with stale data if available
			if s.registryData == nil {
				return err
			}
		}
	}
	return nil
}

// CheckReadiness implements RegistryService.CheckReadiness
func (s *regSvc) CheckReadiness(ctx context.Context) error {
	// Check if we have registry data loaded when a provider is configured
	if s.registryProvider != nil {
		if s.registryData == nil {
			// Try to load it
			if err := s.loadRegistryData(ctx); err != nil {
				return fmt.Errorf("registry data not available: %w", err)
			}
		}
	}
	return nil
}

// GetRegistry implements RegistryService.GetRegistry
func (s *regSvc) GetRegistry(ctx context.Context) (*toolhivetypes.UpstreamRegistry, string, error) {
	if err := s.refreshDataIfNeeded(ctx); err != nil {
		logger.Warnf("Failed to refresh data: %v", err)
	}

	// Get source information from the provider
	source := "unknown"
	if s.registryProvider != nil {
		source = s.registryProvider.GetSource()
	}

	if s.registryData == nil {
		// Return an empty registry if no data is loaded
		return &toolhivetypes.UpstreamRegistry{
			Schema:  registry.UpstreamRegistrySchemaURL,
			Version: registry.UpstreamRegistryVersion,
			Meta: toolhivetypes.UpstreamMeta{
				LastUpdated: time.Now().Format(time.RFC3339),
			},
			Data: toolhivetypes.UpstreamData{
				Servers: make([]upstreamv0.ServerJSON, 0),
				Groups:  make([]toolhivetypes.UpstreamGroup, 0),
			},
		}, source, nil
	}

	return s.registryData, source, nil
}

// ListServers implements RegistryService.ListServers
func (s *regSvc) ListServers(
	ctx context.Context,
	_ ...service.Option[service.ListServersOptions],
) ([]*upstreamv0.ServerJSON, error) {
	if err := s.refreshDataIfNeeded(ctx); err != nil {
		logger.Warnf("Failed to refresh data: %v", err)
	}

	if s.registryData != nil {
		servers := make([]*upstreamv0.ServerJSON, len(s.registryData.Data.Servers))
		for i, server := range s.registryData.Data.Servers {
			servers[i] = &server
		}
		return servers, nil
	}

	return nil, nil
}

// ListServerVersions implements RegistryService.ListServerVersions
func (s *regSvc) ListServerVersions(
	ctx context.Context,
	_ ...service.Option[service.ListServerVersionsOptions],
) ([]*upstreamv0.ServerJSON, error) {
	if err := s.refreshDataIfNeeded(ctx); err != nil {
		logger.Warnf("Failed to refresh data: %v", err)
	}

	return nil, service.ErrNotImplemented
}

// GetServerVersion implements RegistryService.GetServerVersion
func (s *regSvc) GetServerVersion(
	ctx context.Context,
	opts ...service.Option[service.GetServerVersionOptions],
) (*upstreamv0.ServerJSON, error) {
	if err := s.refreshDataIfNeeded(ctx); err != nil {
		logger.Warnf("Failed to refresh data: %v", err)
	}

	options := &service.GetServerVersionOptions{}
	for _, opt := range opts {
		if err := opt(options); err != nil {
			return nil, err
		}
	}

	if s.registryData != nil {
		return s.getServerByName(options.Name)
	}

	return nil, service.ErrServerNotFound
}

// PublishServerVersion implements RegistryService.PublishServerVersion
func (*regSvc) PublishServerVersion(
	_ context.Context,
	_ ...service.Option[service.PublishServerVersionOptions],
) (*upstreamv0.ServerJSON, error) {
	return nil, service.ErrNotImplemented
}

// DeleteServerVersion implements RegistryService.DeleteServerVersion
func (*regSvc) DeleteServerVersion(
	_ context.Context,
	_ ...service.Option[service.DeleteServerVersionOptions],
) error {
	return service.ErrNotImplemented
}

// getServerByNameWithName returns a server by name with name properly populated
func (s *regSvc) getServerByName(name string) (*upstreamv0.ServerJSON, error) {
	// Check container servers first
	for _, server := range s.registryData.Data.Servers {
		if server.Name == name {
			return &server, nil
		}
	}

	return nil, service.ErrServerNotFound
}

// ListRegistries returns all configured registries - not supported for in-memory service
func (*regSvc) ListRegistries(_ context.Context) ([]service.RegistryInfo, error) {
	// TODO: Implement file-based ListRegistries support in a follow-up
	return nil, service.ErrNotImplemented
}
