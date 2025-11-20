package app

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/spf13/cobra"
	"github.com/stacklok/toolhive/pkg/logger"

	"github.com/stacklok/toolhive-registry-server/database"
	"github.com/stacklok/toolhive-registry-server/internal/config"
)

var migrateUpCmd = &cobra.Command{
	Use:   "up",
	Short: "Apply pending database migrations",
	Long: `Apply all pending database migrations to bring the schema up to date.
This command will read the database connection parameters from the config file
and apply all migrations that haven't been run yet.`,
	RunE: runMigrateUp,
}

func runMigrateUp(cmd *cobra.Command, _ []string) error {
	ctx := context.Background()

	// Get flags
	configPath, err := cmd.Flags().GetString("config")
	if err != nil {
		return fmt.Errorf("failed to get config flag: %w", err)
	}

	yes, err := cmd.Flags().GetBool("yes")
	if err != nil {
		return fmt.Errorf("failed to get yes flag: %w", err)
	}

	// Load configuration
	cfg, err := config.LoadConfig(config.WithConfigPath(configPath))
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Validate database configuration
	if cfg.Database == nil {
		return fmt.Errorf("database configuration is required")
	}

	// Get migration connection string (uses migration user if configured)
	connString, err := cfg.Database.GetMigrationConnectionString()
	if err != nil {
		return fmt.Errorf("failed to get migration connection string: %w", err)
	}

	// Get the migration user for display
	migrationUser := cfg.Database.GetMigrationUser()

	// Prompt user if not using --yes flag
	if !yes {
		logger.Infof("About to apply migrations to database: %s@%s:%d/%s (as user: %s)",
			migrationUser, cfg.Database.Host, cfg.Database.Port, cfg.Database.Database, migrationUser)
		fmt.Print("Continue? (yes/no): ")
		var response string
		if _, err := fmt.Scanln(&response); err != nil {
			return fmt.Errorf("failed to read user input: %w", err)
		}
		if response != "yes" && response != "y" {
			logger.Infof("Migration cancelled by user")
			return nil
		}
	}

	// Connect to database
	conn, err := pgx.Connect(ctx, connString)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer func() {
		if closeErr := conn.Close(ctx); closeErr != nil {
			logger.Errorf("Error closing database connection: %v", closeErr)
		}
	}()

	// Run migrations
	logger.Infof("Applying database migrations...")
	if err := database.MigrateUp(ctx, conn); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	// Get current version
	version, dirty, err := database.GetVersion(connString)
	if err != nil {
		logger.Warnf("Unable to get migration version: %v", err)
	} else if dirty {
		logger.Warnf("Database is in a dirty state at version %d", version)
	} else {
		logger.Infof("Migrations applied successfully. Current version: %d", version)
	}

	return nil
}
