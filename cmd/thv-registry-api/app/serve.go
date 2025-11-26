package app

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stacklok/toolhive/pkg/logger"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/stacklok/toolhive-registry-server/database"
	registryapp "github.com/stacklok/toolhive-registry-server/internal/app"
	"github.com/stacklok/toolhive-registry-server/internal/config"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the registry API server",
	Long: `Start the registry API server to serve MCP registry data.

The server requires a configuration file (--config) that specifies:
- Registry name and data source (Git, API, or File)
- Sync policy and filtering rules
- All other operational settings

If database configuration is present, migrations will run automatically on startup.

See examples/ directory for sample configurations.`,
	RunE: runServe,
}

const (
	defaultGracefulTimeout = 30 * time.Second // Kubernetes-friendly shutdown time
)

func init() {
	serveCmd.Flags().String("address", ":8080", "Address to listen on")
	serveCmd.Flags().String("config", "", "Path to configuration file (YAML format, required)")

	err := viper.BindPFlag("address", serveCmd.Flags().Lookup("address"))
	if err != nil {
		logger.Fatalf("Failed to bind address flag: %v", err)
	}
	err = viper.BindPFlag("config", serveCmd.Flags().Lookup("config"))
	if err != nil {
		logger.Fatalf("Failed to bind config flag: %v", err)
	}

	// Mark config as required
	if err := serveCmd.MarkFlagRequired("config"); err != nil {
		logger.Fatalf("Failed to mark config flag as required: %v", err)
	}
}

func runServe(_ *cobra.Command, _ []string) error {
	ctx := context.Background()

	// Initialize controller-runtime logger to suppress warnings
	log.SetLogger(zap.New(zap.UseDevMode(false)))

	// Load and validate configuration
	configPath := viper.GetString("config")
	cfg, err := config.LoadConfig(
		config.WithConfigPath(configPath),
	)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	logger.Infof("Loaded configuration from %s (registry: %s, %d registries configured)",
		configPath, cfg.GetRegistryName(), len(cfg.Registries))

	// Run database migrations if database is configured
	if cfg.Database != nil {
		logger.Infof("Database configuration found, running migrations...")
		if err := runMigrations(ctx, cfg); err != nil {
			return fmt.Errorf("failed to run database migrations: %w", err)
		}

		// Initialize managed registries after migrations complete
		if err := initializeManagedRegistries(ctx, cfg); err != nil {
			return fmt.Errorf("failed to initialize managed registries: %w", err)
		}
	}

	// Build application using the builder pattern
	address := viper.GetString("address")
	app, err := registryapp.NewRegistryApp(
		ctx,
		registryapp.WithConfig(cfg),
		registryapp.WithAddress(address),
	)
	if err != nil {
		return fmt.Errorf("failed to build application: %w", err)
	}

	logger.Infof("Starting registry API server on %s", address)

	// Start application in goroutine
	go func() {
		if err := app.Start(); err != nil {
			logger.Fatalf("Application failed: %v", err)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	// Graceful shutdown
	return app.Stop(defaultGracefulTimeout)
}

// runMigrations executes database migrations on startup
func runMigrations(ctx context.Context, cfg *config.Config) error {
	// Get migration connection string (uses migration user if configured)
	connString, err := cfg.Database.GetMigrationConnectionString()
	if err != nil {
		return fmt.Errorf("failed to get migration connection string: %w", err)
	}

	// Log which user is running migrations
	migrationUser := cfg.Database.GetMigrationUser()
	logger.Infof("Running migrations as user: %s", migrationUser)

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

	tx, err := conn.BeginTx(
		ctx,
		pgx.TxOptions{
			IsoLevel:   pgx.Serializable,
			AccessMode: pgx.ReadWrite,
		},
	)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if err := tx.Rollback(ctx); err != nil {
			logger.Errorf("Error rolling back transaction: %v", err)
		}
	}()

	// Run migrations
	logger.Infof("Applying database migrations...")
	if err := database.MigrateUp(ctx, tx.Conn()); err != nil {
		return fmt.Errorf("failed to apply migrations: %w", err)
	}

	// Get and log current version
	version, dirty, err := database.GetVersion(connString)
	if err != nil {
		logger.Warnf("Unable to get migration version: %v", err)
	} else if dirty {
		logger.Warnf("Database is in a dirty state at version %d", version)
	} else {
		logger.Infof("Database migrations completed successfully. Current version: %d", version)
	}

	return nil
}

// initializeManagedRegistries ensures managed registries from config exist in the database
func initializeManagedRegistries(ctx context.Context, cfg *config.Config) error {
	// Get application connection string (uses regular user)
	connString, err := cfg.Database.GetConnectionString()
	if err != nil {
		return fmt.Errorf("failed to get database connection string: %w", err)
	}

	// Create connection pool for initialization
	pool, err := pgxpool.New(ctx, connString)
	if err != nil {
		return fmt.Errorf("failed to create database pool: %w", err)
	}
	defer pool.Close()

	// Initialize managed registries
	return registryapp.InitializeManagedRegistries(ctx, cfg, pool)
}
