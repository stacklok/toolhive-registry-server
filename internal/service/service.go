// Package service provides the business logic for the MCP registry API
package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	toolhivetypes "github.com/stacklok/toolhive/pkg/registry/registry"
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
)

//go:generate mockgen -destination=mocks/mock_service.go -package=mocks -source=service.go Service

// RegistryService defines the interface for registry operations
type RegistryService interface {
	// CheckReadiness checks if the regSvc is ready to serve requests
	CheckReadiness(ctx context.Context) error

	// GetRegistry returns the registry data with metadata
	GetRegistry(ctx context.Context) (*toolhivetypes.UpstreamRegistry, string, error) // returns registry, source, error

	// ListServers returns all servers in the registry
	ListServers(ctx context.Context, opts ...Option[ListServersOptions]) ([]*upstreamv0.ServerJSON, error)

	// ListServerVersions returns all versions of a specific server
	ListServerVersions(ctx context.Context, opts ...Option[ListServerVersionsOptions]) ([]*upstreamv0.ServerJSON, error)

	// GetServer returns a specific server by name
	GetServerVersion(ctx context.Context, opts ...Option[GetServerVersionOptions]) (*upstreamv0.ServerJSON, error)

	// PublishServerVersion publishes a server version to a managed registry
	PublishServerVersion(ctx context.Context, opts ...Option[PublishServerVersionOptions]) (*upstreamv0.ServerJSON, error)

	// DeleteServerVersion removes a server version from a managed registry
	DeleteServerVersion(ctx context.Context, opts ...Option[DeleteServerVersionOptions]) error
}

// Option is a function that sets an option for the ListServersOptions, ListServerVersionsOptions,
// GetServerVersionOptions, PublishServerVersionOptions, or DeleteServerVersionOptions
type Option[
	T ListServersOptions | ListServerVersionsOptions | GetServerVersionOptions |
		PublishServerVersionOptions | DeleteServerVersionOptions,
] func(*T) error

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
func WithCursor(cursor string) Option[ListServersOptions] {
	return func(o *ListServersOptions) error {
		if cursor == "" {
			return fmt.Errorf("invalid cursor: %s", cursor)
		}
		o.Cursor = cursor
		return nil
	}
}

// WithSearch sets the search for the ListServers operation
func WithSearch(search string) Option[ListServersOptions] {
	return func(o *ListServersOptions) error {
		if search == "" {
			return fmt.Errorf("invalid search: %s", search)
		}
		o.Search = search
		return nil
	}
}

// WithUpdatedSince sets the updated since for the ListServers operation
func WithUpdatedSince(updatedSince time.Time) Option[ListServersOptions] {
	return func(o *ListServersOptions) error {
		if updatedSince.IsZero() {
			return fmt.Errorf("invalid updated since: %s", updatedSince)
		}
		o.UpdatedSince = updatedSince
		return nil
	}
}

// WithRegistryName sets the registry name for the ListServers, ListServerVersions,
// GetServerVersion, PublishServerVersion, or DeleteServerVersion operation
func WithRegistryName[
	T ListServersOptions | ListServerVersionsOptions | GetServerVersionOptions |
		PublishServerVersionOptions | DeleteServerVersionOptions,
](
	registryName string,
) Option[T] {
	return func(o *T) error {
		if registryName == "" {
			return fmt.Errorf("invalid registry name: %s", registryName)
		}
		switch o := any(o).(type) {
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
func WithNext(next time.Time) Option[ListServerVersionsOptions] {
	return func(o *ListServerVersionsOptions) error {
		if next.IsZero() {
			return fmt.Errorf("invalid next: %s", next)
		}
		o.Next = &next
		return nil
	}
}

// WithPrev sets the prev time for the ListServerVersions operation
func WithPrev(prev time.Time) Option[ListServerVersionsOptions] {
	return func(o *ListServerVersionsOptions) error {
		if prev.IsZero() {
			return fmt.Errorf("invalid prev: %s", prev)
		}
		o.Prev = &prev
		return nil
	}
}

// WithVersion sets the version for the ListServers, GetServerVersion,
// or DeleteServerVersion operation
func WithVersion[T ListServersOptions | GetServerVersionOptions | DeleteServerVersionOptions](version string) Option[T] {
	return func(o *T) error {
		if version == "" {
			return fmt.Errorf("invalid version: %s", version)
		}

		switch o := any(o).(type) {
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
func WithName[T ListServerVersionsOptions | GetServerVersionOptions | DeleteServerVersionOptions](name string) Option[T] {
	return func(o *T) error {
		if name == "" {
			return fmt.Errorf("invalid name: %s", name)
		}

		switch o := any(o).(type) {
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
func WithLimit[T ListServersOptions | ListServerVersionsOptions](limit int) Option[T] {
	return func(o *T) error {
		if limit <= 0 {
			return fmt.Errorf("invalid limit: %d", limit)
		}

		switch o := any(o).(type) {
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
func WithServerData(serverData *upstreamv0.ServerJSON) Option[PublishServerVersionOptions] {
	return func(o *PublishServerVersionOptions) error {
		if serverData == nil {
			return fmt.Errorf("server data is required")
		}
		o.ServerData = serverData
		return nil
	}
}
