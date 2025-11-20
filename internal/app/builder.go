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
	"github.com/stacklok/toolhive/pkg/logger"

	"github.com/stacklok/toolhive-registry-server/internal/api"
	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/service"
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
	sourceHandlerFactory sources.SourceHandlerFactory
	storageManager       sources.StorageManager
	statusPersistence    status.StatusPersistence
	syncManager          pkgsync.Manager
	registryProvider     service.RegistryDataProvider

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
	registryService, err := buildServiceComponents(ctx, cfg)
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

	return &RegistryApp{
		config: cfg.config,
		components: &AppComponents{
			SyncCoordinator: syncCoordinator,
			RegistryService: registryService,
		},
		httpServer: httpServer,
		ctx:        appCtx,
		cancelFunc: cancel,
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

// WithSourceHandlerFactory allows injecting a custom source handler factory (for testing)
func WithSourceHandlerFactory(factory sources.SourceHandlerFactory) RegistryAppOptions {
	return func(cfg *registryAppConfig) error {
		cfg.sourceHandlerFactory = factory
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

	// Build source handler factory
	if b.sourceHandlerFactory == nil {
		b.sourceHandlerFactory = sources.NewSourceHandlerFactory()
	}

	// Build storage manager
	if b.storageManager == nil {
		// Ensure data directory exists
		if err := os.MkdirAll(b.dataDir, 0750); err != nil {
			return nil, fmt.Errorf("failed to create data directory %s: %w", b.dataDir, err)
		}
		b.storageManager = sources.NewFileStorageManager(b.dataDir)
	}

	// Build status persistence
	if b.statusPersistence == nil {
		b.statusPersistence = status.NewFileStatusPersistence(b.statusFile)
	}

	// Build sync manager
	if b.syncManager == nil {
		b.syncManager = pkgsync.NewDefaultSyncManager(
			b.sourceHandlerFactory,
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
) (service.RegistryService, error) {
	logger.Info("Initializing service components")

	// Build registry provider (reads from synced data via StorageManager)
	if b.registryProvider == nil {
		// StorageManager was already built in buildSyncComponents
		factory := service.NewRegistryProviderFactory(b.storageManager)
		provider, err := factory.CreateProvider(b.config)
		if err != nil {
			return nil, fmt.Errorf("failed to create registry provider: %w", err)
		}
		b.registryProvider = provider
		logger.Infof("Created registry data provider using storage manager")
	}

	// Create service (deployment provider is optional and can be injected via WithDeploymentProvider for testing)
	svc, err := inmemory.New(ctx, b.registryProvider)
	if err != nil {
		return nil, fmt.Errorf("failed to create registry service: %w", err)
	}

	logger.Info("Service components initialized successfully")
	return svc, nil
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
