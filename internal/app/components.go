package app

import (
	"context"

	"github.com/stacklok/toolhive-registry-server/internal/service"
)

// BackgroundSync runs source synchronization for the app's lifetime.
type BackgroundSync interface {
	// Start runs sync until Stop or ctx ends; it blocks.
	Start(ctx context.Context) error
	// Stop ends sync and waits for in-flight work.
	Stop() error
}

// AppComponents groups all application components
//
//nolint:revive // This name is fine
type AppComponents struct {
	// Sync runs the library's leased source synchronization.
	Sync BackgroundSync

	// RegistryService provides the admin registry business logic
	RegistryService service.RegistryService
}
