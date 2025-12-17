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

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/registry"
	"github.com/stacklok/toolhive-registry-server/internal/service"
)

// regSvc implements the RegistryService interface
type regSvc struct {
	mu               sync.RWMutex // Protects registryData, lastFetch
	registryProvider RegistryDataProvider
	config           *config.Config // Config for registry validation

	// Map of registry name -> registry data
	// Each entry corresponds to one registry from config.Registries
	registryData map[string]*toolhivetypes.UpstreamRegistry

	// Map of registry name -> last fetch time for per-registry caching
	lastFetch     map[string]time.Time
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

// WithConfig sets the config for registry validation
func WithConfig(cfg *config.Config) Option {
	return func(s *regSvc) {
		s.config = cfg
	}
}

// New creates a new registry regSvc with the given providers and options.
// registryProvider is required for registry data access.
// deploymentProvider can be nil if deployed servers functionality is not needed.
func New(
	ctx context.Context,
	registryProvider RegistryDataProvider,
	opts ...Option,
) (service.RegistryService, error) {
	if registryProvider == nil {
		return nil, fmt.Errorf("registry data provider is required")
	}

	s := &regSvc{
		registryProvider: registryProvider,
		config:           nil, // Will be set by WithConfig if provided
		registryData:     make(map[string]*toolhivetypes.UpstreamRegistry),
		lastFetch:        make(map[string]time.Time),
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

// loadRegistryDataLocked loads registry data using the configured provider.
// Caller must hold s.mu write lock.
func (s *regSvc) loadRegistryDataLocked(ctx context.Context) error {
	if s.registryProvider == nil {
		return fmt.Errorf("registry data provider not initialized")
	}

	// Get the merged data from the provider (legacy behavior)
	data, err := s.registryProvider.GetRegistryData(ctx)
	if err != nil {
		return fmt.Errorf("failed to get registry data: %w", err)
	}

	// For each registry in config, initialize its entry
	if s.config != nil {
		for _, regCfg := range s.config.Registries {
			if regCfg.GetType() == config.SourceTypeManaged {
				// MANAGED registries start empty if not already initialized
				if _, exists := s.registryData[regCfg.Name]; !exists {
					s.registryData[regCfg.Name] = &toolhivetypes.UpstreamRegistry{
						Schema:  registry.UpstreamRegistrySchemaURL,
						Version: registry.UpstreamRegistryVersion,
						Meta:    toolhivetypes.UpstreamMeta{},
						Data: toolhivetypes.UpstreamData{
							Servers: make([]upstreamv0.ServerJSON, 0),
							Groups:  make([]toolhivetypes.UpstreamGroup, 0),
						},
					}
					s.lastFetch[regCfg.Name] = time.Now()
				}
			} else {
				// FILE/GIT/API registries get the loaded data
				// For simplicity in this initial version, all non-managed registries share the merged data
				// TODO: In future, storage manager should return per-registry data
				s.registryData[regCfg.Name] = data
				s.lastFetch[regCfg.Name] = time.Now()
			}
		}
	} else {
		// Fallback: no config, use provider's registry name
		defaultName := s.registryProvider.GetRegistryName()
		s.registryData[defaultName] = data
		s.lastFetch[defaultName] = time.Now()
	}

	serverCount := 0
	for _, regData := range s.registryData {
		if regData != nil {
			serverCount += len(regData.Data.Servers)
		}
	}
	slog.InfoContext(ctx, "Loaded registry data", "server_count", serverCount, "registry_count", len(s.registryData))

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
	needsRefresh := s.needsRefresh()
	hasData := len(s.registryData) > 0
	s.mu.RUnlock()

	if needsRefresh {
		s.mu.Lock()
		defer s.mu.Unlock()
		// Double-check after acquiring write lock
		if s.needsRefresh() {
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

// needsRefresh checks if any registry data needs to be refreshed.
// Caller must hold at least s.mu read lock.
func (s *regSvc) needsRefresh() bool {
	// If no data at all, needs refresh
	if len(s.registryData) == 0 {
		return true
	}

	// Check if any non-managed registry has expired cache
	if s.config != nil {
		for _, regCfg := range s.config.Registries {
			// MANAGED registries don't need refresh from external sources
			if regCfg.GetType() == config.SourceTypeManaged {
				continue
			}

			lastFetch, exists := s.lastFetch[regCfg.Name]
			if !exists || time.Since(lastFetch) > s.cacheDuration {
				return true
			}
		}
	} else {
		// No config, check the default registry
		defaultName := s.registryProvider.GetRegistryName()
		lastFetch, exists := s.lastFetch[defaultName]
		if !exists || time.Since(lastFetch) > s.cacheDuration {
			return true
		}
	}

	return false
}

// CheckReadiness implements RegistryService.CheckReadiness
func (s *regSvc) CheckReadiness(ctx context.Context) error {
	// Check if we have registry data loaded when a provider is configured
	if s.registryProvider != nil {
		s.mu.RLock()
		hasData := len(s.registryData) > 0
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

	// Merge all registry data into a single response
	mergedRegistry := s.getMergedRegistryLocked()

	return mergedRegistry, source, nil
}

// getMergedRegistryLocked merges all registry data into a single response.
// Caller must hold s.mu read lock.
func (s *regSvc) getMergedRegistryLocked() *toolhivetypes.UpstreamRegistry {
	if len(s.registryData) == 0 {
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
		}
	}

	// Merge all servers and groups from all registries
	var allServers []upstreamv0.ServerJSON
	var allGroups []toolhivetypes.UpstreamGroup

	for _, regData := range s.registryData {
		if regData != nil {
			allServers = append(allServers, regData.Data.Servers...)
			allGroups = append(allGroups, regData.Data.Groups...)
		}
	}

	if allServers == nil {
		allServers = make([]upstreamv0.ServerJSON, 0)
	}
	if allGroups == nil {
		allGroups = make([]toolhivetypes.UpstreamGroup, 0)
	}

	return &toolhivetypes.UpstreamRegistry{
		Schema:  registry.UpstreamRegistrySchemaURL,
		Version: registry.UpstreamRegistryVersion,
		Meta: toolhivetypes.UpstreamMeta{
			LastUpdated: time.Now().Format(time.RFC3339),
		},
		Data: toolhivetypes.UpstreamData{
			Servers: allServers,
			Groups:  allGroups,
		},
	}
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
	// Collect servers from relevant registries
	var allServers []upstreamv0.ServerJSON

	if options.RegistryName != nil && *options.RegistryName != "" {
		// Filter by specific registry
		regData, exists := s.registryData[*options.RegistryName]
		if !exists {
			return []*upstreamv0.ServerJSON{}, nil
		}
		if regData != nil {
			allServers = regData.Data.Servers
		}
	} else {
		// Merge all registries
		for _, regData := range s.registryData {
			if regData != nil {
				allServers = append(allServers, regData.Data.Servers...)
			}
		}
	}

	// Collect and filter servers
	servers := s.collectAndFilterServers(allServers, options.Search)

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
func (s *regSvc) collectAndFilterServers(allServers []upstreamv0.ServerJSON, search string) []*upstreamv0.ServerJSON {
	var servers []*upstreamv0.ServerJSON
	for i := range allServers {
		server := &allServers[i]
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

	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.listServerVersionsLocked(options), nil
}

// listServerVersionsLocked performs the actual server version listing logic.
// Caller must hold s.mu read lock.
func (s *regSvc) listServerVersionsLocked(
	options *service.ListServerVersionsOptions,
) []*upstreamv0.ServerJSON {
	// Get servers from specific registry or all registries
	allServers := s.collectServersForRegistry(options.RegistryName)

	// Filter by name if provided
	servers := s.filterServersByName(allServers, options.Name)

	// Apply limit if provided
	if options.Limit > 0 && len(servers) > options.Limit {
		servers = servers[:options.Limit]
	}

	return servers
}

// collectServersForRegistry collects servers from the specified registry or all registries.
// Caller must hold s.mu read lock.
func (s *regSvc) collectServersForRegistry(registryName *string) []upstreamv0.ServerJSON {
	var allServers []upstreamv0.ServerJSON

	if registryName != nil && *registryName != "" {
		regData, exists := s.registryData[*registryName]
		if !exists {
			return []upstreamv0.ServerJSON{}
		}
		if regData != nil {
			allServers = regData.Data.Servers
		}
	} else {
		for _, regData := range s.registryData {
			if regData != nil {
				allServers = append(allServers, regData.Data.Servers...)
			}
		}
	}

	return allServers
}

// filterServersByName filters servers by name and returns pointers to matching servers.
func (*regSvc) filterServersByName(allServers []upstreamv0.ServerJSON, name string) []*upstreamv0.ServerJSON {
	var servers []*upstreamv0.ServerJSON
	for i := range allServers {
		server := &allServers[i]

		// Filter by name if provided
		if name != "" && server.Name != name {
			continue
		}

		servers = append(servers, server)
	}

	// Ensure we return empty slice, not nil
	if servers == nil {
		servers = []*upstreamv0.ServerJSON{}
	}

	return servers
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

	s.mu.RLock()
	defer s.mu.RUnlock()

	// Get servers from specific registry or all registries
	var allServers []upstreamv0.ServerJSON
	if options.RegistryName != nil && *options.RegistryName != "" {
		regData, exists := s.registryData[*options.RegistryName]
		if !exists {
			return nil, service.ErrServerNotFound
		}
		if regData != nil {
			allServers = regData.Data.Servers
		}
	} else {
		for _, regData := range s.registryData {
			if regData != nil {
				allServers = append(allServers, regData.Data.Servers...)
			}
		}
	}

	if len(allServers) == 0 {
		return nil, service.ErrServerNotFound
	}

	return s.getServerByNameAndVersion(allServers, options.Name, options.Version)
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

	// Validate that this is a managed registry (before acquiring lock for better performance)
	if _, err := s.validateManagedRegistry(options.RegistryName); err != nil {
		return nil, err
	}

	serverData := options.ServerData

	// Acquire write lock
	s.mu.Lock()
	defer s.mu.Unlock()

	// Get the specific registry data
	regData, exists := s.registryData[options.RegistryName]
	if !exists {
		// Initialize if it doesn't exist (shouldn't happen after validation, but be safe)
		regData = &toolhivetypes.UpstreamRegistry{
			Schema:  registry.UpstreamRegistrySchemaURL,
			Version: registry.UpstreamRegistryVersion,
			Meta:    toolhivetypes.UpstreamMeta{},
			Data: toolhivetypes.UpstreamData{
				Servers: make([]upstreamv0.ServerJSON, 0),
				Groups:  make([]toolhivetypes.UpstreamGroup, 0),
			},
		}
		s.registryData[options.RegistryName] = regData
		s.lastFetch[options.RegistryName] = time.Now()
	}

	// Check for duplicate name+version
	for i := range regData.Data.Servers {
		existing := &regData.Data.Servers[i]
		if existing.Name == serverData.Name && existing.Version == serverData.Version {
			return nil, fmt.Errorf("%w: %s@%s",
				service.ErrVersionAlreadyExists, serverData.Name, serverData.Version)
		}
	}

	// Append the new server
	regData.Data.Servers = append(regData.Data.Servers, *serverData)

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

	// Validate that this is a managed registry (before acquiring lock for better performance)
	if _, err := s.validateManagedRegistry(options.RegistryName); err != nil {
		return err
	}

	// Acquire write lock
	s.mu.Lock()
	defer s.mu.Unlock()

	// Get the specific registry data
	regData, exists := s.registryData[options.RegistryName]
	if !exists || regData == nil {
		return fmt.Errorf("%w: %s@%s",
			service.ErrServerNotFound, options.ServerName, options.Version)
	}

	// Find and remove the server
	found := false
	filtered := make([]upstreamv0.ServerJSON, 0, len(regData.Data.Servers))
	for _, server := range regData.Data.Servers {
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

	regData.Data.Servers = filtered

	slog.InfoContext(ctx, "Server version deleted from in-memory registry",
		"registry", options.RegistryName,
		"server", options.ServerName,
		"version", options.Version)

	return nil
}

// ListRegistries returns all configured registries
func (s *regSvc) ListRegistries(_ context.Context) ([]service.RegistryInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]service.RegistryInfo, 0, len(s.registryData))

	for registryName := range s.registryData {
		// Find the registry config for type info
		regType := s.getRegistryType(registryName)

		timestamp := s.lastFetch[registryName]
		if timestamp.IsZero() {
			timestamp = time.Now()
		}

		result = append(result, service.RegistryInfo{
			Name:       registryName,
			Type:       regType,
			SyncStatus: nil, // We don't track sync status in-memory
			CreatedAt:  timestamp,
			UpdatedAt:  timestamp,
		})
	}

	return result, nil
}

// GetRegistryByName returns a single registry by name
func (s *regSvc) GetRegistryByName(_ context.Context, name string) (*service.RegistryInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	_, exists := s.registryData[name]
	if !exists {
		return nil, service.ErrRegistryNotFound
	}

	// Find the registry config for type info
	regType := s.getRegistryType(name)

	timestamp := s.lastFetch[name]
	if timestamp.IsZero() {
		timestamp = time.Now()
	}

	return &service.RegistryInfo{
		Name:       name,
		Type:       regType,
		SyncStatus: nil,
		CreatedAt:  timestamp,
		UpdatedAt:  timestamp,
	}, nil
}

// CreateRegistry is not supported in the in-memory implementation
func (*regSvc) CreateRegistry(
	_ context.Context,
	_ string,
	_ *service.RegistryCreateRequest,
) (*service.RegistryInfo, error) {
	return nil, service.ErrNotImplemented
}

// UpdateRegistry is not supported in the in-memory implementation
func (*regSvc) UpdateRegistry(
	_ context.Context,
	_ string,
	_ *service.RegistryCreateRequest,
) (*service.RegistryInfo, error) {
	return nil, service.ErrNotImplemented
}

// DeleteRegistry is not supported in the in-memory implementation
func (*regSvc) DeleteRegistry(_ context.Context, _ string) error {
	return service.ErrNotImplemented
}

// ProcessInlineRegistryData is not supported in the in-memory implementation
func (*regSvc) ProcessInlineRegistryData(_ context.Context, _ string, _ string, _ string) error {
	return service.ErrNotImplemented
}

// getRegistryType returns the type of the registry from config or infers it from source.
// Caller must hold s.mu read lock.
func (s *regSvc) getRegistryType(registryName string) string {
	// First, try to get type from config
	if s.config != nil {
		for _, regCfg := range s.config.Registries {
			if regCfg.Name == registryName {
				return strings.ToUpper(regCfg.GetType())
			}
		}
	}

	// Fallback: infer from provider source
	if s.registryProvider != nil {
		source := s.registryProvider.GetSource()
		if strings.HasPrefix(source, "git:") {
			return "GIT"
		}
		if strings.HasPrefix(source, "http:") || strings.HasPrefix(source, "https:") {
			return "REMOTE"
		}
	}

	return "FILE" // default
}

// validateManagedRegistry validates that the registry exists and is a managed registry.
// Returns ErrRegistryNotFound if the registry doesn't exist, or ErrNotManagedRegistry if it's not MANAGED type.
func (s *regSvc) validateManagedRegistry(registryName string) (*config.RegistryConfig, error) {
	if s.config == nil {
		return nil, fmt.Errorf("config not available for registry validation")
	}

	// Find the registry in config
	for i := range s.config.Registries {
		reg := &s.config.Registries[i]
		if reg.Name == registryName {
			// Check if it's a MANAGED registry
			if reg.GetType() != config.SourceTypeManaged {
				return nil, fmt.Errorf("%w: registry %s has type %s",
					service.ErrNotManagedRegistry, registryName, reg.GetType())
			}
			return reg, nil
		}
	}

	return nil, fmt.Errorf("%w: %s", service.ErrRegistryNotFound, registryName)
}

// getServerByNameAndVersion returns a server by name and optionally by version.
// If version is empty, returns the first matching server.
func (*regSvc) getServerByNameAndVersion(
	allServers []upstreamv0.ServerJSON,
	name, version string,
) (*upstreamv0.ServerJSON, error) {
	var firstMatch *upstreamv0.ServerJSON

	for i := range allServers {
		server := &allServers[i]
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
