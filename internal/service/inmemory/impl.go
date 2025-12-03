// Package inmemory provides an in-memory implementation of the RegistryService interface
package inmemory

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	toolhivetypes "github.com/stacklok/toolhive/pkg/registry/registry"

	"github.com/stacklok/toolhive-registry-server/internal/registry"
	"github.com/stacklok/toolhive-registry-server/internal/service"
)

// regSvc implements the RegistryService interface
type regSvc struct {
	mu               sync.RWMutex // Protects registryData, lastFetch
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
		slog.Warn("Failed to load initial registry data", "error", err)
		// Don't fail regSvc creation, allow it to retry later
	}

	return s, nil
}

// loadRegistryData loads registry data using the configured provider.
// Caller must hold s.mu write lock.
func (s *regSvc) loadRegistryDataLocked(ctx context.Context) error {
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
	slog.Info("Loaded registry data", "server_count", totalServers)
	return nil
}

// loadRegistryData loads registry data using the configured provider
func (s *regSvc) loadRegistryData(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadRegistryDataLocked(ctx)
}

// refreshDataIfNeeded refreshes the registry data if cache has expired
func (s *regSvc) refreshDataIfNeeded(ctx context.Context) error {
	if s.registryProvider == nil {
		return nil // No registry provider configured
	}

	// Check if refresh is needed with read lock first
	s.mu.RLock()
	needsRefresh := time.Since(s.lastFetch) > s.cacheDuration
	hasData := s.registryData != nil
	s.mu.RUnlock()

	if needsRefresh {
		s.mu.Lock()
		defer s.mu.Unlock()
		// Double-check after acquiring write lock
		if time.Since(s.lastFetch) > s.cacheDuration {
			if err := s.loadRegistryDataLocked(ctx); err != nil {
				slog.Warn("Failed to refresh registry data", "error", err)
				// Continue with stale data if available
				if !hasData {
					return err
				}
			}
		}
	}
	return nil
}

// CheckReadiness implements RegistryService.CheckReadiness
func (s *regSvc) CheckReadiness(ctx context.Context) error {
	// Check if we have registry data loaded when a provider is configured
	if s.registryProvider != nil {
		s.mu.RLock()
		hasData := s.registryData != nil
		s.mu.RUnlock()

		if !hasData {
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
		slog.Warn("Failed to refresh data", "error", err)
	}

	// Get source information from the provider
	source := "unknown"
	if s.registryProvider != nil {
		source = s.registryProvider.GetSource()
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

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
	opts ...service.Option[service.ListServersOptions],
) ([]*upstreamv0.ServerJSON, error) {
	if err := s.refreshDataIfNeeded(ctx); err != nil {
		slog.Warn("Failed to refresh data", "error", err)
	}

	// Parse options
	options := &service.ListServersOptions{}
	for _, opt := range opts {
		if err := opt(options); err != nil {
			return nil, err
		}
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.listServersLocked(options)
}

// listServersLocked performs the actual server listing logic.
// Caller must hold s.mu read lock.
func (s *regSvc) listServersLocked(options *service.ListServersOptions) ([]*upstreamv0.ServerJSON, error) {
	// Return empty slice if no data
	if s.registryData == nil {
		return []*upstreamv0.ServerJSON{}, nil
	}

	// Filter by registry name if provided
	registryName := s.registryProvider.GetRegistryName()
	if options.RegistryName != nil && *options.RegistryName != registryName {
		return []*upstreamv0.ServerJSON{}, nil
	}

	// Collect and filter servers
	servers := s.collectAndFilterServers(options.Search)

	// Apply cursor pagination
	servers, err := s.applyCursorPagination(servers, options.Cursor)
	if err != nil {
		return nil, err
	}

	// Apply limit if provided
	if options.Limit > 0 && len(servers) > options.Limit {
		servers = servers[:options.Limit]
	}

	return servers, nil
}

// collectAndFilterServers collects servers and optionally filters by search term.
// Caller must hold s.mu read lock.
func (s *regSvc) collectAndFilterServers(search string) []*upstreamv0.ServerJSON {
	var servers []*upstreamv0.ServerJSON
	for i := range s.registryData.Data.Servers {
		server := &s.registryData.Data.Servers[i]
		if search != "" && !s.serverMatchesSearch(server, search) {
			continue
		}
		servers = append(servers, server)
	}

	if servers == nil {
		servers = []*upstreamv0.ServerJSON{}
	}
	return servers
}

// applyCursorPagination applies cursor-based pagination to the server list.
func (*regSvc) applyCursorPagination(servers []*upstreamv0.ServerJSON, cursor string) ([]*upstreamv0.ServerJSON, error) {
	if cursor == "" {
		return servers, nil
	}

	startIndex, err := decodeCursor(cursor)
	if err != nil {
		return nil, fmt.Errorf("invalid cursor format: %w", err)
	}

	if startIndex >= len(servers) {
		return []*upstreamv0.ServerJSON{}, nil
	}
	if startIndex > 0 {
		servers = servers[startIndex:]
	}
	return servers, nil
}

// ListServerVersions implements RegistryService.ListServerVersions
func (s *regSvc) ListServerVersions(
	ctx context.Context,
	opts ...service.Option[service.ListServerVersionsOptions],
) ([]*upstreamv0.ServerJSON, error) {
	if err := s.refreshDataIfNeeded(ctx); err != nil {
		slog.Warn("Failed to refresh data", "error", err)
	}

	// Parse options
	options := &service.ListServerVersionsOptions{}
	for _, opt := range opts {
		if err := opt(options); err != nil {
			return nil, err
		}
	}

	// Return empty slice if no data
	if s.registryData == nil {
		return []*upstreamv0.ServerJSON{}, nil
	}

	// Filter by registry name if provided
	registryName := s.registryProvider.GetRegistryName()
	if options.RegistryName != nil && *options.RegistryName != registryName {
		// Registry name doesn't match, return empty slice
		return []*upstreamv0.ServerJSON{}, nil
	}

	// Collect servers matching the name (all versions)
	var servers []*upstreamv0.ServerJSON
	for i := range s.registryData.Data.Servers {
		server := &s.registryData.Data.Servers[i]

		// Filter by name if provided
		if options.Name != "" && server.Name != options.Name {
			continue
		}

		servers = append(servers, server)
	}

	// Ensure we return empty slice, not nil
	if servers == nil {
		servers = []*upstreamv0.ServerJSON{}
	}

	// Apply limit if provided
	if options.Limit > 0 && len(servers) > options.Limit {
		servers = servers[:options.Limit]
	}

	return servers, nil
}

// GetServerVersion implements RegistryService.GetServerVersion
func (s *regSvc) GetServerVersion(
	ctx context.Context,
	opts ...service.Option[service.GetServerVersionOptions],
) (*upstreamv0.ServerJSON, error) {
	if err := s.refreshDataIfNeeded(ctx); err != nil {
		slog.Warn("Failed to refresh data", "error", err)
	}

	options := &service.GetServerVersionOptions{}
	for _, opt := range opts {
		if err := opt(options); err != nil {
			return nil, err
		}
	}

	if s.registryData == nil {
		return nil, service.ErrServerNotFound
	}

	// Filter by registry name if provided
	registryName := s.registryProvider.GetRegistryName()
	if options.RegistryName != nil && *options.RegistryName != registryName {
		return nil, service.ErrServerNotFound
	}

	return s.getServerByNameAndVersion(options.Name, options.Version)
}

// PublishServerVersion implements RegistryService.PublishServerVersion
func (s *regSvc) PublishServerVersion(
	ctx context.Context,
	opts ...service.Option[service.PublishServerVersionOptions],
) (*upstreamv0.ServerJSON, error) {
	// Parse options
	options := &service.PublishServerVersionOptions{}
	for _, opt := range opts {
		if err := opt(options); err != nil {
			return nil, err
		}
	}

	if options.ServerData == nil {
		return nil, fmt.Errorf("server data is required")
	}

	serverData := options.ServerData

	// Acquire write lock
	s.mu.Lock()
	defer s.mu.Unlock()

	// Initialize registryData if nil
	if s.registryData == nil {
		s.registryData = &toolhivetypes.UpstreamRegistry{
			Schema:  registry.UpstreamRegistrySchemaURL,
			Version: registry.UpstreamRegistryVersion,
			Meta: toolhivetypes.UpstreamMeta{
				LastUpdated: time.Now().Format(time.RFC3339),
			},
			Data: toolhivetypes.UpstreamData{
				Servers: make([]upstreamv0.ServerJSON, 0),
				Groups:  make([]toolhivetypes.UpstreamGroup, 0),
			},
		}
	}

	// Check registry name matches
	registryName := s.registryProvider.GetRegistryName()
	if options.RegistryName != registryName {
		return nil, fmt.Errorf("%w: %s", service.ErrRegistryNotFound, options.RegistryName)
	}

	// Check for duplicate name+version
	for i := range s.registryData.Data.Servers {
		existing := &s.registryData.Data.Servers[i]
		if existing.Name == serverData.Name && existing.Version == serverData.Version {
			return nil, fmt.Errorf("%w: %s@%s",
				service.ErrVersionAlreadyExists, serverData.Name, serverData.Version)
		}
	}

	// Append the new server
	s.registryData.Data.Servers = append(s.registryData.Data.Servers, *serverData)

	slog.InfoContext(ctx, "Server version published to in-memory registry",
		"registry", options.RegistryName,
		"server", serverData.Name,
		"version", serverData.Version)

	// Return the server data
	return serverData, nil
}

// DeleteServerVersion implements RegistryService.DeleteServerVersion
func (s *regSvc) DeleteServerVersion(
	ctx context.Context,
	opts ...service.Option[service.DeleteServerVersionOptions],
) error {
	// Parse options
	options := &service.DeleteServerVersionOptions{}
	for _, opt := range opts {
		if err := opt(options); err != nil {
			return err
		}
	}

	// Acquire write lock
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.registryData == nil {
		return fmt.Errorf("%w: %s@%s",
			service.ErrServerNotFound, options.ServerName, options.Version)
	}

	// Check registry name matches
	registryName := s.registryProvider.GetRegistryName()
	if options.RegistryName != registryName {
		return fmt.Errorf("%w: %s", service.ErrRegistryNotFound, options.RegistryName)
	}

	// Find and remove the server
	found := false
	filtered := make([]upstreamv0.ServerJSON, 0, len(s.registryData.Data.Servers))
	for _, server := range s.registryData.Data.Servers {
		if server.Name == options.ServerName && server.Version == options.Version {
			found = true
			continue // Skip this server (delete it)
		}
		filtered = append(filtered, server)
	}

	if !found {
		return fmt.Errorf("%w: %s@%s",
			service.ErrServerNotFound, options.ServerName, options.Version)
	}

	s.registryData.Data.Servers = filtered

	slog.InfoContext(ctx, "Server version deleted from in-memory registry",
		"registry", options.RegistryName,
		"server", options.ServerName,
		"version", options.Version)

	return nil
}

// ListRegistries returns all configured registries
func (s *regSvc) ListRegistries(_ context.Context) ([]service.RegistryInfo, error) {
	registryInfo := s.buildRegistryInfo()
	return []service.RegistryInfo{registryInfo}, nil
}

// GetRegistryByName returns a single registry by name
func (s *regSvc) GetRegistryByName(_ context.Context, name string) (*service.RegistryInfo, error) {
	registryName := s.registryProvider.GetRegistryName()
	if name != registryName {
		return nil, service.ErrRegistryNotFound
	}

	registryInfo := s.buildRegistryInfo()
	return &registryInfo, nil
}

// getServerByNameAndVersion returns a server by name and optionally by version.
// If version is empty, returns the first matching server.
func (s *regSvc) getServerByNameAndVersion(name, version string) (*upstreamv0.ServerJSON, error) {
	var firstMatch *upstreamv0.ServerJSON

	for i := range s.registryData.Data.Servers {
		server := &s.registryData.Data.Servers[i]
		if server.Name != name {
			continue
		}

		// If no version specified, return first match
		if version == "" {
			return server, nil
		}

		// Track first match in case we don't find exact version
		if firstMatch == nil {
			firstMatch = server
		}

		// Check for exact version match
		if server.Version == version {
			return server, nil
		}
	}

	// If we found matches but not the exact version, return first match when no version specified
	// Otherwise return not found (we had a version requirement that wasn't met)
	if firstMatch != nil && version == "" {
		return firstMatch, nil
	}

	return nil, service.ErrServerNotFound
}

// serverMatchesSearch performs case-insensitive substring matching on server fields
func (*regSvc) serverMatchesSearch(server *upstreamv0.ServerJSON, search string) bool {
	if server == nil {
		return false
	}

	searchLower := strings.ToLower(search)

	// Check name
	if strings.Contains(strings.ToLower(server.Name), searchLower) {
		return true
	}

	// Check title
	if strings.Contains(strings.ToLower(server.Title), searchLower) {
		return true
	}

	// Check description
	if strings.Contains(strings.ToLower(server.Description), searchLower) {
		return true
	}

	return false
}

// buildRegistryInfo creates a RegistryInfo from the provider configuration
func (s *regSvc) buildRegistryInfo() service.RegistryInfo {
	registryName := s.registryProvider.GetRegistryName()
	source := s.registryProvider.GetSource()

	// Determine registry type based on source prefix
	registryType := "FILE"
	if strings.HasPrefix(source, "git:") {
		registryType = "GIT"
	} else if strings.HasPrefix(source, "http:") || strings.HasPrefix(source, "https:") {
		registryType = "REMOTE"
	}

	// Use lastFetch as a reasonable timestamp (when data was loaded)
	timestamp := s.lastFetch
	if timestamp.IsZero() {
		timestamp = time.Now()
	}

	return service.RegistryInfo{
		Name:       registryName,
		Type:       registryType,
		SyncStatus: nil, // We don't track sync status in-memory
		CreatedAt:  timestamp,
		UpdatedAt:  timestamp,
	}
}

// decodeCursor decodes a base64-encoded cursor string to an index position.
// Returns 0 if the cursor is empty.
func decodeCursor(cursor string) (int, error) {
	if cursor == "" {
		return 0, nil
	}
	decoded, err := base64.StdEncoding.DecodeString(cursor)
	if err != nil {
		return 0, err
	}
	idx, err := strconv.Atoi(string(decoded))
	if err != nil {
		return 0, err
	}
	if idx < 0 {
		return 0, fmt.Errorf("cursor index cannot be negative")
	}
	return idx, nil
}

// EncodeCursor encodes an index position to a base64-encoded cursor string.
// This can be used by callers to generate cursors for pagination.
// For example, after fetching a page of N items starting at index X,
// the next cursor would be EncodeCursor(X + N).
func EncodeCursor(index int) string {
	return base64.StdEncoding.EncodeToString([]byte(strconv.Itoa(index)))
}
