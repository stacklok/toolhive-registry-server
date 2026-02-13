// Package auth provides functionality for dynamic database authentication.
package auth

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/stacklok/toolhive-registry-server/internal/app/storage/auth/aws"
	"github.com/stacklok/toolhive-registry-server/internal/config"
)

// NewAuthToken creates a new dynamic authentication token for the given user.
// Returns an empty string if dynamic authentication is not configured.
// The returned token can be used as a password in a PostgreSQL connection string.
// This is useful for short-lived connections (e.g., migrations) where a
// BeforeConnect hook cannot be used.
func NewAuthToken(
	ctx context.Context,
	cfg *config.DatabaseConfig,
	user string,
) (string, error) {
	if cfg == nil {
		return "", fmt.Errorf("database configuration is required")
	}

	if cfg.DynamicAuth == nil {
		return "", nil
	}

	if cfg.DynamicAuth.AWSRDSIAM != nil {
		return aws.NewToken(ctx, cfg, user)
	}

	return "", fmt.Errorf("dynamic auth is configured but no supported auth method (e.g., awsRdsIam) is specified")
}

// NewDynamicAuth creates a new dynamic authentication function based on the configuration.
func NewDynamicAuth(
	ctx context.Context,
	cfg *config.DatabaseConfig,
	user string,
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
		return aws.PgxAuthFunc(ctx, cfg, user)
	}

	return nil, fmt.Errorf("dynamic auth is configured but no supported auth method (e.g., awsRdsIam) is specified")
}
