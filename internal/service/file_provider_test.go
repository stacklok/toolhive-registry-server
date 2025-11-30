package service

import (
	"context"
	"testing"

	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	toolhivetypes "github.com/stacklok/toolhive/pkg/registry/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/registry"
	sourcesmocks "github.com/stacklok/toolhive-registry-server/internal/sources/mocks"
)

func TestFileRegistryDataProvider_GetRegistryName(t *testing.T) {
	t.Parallel()

	provider := &fileRegistryDataProvider{
		registryName: "test-registry",
	}

	assert.Equal(t, "test-registry", provider.GetRegistryName())
}

func TestNewFileRegistryDataProvider(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockStorageManager := sourcesmocks.NewMockStorageManager(ctrl)
	cfg := &config.Config{
		RegistryName: "test-registry",
		Registries: []config.RegistryConfig{
			{
				Name: "registry-1",
				File: &config.FileConfig{Path: "/path/to/file1.json"},
			},
		},
	}

	provider := NewFileRegistryDataProvider(mockStorageManager, cfg)

	require.NotNil(t, provider)
	assert.Equal(t, "test-registry", provider.GetRegistryName())
}

func TestFileRegistryDataProvider_GetRegistryData(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		setupMock     func(*sourcesmocks.MockStorageManager)
		expectedErr   bool
		expectedCount int
		errorContains string
	}{
		{
			name: "success with single registry",
			setupMock: func(m *sourcesmocks.MockStorageManager) {
				allRegs := map[string]*toolhivetypes.UpstreamRegistry{
					"registry-1": registry.NewTestUpstreamRegistry(
						registry.WithServers(
							registry.NewTestServer("test-server-1"),
						),
					),
				}
				m.EXPECT().
					GetAll(gomock.Any()).
					Return(allRegs, nil)
			},
			expectedErr:   false,
			expectedCount: 1,
		},
		{
			name: "success with multiple registries",
			setupMock: func(m *sourcesmocks.MockStorageManager) {
				allRegs := map[string]*toolhivetypes.UpstreamRegistry{
					"registry-1": registry.NewTestUpstreamRegistry(
						registry.WithServers(
							registry.NewTestServer("test-server-1"),
							registry.NewTestServer("test-server-2"),
						),
					),
					"registry-2": registry.NewTestUpstreamRegistry(
						registry.WithServers(
							registry.NewTestServer("test-server-3"),
						),
					),
				}
				m.EXPECT().
					GetAll(gomock.Any()).
					Return(allRegs, nil)
			},
			expectedErr:   false,
			expectedCount: 3,
		},
		{
			name: "success with empty registry",
			setupMock: func(m *sourcesmocks.MockStorageManager) {
				allRegs := map[string]*toolhivetypes.UpstreamRegistry{}
				m.EXPECT().
					GetAll(gomock.Any()).
					Return(allRegs, nil)
			},
			expectedErr:   false,
			expectedCount: 0,
		},
		{
			name: "success with nil registries skipped",
			setupMock: func(m *sourcesmocks.MockStorageManager) {
				allRegs := map[string]*toolhivetypes.UpstreamRegistry{
					"registry-1": registry.NewTestUpstreamRegistry(
						registry.WithServers(
							registry.NewTestServer("test-server-1"),
						),
					),
					"registry-2": nil, // Should be skipped
				}
				m.EXPECT().
					GetAll(gomock.Any()).
					Return(allRegs, nil)
			},
			expectedErr:   false,
			expectedCount: 1,
		},
		{
			name: "error from storage manager",
			setupMock: func(m *sourcesmocks.MockStorageManager) {
				m.EXPECT().
					GetAll(gomock.Any()).
					Return(nil, assert.AnError)
			},
			expectedErr:   true,
			errorContains: "failed to get registry data",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockStorageManager := sourcesmocks.NewMockStorageManager(ctrl)
			tt.setupMock(mockStorageManager)

			cfg := &config.Config{
				RegistryName: "test-registry",
			}

			provider := NewFileRegistryDataProvider(mockStorageManager, cfg)
			registry, err := provider.GetRegistryData(context.Background())

			if tt.expectedErr {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
				assert.Nil(t, registry)
			} else {
				assert.NoError(t, err)
				require.NotNil(t, registry)
				assert.Len(t, registry.Data.Servers, tt.expectedCount)
			}
		})
	}
}

func TestFileRegistryDataProvider_GetRegistryData_AppliesPrefixesDuringMerge(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		setupMock        func(*sourcesmocks.MockStorageManager)
		expectedPrefixes map[string]bool // map of expected prefixed server names
	}{
		{
			name: "two registries with distinct servers",
			setupMock: func(m *sourcesmocks.MockStorageManager) {
				allRegs := map[string]*toolhivetypes.UpstreamRegistry{
					"registry-a": registry.NewTestUpstreamRegistry(
						registry.WithServers(
							registry.NewTestServer("server-1"),
						),
					),
					"registry-b": registry.NewTestUpstreamRegistry(
						registry.WithServers(
							registry.NewTestServer("server-2"),
						),
					),
				}
				m.EXPECT().
					GetAll(gomock.Any()).
					Return(allRegs, nil)
			},
			expectedPrefixes: map[string]bool{
				"registry-a.server-1": true,
				"registry-b.server-2": true,
			},
		},
		{
			name: "three registries with distinct servers",
			setupMock: func(m *sourcesmocks.MockStorageManager) {
				allRegs := map[string]*toolhivetypes.UpstreamRegistry{
					"partner-a": registry.NewTestUpstreamRegistry(
						registry.WithServers(
							registry.NewTestServer("github-mcp"),
						),
					),
					"partner-b": registry.NewTestUpstreamRegistry(
						registry.WithServers(
							registry.NewTestServer("slack-mcp"),
						),
					),
					"internal": registry.NewTestUpstreamRegistry(
						registry.WithServers(
							registry.NewTestServer("custom-mcp"),
						),
					),
				}
				m.EXPECT().
					GetAll(gomock.Any()).
					Return(allRegs, nil)
			},
			expectedPrefixes: map[string]bool{
				"partner-a.github-mcp": true,
				"partner-b.slack-mcp":  true,
				"internal.custom-mcp":  true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockStorageManager := sourcesmocks.NewMockStorageManager(ctrl)
			tt.setupMock(mockStorageManager)

			cfg := &config.Config{
				RegistryName: "test-registry",
			}

			provider := NewFileRegistryDataProvider(mockStorageManager, cfg)
			result, err := provider.GetRegistryData(context.Background())

			require.NoError(t, err)
			require.NotNil(t, result)
			assert.Len(t, result.Data.Servers, len(tt.expectedPrefixes))

			// Verify each server name has the correct prefix format
			actualNames := make(map[string]bool)
			for _, server := range result.Data.Servers {
				actualNames[server.Name] = true
			}
			assert.Equal(t, tt.expectedPrefixes, actualNames)
		})
	}
}

func TestFileRegistryDataProvider_GetRegistryData_PrefixesWithMultipleServers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		setupMock        func(*sourcesmocks.MockStorageManager)
		expectedPrefixes map[string]bool
	}{
		{
			name: "multiple servers per registry all get prefixed",
			setupMock: func(m *sourcesmocks.MockStorageManager) {
				allRegs := map[string]*toolhivetypes.UpstreamRegistry{
					"registry-1": registry.NewTestUpstreamRegistry(
						registry.WithServers(
							registry.NewTestServer("server-a"),
							registry.NewTestServer("server-b"),
							registry.NewTestServer("server-c"),
						),
					),
					"registry-2": registry.NewTestUpstreamRegistry(
						registry.WithServers(
							registry.NewTestServer("server-x"),
							registry.NewTestServer("server-y"),
						),
					),
				}
				m.EXPECT().
					GetAll(gomock.Any()).
					Return(allRegs, nil)
			},
			expectedPrefixes: map[string]bool{
				"registry-1.server-a": true,
				"registry-1.server-b": true,
				"registry-1.server-c": true,
				"registry-2.server-x": true,
				"registry-2.server-y": true,
			},
		},
		{
			name: "registry with many servers",
			setupMock: func(m *sourcesmocks.MockStorageManager) {
				allRegs := map[string]*toolhivetypes.UpstreamRegistry{
					"large-registry": registry.NewTestUpstreamRegistry(
						registry.WithServers(
							registry.NewTestServer("mcp-1"),
							registry.NewTestServer("mcp-2"),
							registry.NewTestServer("mcp-3"),
							registry.NewTestServer("mcp-4"),
							registry.NewTestServer("mcp-5"),
						),
					),
				}
				m.EXPECT().
					GetAll(gomock.Any()).
					Return(allRegs, nil)
			},
			expectedPrefixes: map[string]bool{
				"large-registry.mcp-1": true,
				"large-registry.mcp-2": true,
				"large-registry.mcp-3": true,
				"large-registry.mcp-4": true,
				"large-registry.mcp-5": true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockStorageManager := sourcesmocks.NewMockStorageManager(ctrl)
			tt.setupMock(mockStorageManager)

			cfg := &config.Config{
				RegistryName: "test-registry",
			}

			provider := NewFileRegistryDataProvider(mockStorageManager, cfg)
			result, err := provider.GetRegistryData(context.Background())

			require.NoError(t, err)
			require.NotNil(t, result)
			assert.Len(t, result.Data.Servers, len(tt.expectedPrefixes))

			// Verify all server names match expected prefixed names
			actualNames := make(map[string]bool)
			for _, server := range result.Data.Servers {
				actualNames[server.Name] = true
				// Verify prefix format matches PrefixServerName function
				assert.Contains(t, server.Name, ".")
			}
			assert.Equal(t, tt.expectedPrefixes, actualNames)
		})
	}
}

func TestFileRegistryDataProvider_GetRegistryData_PrefixesServerWithSameNameInDifferentRegistries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		setupMock        func(*sourcesmocks.MockStorageManager)
		expectedPrefixes map[string]bool
		description      string
	}{
		{
			name:        "same server name in two registries",
			description: "Key use case: github-server exists in both registry-a and registry-b",
			setupMock: func(m *sourcesmocks.MockStorageManager) {
				allRegs := map[string]*toolhivetypes.UpstreamRegistry{
					"registry-a": registry.NewTestUpstreamRegistry(
						registry.WithServers(
							registry.NewTestServer("github-server"),
						),
					),
					"registry-b": registry.NewTestUpstreamRegistry(
						registry.WithServers(
							registry.NewTestServer("github-server"),
						),
					),
				}
				m.EXPECT().
					GetAll(gomock.Any()).
					Return(allRegs, nil)
			},
			expectedPrefixes: map[string]bool{
				"registry-a.github-server": true,
				"registry-b.github-server": true,
			},
		},
		{
			name:        "same server name in three registries",
			description: "Duplicate server name across multiple registries",
			setupMock: func(m *sourcesmocks.MockStorageManager) {
				allRegs := map[string]*toolhivetypes.UpstreamRegistry{
					"partner-a": registry.NewTestUpstreamRegistry(
						registry.WithServers(
							registry.NewTestServer("common-mcp"),
						),
					),
					"partner-b": registry.NewTestUpstreamRegistry(
						registry.WithServers(
							registry.NewTestServer("common-mcp"),
						),
					),
					"internal": registry.NewTestUpstreamRegistry(
						registry.WithServers(
							registry.NewTestServer("common-mcp"),
						),
					),
				}
				m.EXPECT().
					GetAll(gomock.Any()).
					Return(allRegs, nil)
			},
			expectedPrefixes: map[string]bool{
				"partner-a.common-mcp": true,
				"partner-b.common-mcp": true,
				"internal.common-mcp":  true,
			},
		},
		{
			name:        "mixed unique and duplicate server names",
			description: "Some servers are unique, some are duplicated across registries",
			setupMock: func(m *sourcesmocks.MockStorageManager) {
				allRegs := map[string]*toolhivetypes.UpstreamRegistry{
					"registry-1": registry.NewTestUpstreamRegistry(
						registry.WithServers(
							registry.NewTestServer("shared-server"),
							registry.NewTestServer("unique-server-1"),
						),
					),
					"registry-2": registry.NewTestUpstreamRegistry(
						registry.WithServers(
							registry.NewTestServer("shared-server"),
							registry.NewTestServer("unique-server-2"),
						),
					),
				}
				m.EXPECT().
					GetAll(gomock.Any()).
					Return(allRegs, nil)
			},
			expectedPrefixes: map[string]bool{
				"registry-1.shared-server":   true,
				"registry-1.unique-server-1": true,
				"registry-2.shared-server":   true,
				"registry-2.unique-server-2": true,
			},
		},
		{
			name:        "namespaced server names that are duplicated",
			description: "Server names with namespaces (io.github.user/server) duplicated across registries",
			setupMock: func(m *sourcesmocks.MockStorageManager) {
				allRegs := map[string]*toolhivetypes.UpstreamRegistry{
					"upstream": registry.NewTestUpstreamRegistry(
						registry.WithServers(
							registry.NewTestServer("io.github.user/mcp-server"),
						),
					),
					"fork": registry.NewTestUpstreamRegistry(
						registry.WithServers(
							registry.NewTestServer("io.github.user/mcp-server"),
						),
					),
				}
				m.EXPECT().
					GetAll(gomock.Any()).
					Return(allRegs, nil)
			},
			expectedPrefixes: map[string]bool{
				"upstream.io.github.user/mcp-server": true,
				"fork.io.github.user/mcp-server":     true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockStorageManager := sourcesmocks.NewMockStorageManager(ctrl)
			tt.setupMock(mockStorageManager)

			cfg := &config.Config{
				RegistryName: "test-registry",
			}

			provider := NewFileRegistryDataProvider(mockStorageManager, cfg)
			result, err := provider.GetRegistryData(context.Background())

			require.NoError(t, err)
			require.NotNil(t, result)
			assert.Len(t, result.Data.Servers, len(tt.expectedPrefixes),
				"Expected %d servers but got %d", len(tt.expectedPrefixes), len(result.Data.Servers))

			// Verify all servers have distinct prefixed names
			actualNames := make(map[string]bool)
			for _, server := range result.Data.Servers {
				// Ensure no duplicate names in result (prefixing should make them unique)
				assert.False(t, actualNames[server.Name],
					"Duplicate server name found: %s - prefixing should make names unique", server.Name)
				actualNames[server.Name] = true
			}
			assert.Equal(t, tt.expectedPrefixes, actualNames)
		})
	}
}

func TestFileRegistryDataProvider_GetRegistryData_SingleRegistryStillGetsPrefixed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		registryName     string
		serverNames      []string
		expectedPrefixes map[string]bool
	}{
		{
			name:         "single registry single server",
			registryName: "my-registry",
			serverNames:  []string{"my-server"},
			expectedPrefixes: map[string]bool{
				"my-registry.my-server": true,
			},
		},
		{
			name:         "single registry multiple servers",
			registryName: "production",
			serverNames:  []string{"github-mcp", "slack-mcp", "jira-mcp"},
			expectedPrefixes: map[string]bool{
				"production.github-mcp": true,
				"production.slack-mcp":  true,
				"production.jira-mcp":   true,
			},
		},
		{
			name:         "single registry with namespaced server",
			registryName: "partner",
			serverNames:  []string{"io.github.user/custom-server"},
			expectedPrefixes: map[string]bool{
				"partner.io.github.user/custom-server": true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			// Build servers from server names
			servers := make([]upstreamv0.ServerJSON, len(tt.serverNames))
			for i, name := range tt.serverNames {
				servers[i] = registry.NewTestServer(name)
			}

			mockStorageManager := sourcesmocks.NewMockStorageManager(ctrl)
			allRegs := map[string]*toolhivetypes.UpstreamRegistry{
				tt.registryName: registry.NewTestUpstreamRegistry(
					registry.WithServers(servers...),
				),
			}
			mockStorageManager.EXPECT().
				GetAll(gomock.Any()).
				Return(allRegs, nil)

			cfg := &config.Config{
				RegistryName: "test-registry",
			}

			provider := NewFileRegistryDataProvider(mockStorageManager, cfg)
			result, err := provider.GetRegistryData(context.Background())

			require.NoError(t, err)
			require.NotNil(t, result)
			assert.Len(t, result.Data.Servers, len(tt.expectedPrefixes))

			// Verify all servers are prefixed even with single registry
			actualNames := make(map[string]bool)
			for _, server := range result.Data.Servers {
				actualNames[server.Name] = true
				// Verify the prefix format matches PrefixServerName behavior
				assert.True(t, len(server.Name) > len(tt.registryName)+1,
					"Server name should be longer than registry name + dot")
				assert.Equal(t, tt.registryName+".", server.Name[:len(tt.registryName)+1],
					"Server name should start with registry name prefix")
			}
			assert.Equal(t, tt.expectedPrefixes, actualNames)
		})
	}
}

func TestFileRegistryDataProvider_GetRegistryData_EmptyRegistryNoPrefixes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		setupMock     func(*sourcesmocks.MockStorageManager)
		expectedCount int
	}{
		{
			name: "empty map of registries",
			setupMock: func(m *sourcesmocks.MockStorageManager) {
				allRegs := map[string]*toolhivetypes.UpstreamRegistry{}
				m.EXPECT().
					GetAll(gomock.Any()).
					Return(allRegs, nil)
			},
			expectedCount: 0,
		},
		{
			name: "single registry with no servers",
			setupMock: func(m *sourcesmocks.MockStorageManager) {
				allRegs := map[string]*toolhivetypes.UpstreamRegistry{
					"empty-registry": registry.NewTestUpstreamRegistry(),
				}
				m.EXPECT().
					GetAll(gomock.Any()).
					Return(allRegs, nil)
			},
			expectedCount: 0,
		},
		{
			name: "multiple registries all empty",
			setupMock: func(m *sourcesmocks.MockStorageManager) {
				allRegs := map[string]*toolhivetypes.UpstreamRegistry{
					"registry-1": registry.NewTestUpstreamRegistry(),
					"registry-2": registry.NewTestUpstreamRegistry(),
					"registry-3": registry.NewTestUpstreamRegistry(),
				}
				m.EXPECT().
					GetAll(gomock.Any()).
					Return(allRegs, nil)
			},
			expectedCount: 0,
		},
		{
			name: "nil registries in map",
			setupMock: func(m *sourcesmocks.MockStorageManager) {
				allRegs := map[string]*toolhivetypes.UpstreamRegistry{
					"registry-1": nil,
					"registry-2": nil,
				}
				m.EXPECT().
					GetAll(gomock.Any()).
					Return(allRegs, nil)
			},
			expectedCount: 0,
		},
		{
			name: "mix of nil and empty registries",
			setupMock: func(m *sourcesmocks.MockStorageManager) {
				allRegs := map[string]*toolhivetypes.UpstreamRegistry{
					"nil-registry":   nil,
					"empty-registry": registry.NewTestUpstreamRegistry(),
				}
				m.EXPECT().
					GetAll(gomock.Any()).
					Return(allRegs, nil)
			},
			expectedCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockStorageManager := sourcesmocks.NewMockStorageManager(ctrl)
			tt.setupMock(mockStorageManager)

			cfg := &config.Config{
				RegistryName: "test-registry",
			}

			provider := NewFileRegistryDataProvider(mockStorageManager, cfg)
			result, err := provider.GetRegistryData(context.Background())

			require.NoError(t, err)
			require.NotNil(t, result)
			assert.Len(t, result.Data.Servers, tt.expectedCount)
			// Verify no servers means no prefixed names to check
			assert.Empty(t, result.Data.Servers)
		})
	}
}

func TestFileRegistryDataProvider_GetSource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		config         *config.Config
		expectedSource string
	}{
		{
			name: "no registries configured",
			config: &config.Config{
				RegistryName: "test-registry",
				Registries:   []config.RegistryConfig{},
			},
			expectedSource: "multi-registry:<not-configured>",
		},
		{
			name: "single file registry",
			config: &config.Config{
				RegistryName: "test-registry",
				Registries: []config.RegistryConfig{
					{
						Name: "registry-1",
						File: &config.FileConfig{Path: "/path/to/file.json"},
					},
				},
			},
			expectedSource: "file:/path/to/file.json",
		},
		{
			name: "single git registry",
			config: &config.Config{
				RegistryName: "test-registry",
				Registries: []config.RegistryConfig{
					{
						Name: "registry-1",
						Git:  &config.GitConfig{Repository: "https://github.com/test/repo.git"},
					},
				},
			},
			expectedSource: "git:https://github.com/test/repo.git",
		},
		{
			name: "single api registry",
			config: &config.Config{
				RegistryName: "test-registry",
				Registries: []config.RegistryConfig{
					{
						Name: "registry-1",
						API:  &config.APIConfig{Endpoint: "https://api.example.com"},
					},
				},
			},
			expectedSource: "api:https://api.example.com",
		},
		{
			name: "multiple registries",
			config: &config.Config{
				RegistryName: "test-registry",
				Registries: []config.RegistryConfig{
					{
						Name: "registry-1",
						File: &config.FileConfig{Path: "/path/to/file1.json"},
					},
					{
						Name: "registry-2",
						Git:  &config.GitConfig{Repository: "https://github.com/test/repo.git"},
					},
				},
			},
			expectedSource: "multi-registry:2-sources",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockStorageManager := sourcesmocks.NewMockStorageManager(ctrl)
			provider := NewFileRegistryDataProvider(mockStorageManager, tt.config)

			source := provider.GetSource()
			assert.Equal(t, tt.expectedSource, source)
		})
	}
}
