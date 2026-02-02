package inmemory_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	toolhivetypes "github.com/stacklok/toolhive/pkg/registry/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/registry"
	"github.com/stacklok/toolhive-registry-server/internal/service"
	"github.com/stacklok/toolhive-registry-server/internal/service/inmemory"
	"github.com/stacklok/toolhive-registry-server/internal/service/inmemory/mocks"
)

// testManagedConfig creates a config with a managed registry for testing write operations
func testManagedConfig(registryName string) *config.Config {
	return &config.Config{
		Registries: []config.RegistryConfig{
			{
				Name:    registryName,
				Managed: &config.ManagedConfig{},
			},
		},
	}
}

// testFileConfig creates a config with a file registry for testing read operations
//
//nolint:unparam // registryName is always "test-registry" but keeping param for consistency with testManagedConfig
func testFileConfig(registryName string) *config.Config {
	return &config.Config{
		Registries: []config.RegistryConfig{
			{
				Name: registryName,
				File: &config.FileConfig{Path: "/path/to/registry.json"},
				SyncPolicy: &config.SyncPolicyConfig{
					Interval: "1h",
				},
			},
		},
	}
}

// testMultipleFileConfig creates a config with multiple file registries for testing
func testMultipleFileConfig(registryNames ...string) *config.Config {
	registries := make([]config.RegistryConfig, 0, len(registryNames))
	for _, name := range registryNames {
		registries = append(registries, config.RegistryConfig{
			Name: name,
			File: &config.FileConfig{Path: "/path/to/" + name + ".json"},
			SyncPolicy: &config.SyncPolicyConfig{
				Interval: "1h",
			},
		})
	}
	return &config.Config{
		Registries: registries,
	}
}

// setupGetAllRegistryData is a helper to set up mock for GetAllRegistryData with per-registry data
func setupGetAllRegistryData(m *mocks.MockRegistryDataProvider, registryName string, reg *toolhivetypes.UpstreamRegistry) {
	allData := map[string]*toolhivetypes.UpstreamRegistry{
		registryName: reg,
	}
	m.EXPECT().GetAllRegistryData(gomock.Any()).Return(allData, nil).AnyTimes()
}

// setupMultipleRegistriesData sets up mock for GetAllRegistryData with multiple registries
func setupMultipleRegistriesData(m *mocks.MockRegistryDataProvider, registries map[string]*toolhivetypes.UpstreamRegistry) {
	m.EXPECT().GetAllRegistryData(gomock.Any()).Return(registries, nil).AnyTimes()
}

func TestService_GetRegistry(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		setupMocks     func(*mocks.MockRegistryDataProvider)
		config         *config.Config
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
				setupGetAllRegistryData(m, "test-registry", testRegistry)
				m.EXPECT().GetSource().Return("file:/path/to/registry.json").AnyTimes()
				m.EXPECT().GetRegistryName().Return("test-registry").AnyTimes()
			},
			config:         testFileConfig("test-registry"),
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
				m.EXPECT().GetAllRegistryData(gomock.Any()).Return(nil, errors.New("file not found")).Times(2) // Once during NewService, once during GetRegistry
				m.EXPECT().GetSource().Return("file:/path/to/registry.json").AnyTimes()
				m.EXPECT().GetRegistryName().Return("test-registry").AnyTimes()
			},
			config:         testFileConfig("test-registry"),
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

			var opts []inmemory.Option
			if tt.config != nil {
				opts = append(opts, inmemory.WithConfig(tt.config))
			}

			svc, err := inmemory.New(context.Background(), mockProvider, opts...)
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
		config        *config.Config
		expectedError string
	}{
		{
			name: "ready with successful data load",
			setupMocks: func(m *mocks.MockRegistryDataProvider) {
				testRegistry := registry.NewTestUpstreamRegistry()
				setupGetAllRegistryData(m, "test-registry", testRegistry)
				m.EXPECT().GetRegistryName().Return("test-registry").AnyTimes()
			},
			config:        testFileConfig("test-registry"),
			expectedError: "",
		},
		{
			name: "not ready when provider fails",
			setupMocks: func(m *mocks.MockRegistryDataProvider) {
				m.EXPECT().GetAllRegistryData(gomock.Any()).Return(nil, errors.New("connection failed")).Times(2)
				m.EXPECT().GetRegistryName().Return("test-registry").AnyTimes()
			},
			config:        testFileConfig("test-registry"),
			expectedError: "registry data not available: failed to get all registry data: connection failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockProvider := mocks.NewMockRegistryDataProvider(ctrl)
			tt.setupMocks(mockProvider)

			var opts []inmemory.Option
			if tt.config != nil {
				opts = append(opts, inmemory.WithConfig(tt.config))
			}

			svc, err := inmemory.New(context.Background(), mockProvider, opts...)
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
		config          *config.Config
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
				setupGetAllRegistryData(m, "test-registry", testRegistry)
				m.EXPECT().GetRegistryName().Return("test-registry").AnyTimes()
			},
			config:        testFileConfig("test-registry"),
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
				setupGetAllRegistryData(m, "test-registry", testRegistry)
				m.EXPECT().GetRegistryName().Return("test-registry").AnyTimes()
			},
			config:        testFileConfig("test-registry"),
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
				setupGetAllRegistryData(m, "test-registry", testRegistry)
				m.EXPECT().GetRegistryName().Return("test-registry").AnyTimes()
			},
			config:        testFileConfig("test-registry"),
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
				setupGetAllRegistryData(m, "test-registry", testRegistry)
				m.EXPECT().GetRegistryName().Return("test-registry").AnyTimes()
			},
			config:        testFileConfig("test-registry"),
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
				setupGetAllRegistryData(m, "test-registry", testRegistry)
				m.EXPECT().GetRegistryName().Return("test-registry").AnyTimes()
			},
			config:        testFileConfig("test-registry"),
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

			var opts []inmemory.Option
			if tt.config != nil {
				opts = append(opts, inmemory.WithConfig(tt.config))
			}

			svc, err := inmemory.New(context.Background(), mockProvider, opts...)
			require.NoError(t, err)

			result, err := svc.ListServers(context.Background(), tt.options...)
			assert.NoError(t, err)
			assert.Len(t, result.Servers, tt.expectedCount)

			if tt.validateServers != nil {
				tt.validateServers(t, result.Servers)
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
		config         *config.Config
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
				setupGetAllRegistryData(m, "test-registry", testRegistry)
				m.EXPECT().GetRegistryName().Return("test-registry").AnyTimes()
			},
			config: testFileConfig("test-registry"),
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
				setupGetAllRegistryData(m, "test-registry", testRegistry)
				m.EXPECT().GetRegistryName().Return("test-registry").AnyTimes()
			},
			config:        testFileConfig("test-registry"),
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
				setupGetAllRegistryData(m, "test-registry", testRegistry)
				m.EXPECT().GetRegistryName().Return("test-registry").AnyTimes()
			},
			config: testFileConfig("test-registry"),
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
				setupGetAllRegistryData(m, "test-registry", testRegistry)
				m.EXPECT().GetRegistryName().Return("test-registry").AnyTimes()
			},
			config:        testFileConfig("test-registry"),
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

			var opts []inmemory.Option
			if tt.config != nil {
				opts = append(opts, inmemory.WithConfig(tt.config))
			}

			svc, err := inmemory.New(context.Background(), mockProvider, opts...)
			require.NoError(t, err)

			getOpts := []service.Option[service.GetServerVersionOptions]{
				service.WithName[service.GetServerVersionOptions](tt.serverName),
			}
			if tt.version != "" {
				getOpts = append(getOpts, service.WithVersion[service.GetServerVersionOptions](tt.version))
			}

			server, err := svc.GetServerVersion(context.Background(), getOpts...)

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
		config        *config.Config
		callCount     int
	}{
		{
			name:          "custom cache duration",
			cacheDuration: 100 * time.Millisecond,
			setupMocks: func(m *mocks.MockRegistryDataProvider) {
				// Should be called twice: once during NewService, once after cache expires
				testRegistry := registry.NewTestUpstreamRegistry()
				allData := map[string]*toolhivetypes.UpstreamRegistry{
					"test-registry": testRegistry,
				}
				m.EXPECT().GetAllRegistryData(gomock.Any()).Return(allData, nil).Times(2)
				m.EXPECT().GetSource().Return("test-source").AnyTimes()
				m.EXPECT().GetRegistryName().Return("test-registry").AnyTimes()
			},
			config:    testFileConfig("test-registry"),
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

			var opts []inmemory.Option
			opts = append(opts, inmemory.WithCacheDuration(tt.cacheDuration))
			if tt.config != nil {
				opts = append(opts, inmemory.WithConfig(tt.config))
			}

			svc, err := inmemory.New(
				context.Background(),
				mockProvider,
				opts...,
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
		config          *config.Config
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
				setupGetAllRegistryData(m, "test-registry", testRegistry)
				m.EXPECT().GetRegistryName().Return("test-registry").AnyTimes()
			},
			config:        testFileConfig("test-registry"),
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
				setupGetAllRegistryData(m, "test-registry", testRegistry)
				m.EXPECT().GetRegistryName().Return("test-registry").AnyTimes()
			},
			config:        testFileConfig("test-registry"),
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
				setupGetAllRegistryData(m, "test-registry", testRegistry)
				m.EXPECT().GetRegistryName().Return("test-registry").AnyTimes()
			},
			config:        testFileConfig("test-registry"),
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
				setupGetAllRegistryData(m, "test-registry", testRegistry)
				m.EXPECT().GetRegistryName().Return("test-registry").AnyTimes()
			},
			config:        testFileConfig("test-registry"),
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

			var opts []inmemory.Option
			if tt.config != nil {
				opts = append(opts, inmemory.WithConfig(tt.config))
			}

			svc, err := inmemory.New(context.Background(), mockProvider, opts...)
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
		config            *config.Config
		expectedCount     int
		validateRegisties func(*testing.T, []service.RegistryInfo)
	}{
		{
			name: "list single registry",
			setupMocks: func(m *mocks.MockRegistryDataProvider) {
				testRegistry := registry.NewTestUpstreamRegistry()
				setupGetAllRegistryData(m, "test-registry", testRegistry)
				m.EXPECT().GetRegistryName().Return("test-registry").AnyTimes()
				m.EXPECT().GetSource().Return("file:/path/to/registry.json").AnyTimes()
			},
			config:        testFileConfig("test-registry"),
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
				setupGetAllRegistryData(m, "git-registry", testRegistry)
				m.EXPECT().GetRegistryName().Return("git-registry").AnyTimes()
				m.EXPECT().GetSource().Return("git:https://github.com/example/registry.git").AnyTimes()
			},
			config: &config.Config{
				Registries: []config.RegistryConfig{
					{
						Name: "git-registry",
						Git: &config.GitConfig{
							Repository: "https://github.com/example/registry.git",
						},
						SyncPolicy: &config.SyncPolicyConfig{
							Interval: "1h",
						},
					},
				},
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
				setupGetAllRegistryData(m, "remote-registry", testRegistry)
				m.EXPECT().GetRegistryName().Return("remote-registry").AnyTimes()
				m.EXPECT().GetSource().Return("https://example.com/registry.json").AnyTimes()
			},
			config: &config.Config{
				Registries: []config.RegistryConfig{
					{
						Name: "remote-registry",
						API: &config.APIConfig{
							Endpoint: "https://example.com",
						},
						SyncPolicy: &config.SyncPolicyConfig{
							Interval: "1h",
						},
					},
				},
			},
			expectedCount: 1,
			validateRegisties: func(t *testing.T, registries []service.RegistryInfo) {
				t.Helper()
				assert.Equal(t, "remote-registry", registries[0].Name)
				assert.Equal(t, "API", registries[0].Type)
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

			var opts []inmemory.Option
			if tt.config != nil {
				opts = append(opts, inmemory.WithConfig(tt.config))
			}

			svc, err := inmemory.New(context.Background(), mockProvider, opts...)
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
		config           *config.Config
		expectedError    string
		validateRegistry func(*testing.T, *service.RegistryInfo)
	}{
		{
			name:         "get existing registry",
			registryName: "test-registry",
			setupMocks: func(m *mocks.MockRegistryDataProvider) {
				testRegistry := registry.NewTestUpstreamRegistry()
				setupGetAllRegistryData(m, "test-registry", testRegistry)
				m.EXPECT().GetRegistryName().Return("test-registry").AnyTimes()
				m.EXPECT().GetSource().Return("file:/path/to/registry.json").AnyTimes()
			},
			config: testFileConfig("test-registry"),
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
				setupGetAllRegistryData(m, "test-registry", testRegistry)
				m.EXPECT().GetRegistryName().Return("test-registry").AnyTimes()
			},
			config:        testFileConfig("test-registry"),
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

			var opts []inmemory.Option
			if tt.config != nil {
				opts = append(opts, inmemory.WithConfig(tt.config))
			}

			svc, err := inmemory.New(context.Background(), mockProvider, opts...)
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
		registryName   string
		setupMocks     func(*mocks.MockRegistryDataProvider)
		options        []service.Option[service.PublishServerVersionOptions]
		expectedError  string
		validateServer func(*testing.T, *upstreamv0.ServerJSON)
	}{
		{
			name:         "successful publish new server",
			registryName: "test-registry",
			setupMocks: func(m *mocks.MockRegistryDataProvider) {
				// Managed registries still call GetAllRegistryData but don't use the data
				m.EXPECT().GetAllRegistryData(gomock.Any()).Return(map[string]*toolhivetypes.UpstreamRegistry{}, nil).AnyTimes()
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
			name:         "publish to empty registry initializes data",
			registryName: "test-registry",
			setupMocks: func(m *mocks.MockRegistryDataProvider) {
				// Managed registries still call GetAllRegistryData but don't use the data
				m.EXPECT().GetAllRegistryData(gomock.Any()).Return(map[string]*toolhivetypes.UpstreamRegistry{}, nil).AnyTimes()
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
			name:         "publish with wrong registry name fails",
			registryName: "test-registry",
			setupMocks: func(m *mocks.MockRegistryDataProvider) {
				// Managed registries still call GetAllRegistryData but don't use the data
				m.EXPECT().GetAllRegistryData(gomock.Any()).Return(map[string]*toolhivetypes.UpstreamRegistry{}, nil).AnyTimes()
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
			name:         "publish without server data fails",
			registryName: "test-registry",
			setupMocks: func(m *mocks.MockRegistryDataProvider) {
				// Managed registries still call GetAllRegistryData but don't use the data
				m.EXPECT().GetAllRegistryData(gomock.Any()).Return(map[string]*toolhivetypes.UpstreamRegistry{}, nil).AnyTimes()
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

			svc, err := inmemory.New(
				context.Background(),
				mockProvider,
				inmemory.WithConfig(testManagedConfig(tt.registryName)),
			)
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
	// Managed registries still call GetAllRegistryData but don't use the data
	mockProvider.EXPECT().GetAllRegistryData(gomock.Any()).Return(map[string]*toolhivetypes.UpstreamRegistry{}, nil).AnyTimes()
	mockProvider.EXPECT().GetRegistryName().Return("test-registry").AnyTimes()

	svc, err := inmemory.New(
		context.Background(),
		mockProvider,
		inmemory.WithConfig(testManagedConfig("test-registry")),
	)
	require.NoError(t, err)

	// First publish should succeed
	_, err = svc.PublishServerVersion(context.Background(),
		service.WithRegistryName[service.PublishServerVersionOptions]("test-registry"),
		service.WithServerData(&upstreamv0.ServerJSON{
			Name:    "existing-server",
			Version: "1.0.0",
		}),
	)
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
		registryName  string
		setupMocks    func(*mocks.MockRegistryDataProvider)
		options       []service.Option[service.DeleteServerVersionOptions]
		expectedError string
		verifyDelete  func(*testing.T, service.RegistryService)
	}{
		{
			name:         "successful delete",
			registryName: "test-registry",
			setupMocks: func(m *mocks.MockRegistryDataProvider) {
				// Managed registries still call GetAllRegistryData but don't use the data
				m.EXPECT().GetAllRegistryData(gomock.Any()).Return(map[string]*toolhivetypes.UpstreamRegistry{}, nil).AnyTimes()
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
				result, err := svc.ListServers(context.Background())
				require.NoError(t, err)
				assert.Len(t, result.Servers, 1)
				assert.Equal(t, "other-server", result.Servers[0].Name)
			},
		},
		{
			name:         "delete with wrong registry name fails",
			registryName: "test-registry",
			setupMocks: func(m *mocks.MockRegistryDataProvider) {
				// Managed registries still call GetAllRegistryData but don't use the data
				m.EXPECT().GetAllRegistryData(gomock.Any()).Return(map[string]*toolhivetypes.UpstreamRegistry{}, nil).AnyTimes()
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

			svc, err := inmemory.New(
				context.Background(),
				mockProvider,
				inmemory.WithConfig(testManagedConfig(tt.registryName)),
			)
			require.NoError(t, err)

			// For the successful delete test, we need to first publish the servers
			if tt.verifyDelete != nil {
				// Publish the servers that we want to test delete with
				_, err = svc.PublishServerVersion(context.Background(),
					service.WithRegistryName[service.PublishServerVersionOptions](tt.registryName),
					service.WithServerData(&upstreamv0.ServerJSON{
						Name:    "server-to-delete",
						Version: "1.0.0",
					}),
				)
				require.NoError(t, err)

				_, err = svc.PublishServerVersion(context.Background(),
					service.WithRegistryName[service.PublishServerVersionOptions](tt.registryName),
					service.WithServerData(&upstreamv0.ServerJSON{
						Name:    "other-server",
						Version: "1.0.0",
					}),
				)
				require.NoError(t, err)
			}

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
	// Managed registries still call GetAllRegistryData but don't use the data
	mockProvider.EXPECT().GetAllRegistryData(gomock.Any()).Return(map[string]*toolhivetypes.UpstreamRegistry{}, nil).AnyTimes()
	mockProvider.EXPECT().GetRegistryName().Return("test-registry").AnyTimes()

	svc, err := inmemory.New(
		context.Background(),
		mockProvider,
		inmemory.WithConfig(testManagedConfig("test-registry")),
	)
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
		config          *config.Config
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
							registry.WithServerVersion("1.0.0"),
							registry.WithOCIPackage("server1:latest"),
						),
						registry.NewTestServer("server2",
							registry.WithServerVersion("1.0.0"),
							registry.WithOCIPackage("server2:latest"),
						),
						registry.NewTestServer("server3",
							registry.WithServerVersion("1.0.0"),
							registry.WithOCIPackage("server3:latest"),
						),
						registry.NewTestServer("server4",
							registry.WithServerVersion("1.0.0"),
							registry.WithOCIPackage("server4:latest"),
						),
					),
				)
				setupGetAllRegistryData(m, "test-registry", testRegistry)
				m.EXPECT().GetRegistryName().Return("test-registry").AnyTimes()
			},
			config: testFileConfig("test-registry"),
			// Use cursor for "server2,1.0.0" to skip to server3 (servers are sorted by name,version)
			options:       []service.Option[service.ListServersOptions]{service.WithCursor(service.EncodeCursor("server2", "1.0.0"))},
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
							registry.WithServerVersion("1.0.0"),
							registry.WithOCIPackage("server1:latest"),
						),
						registry.NewTestServer("server2",
							registry.WithServerVersion("1.0.0"),
							registry.WithOCIPackage("server2:latest"),
						),
						registry.NewTestServer("server3",
							registry.WithServerVersion("1.0.0"),
							registry.WithOCIPackage("server3:latest"),
						),
						registry.NewTestServer("server4",
							registry.WithServerVersion("1.0.0"),
							registry.WithOCIPackage("server4:latest"),
						),
					),
				)
				setupGetAllRegistryData(m, "test-registry", testRegistry)
				m.EXPECT().GetRegistryName().Return("test-registry").AnyTimes()
			},
			config: testFileConfig("test-registry"),
			// Cursor for "server1:1.0.0" skips to server2, then limit to 2
			options: []service.Option[service.ListServersOptions]{
				service.WithCursor(service.EncodeCursor("server1", "1.0.0")),
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
							registry.WithServerVersion("1.0.0"),
							registry.WithOCIPackage("server1:latest"),
						),
					),
				)
				setupGetAllRegistryData(m, "test-registry", testRegistry)
				m.EXPECT().GetRegistryName().Return("test-registry").AnyTimes()
			},
			config: testFileConfig("test-registry"),
			// Cursor for "zzz:9.9.9" is lexicographically after "server1:1.0.0"
			options:       []service.Option[service.ListServersOptions]{service.WithCursor(service.EncodeCursor("zzz", "9.9.9"))},
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
				setupGetAllRegistryData(m, "test-registry", testRegistry)
				m.EXPECT().GetRegistryName().Return("test-registry").AnyTimes()
			},
			config: testFileConfig("test-registry"),
			// Invalid base64
			options:       []service.Option[service.ListServersOptions]{service.WithCursor("not-valid-base64!!!")},
			expectedError: "invalid cursor format",
		},
		{
			name: "cursor without comma separator returns error",
			setupMocks: func(m *mocks.MockRegistryDataProvider) {
				testRegistry := registry.NewTestUpstreamRegistry(
					registry.WithServers(
						registry.NewTestServer("server1",
							registry.WithOCIPackage("server1:latest"),
						),
					),
				)
				setupGetAllRegistryData(m, "test-registry", testRegistry)
				m.EXPECT().GetRegistryName().Return("test-registry").AnyTimes()
			},
			config: testFileConfig("test-registry"),
			// "YWJj" is base64("abc"), which has no comma separator
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

			var opts []inmemory.Option
			if tt.config != nil {
				opts = append(opts, inmemory.WithConfig(tt.config))
			}

			svc, err := inmemory.New(context.Background(), mockProvider, opts...)
			require.NoError(t, err)

			result, err := svc.ListServers(context.Background(), tt.options...)

			if tt.expectedError != "" {
				assert.ErrorContains(t, err, tt.expectedError)
			} else {
				assert.NoError(t, err)
				assert.Len(t, result.Servers, tt.expectedCount)
				if tt.validateServers != nil {
					tt.validateServers(t, result.Servers)
				}
			}
		})
	}
}

// TestListServers_MultipleRegistries_NoDuplication is a regression test for GitHub issue #389.
// It verifies that when multiple registries are configured, each server appears only once
// in the ListServers response. Previously, servers could be duplicated when the provider
// returned data for multiple registries, because the service would iterate over all
// registries and add servers without proper deduplication.
func TestListServers_MultipleRegistries_NoDuplication(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Create test registries with distinct servers
	// registry-1: servers 1, 2, 3
	// registry-2: servers 4, 5
	registry1 := registry.NewTestUpstreamRegistry(
		registry.WithServers(
			registry.NewTestServer("server1",
				registry.WithDescription("Server 1 from registry-1"),
				registry.WithOCIPackage("server1:latest"),
			),
			registry.NewTestServer("server2",
				registry.WithDescription("Server 2 from registry-1"),
				registry.WithOCIPackage("server2:latest"),
			),
			registry.NewTestServer("server3",
				registry.WithDescription("Server 3 from registry-1"),
				registry.WithOCIPackage("server3:latest"),
			),
		),
	)

	registry2 := registry.NewTestUpstreamRegistry(
		registry.WithServers(
			registry.NewTestServer("server4",
				registry.WithDescription("Server 4 from registry-2"),
				registry.WithOCIPackage("server4:latest"),
			),
			registry.NewTestServer("server5",
				registry.WithDescription("Server 5 from registry-2"),
				registry.WithOCIPackage("server5:latest"),
			),
		),
	)

	// Set up mock to return both registries
	mockProvider := mocks.NewMockRegistryDataProvider(ctrl)
	setupMultipleRegistriesData(mockProvider, map[string]*toolhivetypes.UpstreamRegistry{
		"registry-1": registry1,
		"registry-2": registry2,
	})
	mockProvider.EXPECT().GetRegistryName().Return("registry-1").AnyTimes()

	// Create service with multiple registries configured
	cfg := testMultipleFileConfig("registry-1", "registry-2")
	svc, err := inmemory.New(
		context.Background(),
		mockProvider,
		inmemory.WithConfig(cfg),
	)
	require.NoError(t, err)

	// Call ListServers without any filter (should return all servers from all registries)
	result, err := svc.ListServers(context.Background())
	require.NoError(t, err)

	// Verify total count is 5 (not 10 which would indicate duplication)
	assert.Len(t, result.Servers, 5, "Expected 5 unique servers, got %d - possible duplication issue", len(result.Servers))

	// Verify each server appears exactly once by collecting names
	serverNames := make(map[string]int)
	for _, s := range result.Servers {
		serverNames[s.Name]++
	}

	// Check that all expected servers are present exactly once
	expectedServers := []string{"server1", "server2", "server3", "server4", "server5"}
	for _, name := range expectedServers {
		count, exists := serverNames[name]
		assert.True(t, exists, "Expected server %s to be present", name)
		assert.Equal(t, 1, count, "Server %s should appear exactly once, but appeared %d times", name, count)
	}

	// Verify no unexpected servers
	assert.Len(t, serverNames, 5, "Expected exactly 5 unique server names")
}

// TestService_ListServers_PaginationBehavior tests pagination edge cases
// to ensure behavior is consistent with the upstream MCP Registry API.
func TestService_ListServers_PaginationBehavior(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		serverCount        int
		limit              int
		expectedCount      int
		expectNextCursor   bool
		validateNextCursor func(*testing.T, string, int)
	}{
		{
			name:             "exact limit match returns empty NextCursor",
			serverCount:      3,
			limit:            3,
			expectedCount:    3,
			expectNextCursor: false,
		},
		{
			name:             "more servers than limit returns NextCursor",
			serverCount:      5,
			limit:            3,
			expectedCount:    3,
			expectNextCursor: true,
			validateNextCursor: func(t *testing.T, cursor string, limit int) {
				t.Helper()
				// Verify the cursor is non-empty and valid
				assert.NotEmpty(t, cursor, "NextCursor should be non-empty when more results exist")
				// The cursor should be the last server in the current page (server3,1.0.0)
				// Server names are server1, server2, server3... sorted alphabetically
				expectedCursor := service.EncodeCursor(fmt.Sprintf("server%d", limit), "1.0.0")
				assert.Equal(t, expectedCursor, cursor, "NextCursor should point to the last server in page")
			},
		},
		{
			name:             "fewer servers than limit returns empty NextCursor",
			serverCount:      2,
			limit:            5,
			expectedCount:    2,
			expectNextCursor: false,
		},
		{
			name:             "no limit uses default page size (30)",
			serverCount:      35,
			limit:            0, // No limit specified
			expectedCount:    30,
			expectNextCursor: true,
			validateNextCursor: func(t *testing.T, cursor string, _ int) {
				t.Helper()
				assert.NotEmpty(t, cursor, "NextCursor should be non-empty when more results exist")
				// With default page size 30, the last server is server9 (sorted: server1, server10-19, server2, server20-29, server3, server30)
				// Actually with alphabetical sorting: server1, server10, server11..server19, server2, server20..server29, server3, server30..server35
				// The 30th server in alphabetical order for server1-server35 needs calculation
				// Decode the cursor to verify it's valid
				name, version, err := service.DecodeCursor(cursor)
				assert.NoError(t, err, "Cursor should be decodable")
				assert.NotEmpty(t, name, "Cursor name should not be empty")
				assert.Equal(t, "1.0.0", version, "Cursor version should be 1.0.0")
			},
		},
		{
			name:             "exactly default page size returns empty NextCursor",
			serverCount:      30,
			limit:            0, // No limit specified, uses default of 30
			expectedCount:    30,
			expectNextCursor: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			// Create test servers
			servers := make([]upstreamv0.ServerJSON, tt.serverCount)
			for i := 0; i < tt.serverCount; i++ {
				servers[i] = upstreamv0.ServerJSON{
					Name:        fmt.Sprintf("server%d", i+1),
					Version:     "1.0.0",
					Description: fmt.Sprintf("Test server %d", i+1),
				}
			}

			testRegistry := &toolhivetypes.UpstreamRegistry{
				Schema:  "https://modelcontextprotocol.io/registry/v0/schema.json",
				Version: "1.0.0",
				Data: toolhivetypes.UpstreamData{
					Servers: servers,
				},
			}

			mockProvider := mocks.NewMockRegistryDataProvider(ctrl)
			setupGetAllRegistryData(mockProvider, "test-registry", testRegistry)
			mockProvider.EXPECT().GetRegistryName().Return("test-registry").AnyTimes()

			svc, err := inmemory.New(
				context.Background(),
				mockProvider,
				inmemory.WithConfig(testFileConfig("test-registry")),
			)
			require.NoError(t, err)

			var opts []service.Option[service.ListServersOptions]
			if tt.limit > 0 {
				opts = append(opts, service.WithLimit[service.ListServersOptions](tt.limit))
			}

			result, err := svc.ListServers(context.Background(), opts...)
			require.NoError(t, err)

			assert.Len(t, result.Servers, tt.expectedCount, "Expected %d servers, got %d", tt.expectedCount, len(result.Servers))

			if tt.expectNextCursor {
				assert.NotEmpty(t, result.NextCursor, "Expected NextCursor to be set")
				if tt.validateNextCursor != nil {
					tt.validateNextCursor(t, result.NextCursor, tt.limit)
				}
			} else {
				assert.Empty(t, result.NextCursor, "Expected NextCursor to be empty")
			}
		})
	}
}

// TestService_ListServers_RoundTripPagination tests that pagination works correctly
// by fetching multiple pages and verifying all servers are retrieved exactly once.
func TestService_ListServers_RoundTripPagination(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Create 7 test servers
	serverCount := 7
	servers := make([]upstreamv0.ServerJSON, serverCount)
	expectedNames := make([]string, serverCount)
	for i := 0; i < serverCount; i++ {
		name := fmt.Sprintf("server%d", i+1)
		servers[i] = upstreamv0.ServerJSON{
			Name:        name,
			Version:     "1.0.0",
			Description: fmt.Sprintf("Test server %d", i+1),
		}
		expectedNames[i] = name
	}

	testRegistry := &toolhivetypes.UpstreamRegistry{
		Schema:  "https://modelcontextprotocol.io/registry/v0/schema.json",
		Version: "1.0.0",
		Data: toolhivetypes.UpstreamData{
			Servers: servers,
		},
	}

	mockProvider := mocks.NewMockRegistryDataProvider(ctrl)
	setupGetAllRegistryData(mockProvider, "test-registry", testRegistry)
	mockProvider.EXPECT().GetRegistryName().Return("test-registry").AnyTimes()

	svc, err := inmemory.New(
		context.Background(),
		mockProvider,
		inmemory.WithConfig(testFileConfig("test-registry")),
	)
	require.NoError(t, err)

	// Fetch with limit of 3 per page
	pageSize := 3
	var allFetchedServers []*upstreamv0.ServerJSON
	cursor := ""
	pageCount := 0
	maxPages := 10 // Safety limit to prevent infinite loops

	for pageCount < maxPages {
		pageCount++
		var opts []service.Option[service.ListServersOptions]
		opts = append(opts, service.WithLimit[service.ListServersOptions](pageSize))
		if cursor != "" {
			opts = append(opts, service.WithCursor(cursor))
		}

		result, err := svc.ListServers(context.Background(), opts...)
		require.NoError(t, err)

		allFetchedServers = append(allFetchedServers, result.Servers...)

		if result.NextCursor == "" {
			break
		}
		cursor = result.NextCursor
	}

	// Verify we fetched all servers
	assert.Len(t, allFetchedServers, serverCount, "Should have fetched all %d servers", serverCount)

	// Verify pagination took 3 pages: [3, 3, 1]
	assert.Equal(t, 3, pageCount, "Should have taken 3 pages to fetch 7 servers with page size 3")

	// Verify all servers are unique and present
	fetchedNames := make(map[string]int)
	for _, s := range allFetchedServers {
		fetchedNames[s.Name]++
	}

	for _, name := range expectedNames {
		count, exists := fetchedNames[name]
		assert.True(t, exists, "Expected server %s to be fetched", name)
		assert.Equal(t, 1, count, "Server %s should appear exactly once, appeared %d times", name, count)
	}
}

// TestService_ListServers_DefaultLimit verifies that the default page size of 30
// is applied when no limit is specified, matching the upstream MCP Registry API.
func TestService_ListServers_DefaultLimit(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Create 50 test servers (more than the default page size of 30)
	serverCount := 50
	servers := make([]upstreamv0.ServerJSON, serverCount)
	for i := 0; i < serverCount; i++ {
		servers[i] = upstreamv0.ServerJSON{
			Name:        fmt.Sprintf("server%d", i+1),
			Version:     "1.0.0",
			Description: fmt.Sprintf("Test server %d", i+1),
		}
	}

	testRegistry := &toolhivetypes.UpstreamRegistry{
		Schema:  "https://modelcontextprotocol.io/registry/v0/schema.json",
		Version: "1.0.0",
		Data: toolhivetypes.UpstreamData{
			Servers: servers,
		},
	}

	mockProvider := mocks.NewMockRegistryDataProvider(ctrl)
	setupGetAllRegistryData(mockProvider, "test-registry", testRegistry)
	mockProvider.EXPECT().GetRegistryName().Return("test-registry").AnyTimes()

	svc, err := inmemory.New(
		context.Background(),
		mockProvider,
		inmemory.WithConfig(testFileConfig("test-registry")),
	)
	require.NoError(t, err)

	// Call ListServers without specifying a limit
	result, err := svc.ListServers(context.Background())
	require.NoError(t, err)

	// Should return exactly 30 servers (the default page size)
	assert.Len(t, result.Servers, service.DefaultPageSize,
		"Without limit, should return default page size of %d servers", service.DefaultPageSize)

	// Should have a NextCursor since there are more servers
	assert.NotEmpty(t, result.NextCursor, "NextCursor should be set when more results exist")
}

// TestService_ListServers_MaxLimit verifies that the limit is capped at MaxPageSize
// to prevent potential DoS attacks.
func TestService_ListServers_MaxLimit(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Create more servers than MaxPageSize
	serverCount := service.MaxPageSize + 100
	servers := make([]upstreamv0.ServerJSON, serverCount)
	for i := range serverCount {
		servers[i] = upstreamv0.ServerJSON{
			Name:        fmt.Sprintf("server%d", i+1),
			Version:     "1.0.0",
			Description: fmt.Sprintf("Test server %d", i+1),
		}
	}

	testRegistry := &toolhivetypes.UpstreamRegistry{
		Schema:  "https://modelcontextprotocol.io/registry/v0/schema.json",
		Version: "1.0.0",
		Data: toolhivetypes.UpstreamData{
			Servers: servers,
		},
	}

	mockProvider := mocks.NewMockRegistryDataProvider(ctrl)
	setupGetAllRegistryData(mockProvider, "test-registry", testRegistry)
	mockProvider.EXPECT().GetRegistryName().Return("test-registry").AnyTimes()

	svc, err := inmemory.New(
		context.Background(),
		mockProvider,
		inmemory.WithConfig(testFileConfig("test-registry")),
	)
	require.NoError(t, err)

	// Request more than MaxPageSize
	result, err := svc.ListServers(
		context.Background(),
		service.WithLimit[service.ListServersOptions](service.MaxPageSize+500),
	)
	require.NoError(t, err)

	// Should be capped at MaxPageSize
	assert.Len(t, result.Servers, service.MaxPageSize,
		"Limit should be capped at MaxPageSize (%d)", service.MaxPageSize)

	// Should have a NextCursor since there are more servers
	assert.NotEmpty(t, result.NextCursor, "NextCursor should be set when more results exist")
}
