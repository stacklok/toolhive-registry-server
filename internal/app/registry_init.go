package app

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stacklok/toolhive/pkg/logger"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/db/sqlc"
)

// InitializeManagedRegistries ensures all managed registries from config exist in the database.
// This function is idempotent and safe to call on every startup.
//
// For each managed registry in the config:
// - Creates a database entry with type LOCAL if it doesn't exist
// - Updates the registry type if it already exists
// - Skips non-managed registries (git, api, file sources)
func InitializeManagedRegistries(ctx context.Context, cfg *config.Config, pool *pgxpool.Pool) error {
	if cfg == nil {
		return fmt.Errorf("config is required")
	}

	if pool == nil {
		return fmt.Errorf("database pool is required")
	}

	queries := sqlc.New(pool)
	now := time.Now()

	logger.Info("Initializing managed registries from config")

	managedCount := 0
	for _, regCfg := range cfg.Registries {
		// Skip non-managed registries
		if regCfg.GetType() != config.SourceTypeManaged {
			continue
		}

		managedCount++

		// Upsert the registry entry with type LOCAL
		registry, err := queries.UpsertRegistry(ctx, sqlc.UpsertRegistryParams{
			Name:      regCfg.Name,
			RegType:   sqlc.RegistryTypeLOCAL,
			CreatedAt: &now,
			UpdatedAt: &now,
		})
		if err != nil {
			return fmt.Errorf("failed to upsert managed registry '%s': %w", regCfg.Name, err)
		}

		logger.Infof("Initialized managed registry: %s (id: %s, type: %s)",
			registry.Name, registry.ID, registry.RegType)
	}

	if managedCount == 0 {
		logger.Info("No managed registries found in config")
	} else {
		logger.Infof("Successfully initialized %d managed registr%s",
			managedCount, pluralize(managedCount, "y", "ies"))
	}

	return nil
}

// pluralize returns singular or plural suffix based on count
func pluralize(count int, singular, plural string) string {
	if count == 1 {
		return singular
	}
	return plural
}
