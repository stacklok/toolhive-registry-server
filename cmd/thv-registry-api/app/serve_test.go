package app

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/database"
	"github.com/stacklok/toolhive-registry-server/internal/config"
)

const (
	migratorUser     = "migratoruser"
	migratorPassword = "migratorpass"
	appUser          = "appuser"
	appPassword      = "apppass"
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

	// This statement creates the equivalent of the `postgres` user with all
	// privileges. This is meant to be used as the migration user.
	migratorSQL := fmt.Sprintf(`
		DO $$
		DECLARE
			migrator_user TEXT := '%s';
			migrator_password TEXT := '%s';
			db_name TEXT := '%s';
		BEGIN
			EXECUTE format('CREATE USER %%I WITH PASSWORD %%L', migrator_user, migrator_password);
			EXECUTE format('GRANT CONNECT ON DATABASE %%I TO %%I', db_name, migrator_user);
			EXECUTE format('GRANT CREATE ON SCHEMA public TO %%I', migrator_user);
			EXECUTE format('GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO %%I', migrator_user);
		END
		$$;
	`, migratorUser, migratorPassword, database.DBName)

	_, err = tx.Conn().Exec(ctx, migratorSQL)
	require.NoError(t, err)

	// This statement creates the application user with limited privileges.
	// The user running this statement should be the migrator user.
	appSQL := fmt.Sprintf(`
		DO $$
		DECLARE
			app_user TEXT := '%s';
			app_password TEXT := '%s';
			db_name TEXT := '%s';
		BEGIN
			CREATE ROLE toolhive_registry_server;
			EXECUTE format('CREATE USER %%I WITH PASSWORD %%L', app_user, app_password);
			EXECUTE format('GRANT toolhive_registry_server TO %%I', app_user);
			EXECUTE format('GRANT CONNECT ON DATABASE %%I TO %%I', db_name, app_user);
			EXECUTE format('GRANT USAGE ON SCHEMA public TO %%I', app_user);
			EXECUTE format('GRANT CREATE ON SCHEMA public TO %%I', app_user);
			EXECUTE format('GRANT ALL PRIVILEGES ON DATABASE %%I TO %%I', db_name, app_user);
		END
		$$;
	`, appUser, appPassword, database.DBName)

	_, err = tx.Conn().Exec(ctx, appSQL)
	require.NoError(t, err)

	// Create a temporary pgpass file for tests
	pgpassContent := fmt.Sprintf("%s:%d:%s:%s:%s\n%s:%d:%s:%s:%s\n",
		host, port, database.DBName, appUser, appPassword,
		host, port, database.DBName, migratorUser, migratorPassword,
	)
	pgpassFile, err := os.CreateTemp("", "pgpass-test-*")
	require.NoError(t, err)
	_, err = pgpassFile.WriteString(pgpassContent)
	require.NoError(t, err)
	err = pgpassFile.Chmod(0600)
	require.NoError(t, err)
	err = pgpassFile.Close()
	require.NoError(t, err)

	// Unset PG* environment variables to avoid conflicts with local
	// configuration.
	os.Unsetenv("PGPASSWORD")
	os.Unsetenv("PGUSER")
	os.Unsetenv("PGHOST")
	os.Unsetenv("PGPORT")
	os.Unsetenv("PGDATABASE")
	os.Unsetenv("PGSSLMODE")

	// Set PGPASSFILE environment variable
	os.Setenv("PGPASSFILE", pgpassFile.Name())

	// Create config with database settings (two-user model)
	cfg := &config.Config{
		Database: &config.DatabaseConfig{
			Host:          host,
			Port:          port,
			User:          appUser,
			MigrationUser: migratorUser,
			Database:      database.DBName,
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

func TestResolveAuthMode(t *testing.T) {
	t.Parallel()

	t.Run("nil Auth defaults to oauth", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{}
		resolveAuthMode(cfg, "")
		assert.Equal(t, config.DefaultAuthMode, cfg.Auth.Mode)
	})

	t.Run("override wins over config", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{Auth: &config.AuthConfig{Mode: config.AuthModeOAuth}}
		resolveAuthMode(cfg, "anonymous")
		assert.Equal(t, config.AuthModeAnonymous, cfg.Auth.Mode)
	})

	t.Run("invalid mode is passed through for later validation", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{}
		resolveAuthMode(cfg, "invalid")
		// resolveAuthMode no longer validates - it just sets the value
		// Validation happens in NewAuthMiddleware
		assert.Equal(t, config.AuthMode("invalid"), cfg.Auth.Mode)
	})
}
