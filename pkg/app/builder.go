package app

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/stacklok/toolhive/pkg/logger"

	"github.com/stacklok/toolhive-registry-server/internal/api"
	"github.com/stacklok/toolhive-registry-server/internal/service"
	"github.com/stacklok/toolhive-registry-server/pkg/config"
	"github.com/stacklok/toolhive-registry-server/pkg/sources"
	"github.com/stacklok/toolhive-registry-server/pkg/status"
	pkgsync "github.com/stacklok/toolhive-registry-server/pkg/sync"
	"github.com/stacklok/toolhive-registry-server/pkg/sync/coordinator"
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

// RegistryAppBuilder builds a RegistryApp using the builder pattern
// It supports dependency injection for testing while providing sensible defaults for production
type RegistryAppBuilder struct {
	config *config.Config

	// Optional component overrides (primarily for testing)
	sourceHandlerFactory sources.SourceHandlerFactory
	storageManager       sources.StorageManager
	statusPersistence    status.StatusPersistence
	syncManager          pkgsync.Manager
	registryProvider     service.RegistryDataProvider
	deploymentProvider   service.DeploymentProvider

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

	// Service options
	cacheDuration time.Duration
}

// NewRegistryAppBuilder creates a new builder with the given configuration
func NewRegistryAppBuilder(cfg *config.Config) *RegistryAppBuilder {
	return &RegistryAppBuilder{
		config:         cfg,
		address:        defaultHTTPAddress,
		requestTimeout: defaultRequestTimeout,
		readTimeout:    defaultReadTimeout,
		writeTimeout:   defaultWriteTimeout,
		idleTimeout:    defaultIdleTimeout,
		dataDir:        defaultDataDir,
		registryFile:   defaultRegistryFile,
		statusFile:     defaultStatusFile,
	}
}

// WithAddress sets the HTTP server address
func (b *RegistryAppBuilder) WithAddress(addr string) *RegistryAppBuilder {
	b.address = addr
	return b
}

// WithMiddlewares sets custom HTTP middlewares
func (b *RegistryAppBuilder) WithMiddlewares(mw ...func(http.Handler) http.Handler) *RegistryAppBuilder {
	b.middlewares = mw
	return b
}

// WithDataDirectory sets the data directory for storage and status files
func (b *RegistryAppBuilder) WithDataDirectory(dir string) *RegistryAppBuilder {
	b.dataDir = dir
	b.registryFile = dir + "/registry.json"
	b.statusFile = dir + "/status.json"
	return b
}

// WithSourceHandlerFactory allows injecting a custom source handler factory (for testing)
func (b *RegistryAppBuilder) WithSourceHandlerFactory(factory sources.SourceHandlerFactory) *RegistryAppBuilder {
	b.sourceHandlerFactory = factory
	return b
}

// WithStorageManager allows injecting a custom storage manager (for testing)
func (b *RegistryAppBuilder) WithStorageManager(sm sources.StorageManager) *RegistryAppBuilder {
	b.storageManager = sm
	return b
}

// WithStatusPersistence allows injecting custom status persistence (for testing)
func (b *RegistryAppBuilder) WithStatusPersistence(sp status.StatusPersistence) *RegistryAppBuilder {
	b.statusPersistence = sp
	return b
}

// WithSyncManager allows injecting a custom sync manager (for testing)
func (b *RegistryAppBuilder) WithSyncManager(sm pkgsync.Manager) *RegistryAppBuilder {
	b.syncManager = sm
	return b
}

// WithRegistryProvider allows injecting a custom registry provider (for testing)
func (b *RegistryAppBuilder) WithRegistryProvider(provider service.RegistryDataProvider) *RegistryAppBuilder {
	b.registryProvider = provider
	return b
}

// WithDeploymentProvider allows injecting a custom deployment provider (for testing)
func (b *RegistryAppBuilder) WithDeploymentProvider(provider service.DeploymentProvider) *RegistryAppBuilder {
	b.deploymentProvider = provider
	return b
}

// WithCacheDuration sets a custom cache duration for the registry service
func (b *RegistryAppBuilder) WithCacheDuration(duration time.Duration) *RegistryAppBuilder {
	b.cacheDuration = duration
	return b
}

// Build constructs the RegistryApp from the builder configuration
func (b *RegistryAppBuilder) Build(ctx context.Context) (*RegistryApp, error) {
	// Validate config
	if err := b.config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	// Build sync components
	syncCoordinator, err := b.buildSyncComponents()
	if err != nil {
		return nil, fmt.Errorf("failed to build sync components: %w", err)
	}

	// Build service components
	registryService, err := b.buildServiceComponents(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to build service components: %w", err)
	}

	// Build HTTP server
	httpServer := b.buildHTTPServer(registryService)

	// Create application context
	appCtx, cancel := context.WithCancel(context.Background())

	return &RegistryApp{
		config: b.config,
		components: &AppComponents{
			SyncCoordinator: syncCoordinator,
			RegistryService: registryService,
		},
		httpServer: httpServer,
		ctx:        appCtx,
		cancelFunc: cancel,
	}, nil
}

// buildSyncComponents builds sync manager, coordinator, and related components
func (b *RegistryAppBuilder) buildSyncComponents() (coordinator.Coordinator, error) {
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
func (b *RegistryAppBuilder) buildServiceComponents(ctx context.Context) (service.RegistryService, error) {
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
	var opts []service.Option
	if b.cacheDuration > 0 {
		opts = append(opts, service.WithCacheDuration(b.cacheDuration))
	}
	svc, err := service.NewService(ctx, b.registryProvider, b.deploymentProvider, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create registry service: %w", err)
	}

	logger.Info("Service components initialized successfully")
	return svc, nil
}

// buildHTTPServer builds the HTTP server with router and middleware
func (b *RegistryAppBuilder) buildHTTPServer(svc service.RegistryService) *http.Server {
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
	return server
}
