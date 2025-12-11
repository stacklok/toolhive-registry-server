package inmemory

import (
	"context"
	"testing"

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
