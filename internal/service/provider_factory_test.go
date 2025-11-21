package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	sourcesmocks "github.com/stacklok/toolhive-registry-server/internal/sources/mocks"
)

func TestNewRegistryProviderFactory(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockStorageManager := sourcesmocks.NewMockStorageManager(ctrl)
	factory := NewRegistryProviderFactory(mockStorageManager)

	require.NotNil(t, factory)

	// Verify that the factory has the storage manager injected
	concreteFactory, ok := factory.(*defaultRegistryProviderFactory)
	require.True(t, ok)
	assert.NotNil(t, concreteFactory.storageManager)
}

// TODO: Update provider factory tests for multi-registry architecture
// The tests need to be updated to use the new Config structure with Registries array
