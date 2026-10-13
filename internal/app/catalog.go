package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/gobwas/glob"

	"github.com/stacklok/toolhive-registry-server/internal/api"
	"github.com/stacklok/toolhive-registry-server/internal/app/storage"
	"github.com/stacklok/toolhive-registry-server/internal/audit"
	"github.com/stacklok/toolhive-registry-server/internal/auth"
	"github.com/stacklok/toolhive-registry-server/internal/authorization"
	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/kubernetes"
	shutdownresult "github.com/stacklok/toolhive-registry-server/internal/shutdown"
	"github.com/stacklok/toolhive-registry-server/internal/sync/standalone"
	"github.com/stacklok/toolhive-registry-server/internal/telemetry"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/catalog"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/formats"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/httpapi"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/ingest"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/reconcile"
)

// CatalogOperator is consumed here so Kubernetes client types stay in their package.
// Dependencies are borrowed; Worker owns and joins only its own work.
//
//go:generate mockgen -destination=mocks/catalog_operator.go -package=mocks -source=catalog.go CatalogOperator
type CatalogOperator interface {
	Preflight(context.Context, persistence.SourceDefinition) error
	Worker(persistence.Jobs, persistence.SourceDefinition, reconcile.Options) (standalone.Worker, error)
}

// CatalogOptions selects the standalone public-component composition.
// HTTP options configure addresses, timeouts, middleware and telemetry.
type CatalogOptions struct {
	Config *config.Config
	// InitializeEmpty explicitly authorizes only a new, entirely empty catalog.
	InitializeEmpty bool
	Overrides       []func(*config.Config) error
	Storage         storage.CatalogFactory
	Operator        CatalogOperator
	Fetcher         reconcile.Fetcher
	HTTP            []RegistryAppOptions
	Reconcile       reconcile.Options
	ShutdownTimeout time.Duration
}

const (
	catalogPrepared = "prepared"
	catalogRunning  = "running"
	catalogStopping = "stopping"
)

// CatalogApp owns listeners, leased workers, request/inline lifetimes and resources
// returned by its storage factory. Construction never starts application work.
type CatalogApp struct {
	ctx              context.Context
	cancel           context.CancelFunc
	storage          storage.CatalogStorage
	runtime          *standalone.Runtime
	public, internal *http.Server
	audit            *audit.Logger
	registryMetrics  *telemetry.RegistryMetrics
	timeout          time.Duration
	mu               sync.Mutex
	state            string
	done             chan struct{}
	err              error
	inflight         sync.WaitGroup
	cleanup          sync.Once
}

// NewCatalogApp enforces preparation order even for direct calls and injected
// factories: final config -> frozen policy -> open -> read-only preflight ->
// serialized receipt consumption -> reconcile/repair -> guarded services/HTTP.
//
//nolint:gocyclo // Keep the ordered preparation barrier and every failure cleanup visible at one composition root.
func NewCatalogApp(ctx context.Context, options CatalogOptions) (_ *CatalogApp, err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if options.Config == nil {
		return nil, errors.New("catalog config is required")
	}
	cfg := options.Config.Clone()
	for _, override := range options.Overrides {
		if override == nil {
			return nil, persistence.ErrInvalid
		}
		if err := override(cfg); err != nil {
			return nil, err
		}
	}
	// Overrides may retain their argument; runtime policy owns a separate snapshot.
	cfg = cfg.Clone()
	b, err := baseConfig(options.HTTP...)
	if err != nil {
		return nil, err
	}
	if options.Reconcile.PollInterval == 0 {
		options.Reconcile.PollInterval = time.Second
	}
	if options.Reconcile.LeaseDuration == 0 {
		options.Reconcile.LeaseDuration = time.Minute
	}
	if options.Reconcile.PollInterval < 0 {
		return nil, errors.New("poll interval must be positive")
	}
	if err := persistence.ValidateLeaseDuration(options.Reconcile.LeaseDuration); err != nil {
		return nil, err
	}
	if options.ShutdownTimeout < 0 {
		return nil, errors.New("shutdown timeout must be positive")
	}
	b.config = cfg
	if err = cfg.ValidateCatalogCutover(); err != nil {
		return nil, err
	}
	sources, views, err := catalogDefinitions(cfg)
	if err != nil {
		return nil, err
	}
	engine, err := authorization.Load(cfg.Authorization.PolicyFile)
	if err != nil {
		return nil, err
	}
	review, err := cfg.ExposureReview(engine.Revision())
	if err != nil {
		return nil, err
	}
	factory := options.Storage
	if factory == nil {
		factory = storage.CatalogPostgres{}
	}
	store, err := factory.Open(ctx, cfg)
	if err != nil {
		if store != nil {
			store.Close()
		}
		return nil, err
	}
	if store == nil {
		return nil, errors.New("catalog storage factory returned nil preparation capability")
	}
	owned := true
	defer func() {
		if owned {
			store.Close()
		}
	}()
	persisted, err := store.Preflight(ctx, sources, views, cfg.WatchNamespace)
	if err != nil {
		return nil, err
	}
	operators := options.Operator
	if operators == nil {
		operators = &kubernetes.StandaloneFactory{}
	}
	preflight := func(
		ctx context.Context, d persistence.SourceDefinition,
	) error {
		if err := catalogSourcePreflight(d); err != nil {
			return err
		}
		if d.Kubernetes != nil {
			return operators.Preflight(ctx, d)
		}
		return ctx.Err()
	}
	for _, d := range append(sources, persisted...) {
		if err := preflight(ctx, d); err != nil {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if options.InitializeEmpty {
		err = store.ConsumeEmpty(ctx, review.Revision)
	} else {
		err = store.Consume(ctx, review.Revision)
	}
	if err != nil {
		return nil, fmt.Errorf("catalog exposure preparation: %w; see docs/registry-exposure-migration.md", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	components, err := store.Initialize(ctx, sources, views)
	if err != nil {
		return nil, err
	}
	if components.Definitions == nil || components.Entries == nil || components.Jobs == nil {
		return nil, errors.New("catalog storage factory returned incomplete initialized capabilities")
	}
	syncMetrics, err := telemetry.NewSyncMetrics(b.meterProvider)
	if err != nil {
		return nil, err
	}
	if syncMetrics != nil {
		components.Jobs = &measuredCatalogJobs{
			Jobs: components.Jobs, metrics: syncMetrics, started: map[string]time.Time{},
		}
	}
	fetcher := options.Fetcher
	if fetcher == nil {
		fetcher = ingest.New(ingest.Options{LoadSecret: config.LoadSecret})
	}
	if b.tracerProvider != nil {
		fetcher = tracedCatalogFetcher{raw: fetcher, tracer: b.tracerProvider.Tracer("catalog.ingestion")}
	}
	runtime, err := standalone.New(components.Definitions, components.Jobs, fetcher, standalone.Options{
		Reconcile: options.Reconcile,
		Operator: func(d persistence.SourceDefinition) (standalone.Worker, error) {
			return operators.Worker(components.Jobs, d, options.Reconcile)
		},
		Preflight: preflight,
	})
	if err != nil {
		return nil, err
	}
	appCtx, cancel := context.WithCancel(ctx)
	app := &CatalogApp{
		ctx: appCtx, cancel: cancel, storage: store, runtime: runtime, state: catalogPrepared,
		done: make(chan struct{}), timeout: options.ShutdownTimeout,
	}
	if app.timeout == 0 {
		app.timeout = 30 * time.Second
	}
	defer func() {
		if err != nil {
			cancel()
			if app.registryMetrics != nil {
				err = errors.Join(err, app.registryMetrics.Unregister())
			}
			if app.audit != nil {
				err = errors.Join(err, app.audit.Close())
			}
		}
	}()
	if b.meterProvider != nil {
		if components.Metrics == nil {
			return nil, errors.New("catalog storage metrics capability required")
		}
		app.registryMetrics, err = telemetry.NewRegistryMetrics(b.meterProvider, catalogMetricReader{app: app, raw: components.Metrics})
		if err != nil {
			return nil, err
		}
	}
	reader, err := catalog.NewReader(components.Entries)
	if err != nil {
		return nil, err
	}
	scoped := &catalogScopedSources{Sources: components.Definitions, watch: cfg.WatchNamespace, preflight: preflight}
	admin, err := catalog.NewDefinitionAdmin(scoped, components.Definitions, catalog.DefinitionAdminOptions{
		SyncInline: app.SyncInline, NotifyRefresh: runtime.Refresh,
	})
	if err != nil {
		return nil, err
	}
	publisher, err := catalog.NewPublisher(components.Entries)
	if err != nil {
		return nil, err
	}
	diagnostics, err := catalog.NewDiagnostics(components.Entries, components.Jobs)
	if err != nil {
		return nil, err
	}
	app.audit, err = buildAuditLogger(cfg)
	if err != nil {
		return nil, err
	}
	authenticate, info, err := auth.NewCatalogAuthMiddleware(
		appCtx, cfg.Auth, cfg.Authorization.AllowAnonymousCatalog, auth.DormantValidatorFactory)
	if err != nil {
		return nil, errors.New("catalog authentication initialization failed")
	}
	registrar, err := authorization.NewRegistrar(engine, cfg.Authorization.IdentityMappings, app.audit)
	if err != nil {
		return nil, err
	}
	if b.middlewares == nil {
		b.middlewares = []func(http.Handler) http.Handler{
			middleware.RequestID, middleware.Recoverer, middleware.Timeout(b.requestTimeout),
			api.LoggingMiddleware, securityHeadersMiddleware,
		}
	}
	if err = addObservabilityMiddlewares(b); err != nil {
		return nil, err
	}
	b.middlewares = append([]func(http.Handler) http.Handler{app.track}, b.middlewares...)
	b.middlewares = append(b.middlewares, audit.AuthFailureMiddleware(cfg.Audit, app.audit))
	postAuth := chi.Chain(audit.Middleware(cfg.Audit, app.audit))
	operations := httpapi.AdminOperations{Sources: admin, Views: admin, Publisher: publisher, Diagnostics: diagnostics}
	router, err := api.NewCatalogServer(reader, operations, registrar, cfg.Admin.Enabled,
		func(next http.Handler) http.Handler { return authenticate(postAuth.Handler(next)) },
		api.WithMiddlewares(b.middlewares...), api.WithAuthInfoHandler(info))
	if err != nil {
		return nil, err
	}
	app.public = &http.Server{
		Addr: b.address, Handler: router, ReadTimeout: b.readTimeout,
		WriteTimeout: b.writeTimeout, IdleTimeout: b.idleTimeout,
	}
	app.internal = &http.Server{
		Addr: b.internalAddress, Handler: app.track(api.NewInternalServer(app, b.metricsHandler)),
		ReadTimeout: b.readTimeout, WriteTimeout: b.writeTimeout, IdleTimeout: b.idleTimeout,
	}
	owned = false
	return app, nil
}

// Start binds both listeners before starting the exclusive leased runtime. It is
// nonblocking and single-use. Wait (or Run) reports terminal errors, never log-only.
func (a *CatalogApp) Start() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.state != catalogPrepared {
		return persistence.ErrBusy
	}
	if err := a.ctx.Err(); err != nil {
		a.err = err
		a.finishLocked()
		return err
	}
	public, err := net.Listen("tcp", a.public.Addr)
	if err != nil {
		a.err = err
		a.finishLocked()
		return err
	}
	internal, err := net.Listen("tcp", a.internal.Addr)
	if err != nil {
		_ = public.Close()
		a.err = err
		a.finishLocked()
		return err
	}
	a.public.Addr = public.Addr().String()
	a.internal.Addr = internal.Addr().String()
	a.state = catalogRunning
	go a.serve(public, internal)
	return nil
}

// Run starts and then joins the application, preserving terminal failures.
func (a *CatalogApp) Run() error {
	if err := a.Start(); err != nil {
		if a.ctx.Err() != nil {
			return shutdownresult.Result(err)
		}
		return err
	}
	return a.Wait()
}

// Wait joins the application and reports its terminal result. Before Start it returns ErrBusy.
func (a *CatalogApp) Wait() error {
	a.mu.Lock()
	state := a.state
	a.mu.Unlock()
	if state == catalogPrepared {
		return persistence.ErrBusy
	}
	<-a.done
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.err
}

// Stop cancels admission, active requests and caller-owned inline sync, drains
// listeners, joins all work, then closes owned dependencies. A timeout bounds HTTP
// graceful drain, not permission to close storage underneath an active operation.
func (a *CatalogApp) Stop() error {
	a.mu.Lock()
	if a.state == catalogPrepared {
		a.finishLocked()
		err := a.err
		a.mu.Unlock()
		return err
	}
	if a.state == catalogRunning {
		a.state = catalogStopping
		a.cancel()
	}
	a.mu.Unlock()
	return a.Wait()
}

// SyncInline exposes a tracked caller-owned operation, never the raw runtime.
func (a *CatalogApp) SyncInline(ctx context.Context, name string) error {
	ctx, done, err := a.begin(ctx)
	if err != nil {
		return err
	}
	defer done()
	return a.runtime.SyncInline(ctx, name)
}

// CheckReadiness requires scheduled discovery and every current inline/operator startup to be ready.
func (a *CatalogApp) CheckReadiness(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !a.runtime.Ready() {
		return persistence.ErrUnavailable
	}
	return nil
}

// Addresses returns the actual bound addresses after Start. No raw services are exposed.
func (a *CatalogApp) Addresses() (string, string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.public.Addr, a.internal.Addr
}

func (a *CatalogApp) serve(public, internal net.Listener) {
	results := make(chan error, 3)
	var background sync.WaitGroup
	for _, run := range []func() error{
		func() error { return a.runtime.Run(a.ctx) },
		func() error { return a.public.Serve(public) },
		func() error { return a.internal.Serve(internal) },
	} {
		background.Add(1)
		go func() { defer background.Done(); results <- run() }()
	}
	var terminal error
	select {
	case terminal = <-results:
	case <-a.ctx.Done():
	}
	if errors.Is(terminal, http.ErrServerClosed) {
		terminal = nil
	}
	a.mu.Lock()
	a.state = catalogStopping
	a.cancel()
	a.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), a.timeout)
	var shutdown error
	for _, server := range []*http.Server{a.public, a.internal} {
		if err := server.Shutdown(ctx); err != nil {
			shutdown = errors.Join(shutdown, err)
			_ = server.Close()
		}
	}
	cancel()
	a.inflight.Wait()
	background.Wait()
	close(results)
	for err := range results {
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			terminal = errors.Join(terminal, err)
		}
	}
	a.mu.Lock()
	a.err = errors.Join(terminal, shutdown)
	a.finishLocked()
	a.mu.Unlock()
}

// finishLocked closes admission before releasing the lock to drain callbacks
// and close external dependencies. It returns with the lock held.
func (a *CatalogApp) finishLocked() {
	a.state = catalogStopping
	a.cancel()
	a.mu.Unlock()
	a.inflight.Wait()
	var cleanupErr error
	a.cleanup.Do(func() {
		if a.registryMetrics != nil {
			cleanupErr = errors.Join(cleanupErr, a.registryMetrics.Unregister())
		}
		if a.audit != nil {
			cleanupErr = errors.Join(cleanupErr, a.audit.Close())
		}
		a.storage.Close()
	})
	a.mu.Lock()
	a.err = errors.Join(a.err, cleanupErr)
	a.state = "stopped"
	close(a.done)
}
func (a *CatalogApp) begin(ctx context.Context) (context.Context, func(), error) {
	a.mu.Lock()
	if a.state != catalogRunning || a.ctx.Err() != nil {
		a.mu.Unlock()
		return nil, nil, persistence.ErrBusy
	}
	a.inflight.Add(1)
	a.mu.Unlock()
	work, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(a.ctx, cancel)
	return work, func() { stop(); cancel(); a.inflight.Done() }, nil
}
func (a *CatalogApp) track(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, done, err := a.begin(r.Context())
		if err != nil {
			http.Error(w, "application stopping", http.StatusServiceUnavailable)
			return
		}
		defer done()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

//nolint:gocyclo // Explicit conversion preserves each source specification without dropping unsupported fields.
func catalogDefinitions(cfg *config.Config) ([]persistence.SourceDefinition, []persistence.ViewDefinition, error) {
	sources := make([]persistence.SourceDefinition, 0, len(cfg.Sources))
	for _, s := range cfg.Sources {
		d := persistence.SourceDefinition{Name: s.Name}
		switch s.GetType() {
		case config.SourceTypeGit:
			g := s.Git
			d.Git = &persistence.GitSpec{Repository: g.Repository, Branch: g.Branch, Tag: g.Tag, Commit: g.Commit, Path: g.Path}
			if g.Auth != nil {
				d.Git.Auth = &persistence.GitAuth{Username: g.Auth.Username, PasswordFile: g.Auth.PasswordFile}
			}
		case config.SourceTypeAPI:
			d.API = &persistence.APISpec{Endpoint: s.API.Endpoint, Timeout: s.API.Timeout}
		case config.SourceTypeFile:
			d.File = &persistence.FileSpec{Path: s.File.Path, URL: s.File.URL, Data: s.File.Data, Timeout: s.File.Timeout}
		case config.SourceTypeManaged:
			d.Managed = &persistence.ManagedSpec{}
		case config.SourceTypeKubernetes:
			namespaces, err := storage.EffectiveNamespaces(s.Kubernetes.Namespaces, cfg.WatchNamespace)
			if err != nil {
				return nil, nil, err
			}
			d.Kubernetes = &persistence.KubernetesSpec{Namespaces: namespaces}
		default:
			return nil, nil, persistence.ErrInvalid
		}
		count := 0
		for _, present := range []bool{s.Git != nil, s.API != nil, s.File != nil, s.Managed != nil, s.Kubernetes != nil} {
			if present {
				count++
			}
		}
		if count != 1 {
			return nil, nil, persistence.ErrInvalid
		}
		if s.SyncPolicy != nil {
			if s.IsNonSyncedSource() || (s.File != nil && s.File.Data != "") {
				return nil, nil, fmt.Errorf("%w: source %q has a non-synced syncPolicy block; explicitly remediate before cutover",
					persistence.ErrInvalid, s.Name)
			}
			d.Schedule = s.SyncPolicy.Interval
		}
		if s.Filter != nil {
			d.Filter = &persistence.Filter{Names: s.Filter.Names, Tags: s.Filter.Tags}
		}
		if err := catalogSourcePreflight(d); err != nil {
			return nil, nil, err
		}
		sources = append(sources, d)
	}
	views := make([]persistence.ViewDefinition, 0, len(cfg.Registries))
	for _, v := range cfg.Registries {
		views = append(views, persistence.ViewDefinition{Name: v.Name, Sources: v.Sources})
	}
	return sources, views, persistence.ValidateReconcile(sources, views)
}
func catalogSourcePreflight(d persistence.SourceDefinition) error {
	if _, err := d.Kind(); err != nil {
		return fmt.Errorf("source %q is incompatible; explicitly remediate name/specification and remove non-synced "+
			"schedule/filter (inline filters require explicit remediation, not silent removal): %w", d.Name, err)
	}
	if d.File != nil && d.File.Data != "" {
		snapshot, err := formats.DecodeUpstream([]byte(d.File.Data))
		if err != nil {
			return fmt.Errorf("%w: invalid inline registry; explicitly remediate before cutover", persistence.ErrInvalid)
		}
		if err := persistence.ValidateSnapshot(snapshot); err != nil {
			return fmt.Errorf("%w: incompatible inline registry; explicitly remediate before cutover", persistence.ErrInvalid)
		}
	}
	if d.Filter != nil && d.Filter.Names != nil {
		for _, patterns := range [][]string{d.Filter.Names.Include, d.Filter.Names.Exclude} {
			for _, pattern := range patterns {
				if _, err := filepath.Match(pattern, ""); err != nil {
					return fmt.Errorf("%w: invalid source name filter; explicitly remediate before cutover", persistence.ErrInvalid)
				}
				if _, err := glob.Compile(pattern); err != nil {
					return fmt.Errorf("%w: invalid source name filter; explicitly remediate before cutover", persistence.ErrInvalid)
				}
			}
		}
	}
	return nil
}

type catalogScopedSources struct {
	persistence.Sources
	watch     string
	preflight func(context.Context, persistence.SourceDefinition) error
}

func (s *catalogScopedSources) prepare(
	ctx context.Context, d persistence.SourceDefinition,
) (persistence.SourceDefinition, error) {
	if d.Kubernetes != nil {
		namespaces, err := storage.EffectiveNamespaces(d.Kubernetes.Namespaces, s.watch)
		if err != nil {
			return d, err
		}
		spec := *d.Kubernetes
		spec.Namespaces = namespaces
		d.Kubernetes = &spec
		if err := s.preflight(ctx, d); err != nil {
			return d, err
		}
	}
	return d, catalogSourcePreflight(d)
}
func (s *catalogScopedSources) CreateSource(
	ctx context.Context, d persistence.SourceDefinition,
) (persistence.SourceDefinition, error) {
	d, err := s.prepare(ctx, d)
	if err != nil {
		return d, err
	}
	return s.Sources.CreateSource(ctx, d)
}
func (s *catalogScopedSources) UpdateSource(
	ctx context.Context, n string, d persistence.SourceDefinition,
) (persistence.SourceDefinition, error) {
	d, err := s.prepare(ctx, d)
	if err != nil {
		return d, err
	}
	return s.Sources.UpdateSource(ctx, n, d)
}
