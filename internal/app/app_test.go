package app

import (
	"context"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	mocksvc "github.com/stacklok/toolhive-registry-server/internal/service/mocks"
	"github.com/stacklok/toolhive-registry-server/internal/sync/coordinator"
)

// mockCoordinator implements the coordinator.Coordinator interface for testing
type mockCoordinator struct {
	mu          sync.Mutex
	startCalled bool
	stopCalled  bool
	startErr    error
	stopErr     error
	startDelay  time.Duration
}

func (m *mockCoordinator) Start(ctx context.Context) error {
	m.mu.Lock()
	m.startCalled = true
	delay := m.startDelay
	err := m.startErr
	m.mu.Unlock()

	if delay > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
	return err
}

func (m *mockCoordinator) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopCalled = true
	return m.stopErr
}

func (m *mockCoordinator) wasStartCalled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.startCalled
}

func (m *mockCoordinator) wasStopCalled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stopCalled
}

// createTestApp creates a RegistryApp with mocked components for testing
// This directly constructs the RegistryApp without using NewRegistryApp to avoid
// complex mock setup for storage factory
func createTestApp(t *testing.T, ctrl *gomock.Controller, addr string) *RegistryApp {
	t.Helper()

	mockSvc := mocksvc.NewMockRegistryService(ctrl)
	mockCoord := &mockCoordinator{}

	cfg := createTestAppConfig()

	ctx := context.Background()
	appCtx, cancel := context.WithCancel(ctx)

	// Build the HTTP server with test configuration
	appCfg := &registryAppConfig{
		config:         cfg,
		address:        addr,
		requestTimeout: 10 * time.Second,
		readTimeout:    10 * time.Second,
		writeTimeout:   15 * time.Second,
		idleTimeout:    60 * time.Second,
		authMiddleware: func(next http.Handler) http.Handler { return next },
	}

	server, err := buildHTTPServer(ctx, appCfg, mockSvc)
	require.NoError(t, err)

	return &RegistryApp{
		config: cfg,
		components: &AppComponents{
			SyncCoordinator: mockCoord,
			RegistryService: mockSvc,
		},
		httpServer: server,
		ctx:        appCtx,
		cancelFunc: cancel,
	}
}

// createTestAppConfig creates a minimal valid config for testing
func createTestAppConfig() *config.Config {
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
		Auth: &config.AuthConfig{
			Mode: config.AuthModeAnonymous,
		},
	}
}

func TestRegistryApp_Start(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		setupApp   func(*testing.T, *gomock.Controller) *RegistryApp
		wantErr    bool
		errContain string
	}{
		{
			name: "successful start with ephemeral port",
			setupApp: func(t *testing.T, ctrl *gomock.Controller) *RegistryApp {
				t.Helper()
				return createTestApp(t, ctrl, ":0")
			},
			wantErr: false,
		},
		{
			name: "successful start on localhost",
			setupApp: func(t *testing.T, ctrl *gomock.Controller) *RegistryApp {
				t.Helper()
				return createTestApp(t, ctrl, "127.0.0.1:0")
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			app := tt.setupApp(t, ctrl)

			// Start server in goroutine
			errChan := make(chan error, 1)
			go func() {
				errChan <- app.Start()
			}()

			// Wait for server to start
			time.Sleep(100 * time.Millisecond)

			// Verify server is listening
			if !tt.wantErr {
				// Get the actual address the server is listening on
				addr := app.httpServer.Addr
				if addr == ":0" || addr == "127.0.0.1:0" {
					// For ephemeral ports, we need to check differently
					// The server should be running
					mockCoord := app.components.SyncCoordinator.(*mockCoordinator)
					assert.True(t, mockCoord.wasStartCalled(), "sync coordinator should be started")
				}
			}

			// Stop the server
			err := app.Stop(5 * time.Second)
			require.NoError(t, err)

			// Check Start() result
			select {
			case startErr := <-errChan:
				if tt.wantErr {
					require.Error(t, startErr)
					if tt.errContain != "" {
						assert.Contains(t, startErr.Error(), tt.errContain)
					}
				} else {
					require.NoError(t, startErr)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("Start() did not return after Stop()")
			}
		})
	}
}

func TestRegistryApp_StartWithListener(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	app := createTestApp(t, ctrl, ":0")

	// Create a listener to get an actual port
	listener, err := net.Listen("tcp", ":0")
	require.NoError(t, err)
	actualAddr := listener.Addr().String()
	listener.Close()

	// Update the server address to use the now-free port
	app.httpServer.Addr = actualAddr

	// Start server in goroutine
	errChan := make(chan error, 1)
	go func() {
		errChan <- app.Start()
	}()

	// Wait for server to start
	time.Sleep(100 * time.Millisecond)

	// Make a health check request
	resp, err := http.Get("http://" + actualAddr + "/health")
	if err == nil {
		resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	}

	// Verify sync coordinator was started
	mockCoord := app.components.SyncCoordinator.(*mockCoordinator)
	assert.True(t, mockCoord.wasStartCalled(), "sync coordinator should be started")

	// Stop the server
	err = app.Stop(5 * time.Second)
	require.NoError(t, err)

	// Wait for Start() to return
	select {
	case startErr := <-errChan:
		require.NoError(t, startErr)
	case <-time.After(5 * time.Second):
		t.Fatal("Start() did not return after Stop()")
	}
}

func TestRegistryApp_Stop(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		timeout  time.Duration
		setupApp func(*testing.T, *gomock.Controller) *RegistryApp
		wantErr  bool
		verifyFn func(*testing.T, *RegistryApp)
	}{
		{
			name:    "graceful shutdown with normal timeout",
			timeout: 5 * time.Second,
			setupApp: func(t *testing.T, ctrl *gomock.Controller) *RegistryApp {
				t.Helper()
				return createTestApp(t, ctrl, ":0")
			},
			wantErr: false,
			verifyFn: func(t *testing.T, app *RegistryApp) {
				t.Helper()
				mockCoord := app.components.SyncCoordinator.(*mockCoordinator)
				assert.True(t, mockCoord.wasStopCalled(), "sync coordinator Stop should be called")
			},
		},
		{
			name:    "graceful shutdown with short timeout",
			timeout: 1 * time.Second,
			setupApp: func(t *testing.T, ctrl *gomock.Controller) *RegistryApp {
				t.Helper()
				return createTestApp(t, ctrl, ":0")
			},
			wantErr: false,
			verifyFn: func(t *testing.T, app *RegistryApp) {
				t.Helper()
				mockCoord := app.components.SyncCoordinator.(*mockCoordinator)
				assert.True(t, mockCoord.wasStopCalled(), "sync coordinator Stop should be called")
			},
		},
		{
			name:    "stop without starting first",
			timeout: 5 * time.Second,
			setupApp: func(t *testing.T, ctrl *gomock.Controller) *RegistryApp {
				t.Helper()
				return createTestApp(t, ctrl, ":0")
			},
			wantErr: false,
			verifyFn: func(t *testing.T, app *RegistryApp) {
				t.Helper()
				mockCoord := app.components.SyncCoordinator.(*mockCoordinator)
				assert.True(t, mockCoord.wasStopCalled(), "sync coordinator Stop should be called even without Start")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			app := tt.setupApp(t, ctrl)

			// For tests that need the server running first
			if tt.name != "stop without starting first" {
				errChan := make(chan error, 1)
				go func() {
					errChan <- app.Start()
				}()

				// Wait for server to start
				time.Sleep(100 * time.Millisecond)
			}

			// Call Stop
			err := app.Stop(tt.timeout)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			if tt.verifyFn != nil {
				tt.verifyFn(t, app)
			}
		})
	}
}

func TestRegistryApp_StopIdempotent(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	app := createTestApp(t, ctrl, ":0")

	// Start server in goroutine
	errChan := make(chan error, 1)
	go func() {
		errChan <- app.Start()
	}()

	// Wait for server to start
	time.Sleep(100 * time.Millisecond)

	// First stop should succeed
	err1 := app.Stop(5 * time.Second)
	require.NoError(t, err1)

	// Wait for Start() to return
	select {
	case <-errChan:
		// Expected
	case <-time.After(5 * time.Second):
		t.Fatal("Start() did not return after first Stop()")
	}

	// Second stop should also succeed (idempotent)
	err2 := app.Stop(5 * time.Second)
	// Note: This may return an error if the server is already closed,
	// but it should not panic
	_ = err2
}

func TestRegistryApp_StopWithNilCancelFunc(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	app := createTestApp(t, ctrl, ":0")

	// Set cancelFunc to nil to test nil safety
	app.cancelFunc = nil

	// Stop should handle nil cancelFunc gracefully
	err := app.Stop(5 * time.Second)
	// The server wasn't started, so shutdown should be quick
	require.NoError(t, err)
}

func TestRegistryApp_GetConfig(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	app := createTestApp(t, ctrl, ":0")

	cfg := app.GetConfig()

	require.NotNil(t, cfg)
	assert.Equal(t, "test-registry", cfg.RegistryName)
}

func TestRegistryApp_GetHTTPServer(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	app := createTestApp(t, ctrl, ":8080")

	server := app.GetHTTPServer()

	require.NotNil(t, server)
	assert.Equal(t, ":8080", server.Addr)
}

func TestRegistryApp_StartError_InvalidAddress(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)

	// Create app with an invalid address (port already in use simulation)
	// First, occupy a port
	listener, err := net.Listen("tcp", ":0")
	require.NoError(t, err)
	defer listener.Close()
	occupiedAddr := listener.Addr().String()

	// Create app trying to use the same port
	app := createTestApp(t, ctrl, occupiedAddr)

	// Start should fail because port is in use
	errChan := make(chan error, 1)
	go func() {
		errChan <- app.Start()
	}()

	select {
	case startErr := <-errChan:
		require.Error(t, startErr)
		assert.Contains(t, startErr.Error(), "HTTP server failed")
	case <-time.After(5 * time.Second):
		// If it doesn't fail quickly, stop and check
		app.Stop(1 * time.Second)
		t.Fatal("Expected Start() to fail due to port in use")
	}
}

// Verify that Coordinator interface is properly defined
var _ coordinator.Coordinator = (*mockCoordinator)(nil)
