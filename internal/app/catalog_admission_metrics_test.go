package app

import (
	"context"
	"net"
	"net/http"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	otelmetric "go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/internal/app/storage"
	storagemocks "github.com/stacklok/toolhive-registry-server/internal/app/storage/mocks"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

func TestCatalogAnonymousRequiresPermitAndAdminIsAbsent(t *testing.T) {
	t.Parallel()
	cfg := catalogTestConfig(t)
	conn, pool := catalogPG(t, cfg)
	approveCatalog(t, conn, cfg, true)
	app := constructCatalog(t, cfg, pool, nil)
	require.NoError(t, app.Start())
	public, _ := app.Addresses()
	catalogRequest(t, public, "GET", "/registry/public/v0.1/servers", "", "", 200)
	catalogRequest(t, public, "GET", "/openapi.json", "", "", 403)
	for _, path := range []string{"/v1/sources", "/v1/registries", "/v1/entries", "/v1/me", "/v1/entries/server/name/claims"} {
		catalogRequest(t, public, "GET", path, "", "", 404)
	}
	require.NoError(t, app.Stop())
	require.NoError(t, os.WriteFile(cfg.Authorization.PolicyFile, []byte(`forbid(principal,action,resource);`), 0600))
	denied := constructCatalog(t, cfg, pool, nil)
	require.NoError(t, denied.Start())
	public, _ = denied.Addresses()
	catalogRequest(t, public, "GET", "/registry/public/v0.1/servers", "", "", 403)
	require.NoError(t, denied.Stop())
}

func TestCatalogCancelledContextAndFailedOpenCleanUp(t *testing.T) {
	t.Parallel()
	cfg := catalogTestConfig(t)
	ctrl := gomock.NewController(t)
	factory := storagemocks.NewMockCatalogFactory(ctrl)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	app, err := NewCatalogApp(ctx, CatalogOptions{Config: cfg, Storage: factory})
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, app)
	store := storagemocks.NewMockCatalogStorage(ctrl)
	factory.EXPECT().Open(gomock.Any(), gomock.Any()).Return(store, context.Canceled)
	store.EXPECT().Close()
	app, err = NewCatalogApp(t.Context(), CatalogOptions{Config: cfg, Storage: factory})
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, app)
}

func TestCatalogMetricsUsePublicStorageAndSeparatePort(t *testing.T) {
	t.Parallel()
	cfg := catalogTestConfig(t)
	conn, pool := catalogPG(t, cfg)
	approveCatalog(t, conn, cfg, true)
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	app, err := NewCatalogApp(t.Context(), CatalogOptions{Config: cfg, Storage: storage.CatalogPostgres{BorrowedPool: pool}, HTTP: []RegistryAppOptions{
		WithAddress("127.0.0.1:0"), WithInternalAddress("127.0.0.1:0"), WithMeterProvider(provider),
		WithMetricsHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })),
	}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = app.Stop() })
	require.NoError(t, app.Start())
	require.Eventually(t, func() bool { return app.runtime.Ready() }, time.Second, 10*time.Millisecond)
	public, internal := app.Addresses()
	catalogRequest(t, public, "GET", "/metrics", "", "", 404)
	catalogRequest(t, internal, "GET", "/metrics", "", "", 200)
	var metrics metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &metrics))
	var found bool
	for _, scope := range metrics.ScopeMetrics {
		for _, metric := range scope.Metrics {
			if metric.Name == "stacklok.registry.servers" {
				found = true
			}
		}
	}
	require.True(t, found)
	require.NoError(t, app.Stop())
	require.NoError(t, pool.Ping(t.Context()))
	require.NoError(t, reader.Collect(t.Context(), &metrics), "provider-owned callbacks must be unregistered after join")
}

func TestCatalogStopReleasesLockBeforeMetricsUnregister(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{"prepared", "failed-start", "running"} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			cfg := catalogTestConfig(t)
			conn, pool := catalogPG(t, cfg)
			approveCatalog(t, conn, cfg, true)
			reader := sdkmetric.NewManualReader()
			provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
			t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
			entered, release, drained := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var app *CatalogApp
			var closed atomic.Bool
			gauge, err := provider.Meter("barrier").Int64ObservableGauge("barrier")
			require.NoError(t, err)
			registration, err := provider.Meter("barrier").RegisterCallback(func(ctx context.Context, _ otelmetric.Observer) error {
				var done func()
				if stage == "running" {
					_, done, err = app.begin(ctx)
					if err != nil {
						return err
					}
				}
				close(entered)
				<-release
				if done != nil {
					done()
				}
				// Collection still holds the SDK lock. Stop must not hold a.mu
				// while waiting to unregister its callback from this pipeline.
				_, _, admissionErr := app.begin(ctx)
				if admissionErr != persistence.ErrBusy {
					return admissionErr
				}
				close(drained)
				return nil
			}, gauge)
			require.NoError(t, err)
			t.Cleanup(func() { _ = registration.Unregister() })
			app, err = NewCatalogApp(t.Context(), CatalogOptions{Config: cfg, Storage: storage.CatalogPostgres{BorrowedPool: pool},
				HTTP: []RegistryAppOptions{WithAddress("127.0.0.1:0"), WithInternalAddress("127.0.0.1:0"), WithMeterProvider(provider)}})
			require.NoError(t, err)
			store := storagemocks.NewMockCatalogStorage(gomock.NewController(t))
			store.EXPECT().Close().Do(func() {
				select {
				case <-drained:
				default:
					t.Error("dependency closed before callback drained")
				}
				closed.Store(true)
			})
			app.storage = store
			if stage == "running" {
				require.NoError(t, app.Start())
			}
			var occupied net.Listener
			if stage == "failed-start" {
				occupied, err = net.Listen("tcp", "127.0.0.1:0")
				require.NoError(t, err)
				defer occupied.Close()
				app.public.Addr = occupied.Addr().String()
			}
			collected := make(chan error, 1)
			go func() { var data metricdata.ResourceMetrics; collected <- reader.Collect(t.Context(), &data) }()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("collection did not reach barrier")
			}
			stopped := make(chan error, 1)
			go func() {
				if stage == "failed-start" {
					stopped <- app.Start()
				} else {
					stopped <- app.Stop()
				}
			}()
			require.Eventually(t, func() bool {
				if !app.mu.TryLock() {
					return false
				}
				defer app.mu.Unlock()
				return app.state == "stopping"
			}, 5*time.Second, time.Millisecond, "cleanup must release the app lock while awaiting callbacks")
			require.False(t, closed.Load())
			close(release)
			for _, result := range []<-chan error{collected, stopped} {
				select {
				case err := <-result:
					if result == collected {
						require.ErrorIs(t, err, persistence.ErrBusy, "the catalog callback must reject admission after shutdown")
					} else if stage == "failed-start" {
						require.Error(t, err)
					} else {
						require.NoError(t, err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("collection and cleanup must terminate")
				}
			}
			require.True(t, closed.Load())
		})
	}
}
