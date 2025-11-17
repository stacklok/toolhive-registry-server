package app

import (
	"fmt"
	"math"

	"github.com/golang-migrate/migrate/v4"
	"github.com/spf13/cobra"
	"github.com/stacklok/toolhive/pkg/logger"

	"github.com/stacklok/toolhive-registry-server/database"
)

var downCmd = &cobra.Command{
	Use:   "down",
	Short: "Migrate the database down",
	Long: `Migrate the database schema down by reverting migrations.
WARNING: This operation can result in data loss. Use with caution.

Examples:
  # Migrate down by 1 step
  thv-registry-api migrate down --config config.yaml --num-steps 1 --yes

  # Migrate down all the way (WARNING: destroys all data)
  thv-registry-api migrate down --config config.yaml --yes`,
	RunE: runMigrateDown,
}

func init() {
	migrateCmd.AddCommand(downCmd)
}

func runMigrateDown(cmd *cobra.Command, _ []string) error {
	cfg, dbConn, err := setupMigration(cmd)
	if err != nil {
		return err
	}
	defer closeDatabaseConnection(dbConn)

	numSteps, err := cmd.Flags().GetUint("num-steps")
	if err != nil {
		return fmt.Errorf("failed to get num-steps flag: %w", err)
	}

	if err := confirmMigrateDown(cmd, numSteps); err != nil {
		return err
	}

	connString, err := cfg.Database.GetConnectionString()
	if err != nil {
		return fmt.Errorf("failed to build connection string: %w", err)
	}

	m, err := database.NewFromConnectionString(connString)
	if err != nil {
		return fmt.Errorf("failed to create migrator: %w", err)
	}

	if err := executeMigrateDown(m, numSteps); err != nil {
		return err
	}

	displayMigrationVersion(m, numSteps)
	return nil
}

func confirmMigrateDown(cmd *cobra.Command, numSteps uint) error {
	yes, err := cmd.Flags().GetBool("yes")
	if err != nil {
		return fmt.Errorf("failed to get yes flag: %w", err)
	}

	if yes {
		return nil
	}

	var prompt string
	if numSteps == 0 {
		prompt = "WARNING: This will migrate down ALL steps and may result in complete data loss. Continue?"
	} else {
		prompt = fmt.Sprintf("WARNING: This will migrate down %d step(s) and may result in data loss. Continue?", numSteps)
	}

	if !confirm(prompt) {
		logger.Info("Migration cancelled")
		return fmt.Errorf("migration cancelled by user")
	}

	return nil
}

func executeMigrateDown(m database.Migrator, numSteps uint) error {
	var err error
	if numSteps == 0 {
		logger.Warn("Migrating down all steps - this will remove all schema!")
		err = m.Down()
	} else {
		logger.Infof("Migrating down %d step(s)...", numSteps)
		// Check for overflow before conversion
		if numSteps > math.MaxInt {
			return fmt.Errorf("number of steps exceeds maximum allowed value")
		}
		err = m.Steps(-1 * int(numSteps)) // #nosec G115 -- overflow checked above
	}

	if err != nil {
		if err == migrate.ErrNoChange {
			logger.Info("No migrations to revert - database is already at the oldest version")
			return nil
		}
		return fmt.Errorf("migration failed: %w", err)
	}

	logger.Info("Migration completed successfully")
	return nil
}

func displayMigrationVersion(m database.Migrator, numSteps uint) {
	version, dirty, err := m.Version()
	if err != nil {
		if numSteps == 0 {
			logger.Info("Database schema has been completely removed")
		} else {
			logger.Warnf("Failed to get migration version: %v", err)
		}
		return
	}

	if dirty {
		logger.Warnf("Current migration version: %d (dirty - manual intervention may be required)", version)
	} else {
		logger.Infof("Current migration version: %d", version)
	}
}
