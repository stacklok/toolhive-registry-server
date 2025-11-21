package app

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/service"
	"github.com/stacklok/toolhive-registry-server/internal/service/mocks"
	"github.com/stacklok/toolhive-registry-server/internal/sources"
	"github.com/stacklok/toolhive-registry-server/internal/status"
	pkgsync "github.com/stacklok/toolhive-registry-server/internal/sync"
	"github.com/stacklok/toolhive-registry-server/internal/sync/coordinator"
)

func TestNewRegistryAppBuilder(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		RegistryName: "test-registry",
		Registries: []config.RegistryConfig{
			{
				Name:   "test-registry-1",
				Format: config.SourceFormatToolHive,
				File: &config.FileConfig{
					Path: "/tmp/test-registry.json",
				},
				SyncPolicy: &config.SyncPolicyConfig{
					Interval: "30m",
				},
			},
		},
	}

	built, err := baseConfig(WithConfig(cfg))
	require.NoError(t, err)
	require.NotNil(t, built)
	assert.Equal(t, defaultHTTPAddress, built.address)
	assert.Equal(t, defaultDataDir, built.dataDir)
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

func TestRegistryAppBuilder_ChainedBuilder(t *testing.T) {
	t.Parallel()
	cfg := createValidTestConfig()

	built, err := baseConfig(
		WithConfig(cfg),
		WithAddress(":8888"),
		WithDataDirectory("/tmp/test-data"),
	)
	require.NoError(t, err)
	require.NotNil(t, built)
	assert.Equal(t, ":8888", built.address)
	assert.Equal(t, "/tmp/test-data", built.dataDir)
	assert.Equal(t, "/tmp/test-data/registry.json", built.registryFile)
	assert.Equal(t, "/tmp/test-data/status.json", built.statusFile)
}

// createValidTestConfig creates a minimal valid config for testing
func createValidTestConfig() *config.Config {
	return &config.Config{
		RegistryName: "test-registry",
		Registries: []config.RegistryConfig{
			{
				Name:   "test-registry-1",
				Format: config.SourceFormatToolHive,
				File: &config.FileConfig{
					Path: "/tmp/test-registry.json",
				},
				SyncPolicy: &config.SyncPolicyConfig{
					Interval: "30m",
				},
			},
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

func TestWithSourceHandlerFactory(t *testing.T) {
	t.Parallel()
	cfg := &registryAppConfig{}
	// Use nil factory for testing - we're just verifying the field is set
	var testFactory sources.SourceHandlerFactory

	opt := WithSourceHandlerFactory(testFactory)
	err := opt(cfg)

	require.NoError(t, err)
	assert.Equal(t, testFactory, cfg.sourceHandlerFactory)
}

func TestWithStorageManager(t *testing.T) {
	t.Parallel()
	cfg := &registryAppConfig{}
	// Use nil storage manager for testing - we're just verifying the field is set
	var testStorageManager sources.StorageManager

	opt := WithStorageManager(testStorageManager)
	err := opt(cfg)

	require.NoError(t, err)
	assert.Equal(t, testStorageManager, cfg.storageManager)
}

func TestWithStatusPersistence(t *testing.T) {
	t.Parallel()
	cfg := &registryAppConfig{}
	// Use nil status persistence for testing - we're just verifying the field is set
	var testStatusPersistence status.StatusPersistence

	opt := WithStatusPersistence(testStatusPersistence)
	err := opt(cfg)

	require.NoError(t, err)
	assert.Equal(t, testStatusPersistence, cfg.statusPersistence)
}

func TestWithSyncManager(t *testing.T) {
	t.Parallel()
	cfg := &registryAppConfig{}
	// Use nil sync manager for testing - we're just verifying the field is set
	var testSyncManager pkgsync.Manager

	opt := WithSyncManager(testSyncManager)
	err := opt(cfg)

	require.NoError(t, err)
	assert.Equal(t, testSyncManager, cfg.syncManager)
}

func TestWithRegistryProvider(t *testing.T) {
	t.Parallel()
	cfg := &registryAppConfig{}
	// Use nil registry provider for testing - we're just verifying the field is set
	var testRegistryProvider service.RegistryDataProvider

	opt := WithRegistryProvider(testRegistryProvider)
	err := opt(cfg)

	require.NoError(t, err)
	assert.Equal(t, testRegistryProvider, cfg.registryProvider)
}

func TestBuildHTTPServer(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		name           string
		config         *registryAppConfig
		setupMock      func(*mocks.MockRegistryService)
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
			setupMock:      func(_ *mocks.MockRegistryService) {},
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
			setupMock:      func(_ *mocks.MockRegistryService) {},
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
			setupMock:      func(_ *mocks.MockRegistryService) {},
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

			mockSvc := mocks.NewMockRegistryService(ctrl)
			tt.setupMock(mockSvc)

			server, err := buildHTTPServer(ctx, tt.config, mockSvc)

			require.NoError(t, err)
			require.NotNil(t, server)
			assert.Equal(t, tt.wantAddr, server.Addr)
			assert.Equal(t, tt.wantReadTO, server.ReadTimeout)
			assert.Equal(t, tt.wantWriteTO, server.WriteTimeout)
			assert.Equal(t, tt.wantIdleTO, server.IdleTimeout)
			assert.NotNil(t, server.Handler)

			// Verify middlewares were set
			if tt.expectDefaults {
				assert.NotNil(t, tt.config.middlewares)
				assert.Greater(t, len(tt.config.middlewares), 0, "default middlewares should be set")
			} else {
				assert.Equal(t, 1, len(tt.config.middlewares), "custom middlewares should be preserved")
			}
		})
	}
}

func TestBuildServiceComponents(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		name       string
		config     *registryAppConfig
		setupMocks func(
			*testing.T,
			*gomock.Controller,
		) *mocks.MockRegistryDataProvider
		wantErr bool
		verify  func(
			*testing.T,
			service.RegistryService,
			*registryAppConfig,
			service.RegistryDataProvider,
		)
	}{
		{
			name: "success with nil registryProvider - creates provider and service",
			config: &registryAppConfig{
				config:         createValidTestConfig(),
				storageManager: sources.NewFileStorageManager(t.TempDir()),
			},
			setupMocks: func(
				t *testing.T,
				_ *gomock.Controller,
			) *mocks.MockRegistryDataProvider {
				t.Helper()
				return nil
			},
			wantErr: false,
			//nolint:thelper // we want to see these lines
			verify: func(
				t *testing.T,
				_ service.RegistryService,
				config *registryAppConfig,
				originalProvider service.RegistryDataProvider,
			) {
				assert.NotNil(
					t,
					config.registryProvider,
					"registryProvider should be set when created",
				)
				assert.NotEqual(
					t,
					originalProvider,
					config.registryProvider,
					"provider should be newly created",
				)
			},
		},
		{
			name: "success with pre-set registryProvider - skips creation",
			config: &registryAppConfig{
				config: createValidTestConfig(),
			},
			setupMocks: func(
				t *testing.T,
				ctrl *gomock.Controller,
			) *mocks.MockRegistryDataProvider {
				t.Helper()
				mockProvider := mocks.NewMockRegistryDataProvider(ctrl)
				// service.NewService calls GetRegistryData during initialization
				mockProvider.EXPECT().GetRegistryData(gomock.Any()).
					Return(nil, fmt.Errorf("registry file not found")).
					AnyTimes()
				return mockProvider
			},
			wantErr: false,
			//nolint:thelper // we want to see these lines
			verify: func(
				t *testing.T,
				_ service.RegistryService,
				config *registryAppConfig,
				originalProvider service.RegistryDataProvider,
			) {
				assert.Equal(
					t,
					originalProvider,
					config.registryProvider,
					"provider should remain unchanged when pre-set",
				)
			},
		},
		{
			name: "success with pre-set registryProvider and deploymentProvider",
			config: &registryAppConfig{
				config: createValidTestConfig(),
			},
			setupMocks: func(
				t *testing.T,
				ctrl *gomock.Controller,
			) *mocks.MockRegistryDataProvider {
				t.Helper()
				mockProvider := mocks.NewMockRegistryDataProvider(ctrl)
				// service.NewService calls GetRegistryData during initialization
				mockProvider.EXPECT().GetRegistryData(gomock.Any()).
					Return(nil, fmt.Errorf("registry file not found")).
					AnyTimes()
				return mockProvider
			},
			wantErr: false,
			//nolint:thelper // we want to see these lines
			verify: func(
				t *testing.T,
				_ service.RegistryService,
				config *registryAppConfig,
				originalProvider service.RegistryDataProvider,
			) {
				assert.Equal(
					t,
					originalProvider,
					config.registryProvider,
					"provider should remain unchanged when pre-set",
				)
			},
		},
		{
			name: "error when config is nil - factory.CreateProvider fails",
			config: &registryAppConfig{
				config:         nil,
				storageManager: sources.NewFileStorageManager(t.TempDir()),
			},
			setupMocks: func(
				t *testing.T,
				_ *gomock.Controller,
			) *mocks.MockRegistryDataProvider {
				t.Helper()
				return nil
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockRegProvider := tt.setupMocks(t, ctrl)
			if mockRegProvider != nil {
				tt.config.registryProvider = mockRegProvider
			}

			// Store original provider to check if it was set
			originalProvider := tt.config.registryProvider

			svc, err := buildServiceComponents(ctx, tt.config)

			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, svc)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, svc)

			if tt.verify != nil {
				tt.verify(t, svc, tt.config, originalProvider)
			}
		})
	}
}

func TestBuildSyncComponents(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		config  *registryAppConfig
		wantErr bool
		verify  func(*testing.T, coordinator.Coordinator, *registryAppConfig)
	}{
		{
			name: "success with all nil components - creates defaults",
			config: &registryAppConfig{
				config:               createValidTestConfig(),
				dataDir:              t.TempDir(),
				statusFile:           t.TempDir() + "/status.json",
				sourceHandlerFactory: nil,
				storageManager:       nil,
				statusPersistence:    nil,
				syncManager:          nil,
			},
			wantErr: false,
			//nolint:thelper // we want to see these lines
			verify: func(t *testing.T, coord coordinator.Coordinator, cfg *registryAppConfig) {
				assert.NotNil(t, coord, "coordinator should be created")
				assert.NotNil(t, cfg.sourceHandlerFactory, "sourceHandlerFactory should be created")
				assert.NotNil(t, cfg.storageManager, "storageManager should be created")
				assert.NotNil(t, cfg.statusPersistence, "statusPersistence should be created")
				assert.NotNil(t, cfg.syncManager, "syncManager should be created")
			},
		},
		{
			name: "success with all pre-set components - uses provided ones",
			config: func() *registryAppConfig {
				tempDir := t.TempDir()
				return &registryAppConfig{
					config:               createValidTestConfig(),
					dataDir:              tempDir,
					statusFile:           tempDir + "/status.json",
					sourceHandlerFactory: sources.NewSourceHandlerFactory(),
					storageManager:       sources.NewFileStorageManager(tempDir),
					statusPersistence:    status.NewFileStatusPersistence(tempDir + "/status.json"),
					syncManager: pkgsync.NewDefaultSyncManager(
						sources.NewSourceHandlerFactory(),
						sources.NewFileStorageManager(tempDir),
					),
				}
			}(),
			wantErr: false,
			//nolint:thelper // we want to see these lines
			verify: func(t *testing.T, coord coordinator.Coordinator, cfg *registryAppConfig) {
				assert.NotNil(t, coord, "coordinator should be created")
				// Verify that the original components are still set (not replaced)
				assert.NotNil(t, cfg.sourceHandlerFactory, "sourceHandlerFactory should remain set")
				assert.NotNil(t, cfg.storageManager, "storageManager should remain set")
				assert.NotNil(t, cfg.statusPersistence, "statusPersistence should remain set")
				assert.NotNil(t, cfg.syncManager, "syncManager should remain set")
			},
		},
		{
			name: "success with mixed nil and pre-set components",
			config: func() *registryAppConfig {
				tempDir := t.TempDir()
				return &registryAppConfig{
					config:               createValidTestConfig(),
					dataDir:              tempDir,
					statusFile:           tempDir + "/status.json",
					sourceHandlerFactory: sources.NewSourceHandlerFactory(), // pre-set
					storageManager:       nil,                               // will be created
					statusPersistence:    nil,                               // will be created
					syncManager:          nil,                               // will be created
				}
			}(),
			wantErr: false,
			//nolint:thelper // we want to see these lines
			verify: func(t *testing.T, coord coordinator.Coordinator, cfg *registryAppConfig) {
				assert.NotNil(t, coord, "coordinator should be created")
				assert.NotNil(t, cfg.sourceHandlerFactory, "pre-set sourceHandlerFactory should remain")
				assert.NotNil(t, cfg.storageManager, "storageManager should be created")
				assert.NotNil(t, cfg.statusPersistence, "statusPersistence should be created")
				assert.NotNil(t, cfg.syncManager, "syncManager should be created")
			},
		},
		{
			name: "error when data directory creation fails",
			config: &registryAppConfig{
				config:               createValidTestConfig(),
				dataDir:              "/dev/null/invalid/path", // Invalid path that should fail
				statusFile:           "/dev/null/invalid/path/status.json",
				sourceHandlerFactory: nil,
				storageManager:       nil, // This will trigger directory creation
				statusPersistence:    nil,
				syncManager:          nil,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			coord, err := buildSyncComponents(tt.config)

			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, coord)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, coord)

			if tt.verify != nil {
				tt.verify(t, coord, tt.config)
			}
		})
	}
}

func TestNewRegistryApp(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		name   string
		opts   []RegistryAppOptions
		verify func(*testing.T, *RegistryApp)
	}{
		{
			name: "success with minimal config",
			opts: []RegistryAppOptions{
				WithConfig(createValidTestConfig()),
				WithDataDirectory(t.TempDir()),
			},
			//nolint:thelper // we want to see these lines
			verify: func(t *testing.T, app *RegistryApp) {
				assert.NotNil(t, app)
				assert.NotNil(t, app.config)
				assert.Equal(t, "test-registry", app.config.RegistryName)
				assert.NotNil(t, app.components)
				assert.NotNil(t, app.components.SyncCoordinator)
				assert.NotNil(t, app.components.RegistryService)
				assert.NotNil(t, app.httpServer)
				assert.NotNil(t, app.ctx)
				assert.NotNil(t, app.cancelFunc)
				assert.Equal(t, defaultHTTPAddress, app.httpServer.Addr)
			},
		},
		{
			name: "success with custom address",
			opts: []RegistryAppOptions{
				WithConfig(createValidTestConfig()),
				WithAddress(":9090"),
				WithDataDirectory(t.TempDir()),
			},
			//nolint:thelper // we want to see these lines
			verify: func(t *testing.T, app *RegistryApp) {
				assert.NotNil(t, app)
				assert.NotNil(t, app.httpServer)
				assert.Equal(t, ":9090", app.httpServer.Addr)
				assert.NotNil(t, app.components.SyncCoordinator)
				assert.NotNil(t, app.components.RegistryService)
			},
		},
		{
			name: "success with custom data directory",
			opts: []RegistryAppOptions{
				WithConfig(createValidTestConfig()),
				WithDataDirectory(t.TempDir()),
			},
			//nolint:thelper // we want to see these lines
			verify: func(t *testing.T, app *RegistryApp) {
				assert.NotNil(t, app)
				assert.NotNil(t, app.components)
				assert.NotNil(t, app.components.SyncCoordinator)
				assert.NotNil(t, app.components.RegistryService)
				assert.NotNil(t, app.httpServer)
			},
		},
		{
			name: "success with multiple options",
			opts: []RegistryAppOptions{
				WithConfig(createValidTestConfig()),
				WithAddress(":8888"),
				WithDataDirectory(t.TempDir()),
			},
			//nolint:thelper // we want to see these lines
			verify: func(t *testing.T, app *RegistryApp) {
				assert.NotNil(t, app)
				assert.NotNil(t, app.config)
				assert.NotNil(t, app.components)
				assert.NotNil(t, app.components.SyncCoordinator)
				assert.NotNil(t, app.components.RegistryService)
				assert.NotNil(t, app.httpServer)
				assert.Equal(t, ":8888", app.httpServer.Addr)
				assert.NotNil(t, app.ctx)
				assert.NotNil(t, app.cancelFunc)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			app, err := NewRegistryApp(ctx, tt.opts...)

			require.NoError(t, err)
			require.NotNil(t, app)

			if tt.verify != nil {
				tt.verify(t, app)
			}
		})
	}
}
