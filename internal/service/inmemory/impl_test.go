// Package inmemory provides an in-memory implementation of the RegistryService interface
package inmemory

import (
	"context"
	"testing"
	"time"

	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	toolhivetypes "github.com/stacklok/toolhive/pkg/registry/registry"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/service"
	"github.com/stacklok/toolhive-registry-server/internal/service/inmemory/mocks"
)

func TestValidateManagedRegistry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		registryName  string
		config        *config.Config
		expectedError error
		errorContains string
	}{
		{
			name:         "success - managed registry",
			registryName: "managed-registry",
			config: &config.Config{
				Registries: []config.RegistryConfig{
					{
						Name:    "managed-registry",
						Managed: &config.ManagedConfig{},
					},
				},
			},
			expectedError: nil,
		},
		{
			name:         "failure - file registry returns ErrNotManagedRegistry",
			registryName: "file-registry",
			config: &config.Config{
				Registries: []config.RegistryConfig{
					{
						Name: "file-registry",
						File: &config.FileConfig{Path: "/path/to/file"},
					},
				},
			},
			expectedError: service.ErrNotManagedRegistry,
			errorContains: "file-registry has type file",
		},
		{
			name:         "failure - git registry returns ErrNotManagedRegistry",
			registryName: "git-registry",
			config: &config.Config{
				Registries: []config.RegistryConfig{
					{
						Name: "git-registry",
						Git:  &config.GitConfig{Repository: "https://github.com/example/repo"},
					},
				},
			},
			expectedError: service.ErrNotManagedRegistry,
			errorContains: "git-registry has type git",
		},
		{
			name:         "failure - api registry returns ErrNotManagedRegistry",
			registryName: "api-registry",
			config: &config.Config{
				Registries: []config.RegistryConfig{
					{
						Name: "api-registry",
						API:  &config.APIConfig{Endpoint: "https://example.com/api"},
					},
				},
			},
			expectedError: service.ErrNotManagedRegistry,
			errorContains: "api-registry has type api",
		},
		{
			name:         "failure - registry not found",
			registryName: "nonexistent-registry",
			config: &config.Config{
				Registries: []config.RegistryConfig{
					{
						Name:    "other-registry",
						Managed: &config.ManagedConfig{},
					},
				},
			},
			expectedError: service.ErrRegistryNotFound,
			errorContains: "nonexistent-registry",
		},
		{
			name:          "failure - nil config",
			registryName:  "any-registry",
			config:        nil,
			expectedError: nil, // Custom error, not a sentinel
			errorContains: "config not available for registry validation",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create a regSvc with the test config
			svc := &regSvc{
				config:       tt.config,
				registryData: make(map[string]*toolhivetypes.UpstreamRegistry),
				lastFetch:    make(map[string]time.Time),
			}

			// Call validateManagedRegistry
			result, err := svc.validateManagedRegistry(tt.registryName)

			if tt.expectedError != nil {
				require.Error(t, err)
				require.ErrorIs(t, err, tt.expectedError)
				require.Nil(t, result)
			} else if tt.errorContains != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errorContains)
				require.Nil(t, result)
			} else {
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Equal(t, tt.registryName, result.Name)
			}
		})
	}
}

func TestPublishServerVersion_ManagedRegistryValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		registryName  string
		config        *config.Config
		serverData    *upstreamv0.ServerJSON
		expectedError error
		errorContains string
	}{
		{
			name:         "failure - file registry returns ErrNotManagedRegistry",
			registryName: "file-registry",
			config: &config.Config{
				Registries: []config.RegistryConfig{
					{
						Name: "file-registry",
						File: &config.FileConfig{Path: "/path/to/file"},
						SyncPolicy: &config.SyncPolicyConfig{
							Interval: "1h",
						},
					},
				},
			},
			serverData: &upstreamv0.ServerJSON{
				Name:        "test-server",
				Version:     "1.0.0",
				Description: "Test server",
			},
			expectedError: service.ErrNotManagedRegistry,
		},
		{
			name:         "failure - git registry returns ErrNotManagedRegistry",
			registryName: "git-registry",
			config: &config.Config{
				Registries: []config.RegistryConfig{
					{
						Name: "git-registry",
						Git:  &config.GitConfig{Repository: "https://github.com/example/repo"},
						SyncPolicy: &config.SyncPolicyConfig{
							Interval: "1h",
						},
					},
				},
			},
			serverData: &upstreamv0.ServerJSON{
				Name:        "test-server",
				Version:     "1.0.0",
				Description: "Test server",
			},
			expectedError: service.ErrNotManagedRegistry,
		},
		{
			name:         "failure - registry not found",
			registryName: "nonexistent-registry",
			config: &config.Config{
				Registries: []config.RegistryConfig{
					{
						Name:    "other-registry",
						Managed: &config.ManagedConfig{},
					},
				},
			},
			serverData: &upstreamv0.ServerJSON{
				Name:        "test-server",
				Version:     "1.0.0",
				Description: "Test server",
			},
			expectedError: service.ErrRegistryNotFound,
		},
		{
			name:         "success - managed registry allows publish",
			registryName: "managed-registry",
			config: &config.Config{
				Registries: []config.RegistryConfig{
					{
						Name:    "managed-registry",
						Managed: &config.ManagedConfig{},
					},
				},
			},
			serverData: &upstreamv0.ServerJSON{
				Name:        "test-server",
				Version:     "1.0.0",
				Description: "Test server",
			},
			expectedError: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockProvider := mocks.NewMockRegistryDataProvider(ctrl)

			// Set up mock expectations
			mockProvider.EXPECT().GetRegistryData(gomock.Any()).Return(&toolhivetypes.UpstreamRegistry{
				Data: toolhivetypes.UpstreamData{
					Servers: []upstreamv0.ServerJSON{},
				},
			}, nil).AnyTimes()
			mockProvider.EXPECT().GetSource().Return("file:/path/to/file").AnyTimes()
			mockProvider.EXPECT().GetRegistryName().Return(tt.registryName).AnyTimes()

			// Create the service with config
			svc, err := New(context.Background(), mockProvider, WithConfig(tt.config))
			require.NoError(t, err)

			// Call PublishServerVersion
			result, err := svc.PublishServerVersion(
				context.Background(),
				service.WithRegistryName[service.PublishServerVersionOptions](tt.registryName),
				service.WithServerData(tt.serverData),
			)

			if tt.expectedError != nil {
				require.Error(t, err)
				require.ErrorIs(t, err, tt.expectedError)
				require.Nil(t, result)
			} else {
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Equal(t, tt.serverData.Name, result.Name)
				require.Equal(t, tt.serverData.Version, result.Version)
			}
		})
	}
}

func TestDeleteServerVersion_ManagedRegistryValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		registryName  string
		config        *config.Config
		serverName    string
		version       string
		expectedError error
	}{
		{
			name:         "failure - file registry returns ErrNotManagedRegistry",
			registryName: "file-registry",
			config: &config.Config{
				Registries: []config.RegistryConfig{
					{
						Name: "file-registry",
						File: &config.FileConfig{Path: "/path/to/file"},
						SyncPolicy: &config.SyncPolicyConfig{
							Interval: "1h",
						},
					},
				},
			},
			serverName:    "test-server",
			version:       "1.0.0",
			expectedError: service.ErrNotManagedRegistry,
		},
		{
			name:         "failure - git registry returns ErrNotManagedRegistry",
			registryName: "git-registry",
			config: &config.Config{
				Registries: []config.RegistryConfig{
					{
						Name: "git-registry",
						Git:  &config.GitConfig{Repository: "https://github.com/example/repo"},
						SyncPolicy: &config.SyncPolicyConfig{
							Interval: "1h",
						},
					},
				},
			},
			serverName:    "test-server",
			version:       "1.0.0",
			expectedError: service.ErrNotManagedRegistry,
		},
		{
			name:         "failure - api registry returns ErrNotManagedRegistry",
			registryName: "api-registry",
			config: &config.Config{
				Registries: []config.RegistryConfig{
					{
						Name: "api-registry",
						API:  &config.APIConfig{Endpoint: "https://example.com/api"},
						SyncPolicy: &config.SyncPolicyConfig{
							Interval: "1h",
						},
					},
				},
			},
			serverName:    "test-server",
			version:       "1.0.0",
			expectedError: service.ErrNotManagedRegistry,
		},
		{
			name:         "failure - registry not found",
			registryName: "nonexistent-registry",
			config: &config.Config{
				Registries: []config.RegistryConfig{
					{
						Name:    "other-registry",
						Managed: &config.ManagedConfig{},
					},
				},
			},
			serverName:    "test-server",
			version:       "1.0.0",
			expectedError: service.ErrRegistryNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockProvider := mocks.NewMockRegistryDataProvider(ctrl)

			// Set up mock expectations
			mockProvider.EXPECT().GetRegistryData(gomock.Any()).Return(&toolhivetypes.UpstreamRegistry{
				Data: toolhivetypes.UpstreamData{
					Servers: []upstreamv0.ServerJSON{
						{
							Name:    tt.serverName,
							Version: tt.version,
						},
					},
				},
			}, nil).AnyTimes()
			mockProvider.EXPECT().GetSource().Return("file:/path/to/file").AnyTimes()
			mockProvider.EXPECT().GetRegistryName().Return(tt.registryName).AnyTimes()

			// Create the service with config
			svc, err := New(context.Background(), mockProvider, WithConfig(tt.config))
			require.NoError(t, err)

			// Call DeleteServerVersion
			err = svc.DeleteServerVersion(
				context.Background(),
				service.WithRegistryName[service.DeleteServerVersionOptions](tt.registryName),
				service.WithName[service.DeleteServerVersionOptions](tt.serverName),
				service.WithVersion[service.DeleteServerVersionOptions](tt.version),
			)

			if tt.expectedError != nil {
				require.Error(t, err)
				require.ErrorIs(t, err, tt.expectedError)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestDeleteServerVersion_ManagedRegistrySuccess(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	registryName := "managed-registry"
	serverName := "test-server"
	version := "1.0.0"

	mockProvider := mocks.NewMockRegistryDataProvider(ctrl)

	// Set up mock expectations - start with empty registry (managed registries start empty)
	mockProvider.EXPECT().GetRegistryData(gomock.Any()).Return(&toolhivetypes.UpstreamRegistry{
		Data: toolhivetypes.UpstreamData{
			Servers: []upstreamv0.ServerJSON{},
		},
	}, nil).AnyTimes()
	mockProvider.EXPECT().GetSource().Return("managed:").AnyTimes()
	mockProvider.EXPECT().GetRegistryName().Return(registryName).AnyTimes()

	cfg := &config.Config{
		Registries: []config.RegistryConfig{
			{
				Name:    registryName,
				Managed: &config.ManagedConfig{},
			},
		},
	}

	// Create the service with config
	svc, err := New(context.Background(), mockProvider, WithConfig(cfg))
	require.NoError(t, err)

	// First, publish the server version so we can delete it
	_, err = svc.PublishServerVersion(
		context.Background(),
		service.WithRegistryName[service.PublishServerVersionOptions](registryName),
		service.WithServerData(&upstreamv0.ServerJSON{
			Name:    serverName,
			Version: version,
		}),
	)
	require.NoError(t, err)

	// Call DeleteServerVersion
	err = svc.DeleteServerVersion(
		context.Background(),
		service.WithRegistryName[service.DeleteServerVersionOptions](registryName),
		service.WithName[service.DeleteServerVersionOptions](serverName),
		service.WithVersion[service.DeleteServerVersionOptions](version),
	)

	require.NoError(t, err)
}
