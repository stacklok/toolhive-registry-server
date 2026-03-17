package database

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	toolhivetypes "github.com/stacklok/toolhive-core/registry/types"

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

// GetRegistry returns the registry data with metadata
func (*dbService) GetRegistry(
	_ context.Context,
) (*toolhivetypes.UpstreamRegistry, string, error) {
	return nil, "", service.ErrNotImplemented
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
