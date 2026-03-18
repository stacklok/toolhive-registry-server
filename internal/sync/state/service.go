// Package state contains logic for managing registry state which the server persists.
package state

import (
	"context"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/status"
)

// RegistryStateService provides methods for inspecting the sync state of a service.
//
//go:generate mockgen -destination=mocks/mock_registry_state_service.go -package=mocks github.com/stacklok/toolhive-registry-server/internal/sync/state RegistryStateService
type RegistryStateService interface {
	// Initialize populates the state store with the set of sources and registries.
	// It is intended that this is called at application startup, and it
	// will overwrite any previous state.
	Initialize(ctx context.Context, cfg *config.Config) error
	// ListSyncStatuses lists all available sync statuses.
	ListSyncStatuses(ctx context.Context) (map[string]*status.SyncStatus, error)
	// GetSyncStatus lists the status of the named source.
	GetSyncStatus(ctx context.Context, registryName string) (*status.SyncStatus, error)
	// UpdateSyncStatus overrides the value of the named source with the syncStatus parameter.
	UpdateSyncStatus(ctx context.Context, registryName string, syncStatus *status.SyncStatus) error
	// GetNextSyncJob returns the next source configuration that needs syncing.
	// The predicate function is used to filter sources based on their config and sync status.
	// The source configs are cached from the Initialize call.
	GetNextSyncJob(
		ctx context.Context,
		predicate func(*config.SourceConfig, *status.SyncStatus) bool,
	) (*config.SourceConfig, error)
}
