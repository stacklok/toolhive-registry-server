package state

import (
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/status"
)

// NewStateService creates a RegistryStateService based on the configured storage type.
//
// For file-based storage, it returns a FileStateService that uses the provided
// StatusPersistence for persisting sync status to disk.
//
// For database storage, it returns a DBStateService that stores sync status
// directly in PostgreSQL. The pool parameter must not be nil when database
// storage is configured.
//
// Returns an error if database storage is configured but the pool is nil.
func NewStateService(
	cfg *config.Config,
	statusPersistence status.StatusPersistence,
	pool *pgxpool.Pool,
) (RegistryStateService, error) {
	switch cfg.GetStorageType() {
	case config.StorageTypeDatabase:
		if pool == nil {
			return nil, fmt.Errorf("database pool is required when storage type is database")
		}
		return NewDBStateService(pool), nil
	case config.StorageTypeFile:
		return NewFileStateService(statusPersistence), nil
	default:
		// Default to file-based storage for unknown types
		return NewFileStateService(statusPersistence), nil
	}
}
