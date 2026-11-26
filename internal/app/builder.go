package app

import (
	"compress/gzip"
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
	"github.com/stacklok/toolhive-registry-server/internal/claims"
	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/db"
	"github.com/stacklok/toolhive-registry-server/internal/db/sqlc"
	"github.com/stacklok/toolhive-registry-server/internal/kubernetes"
	"github.com/stacklok/toolhive-registry-server/internal/service"
	database "github.com/stacklok/toolhive-registry-server/internal/service/db"
	"github.com/stacklok/toolhive-registry-server/internal/sync/standalone"
	"github.com/stacklok/toolhive-registry-server/internal/telemetry"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/catalog"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/cedar"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/httpapi"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/ingest"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/postgres"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/reconcile"
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
	storageFactory storage.Factory
	fetcher        reconcile.Fetcher
	operator       catalogOperator
	reconcile      reconcile.Options
	// managedSourceIDs is set by buildCatalog for the consumer API's shapes.
	managedSourceIDs api.ManagedSourceIDs
	// admin is set by buildCatalog in Cedar mode when admin is enabled.
	admin *httpapi.AdminOperations
	// policy is the Cedar policy, loaded by buildCatalog in Cedar mode before
	// the database's mode is recorded, so a broken policy file cannot tie a
	// fresh database to Cedar mode.
	policy *cedar.Engine

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
	metricsHandler http.Handler
}

// catalogOperator builds the leased workers of Kubernetes sources.
type catalogOperator interface {
	Preflight(context.Context, persistence.SourceDefinition) error
	Worker(persistence.Jobs, persistence.SourceDefinition, reconcile.Options) (standalone.Worker, error)
}

const (
	defaultSyncPollInterval  = time.Second
	defaultSyncLeaseDuration = time.Minute
)

type registryMetricsReaderFactory interface {
	CreateRegistryMetricsReader(ctx context.Context) (telemetry.RegistryMetricReader, error)
}

func baseConfig(opts ...RegistryAppOptions) (*registryAppConfig, error) {
	cfg := &registryAppConfig{
		address:         defaultHTTPAddress,
		internalAddress: defaultInternalHTTPAddress,
		requestTimeout:  defaultRequestTimeout,
		readTimeout:     defaultReadTimeout,
		writeTimeout:    defaultWriteTimeout,
		idleTimeout:     defaultIdleTimeout,
		reconcile: reconcile.Options{
			PollInterval: defaultSyncPollInterval, LeaseDuration: defaultSyncLeaseDuration,
		},
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

	if cfg.config == nil {
		return nil, fmt.Errorf("configuration is required")
	}

	// Auth and audit are built before anything touches the database, so a start
	// that fails on their configuration leaves the database unchanged.
	if cfg.authMiddleware == nil {
		var authErr error
		cfg.authMiddleware, cfg.authInfoHandler, authErr = auth.NewAuthMiddleware(ctx, cfg.config.Auth, auth.DefaultValidatorFactory)
		if authErr != nil {
			return nil, fmt.Errorf("failed to build auth middleware: %w", authErr)
		}
	}
	auditLogger, err := buildAuditLogger(cfg.config)
	if err != nil {
		return nil, fmt.Errorf("failed to create audit logger: %w", err)
	}

	// Ensure cleanup happens on error
	var cleanupNeeded = true
	defer func() {
		if !cleanupNeeded {
			return
		}
		if auditLogger != nil {
			_ = auditLogger.Close()
		}
		if cfg.storageFactory != nil {
			cfg.storageFactory.Cleanup()
		}
	}()

	// Create storage factory for all storage-dependent components
	if err := ensureStorageFactory(ctx, cfg); err != nil {
		return nil, err
	}

	built, err := buildCatalog(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to build catalog: %w", err)
	}

	registryService, err := cfg.storageFactory.CreateRegistryService(ctx, built.catalog.Entries,
		database.WithSourcesChanged(built.sync.runtime.Refresh), database.WithSourcePreparer(built.prepareSource))
	if err != nil {
		return nil, fmt.Errorf("failed to build service components: %w", err)
	}

	// Build HTTP server
	httpServer, err := buildHTTPServer(ctx, cfg, registryService, built.pages, auditLogger)
	if err != nil {
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
			Sync:            built.sync,
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

// WithStorageFactory allows injecting a custom storage factory (for testing)
func WithStorageFactory(f storage.Factory) RegistryAppOptions {
	return func(cfg *registryAppConfig) error {
		cfg.storageFactory = f
		return nil
	}
}

// WithFetcher replaces the source fetcher (for testing).
func WithFetcher(f reconcile.Fetcher) RegistryAppOptions {
	return func(cfg *registryAppConfig) error {
		cfg.fetcher = f
		return nil
	}
}

// WithReconcileOptions sets the sync poll interval and lease duration (for testing).
func WithReconcileOptions(o reconcile.Options) RegistryAppOptions {
	return func(cfg *registryAppConfig) error {
		if o.PollInterval <= 0 {
			return fmt.Errorf("sync poll interval must be positive")
		}
		if err := persistence.ValidateLeaseDuration(o.LeaseDuration); err != nil {
			return fmt.Errorf("invalid sync lease duration: %w", err)
		}
		cfg.reconcile = o
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

// WithMetricsHandler sets the Prometheus /metrics HTTP handler, mounted on
// the internal server alongside health/readiness/version. Nil disables the
// route (metrics disabled or no Prometheus reader configured).
func WithMetricsHandler(h http.Handler) RegistryAppOptions {
	return func(cfg *registryAppConfig) error {
		cfg.metricsHandler = h
		return nil
	}
}

// builtCatalog is the server's library-backed core.
type builtCatalog struct {
	catalog *storage.Catalog
	sync    *syncRunner
	pages   catalog.Reader
	// admin is the library admin API for Cedar mode; nil in claims mode,
	// whose /v1 handlers run over the registry service.
	admin *httpapi.AdminOperations
	// prepareSource scopes and preflights admin-API Kubernetes sources.
	prepareSource database.SourcePreparer
}

// buildCatalog opens the library adapters, checks the database's
// authorization mode, reconciles the config file's sources and registries,
// stores config claims and normalizes the v1.5.4 settings of stored
// API-created sources (claims mode), and prepares the sync runtime. Nothing
// runs until Start.
//
//nolint:gocyclo // Each step is a distinct, ordered preparation stage.
func buildCatalog(ctx context.Context, b *registryAppConfig) (*builtCatalog, error) {
	slog.Info("Initializing catalog components")
	sources, views, err := catalogDefinitions(b.config)
	if err != nil {
		return nil, fmt.Errorf("invalid sources or registries: %w", err)
	}
	claimsMode := b.config.AuthorizationMode() == config.AuthorizationModeClaims
	authzOn := claimsMode && b.config.Auth != nil && b.config.Auth.Authz != nil
	// Claims mode stores claims with or without auth.authz, as v1.5.4 did, so
	// Kubernetes annotation claims are recorded whenever claims are in use.
	var annotations *kubernetes.AnnotationClaims
	if claimsMode {
		annotations = kubernetes.NewAnnotationClaims()
	}
	operator := b.operator
	if operator == nil {
		// Cedar mode never uses claims, so annotations are not even read.
		operator = &kubernetes.StandaloneFactory{Claims: annotations, IgnoreClaimAnnotations: !claimsMode}
	}
	if !claimsMode {
		if b.policy, err = cedar.Load(b.config.Authorization.PolicyFile); err != nil {
			return nil, fmt.Errorf("failed to load authorization policy: %w", err)
		}
	}
	// Claims mode keeps registry_entry.claims in step with library writes;
	// Cedar mode never reads or writes claims.
	var hook postgres.EntryWriteHook
	if claimsMode {
		var overrides database.EntryClaimsProvider
		if annotations != nil {
			overrides = annotations
		}
		hook = database.NewClaimsHook(overrides)
	}
	cat, err := b.storageFactory.CreateCatalog(ctx, hook)
	if err != nil {
		return nil, fmt.Errorf("failed to open catalog storage: %w", err)
	}
	if err := db.EnsureAuthorizationMode(ctx, cat.Pool, string(b.config.AuthorizationMode())); err != nil {
		return nil, err
	}
	preflight := func(ctx context.Context, d persistence.SourceDefinition) error {
		if err := sourcePreflight(d); err != nil {
			return err
		}
		if d.Kubernetes != nil {
			return operator.Preflight(ctx, d)
		}
		return ctx.Err()
	}
	for _, d := range sources {
		if err := startupPreflight(ctx, d, preflight); err != nil {
			return nil, err
		}
	}
	if err := reconcileConfig(ctx, cat, b.config, sources, views, claimsMode); err != nil {
		return nil, err
	}
	// Only a claims-mode database can hold rows v1.5.4's admin API stored;
	// Cedar mode always starts on a new database.
	if claimsMode {
		if err := normalizeStoredSources(ctx, cat.Pool); err != nil {
			return nil, err
		}
	}

	var jobs persistence.Jobs = cat.Jobs
	var syncMetrics *telemetry.SyncMetrics
	if b.meterProvider != nil {
		syncMetrics, err = telemetry.NewSyncMetrics(b.meterProvider)
		if err != nil {
			return nil, fmt.Errorf("failed to create sync metrics: %w", err)
		}
		if syncMetrics != nil {
			slog.Info("Sync metrics enabled")
		}
		if readerFactory, ok := b.storageFactory.(registryMetricsReaderFactory); ok {
			metricsReader, err := readerFactory.CreateRegistryMetricsReader(ctx)
			if err != nil {
				return nil, fmt.Errorf("failed to create registry metrics reader: %w", err)
			}
			if _, err := telemetry.NewRegistryMetrics(b.meterProvider, metricsReader); err != nil {
				return nil, fmt.Errorf("failed to create registry metrics: %w", err)
			}
		}
	}
	var syncTracer, readTracer trace.Tracer
	if b.tracerProvider != nil {
		syncTracer, readTracer = b.tracerProvider.Tracer(SyncTracerName), b.tracerProvider.Tracer(database.ServiceTracerName)
	}
	if syncMetrics != nil || syncTracer != nil {
		jobs = newObservedJobs(jobs, syncMetrics, syncTracer)
	}
	fetcher := b.fetcher
	if fetcher == nil {
		fetcher = ingest.New(ingest.Options{LoadSecret: config.LoadSecret})
	}
	fetcher = withAmbiguousGitRefs(fetcher, b.config)
	runtime, err := standalone.New(cat.Definitions, jobs, fetcher, standalone.Options{
		Reconcile: b.reconcile,
		Operator: func(d persistence.SourceDefinition) (standalone.Worker, error) {
			return operator.Worker(jobs, d, b.reconcile)
		},
		Preflight: preflight,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create sync runtime: %w", err)
	}

	var entryReader persistence.ViewEntryReader = cat.Entries
	if authzOn {
		entryReader, err = claims.NewReader(cat.Entries, sqlc.New(cat.Pool))
		if err != nil {
			return nil, err
		}
	}
	pages, err := catalog.NewReader(entryReader)
	if err != nil {
		return nil, err
	}
	queries := sqlc.New(cat.Pool)
	b.managedSourceIDs = func(ctx context.Context) (map[string]bool, error) {
		rows, err := queries.GetManagedSources(ctx)
		if err != nil {
			return nil, err
		}
		ids := make(map[string]bool, len(rows))
		for _, row := range rows {
			ids[row.ID.String()] = true
		}
		return ids, nil
	}
	built := &builtCatalog{
		catalog: cat, sync: &syncRunner{runtime: runtime}, pages: newTracedPages(pages, readTracer),
		prepareSource: kubernetesSourcePreparer(b.config.WatchNamespace, operator.Preflight),
	}
	if !claimsMode && b.config.AdminEnabled() {
		if built.admin, err = libraryAdmin(cat, runtime, b.config.WatchNamespace, preflight); err != nil {
			return nil, err
		}
		b.admin = built.admin
	}
	slog.Info("Catalog components initialized", "authorization_mode", b.config.AuthorizationMode())
	return built, nil
}

// libraryAdmin assembles the library's admin operations for Cedar mode.
// Definition changes refresh the sync runtime, and inline sources sync
// immediately, as the library's admin API expects.
func libraryAdmin(
	cat *storage.Catalog, runtime *standalone.Runtime, watch string,
	preflight func(context.Context, persistence.SourceDefinition) error,
) (*httpapi.AdminOperations, error) {
	scoped := &scopedSources{Sources: cat.Definitions, watch: watch, preflight: preflight}
	definitions, err := catalog.NewDefinitionAdmin(scoped, cat.Definitions, catalog.DefinitionAdminOptions{
		SyncInline: runtime.SyncInline, NotifyRefresh: runtime.Refresh,
	})
	if err != nil {
		return nil, err
	}
	publisher, err := catalog.NewPublisher(cat.Entries)
	if err != nil {
		return nil, err
	}
	diagnostics, err := catalog.NewDiagnostics(cat.Entries, cat.Jobs)
	if err != nil {
		return nil, err
	}
	return &httpapi.AdminOperations{
		Sources: definitions, Views: definitions, Publisher: publisher, Diagnostics: diagnostics,
	}, nil
}

// buildHTTPServer builds the HTTP server with router and middleware
//
//nolint:unparam // we prefer having a similar interface
func buildHTTPServer(
	_ context.Context,
	b *registryAppConfig,
	svc service.RegistryService,
	pages httpapi.ConsumerPages,
	auditLogger *auditmw.Logger,
) (*http.Server, error) {
	slog.Info("Initializing HTTP server")

	// Use default middlewares if not provided
	if b.middlewares == nil {
		b.middlewares = []func(http.Handler) http.Handler{
			middleware.RequestID,
			// middleware.RealIP is intentionally omitted: it was deprecated in
			// chi v5.3.0 because it blindly trusts client-supplied headers
			// (True-Client-IP / X-Real-IP / leftmost X-Forwarded-For) and
			// mutates r.RemoteAddr, which is an IP-spoofing risk. We keep
			// r.RemoteAddr as the direct peer address; the raw X-Forwarded-For
			// header is still preserved for forensics in audit events.
			middleware.Recoverer,
			middleware.Timeout(b.requestTimeout),
			api.LoggingMiddleware,
			securityHeadersMiddleware,
		}
	}

	// Add observability and compression middlewares (metrics, tracing, compression)
	if err := addObservabilityMiddlewares(b); err != nil {
		return nil, err
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

	if b.config != nil && b.config.AuthorizationMode() == config.AuthorizationModeCedar {
		return buildCedarHTTPServer(b, pages, auditCfg, auditLogger)
	}

	authMw := auth.WrapWithPublicPaths(b.authMiddleware, publicPaths)
	b.middlewares = append(b.middlewares, authMw)

	// Extract authentication config if available; the v1 router derives both
	// role checks (Authz) and publish-time claim enforcement (Mode) from it.
	var authCfg *config.AuthConfig
	if b.config != nil && b.config.Auth != nil {
		authCfg = b.config.Auth
	}
	var authzCfg *config.AuthzConfig
	if authCfg != nil {
		authzCfg = authCfg.Authz
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
		api.WithManagedSourceIDs(b.managedSourceIDs),
		api.WithAuthInfoHandler(b.authInfoHandler),
		api.WithAuthConfig(authCfg),
	}
	// Create router with middlewares
	router, err := api.NewServer(svc, pages, serverOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to build router: %w", err)
	}

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

// buildCedarHTTPServer finishes the public server for Cedar mode: anonymous
// admission and authentication, the audit middleware, and the library's
// routers behind the Cedar authorizer. No roles are resolved.
func buildCedarHTTPServer(
	b *registryAppConfig, pages httpapi.ConsumerPages, auditCfg *config.AuditConfig, auditLogger *auditmw.Logger,
) (*http.Server, error) {
	cfg := b.config
	authMw := auth.WithAnonymousAdmission(auth.WrapWithPublicPaths(b.authMiddleware, []string{"/.well-known"}),
		cfg.Auth.Mode, cfg.Authorization.AllowAnonymousCatalog)
	b.middlewares = append(b.middlewares, authMw, auditmw.Middleware(auditCfg, auditLogger))

	engine := b.policy
	if engine == nil {
		var err error
		if engine, err = cedar.Load(cfg.Authorization.PolicyFile); err != nil {
			return nil, fmt.Errorf("failed to load authorization policy: %w", err)
		}
	}
	if cfg.Authorization.PolicyFile == "" && cfg.AdminEnabled() {
		slog.Warn("No authorization policy file: the built-in policy lets every authenticated caller use the admin API")
	}
	opts := cedar.Options{
		Identity: cedarIdentity, Mappings: cfg.Authorization.CedarMappings(), Challenge: auth.AnonymousChallenge(cfg.Auth),
	}
	if auditLogger != nil {
		opts.OnDecision = admissionAudit(auditLogger)
	}
	authorizer, err := cedar.NewAuthorizer(engine, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to build authorizer: %w", err)
	}
	router, err := api.NewCedarServer(pages, b.admin, authorizer,
		api.WithMiddlewares(b.middlewares...),
		api.WithManagedSourceIDs(b.managedSourceIDs),
		api.WithAuthInfoHandler(b.authInfoHandler),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to build router: %w", err)
	}
	slog.Info("HTTP server configured", "address", b.address, "authorization_mode", config.AuthorizationModeCedar,
		"policy_revision", engine.Revision(), "admin_enabled", b.admin != nil)
	return &http.Server{
		Addr: b.address, Handler: router,
		ReadTimeout: b.readTimeout, WriteTimeout: b.writeTimeout, IdleTimeout: b.idleTimeout,
	}, nil
}

// cedarIdentity is the caller Cedar evaluates: anonymous when admitted
// without a token, otherwise the verified token's issuer, subject and claims.
func cedarIdentity(r *http.Request) (cedar.Identity, bool) {
	ctx := r.Context()
	if auth.IsAnonymous(ctx) {
		return cedar.Identity{Anonymous: true}, true
	}
	jwt := auth.ClaimsFromContext(ctx)
	if jwt == nil {
		return cedar.Identity{}, false
	}
	issuer, _ := jwt["iss"].(string)
	subject, _ := jwt["sub"].(string)
	return cedar.Identity{Issuer: issuer, Subject: subject, Claims: map[string]any(jwt)}, true
}

// admissionAudit records each Cedar decision as an audit event.
func admissionAudit(logger *auditmw.Logger) func(*http.Request, cedar.Decision) {
	return func(r *http.Request, d cedar.Decision) {
		decision := "deny"
		switch {
		case d.Err != nil:
			decision = "error"
		case d.Allowed:
			decision = "allow"
		}
		logger.Admission(r.Context(), r, d.Principal, string(d.Action), d.Resource, d.Revision, decision)
	}
}

// buildInternalHTTPServer builds the internal HTTP server for health, readiness, version, and metrics endpoints
func buildInternalHTTPServer(b *registryAppConfig, svc service.RegistryService) *http.Server {
	router := api.NewInternalServer(svc, b.metricsHandler)
	return &http.Server{
		Addr:         b.internalAddress,
		Handler:      router,
		ReadTimeout:  b.readTimeout,
		WriteTimeout: b.writeTimeout,
		IdleTimeout:  b.idleTimeout,
	}
}

// addObservabilityMiddlewares prepends metrics, tracing, and compression
// middlewares to the middleware chain when the corresponding providers or
// feature flags are configured.
func addObservabilityMiddlewares(b *registryAppConfig) error {
	// Add metrics middleware if meter provider is configured
	// This should be added early in the chain to capture all requests
	if b.meterProvider != nil {
		metricsMiddleware, err := telemetry.MetricsMiddleware(b.meterProvider)
		if err != nil {
			return fmt.Errorf("failed to create metrics middleware: %w", err)
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

	// Add compression middleware if enabled via THV_REGISTRY_COMPRESS_RESPONSE
	if b.config != nil && b.config.IsCompressionEnabled() {
		b.middlewares = append([]func(http.Handler) http.Handler{middleware.Compress(gzip.DefaultCompression)}, b.middlewares...)
		slog.Info("HTTP response compression enabled")
	}

	return nil
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

// securityHeadersMiddleware sets baseline security response headers on every request.
func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}
