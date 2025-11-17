package app

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	"github.com/spf13/cobra"
	"github.com/stacklok/toolhive/pkg/logger"

	"github.com/stacklok/toolhive-registry-server/database"
	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/db"
)

var upCmd = &cobra.Command{
	Use:   "up",
	Short: "Migrate the database to the latest version",
	Long: `Migrate the database schema up to the latest version or by a specified number of steps.

Examples:
  # Migrate to latest version
  thv-registry-api migrate up --config config.yaml --yes

  # Migrate up by 2 steps
  thv-registry-api migrate up --config config.yaml --num-steps 2 --yes`,
	RunE: runMigrateUp,
}

func init() {
	migrateCmd.AddCommand(upCmd)
}

func runMigrateUp(cmd *cobra.Command, _ []string) error {
	cfg, dbConn, err := setupMigration(cmd)
	if err != nil {
		return err
	}
	defer closeDatabaseConnection(dbConn)

	yes, err := cmd.Flags().GetBool("yes")
	if err != nil {
		return fmt.Errorf("failed to get yes flag: %w", err)
	}

	if !yes && !confirm("Running this command will change the database structure. Continue?") {
		logger.Info("Migration cancelled")
		return nil
	}

	connString := buildConnectionString(cfg.Database)
	m, err := database.NewFromConnectionString(connString)
	if err != nil {
		return fmt.Errorf("failed to create migrator: %w", err)
	}

	numSteps, err := cmd.Flags().GetUint("num-steps")
	if err != nil {
		return fmt.Errorf("failed to get num-steps flag: %w", err)
	}

	if err := executeMigrateUp(m, numSteps); err != nil {
		return err
	}

	displayMigrationInfo(m)
	return nil
}

func executeMigrateUp(m database.Migrator, numSteps uint) error {
	var err error
	if numSteps == 0 {
		logger.Info("Migrating to latest version...")
		err = m.Up()
	} else {
		logger.Infof("Migrating up %d step(s)...", numSteps)
		// Check for overflow before conversion
		if numSteps > math.MaxInt {
			return fmt.Errorf("number of steps exceeds maximum allowed value")
		}
		err = m.Steps(int(numSteps)) // #nosec G115 -- overflow checked above
	}

	if err != nil {
		if err == migrate.ErrNoChange {
			logger.Info("No migrations to apply - database is up to date")
			return nil
		}
		return fmt.Errorf("migration failed: %w", err)
	}

	logger.Info("Migration completed successfully")
	return nil
}

func displayMigrationInfo(m database.Migrator) {
	version, dirty, err := m.Version()
	if err != nil {
		logger.Warnf("Failed to get migration version: %v", err)
		return
	}

	if dirty {
		logger.Warnf("Current migration version: %d (dirty - manual intervention may be required)", version)
	} else {
		logger.Infof("Current migration version: %d", version)
	}
}

// setupMigration loads configuration and establishes a database connection
func setupMigration(cmd *cobra.Command) (*config.Config, *db.Connection, error) {
	configPath := cmd.Flag("config").Value.String()
	cfg, err := config.LoadConfig(config.WithConfigPath(configPath))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load configuration: %w", err)
	}

	if cfg.Database == nil {
		return nil, nil, fmt.Errorf("database configuration is required for migrations")
	}

	logger.Infof("Connecting to database: %s@%s:%d/%s",
		cfg.Database.User, cfg.Database.Host, cfg.Database.Port, cfg.Database.Database)

	dbConn, err := db.NewConnection(cfg.Database)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	return cfg, dbConn, nil
}

// closeDatabaseConnection closes the database connection and logs any errors
func closeDatabaseConnection(dbConn *db.Connection) {
	if dbConn == nil {
		return
	}
	if err := dbConn.Close(); err != nil {
		logger.Errorf("Failed to close database connection: %v", err)
	}
}

// confirm prompts the user for confirmation
func confirm(prompt string) bool {
	reader := bufio.NewReader(os.Stdin)
	fmt.Printf("%s [y/N]: ", prompt)

	response, err := reader.ReadString('\n')
	if err != nil {
		return false
	}

	response = strings.ToLower(strings.TrimSpace(response))
	return response == "y" || response == "yes"
}

// buildConnectionString builds a PostgreSQL connection string from the config
func buildConnectionString(cfg *config.DatabaseConfig) string {
	sslMode := cfg.SSLMode
	if sslMode == "" {
		sslMode = "require"
	}

	return fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=%s",
		cfg.User,
		cfg.Password,
		cfg.Host,
		cfg.Port,
		cfg.Database,
		sslMode,
	)
}
