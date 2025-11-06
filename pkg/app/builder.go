package app

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/stacklok/toolhive-registry-server/internal/api"
	"github.com/stacklok/toolhive-registry-server/internal/service"
	"github.com/stacklok/toolhive-registry-server/pkg/config"
	"github.com/stacklok/toolhive-registry-server/pkg/sources"
	"github.com/stacklok/toolhive-registry-server/pkg/status"
	pkgsync "github.com/stacklok/toolhive-registry-server/pkg/sync"
	"github.com/stacklok/toolhive-registry-server/pkg/sync/coordinator"
	"github.com/stacklok/toolhive/pkg/logger"
)

const (
	defaultDataDir         = "./data"
	defaultRegistryFile    = "./data/registry.json"
	defaultStatusFile      = "./data/status.json"
	defaultHTTPAddress     = ":8080"
	defaultRequestTimeout  = 10 * time.Second
	defaultReadTimeout     = 10 * time.Second
	defaultWriteTimeout    = 15 * time.Second
	defaultIdleTimeout     = 60 * time.Second
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

	// Kubernetes components (optional)
	kubeConfig *rest.Config
	kubeClient client.Client
	kubeScheme *runtime.Scheme

	// HTTP server options
	address            string
	middlewares        []func(http.Handler) http.Handler
	requestTimeout     time.Duration
	readTimeout        time.Duration
	writeTimeout       time.Duration
	idleTimeout        time.Duration

	// Data directories
	dataDir      string
	registryFile string
	statusFile   string
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

// WithKubernetesConfig sets the Kubernetes REST configuration
func (b *RegistryAppBuilder) WithKubernetesConfig(cfg *rest.Config) *RegistryAppBuilder {
	b.kubeConfig = cfg
	return b
}

// WithKubernetesClient sets the Kubernetes client (for testing)
func (b *RegistryAppBuilder) WithKubernetesClient(client client.Client, scheme *runtime.Scheme) *RegistryAppBuilder {
	b.kubeClient = client
	b.kubeScheme = scheme
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

// Build constructs the RegistryApp from the builder configuration
func (b *RegistryAppBuilder) Build(ctx context.Context) (*RegistryApp, error) {
	// Validate config
	if err := b.config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	// Build Kubernetes components if needed (non-fatal if unavailable)
	kubeComponents, err := b.buildKubernetesComponents()
	if err != nil {
		logger.Warnf("Kubernetes components unavailable: %v", err)
		logger.Warn("ConfigMap source and deployment provider will not be available")
	}

	// Build sync components
	syncCoordinator, err := b.buildSyncComponents(kubeComponents)
	if err != nil {
		return nil, fmt.Errorf("failed to build sync components: %w", err)
	}

	// Build service components
	registryService, err := b.buildServiceComponents(ctx, kubeComponents)
	if err != nil {
		return nil, fmt.Errorf("failed to build service components: %w", err)
	}

	// Build HTTP server
	httpServer, err := b.buildHTTPServer(registryService)
	if err != nil {
		return nil, fmt.Errorf("failed to build HTTP server: %w", err)
	}

	// Create application context
	appCtx, cancel := context.WithCancel(context.Background())

	return &RegistryApp{
		config: b.config,
		components: &AppComponents{
			SyncCoordinator: syncCoordinator,
			RegistryService: registryService,
			KubeComponents:  kubeComponents,
		},
		httpServer: httpServer,
		ctx:        appCtx,
		cancelFunc: cancel,
	}, nil
}

// buildKubernetesComponents builds Kubernetes client, scheme, etc.
// Returns nil if Kubernetes is not available (non-fatal)
func (b *RegistryAppBuilder) buildKubernetesComponents() (*KubernetesComponents, error) {
	// Use injected config if provided (for testing)
	if b.kubeConfig == nil {
		cfg, err := getKubernetesConfig()
		if err != nil {
			return nil, fmt.Errorf("kubernetes config unavailable: %w", err)
		}
		b.kubeConfig = cfg
	}

	// Use injected client if provided (for testing)
	if b.kubeClient == nil {
		client, scheme, err := createKubernetesClient(b.kubeConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
		}
		b.kubeClient = client
		b.kubeScheme = scheme
	}

	logger.Info("Kubernetes components initialized successfully")
	return &KubernetesComponents{
		RestConfig: b.kubeConfig,
		Client:     b.kubeClient,
		Scheme:     b.kubeScheme,
	}, nil
}

// buildSyncComponents builds sync manager, coordinator, and related components
func (b *RegistryAppBuilder) buildSyncComponents(kube *KubernetesComponents) (coordinator.Coordinator, error) {
	logger.Info("Initializing sync components")

	// Build source handler factory
	if b.sourceHandlerFactory == nil {
		var k8sClient client.Client
		if kube != nil {
			k8sClient = kube.Client
		}
		b.sourceHandlerFactory = sources.NewSourceHandlerFactory(k8sClient)
	}

	// Build storage manager
	if b.storageManager == nil {
		// Ensure data directory exists
		if err := os.MkdirAll(b.dataDir, 0755); err != nil {
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
		var k8sClient client.Client
		var scheme *runtime.Scheme
		if kube != nil {
			k8sClient = kube.Client
			scheme = kube.Scheme
		}
		b.syncManager = pkgsync.NewDefaultSyncManager(
			k8sClient,
			scheme,
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
func (b *RegistryAppBuilder) buildServiceComponents(ctx context.Context, kube *KubernetesComponents) (service.RegistryService, error) {
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

	// Build deployment provider (optional, requires Kubernetes)
	if b.deploymentProvider == nil && kube != nil {
		provider, err := service.NewK8sDeploymentProvider(kube.RestConfig, b.config.GetRegistryName())
		if err != nil {
			logger.Warnf("Failed to create deployment provider: %v", err)
		} else {
			b.deploymentProvider = provider
			logger.Infof("Created Kubernetes deployment provider for registry: %s", b.config.GetRegistryName())
		}
	}

	// Create service
	svc, err := service.NewService(ctx, b.registryProvider, b.deploymentProvider)
	if err != nil {
		return nil, fmt.Errorf("failed to create registry service: %w", err)
	}

	logger.Info("Service components initialized successfully")
	return svc, nil
}

// buildHTTPServer builds the HTTP server with router and middleware
func (b *RegistryAppBuilder) buildHTTPServer(svc service.RegistryService) (*http.Server, error) {
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
