package database

import (
	"context"
	"fmt"

	toolhivetypes "github.com/stacklok/toolhive/pkg/registry/registry"

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

// GetRegistry returns the registry data with metadata
func (*dbService) GetRegistry(
	_ context.Context,
) (*toolhivetypes.UpstreamRegistry, string, error) {
	return nil, "", service.ErrNotImplemented
}
