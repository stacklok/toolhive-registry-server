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
// Creates two users following the two-user security model:
// - appuser: Application user with limited privileges
// - migratoruser: Migration user with elevated privileges
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

	// Two-user security model
	appUser := "appuser"
	appPassword := "apppass"
	migratorUser := "migratoruser"
	migratorPassword := "migratorpass"

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

	// Create the application user with limited privileges
	_, err = tx.Conn().Exec(ctx, fmt.Sprintf(
		"CREATE USER %s WITH PASSWORD '%s'",
		pgx.Identifier{appUser}.Sanitize(),
		appPassword,
	))
	require.NoError(t, err)

	// Grant the toolhive_registry_server role to the app user
	_, err = tx.Conn().Exec(ctx, fmt.Sprintf(
		"GRANT toolhive_registry_server TO %s",
		pgx.Identifier{appUser}.Sanitize(),
	))
	require.NoError(t, err)

	// Grant connect and schema usage to app user
	_, err = tx.Conn().Exec(ctx, fmt.Sprintf(
		"GRANT CONNECT ON DATABASE %s TO %s",
		pgx.Identifier{dbName}.Sanitize(),
		pgx.Identifier{appUser}.Sanitize(),
	))
	require.NoError(t, err)

	_, err = tx.Conn().Exec(ctx, fmt.Sprintf(
		"GRANT USAGE ON SCHEMA public TO %s",
		pgx.Identifier{appUser}.Sanitize(),
	))
	require.NoError(t, err)

	// Create the migration user with elevated privileges
	_, err = tx.Conn().Exec(ctx, fmt.Sprintf(
		"CREATE USER %s WITH PASSWORD '%s'",
		pgx.Identifier{migratorUser}.Sanitize(),
		migratorPassword,
	))
	require.NoError(t, err)

	// Grant the toolhive_registry_server role to the migrator user
	_, err = tx.Conn().Exec(ctx, fmt.Sprintf(
		"GRANT toolhive_registry_server TO %s",
		pgx.Identifier{migratorUser}.Sanitize(),
	))
	require.NoError(t, err)

	// Grant schema modification privileges for migrations
	_, err = tx.Conn().Exec(ctx, fmt.Sprintf(
		"GRANT CREATE ON SCHEMA public TO %s",
		pgx.Identifier{migratorUser}.Sanitize(),
	))
	require.NoError(t, err)

	// Grant all privileges on the database for migrations
	_, err = tx.Conn().Exec(ctx, fmt.Sprintf(
		"GRANT ALL PRIVILEGES ON DATABASE %s TO %s",
		pgx.Identifier{dbName}.Sanitize(),
		pgx.Identifier{migratorUser}.Sanitize(),
	))
	require.NoError(t, err)

	// Create a temporary pgpass file for tests
	pgpassContent := fmt.Sprintf("%s:%d:%s:%s:%s\n%s:%d:%s:%s:%s\n",
		host, port, dbName, appUser, appPassword,
		host, port, dbName, migratorUser, migratorPassword,
	)
	pgpassFile, err := os.CreateTemp("", "pgpass-test-*")
	require.NoError(t, err)
	_, err = pgpassFile.WriteString(pgpassContent)
	require.NoError(t, err)
	err = pgpassFile.Chmod(0600)
	require.NoError(t, err)
	err = pgpassFile.Close()
	require.NoError(t, err)

	// Set PGPASSFILE environment variable
	os.Setenv("PGPASSFILE", pgpassFile.Name())

	// Create config with database settings (two-user model)
	cfg := &config.Config{
		Database: &config.DatabaseConfig{
			Host:          host,
			Port:          port,
			User:          appUser,
			MigrationUser: migratorUser,
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
		os.Remove(pgpassFile.Name())
		os.Unsetenv("PGPASSFILE")
		cleanupFunc()
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

			_, dirty, err := database.GetVersion(connStr)
			require.NoError(t, err)
			require.False(t, dirty)
		})
	}
}
