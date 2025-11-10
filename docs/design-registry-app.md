# Design: RegistryApp Refactoring

## Problem Statement

Currently, `cmd/thv-registry-api/app/serve.go` creates many clients, schemes, and variables directly in the `runServe()` function. This has several issues:

1. **Hard to test** - Cannot inject mocks for unit testing
2. **Poor separation of concerns** - Config loading, component creation, and lifecycle management are mixed
3. **Difficult to reuse** - Cannot use the server in different contexts (e.g., integration tests)
4. **Complex initialization** - 150+ lines of setup code in one function

## Proposed Solution

Introduce a **RegistryApp** struct with a **Builder pattern** to cleanly separate concerns.

### Design Principles

1. **Dependency Injection** - Components can be injected (for testing) or built from config (for production)
2. **Single Responsibility** - Each component has one clear purpose
3. **Testability** - Easy to mock and test individual components
4. **Lifecycle Management** - Clear Start/Stop methods with graceful shutdown
5. **Fail Fast** - Validate all dependencies during build phase

## Architecture

### Component Hierarchy

```
RegistryApp
├── Config (loaded externally)
├── Components
│   ├── SyncComponents
│   │   ├── SourceHandlerFactory
│   │   ├── StorageManager
│   │   ├── StatusPersistence
│   │   ├── SyncManager
│   │   └── SyncCoordinator
│   ├── RegistryService
│   │   ├── RegistryProvider
│   │   └── DeploymentProvider (optional)
│   └── HTTPServer
│       ├── Router (with middleware)
│       └── http.Server
└── Kubernetes (optional)
    ├── RestConfig
    ├── Client
    └── Scheme
```

### Code Structure

```
pkg/app/
├── app.go              # RegistryApp struct and lifecycle methods
├── builder.go          # RegistryAppBuilder with options pattern
├── components.go       # Component groupings
├── kubernetes.go       # K8s setup helpers
└── defaults.go         # Default builders from config
```

## Implementation

### 1. RegistryApp Struct

```go
// pkg/app/app.go
package app

import (
    "context"
    "net/http"
    "github.com/stacklok/toolhive-registry-server/pkg/config"
    "github.com/stacklok/toolhive-registry-server/pkg/sync/coordinator"
    "github.com/stacklok/toolhive-registry-server/internal/service"
)

// RegistryApp encapsulates all components needed to run the registry API server
type RegistryApp struct {
    config     *config.Config
    components *AppComponents
    httpServer *http.Server

    // Lifecycle management
    ctx        context.Context
    cancelFunc context.CancelFunc
}

// AppComponents groups all application components
type AppComponents struct {
    // Sync components
    SyncCoordinator coordinator.Coordinator

    // Service components
    RegistryService service.Service

    // Optional Kubernetes components
    KubeComponents *KubernetesComponents
}

// KubernetesComponents groups Kubernetes-related components
type KubernetesComponents struct {
    RestConfig *rest.Config
    Client     client.Client
    Scheme     *runtime.Scheme
}

// Start starts the application (HTTP server and background sync)
func (app *RegistryApp) Start() error {
    // Start sync coordinator in background
    go func() {
        if err := app.components.SyncCoordinator.Start(app.ctx); err != nil {
            logger.Errorf("Sync coordinator failed: %v", err)
        }
    }()

    // Start HTTP server
    logger.Infof("Server listening on %s", app.httpServer.Addr)
    if err := app.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
        return fmt.Errorf("server failed: %w", err)
    }

    return nil
}

// Stop gracefully stops the application
func (app *RegistryApp) Stop(timeout time.Duration) error {
    logger.Info("Shutting down server...")

    // Stop sync coordinator
    if err := app.components.SyncCoordinator.Stop(); err != nil {
        logger.Errorf("Failed to stop sync coordinator: %v", err)
    }

    // Graceful HTTP server shutdown
    shutdownCtx, cancel := context.WithTimeout(context.Background(), timeout)
    defer cancel()

    if err := app.httpServer.Shutdown(shutdownCtx); err != nil {
        return fmt.Errorf("server forced to shutdown: %w", err)
    }

    logger.Info("Server shutdown complete")
    return nil
}
```

### 2. Builder Pattern

```go
// pkg/app/builder.go
package app

type RegistryAppBuilder struct {
    config *config.Config

    // Optional component overrides (for testing)
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
}

// NewRegistryAppBuilder creates a new builder
func NewRegistryAppBuilder(cfg *config.Config) *RegistryAppBuilder {
    return &RegistryAppBuilder{
        config:  cfg,
        address: ":8080",
    }
}

// WithAddress sets the HTTP server address
func (b *RegistryAppBuilder) WithAddress(addr string) *RegistryAppBuilder {
    b.address = addr
    return b
}

// WithKubernetesConfig sets the Kubernetes configuration
func (b *RegistryAppBuilder) WithKubernetesConfig(cfg *rest.Config) *RegistryAppBuilder {
    b.kubeConfig = cfg
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

// WithSyncManager allows injecting a custom sync manager (for testing)
func (b *RegistryAppBuilder) WithSyncManager(sm pkgsync.Manager) *RegistryAppBuilder {
    b.syncManager = sm
    return b
}

// Build constructs the RegistryApp from the builder configuration
func (b *RegistryAppBuilder) Build(ctx context.Context) (*RegistryApp, error) {
    // Validate config
    if err := b.config.Validate(); err != nil {
        return nil, fmt.Errorf("invalid configuration: %w", err)
    }

    // Build Kubernetes components if needed
    kubeComponents, err := b.buildKubernetesComponents()
    if err != nil {
        // Non-fatal - log warning and continue without K8s
        logger.Warnf("Kubernetes components unavailable: %v", err)
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

    // Create app context
    appCtx, cancel := context.WithCancel(context.Background())

    return &RegistryApp{
        config: b.config,
        components: &AppComponents{
            SyncCoordinator:  syncCoordinator,
            RegistryService:  registryService,
            KubeComponents:   kubeComponents,
        },
        httpServer: httpServer,
        ctx:        appCtx,
        cancelFunc: cancel,
    }, nil
}

// buildKubernetesComponents builds K8s client, scheme, etc.
func (b *RegistryAppBuilder) buildKubernetesComponents() (*KubernetesComponents, error) {
    // Use injected config if provided (for testing)
    if b.kubeConfig == nil {
        cfg, err := getKubernetesConfig()
        if err != nil {
            return nil, err
        }
        b.kubeConfig = cfg
    }

    // Use injected client if provided (for testing)
    if b.kubeClient == nil {
        scheme := runtime.NewScheme()
        if err := clientgoscheme.AddToScheme(scheme); err != nil {
            return nil, fmt.Errorf("failed to add k8s types to scheme: %w", err)
        }

        client, err := client.New(b.kubeConfig, client.Options{Scheme: scheme})
        if err != nil {
            return nil, fmt.Errorf("failed to create k8s client: %w", err)
        }

        b.kubeClient = client
        b.kubeScheme = scheme
    }

    return &KubernetesComponents{
        RestConfig: b.kubeConfig,
        Client:     b.kubeClient,
        Scheme:     b.kubeScheme,
    }, nil
}

// buildSyncComponents builds sync manager, coordinator, etc.
func (b *RegistryAppBuilder) buildSyncComponents(kube *KubernetesComponents) (coordinator.Coordinator, error) {
    // Use injected components or build defaults
    if b.sourceHandlerFactory == nil {
        var k8sClient client.Client
        if kube != nil {
            k8sClient = kube.Client
        }
        b.sourceHandlerFactory = sources.NewSourceHandlerFactory(k8sClient)
    }

    if b.storageManager == nil {
        if err := os.MkdirAll("./data", 0755); err != nil {
            return nil, fmt.Errorf("failed to create data directory: %w", err)
        }
        b.storageManager = sources.NewFileStorageManager("./data")
    }

    if b.statusPersistence == nil {
        b.statusPersistence = status.NewFileStatusPersistence("./data/status.json")
    }

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

    return coordinator.New(b.syncManager, b.statusPersistence, b.config), nil
}

// buildServiceComponents builds registry service and providers
func (b *RegistryAppBuilder) buildServiceComponents(ctx context.Context, kube *KubernetesComponents) (service.Service, error) {
    // Build registry provider (reads from synced data)
    if b.registryProvider == nil {
        providerConfig := &service.RegistryProviderConfig{
            FilePath:     "./data/registry.json",
            RegistryName: b.config.GetRegistryName(),
        }

        factory := service.NewRegistryProviderFactory()
        provider, err := factory.CreateProvider(providerConfig)
        if err != nil {
            return nil, fmt.Errorf("failed to create registry provider: %w", err)
        }
        b.registryProvider = provider
    }

    // Build deployment provider (optional, requires K8s)
    if b.deploymentProvider == nil && kube != nil {
        provider, err := service.NewK8sDeploymentProvider(kube.RestConfig, b.config.GetRegistryName())
        if err != nil {
            logger.Warnf("Failed to create deployment provider: %v", err)
        } else {
            b.deploymentProvider = provider
        }
    }

    return service.NewService(ctx, b.registryProvider, b.deploymentProvider)
}

// buildHTTPServer builds the HTTP server with router and middleware
func (b *RegistryAppBuilder) buildHTTPServer(svc service.Service) (*http.Server, error) {
    // Default middlewares if not provided
    if b.middlewares == nil {
        b.middlewares = []func(http.Handler) http.Handler{
            middleware.RequestID,
            middleware.RealIP,
            middleware.Recoverer,
            middleware.Timeout(10 * time.Second),
            api.LoggingMiddleware,
        }
    }

    router := api.NewServer(svc, api.WithMiddlewares(b.middlewares...))

    return &http.Server{
        Addr:         b.address,
        Handler:      router,
        ReadTimeout:  10 * time.Second,
        WriteTimeout: 15 * time.Second,
        IdleTimeout:  60 * time.Second,
    }, nil
}
```

### 3. Updated serve.go

```go
// cmd/thv-registry-api/app/serve.go (simplified)
func runServe(_ *cobra.Command, _ []string) error {
    ctx := context.Background()

    // Initialize logger
    log.SetLogger(zap.New(zap.UseDevMode(false)))

    // Load configuration
    configPath := viper.GetString("config")
    cfg, err := config.NewConfigLoader().LoadConfig(configPath)
    if err != nil {
        return fmt.Errorf("failed to load configuration: %w", err)
    }

    logger.Infof("Loaded configuration from %s (registry: %s, source: %s)",
        configPath, cfg.GetRegistryName(), cfg.Source.Type)

    // Build application
    address := viper.GetString("address")
    app, err := app.NewRegistryAppBuilder(cfg).
        WithAddress(address).
        Build(ctx)
    if err != nil {
        return fmt.Errorf("failed to build application: %w", err)
    }

    // Start application in goroutine
    go func() {
        if err := app.Start(); err != nil {
            logger.Fatalf("Application failed: %v", err)
        }
    }()

    // Wait for shutdown signal
    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit

    // Graceful shutdown
    return app.Stop(30 * time.Second)
}
```

### 4. Testing Example

```go
// pkg/app/app_test.go
func TestRegistryApp_WithMocks(t *testing.T) {
    cfg := &config.Config{
        Source: config.SourceConfig{
            Type: "file",
            File: &config.FileConfig{Path: "/tmp/test.json"},
        },
        SyncPolicy: &config.SyncPolicyConfig{
            Interval: "30m",
        },
    }

    // Create mocks
    mockStorage := mocks.NewMockStorageManager(t)
    mockSyncManager := mocks.NewMockManager(t)

    // Build app with mocks
    app, err := app.NewRegistryAppBuilder(cfg).
        WithAddress(":0"). // Random port for testing
        WithStorageManager(mockStorage).
        WithSyncManager(mockSyncManager).
        Build(context.Background())

    require.NoError(t, err)

    // Test lifecycle
    go app.Start()
    defer app.Stop(5 * time.Second)

    // Verify mocks were called
    // ...
}
```

## Benefits

1. **Testability** ✅
   - Easy to inject mocks for any component
   - Can test individual builders in isolation
   - Integration tests can use real components selectively

2. **Maintainability** ✅
   - Clear separation of concerns
   - Each builder method has a single responsibility
   - Easy to add new components or modify existing ones

3. **Reusability** ✅
   - RegistryApp can be used in different contexts (CLI, tests, embedding)
   - Builder pattern allows flexible construction
   - Components can be shared across different builds

4. **Clarity** ✅
   - serve.go is now ~30 lines instead of 150+
   - Component dependencies are explicit
   - Lifecycle management is centralized

## Migration Path

1. Create new `pkg/app/` package with new structures
2. Keep existing `serve.go` working
3. Gradually migrate to new builder pattern
4. Remove old initialization code
5. Update integration tests to use new builder

## Future Extensions

- **Health checks**: Add `/readyz` and `/livez` endpoints using component status
- **Metrics**: Inject metrics collectors into components
- **Tracing**: Add OpenTelemetry integration at app level
- **Configuration reload**: Hot reload config without restart
- **Multiple registries**: Support running multiple registries in one process
