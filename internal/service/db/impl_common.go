package database

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/stacklok/toolhive-registry-server/internal/db/sqlc"
	"github.com/stacklok/toolhive-registry-server/internal/service"
	"github.com/stacklok/toolhive-registry-server/internal/versions"
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

// upsertLatestFunc is a callback used by rePointLatestVersionIfNeeded to update
// the latest-version pointer for a specific entry type (MCP server or skill).
type upsertLatestFunc func(
	ctx context.Context,
	querier *sqlc.Queries,
	sourceID uuid.UUID,
	name string,
	version string,
	versionID uuid.UUID,
) error

// rePointLatestVersionIfNeeded checks whether the latest_entry_version pointer
// was cascade-deleted (because the deleted version was the current latest) and,
// if so, re-points it to the next-highest remaining version.
//
// The upsertLatest callback is responsible for writing the new pointer using the
// appropriate SQL query for the entry type (server or skill).
func rePointLatestVersionIfNeeded(
	ctx context.Context,
	querier *sqlc.Queries,
	sourceID uuid.UUID,
	name string,
	entryID uuid.UUID,
	upsertLatest upsertLatestFunc,
) error {
	_, err := querier.GetLatestEntryVersion(ctx, sqlc.GetLatestEntryVersionParams{
		Name:     name,
		SourceID: sourceID,
	})
	if err == nil {
		// Pointer still exists — deleted version was not the latest, nothing to do.
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("failed to check latest version: %w", err)
	}

	// Pointer was cascade-deleted. Find the next-highest remaining version.
	remaining, err := querier.ListEntryVersions(ctx, entryID)
	if err != nil {
		return fmt.Errorf("failed to list remaining versions: %w", err)
	}

	newVersionID, newVersion := findHighestVersion(remaining)
	if newVersionID == uuid.Nil {
		// No remaining versions — the cascade on registry_entry will handle full cleanup.
		return nil
	}

	return upsertLatest(ctx, querier, sourceID, name, newVersion, newVersionID)
}

// findHighestVersion scans a slice of ListEntryVersionsRow and returns the ID
// and version string of the entry with the highest version as determined by
// versions.IsNewerVersion (semantic comparison when both are valid semver,
// otherwise lexicographic string comparison). Returns uuid.Nil, "" if rows is empty.
func findHighestVersion(rows []sqlc.ListEntryVersionsRow) (uuid.UUID, string) {
	if len(rows) == 0 {
		return uuid.Nil, ""
	}
	best := rows[0]
	for _, row := range rows[1:] {
		if versions.IsNewerVersion(row.Version, best.Version) {
			best = row
		}
	}
	return best.ID, best.Version
}
