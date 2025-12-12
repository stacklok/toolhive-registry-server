package app

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/internal/app/storage/mocks"
	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/kubernetes"
	mocksvc "github.com/stacklok/toolhive-registry-server/internal/service/mocks"
	"github.com/stacklok/toolhive-registry-server/internal/sources"
	pkgsync "github.com/stacklok/toolhive-registry-server/internal/sync"
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
		// Auth is required by default; use anonymous mode for tests
		Auth: &config.AuthConfig{
			Mode: config.AuthModeAnonymous,
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

func TestWithRegistryName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		registryName string
		want         string
		wantErr      bool
	}{
		{
			name:         "valid registry name",
			registryName: "my-registry",
			want:         "my-registry",
			wantErr:      false,
		},
		{
			name:         "valid registry name with underscores",
			registryName: "my_registry_v1",
			want:         "my_registry_v1",
			wantErr:      false,
		},
		{
			name:         "valid registry name with numbers",
			registryName: "registry-123",
			want:         "registry-123",
			wantErr:      false,
		},
		{
			name:         "valid long registry name",
			registryName: "my-very-long-registry-name-with-many-segments",
			want:         "my-very-long-registry-name-with-many-segments",
			wantErr:      false,
		},
		{
			name:         "invalid empty registry name",
			registryName: "",
			want:         "",
			wantErr:      true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opt := kubernetes.WithRegistryName(tt.registryName)

			// Get the Option function type to extract the parameter type
			optionFuncType := reflect.TypeOf(opt)
			// Option is func(*mcpServerReconcilerOptions) error
			// Get the element type of the first parameter
			paramType := optionFuncType.In(0).Elem()

			// Create a new instance of the struct
			optionsValue := reflect.New(paramType)

			// Call the option function
			results := reflect.ValueOf(opt).Call([]reflect.Value{optionsValue})
			errVal := results[0]
			var err error
			if !errVal.IsNil() {
				err = errVal.Interface().(error)
			}

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)

			// Use reflection to get the registryName field
			registryNameField := optionsValue.Elem().FieldByName("registryName")
			require.True(t, registryNameField.IsValid(), "registryName field should exist")
			got := registryNameField.String()
			assert.Equal(t, tt.want, got)
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

func TestWithRegistryHandlerFactory(t *testing.T) {
	t.Parallel()
	cfg := &registryAppConfig{}
	// Use nil factory for testing - we're just verifying the field is set
	var testFactory sources.RegistryHandlerFactory

	opt := WithRegistryHandlerFactory(testFactory)
	err := opt(cfg)

	require.NoError(t, err)
	assert.Equal(t, testFactory, cfg.registryHandlerFactory)
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
			server, err := buildHTTPServer(ctx, tt.config, mockSvc)

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
				assert.Equal(t, 2, len(tt.config.middlewares), "custom middlewares should be preserved plus auth middleware")
			}
		})
	}
}

func TestBuildServiceComponents(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("success with storage factory", func(t *testing.T) {
		t.Parallel()
		ctrl := gomock.NewController(t)
		mockFactory := mocks.NewMockFactory(ctrl)
		mockSvc := mocksvc.NewMockRegistryService(ctrl)

		mockFactory.EXPECT().
			CreateRegistryService(gomock.Any()).
			Return(mockSvc, nil)

		cfg := &registryAppConfig{
			config:         createValidTestConfig(),
			storageFactory: mockFactory,
		}

		svc, err := buildServiceComponents(ctx, cfg)

		require.NoError(t, err)
		require.NotNil(t, svc)
		assert.Equal(t, mockSvc, svc)
	})

	t.Run("error when config is nil", func(t *testing.T) {
		t.Parallel()
		ctrl := gomock.NewController(t)
		mockFactory := mocks.NewMockFactory(ctrl)

		// No expectations - config check happens before factory call

		cfg := &registryAppConfig{
			config:         nil,
			storageFactory: mockFactory,
		}

		svc, err := buildServiceComponents(ctx, cfg)

		require.Error(t, err)
		assert.Nil(t, svc)
	})

	t.Run("error when factory fails", func(t *testing.T) {
		t.Parallel()
		ctrl := gomock.NewController(t)
		mockFactory := mocks.NewMockFactory(ctrl)

		mockFactory.EXPECT().
			CreateRegistryService(gomock.Any()).
			Return(nil, fmt.Errorf("factory creation failed"))

		cfg := &registryAppConfig{
			config:         createValidTestConfig(),
			storageFactory: mockFactory,
		}

		svc, err := buildServiceComponents(ctx, cfg)

		require.Error(t, err)
		assert.Nil(t, svc)
	})
}

func TestBuildSyncComponents(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("success with mock factory", func(t *testing.T) {
		t.Parallel()
		ctrl := gomock.NewController(t)
		mockFactory := mocks.NewMockFactory(ctrl)

		mockFactory.EXPECT().
			CreateStateService(gomock.Any()).
			Return(nil, nil)

		mockFactory.EXPECT().
			CreateSyncWriter(gomock.Any()).
			Return(nil, nil)

		cfg := &registryAppConfig{
			config:         createValidTestConfig(),
			storageFactory: mockFactory,
		}

		coord, err := buildSyncComponents(ctx, cfg)

		require.NoError(t, err)
		require.NotNil(t, coord)
	})

	t.Run("error when state service creation fails", func(t *testing.T) {
		t.Parallel()
		ctrl := gomock.NewController(t)
		mockFactory := mocks.NewMockFactory(ctrl)

		mockFactory.EXPECT().
			CreateStateService(gomock.Any()).
			Return(nil, fmt.Errorf("state service creation failed"))

		cfg := &registryAppConfig{
			config:         createValidTestConfig(),
			storageFactory: mockFactory,
		}

		coord, err := buildSyncComponents(ctx, cfg)

		require.Error(t, err)
		assert.Nil(t, coord)
	})

	t.Run("error when sync writer creation fails", func(t *testing.T) {
		t.Parallel()
		ctrl := gomock.NewController(t)
		mockFactory := mocks.NewMockFactory(ctrl)

		mockFactory.EXPECT().
			CreateStateService(gomock.Any()).
			Return(nil, nil)

		mockFactory.EXPECT().
			CreateSyncWriter(gomock.Any()).
			Return(nil, fmt.Errorf("sync writer creation failed"))

		cfg := &registryAppConfig{
			config:         createValidTestConfig(),
			storageFactory: mockFactory,
		}

		coord, err := buildSyncComponents(ctx, cfg)

		require.Error(t, err)
		assert.Nil(t, coord)
	})
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

func TestNewRegistryApp_ErrorPaths(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		name           string
		setupMocks     func(*gomock.Controller) *mocks.MockFactory
		opts           func(*mocks.MockFactory, string) []RegistryAppOptions
		wantErrContain string
		verifyCleanup  bool
	}{
		{
			name: "error when option returns error",
			setupMocks: func(ctrl *gomock.Controller) *mocks.MockFactory {
				return mocks.NewMockFactory(ctrl)
			},
			opts: func(_ *mocks.MockFactory, _ string) []RegistryAppOptions {
				return []RegistryAppOptions{
					WithConfig(createValidTestConfig()),
					WithAddress(":"), // Invalid address triggers error
				}
			},
			wantErrContain: "failed to build base configuration",
			verifyCleanup:  false, // Storage factory not created yet
		},
		{
			name: "error when state service creation fails",
			setupMocks: func(ctrl *gomock.Controller) *mocks.MockFactory {
				mockFactory := mocks.NewMockFactory(ctrl)
				mockFactory.EXPECT().
					CreateStateService(gomock.Any()).
					Return(nil, fmt.Errorf("state service creation failed"))
				mockFactory.EXPECT().
					Cleanup()
				return mockFactory
			},
			opts: func(mockFactory *mocks.MockFactory, tmpDir string) []RegistryAppOptions {
				return []RegistryAppOptions{
					WithConfig(createValidTestConfig()),
					WithStorageFactory(mockFactory),
					WithDataDirectory(tmpDir),
				}
			},
			wantErrContain: "failed to build sync components",
			verifyCleanup:  true,
		},
		{
			name: "error when sync writer creation fails",
			setupMocks: func(ctrl *gomock.Controller) *mocks.MockFactory {
				mockFactory := mocks.NewMockFactory(ctrl)
				mockFactory.EXPECT().
					CreateStateService(gomock.Any()).
					Return(nil, nil)
				mockFactory.EXPECT().
					CreateSyncWriter(gomock.Any()).
					Return(nil, fmt.Errorf("sync writer creation failed"))
				mockFactory.EXPECT().
					Cleanup()
				return mockFactory
			},
			opts: func(mockFactory *mocks.MockFactory, tmpDir string) []RegistryAppOptions {
				return []RegistryAppOptions{
					WithConfig(createValidTestConfig()),
					WithStorageFactory(mockFactory),
					WithDataDirectory(tmpDir),
				}
			},
			wantErrContain: "failed to build sync components",
			verifyCleanup:  true,
		},
		{
			name: "error when service component build fails",
			setupMocks: func(ctrl *gomock.Controller) *mocks.MockFactory {
				mockFactory := mocks.NewMockFactory(ctrl)
				mockFactory.EXPECT().
					CreateStateService(gomock.Any()).
					Return(nil, nil)
				mockFactory.EXPECT().
					CreateSyncWriter(gomock.Any()).
					Return(nil, nil)
				mockFactory.EXPECT().
					CreateRegistryService(gomock.Any()).
					Return(nil, fmt.Errorf("registry service creation failed"))
				mockFactory.EXPECT().
					Cleanup()
				return mockFactory
			},
			opts: func(mockFactory *mocks.MockFactory, tmpDir string) []RegistryAppOptions {
				return []RegistryAppOptions{
					WithConfig(createValidTestConfig()),
					WithStorageFactory(mockFactory),
					WithDataDirectory(tmpDir),
				}
			},
			wantErrContain: "failed to build service components",
			verifyCleanup:  true,
		},
		{
			name: "error when auth middleware creation fails",
			setupMocks: func(ctrl *gomock.Controller) *mocks.MockFactory {
				mockFactory := mocks.NewMockFactory(ctrl)
				mockSvc := mocksvc.NewMockRegistryService(ctrl)
				mockFactory.EXPECT().
					CreateStateService(gomock.Any()).
					Return(nil, nil)
				mockFactory.EXPECT().
					CreateSyncWriter(gomock.Any()).
					Return(nil, nil)
				mockFactory.EXPECT().
					CreateRegistryService(gomock.Any()).
					Return(mockSvc, nil)
				mockFactory.EXPECT().
					Cleanup()
				return mockFactory
			},
			opts: func(mockFactory *mocks.MockFactory, tmpDir string) []RegistryAppOptions {
				// Config with invalid auth mode to trigger auth middleware error
				invalidAuthConfig := createValidTestConfig()
				invalidAuthConfig.Auth = nil // nil auth config causes error
				return []RegistryAppOptions{
					WithConfig(invalidAuthConfig),
					WithStorageFactory(mockFactory),
					WithDataDirectory(tmpDir),
				}
			},
			wantErrContain: "failed to build auth middleware",
			verifyCleanup:  true,
		},
		{
			name: "error when service creation returns nil config error",
			setupMocks: func(ctrl *gomock.Controller) *mocks.MockFactory {
				mockFactory := mocks.NewMockFactory(ctrl)
				mockSvc := mocksvc.NewMockRegistryService(ctrl)
				mockFactory.EXPECT().
					CreateStateService(gomock.Any()).
					Return(nil, nil)
				mockFactory.EXPECT().
					CreateSyncWriter(gomock.Any()).
					Return(nil, nil)
				mockFactory.EXPECT().
					CreateRegistryService(gomock.Any()).
					Return(mockSvc, nil)
				mockFactory.EXPECT().
					Cleanup()
				return mockFactory
			},
			opts: func(mockFactory *mocks.MockFactory, tmpDir string) []RegistryAppOptions {
				// Config with unsupported auth mode to trigger error
				invalidConfig := createValidTestConfig()
				invalidConfig.Auth = &config.AuthConfig{
					Mode: "unsupported-mode", // Invalid auth mode
				}
				return []RegistryAppOptions{
					WithConfig(invalidConfig),
					WithStorageFactory(mockFactory),
					WithDataDirectory(tmpDir),
				}
			},
			wantErrContain: "failed to build auth middleware",
			verifyCleanup:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			tmpDir := t.TempDir()

			mockFactory := tt.setupMocks(ctrl)
			opts := tt.opts(mockFactory, tmpDir)

			app, err := NewRegistryApp(ctx, opts...)

			require.Error(t, err)
			assert.Nil(t, app)
			assert.Contains(t, err.Error(), tt.wantErrContain)
			// Cleanup verification is done through gomock expectations
		})
	}
}
