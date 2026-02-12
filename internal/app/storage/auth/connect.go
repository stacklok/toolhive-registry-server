package auth

import (
	"context"
	"fmt"

	"github.com/stacklok/toolhive-registry-server/internal/config"
)

// MigrationConnectionString builds a PostgreSQL connection string suitable for
// running migrations. It resolves a dynamic auth token (if configured) and
// embeds it in the connection string so that both pgx.Connect and golang-migrate
// (which opens its own internal connection) can authenticate.
//
// When dynamic auth is not configured, the returned string has no password,
// preserving the existing pgpass-based fallback behavior.
func MigrationConnectionString(ctx context.Context, cfg *config.DatabaseConfig) (string, error) {
	if cfg == nil {
		return "", fmt.Errorf("database configuration is required")
	}

	user := cfg.GetMigrationUser()

	token, err := ResolveAuthToken(ctx, cfg, user)
	if err != nil {
		return "", fmt.Errorf("failed to resolve auth token for migration user: %w", err)
	}

	return cfg.BuildConnectionStringWithAuth(user, token), nil
}
