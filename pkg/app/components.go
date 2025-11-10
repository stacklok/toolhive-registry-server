package app

import (
	"github.com/stacklok/toolhive-registry-server/internal/service"
	"github.com/stacklok/toolhive-registry-server/pkg/sync/coordinator"
)

// AppComponents groups all application components
//
//nolint:revive // This name is fine
type AppComponents struct {
	// SyncCoordinator manages background synchronization
	SyncCoordinator coordinator.Coordinator

	// RegistryService provides registry business logic
	RegistryService service.RegistryService
}
