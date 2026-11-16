package app

import (
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/spf13/cobra"

	"github.com/stacklok/toolhive-registry-server/database"
	"github.com/stacklok/toolhive-registry-server/internal/config"
)

var migrateUpCmd = &cobra.Command{
	Use:   "up",
	Short: "Apply pending database migrations",
	Long: `Apply all pending database migrations to bring the schema up to date.
This command will read the database connection parameters from the config file
and apply all migrations that haven't been run yet.`,
	SilenceUsage: true,
	RunE:         runMigrateUp,
}

func parseMigrateUpFlags(cmd *cobra.Command) (string, bool, uint, error) {
	path, err := cmd.Flags().GetString("config")
	if err != nil {
		return "", false, 0, err
	}
	yes, err := cmd.Flags().GetBool("yes")
	if err != nil {
		return "", false, 0, err
	}
	target, err := cmd.Flags().GetUint("to-version")
	if !cmd.Flags().Changed("to-version") {
		target = 30
	}
	return path, yes, target, err
}

func runMigrateUp(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()

	configPath, yes, target, err := parseMigrateUpFlags(cmd)
	if err != nil {
		return err
	}

	// Load configuration
	cfg, err := config.LoadCatalogConfig(config.WithConfigPath(configPath))
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Validate database configuration
	if cfg.Database == nil {
		return fmt.Errorf("database configuration is required")
	}

	connString, err := buildMigrationConnString(ctx, cfg.Database)
	if err != nil {
		return err
	}

	// Prompt user if not using --yes flag
	if !yes {
		slog.Info("About to apply migrations to database",
			"user", cfg.Database.GetMigrationUser(),
			"host", cfg.Database.Host,
			"port", cfg.Database.Port,
			"database", cfg.Database.Database)
		fmt.Print("Continue? (yes/no): ")
		var response string
		if _, err := fmt.Scanln(&response); err != nil {
			return fmt.Errorf("failed to read user input: %w", err)
		}
		if response != "yes" && response != "y" {
			slog.Info("Migration cancelled by user")
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
			slog.Error("Error closing database connection", "error", closeErr)
		}
	}()

	// Run migrations
	slog.Info("Applying database migrations")
	if err := database.MigrateUpTo(ctx, conn, target); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	// Get current version
	version, dirty, err := database.GetVersionContext(ctx, connString)
	if err != nil {
		slog.Warn("Unable to get migration version", "error", err)
	} else if dirty {
		slog.Warn("Database is in a dirty state", "version", version)
	} else {
		slog.Info("Migrations applied successfully", "version", version)
	}

	return nil
}
