package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/internal/app/storage/mocks"
	"github.com/stacklok/toolhive-registry-server/internal/config"
	mocksvc "github.com/stacklok/toolhive-registry-server/internal/service/mocks"
)

func TestNewRegistryAppBuilder(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		Sources: []config.SourceConfig{
			{
				Name: "test-registry-1",
				File: &config.FileConfig{
					Path: "/tmp/test-registry.json",
				},
				SyncPolicy: &config.SyncPolicyConfig{
					Interval: "30m",
				},
			},
		},
		// Auth is required by default; use anonymous mode for tests
		Auth: &config.AuthConfig{
			Mode: config.AuthModeAnonymous,
		},
	}

	built, err := baseConfig(WithConfig(cfg))
	require.NoError(t, err)
	require.NotNil(t, built)
	assert.Equal(t, defaultHTTPAddress, built.address)
}

func TestRegistryAppWithFunctions(t *testing.T) {
	t.Parallel()
	built, err := baseConfig(
		WithConfig(createValidTestConfig()),
		WithAddress(":9090"),
	)
	require.NoError(t, err)
	require.NotNil(t, built)
}

func TestRegistryAppWithFunctionsError(t *testing.T) {
	t.Parallel()
	built, err := baseConfig(
		WithConfig(createValidTestConfig()),
		WithAddress(":"),
	)
	require.Error(t, err)
	require.Nil(t, built)
}

func TestRegistryAppBuilder_WithAddress(t *testing.T) {
	t.Parallel()
	built, err := baseConfig(
		WithConfig(createValidTestConfig()),
		WithAddress(":9090"),
	)
	require.NoError(t, err)
	assert.Equal(t, ":9090", built.address)
}

// createValidTestConfig creates a minimal valid config for testing
func createValidTestConfig() *config.Config {
	return &config.Config{
		Sources: []config.SourceConfig{
			{
				Name: "test-registry-1",
				File: &config.FileConfig{
					Path: "/tmp/test-registry.json",
				},
				SyncPolicy: &config.SyncPolicyConfig{
					Interval: "30m",
				},
			},
		},
		// Auth is required by default; use anonymous mode for tests
		Auth: &config.AuthConfig{
			Mode: config.AuthModeAnonymous,
		},
	}
}

func TestWithConfig(t *testing.T) {
	t.Parallel()
	cfg := &registryAppConfig{}
	testConfig := createValidTestConfig()

	opt := WithConfig(testConfig)
	err := opt(cfg)

	require.NoError(t, err)
	assert.Equal(t, testConfig, cfg.config)
}

func TestWithAddress(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		address string
		want    string
		wantErr bool
	}{
		{name: "valid address", address: ":9999", want: ":9999"},
		{name: "valid address with host", address: "127.0.0.1:9999", want: "127.0.0.1:9999"},
		{name: "valid address with host and port", address: "localhost:9999", want: "localhost:9999"},
		{name: "invalid empty address", address: "", want: "", wantErr: true},
		{name: "invalid empty port", address: ":", want: "", wantErr: true},
		{name: "invalid address with host and port", address: "localhost:999999", want: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := &registryAppConfig{}
			opt := WithAddress(tt.address)
			err := opt(cfg)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, cfg.address)
		})
	}
}

func TestWithMiddlewares(t *testing.T) {
	t.Parallel()
	cfg := &registryAppConfig{}
	middleware1 := func(next http.Handler) http.Handler { return next }
	middleware2 := func(next http.Handler) http.Handler { return next }

	opt := WithMiddlewares(middleware1, middleware2)
	err := opt(cfg)

	require.NoError(t, err)
	assert.Len(t, cfg.middlewares, 2)
}

func TestWithStorageFactory(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	cfg := &registryAppConfig{}
	testFactory := mocks.NewMockFactory(ctrl)

	opt := WithStorageFactory(testFactory)
	err := opt(cfg)

	require.NoError(t, err)
	assert.Equal(t, testFactory, cfg.storageFactory)
}

func TestWithMeterProvider(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		provider metric.MeterProvider
		wantNil  bool
	}{
		{
			name:     "sets noop meter provider",
			provider: noop.NewMeterProvider(),
			wantNil:  false,
		},
		{
			name:     "sets nil meter provider",
			provider: nil,
			wantNil:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := &registryAppConfig{}

			opt := WithMeterProvider(tt.provider)
			err := opt(cfg)

			require.NoError(t, err)
			if tt.wantNil {
				assert.Nil(t, cfg.meterProvider)
			} else {
				assert.Equal(t, tt.provider, cfg.meterProvider)
			}
		})
	}
}

func TestWithMetricsHandler(t *testing.T) {
	t.Parallel()

	t.Run("sets a non-nil handler", func(t *testing.T) {
		t.Parallel()
		cfg := &registryAppConfig{}
		const body = "handler-identity-marker"
		handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(body))
		})

		err := WithMetricsHandler(handler)(cfg)
		require.NoError(t, err)
		require.NotNil(t, cfg.metricsHandler)

		rr := httptest.NewRecorder()
		cfg.metricsHandler.ServeHTTP(rr, httptest.NewRequest("GET", "/metrics", nil))
		assert.Equal(t, body, rr.Body.String(), "cfg.metricsHandler should be the exact handler passed in")
	})

	t.Run("sets nil handler", func(t *testing.T) {
		t.Parallel()
		cfg := &registryAppConfig{}

		err := WithMetricsHandler(nil)(cfg)

		require.NoError(t, err)
		assert.Nil(t, cfg.metricsHandler)
	})
}

func TestBuildInternalHTTPServer(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	mockSvc := mocksvc.NewMockRegistryService(ctrl)

	t.Run("mounts the configured metrics handler at /metrics", func(t *testing.T) {
		t.Parallel()

		const body = "# fake metrics\n"
		cfg := &registryAppConfig{
			internalAddress: ":8081",
			metricsHandler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(body))
			}),
		}

		server := buildInternalHTTPServer(cfg, mockSvc)

		req := httptest.NewRequest("GET", "/metrics", nil)
		rr := httptest.NewRecorder()
		server.Handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
		assert.Equal(t, body, rr.Body.String())
	})

	t.Run("omits /metrics when no handler is configured", func(t *testing.T) {
		t.Parallel()

		cfg := &registryAppConfig{internalAddress: ":8081"}

		server := buildInternalHTTPServer(cfg, mockSvc)

		req := httptest.NewRequest("GET", "/metrics", nil)
		rr := httptest.NewRecorder()
		server.Handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusNotFound, rr.Code)
	})
}

func TestBuildHTTPServer(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		name           string
		config         *registryAppConfig
		setupMock      func(*mocksvc.MockRegistryService)
		wantAddr       string
		wantReadTO     time.Duration
		wantWriteTO    time.Duration
		wantIdleTO     time.Duration
		expectDefaults bool
	}{
		{
			name: "with default middlewares",
			config: &registryAppConfig{
				address:        ":8080",
				middlewares:    nil, // nil triggers default middlewares
				requestTimeout: 10 * time.Second,
				readTimeout:    10 * time.Second,
				writeTimeout:   15 * time.Second,
				idleTimeout:    60 * time.Second,
			},
			setupMock:      func(_ *mocksvc.MockRegistryService) {},
			wantAddr:       ":8080",
			wantReadTO:     10 * time.Second,
			wantWriteTO:    15 * time.Second,
			wantIdleTO:     60 * time.Second,
			expectDefaults: true,
		},
		{
			name: "with custom middlewares",
			config: &registryAppConfig{
				address: ":9090",
				middlewares: []func(http.Handler) http.Handler{
					func(next http.Handler) http.Handler { return next },
				},
				requestTimeout: 5 * time.Second,
				readTimeout:    5 * time.Second,
				writeTimeout:   10 * time.Second,
				idleTimeout:    30 * time.Second,
			},
			setupMock:      func(_ *mocksvc.MockRegistryService) {},
			wantAddr:       ":9090",
			wantReadTO:     5 * time.Second,
			wantWriteTO:    10 * time.Second,
			wantIdleTO:     30 * time.Second,
			expectDefaults: false,
		},
		{
			name: "with custom address and timeouts",
			config: &registryAppConfig{
				address:        "127.0.0.1:3000",
				middlewares:    nil,
				requestTimeout: 20 * time.Second,
				readTimeout:    20 * time.Second,
				writeTimeout:   30 * time.Second,
				idleTimeout:    120 * time.Second,
			},
			setupMock:      func(_ *mocksvc.MockRegistryService) {},
			wantAddr:       "127.0.0.1:3000",
			wantReadTO:     20 * time.Second,
			wantWriteTO:    30 * time.Second,
			wantIdleTO:     120 * time.Second,
			expectDefaults: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockSvc := mocksvc.NewMockRegistryService(ctrl)
			tt.setupMock(mockSvc)

			// Set auth middleware in config for tests
			tt.config.authMiddleware = func(next http.Handler) http.Handler { return next }
			tt.config.authInfoHandler = nil
			server, err := buildHTTPServer(ctx, tt.config, mockSvc, emptyPages{}, nil)

			require.NoError(t, err)
			require.NotNil(t, server)
			assert.Equal(t, tt.wantAddr, server.Addr)
			assert.Equal(t, tt.wantReadTO, server.ReadTimeout)
			assert.Equal(t, tt.wantWriteTO, server.WriteTimeout)
			assert.Equal(t, tt.wantIdleTO, server.IdleTimeout)
			assert.NotNil(t, server.Handler)

			// Verify middlewares were set
			// Note: auth middleware is always appended, so counts are +1
			if tt.expectDefaults {
				assert.NotNil(t, tt.config.middlewares)
				assert.Greater(t, len(tt.config.middlewares), 0, "default middlewares should be set")
			} else {
				assert.Equal(t, 5, len(tt.config.middlewares), "custom middlewares should be preserved plus auth-failure-audit + auth + role-resolution + audit middlewares")
			}
		})
	}
}

func TestBuildHTTPServer_WithMeterProvider(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		name                    string
		meterProvider           metric.MeterProvider
		initialMiddlewareCount  int
		expectedMiddlewareCount int
		description             string
	}{
		{
			name:                    "with meter provider adds metrics middleware",
			meterProvider:           noop.NewMeterProvider(),
			initialMiddlewareCount:  0,  // nil triggers defaults (5) + metrics (1) + auth-failure-audit (1) + auth (1) + roles (1) + audit (1) = 10
			expectedMiddlewareCount: 10, // 5 default + 1 metrics + 1 auth-failure-audit + 1 auth + 1 roles + 1 audit
			description:             "metrics middleware should be prepended when meter provider is set",
		},
		{
			name:                    "without meter provider no metrics middleware",
			meterProvider:           nil,
			initialMiddlewareCount:  0, // nil triggers defaults (5) + auth-failure-audit (1) + auth (1) + roles (1) + audit (1) = 9
			expectedMiddlewareCount: 9, // 5 default + 1 auth-failure-audit + 1 auth + 1 roles + 1 audit (no metrics)
			description:             "no metrics middleware should be added when meter provider is nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockSvc := mocksvc.NewMockRegistryService(ctrl)

			cfg := &registryAppConfig{
				address:        ":8080",
				middlewares:    nil, // nil triggers default middlewares
				requestTimeout: 10 * time.Second,
				readTimeout:    10 * time.Second,
				writeTimeout:   15 * time.Second,
				idleTimeout:    60 * time.Second,
				meterProvider:  tt.meterProvider,
				authMiddleware: func(next http.Handler) http.Handler { return next },
			}

			server, err := buildHTTPServer(ctx, cfg, mockSvc, emptyPages{}, nil)

			require.NoError(t, err, tt.description)
			require.NotNil(t, server)
			assert.Len(t, cfg.middlewares, tt.expectedMiddlewareCount, tt.description)
		})
	}
}

func TestBuildHTTPServer_MetricsMiddlewareWithCustomMiddlewares(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		name                    string
		meterProvider           metric.MeterProvider
		customMiddlewareCount   int
		expectedMiddlewareCount int
		description             string
	}{
		{
			name:                    "meter provider with custom middlewares prepends metrics",
			meterProvider:           noop.NewMeterProvider(),
			customMiddlewareCount:   2,
			expectedMiddlewareCount: 7, // 1 metrics + 2 custom + 1 auth-failure-audit + 1 auth + 1 roles + 1 audit
			description:             "metrics middleware should be prepended to custom middlewares",
		},
		{
			name:                    "no meter provider with custom middlewares",
			meterProvider:           nil,
			customMiddlewareCount:   2,
			expectedMiddlewareCount: 6, // 2 custom + 1 auth-failure-audit + 1 auth + 1 roles + 1 audit (no metrics)
			description:             "no metrics middleware with custom middlewares when provider is nil",
		},
		{
			name:                    "meter provider with single custom middleware",
			meterProvider:           noop.NewMeterProvider(),
			customMiddlewareCount:   1,
			expectedMiddlewareCount: 6, // 1 metrics + 1 custom + 1 auth-failure-audit + 1 auth + 1 roles + 1 audit
			description:             "metrics middleware prepended to single custom middleware",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockSvc := mocksvc.NewMockRegistryService(ctrl)

			// Create custom middlewares
			customMiddlewares := make([]func(http.Handler) http.Handler, tt.customMiddlewareCount)
			for i := range customMiddlewares {
				customMiddlewares[i] = func(next http.Handler) http.Handler { return next }
			}

			cfg := &registryAppConfig{
				address:        ":8080",
				middlewares:    customMiddlewares,
				requestTimeout: 10 * time.Second,
				readTimeout:    10 * time.Second,
				writeTimeout:   15 * time.Second,
				idleTimeout:    60 * time.Second,
				meterProvider:  tt.meterProvider,
				authMiddleware: func(next http.Handler) http.Handler { return next },
			}

			server, err := buildHTTPServer(ctx, cfg, mockSvc, emptyPages{}, nil)

			require.NoError(t, err, tt.description)
			require.NotNil(t, server)
			assert.Len(t, cfg.middlewares, tt.expectedMiddlewareCount, tt.description)
		})
	}
}

func TestSecurityHeadersInDefaultMiddleware(t *testing.T) {
	t.Parallel()

	// securityHeadersMiddleware is part of the default middleware chain in
	// buildHTTPServer. Verify the named function sets both headers.
	req, err := http.NewRequest(http.MethodGet, "/", nil)
	require.NoError(t, err)

	rr := httptest.NewRecorder()
	securityHeadersMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rr, req)

	assert.Equal(t, "nosniff", rr.Header().Get("X-Content-Type-Options"))
	assert.Equal(t, "same-origin", rr.Header().Get("Cross-Origin-Resource-Policy"))
}
