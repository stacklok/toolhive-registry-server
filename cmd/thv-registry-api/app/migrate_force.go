package app

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/stacklok/toolhive-registry-server/database"
	"github.com/stacklok/toolhive-registry-server/internal/config"
)

var migrateForceCmd = &cobra.Command{
	Use:   "force VERSION",
	Short: "Record a schema version without running migrations",
	Long: `Record VERSION as the current schema version and clear the dirty flag,
without running any migration. Use -1 to record no version.

This is the recovery for a migration that failed part-way and left the schema
version dirty. It does not change the schema: first make the schema match
VERSION by hand (finish or undo the failed migration), then force VERSION.
Forcing a version the schema does not match corrupts later migrations.
See "Recovering from a dirty schema version" in docs/database.md.`,
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE:         runMigrateForce,
}

func runMigrateForce(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	version, err := parseForceVersion(args[0])
	if err != nil {
		return err
	}
	configPath, err := cmd.Flags().GetString("config")
	if err != nil {
		return fmt.Errorf("failed to get config flag: %w", err)
	}
	yes, err := cmd.Flags().GetBool("yes")
	if err != nil {
		return fmt.Errorf("failed to get yes flag: %w", err)
	}

	cfg, err := config.LoadConfig(config.WithConfigPath(configPath))
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	if cfg.Database == nil {
		return fmt.Errorf("database configuration is required")
	}
	connString, err := buildMigrationConnString(ctx, cfg.Database)
	if err != nil {
		return err
	}

	current, dirty, err := database.GetVersion(connString)
	if err != nil {
		return err
	}
	if !yes && !confirmMigrationForce(current, dirty, version, cfg.Database) {
		slog.Info("Force cancelled by user")
		return nil
	}

	if err := database.Force(connString, version); err != nil {
		return err
	}
	slog.Info("Schema version recorded", "previous_version", current, "previous_dirty", dirty, "version", version)
	return nil
}

// parseForceVersion accepts a migration version or -1 for no version.
func parseForceVersion(arg string) (int, error) {
	version, err := strconv.Atoi(arg)
	if err != nil || version < -1 {
		return 0, fmt.Errorf("invalid version %q: want a migration version, or -1 for none", arg)
	}
	return version, nil
}

func confirmMigrationForce(current uint, dirty bool, version int, dbCfg *config.DatabaseConfig) bool {
	slog.Warn("WARNING: This records a schema version without running any migration",
		"current_version", current,
		"current_dirty", dirty,
		"new_version", version,
		"user", dbCfg.GetMigrationUser(),
		"host", dbCfg.Host,
		"port", dbCfg.Port,
		"database", dbCfg.Database)
	slog.Warn("WARNING: The schema must already match the new version, or later migrations will fail or corrupt data")
	fmt.Print("Are you sure you want to continue? Type 'yes' to proceed: ")
	var response string
	if _, err := fmt.Scanln(&response); err != nil {
		return false
	}
	return response == confirmYes
}
