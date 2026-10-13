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
	"github.com/stacklok/toolhive-core/postgres"

	"github.com/stacklok/toolhive-registry-server/database"
	registryapp "github.com/stacklok/toolhive-registry-server/internal/app"
	"github.com/stacklok/toolhive-registry-server/internal/app/storage"
	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/shutdown"
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

Database configuration is required. Validated startup applies schema migrations,
but never authorizes exposure of existing data. Existing catalogs require the offline
procedure in docs/registry-exposure-migration.md. For a genuinely new, entirely empty
catalog only, --initialize-empty explicitly authorizes empty-install preparation.
It grants no Cedar permissions and never retires or approves existing data.

See examples/ directory for sample configurations.`,
	SilenceUsage: true,
	RunE:         runServe,
}

const (
	defaultGracefulTimeout = 30 * time.Second // Kubernetes-friendly shutdown time
)

func init() {
	serveCmd.Flags().String("address", ":8080", "Address to listen on")
	serveCmd.Flags().String("internal-address", ":8081", "Address to listen on for internal endpoints (health, readiness, version)")
	serveCmd.Flags().String("config", "", "Path to configuration file (YAML format, required)")
	serveCmd.Flags().String("auth-mode", "", "Override auth mode from config (anonymous or oauth)")
	serveCmd.Flags().Bool("initialize-empty", false,
		"Explicitly authorize a new, entirely empty catalog only; never approve existing data")

	err := viper.BindPFlag("address", serveCmd.Flags().Lookup("address"))
	if err != nil {
		slog.Error("Failed to bind address flag", "error", err)
		os.Exit(1)
	}
	err = viper.BindPFlag("internal-address", serveCmd.Flags().Lookup("internal-address"))
	if err != nil {
		slog.Error("Failed to bind internal-address flag", "error", err)
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

func runServe(cmd *cobra.Command, _ []string) (err error) {
	ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	preparing := true
	defer func() {
		if preparing && ctx.Err() != nil {
			err = shutdown.Result(err)
		}
	}()

	// Decode one YAML/environment snapshot; constructor validates final overrides.
	configPath := viper.GetString("config")
	cfg, err := config.LoadCatalogConfig(
		config.WithConfigPath(configPath),
	)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	authModeOverride, _ := cmd.Flags().GetString("auth-mode")
	initializeEmpty, _ := cmd.Flags().GetBool("initialize-empty")

	slog.Info("Loaded configuration",
		"config_path", configPath,
		"source_count", len(cfg.Sources),
		"registry_count", len(cfg.Registries),
	)

	// Log database configuration (sanitized - no passwords or connection strings)
	if cfg.Database != nil {
		slog.Info("Database configuration",
			"host", cfg.Database.Host,
			"port", cfg.Database.Port,
			"database", cfg.Database.Database,
			"max_open_conns", cfg.Database.MaxOpenConns,
			"max_idle_conns", cfg.Database.MaxIdleConns,
			"conn_max_lifetime", cfg.Database.ConnMaxLifetime,
			"dynamic_auth", cfg.Database.DynamicAuth != nil,
		)
	}

	// Log telemetry configuration
	if cfg.Telemetry != nil {
		slog.Info("Telemetry configuration",
			"enabled", cfg.Telemetry.Enabled,
			"endpoint", cfg.Telemetry.Endpoint,
		)
	}

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

	address := viper.GetString("address")
	internalAddress := viper.GetString("internal-address")
	application, err := registryapp.NewCatalogApp(ctx, registryapp.CatalogOptions{
		Config: cfg, InitializeEmpty: initializeEmpty,
		Overrides: []func(*config.Config) error{func(final *config.Config) error {
			resolveAuthMode(final, authModeOverride)
			return nil
		}},
		Storage: serveCatalogStorage{},
		HTTP: []registryapp.RegistryAppOptions{
			registryapp.WithAddress(address),
			registryapp.WithInternalAddress(internalAddress),
			registryapp.WithMeterProvider(tel.MeterProvider()),
			registryapp.WithTracerProvider(tel.TracerProvider()),
			registryapp.WithMetricsHandler(tel.MetricsHandler()),
		},
	})
	if err != nil {
		return fmt.Errorf("failed to prepare catalog application: %w", err)
	}
	// Run owns shutdown classification; never renormalize its terminal result.
	preparing = false
	return application.Run()
}

// Open is called only after NewCatalogApp validates final config and freezes Cedar.
// The evaluator and review fingerprint both use those same frozen policy bytes.
// Migrations supply schema, never consent; receipt consumption follows preflight.
type serveCatalogStorage struct{}

func (serveCatalogStorage) Open(ctx context.Context, cfg *config.Config) (storage.CatalogStorage, error) {
	if err := runMigrations(ctx, cfg); err != nil {
		return nil, fmt.Errorf("failed to run database migrations: %w", err)
	}
	return (storage.CatalogPostgres{}).Open(ctx, cfg)
}

// buildMigrationConnString builds a connection string for the migration user.
// Precedence: dynamic auth token > config password > PGPASSFILE.
func buildMigrationConnString(ctx context.Context, dbCfg *config.DatabaseConfig) (string, error) {
	corePgCfg, err := dbCfg.ToCorePostgresConfig()
	if err != nil {
		return "", fmt.Errorf("failed to build migration connection string: %w", err)
	}

	user := corePgCfg.GetMigrationUser()

	// Dynamic auth (e.g. AWS RDS IAM) cannot use a pool BeforeConnect hook for
	// the one-shot migration connection, so materialize a single token here.
	token, err := postgres.NewAuthToken(ctx, corePgCfg, user)
	if err != nil {
		return "", fmt.Errorf("failed to build migration connection string: %w", err)
	}
	if token != "" {
		return corePgCfg.BuildConnectionStringWithAuth(user, token), nil
	}

	// Static password / PGPASSFILE fallback.
	return corePgCfg.MigrationConnectionString(), nil
}

// runMigrations executes database migrations on startup
func runMigrations(ctx context.Context, cfg *config.Config) error {
	connString, err := buildMigrationConnString(ctx, cfg.Database)
	if err != nil {
		return err
	}

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

	// Run migrations
	slog.Info("Applying database migrations...")
	if err := database.MigrateUp(ctx, conn); err != nil {
		return fmt.Errorf("failed to apply migrations: %w", err)
	}

	// Get and log new version.
	// Note: This reuses the same connString (and embedded auth token) from above.
	// AWS RDS IAM tokens are valid for 15 minutes, so this is safe for typical
	// migration durations. If the token has expired, GetVersion will fail
	// gracefully (logged as a warning below).
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
