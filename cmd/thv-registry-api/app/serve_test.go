package app

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/database"
	"github.com/stacklok/toolhive-registry-server/internal/config"
)

// setupBenchmarkDB sets up a test database container for benchmarking
// Returns a config and cleanup function
func setupBenchmarkDB(t *testing.T) (*config.Config, string, func()) {
	t.Helper()

	ctx := context.Background()
	db, cleanupFunc := database.SetupTestDBContainer(t, ctx)

	// Get connection details from the database connection
	connStr := db.Config().ConnString()
	parsedURL, err := url.Parse(connStr)
	require.NoError(t, err)

	tx, err := db.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.Serializable,
		AccessMode: pgx.ReadWrite,
	})
	require.NoError(t, err)
	defer tx.Commit(ctx)

	// Extract connection details
	host := parsedURL.Hostname()
	port := 5432
	if parsedURL.Port() != "" {
		_, err := fmt.Sscanf(parsedURL.Port(), "%d", &port)
		require.NoError(t, err)
	}

	dbName := "testdb"

	user := "appuser"
	password := "apppass"

	// Create the toolhive_registry_server role if it doesn't exist
	// (This role is normally created by migrations, but we need it for the test user)
	// Use DO block to check existence first since CREATE ROLE IF NOT EXISTS may not work in all contexts
	_, err = tx.Conn().Exec(ctx, `
		DO $$
		BEGIN
			IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'toolhive_registry_server') THEN
				CREATE ROLE toolhive_registry_server;
			END IF;
		END
		$$;
	`)
	require.NoError(t, err)

	// Create the application user with the given password
	_, err = tx.Conn().Exec(ctx, fmt.Sprintf(
		"CREATE USER %s WITH PASSWORD '%s'",
		pgx.Identifier{user}.Sanitize(),
		password,
	))
	require.NoError(t, err)

	// Grant the toolhive_registry_server role to the user
	_, err = tx.Conn().Exec(ctx, fmt.Sprintf(
		"GRANT toolhive_registry_server TO %s",
		pgx.Identifier{user}.Sanitize(),
	))
	require.NoError(t, err)

	adminUser := "testuser"
	adminPassword := "testpass"
	os.Setenv("THV_DATABASE_MIGRATION_PASSWORD", adminPassword)

	// Create config with database settings
	cfg := &config.Config{
		Database: &config.DatabaseConfig{
			Host:          host,
			Port:          port,
			User:          user,
			MigrationUser: adminUser,
			Database:      dbName,
			SSLMode:       "disable",
		},
		Registries: []config.RegistryConfig{
			// Add a minimal registry config to satisfy validation
			{
				Name:   "test",
				Format: "toolhive",
				File: &config.FileConfig{
					Path: "./examples/registry-sample.json",
				},
				SyncPolicy: &config.SyncPolicyConfig{
					Interval: "1h",
				},
			},
		},
	}

	return cfg, connStr, func() {
		cleanupFunc()
		os.Unsetenv("THV_DATABASE_MIGRATION_PASSWORD")
	}
}

// TestRunMigrations tests the runMigrations function
func TestRunMigrations(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Set up database container once for all iterations
	cfg, connStr, cleanupFunc := setupBenchmarkDB(t)
	t.Cleanup(cleanupFunc)

	// Run migrations. This was tested with 100 instances without issues.
	for i := range 3 {
		t.Run(fmt.Sprintf("migrations-instance-%d", i), func(t *testing.T) {
			t.Parallel()
			err := runMigrations(ctx, cfg)
			require.NoError(t, err)

			version, dirty, err := database.GetVersion(connStr)
			require.NoError(t, err)
			require.False(t, dirty)
			require.Equal(t, uint(1), version)
		})
	}
}
