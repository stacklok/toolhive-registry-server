// Package service provides the business logic for the MCP registry API
package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	toolhivetypes "github.com/stacklok/toolhive/pkg/registry/registry"

	"github.com/stacklok/toolhive-registry-server/internal/config"
)

// Pagination constants used by all RegistryService implementations.
// These values are aligned with the upstream MCP Registry API specification.
const (
	// DefaultPageSize is the default number of items per page when no limit is specified.
	// This matches the upstream MCP Registry API default of 30.
	DefaultPageSize = 30
	// MaxPageSize is the maximum allowed items per page to prevent potential DoS.
	MaxPageSize = 1000
)

var (
	// ErrServerNotFound is returned when a server is not found
	ErrServerNotFound = errors.New("server not found")
	// ErrNotImplemented is returned when a feature is not implemented
	ErrNotImplemented = errors.New("not implemented")
	// ErrRegistryNotFound is returned when a registry is not found
	ErrRegistryNotFound = errors.New("registry not found")
	// ErrNotManagedRegistry is returned when attempting write operations on a non-managed registry
	ErrNotManagedRegistry = errors.New("registry is not managed")
	// ErrVersionAlreadyExists is returned when attempting to publish a version that already exists
	ErrVersionAlreadyExists = errors.New("version already exists")
	// ErrConfigRegistry is returned when attempting to modify a CONFIG-created registry via API
	ErrConfigRegistry = errors.New("cannot modify config-created registry via API")
	// ErrInvalidRegistryConfig is returned when registry configuration is invalid
	ErrInvalidRegistryConfig = errors.New("invalid registry configuration")
	// ErrRegistryAlreadyExists is returned when attempting to create a registry that already exists
	ErrRegistryAlreadyExists = errors.New("registry already exists")
	// ErrSourceTypeChangeNotAllowed is returned when attempting to change a registry's source type
	ErrSourceTypeChangeNotAllowed = errors.New("changing registry source type is not allowed")
)

//go:generate mockgen -destination=mocks/mock_service.go -package=mocks -source=service.go Service

// RegistryService defines the interface for registry operations
type RegistryService interface {
	// CheckReadiness checks if the regSvc is ready to serve requests
	CheckReadiness(ctx context.Context) error

	// GetRegistry returns the registry data with metadata
	GetRegistry(ctx context.Context) (*toolhivetypes.UpstreamRegistry, string, error) // returns registry, source, error

	// ListServers returns all servers in the registry with pagination info
	ListServers(ctx context.Context, opts ...Option) (*ListServersResult, error)

	// ListServerVersions returns all versions of a specific server
	ListServerVersions(ctx context.Context, opts ...Option) ([]*upstreamv0.ServerJSON, error)

	// GetServer returns a specific server by name
	GetServerVersion(ctx context.Context, opts ...Option) (*upstreamv0.ServerJSON, error)

	// PublishServerVersion publishes a server version to a managed registry
	PublishServerVersion(ctx context.Context, opts ...Option) (*upstreamv0.ServerJSON, error)

	// DeleteServerVersion removes a server version from a managed registry
	DeleteServerVersion(ctx context.Context, opts ...Option) error

	// ListRegistries returns all configured registries
	ListRegistries(ctx context.Context) ([]RegistryInfo, error)

	// GetRegistryByName returns a single registry by name
	GetRegistryByName(ctx context.Context, name string) (*RegistryInfo, error)

	// CreateRegistry creates a new API-managed registry
	CreateRegistry(ctx context.Context, name string, req *RegistryCreateRequest) (*RegistryInfo, error)

	// UpdateRegistry updates an existing API-managed registry
	UpdateRegistry(ctx context.Context, name string, req *RegistryCreateRequest) (*RegistryInfo, error)

	// DeleteRegistry deletes an API-managed registry
	DeleteRegistry(ctx context.Context, name string) error

	// ProcessInlineRegistryData processes inline data for a managed/file registry
	ProcessInlineRegistryData(ctx context.Context, name string, data string, format string) error
}

// RegistryInfo represents detailed information about a registry
type RegistryInfo struct {
	Name         string               `json:"name"`
	Type         string               `json:"type"`                   // MANAGED, FILE, REMOTE, KUBERNETES
	CreationType CreationType         `json:"creationType,omitempty"` // API or CONFIG
	SourceType   config.SourceType    `json:"sourceType,omitempty"`   // git, api, file, managed, kubernetes
	Format       string               `json:"format,omitempty"`       // toolhive or upstream
	SourceConfig interface{}          `json:"sourceConfig,omitempty"` // Type-specific source configuration
	FilterConfig *config.FilterConfig `json:"filterConfig,omitempty"` // Filtering rules
	SyncSchedule string               `json:"syncSchedule,omitempty"` // Sync interval string
	SyncStatus   *RegistrySyncStatus  `json:"syncStatus,omitempty"`
	CreatedAt    time.Time            `json:"createdAt"`
	UpdatedAt    time.Time            `json:"updatedAt"`
}

// RegistrySyncStatus represents the sync status of a registry
type RegistrySyncStatus struct {
	Phase        string     `json:"phase"`                  // complete, syncing, failed
	LastSyncTime *time.Time `json:"lastSyncTime,omitempty"` // Last successful sync
	LastAttempt  *time.Time `json:"lastAttempt,omitempty"`  // Last sync attempt
	AttemptCount int        `json:"attemptCount"`           // Number of sync attempts
	ServerCount  int        `json:"serverCount"`            // Number of servers in registry
	Message      string     `json:"message,omitempty"`      // Status or error message
}

// RegistryListResponse represents the response for listing registries
type RegistryListResponse struct {
	Registries []RegistryInfo `json:"registries"`
}

// Option is a function that sets an option for the ListServersOptions, ListServerVersionsOptions,
// GetServerVersionOptions, PublishServerVersionOptions, or DeleteServerVersionOptions
type Option func(any) error

// ListServersOptions is the options for the ListServers operation
type ListServersOptions struct {
	RegistryName *string
	Cursor       string
	Limit        int
	Search       string
	UpdatedSince time.Time
	Version      string
}

// ListServerVersionsOptions is the options for the ListServerVersions operation
type ListServerVersionsOptions struct {
	RegistryName *string
	Name         string
	Next         *time.Time
	Prev         *time.Time
	Limit        int
}

// GetServerVersionOptions is the options for the GetServerVersion operation
type GetServerVersionOptions struct {
	RegistryName *string
	Name         string
	Version      string
}

// PublishServerVersionOptions is the options for the PublishServerVersion operation
type PublishServerVersionOptions struct {
	RegistryName string
	ServerData   *upstreamv0.ServerJSON
}

// DeleteServerVersionOptions is the options for the DeleteServerVersion operation
type DeleteServerVersionOptions struct {
	RegistryName string
	ServerName   string
	Version      string
}

// WithCursor sets the cursor for the ListServers operation
func WithCursor(cursor string) Option {
	return func(o any) error {
		if cursor == "" {
			return fmt.Errorf("invalid cursor: %s", cursor)
		}

		switch o := o.(type) {
		case *ListServersOptions:
			o.Cursor = cursor
		default:
			return fmt.Errorf("invalid option type: %T", o)
		}

		return nil
	}
}

// WithSearch sets the search for the ListServers operation
func WithSearch(search string) Option {
	return func(o any) error {
		if search == "" {
			return fmt.Errorf("invalid search: %s", search)
		}

		switch o := o.(type) {
		case *ListServersOptions:
			o.Search = search
		default:
			return fmt.Errorf("invalid option type: %T", o)
		}

		return nil
	}
}

// WithUpdatedSince sets the updated since for the ListServers operation
func WithUpdatedSince(updatedSince time.Time) Option {
	return func(o any) error {
		if updatedSince.IsZero() {
			return fmt.Errorf("invalid updated since: %s", updatedSince)
		}

		switch o := o.(type) {
		case *ListServersOptions:
			o.UpdatedSince = updatedSince
		default:
			return fmt.Errorf("invalid option type: %T", o)
		}

		return nil
	}
}

// WithRegistryName sets the registry name for the ListServers, ListServerVersions,
// GetServerVersion, PublishServerVersion, or DeleteServerVersion operation
func WithRegistryName(
	registryName string,
) Option {
	return func(o any) error {
		if registryName == "" {
			return fmt.Errorf("invalid registry name: %s", registryName)
		}

		switch o := o.(type) {
		case *ListServersOptions:
			o.RegistryName = &registryName
		case *ListServerVersionsOptions:
			o.RegistryName = &registryName
		case *GetServerVersionOptions:
			o.RegistryName = &registryName
		case *PublishServerVersionOptions:
			o.RegistryName = registryName
		case *DeleteServerVersionOptions:
			o.RegistryName = registryName
		default:
			return fmt.Errorf("invalid option type: %T", o)
		}

		return nil
	}
}

// WithNext sets the next time for the ListServerVersions operation
func WithNext(next time.Time) Option {
	return func(o any) error {
		if next.IsZero() {
			return fmt.Errorf("invalid next: %s", next)
		}

		switch o := o.(type) {
		case *ListServerVersionsOptions:
			o.Next = &next
		default:
			return fmt.Errorf("invalid option type: %T", o)
		}

		return nil
	}
}

// WithPrev sets the prev time for the ListServerVersions operation
func WithPrev(prev time.Time) Option {
	return func(o any) error {
		if prev.IsZero() {
			return fmt.Errorf("invalid prev: %s", prev)
		}

		switch o := o.(type) {
		case *ListServerVersionsOptions:
			o.Prev = &prev
		default:
			return fmt.Errorf("invalid option type: %T", o)
		}

		return nil
	}
}

// WithVersion sets the version for the ListServers, GetServerVersion,
// or DeleteServerVersion operation
func WithVersion(version string) Option {
	return func(o any) error {
		if version == "" {
			return fmt.Errorf("invalid version: %s", version)
		}

		switch o := o.(type) {
		case *ListServersOptions:
			o.Version = version
		case *GetServerVersionOptions:
			o.Version = version
		case *DeleteServerVersionOptions:
			o.Version = version
		default:
			return fmt.Errorf("invalid option type: %T", o)
		}

		return nil
	}
}

// WithName sets the name for the ListServerVersions, GetServerVersion,
// or DeleteServerVersion operation
func WithName(name string) Option {
	return func(o any) error {
		if name == "" {
			return fmt.Errorf("invalid name: %s", name)
		}

		switch o := o.(type) {
		case *ListServerVersionsOptions:
			o.Name = name
		case *GetServerVersionOptions:
			o.Name = name
		case *DeleteServerVersionOptions:
			o.ServerName = name
		default:
			return fmt.Errorf("invalid option type: %T", o)
		}

		return nil
	}
}

// WithLimit sets the limit for the ListServers or ListServerVersions operation
func WithLimit(limit int) Option {
	return func(o any) error {
		if limit <= 0 {
			return fmt.Errorf("invalid limit: %d", limit)
		}

		switch o := o.(type) {
		case *ListServersOptions:
			o.Limit = limit
		case *ListServerVersionsOptions:
			o.Limit = limit
		default:
			return fmt.Errorf("invalid option type: %T", o)
		}

		return nil
	}
}

// WithServerData sets the server data for the PublishServerVersion operation
func WithServerData(serverData *upstreamv0.ServerJSON) Option {
	return func(o any) error {
		if serverData == nil {
			return fmt.Errorf("server data is required")
		}

		switch o := o.(type) {
		case *PublishServerVersionOptions:
			o.ServerData = serverData
		default:
			return fmt.Errorf("invalid option type: %T", o)
		}

		return nil
	}
}

// ListServersResult contains the result of a ListServers operation with pagination info.
// It wraps the server list with cursor-based pagination metadata.
type ListServersResult struct {
	// Servers is the list of servers matching the query
	Servers []*upstreamv0.ServerJSON
	// NextCursor is the cursor to use for fetching the next page of results.
	// Empty string indicates no more results are available.
	NextCursor string
}
