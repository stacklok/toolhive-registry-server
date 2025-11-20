// Package service provides the business logic for the MCP registry API
package service

import (
	"context"
	"errors"

	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
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
}
