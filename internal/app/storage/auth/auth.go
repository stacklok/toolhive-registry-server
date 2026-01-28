// Package auth provides functionality for dynamic database authentication.
package auth

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/stacklok/toolhive-registry-server/internal/config"
)

// NewDynamicAuth creates a new dynamic authentication function based on the configuration.
func NewDynamicAuth(
	ctx context.Context,
	cfg *config.DatabaseConfig,
) (func(ctx context.Context, connConfig *pgx.ConnConfig) error, error) {
	if cfg == nil {
		return nil, fmt.Errorf("database configuration is required")
	}

	if cfg.DynamicAuth == nil {
		return nil, fmt.Errorf("dynamic authentication is not configured")
	}

	// NOTE: if and when more dynamic authentication types are added, we should
	// add a check that ensures only one dynamic authentication type is
	// configured.

	if cfg.DynamicAuth.AWSRDSIAM != nil {
		return awsRdsIamAuthFunc(ctx, cfg)
	}

	return nil, fmt.Errorf("unknown dynamic authentication type: %T", cfg.DynamicAuth)
}
