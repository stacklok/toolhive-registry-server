package app

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric/noop"
	tracenoop "go.opentelemetry.io/otel/trace/noop"

	"github.com/stacklok/toolhive-registry-server/internal/config"
)

func createValidTestConfig() *config.Config {
	path, err := filepath.Abs("testdata/admission.cedar")
	if err != nil {
		panic(err)
	}
	return &config.Config{
		Authorization: &config.AuthorizationConfig{PolicyFile: path, AllowAnonymousCatalog: true},
		Sources:       []config.SourceConfig{{Name: "test-registry-1", File: &config.FileConfig{Path: "/tmp/test-registry.json"}, SyncPolicy: &config.SyncPolicyConfig{Interval: "30m"}}},
		Auth:          &config.AuthConfig{Mode: config.AuthModeAnonymous},
	}
}

func TestCatalogHTTPOptions(t *testing.T) {
	t.Parallel()
	cfg, err := baseConfig()
	require.NoError(t, err)
	require.Equal(t, defaultHTTPAddress, cfg.address)
	require.Equal(t, defaultInternalHTTPAddress, cfg.internalAddress)
	require.Equal(t, defaultRequestTimeout, cfg.requestTimeout)
	require.Equal(t, defaultReadTimeout, cfg.readTimeout)
	require.Equal(t, defaultWriteTimeout, cfg.writeTimeout)
	require.Equal(t, defaultIdleTimeout, cfg.idleTimeout)
	mp, tp := noop.NewMeterProvider(), tracenoop.NewTracerProvider()
	h := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	cfg, err = baseConfig(WithAddress(":9090"), WithInternalAddress("localhost:9091"), WithMeterProvider(mp), WithTracerProvider(tp), WithMetricsHandler(h), WithMiddlewares(securityHeadersMiddleware))
	require.NoError(t, err)
	require.Equal(t, ":9090", cfg.address)
	require.Equal(t, "localhost:9091", cfg.internalAddress)
	require.Equal(t, mp, cfg.meterProvider)
	require.Equal(t, tp, cfg.tracerProvider)
	require.NotNil(t, cfg.metricsHandler)
	require.Len(t, cfg.middlewares, 1)
	require.NoError(t, addObservabilityMiddlewares(cfg))
	require.Greater(t, len(cfg.middlewares), 1)
	failure := errors.New("option failed")
	cfg, err = baseConfig(func(*registryAppConfig) error { return failure })
	require.ErrorIs(t, err, failure)
	require.Nil(t, cfg)
}

func TestCatalogHTTPAddressValidation(t *testing.T) {
	t.Parallel()
	for _, addr := range []string{":9999", "127.0.0.1:9999", "localhost:9999", "", ":", "localhost:999999", "missing-port"} {
		t.Run(addr, func(t *testing.T) {
			t.Parallel()
			valid := addr == ":9999" || addr == "127.0.0.1:9999" || addr == "localhost:9999"
			for _, option := range []RegistryAppOptions{WithAddress(addr), WithInternalAddress(addr)} {
				_, err := baseConfig(option)
				if valid {
					require.NoError(t, err)
				} else {
					require.Error(t, err)
				}
			}
		})
	}
}

func TestSecurityHeadersMiddleware(t *testing.T) {
	t.Parallel()
	w := httptest.NewRecorder()
	securityHeadersMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(403) })).ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	require.Equal(t, 403, w.Code)
	require.Equal(t, "nosniff", w.Header().Get("X-Content-Type-Options"))
	require.Equal(t, "same-origin", w.Header().Get("Cross-Origin-Resource-Policy"))
}

func TestCatalogAuditLoggerDisabled(t *testing.T) {
	t.Parallel()
	for _, cfg := range []*config.Config{nil, {}, {Audit: &config.AuditConfig{Enabled: false}}} {
		logger, err := buildAuditLogger(cfg)
		require.NoError(t, err)
		require.Nil(t, logger)
	}
}
