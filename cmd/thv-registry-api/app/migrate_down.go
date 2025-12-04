package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/spf13/cobra"

	"github.com/stacklok/toolhive-registry-server/database"
	"github.com/stacklok/toolhive-registry-server/internal/config"
)

var migrateDownCmd = &cobra.Command{
	Use:   "down",
	Short: "Revert database migrations",
	Long: `Revert database migrations by rolling back changes.
WARNING: This operation may result in data loss. Use with caution.

By default, this command requires the --num-steps flag to specify how many
migrations to revert. This is a safety measure to prevent accidental data loss.`,
	SilenceUsage: true,
	RunE:         runMigrateDown,
}

func runMigrateDown(cmd *cobra.Command, _ []string) error {
	ctx := context.Background()

	// Parse and validate flags
	numSteps, yes, configPath, err := parseMigrateDownFlags(cmd)
	if err != nil {
		return err
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
	connString := cfg.Database.GetMigrationConnectionString()

	// Prompt user for confirmation if not using --yes flag
	if !yes {
		if !confirmMigrationDown(numSteps, cfg.Database) {
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

	// Run down migrations
	return executeMigrationDown(ctx, conn, connString, numSteps)
}

func parseMigrateDownFlags(cmd *cobra.Command) (uint, bool, string, error) {
	configPath, err := cmd.Flags().GetString("config")
	if err != nil {
		return 0, false, "", fmt.Errorf("failed to get config flag: %w", err)
	}

	yes, err := cmd.Flags().GetBool("yes")
	if err != nil {
		return 0, false, "", fmt.Errorf("failed to get yes flag: %w", err)
	}

	numSteps, err := cmd.Flags().GetUint("num-steps")
	if err != nil {
		return 0, false, "", fmt.Errorf("failed to get num-steps flag: %w", err)
	}

	// Require num-steps for safety
	if numSteps == 0 {
		return 0, false, "", fmt.Errorf("--num-steps flag is required for down migrations (safety measure)")
	}

	// Validate range to prevent integer overflow
	const maxSteps = 1000
	if numSteps > maxSteps {
		return 0, false, "", fmt.Errorf("num-steps must be less than %d", maxSteps)
	}

	return numSteps, yes, configPath, nil
}

func confirmMigrationDown(numSteps uint, dbCfg *config.DatabaseConfig) bool {
	slog.Warn("WARNING: This will revert migrations from database",
		"num_steps", numSteps,
		"user", dbCfg.GetMigrationUser(),
		"host", dbCfg.Host,
		"port", dbCfg.Port,
		"database", dbCfg.Database)
	slog.Warn("WARNING: This operation may result in DATA LOSS")
	fmt.Print("Are you sure you want to continue? Type 'yes' to proceed: ")
	var response string
	if _, err := fmt.Scanln(&response); err != nil {
		return false
	}
	return response == "yes"
}

func executeMigrationDown(ctx context.Context, conn *pgx.Conn, connString string, numSteps uint) error {
	slog.Info("Reverting database migrations", "num_steps", numSteps)
	//nolint:gosec // numSteps is validated to be within safe range
	if err := database.MigrateDown(ctx, conn, int(numSteps)); err != nil {
		return fmt.Errorf("failed to revert migrations: %w", err)
	}

	// Get current version
	version, dirty, err := database.GetVersion(connString)
	if err != nil {
		slog.Warn("Unable to get migration version", "error", err)
	} else if dirty {
		slog.Warn("Database is in a dirty state", "version", version)
	} else {
		slog.Info("Migrations reverted successfully", "version", version)
	}

	return nil
}
