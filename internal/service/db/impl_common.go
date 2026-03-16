package database

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/stacklok/toolhive-registry-server/internal/db/sqlc"
	"github.com/stacklok/toolhive-registry-server/internal/service"
)

// CheckReadiness checks if the service is ready to serve requests
func (s *dbService) CheckReadiness(ctx context.Context) error {
	err := s.pool.Ping(ctx)
	if err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}
	return nil
}

// checkRegistryExists validates that a registry with the given name exists.
// Returns ErrRegistryNotFound if the registry does not exist.
func checkRegistryExists(ctx context.Context, pool sqlc.DBTX, registryName string) error {
	querier := sqlc.New(pool)
	if _, err := querier.GetRegistryByName(ctx, registryName); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: %s", service.ErrRegistryNotFound, registryName)
		}
		return err
	}
	return nil
}
