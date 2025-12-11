// Package factory provides factory functions for creating service implementations.
package factory

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/service"
	database "github.com/stacklok/toolhive-registry-server/internal/service/db"
	"github.com/stacklok/toolhive-registry-server/internal/service/inmemory"
)

// NewRegistryService creates a RegistryService based on the configured storage type.
//
// For file-based storage, it returns an in-memory service that uses the provided
// RegistryDataProvider to read registry data from the filesystem.
//
// For database storage, it creates and returns a database-backed service that
// stores registry data directly in PostgreSQL. The pool parameter must not be nil
// when database storage is configured.
//
// Returns an error if database storage is configured but the pool is nil, or if
// file storage is configured but the provider is nil.
func NewRegistryService(
	ctx context.Context,
	cfg *config.Config,
	pool *pgxpool.Pool,
	provider inmemory.RegistryDataProvider,
) (service.RegistryService, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	switch cfg.GetStorageType() {
	case config.StorageTypeDatabase:
		if pool == nil {
			return nil, fmt.Errorf("database pool is required when storage type is database")
		}
		slog.Info("Creating database-backed registry service")
		return database.New(database.WithConnectionPool(pool))

	case config.StorageTypeFile:
		if provider == nil {
			return nil, fmt.Errorf("registry provider is required when storage type is file")
		}
		slog.Info("Creating in-memory registry service")
		return inmemory.New(ctx, provider, inmemory.WithConfig(cfg))

	default:
		return nil, fmt.Errorf("unknown storage type: %s", cfg.GetStorageType())
	}
}
