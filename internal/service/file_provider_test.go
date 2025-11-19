package service

import (
	"context"
	"errors"
	"testing"

	toolhivetypes "github.com/stacklok/toolhive/pkg/registry/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/registry"
	sourcesmocks "github.com/stacklok/toolhive-registry-server/internal/sources/mocks"
)

func TestFileRegistryDataProvider_GetRegistryData(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		name        string
		setupMock   func(*sourcesmocks.MockStorageManager)
		wantErr     bool
		errContains string
		validate    func(*testing.T, *toolhivetypes.UpstreamRegistry)
	}{
		{
			name: "successful retrieval",
			setupMock: func(m *sourcesmocks.MockStorageManager) {
				expectedRegistry := registry.NewTestUpstreamRegistry(
					registry.WithVersion("1.0"),
					registry.WithLastUpdated("2024-01-01T00:00:00Z"),
					registry.WithServers(
						registry.NewTestServer("test-server",
							registry.WithDescription("A test server"),
							registry.WithOCIPackage("test:latest"),
						),
					),
				)
				m.EXPECT().
					Get(gomock.Any(), gomock.Any()).
					Return(expectedRegistry, nil)
			},
			wantErr: false,
			validate: func(t *testing.T, reg *toolhivetypes.UpstreamRegistry) {
				t.Helper()
				assert.Equal(t, "1.0", reg.Version)
				assert.Equal(t, "2024-01-01T00:00:00Z", reg.LastUpdated)
				assert.Len(t, reg.Servers, 1)
				assert.Equal(t, "test-server", reg.Servers[0].Name)
			},
		},
		{
			name: "empty registry",
			setupMock: func(m *sourcesmocks.MockStorageManager) {
				expectedRegistry := registry.NewTestUpstreamRegistry(
					registry.WithVersion("1.0"),
					registry.WithLastUpdated("2024-01-01T00:00:00Z"),
				)
				m.EXPECT().
					Get(gomock.Any(), gomock.Any()).
					Return(expectedRegistry, nil)
			},
			wantErr: false,
			validate: func(t *testing.T, reg *toolhivetypes.UpstreamRegistry) {
				t.Helper()
				assert.Equal(t, "1.0", reg.Version)
				assert.Len(t, reg.Servers, 0)
			},
		},
		{
			name: "storage manager returns error",
			setupMock: func(m *sourcesmocks.MockStorageManager) {
				m.EXPECT().
					Get(gomock.Any(), gomock.Any()).
					Return(nil, errors.New("registry file not found"))
			},
			wantErr:     true,
			errContains: "registry file not found",
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
				Source: config.SourceConfig{
					Type:   config.SourceTypeFile,
					Format: config.SourceFormatToolHive,
					File:   &config.FileConfig{Path: "/tmp/registry.json"},
				},
			}

			provider := NewFileRegistryDataProvider(mockStorageManager, cfg)

			result, err := provider.GetRegistryData(ctx)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				if tt.validate != nil {
					tt.validate(t, result)
				}
			}
		})
	}
}

func TestFileRegistryDataProvider_GetSource(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	mockStorageManager := sourcesmocks.NewMockStorageManager(ctrl)

	tests := []struct {
		name     string
		config   *config.Config
		expected string
	}{
		{
			name: "absolute path",
			config: &config.Config{
				RegistryName: "test-registry",
				Source: config.SourceConfig{
					Type:   config.SourceTypeFile,
					Format: config.SourceFormatToolHive,
					File:   &config.FileConfig{Path: "/data/registry/registry.json"},
				},
			},
			expected: "file:/data/registry/registry.json",
		},
		{
			name: "relative path",
			config: &config.Config{
				RegistryName: "test-registry",
				Source: config.SourceConfig{
					Type:   config.SourceTypeFile,
					Format: config.SourceFormatToolHive,
					File:   &config.FileConfig{Path: "./registry.json"},
				},
			},
			expected: "file:./registry.json",
		},
		{
			name: "empty path",
			config: &config.Config{
				RegistryName: "test-registry",
				Source: config.SourceConfig{
					Type:   config.SourceTypeFile,
					Format: config.SourceFormatToolHive,
					File:   &config.FileConfig{Path: ""},
				},
			},
			expected: "file:<not-configured>",
		},
		{
			name: "nil file config",
			config: &config.Config{
				RegistryName: "test-registry",
				Source: config.SourceConfig{
					Type:   config.SourceTypeFile,
					Format: config.SourceFormatToolHive,
					File:   nil,
				},
			},
			expected: "file:<not-configured>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			provider := NewFileRegistryDataProvider(mockStorageManager, tt.config)
			result := provider.GetSource()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNewFileRegistryDataProvider(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockStorageManager := sourcesmocks.NewMockStorageManager(ctrl)

	cfg := &config.Config{
		RegistryName: "test-registry",
		Source: config.SourceConfig{
			Type:   config.SourceTypeFile,
			Format: config.SourceFormatToolHive,
			File:   &config.FileConfig{Path: "/test/path/registry.json"},
		},
	}

	provider := NewFileRegistryDataProvider(mockStorageManager, cfg)

	require.NotNil(t, provider)
	assert.Equal(t, "file:/test/path/registry.json", provider.GetSource())
	assert.Equal(t, "test-registry", provider.GetRegistryName())
}

func TestFileRegistryDataProvider_GetRegistryName(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	mockStorageManager := sourcesmocks.NewMockStorageManager(ctrl)

	tests := []struct {
		name         string
		registryName string
	}{
		{
			name:         "normal registry name",
			registryName: "my-custom-registry",
		},
		{
			name:         "production registry",
			registryName: "production-registry",
		},
		{
			name:         "empty registry name defaults to default",
			registryName: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := &config.Config{
				RegistryName: tt.registryName,
				Source: config.SourceConfig{
					Type:   config.SourceTypeFile,
					Format: config.SourceFormatToolHive,
					File:   &config.FileConfig{Path: "/path/to/file.json"},
				},
			}

			provider := NewFileRegistryDataProvider(mockStorageManager, cfg)
			result := provider.GetRegistryName()

			// Config.GetRegistryName() returns "default" for empty names
			expectedName := tt.registryName
			if expectedName == "" {
				expectedName = "default"
			}
			assert.Equal(t, expectedName, result)
		})
	}
}
