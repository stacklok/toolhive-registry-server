package inmemory_test

import (
	"context"
	"errors"
	"testing"
	"time"

	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	toolhivetypes "github.com/stacklok/toolhive/pkg/registry/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/internal/registry"
	"github.com/stacklok/toolhive-registry-server/internal/service"
	"github.com/stacklok/toolhive-registry-server/internal/service/inmemory"
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
				assert.Len(t, r.Data.Servers, 1)
				assert.Equal(t, "test-server", r.Data.Servers[0].Name)
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
				assert.Empty(t, r.Data.Servers)
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

			svc, err := inmemory.New(context.Background(), mockProvider)
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

			svc, err := inmemory.New(context.Background(), mockProvider)
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
		options         []service.Option[service.ListServersOptions]
		expectedCount   int
		validateServers func(*testing.T, []*upstreamv0.ServerJSON)
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
				m.EXPECT().GetRegistryName().Return("test-registry").AnyTimes()
			},
			expectedCount: 3,
			validateServers: func(t *testing.T, servers []*upstreamv0.ServerJSON) {
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
				m.EXPECT().GetRegistryName().Return("test-registry").AnyTimes()
			},
			expectedCount: 0,
		},
		{
			name: "list servers with limit",
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
						registry.NewTestServer("server3",
							registry.WithDescription("Server 3"),
							registry.WithOCIPackage("server3:latest"),
						),
					),
				)
				m.EXPECT().GetRegistryData(gomock.Any()).Return(testRegistry, nil).AnyTimes()
				m.EXPECT().GetRegistryName().Return("test-registry").AnyTimes()
			},
			options:       []service.Option[service.ListServersOptions]{service.WithLimit[service.ListServersOptions](2)},
			expectedCount: 2,
		},
		{
			name: "list servers with search filter",
			setupMocks: func(m *mocks.MockRegistryDataProvider) {
				testRegistry := registry.NewTestUpstreamRegistry(
					registry.WithServers(
						registry.NewTestServer("weather-server",
							registry.WithDescription("Weather data provider"),
							registry.WithOCIPackage("weather:latest"),
						),
						registry.NewTestServer("database-server",
							registry.WithDescription("Database connector"),
							registry.WithOCIPackage("database:latest"),
						),
					),
				)
				m.EXPECT().GetRegistryData(gomock.Any()).Return(testRegistry, nil).AnyTimes()
				m.EXPECT().GetRegistryName().Return("test-registry").AnyTimes()
			},
			options:       []service.Option[service.ListServersOptions]{service.WithSearch("weather")},
			expectedCount: 1,
			validateServers: func(t *testing.T, servers []*upstreamv0.ServerJSON) {
				t.Helper()
				assert.Equal(t, "weather-server", servers[0].Name)
			},
		},
		{
			name: "list servers with non-matching registry name returns empty",
			setupMocks: func(m *mocks.MockRegistryDataProvider) {
				testRegistry := registry.NewTestUpstreamRegistry(
					registry.WithServers(
						registry.NewTestServer("server1",
							registry.WithDescription("Server 1"),
							registry.WithOCIPackage("server1:latest"),
						),
					),
				)
				m.EXPECT().GetRegistryData(gomock.Any()).Return(testRegistry, nil).AnyTimes()
				m.EXPECT().GetRegistryName().Return("test-registry").AnyTimes()
			},
			options:       []service.Option[service.ListServersOptions]{service.WithRegistryName[service.ListServersOptions]("other-registry")},
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

			svc, err := inmemory.New(context.Background(), mockProvider)
			require.NoError(t, err)

			servers, err := svc.ListServers(context.Background(), tt.options...)
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
		version        string
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
				m.EXPECT().GetRegistryName().Return("test-registry").AnyTimes()
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
				m.EXPECT().GetRegistryName().Return("test-registry").AnyTimes()
			},
			expectedError: "server not found",
		},
		{
			name:       "get server with specific version",
			serverName: "test-server",
			version:    "1.0.0",
			setupMocks: func(m *mocks.MockRegistryDataProvider) {
				testRegistry := registry.NewTestUpstreamRegistry(
					registry.WithServers(
						registry.NewTestServer("test-server",
							registry.WithDescription("A test server"),
							registry.WithServerVersion("1.0.0"),
							registry.WithOCIPackage("test:1.0.0"),
						),
					),
				)
				m.EXPECT().GetRegistryData(gomock.Any()).Return(testRegistry, nil).AnyTimes()
				m.EXPECT().GetRegistryName().Return("test-registry").AnyTimes()
			},
			validateServer: func(t *testing.T, s upstreamv0.ServerJSON) {
				t.Helper()
				assert.Equal(t, "test-server", s.Name)
				assert.Equal(t, "1.0.0", s.Version)
			},
		},
		{
			name:       "get server with non-matching version",
			serverName: "test-server",
			version:    "2.0.0",
			setupMocks: func(m *mocks.MockRegistryDataProvider) {
				testRegistry := registry.NewTestUpstreamRegistry(
					registry.WithServers(
						registry.NewTestServer("test-server",
							registry.WithDescription("A test server"),
							registry.WithServerVersion("1.0.0"),
							registry.WithOCIPackage("test:1.0.0"),
						),
					),
				)
				m.EXPECT().GetRegistryData(gomock.Any()).Return(testRegistry, nil).AnyTimes()
				m.EXPECT().GetRegistryName().Return("test-registry").AnyTimes()
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

			svc, err := inmemory.New(context.Background(), mockProvider)
			require.NoError(t, err)

			opts := []service.Option[service.GetServerVersionOptions]{
				service.WithName[service.GetServerVersionOptions](tt.serverName),
			}
			if tt.version != "" {
				opts = append(opts, service.WithVersion[service.GetServerVersionOptions](tt.version))
			}

			server, err := svc.GetServerVersion(context.Background(), opts...)

			if tt.expectedError != "" {
				assert.EqualError(t, err, tt.expectedError)
				assert.Nil(t, server)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, server)
				if tt.validateServer != nil {
					tt.validateServer(t, *server)
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

			svc, err := inmemory.New(
				context.Background(),
				mockProvider,
				inmemory.WithCacheDuration(tt.cacheDuration),
			)
			require.NoError(t, err)

			// First call - should use cached data from New
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

func TestService_ListServerVersions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		setupMocks      func(*mocks.MockRegistryDataProvider)
		options         []service.Option[service.ListServerVersionsOptions]
		expectedCount   int
		validateServers func(*testing.T, []*upstreamv0.ServerJSON)
	}{
		{
			name: "list server versions by name",
			setupMocks: func(m *mocks.MockRegistryDataProvider) {
				testRegistry := registry.NewTestUpstreamRegistry(
					registry.WithServers(
						registry.NewTestServer("test-server",
							registry.WithDescription("Test server v1"),
							registry.WithServerVersion("1.0.0"),
							registry.WithOCIPackage("test:1.0.0"),
						),
						registry.NewTestServer("other-server",
							registry.WithDescription("Other server"),
							registry.WithOCIPackage("other:latest"),
						),
					),
				)
				m.EXPECT().GetRegistryData(gomock.Any()).Return(testRegistry, nil).AnyTimes()
				m.EXPECT().GetRegistryName().Return("test-registry").AnyTimes()
			},
			options:       []service.Option[service.ListServerVersionsOptions]{service.WithName[service.ListServerVersionsOptions]("test-server")},
			expectedCount: 1,
			validateServers: func(t *testing.T, servers []*upstreamv0.ServerJSON) {
				t.Helper()
				assert.Equal(t, "test-server", servers[0].Name)
			},
		},
		{
			name: "list all servers when no name filter",
			setupMocks: func(m *mocks.MockRegistryDataProvider) {
				testRegistry := registry.NewTestUpstreamRegistry(
					registry.WithServers(
						registry.NewTestServer("server1",
							registry.WithOCIPackage("server1:latest"),
						),
						registry.NewTestServer("server2",
							registry.WithOCIPackage("server2:latest"),
						),
					),
				)
				m.EXPECT().GetRegistryData(gomock.Any()).Return(testRegistry, nil).AnyTimes()
				m.EXPECT().GetRegistryName().Return("test-registry").AnyTimes()
			},
			expectedCount: 2,
		},
		{
			name: "list server versions with limit",
			setupMocks: func(m *mocks.MockRegistryDataProvider) {
				testRegistry := registry.NewTestUpstreamRegistry(
					registry.WithServers(
						registry.NewTestServer("server1",
							registry.WithOCIPackage("server1:latest"),
						),
						registry.NewTestServer("server2",
							registry.WithOCIPackage("server2:latest"),
						),
						registry.NewTestServer("server3",
							registry.WithOCIPackage("server3:latest"),
						),
					),
				)
				m.EXPECT().GetRegistryData(gomock.Any()).Return(testRegistry, nil).AnyTimes()
				m.EXPECT().GetRegistryName().Return("test-registry").AnyTimes()
			},
			options:       []service.Option[service.ListServerVersionsOptions]{service.WithLimit[service.ListServerVersionsOptions](2)},
			expectedCount: 2,
		},
		{
			name: "list server versions with non-matching registry",
			setupMocks: func(m *mocks.MockRegistryDataProvider) {
				testRegistry := registry.NewTestUpstreamRegistry(
					registry.WithServers(
						registry.NewTestServer("server1",
							registry.WithOCIPackage("server1:latest"),
						),
					),
				)
				m.EXPECT().GetRegistryData(gomock.Any()).Return(testRegistry, nil).AnyTimes()
				m.EXPECT().GetRegistryName().Return("test-registry").AnyTimes()
			},
			options:       []service.Option[service.ListServerVersionsOptions]{service.WithRegistryName[service.ListServerVersionsOptions]("other-registry")},
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

			svc, err := inmemory.New(context.Background(), mockProvider)
			require.NoError(t, err)

			servers, err := svc.ListServerVersions(context.Background(), tt.options...)
			assert.NoError(t, err)
			assert.Len(t, servers, tt.expectedCount)

			if tt.validateServers != nil {
				tt.validateServers(t, servers)
			}
		})
	}
}

func TestService_ListRegistries(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name              string
		setupMocks        func(*mocks.MockRegistryDataProvider)
		expectedCount     int
		validateRegisties func(*testing.T, []service.RegistryInfo)
	}{
		{
			name: "list single registry",
			setupMocks: func(m *mocks.MockRegistryDataProvider) {
				testRegistry := registry.NewTestUpstreamRegistry()
				m.EXPECT().GetRegistryData(gomock.Any()).Return(testRegistry, nil).AnyTimes()
				m.EXPECT().GetRegistryName().Return("test-registry").AnyTimes()
				m.EXPECT().GetSource().Return("file:/path/to/registry.json").AnyTimes()
			},
			expectedCount: 1,
			validateRegisties: func(t *testing.T, registries []service.RegistryInfo) {
				t.Helper()
				assert.Equal(t, "test-registry", registries[0].Name)
				assert.Equal(t, "FILE", registries[0].Type)
				assert.Nil(t, registries[0].SyncStatus)
			},
		},
		{
			name: "list registry with git source",
			setupMocks: func(m *mocks.MockRegistryDataProvider) {
				testRegistry := registry.NewTestUpstreamRegistry()
				m.EXPECT().GetRegistryData(gomock.Any()).Return(testRegistry, nil).AnyTimes()
				m.EXPECT().GetRegistryName().Return("git-registry").AnyTimes()
				m.EXPECT().GetSource().Return("git:https://github.com/example/registry.git").AnyTimes()
			},
			expectedCount: 1,
			validateRegisties: func(t *testing.T, registries []service.RegistryInfo) {
				t.Helper()
				assert.Equal(t, "git-registry", registries[0].Name)
				assert.Equal(t, "GIT", registries[0].Type)
			},
		},
		{
			name: "list registry with remote source",
			setupMocks: func(m *mocks.MockRegistryDataProvider) {
				testRegistry := registry.NewTestUpstreamRegistry()
				m.EXPECT().GetRegistryData(gomock.Any()).Return(testRegistry, nil).AnyTimes()
				m.EXPECT().GetRegistryName().Return("remote-registry").AnyTimes()
				m.EXPECT().GetSource().Return("https://example.com/registry.json").AnyTimes()
			},
			expectedCount: 1,
			validateRegisties: func(t *testing.T, registries []service.RegistryInfo) {
				t.Helper()
				assert.Equal(t, "remote-registry", registries[0].Name)
				assert.Equal(t, "REMOTE", registries[0].Type)
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

			svc, err := inmemory.New(context.Background(), mockProvider)
			require.NoError(t, err)

			registries, err := svc.ListRegistries(context.Background())
			assert.NoError(t, err)
			assert.Len(t, registries, tt.expectedCount)

			if tt.validateRegisties != nil {
				tt.validateRegisties(t, registries)
			}
		})
	}
}

func TestService_GetRegistryByName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name             string
		registryName     string
		setupMocks       func(*mocks.MockRegistryDataProvider)
		expectedError    string
		validateRegistry func(*testing.T, *service.RegistryInfo)
	}{
		{
			name:         "get existing registry",
			registryName: "test-registry",
			setupMocks: func(m *mocks.MockRegistryDataProvider) {
				testRegistry := registry.NewTestUpstreamRegistry()
				m.EXPECT().GetRegistryData(gomock.Any()).Return(testRegistry, nil).AnyTimes()
				m.EXPECT().GetRegistryName().Return("test-registry").AnyTimes()
				m.EXPECT().GetSource().Return("file:/path/to/registry.json").AnyTimes()
			},
			validateRegistry: func(t *testing.T, reg *service.RegistryInfo) {
				t.Helper()
				assert.Equal(t, "test-registry", reg.Name)
				assert.Equal(t, "FILE", reg.Type)
			},
		},
		{
			name:         "registry not found",
			registryName: "nonexistent-registry",
			setupMocks: func(m *mocks.MockRegistryDataProvider) {
				testRegistry := registry.NewTestUpstreamRegistry()
				m.EXPECT().GetRegistryData(gomock.Any()).Return(testRegistry, nil).AnyTimes()
				m.EXPECT().GetRegistryName().Return("test-registry").AnyTimes()
			},
			expectedError: "registry not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockProvider := mocks.NewMockRegistryDataProvider(ctrl)
			tt.setupMocks(mockProvider)

			svc, err := inmemory.New(context.Background(), mockProvider)
			require.NoError(t, err)

			reg, err := svc.GetRegistryByName(context.Background(), tt.registryName)

			if tt.expectedError != "" {
				assert.EqualError(t, err, tt.expectedError)
				assert.Nil(t, reg)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, reg)
				if tt.validateRegistry != nil {
					tt.validateRegistry(t, reg)
				}
			}
		})
	}
}

func TestService_PublishServerVersion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		setupMocks     func(*mocks.MockRegistryDataProvider)
		options        []service.Option[service.PublishServerVersionOptions]
		expectedError  string
		validateServer func(*testing.T, *upstreamv0.ServerJSON)
	}{
		{
			name: "successful publish new server",
			setupMocks: func(m *mocks.MockRegistryDataProvider) {
				testRegistry := registry.NewTestUpstreamRegistry()
				m.EXPECT().GetRegistryData(gomock.Any()).Return(testRegistry, nil).AnyTimes()
				m.EXPECT().GetRegistryName().Return("test-registry").AnyTimes()
			},
			options: []service.Option[service.PublishServerVersionOptions]{
				service.WithRegistryName[service.PublishServerVersionOptions]("test-registry"),
				service.WithServerData(&upstreamv0.ServerJSON{
					Name:        "new-server",
					Version:     "1.0.0",
					Description: "A new server",
				}),
			},
			validateServer: func(t *testing.T, s *upstreamv0.ServerJSON) {
				t.Helper()
				assert.Equal(t, "new-server", s.Name)
				assert.Equal(t, "1.0.0", s.Version)
				assert.Equal(t, "A new server", s.Description)
			},
		},
		{
			name: "publish to empty registry initializes data",
			setupMocks: func(m *mocks.MockRegistryDataProvider) {
				// Return nil to simulate empty/uninitialized registry
				m.EXPECT().GetRegistryData(gomock.Any()).Return(nil, errors.New("no data")).AnyTimes()
				m.EXPECT().GetRegistryName().Return("test-registry").AnyTimes()
			},
			options: []service.Option[service.PublishServerVersionOptions]{
				service.WithRegistryName[service.PublishServerVersionOptions]("test-registry"),
				service.WithServerData(&upstreamv0.ServerJSON{
					Name:        "first-server",
					Version:     "1.0.0",
					Description: "First server in empty registry",
				}),
			},
			validateServer: func(t *testing.T, s *upstreamv0.ServerJSON) {
				t.Helper()
				assert.Equal(t, "first-server", s.Name)
				assert.Equal(t, "1.0.0", s.Version)
			},
		},
		{
			name: "publish with wrong registry name fails",
			setupMocks: func(m *mocks.MockRegistryDataProvider) {
				testRegistry := registry.NewTestUpstreamRegistry()
				m.EXPECT().GetRegistryData(gomock.Any()).Return(testRegistry, nil).AnyTimes()
				m.EXPECT().GetRegistryName().Return("test-registry").AnyTimes()
			},
			options: []service.Option[service.PublishServerVersionOptions]{
				service.WithRegistryName[service.PublishServerVersionOptions]("wrong-registry"),
				service.WithServerData(&upstreamv0.ServerJSON{
					Name:    "new-server",
					Version: "1.0.0",
				}),
			},
			expectedError: "registry not found: wrong-registry",
		},
		{
			name: "publish without server data fails",
			setupMocks: func(m *mocks.MockRegistryDataProvider) {
				testRegistry := registry.NewTestUpstreamRegistry()
				m.EXPECT().GetRegistryData(gomock.Any()).Return(testRegistry, nil).AnyTimes()
				m.EXPECT().GetRegistryName().Return("test-registry").AnyTimes()
			},
			options: []service.Option[service.PublishServerVersionOptions]{
				service.WithRegistryName[service.PublishServerVersionOptions]("test-registry"),
			},
			expectedError: "server data is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockProvider := mocks.NewMockRegistryDataProvider(ctrl)
			tt.setupMocks(mockProvider)

			svc, err := inmemory.New(context.Background(), mockProvider)
			require.NoError(t, err)

			server, err := svc.PublishServerVersion(context.Background(), tt.options...)

			if tt.expectedError != "" {
				assert.ErrorContains(t, err, tt.expectedError)
				assert.Nil(t, server)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, server)
				if tt.validateServer != nil {
					tt.validateServer(t, server)
				}
			}
		})
	}
}

func TestService_PublishServerVersion_Duplicate(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockProvider := mocks.NewMockRegistryDataProvider(ctrl)
	testRegistry := registry.NewTestUpstreamRegistry(
		registry.WithServers(
			registry.NewTestServer("existing-server",
				registry.WithServerVersion("1.0.0"),
				registry.WithDescription("Existing server"),
			),
		),
	)
	mockProvider.EXPECT().GetRegistryData(gomock.Any()).Return(testRegistry, nil).AnyTimes()
	mockProvider.EXPECT().GetRegistryName().Return("test-registry").AnyTimes()

	svc, err := inmemory.New(context.Background(), mockProvider)
	require.NoError(t, err)

	// Try to publish a server with the same name and version
	_, err = svc.PublishServerVersion(context.Background(),
		service.WithRegistryName[service.PublishServerVersionOptions]("test-registry"),
		service.WithServerData(&upstreamv0.ServerJSON{
			Name:    "existing-server",
			Version: "1.0.0",
		}),
	)

	assert.ErrorIs(t, err, service.ErrVersionAlreadyExists)
	assert.ErrorContains(t, err, "existing-server@1.0.0")
}

func TestService_DeleteServerVersion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		setupMocks    func(*mocks.MockRegistryDataProvider)
		options       []service.Option[service.DeleteServerVersionOptions]
		expectedError string
		verifyDelete  func(*testing.T, service.RegistryService)
	}{
		{
			name: "successful delete",
			setupMocks: func(m *mocks.MockRegistryDataProvider) {
				testRegistry := registry.NewTestUpstreamRegistry(
					registry.WithServers(
						registry.NewTestServer("server-to-delete",
							registry.WithServerVersion("1.0.0"),
							registry.WithOCIPackage("test:1.0.0"),
						),
						registry.NewTestServer("other-server",
							registry.WithServerVersion("1.0.0"),
							registry.WithOCIPackage("other:1.0.0"),
						),
					),
				)
				m.EXPECT().GetRegistryData(gomock.Any()).Return(testRegistry, nil).AnyTimes()
				m.EXPECT().GetRegistryName().Return("test-registry").AnyTimes()
			},
			options: []service.Option[service.DeleteServerVersionOptions]{
				service.WithRegistryName[service.DeleteServerVersionOptions]("test-registry"),
				service.WithName[service.DeleteServerVersionOptions]("server-to-delete"),
				service.WithVersion[service.DeleteServerVersionOptions]("1.0.0"),
			},
			verifyDelete: func(t *testing.T, svc service.RegistryService) {
				t.Helper()
				// Verify the server was deleted
				servers, err := svc.ListServers(context.Background())
				require.NoError(t, err)
				assert.Len(t, servers, 1)
				assert.Equal(t, "other-server", servers[0].Name)
			},
		},
		{
			name: "delete with wrong registry name fails",
			setupMocks: func(m *mocks.MockRegistryDataProvider) {
				testRegistry := registry.NewTestUpstreamRegistry(
					registry.WithServers(
						registry.NewTestServer("server",
							registry.WithServerVersion("1.0.0"),
						),
					),
				)
				m.EXPECT().GetRegistryData(gomock.Any()).Return(testRegistry, nil).AnyTimes()
				m.EXPECT().GetRegistryName().Return("test-registry").AnyTimes()
			},
			options: []service.Option[service.DeleteServerVersionOptions]{
				service.WithRegistryName[service.DeleteServerVersionOptions]("wrong-registry"),
				service.WithName[service.DeleteServerVersionOptions]("server"),
				service.WithVersion[service.DeleteServerVersionOptions]("1.0.0"),
			},
			expectedError: "registry not found: wrong-registry",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockProvider := mocks.NewMockRegistryDataProvider(ctrl)
			tt.setupMocks(mockProvider)

			svc, err := inmemory.New(context.Background(), mockProvider)
			require.NoError(t, err)

			err = svc.DeleteServerVersion(context.Background(), tt.options...)

			if tt.expectedError != "" {
				assert.ErrorContains(t, err, tt.expectedError)
			} else {
				assert.NoError(t, err)
				if tt.verifyDelete != nil {
					tt.verifyDelete(t, svc)
				}
			}
		})
	}
}

func TestService_DeleteServerVersion_NotFound(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockProvider := mocks.NewMockRegistryDataProvider(ctrl)
	testRegistry := registry.NewTestUpstreamRegistry(
		registry.WithServers(
			registry.NewTestServer("existing-server",
				registry.WithServerVersion("1.0.0"),
			),
		),
	)
	mockProvider.EXPECT().GetRegistryData(gomock.Any()).Return(testRegistry, nil).AnyTimes()
	mockProvider.EXPECT().GetRegistryName().Return("test-registry").AnyTimes()

	svc, err := inmemory.New(context.Background(), mockProvider)
	require.NoError(t, err)

	// Try to delete a non-existent server
	err = svc.DeleteServerVersion(context.Background(),
		service.WithRegistryName[service.DeleteServerVersionOptions]("test-registry"),
		service.WithName[service.DeleteServerVersionOptions]("nonexistent-server"),
		service.WithVersion[service.DeleteServerVersionOptions]("1.0.0"),
	)

	assert.ErrorIs(t, err, service.ErrServerNotFound)
	assert.ErrorContains(t, err, "nonexistent-server@1.0.0")
}

func TestService_ListServers_WithCursor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		setupMocks      func(*mocks.MockRegistryDataProvider)
		options         []service.Option[service.ListServersOptions]
		expectedCount   int
		expectedError   string
		validateServers func(*testing.T, []*upstreamv0.ServerJSON)
	}{
		{
			name: "list servers with cursor skips to position",
			setupMocks: func(m *mocks.MockRegistryDataProvider) {
				testRegistry := registry.NewTestUpstreamRegistry(
					registry.WithServers(
						registry.NewTestServer("server1",
							registry.WithOCIPackage("server1:latest"),
						),
						registry.NewTestServer("server2",
							registry.WithOCIPackage("server2:latest"),
						),
						registry.NewTestServer("server3",
							registry.WithOCIPackage("server3:latest"),
						),
						registry.NewTestServer("server4",
							registry.WithOCIPackage("server4:latest"),
						),
					),
				)
				m.EXPECT().GetRegistryData(gomock.Any()).Return(testRegistry, nil).AnyTimes()
				m.EXPECT().GetRegistryName().Return("test-registry").AnyTimes()
			},
			// Use EncodeCursor(2) which generates base64("2") = "Mg==" to skip first 2 servers
			options:       []service.Option[service.ListServersOptions]{service.WithCursor(inmemory.EncodeCursor(2))},
			expectedCount: 2,
			validateServers: func(t *testing.T, servers []*upstreamv0.ServerJSON) {
				t.Helper()
				assert.Equal(t, "server3", servers[0].Name)
				assert.Equal(t, "server4", servers[1].Name)
			},
		},
		{
			name: "cursor with limit",
			setupMocks: func(m *mocks.MockRegistryDataProvider) {
				testRegistry := registry.NewTestUpstreamRegistry(
					registry.WithServers(
						registry.NewTestServer("server1",
							registry.WithOCIPackage("server1:latest"),
						),
						registry.NewTestServer("server2",
							registry.WithOCIPackage("server2:latest"),
						),
						registry.NewTestServer("server3",
							registry.WithOCIPackage("server3:latest"),
						),
						registry.NewTestServer("server4",
							registry.WithOCIPackage("server4:latest"),
						),
					),
				)
				m.EXPECT().GetRegistryData(gomock.Any()).Return(testRegistry, nil).AnyTimes()
				m.EXPECT().GetRegistryName().Return("test-registry").AnyTimes()
			},
			// Cursor "MQ==" is base64("1"), skip first 1, then limit to 2
			options: []service.Option[service.ListServersOptions]{
				service.WithCursor("MQ=="),
				service.WithLimit[service.ListServersOptions](2),
			},
			expectedCount: 2,
			validateServers: func(t *testing.T, servers []*upstreamv0.ServerJSON) {
				t.Helper()
				assert.Equal(t, "server2", servers[0].Name)
				assert.Equal(t, "server3", servers[1].Name)
			},
		},
		{
			name: "cursor beyond data returns empty",
			setupMocks: func(m *mocks.MockRegistryDataProvider) {
				testRegistry := registry.NewTestUpstreamRegistry(
					registry.WithServers(
						registry.NewTestServer("server1",
							registry.WithOCIPackage("server1:latest"),
						),
					),
				)
				m.EXPECT().GetRegistryData(gomock.Any()).Return(testRegistry, nil).AnyTimes()
				m.EXPECT().GetRegistryName().Return("test-registry").AnyTimes()
			},
			// Cursor "MTAw" is base64("100"), way beyond the 1 server we have
			options:       []service.Option[service.ListServersOptions]{service.WithCursor("MTAw")},
			expectedCount: 0,
		},
		{
			name: "invalid cursor format returns error",
			setupMocks: func(m *mocks.MockRegistryDataProvider) {
				testRegistry := registry.NewTestUpstreamRegistry(
					registry.WithServers(
						registry.NewTestServer("server1",
							registry.WithOCIPackage("server1:latest"),
						),
					),
				)
				m.EXPECT().GetRegistryData(gomock.Any()).Return(testRegistry, nil).AnyTimes()
				m.EXPECT().GetRegistryName().Return("test-registry").AnyTimes()
			},
			// Invalid base64
			options:       []service.Option[service.ListServersOptions]{service.WithCursor("not-valid-base64!!!")},
			expectedError: "invalid cursor format",
		},
		{
			name: "cursor with non-numeric value returns error",
			setupMocks: func(m *mocks.MockRegistryDataProvider) {
				testRegistry := registry.NewTestUpstreamRegistry(
					registry.WithServers(
						registry.NewTestServer("server1",
							registry.WithOCIPackage("server1:latest"),
						),
					),
				)
				m.EXPECT().GetRegistryData(gomock.Any()).Return(testRegistry, nil).AnyTimes()
				m.EXPECT().GetRegistryName().Return("test-registry").AnyTimes()
			},
			// "YWJj" is base64("abc"), which is not a number
			options:       []service.Option[service.ListServersOptions]{service.WithCursor("YWJj")},
			expectedError: "invalid cursor format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockProvider := mocks.NewMockRegistryDataProvider(ctrl)
			tt.setupMocks(mockProvider)

			svc, err := inmemory.New(context.Background(), mockProvider)
			require.NoError(t, err)

			servers, err := svc.ListServers(context.Background(), tt.options...)

			if tt.expectedError != "" {
				assert.ErrorContains(t, err, tt.expectedError)
			} else {
				assert.NoError(t, err)
				assert.Len(t, servers, tt.expectedCount)
				if tt.validateServers != nil {
					tt.validateServers(t, servers)
				}
			}
		})
	}
}
