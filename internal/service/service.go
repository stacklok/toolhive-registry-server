// Package service provides the business logic for the MCP registry API
package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/stacklok/toolhive/pkg/logger"
	toolhivetypes "github.com/stacklok/toolhive/pkg/registry/registry"
)

var (
	// ErrServerNotFound is returned when a server is not found
	ErrServerNotFound = errors.New("server not found")
)

//go:generate mockgen -destination=mocks/mock_service.go -package=mocks -source=service.go Service

// RegistryService defines the interface for registry operations
type RegistryService interface {
	// CheckReadiness checks if the regSvc is ready to serve requests
	CheckReadiness(ctx context.Context) error

	// GetRegistry returns the registry data with metadata
	GetRegistry(ctx context.Context) (*toolhivetypes.UpstreamRegistry, string, error) // returns registry, source, error

	// ListServers returns all servers in the registry
	ListServers(ctx context.Context) ([]upstreamv0.ServerJSON, error)

	// GetServer returns a specific server by name
	GetServer(ctx context.Context, name string) (upstreamv0.ServerJSON, error)

	// ListDeployedServers returns all deployed MCP servers
	ListDeployedServers(ctx context.Context) ([]*DeployedServer, error)

	// GetDeployedServer returns all deployed servers matching the server registry name
	GetDeployedServer(ctx context.Context, name string) ([]*DeployedServer, error)
}

// regSvc implements the RegistryService interface
type regSvc struct {
	registryProvider   RegistryDataProvider
	deploymentProvider DeploymentProvider

	registryData *toolhivetypes.UpstreamRegistry

	lastFetch     time.Time
	cacheDuration time.Duration
}

// Option is a functional option for configuring the regSvc
type Option func(*regSvc)

// WithCacheDuration sets a custom cache duration for registry data
func WithCacheDuration(duration time.Duration) Option {
	return func(s *regSvc) {
		s.cacheDuration = duration
	}
}

// NewService creates a new registry regSvc with the given providers and options.
// registryProvider is required for registry data access.
// deploymentProvider can be nil if deployed servers functionality is not needed.
func NewService(
	ctx context.Context,
	registryProvider RegistryDataProvider,
	deploymentProvider DeploymentProvider,
	opts ...Option,
) (RegistryService, error) {
	if registryProvider == nil {
		return nil, fmt.Errorf("registry data provider is required")
	}

	s := &regSvc{
		registryProvider:   registryProvider,
		deploymentProvider: deploymentProvider,
		cacheDuration:      30 * time.Second, // Default cache duration
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
	totalServers := len(data.Servers)
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
			Version:     "1.0.0",
			LastUpdated: time.Now().Format(time.RFC3339),
			Servers:     make([]upstreamv0.ServerJSON, 0),
		}, source, nil
	}

	return s.registryData, source, nil
}

// ListServers implements RegistryService.ListServers
func (s *regSvc) ListServers(ctx context.Context) ([]upstreamv0.ServerJSON, error) {
	if err := s.refreshDataIfNeeded(ctx); err != nil {
		logger.Warnf("Failed to refresh data: %v", err)
	}

	if s.registryData != nil {
		return s.registryData.Servers, nil
	}

	return []upstreamv0.ServerJSON{}, nil
}

// GetServer implements RegistryService.GetServer
func (s *regSvc) GetServer(ctx context.Context, name string) (upstreamv0.ServerJSON, error) {
	if err := s.refreshDataIfNeeded(ctx); err != nil {
		logger.Warnf("Failed to refresh data: %v", err)
	}

	if s.registryData != nil {
		return s.getServerByName(name)
	}

	return upstreamv0.ServerJSON{}, ErrServerNotFound
}

// ListDeployedServers implements RegistryService.ListDeployedServers
func (s *regSvc) ListDeployedServers(ctx context.Context) ([]*DeployedServer, error) {
	if s.deploymentProvider == nil {
		return []*DeployedServer{}, nil
	}

	return s.deploymentProvider.ListDeployedServers(ctx)
}

// GetDeployedServer implements RegistryService.GetDeployedServer
func (s *regSvc) GetDeployedServer(ctx context.Context, name string) ([]*DeployedServer, error) {
	if s.deploymentProvider == nil {
		return []*DeployedServer{}, nil
	}

	return s.deploymentProvider.GetDeployedServer(ctx, name)
}

// getServerByNameWithName returns a server by name with name properly populated
func (s *regSvc) getServerByName(name string) (upstreamv0.ServerJSON, error) {
	// Check container servers first
	for _, server := range s.registryData.Servers {
		if server.Name == name {
			return server, nil
		}
	}

	return upstreamv0.ServerJSON{}, ErrServerNotFound
}
