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
	"go.opentelemetry.io/otel/trace"

	"github.com/stacklok/toolhive-registry-server/internal/api"
	"github.com/stacklok/toolhive-registry-server/internal/app/storage"
	auditmw "github.com/stacklok/toolhive-registry-server/internal/audit"
	"github.com/stacklok/toolhive-registry-server/internal/auth"
	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/kubernetes"
	"github.com/stacklok/toolhive-registry-server/internal/service"
	"github.com/stacklok/toolhive-registry-server/internal/sources"
	pkgsync "github.com/stacklok/toolhive-registry-server/internal/sync"
	"github.com/stacklok/toolhive-registry-server/internal/sync/coordinator"
	"github.com/stacklok/toolhive-registry-server/internal/sync/writer"
	"github.com/stacklok/toolhive-registry-server/internal/telemetry"
)

const (
	defaultHTTPAddress         = ":8080"
	defaultInternalHTTPAddress = ":8081"
	defaultRequestTimeout      = 10 * time.Second
	defaultReadTimeout         = 10 * time.Second
	defaultWriteTimeout        = 15 * time.Second
	defaultIdleTimeout         = 60 * time.Second
)

// defaultPublicPaths are paths that never require authentication
var defaultPublicPaths = []string{"/openapi.json", "/.well-known"}

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
	address         string
	internalAddress string
	middlewares     []func(http.Handler) http.Handler
	requestTimeout  time.Duration
	readTimeout     time.Duration
	writeTimeout    time.Duration
	idleTimeout     time.Duration

	// Auth components
	authMiddleware  func(http.Handler) http.Handler
	authInfoHandler http.Handler

	// Telemetry components
	meterProvider  metric.MeterProvider
	tracerProvider trace.TracerProvider

	// Coordinator options (primarily for testing)
	coordinatorOpts []coordinator.Option
}

func baseConfig(opts ...RegistryAppOptions) (*registryAppConfig, error) {
	cfg := &registryAppConfig{
		address:         defaultHTTPAddress,
		internalAddress: defaultInternalHTTPAddress,
		requestTimeout:  defaultRequestTimeout,
		readTimeout:     defaultReadTimeout,
		writeTimeout:    defaultWriteTimeout,
		idleTimeout:     defaultIdleTimeout,
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

	// Create storage factory for all storage-dependent components
	if err := ensureStorageFactory(ctx, cfg); err != nil {
		return nil, err
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

	// Build audit logger (if audit is enabled)
	auditLogger, err := buildAuditLogger(cfg.config)
	if err != nil {
		return nil, fmt.Errorf("failed to create audit logger: %w", err)
	}

	// Build HTTP server
	httpServer, err := buildHTTPServer(ctx, cfg, registryService, auditLogger)
	if err != nil {
		if auditLogger != nil {
			_ = auditLogger.Close()
		}
		return nil, fmt.Errorf("failed to build HTTP server: %w", err)
	}

	// Build internal HTTP server for health/readiness/version
	internalHTTPServer := buildInternalHTTPServer(cfg, registryService)

	// Create application context
	appCtx, cancel := context.WithCancel(ctx) //nolint:gosec // G118 false positive: cancel is called in cancelFunc below

	// Cleanup is now handled by the app, not in defer
	cleanupNeeded = false

	cancelFunc := func() {
		if auditLogger != nil {
			_ = auditLogger.Close()
		}
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
		httpServer:         httpServer,
		internalHTTPServer: internalHTTPServer,
		ctx:                appCtx,
		cancelFunc:         cancelFunc,
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

// WithInternalAddress sets the internal HTTP server address for health, readiness, and version endpoints
func WithInternalAddress(addr string) RegistryAppOptions {
	return func(cfg *registryAppConfig) error {
		if addr == "" {
			return fmt.Errorf("internal address cannot be empty")
		}

		parts := strings.SplitN(addr, ":", 2)
		host := parts[0]
		port := parts[1]

		if port == "" {
			return fmt.Errorf("internal address is not a valid port: %s", addr)
		}
		if host == "localhost" {
			host = "127.0.0.1"
		}
		if host == "" {
			host = "0.0.0.0"
		}

		if _, err := netip.ParseAddrPort(host + ":" + port); err != nil {
			return fmt.Errorf("internal address is not a valid port: %w", err)
		}

		cfg.internalAddress = addr
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

// WithCoordinatorOptions sets additional coordinator options (e.g., for testing).
func WithCoordinatorOptions(opts ...coordinator.Option) RegistryAppOptions {
	return func(cfg *registryAppConfig) error {
		cfg.coordinatorOpts = append(cfg.coordinatorOpts, opts...)
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

// WithTracerProvider sets the OpenTelemetry tracer provider for HTTP tracing
func WithTracerProvider(tp trace.TracerProvider) RegistryAppOptions {
	return func(cfg *registryAppConfig) error {
		cfg.tracerProvider = tp
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
		if err := setupKubernetesReconciler(ctx, b.config, syncWriter); err != nil {
			return nil, err
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

	// Add tracing if tracer provider is configured
	if b.tracerProvider != nil {
		tracer := b.tracerProvider.Tracer(coordinator.CoordinatorTracerName)
		coordOpts = append(coordOpts, coordinator.WithTracer(tracer))
		slog.Info("Sync coordinator tracing enabled")
	}

	// Append any externally provided coordinator options (e.g., polling interval for tests)
	coordOpts = append(coordOpts, b.coordinatorOpts...)

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
	auditLogger *auditmw.Logger,
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

	// Add tracing middleware if tracer provider is configured
	// This should be added early in the chain to capture all requests
	if b.tracerProvider != nil {
		tracingMiddleware := telemetry.TracingMiddleware(b.tracerProvider)
		// Prepend tracing middleware to capture all requests including those rejected by auth
		b.middlewares = append([]func(http.Handler) http.Handler{tracingMiddleware}, b.middlewares...)
		slog.Info("HTTP tracing middleware enabled")
	}

	// Create auth middleware that bypasses public paths
	publicPaths := defaultPublicPaths
	if b.config != nil && b.config.Auth != nil && len(b.config.Auth.PublicPaths) > 0 {
		publicPaths = append(publicPaths, b.config.Auth.PublicPaths...)
	}

	var auditCfg *config.AuditConfig
	if b.config != nil {
		auditCfg = b.config.Audit
	}

	// Auth failure audit middleware runs BEFORE auth so it can observe 401
	// rejections that never reach the post-auth audit middleware. This is
	// critical for SIEM visibility into failed authentication attempts.
	b.middlewares = append(b.middlewares, auditmw.AuthFailureMiddleware(auditCfg, auditLogger))

	authMw := auth.WrapWithPublicPaths(b.authMiddleware, publicPaths)
	b.middlewares = append(b.middlewares, authMw)

	// Extract authorization config if available
	var authzCfg *config.AuthzConfig
	if b.config != nil && b.config.Auth != nil {
		authzCfg = b.config.Auth.Authz
	}

	// Resolve roles for all authenticated requests so that downstream code
	// (e.g., super-admin bypass in claim validation) can use IsSuperAdmin(ctx).
	b.middlewares = append(b.middlewares, auth.ResolveRolesMiddleware(authzCfg))

	// Add audit middleware after auth and roles are resolved so that JWT
	// claims and roles are available for the audit event subjects.
	b.middlewares = append(b.middlewares, auditmw.Middleware(auditCfg, auditLogger))
	if auditCfg != nil && auditCfg.Enabled {
		slog.Info("Audit logging middleware enabled")
	}

	serverOpts := []api.ServerOption{
		api.WithMiddlewares(b.middlewares...),
		api.WithAuthInfoHandler(b.authInfoHandler),
		api.WithAuthzConfig(authzCfg),
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

// buildInternalHTTPServer builds the internal HTTP server for health, readiness, and version endpoints
func buildInternalHTTPServer(b *registryAppConfig, svc service.RegistryService) *http.Server {
	router := api.NewInternalServer(svc)
	return &http.Server{
		Addr:         b.internalAddress,
		Handler:      router,
		ReadTimeout:  b.readTimeout,
		WriteTimeout: b.writeTimeout,
		IdleTimeout:  b.idleTimeout,
	}
}

// ensureStorageFactory creates the storage factory if not already injected.
func ensureStorageFactory(ctx context.Context, cfg *registryAppConfig) error {
	if cfg.storageFactory != nil {
		return nil
	}
	var storageOpts []storage.FactoryOption
	if cfg.tracerProvider != nil {
		storageOpts = append(storageOpts, storage.WithTracerProvider(cfg.tracerProvider))
	}
	var err error
	cfg.storageFactory, err = storage.NewStorageFactory(ctx, cfg.config, storageOpts...)
	if err != nil {
		return fmt.Errorf("failed to create storage factory: %w", err)
	}
	return nil
}

// buildAuditLogger creates a dedicated audit logger when audit logging is enabled.
// Returns nil when audit is not enabled.
func buildAuditLogger(cfg *config.Config) (*auditmw.Logger, error) {
	if cfg == nil || !cfg.IsAuditEnabled() {
		return nil, nil
	}
	var logFile string
	if cfg.Audit != nil {
		logFile = cfg.Audit.LogFile
	}
	return auditmw.NewLogger(logFile)
}

// setupKubernetesReconciler creates a Kubernetes reconciler if any registry uses the Kubernetes source type.
func setupKubernetesReconciler(ctx context.Context, cfg *config.Config, syncWriter writer.SyncWriter) error {
	for _, reg := range cfg.Sources {
		if reg.GetType() != config.SourceTypeKubernetes {
			continue
		}

		opts := []kubernetes.Option{
			kubernetes.WithSyncWriter(syncWriter),
			kubernetes.WithRegistryName(reg.Name),
		}

		// Use namespaces from source config if set, otherwise fall back to global WatchNamespace
		if reg.Kubernetes != nil && len(reg.Kubernetes.Namespaces) > 0 {
			opts = append(opts, kubernetes.WithNamespaces(reg.Kubernetes.Namespaces...))
		} else if cfg.WatchNamespace != "" {
			namespaces := strings.Split(cfg.WatchNamespace, ",")
			opts = append(opts, kubernetes.WithNamespaces(namespaces...))
		}

		// Each K8s source needs a unique leader election ID to avoid lease conflicts.
		// Source names are validated as unique (config validation rejects duplicates),
		// so appending the source name is sufficient for uniqueness. We intentionally
		// do NOT include namespace names: since both source names and namespaces use
		// the DNS label charset (lowercase alphanumeric + hyphens), joining them with
		// a hyphen would create ambiguous boundaries (e.g. source "foo-bar" + ns "baz"
		// would collide with source "foo" + ns "bar-baz").
		leaderID := cfg.LeaderElectionID
		if leaderID == "" {
			leaderID = "toolhive-registry-server-leader-election"
		}
		leaseID := fmt.Sprintf("%s-%s", leaderID, reg.Name)
		opts = append(opts, kubernetes.WithLeaderElectionID(leaseID))

		_, err := kubernetes.NewMCPServerReconciler(ctx, opts...)
		if err != nil {
			return fmt.Errorf("failed to create kubernetes reconciler for source %s: %w", reg.Name, err)
		}
	}
	return nil
}
