package app

import (
	"context"
	"fmt"
	"net/http"
	"net/netip"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stacklok/toolhive/pkg/logger"

	"github.com/stacklok/toolhive-registry-server/internal/api"
	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/service"
	database "github.com/stacklok/toolhive-registry-server/internal/service/db"
	"github.com/stacklok/toolhive-registry-server/internal/service/inmemory"
	"github.com/stacklok/toolhive-registry-server/internal/sources"
	"github.com/stacklok/toolhive-registry-server/internal/status"
	pkgsync "github.com/stacklok/toolhive-registry-server/internal/sync"
	"github.com/stacklok/toolhive-registry-server/internal/sync/coordinator"
)

const (
	defaultDataDir        = "./data"
	defaultRegistryFile   = "./data/registry.json"
	defaultStatusFile     = "./data/status.json"
	defaultHTTPAddress    = ":8080"
	defaultRequestTimeout = 10 * time.Second
	defaultReadTimeout    = 10 * time.Second
	defaultWriteTimeout   = 15 * time.Second
	defaultIdleTimeout    = 60 * time.Second
)

// RegistryAppOptions is a function that configures the registry app builder
type RegistryAppOptions func(*registryAppConfig) error

// registryAppBuilder builds a RegistryApp using the builder pattern
// It supports dependency injection for testing while providing sensible defaults for production
type registryAppConfig struct {
	config *config.Config

	// Optional component overrides (primarily for testing)
	registryHandlerFactory sources.RegistryHandlerFactory
	storageManager         sources.StorageManager
	statusPersistence      status.StatusPersistence
	syncManager            pkgsync.Manager
	registryProvider       service.RegistryDataProvider

	// HTTP server options
	address        string
	middlewares    []func(http.Handler) http.Handler
	requestTimeout time.Duration
	readTimeout    time.Duration
	writeTimeout   time.Duration
	idleTimeout    time.Duration

	// Data directories
	dataDir      string
	registryFile string
	statusFile   string
}

func baseConfig(opts ...RegistryAppOptions) (*registryAppConfig, error) {
	cfg := &registryAppConfig{
		address:        defaultHTTPAddress,
		requestTimeout: defaultRequestTimeout,
		readTimeout:    defaultReadTimeout,
		writeTimeout:   defaultWriteTimeout,
		idleTimeout:    defaultIdleTimeout,
		dataDir:        defaultDataDir,
		registryFile:   defaultRegistryFile,
		statusFile:     defaultStatusFile,
	}

	// Apply options
	for _, opt := range opts {
		if err := opt(cfg); err != nil {
			return nil, err
		}
	}

	return cfg, nil
}

// NewRegistryApp creates a new builder with the given configuration
func NewRegistryApp(
	ctx context.Context,
	opts ...RegistryAppOptions,
) (*RegistryApp, error) {
	cfg, err := baseConfig(opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to build base configuration: %w", err)
	}

	// Build sync components
	syncCoordinator, err := buildSyncComponents(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to build sync components: %w", err)
	}

	// Build service components
	registryService, cleanupFunc, err := buildServiceComponents(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to build service components: %w", err)
	}

	// Build HTTP server
	httpServer, err := buildHTTPServer(ctx, cfg, registryService)
	if err != nil {
		return nil, fmt.Errorf("failed to build HTTP server: %w", err)
	}

	// Create application context
	appCtx, cancel := context.WithCancel(ctx)

	cancelFunc := func() {
		cleanupFunc()
		cancel()
	}

	return &RegistryApp{
		config: cfg.config,
		components: &AppComponents{
			SyncCoordinator: syncCoordinator,
			RegistryService: registryService,
		},
		httpServer: httpServer,
		ctx:        appCtx,
		cancelFunc: cancelFunc,
	}, nil
}

// WithConfig sets the configuration
func WithConfig(c *config.Config) RegistryAppOptions {
	return func(cfg *registryAppConfig) error {
		cfg.config = c
		return nil
	}
}

// WithAddress sets the HTTP server address
func WithAddress(addr string) RegistryAppOptions {
	return func(cfg *registryAppConfig) error {
		if addr == "" {
			return fmt.Errorf("address cannot be empty")
		}

		parts := strings.SplitN(addr, ":", 2)
		host := parts[0]
		port := parts[1]

		if port == "" {
			return fmt.Errorf("address is not a valid port: %s", addr)
		}
		if host == "localhost" {
			host = "127.0.0.1"
		}
		if host == "" {
			host = "0.0.0.0"
		}

		if _, err := netip.ParseAddrPort(host + ":" + port); err != nil {
			return fmt.Errorf("address is not a valid port: %w", err)
		}

		cfg.address = addr
		return nil
	}
}

// WithMiddlewares sets custom HTTP middlewares
func WithMiddlewares(mw ...func(http.Handler) http.Handler) RegistryAppOptions {
	return func(cfg *registryAppConfig) error {
		cfg.middlewares = mw
		return nil
	}
}

// WithDataDirectory sets the data directory for storage and status files
func WithDataDirectory(dir string) RegistryAppOptions {
	return func(cfg *registryAppConfig) error {
		cfg.dataDir = dir
		cfg.registryFile = dir + "/registry.json"
		cfg.statusFile = dir + "/status.json"
		return nil
	}
}

// WithRegistryHandlerFactory allows injecting a custom registry handler factory (for testing)
func WithRegistryHandlerFactory(factory sources.RegistryHandlerFactory) RegistryAppOptions {
	return func(cfg *registryAppConfig) error {
		cfg.registryHandlerFactory = factory
		return nil
	}
}

// WithStorageManager allows injecting a custom storage manager (for testing)
func WithStorageManager(sm sources.StorageManager) RegistryAppOptions {
	return func(cfg *registryAppConfig) error {
		cfg.storageManager = sm
		return nil
	}
}

// WithStatusPersistence allows injecting custom status persistence (for testing)
func WithStatusPersistence(sp status.StatusPersistence) RegistryAppOptions {
	return func(cfg *registryAppConfig) error {
		cfg.statusPersistence = sp
		return nil
	}
}

// WithSyncManager allows injecting a custom sync manager (for testing)
func WithSyncManager(sm pkgsync.Manager) RegistryAppOptions {
	return func(cfg *registryAppConfig) error {
		cfg.syncManager = sm
		return nil
	}
}

// WithRegistryProvider allows injecting a custom registry provider (for testing)
func WithRegistryProvider(provider service.RegistryDataProvider) RegistryAppOptions {
	return func(cfg *registryAppConfig) error {
		cfg.registryProvider = provider
		return nil
	}
}

// buildSyncComponents builds sync manager, coordinator, and related components
func buildSyncComponents(
	b *registryAppConfig,
) (coordinator.Coordinator, error) {
	logger.Info("Initializing sync components")

	// Build registry handler factory
	if b.registryHandlerFactory == nil {
		b.registryHandlerFactory = sources.NewRegistryHandlerFactory()
	}

	// Build storage manager
	if b.storageManager == nil {
		// Use config's file storage base directory (defaults to "./data")
		baseDir := b.config.GetFileStorageBaseDir()
		// Ensure data directory exists
		if err := os.MkdirAll(baseDir, 0750); err != nil {
			return nil, fmt.Errorf("failed to create data directory %s: %w", baseDir, err)
		}
		b.storageManager = sources.NewFileStorageManager(baseDir)
	}

	// Build status persistence (now uses dataDir as base path for per-registry status files)
	if b.statusPersistence == nil {
		b.statusPersistence = status.NewFileStatusPersistence(b.dataDir)
	}

	// Build sync manager
	if b.syncManager == nil {
		b.syncManager = pkgsync.NewDefaultSyncManager(
			b.registryHandlerFactory,
			b.storageManager,
		)
	}

	// Create coordinator
	syncCoordinator := coordinator.New(b.syncManager, b.statusPersistence, b.config)
	logger.Info("Sync components initialized successfully")

	return syncCoordinator, nil
}

// buildServiceComponents builds registry service and providers
func buildServiceComponents(
	ctx context.Context,
	b *registryAppConfig,
) (service.RegistryService, func(), error) {
	logger.Info("Initializing service components")

	if b.config == nil {
		return nil, nil, fmt.Errorf("config cannot be nil")
	}

	// Determine storage type from config
	storageType := b.config.GetStorageType()

	// Create service based on storage type
	var svc service.RegistryService
	var cleanupFunc func()
	switch storageType {
	case config.StorageTypeFile:
		// Build registry provider (reads from synced data via StorageManager)
		if b.registryProvider == nil {
			// StorageManager was already built in buildSyncComponents
			factory := service.NewRegistryProviderFactory(b.storageManager)
			provider, err := factory.CreateProvider(b.config)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to create registry provider: %w", err)
			}
			b.registryProvider = provider
			logger.Infof("Created registry data provider using storage manager")
		}

		// Create in-memory service (reads from file storage)
		inMemorySvc, err := inmemory.New(ctx, b.registryProvider)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create in-memory registry service: %w", err)
		}
		logger.Info("Created in-memory registry service")

		svc = inMemorySvc
		cleanupFunc = func() {}
	case config.StorageTypeDatabase:
		// Create database-backed service
		pool, err := buildDatabaseConnectionPool(ctx, b.config)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create database service: %w", err)
		}

		databaseSvc, err := database.New(database.WithConnectionPool(pool))
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create database service: %w", err)
		}
		logger.Info("Created database-backed registry service")

		svc = databaseSvc
		cleanupFunc = pool.Close
	default:
		return nil, nil, fmt.Errorf("unknown storage type: %s", storageType)
	}

	logger.Info("Service components initialized successfully")
	return svc, cleanupFunc, nil
}

// buildDatabaseConnectionPool creates a database connection pool
func buildDatabaseConnectionPool(
	ctx context.Context,
	cfg *config.Config,
) (*pgxpool.Pool, error) {
	if cfg.Database == nil {
		return nil, fmt.Errorf("database configuration is required for database storage type")
	}

	// Get connection string from config
	connStr, err := cfg.Database.GetConnectionString()
	if err != nil {
		return nil, fmt.Errorf("failed to get database connection string: %w", err)
	}

	// Parse connection string into config
	poolConfig, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse database connection string: %w", err)
	}

	// Configure pool settings from config
	if cfg.Database.MaxOpenConns > 0 {
		poolConfig.MaxConns = int32(cfg.Database.MaxOpenConns)
	}
	if cfg.Database.MaxIdleConns > 0 {
		poolConfig.MinConns = int32(cfg.Database.MaxIdleConns)
	}
	if cfg.Database.ConnMaxLifetime != "" {
		lifetime, err := time.ParseDuration(cfg.Database.ConnMaxLifetime)
		if err != nil {
			return nil, fmt.Errorf("failed to parse connMaxLifetime: %w", err)
		}
		poolConfig.MaxConnLifetime = lifetime
	}

	// Create connection pool
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create database connection pool: %w", err)
	}

	return pool, nil
}

// buildHTTPServer builds the HTTP server with router and middleware
//
//nolint:unparam // we prefer having a similar interface
func buildHTTPServer(
	_ context.Context,
	b *registryAppConfig,
	svc service.RegistryService,
) (*http.Server, error) {
	logger.Info("Initializing HTTP server")

	// Use default middlewares if not provided
	if b.middlewares == nil {
		b.middlewares = []func(http.Handler) http.Handler{
			middleware.RequestID,
			middleware.RealIP,
			middleware.Recoverer,
			middleware.Timeout(b.requestTimeout),
			api.LoggingMiddleware,
		}
	}

	// Create router with middlewares
	router := api.NewServer(svc, api.WithMiddlewares(b.middlewares...))

	// Create HTTP server
	server := &http.Server{
		Addr:         b.address,
		Handler:      router,
		ReadTimeout:  b.readTimeout,
		WriteTimeout: b.writeTimeout,
		IdleTimeout:  b.idleTimeout,
	}

	logger.Infof("HTTP server configured on %s", b.address)
	return server, nil
}
