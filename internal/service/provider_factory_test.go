package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/pkg/config"
	sourcesmocks "github.com/stacklok/toolhive-registry-server/pkg/sources/mocks"
)

func TestDefaultRegistryProviderFactory_CreateProvider(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		config      *config.Config
		wantErr     bool
		errContains string
		checkType   func(*testing.T, RegistryDataProvider)
	}{
		{
			name: "create file provider with valid config",
			config: &config.Config{
				RegistryName: "test-file-registry",
				Source: config.SourceConfig{
					Type:   config.SourceTypeFile,
					Format: config.SourceFormatToolHive,
					File:   &config.FileConfig{Path: "/data/registry.json"},
				},
				SyncPolicy: &config.SyncPolicyConfig{Interval: "30m"},
			},
			wantErr: false,
			checkType: func(t *testing.T, provider RegistryDataProvider) {
				t.Helper()
				assert.IsType(t, &FileRegistryDataProvider{}, provider)
				assert.Equal(t, "file:/data/registry.json", provider.GetSource())
				assert.Equal(t, "test-file-registry", provider.GetRegistryName())
			},
		},
		{
			name:        "nil config",
			config:      nil,
			wantErr:     true,
			errContains: "config cannot be nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockStorageManager := sourcesmocks.NewMockStorageManager(ctrl)
			factory := NewRegistryProviderFactory(mockStorageManager)

			provider, err := factory.CreateProvider(tt.config)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				assert.Nil(t, provider)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, provider)
				if tt.checkType != nil {
					tt.checkType(t, provider)
				}
			}
		})
	}
}

func TestNewRegistryProviderFactory(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockStorageManager := sourcesmocks.NewMockStorageManager(ctrl)
	factory := NewRegistryProviderFactory(mockStorageManager)

	require.NotNil(t, factory)
	assert.IsType(t, &DefaultRegistryProviderFactory{}, factory)

	// Verify that the factory has the storage manager injected
	concreteFactory, ok := factory.(*DefaultRegistryProviderFactory)
	require.True(t, ok)
	assert.NotNil(t, concreteFactory.storageManager)
}
