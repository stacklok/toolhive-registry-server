package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	toolhivetypes "github.com/stacklok/toolhive/pkg/registry/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/internal/registry"
	"github.com/stacklok/toolhive-registry-server/internal/service"
	"github.com/stacklok/toolhive-registry-server/internal/service/mocks"
)

func TestService_GetRegistry(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		setupMocks     func(*mocks.MockRegistryDataProvider)
		expectedError  string
		expectedSource string
		validateResult func(*testing.T, *toolhivetypes.UpstreamRegistry)
	}{
		{
			name: "successful registry fetch",
			setupMocks: func(m *mocks.MockRegistryDataProvider) {
				testRegistry := registry.NewTestUpstreamRegistry(
					registry.WithServers(
						registry.NewTestServer("test-server",
							registry.WithDescription("A test server"),
							registry.WithOCIPackage("test:latest"),
						),
					),
				)
				m.EXPECT().GetRegistryData(gomock.Any()).Return(testRegistry, nil)
				m.EXPECT().GetSource().Return("file:/path/to/registry.json").AnyTimes()
			},
			expectedSource: "file:/path/to/registry.json",
			validateResult: func(t *testing.T, r *toolhivetypes.UpstreamRegistry) {
				t.Helper()
				assert.Equal(t, "1.0.0", r.Version)
				assert.Len(t, r.Servers, 1)
				assert.Equal(t, "test-server", r.Servers[0].Name)
			},
		},
		{
			name: "provider returns error",
			setupMocks: func(m *mocks.MockRegistryDataProvider) {
				m.EXPECT().GetRegistryData(gomock.Any()).Return(nil, errors.New("file not found")).Times(2) // Once during NewService, once during GetRegistry
				m.EXPECT().GetSource().Return("file:/path/to/registry.json").AnyTimes()
			},
			expectedSource: "file:/path/to/registry.json",
			validateResult: func(t *testing.T, r *toolhivetypes.UpstreamRegistry) {
				t.Helper()
				// Should return empty registry on error
				assert.NotNil(t, r)
				assert.Equal(t, "1.0.0", r.Version)
				assert.Empty(t, r.Servers)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockProvider := mocks.NewMockRegistryDataProvider(ctrl)
			tt.setupMocks(mockProvider)

			svc, err := service.NewService(context.Background(), mockProvider, nil)
			require.NoError(t, err)

			upstreamRegistry, source, err := svc.GetRegistry(context.Background())
			if tt.expectedError != "" {
				assert.EqualError(t, err, tt.expectedError)
			} else {
				assert.NoError(t, err)
			}

			assert.Equal(t, tt.expectedSource, source)
			if tt.validateResult != nil {
				tt.validateResult(t, upstreamRegistry)
			}
		})
	}
}

func TestService_CheckReadiness(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		setupMocks    func(*mocks.MockRegistryDataProvider)
		expectedError string
	}{
		{
			name: "ready with successful data load",
			setupMocks: func(m *mocks.MockRegistryDataProvider) {
				testRegistry := registry.NewTestUpstreamRegistry()
				m.EXPECT().GetRegistryData(gomock.Any()).Return(testRegistry, nil).Times(1) // Only during NewService, CheckReadiness uses cached data
			},
			expectedError: "",
		},
		{
			name: "not ready when provider fails",
			setupMocks: func(m *mocks.MockRegistryDataProvider) {
				m.EXPECT().GetRegistryData(gomock.Any()).Return(nil, errors.New("connection failed")).Times(2)
			},
			expectedError: "registry data not available: failed to get registry data: connection failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockProvider := mocks.NewMockRegistryDataProvider(ctrl)
			tt.setupMocks(mockProvider)

			svc, err := service.NewService(context.Background(), mockProvider, nil)
			require.NoError(t, err)

			err = svc.CheckReadiness(context.Background())

			if tt.expectedError != "" {
				assert.EqualError(t, err, tt.expectedError)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestService_ListServers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		setupMocks      func(*mocks.MockRegistryDataProvider)
		expectedCount   int
		validateServers func(*testing.T, []upstreamv0.ServerJSON)
	}{
		{
			name: "list servers from registry",
			setupMocks: func(m *mocks.MockRegistryDataProvider) {
				testRegistry := registry.NewTestUpstreamRegistry(
					registry.WithServers(
						registry.NewTestServer("server1",
							registry.WithDescription("Server 1"),
							registry.WithOCIPackage("server1:latest"),
						),
						registry.NewTestServer("server2",
							registry.WithDescription("Server 2"),
							registry.WithOCIPackage("server2:latest"),
						),
						registry.NewTestServer("remote1",
							registry.WithDescription("Remote server 1"),
							registry.WithHTTPPackage("https://example.com/remote1"),
						),
					),
				)
				m.EXPECT().GetRegistryData(gomock.Any()).Return(testRegistry, nil).AnyTimes()
			},
			expectedCount: 3,
			validateServers: func(t *testing.T, servers []upstreamv0.ServerJSON) {
				t.Helper()
				names := make([]string, len(servers))
				for i, s := range servers {
					names[i] = s.Name
				}
				assert.Contains(t, names, "server1")
				assert.Contains(t, names, "server2")
				assert.Contains(t, names, "remote1")
			},
		},
		{
			name: "empty registry returns empty list",
			setupMocks: func(m *mocks.MockRegistryDataProvider) {
				testRegistry := registry.NewTestUpstreamRegistry()
				m.EXPECT().GetRegistryData(gomock.Any()).Return(testRegistry, nil).AnyTimes()
			},
			expectedCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockProvider := mocks.NewMockRegistryDataProvider(ctrl)
			tt.setupMocks(mockProvider)

			svc, err := service.NewService(context.Background(), mockProvider, nil)
			require.NoError(t, err)

			servers, err := svc.ListServers(context.Background())
			assert.NoError(t, err)
			assert.Len(t, servers, tt.expectedCount)

			if tt.validateServers != nil {
				tt.validateServers(t, servers)
			}
		})
	}
}

func TestService_GetServer(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		serverName     string
		setupMocks     func(*mocks.MockRegistryDataProvider)
		expectedError  string
		validateServer func(*testing.T, upstreamv0.ServerJSON)
	}{
		{
			name:       "get existing server",
			serverName: "test-server",
			setupMocks: func(m *mocks.MockRegistryDataProvider) {
				testRegistry := registry.NewTestUpstreamRegistry(
					registry.WithServers(
						registry.NewTestServer("test-server",
							registry.WithDescription("A test server"),
							registry.WithOCIPackage("test:latest"),
						),
					),
				)
				m.EXPECT().GetRegistryData(gomock.Any()).Return(testRegistry, nil).AnyTimes()
			},
			validateServer: func(t *testing.T, s upstreamv0.ServerJSON) {
				t.Helper()
				assert.Equal(t, "test-server", s.Name)
				assert.Equal(t, "A test server", s.Description)
			},
		},
		{
			name:       "server not found",
			serverName: "nonexistent",
			setupMocks: func(m *mocks.MockRegistryDataProvider) {
				testRegistry := registry.NewTestUpstreamRegistry()
				m.EXPECT().GetRegistryData(gomock.Any()).Return(testRegistry, nil).AnyTimes()
			},
			expectedError: "server not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockProvider := mocks.NewMockRegistryDataProvider(ctrl)
			tt.setupMocks(mockProvider)

			svc, err := service.NewService(context.Background(), mockProvider, nil)
			require.NoError(t, err)

			server, err := svc.GetServer(context.Background(), tt.serverName)

			if tt.expectedError != "" {
				assert.EqualError(t, err, tt.expectedError)
				assert.Empty(t, server.Name)
			} else {
				assert.NoError(t, err)
				assert.NotEmpty(t, server.Name)
				if tt.validateServer != nil {
					tt.validateServer(t, server)
				}
			}
		})
	}
}

func TestService_ListDeployedServers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		setupMocks    func(*mocks.MockRegistryDataProvider, *mocks.MockDeploymentProvider)
		useDeployment bool
		expectedCount int
		expectedError string
	}{
		{
			name: "list deployed servers",
			setupMocks: func(reg *mocks.MockRegistryDataProvider, dep *mocks.MockDeploymentProvider) {
				testRegistry := registry.NewTestUpstreamRegistry()
				reg.EXPECT().GetRegistryData(gomock.Any()).Return(testRegistry, nil)
				dep.EXPECT().ListDeployedServers(gomock.Any()).Return([]*service.DeployedServer{
					{
						Name:      "deployed1",
						Namespace: "default",
						Status:    "Running",
						Image:     "server:latest",
						Ready:     true,
					},
					{
						Name:      "deployed2",
						Namespace: "test",
						Status:    "Running",
						Image:     "server2:latest",
						Ready:     true,
					},
				}, nil)
			},
			useDeployment: true,
			expectedCount: 2,
		},
		{
			name: "no deployment provider returns empty list",
			setupMocks: func(reg *mocks.MockRegistryDataProvider, _ *mocks.MockDeploymentProvider) {
				testRegistry := registry.NewTestUpstreamRegistry()
				reg.EXPECT().GetRegistryData(gomock.Any()).Return(testRegistry, nil)
			},
			useDeployment: false,
			expectedCount: 0,
		},
		{
			name: "deployment provider error",
			setupMocks: func(reg *mocks.MockRegistryDataProvider, dep *mocks.MockDeploymentProvider) {
				testRegistry := registry.NewTestUpstreamRegistry()
				reg.EXPECT().GetRegistryData(gomock.Any()).Return(testRegistry, nil)
				dep.EXPECT().ListDeployedServers(gomock.Any()).Return(nil, errors.New("k8s api error"))
			},
			useDeployment: true,
			expectedError: "k8s api error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockRegProvider := mocks.NewMockRegistryDataProvider(ctrl)
			var mockDepProvider *mocks.MockDeploymentProvider
			if tt.useDeployment {
				mockDepProvider = mocks.NewMockDeploymentProvider(ctrl)
			}

			tt.setupMocks(mockRegProvider, mockDepProvider)

			var depProvider service.DeploymentProvider
			if tt.useDeployment {
				depProvider = mockDepProvider
			}

			svc, err := service.NewService(context.Background(), mockRegProvider, depProvider)
			require.NoError(t, err)

			servers, err := svc.ListDeployedServers(context.Background())

			if tt.expectedError != "" {
				assert.EqualError(t, err, tt.expectedError)
			} else {
				assert.NoError(t, err)
				assert.Len(t, servers, tt.expectedCount)
			}
		})
	}
}

func TestService_GetDeployedServer(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		serverName      string
		setupMocks      func(*mocks.MockRegistryDataProvider, *mocks.MockDeploymentProvider)
		useDeployment   bool
		expectedError   string
		validateServers func(*testing.T, []*service.DeployedServer)
	}{
		{
			name:       "get deployed server",
			serverName: "deployed1",
			setupMocks: func(reg *mocks.MockRegistryDataProvider, dep *mocks.MockDeploymentProvider) {
				testRegistry := registry.NewTestUpstreamRegistry()
				reg.EXPECT().GetRegistryData(gomock.Any()).Return(testRegistry, nil)
				dep.EXPECT().GetDeployedServer(gomock.Any(), "deployed1").Return([]*service.DeployedServer{
					{
						Name:      "deployed1",
						Namespace: "default",
						Status:    "Running",
						Image:     "server:latest",
						Ready:     true,
					},
				}, nil)
			},
			useDeployment: true,
			validateServers: func(t *testing.T, servers []*service.DeployedServer) {
				t.Helper()
				require.Len(t, servers, 1)
				s := servers[0]
				assert.Equal(t, "deployed1", s.Name)
				assert.Equal(t, "default", s.Namespace)
				assert.True(t, s.Ready)
			},
		},
		{
			name:       "no deployment provider",
			serverName: "any",
			setupMocks: func(reg *mocks.MockRegistryDataProvider, _ *mocks.MockDeploymentProvider) {
				testRegistry := registry.NewTestUpstreamRegistry()
				reg.EXPECT().GetRegistryData(gomock.Any()).Return(testRegistry, nil)
			},
			useDeployment: false,
			validateServers: func(t *testing.T, servers []*service.DeployedServer) {
				t.Helper()
				assert.Len(t, servers, 0)
			},
		},
		{
			name:       "server not found",
			serverName: "nonexistent",
			setupMocks: func(reg *mocks.MockRegistryDataProvider, dep *mocks.MockDeploymentProvider) {
				testRegistry := registry.NewTestUpstreamRegistry()
				reg.EXPECT().GetRegistryData(gomock.Any()).Return(testRegistry, nil)
				dep.EXPECT().GetDeployedServer(gomock.Any(), "nonexistent").Return([]*service.DeployedServer{}, nil)
			},
			useDeployment: true,
			validateServers: func(t *testing.T, servers []*service.DeployedServer) {
				t.Helper()
				assert.Len(t, servers, 0)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockRegProvider := mocks.NewMockRegistryDataProvider(ctrl)
			var mockDepProvider *mocks.MockDeploymentProvider
			if tt.useDeployment {
				mockDepProvider = mocks.NewMockDeploymentProvider(ctrl)
			}

			tt.setupMocks(mockRegProvider, mockDepProvider)

			var depProvider service.DeploymentProvider
			if tt.useDeployment {
				depProvider = mockDepProvider
			}

			svc, err := service.NewService(context.Background(), mockRegProvider, depProvider)
			require.NoError(t, err)

			servers, err := svc.GetDeployedServer(context.Background(), tt.serverName)

			if tt.expectedError != "" {
				assert.EqualError(t, err, tt.expectedError)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, servers)
				if tt.validateServers != nil {
					tt.validateServers(t, servers)
				}
			}
		})
	}
}

func TestService_WithCacheDuration(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		cacheDuration time.Duration
		setupMocks    func(*mocks.MockRegistryDataProvider)
		callCount     int
	}{
		{
			name:          "custom cache duration",
			cacheDuration: 100 * time.Millisecond,
			setupMocks: func(m *mocks.MockRegistryDataProvider) {
				// Should be called twice: once during NewService, once after cache expires
				testRegistry := registry.NewTestUpstreamRegistry()
				m.EXPECT().GetRegistryData(gomock.Any()).Return(testRegistry, nil).Times(2)
				m.EXPECT().GetSource().Return("test-source").AnyTimes()
			},
			callCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockProvider := mocks.NewMockRegistryDataProvider(ctrl)
			tt.setupMocks(mockProvider)

			svc, err := service.NewService(
				context.Background(),
				mockProvider,
				nil,
				service.WithCacheDuration(tt.cacheDuration),
			)
			require.NoError(t, err)

			// First call - should use cached data from NewService
			_, _, err = svc.GetRegistry(context.Background())
			assert.NoError(t, err)

			// Wait for cache to expire
			time.Sleep(tt.cacheDuration + 10*time.Millisecond)

			// Second call - should fetch new data
			_, _, err = svc.GetRegistry(context.Background())
			assert.NoError(t, err)
		})
	}
}
