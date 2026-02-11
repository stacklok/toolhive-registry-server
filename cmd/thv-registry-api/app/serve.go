package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/stacklok/toolhive-registry-server/database"
	registryapp "github.com/stacklok/toolhive-registry-server/internal/app"
	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/telemetry"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the registry API server",
	Long: `Start the registry API server to serve MCP registry data.

The server requires a configuration file (--config) that specifies:
- Registry name and data sources (Git, API, File, Managed, or Kubernetes)
- Sync policy and filtering rules (per registry)
- Authentication configuration
- All other operational settings

Database configuration is required. Migrations run automatically on startup.

See examples/ directory for sample configurations.`,
	SilenceUsage: true,
	RunE:         runServe,
}

const (
	defaultGracefulTimeout = 30 * time.Second // Kubernetes-friendly shutdown time
)

func init() {
	serveCmd.Flags().String("address", ":8080", "Address to listen on")
	serveCmd.Flags().String("config", "", "Path to configuration file (YAML format, required)")
	serveCmd.Flags().String("auth-mode", "", "Override auth mode from config (anonymous or oauth)")

	err := viper.BindPFlag("address", serveCmd.Flags().Lookup("address"))
	if err != nil {
		slog.Error("Failed to bind address flag", "error", err)
		os.Exit(1)
	}
	err = viper.BindPFlag("config", serveCmd.Flags().Lookup("config"))
	if err != nil {
		slog.Error("Failed to bind config flag", "error", err)
		os.Exit(1)
	}

	// Mark config as required
	if err := serveCmd.MarkFlagRequired("config"); err != nil {
		slog.Error("Failed to mark config flag as required", "error", err)
		os.Exit(1)
	}
}

func runServe(cmd *cobra.Command, _ []string) error {
	ctx := context.Background()

	// Load and validate configuration
	configPath := viper.GetString("config")
	cfg, err := config.LoadConfig(
		config.WithConfigPath(configPath),
	)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Resolve auth mode: apply override if specified, then apply default if still empty
	// Priority: flag > config file > default
	authModeOverride, _ := cmd.Flags().GetString("auth-mode")
	resolveAuthMode(cfg, authModeOverride)

	slog.Info("Loaded configuration",
		"config_path", configPath,
		"registry_name", cfg.GetRegistryName(),
		"registry_count", len(cfg.Registries))

	// Initialize telemetry (tracing and metrics)
	tel, err := telemetry.New(ctx, telemetry.WithTelemetryConfig(cfg.Telemetry))
	if err != nil {
		return fmt.Errorf("failed to initialize telemetry: %w", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), defaultGracefulTimeout)
		defer cancel()
		if err := tel.Shutdown(shutdownCtx); err != nil {
			slog.Error("Failed to shutdown telemetry", "error", err)
		}
	}()

	// Database is required — validate before proceeding
	if cfg.Database == nil {
		return fmt.Errorf("database configuration is required")
	}

	// Run database migrations
	slog.Info("Running database migrations")
	if err := runMigrations(ctx, cfg); err != nil {
		return fmt.Errorf("failed to run database migrations: %w", err)
	}

	// Build application using the builder pattern
	address := viper.GetString("address")
	app, err := registryapp.NewRegistryApp(
		ctx,
		registryapp.WithConfig(cfg),
		registryapp.WithAddress(address),
		registryapp.WithMeterProvider(tel.MeterProvider()),
		registryapp.WithTracerProvider(tel.TracerProvider()),
	)
	if err != nil {
		return fmt.Errorf("failed to build application: %w", err)
	}

	slog.Info("Starting registry API server", "address", address)

	// Start application in goroutine
	go func() {
		if err := app.Start(); err != nil {
			slog.Error("Application failed", "error", err)
			os.Exit(1)
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
	connString := cfg.Database.GetMigrationConnectionString()

	// Log which user is running migrations
	slog.Info("Running migrations as user", "user", cfg.Database.GetMigrationUser())

	// Get current version before migrations
	currentVersion, _, versionErr := database.GetVersion(connString)
	if versionErr == nil {
		slog.Info("Current database schema version", "version", currentVersion)
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
		_ = tx.Rollback(ctx)
	}()

	// Run migrations
	slog.Info("Applying database migrations...")
	if err := database.MigrateUp(ctx, tx.Conn()); err != nil {
		return fmt.Errorf("failed to apply migrations: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit migration transaction: %w", err)
	}

	// Get and log new version
	version, dirty, err := database.GetVersion(connString)
	if err != nil {
		slog.Warn("Unable to get migration version", "error", err)
	} else if dirty {
		slog.Warn("Database is in a dirty state", "version", version)
	} else {
		slog.Info("Database migrations completed successfully. Current version", "version", version)
	}

	return nil
}

// resolveAuthMode resolves the final auth mode after all configuration sources have been combined.
// It applies the override if provided, then applies the default if mode is still empty.
// Validation of the mode value is handled by NewAuthMiddleware.
//
// Priority: flag > config file > default
func resolveAuthMode(cfg *config.Config, override string) {
	// Ensure Auth config exists
	if cfg.Auth == nil {
		cfg.Auth = &config.AuthConfig{}
	}

	// Apply override if provided (from --auth-mode flag)
	if override != "" {
		cfg.Auth.Mode = config.AuthMode(override)
		slog.Info("Auth mode overridden", "mode", override)
	}

	// Apply default if mode is still empty
	if cfg.Auth.Mode == "" {
		cfg.Auth.Mode = config.DefaultAuthMode
		slog.Info("Auth mode defaulting", "mode", config.DefaultAuthMode)
	}
}
