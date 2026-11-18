package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/internal/app/storage"
	"github.com/stacklok/toolhive-registry-server/internal/app/storage/mocks"
	"github.com/stacklok/toolhive-registry-server/internal/auth"
	"github.com/stacklok/toolhive-registry-server/internal/claims"
	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/sync/standalone"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/catalog"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/ingest"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/reconcile"
	reconcilemocks "github.com/stacklok/toolhive-registry-server/pkg/registry/reconcile/mocks"
)

// catalogTestConfig is an anonymous-mode config with an inline file source,
// a managed source and one registry over both.
func catalogTestConfig(db *config.DatabaseConfig) *config.Config {
	return &config.Config{
		Database: db,
		Auth:     &config.AuthConfig{Mode: config.AuthModeAnonymous},
		Sources: []config.SourceConfig{
			{Name: "inline", File: &config.FileConfig{Data: validInlineRegistry}},
			{Name: "managed-src", Managed: &config.ManagedConfig{}},
		},
		Registries: []config.RegistryConfig{{Name: "main", Sources: []string{"inline", "managed-src"}}},
	}
}

// fastReconcile keeps lease/poll timing short so sync tests finish quickly.
var fastReconcile = reconcile.Options{PollInterval: 50 * time.Millisecond, LeaseDuration: time.Minute}

func newTestRegistryApp(t *testing.T, opts ...RegistryAppOptions) *RegistryApp {
	t.Helper()
	app, err := NewRegistryApp(context.Background(), opts...)
	require.NoError(t, err)
	require.NotNil(t, app)
	t.Cleanup(func() {
		_ = app.components.Sync.Stop()
		app.cancelFunc()
	})
	return app
}

// runSync starts the app's background sync and stops it at test cleanup.
func runSync(t *testing.T, bg BackgroundSync) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- bg.Start(context.Background()) }()
	t.Cleanup(func() {
		require.NoError(t, bg.Stop())
		select {
		case err := <-done:
			assert.NoError(t, err)
		case <-time.After(10 * time.Second):
			t.Error("sync did not stop")
		}
	})
}

func TestNewRegistryApp_WithCatalog(t *testing.T) {
	t.Parallel()
	cfg := catalogTestConfig(newTestDatabaseConfig(t))

	app := newTestRegistryApp(t, WithConfig(cfg), WithReconcileOptions(fastReconcile))

	require.NotNil(t, app.components)
	require.NotNil(t, app.components.RegistryService)
	runner, ok := app.components.Sync.(*syncRunner)
	require.True(t, ok, "Sync is the library sync runner")
	require.NotNil(t, runner.runtime)
	assert.False(t, runner.Ready(), "nothing runs until Start")
	assert.Equal(t, defaultHTTPAddress, app.GetHTTPServer().Addr)
	assert.Equal(t, defaultInternalHTTPAddress, app.GetInternalHTTPServer().Addr)
	assert.Same(t, cfg, app.GetConfig())

	// The inline source is synced by the runtime and served by the consumer API.
	runSync(t, app.components.Sync)
	require.Eventually(t, func() bool {
		rec := httptest.NewRecorder()
		app.GetHTTPServer().Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/registry/main/v0.1/servers", nil))
		return rec.Code == http.StatusOK && strings.Contains(rec.Body.String(), "io.example/weather")
	}, 15*time.Second, 50*time.Millisecond, "inline server never appeared in the consumer API")
	require.Eventually(t, runner.Ready, 5*time.Second, 20*time.Millisecond)
}

func TestNewRegistryApp_UsesInjectedFetcher(t *testing.T) {
	t.Parallel()
	cfg := catalogTestConfig(newTestDatabaseConfig(t))

	var once sync.Once
	called := make(chan persistence.SourceDefinition, 1)
	fetcher := reconcilemocks.NewMockFetcher(gomock.NewController(t))
	fetcher.EXPECT().Fetch(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, d persistence.SourceDefinition) (ingest.Result, error) {
			once.Do(func() { called <- d })
			// Block until Stop: injected I/O must honor cancellation
			// (standalone.Options), and this keeps shutdown deterministic.
			<-ctx.Done()
			return ingest.Result{}, ctx.Err()
		}).AnyTimes()

	recorder := tracetest.NewSpanRecorder()
	tracer := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = tracer.Shutdown(context.Background()) })

	app := newTestRegistryApp(t,
		WithConfig(cfg), WithReconcileOptions(fastReconcile), WithFetcher(fetcher), WithTracerProvider(tracer))
	runSync(t, app.components.Sync)

	select {
	case d := <-called:
		assert.Equal(t, "inline", d.Name)
		require.NotNil(t, d.File)
		assert.Equal(t, validInlineRegistry, d.File.Data)
	case <-time.After(15 * time.Second):
		t.Fatal("injected fetcher was never called")
	}
	require.Eventually(t, func() bool {
		for _, s := range recorder.Started() {
			if s.Name() == "sync.fetch" {
				return true
			}
		}
		return false
	}, 5*time.Second, 20*time.Millisecond, "a tracer provider wraps the fetcher in a span")
}

func TestNewRegistryApp_WithMeterProvider(t *testing.T) {
	t.Parallel()
	cfg := catalogTestConfig(newTestDatabaseConfig(t))
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	app := newTestRegistryApp(t, WithConfig(cfg), WithReconcileOptions(fastReconcile), WithMeterProvider(provider))
	runSync(t, app.components.Sync)

	// The inline sync completes through measuredJobs, which records it.
	require.Eventually(t, func() bool {
		var rm metricdata.ResourceMetrics
		if err := reader.Collect(context.Background(), &rm); err != nil {
			return false
		}
		_, ok := findMetric(rm, syncDurationMetric)
		return ok
	}, 15*time.Second, 50*time.Millisecond, "sync duration was never recorded")
}

func TestNewRegistryApp_InvalidSourceConfig(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	factory := mocks.NewMockFactory(ctrl)
	factory.EXPECT().Cleanup().Times(1)

	cfg := &config.Config{
		Auth:    &config.AuthConfig{Mode: config.AuthModeAnonymous},
		Sources: []config.SourceConfig{{Name: "bad", File: &config.FileConfig{Data: "{not json"}}},
	}
	app, err := NewRegistryApp(context.Background(), WithConfig(cfg), WithStorageFactory(factory))
	require.Error(t, err)
	assert.Nil(t, app)
	assert.Contains(t, err.Error(), "failed to build catalog")
	assert.Contains(t, err.Error(), "invalid sources or registries")
	assert.ErrorIs(t, err, persistence.ErrInvalid)
}

func TestNewRegistryApp_CreateCatalogFails(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	factory := mocks.NewMockFactory(ctrl)
	storageErr := errors.New("connection refused")
	// Annotation claims are recorded with or without auth.authz, as in v1.5.4.
	factory.EXPECT().CreateCatalog(gomock.Any(), gomock.Not(gomock.Nil())).Return(nil, storageErr)
	factory.EXPECT().Cleanup().Times(1)

	app, err := NewRegistryApp(context.Background(),
		WithConfig(catalogTestConfig(nil)), WithStorageFactory(factory))
	require.Error(t, err)
	assert.Nil(t, app)
	assert.Contains(t, err.Error(), "failed to build catalog")
	assert.Contains(t, err.Error(), "failed to open catalog storage")
	assert.ErrorIs(t, err, storageErr)
}

func TestNewRegistryApp_CreateCatalogReceivesAnnotationOverrides(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	factory := mocks.NewMockFactory(ctrl)
	factory.EXPECT().CreateCatalog(gomock.Any(), gomock.Not(gomock.Nil())).Return(nil, errors.New("stop here"))
	factory.EXPECT().Cleanup().Times(1)

	cfg := catalogTestConfig(nil)
	cfg.Auth.Authz = &config.AuthzConfig{}
	_, err := NewRegistryApp(context.Background(), WithConfig(cfg), WithStorageFactory(factory))
	require.Error(t, err, "with authz the catalog is opened with kubernetes annotation claim overrides")
}

func TestNewRegistryApp_CreateRegistryServiceFails(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cfg := catalogTestConfig(newTestDatabaseConfig(t))
	dbFactory, err := storage.NewDatabaseFactory(ctx, cfg)
	require.NoError(t, err)
	t.Cleanup(dbFactory.Cleanup)

	ctrl := gomock.NewController(t)
	factory := mocks.NewMockFactory(ctrl)
	factory.EXPECT().CreateCatalog(gomock.Any(), gomock.Any()).DoAndReturn(dbFactory.CreateCatalog)
	factory.EXPECT().CreateRegistryService(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("no service"))
	factory.EXPECT().Cleanup().Times(1)

	_, err = NewRegistryApp(ctx, WithConfig(cfg), WithStorageFactory(factory))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to build service components")
}

func TestWithReconcileOptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		opts        reconcile.Options
		errContains string
	}{
		{name: "valid", opts: reconcile.Options{PollInterval: time.Second, LeaseDuration: 30 * time.Second}},
		{name: "max lease", opts: reconcile.Options{PollInterval: time.Millisecond, LeaseDuration: 24 * time.Hour}},
		{
			name:        "zero poll interval",
			opts:        reconcile.Options{LeaseDuration: time.Minute},
			errContains: "sync poll interval must be positive",
		},
		{
			name:        "negative poll interval",
			opts:        reconcile.Options{PollInterval: -time.Second, LeaseDuration: time.Minute},
			errContains: "sync poll interval must be positive",
		},
		{
			name:        "zero lease",
			opts:        reconcile.Options{PollInterval: time.Second},
			errContains: "invalid sync lease duration",
		},
		{
			name:        "lease above 24h",
			opts:        reconcile.Options{PollInterval: time.Second, LeaseDuration: 25 * time.Hour},
			errContains: "invalid sync lease duration",
		},
		{
			name:        "sub-microsecond lease",
			opts:        reconcile.Options{PollInterval: time.Second, LeaseDuration: time.Second + time.Nanosecond},
			errContains: "invalid sync lease duration",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg, err := baseConfig(WithReconcileOptions(tt.opts))
			if tt.errContains != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				assert.Nil(t, cfg)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.opts, cfg.reconcile)
		})
	}
}

func TestNewRegistryApp_InvalidReconcileOptions(t *testing.T) {
	t.Parallel()
	app, err := NewRegistryApp(context.Background(),
		WithConfig(catalogTestConfig(nil)), WithReconcileOptions(reconcile.Options{}))
	require.Error(t, err)
	assert.Nil(t, app)
	assert.Contains(t, err.Error(), "failed to build base configuration")
	assert.Contains(t, err.Error(), "sync poll interval must be positive")
}

func TestBaseConfig_ReconcileDefaultsAndFetcher(t *testing.T) {
	t.Parallel()
	cfg, err := baseConfig()
	require.NoError(t, err)
	assert.Equal(t, reconcile.Options{PollInterval: defaultSyncPollInterval, LeaseDuration: defaultSyncLeaseDuration}, cfg.reconcile)
	assert.Nil(t, cfg.fetcher)

	fetcher := reconcilemocks.NewMockFetcher(gomock.NewController(t))
	cfg, err = baseConfig(WithFetcher(fetcher))
	require.NoError(t, err)
	assert.Same(t, fetcher, cfg.fetcher)
}

// fakeOperator is a catalogOperator whose preflight result is configurable
// and whose workers report readiness immediately.
type fakeOperator struct {
	preflightErr error
	preflighted  atomic.Int32
	workers      atomic.Int32
}

func (o *fakeOperator) Preflight(_ context.Context, d persistence.SourceDefinition) error {
	if d.Kubernetes == nil {
		return errors.New("operator preflight called for a non-kubernetes source")
	}
	o.preflighted.Add(1)
	return o.preflightErr
}

func (o *fakeOperator) Worker(
	jobs persistence.Jobs, _ persistence.SourceDefinition, _ reconcile.Options,
) (standalone.Worker, error) {
	if jobs == nil {
		return nil, errors.New("no jobs")
	}
	o.workers.Add(1)
	return &idleWorker{}, nil
}

type idleWorker struct{ ready atomic.Bool }

func (w *idleWorker) Ready() bool { return w.ready.Load() }
func (w *idleWorker) Run(ctx context.Context) error {
	w.ready.Store(true)
	<-ctx.Done()
	return nil
}

func newBuildCatalogConfig(t *testing.T, cfg *config.Config) *registryAppConfig {
	t.Helper()
	b, err := baseConfig(WithConfig(cfg), WithReconcileOptions(fastReconcile))
	require.NoError(t, err)
	factory, err := storage.NewDatabaseFactory(context.Background(), cfg)
	require.NoError(t, err)
	t.Cleanup(factory.Cleanup)
	b.storageFactory = factory
	return b
}

func TestBuildCatalog_KubernetesSourceUsesOperator(t *testing.T) {
	t.Parallel()
	cfg := catalogTestConfig(newTestDatabaseConfig(t))
	cfg.Sources = append(cfg.Sources, config.SourceConfig{
		Name: "k8s-src", Kubernetes: &config.KubernetesConfig{Namespaces: []string{"default"}},
		Claims: map[string]any{"org": "acme"},
	})
	operator := &fakeOperator{}
	b := newBuildCatalogConfig(t, cfg)
	b.operator = operator

	built, err := buildCatalog(context.Background(), b)
	require.NoError(t, err)
	require.NotNil(t, built.catalog)
	require.NotNil(t, built.pages)
	assert.Equal(t, int32(1), operator.preflighted.Load(), "only the kubernetes source reaches operator preflight")

	runner := built.sync
	done := make(chan error, 1)
	go func() { done <- runner.Start(context.Background()) }()
	require.Eventually(t, func() bool { return operator.workers.Load() == 1 }, 10*time.Second, 20*time.Millisecond,
		"the runtime builds a worker for the kubernetes source through the operator")
	require.NoError(t, runner.Stop())
	require.NoError(t, <-done)
}

func TestBuildCatalog_OperatorPreflightFailure(t *testing.T) {
	t.Parallel()
	cfg := catalogTestConfig(newTestDatabaseConfig(t))
	cfg.Sources = append(cfg.Sources, config.SourceConfig{Name: "k8s-src", Kubernetes: &config.KubernetesConfig{}})
	preflightErr := errors.New("CRDs not installed")
	b := newBuildCatalogConfig(t, cfg)
	b.operator = &fakeOperator{preflightErr: preflightErr}

	built, err := buildCatalog(context.Background(), b)
	require.ErrorIs(t, err, preflightErr)
	assert.Nil(t, built)

	// Nothing was reconciled: preflight runs before definitions are written.
	sources, err := listPersistedSources(t, b)
	require.NoError(t, err)
	assert.Empty(t, sources)
}

// listPersistedSources lists persisted sources through a fresh catalog handle.
func listPersistedSources(t *testing.T, b *registryAppConfig) ([]persistence.SourceDefinition, error) {
	t.Helper()
	cat, err := b.storageFactory.CreateCatalog(context.Background(), nil)
	require.NoError(t, err)
	return cat.Definitions.ListSources(context.Background())
}

func TestBuildCatalog_CancelledContext(t *testing.T) {
	t.Parallel()
	cfg := catalogTestConfig(newTestDatabaseConfig(t))
	b := newBuildCatalogConfig(t, cfg)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := buildCatalog(ctx, b)
	require.Error(t, err)
}

func TestBuildCatalog_ReconcilesAndSetsManagedSourceIDs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cfg := catalogTestConfig(newTestDatabaseConfig(t))
	cfg.Sources[1].Claims = map[string]any{"org": "acme"}
	cfg.Registries[0].Claims = map[string]any{"org": "acme"}
	b := newBuildCatalogConfig(t, cfg)

	built, err := buildCatalog(ctx, b)
	require.NoError(t, err)

	sources, err := built.catalog.Definitions.ListSources(ctx)
	require.NoError(t, err)
	byName := map[string]persistence.SourceDefinition{}
	for _, s := range sources {
		byName[s.Name] = s
		assert.Equal(t, persistence.OriginConfig, s.Origin)
	}
	require.Contains(t, byName, "inline")
	require.Contains(t, byName, "managed-src")
	view, err := built.catalog.Definitions.GetView(ctx, "main")
	require.NoError(t, err)
	assert.Equal(t, []string{"inline", "managed-src"}, view.Sources)

	var sourceClaims, registryClaims string
	require.NoError(t, built.catalog.Pool.QueryRow(ctx, `SELECT claims::text FROM source WHERE name='managed-src'`).Scan(&sourceClaims))
	require.NoError(t, built.catalog.Pool.QueryRow(ctx, `SELECT claims::text FROM registry WHERE name='main'`).Scan(&registryClaims))
	assert.JSONEq(t, `{"org":"acme"}`, sourceClaims)
	assert.JSONEq(t, `{"org":"acme"}`, registryClaims)

	require.NotNil(t, b.managedSourceIDs)
	ids, err := b.managedSourceIDs(ctx)
	require.NoError(t, err)
	assert.Equal(t, map[string]bool{byName["managed-src"].ID: true}, ids)

	// A second build against the same database (a restart) is idempotent.
	_, err = buildCatalog(ctx, b)
	require.NoError(t, err)
}

func TestBuildCatalog_ClaimsReaderWhenAuthzConfigured(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	claimBearer := auth.ContextWithClaims(ctx, jwt.MapClaims{"sub": "u", "org": "acme"})
	query := catalog.Query{Kind: persistence.ServerKind, Limit: 10}

	tests := []struct {
		name          string
		authz         *config.AuthzConfig
		wantForbidden bool
	}{
		// Registry "main" has no claims: the claims reader denies it to any
		// claim-bearing caller, the plain library reader does not gate it.
		{name: "authz configured gates the registry", authz: &config.AuthzConfig{}, wantForbidden: true},
		{name: "no authz reads through the library reader"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := catalogTestConfig(newTestDatabaseConfig(t))
			cfg.Auth.Authz = tt.authz
			built, err := buildCatalog(ctx, newBuildCatalogConfig(t, cfg))
			require.NoError(t, err)

			_, err = built.pages.List(claimBearer, "main", query)
			if tt.wantForbidden {
				require.ErrorIs(t, err, claims.ErrRegistryForbidden)
			} else {
				require.NoError(t, err)
			}
			_, err = built.pages.List(ctx, "main", query)
			require.NoError(t, err, "anonymous callers are never gated")
		})
	}
}
