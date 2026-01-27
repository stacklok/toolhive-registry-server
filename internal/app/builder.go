package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"go.opentelemetry.io/otel/metric"

	"github.com/stacklok/toolhive-registry-server/internal/api"
	"github.com/stacklok/toolhive-registry-server/internal/app/storage"
	"github.com/stacklok/toolhive-registry-server/internal/auth"
	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/kubernetes"
	"github.com/stacklok/toolhive-registry-server/internal/service"
	"github.com/stacklok/toolhive-registry-server/internal/sources"
	pkgsync "github.com/stacklok/toolhive-registry-server/internal/sync"
	"github.com/stacklok/toolhive-registry-server/internal/sync/coordinator"
	"github.com/stacklok/toolhive-registry-server/internal/telemetry"
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

// defaultPublicPaths are paths that never require authentication
var defaultPublicPaths = []string{"/health", "/readiness", "/version", "/openapi.json", "/.well-known"}

// RegistryAppOptions is a function that configures the registry app builder
type RegistryAppOptions func(*registryAppConfig) error

// registryAppBuilder builds a RegistryApp using the builder pattern
// It supports dependency injection for testing while providing sensible defaults for production
type registryAppConfig struct {
	config *config.Config

	// Optional component overrides (primarily for testing)
	registryHandlerFactory sources.RegistryHandlerFactory
	syncManager            pkgsync.Manager
	storageFactory         storage.Factory // Replaces: storageManager, statusPersistence, registryProvider

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

	// Auth components
	authMiddleware  func(http.Handler) http.Handler
	authInfoHandler http.Handler

	// Telemetry components
	meterProvider metric.MeterProvider
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

	// Create storage factory (single decision point for DB vs File)
	// This factory creates all storage-dependent components
	if cfg.storageFactory == nil {
		cfg.storageFactory, err = storage.NewStorageFactory(ctx, cfg.config, cfg.dataDir)
		if err != nil {
			return nil, fmt.Errorf("failed to create storage factory: %w", err)
		}
	}

	// Ensure cleanup happens on error
	var cleanupNeeded = true
	defer func() {
		if cleanupNeeded && cfg.storageFactory != nil {
			cfg.storageFactory.Cleanup()
		}
	}()

	// Build sync components using factory
	syncCoordinator, err := buildSyncComponents(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to build sync components: %w", err)
	}

	// Build service components using factory
	registryService, err := buildServiceComponents(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to build service components: %w", err)
	}

	// Build auth middleware (if not injected)
	if cfg.authMiddleware == nil {
		var authErr error
		cfg.authMiddleware, cfg.authInfoHandler, authErr = auth.NewAuthMiddleware(ctx, cfg.config.Auth, auth.DefaultValidatorFactory)
		if authErr != nil {
			return nil, fmt.Errorf("failed to build auth middleware: %w", authErr)
		}
	}

	// Build HTTP server
	httpServer, err := buildHTTPServer(ctx, cfg, registryService)
	if err != nil {
		return nil, fmt.Errorf("failed to build HTTP server: %w", err)
	}

	// Create application context
	appCtx, cancel := context.WithCancel(ctx)

	// Cleanup is now handled by the app, not in defer
	cleanupNeeded = false

	cancelFunc := func() {
		if cfg.storageFactory != nil {
			cfg.storageFactory.Cleanup()
		}
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
func WithRegistryHandlerFactory(f sources.RegistryHandlerFactory) RegistryAppOptions {
	return func(cfg *registryAppConfig) error {
		cfg.registryHandlerFactory = f
		return nil
	}
}

// WithStorageFactory allows injecting a custom storage factory (for testing)
func WithStorageFactory(f storage.Factory) RegistryAppOptions {
	return func(cfg *registryAppConfig) error {
		cfg.storageFactory = f
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

// WithMeterProvider sets the OpenTelemetry meter provider for HTTP metrics
func WithMeterProvider(mp metric.MeterProvider) RegistryAppOptions {
	return func(cfg *registryAppConfig) error {
		cfg.meterProvider = mp
		return nil
	}
}

// buildSyncComponents builds sync manager, coordinator, and related components
func buildSyncComponents(
	ctx context.Context,
	b *registryAppConfig,
) (coordinator.Coordinator, error) {
	slog.Info("Initializing sync components")

	// Build registry handler factory (storage-agnostic)
	if b.registryHandlerFactory == nil {
		b.registryHandlerFactory = sources.NewRegistryHandlerFactory()
	}

	// Create state service using storage factory
	stateService, err := b.storageFactory.CreateStateService(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create state service: %w", err)
	}

	// Build sync manager using storage factory
	if b.syncManager == nil {
		syncWriter, err := b.storageFactory.CreateSyncWriter(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to create sync writer: %w", err)
		}

		b.syncManager = pkgsync.NewDefaultSyncManager(
			b.registryHandlerFactory,
			syncWriter,
		)

		// Setup Kubernetes reconciler if any registry uses Kubernetes source
		for _, reg := range b.config.Registries {
			if reg.GetType() == config.SourceTypeKubernetes {
				_, err := kubernetes.NewMCPServerReconciler(
					ctx,
					kubernetes.WithSyncWriter(syncWriter),
					kubernetes.WithRegistryName(reg.Name),
					// TODO make it configurable
					kubernetes.WithCurrentNamespace(),
				)
				if err != nil {
					return nil, fmt.Errorf("failed to create kubernetes reconciler: %w", err)
				}
				break
			}
		}
	}

	// Create coordinator options for metrics
	var coordOpts []coordinator.Option

	// Create sync metrics if meter provider is configured
	if b.meterProvider != nil {
		syncMetrics, err := telemetry.NewSyncMetrics(b.meterProvider)
		if err != nil {
			return nil, fmt.Errorf("failed to create sync metrics: %w", err)
		}
		if syncMetrics != nil {
			coordOpts = append(coordOpts, coordinator.WithSyncMetrics(syncMetrics))
			slog.Info("Sync metrics enabled")
		}

		registryMetrics, err := telemetry.NewRegistryMetrics(b.meterProvider)
		if err != nil {
			return nil, fmt.Errorf("failed to create registry metrics: %w", err)
		}
		if registryMetrics != nil {
			coordOpts = append(coordOpts, coordinator.WithRegistryMetrics(registryMetrics))
			slog.Info("Registry metrics enabled")
		}
	}

	// Create coordinator (storage-agnostic)
	syncCoordinator := coordinator.New(b.syncManager, stateService, b.config, coordOpts...)
	slog.Info("Sync components initialized successfully")

	return syncCoordinator, nil
}

// buildServiceComponents builds registry service and providers
func buildServiceComponents(
	ctx context.Context,
	b *registryAppConfig,
) (service.RegistryService, error) {
	slog.Info("Initializing service components")

	if b.config == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	// Use storage factory to create the service
	// No storage type checks needed - factory handles everything!
	svc, err := b.storageFactory.CreateRegistryService(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create registry service: %w", err)
	}

	slog.Info("Service components initialized successfully")
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
	slog.Info("Initializing HTTP server")

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

	// Add metrics middleware if meter provider is configured
	// This should be added early in the chain to capture all requests
	if b.meterProvider != nil {
		metricsMiddleware, err := telemetry.MetricsMiddleware(b.meterProvider)
		if err != nil {
			return nil, fmt.Errorf("failed to create metrics middleware: %w", err)
		}
		if metricsMiddleware != nil {
			// Prepend metrics middleware to capture all requests including those rejected by auth
			b.middlewares = append([]func(http.Handler) http.Handler{metricsMiddleware}, b.middlewares...)
			slog.Info("HTTP metrics middleware enabled")
		}
	}

	// Create auth middleware that bypasses public paths
	publicPaths := defaultPublicPaths
	if b.config != nil && b.config.Auth != nil && len(b.config.Auth.PublicPaths) > 0 {
		publicPaths = append(publicPaths, b.config.Auth.PublicPaths...)
	}
	authMw := auth.WrapWithPublicPaths(b.authMiddleware, publicPaths)
	b.middlewares = append(b.middlewares, authMw)

	serverOpts := []api.ServerOption{
		api.WithMiddlewares(b.middlewares...),
		api.WithAuthInfoHandler(b.authInfoHandler),
	}
	if b.config != nil && b.config.EnableAggregatedEndpoints {
		serverOpts = append(serverOpts, api.WithAggregatedEndpoints(true))
	}
	// Create router with middlewares
	router := api.NewServer(svc, serverOpts...)

	// Create HTTP server
	server := &http.Server{
		Addr:         b.address,
		Handler:      router,
		ReadTimeout:  b.readTimeout,
		WriteTimeout: b.writeTimeout,
		IdleTimeout:  b.idleTimeout,
	}

	slog.Info("HTTP server configured", "address", b.address)
	return server, nil
}
