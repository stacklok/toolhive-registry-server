package database

import (
	"context"

	"github.com/stacklok/toolhive-registry-server/internal/service"
)

// CreateRegistry creates a new lightweight registry (name + ordered sources).
func (*dbService) CreateRegistry(_ context.Context, _ string, _ *service.RegistryCreateRequest) (*service.RegistryInfo, error) {
	return nil, service.ErrNotImplemented
}

// UpdateRegistry updates an existing lightweight registry.
func (*dbService) UpdateRegistry(_ context.Context, _ string, _ *service.RegistryCreateRequest) (*service.RegistryInfo, error) {
	return nil, service.ErrNotImplemented
}

// DeleteRegistry deletes a lightweight registry.
func (*dbService) DeleteRegistry(_ context.Context, _ string) error {
	return service.ErrNotImplemented
}

// ListRegistries returns all configured registries
func (*dbService) ListRegistries(_ context.Context) ([]service.RegistryInfo, error) {
	return nil, service.ErrNotImplemented
}

// GetRegistryByName returns a single registry by name
func (*dbService) GetRegistryByName(_ context.Context, _ string) (*service.RegistryInfo, error) {
	return nil, service.ErrNotImplemented
}
