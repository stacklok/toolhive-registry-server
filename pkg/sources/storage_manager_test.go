package sources

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/stacklok/toolhive/pkg/registry"
)

func TestFileStorageManager_StoreAndGet(t *testing.T) {
	t.Parallel()

	// Create temporary directory for test
	tmpDir := t.TempDir()

	manager := NewFileStorageManager(tmpDir)
	require.NotNil(t, manager)

	// Create a test registry
	testRegistry := &registry.Registry{
		Version:     "1.0.0",
		LastUpdated: "2024-01-01T00:00:00Z",
		Servers:     make(map[string]*registry.ImageMetadata),
	}

	// Store the registry
	ctx := context.Background()
	err := manager.Store(ctx, nil, testRegistry)
	require.NoError(t, err)

	// Verify file was created
	filePath := filepath.Join(tmpDir, RegistryFileName)
	_, err = os.Stat(filePath)
	require.NoError(t, err)

	// Get the registry back
	retrieved, err := manager.Get(ctx, nil)
	require.NoError(t, err)
	require.NotNil(t, retrieved)
	require.Equal(t, testRegistry.Version, retrieved.Version)
	require.Equal(t, testRegistry.LastUpdated, retrieved.LastUpdated)
}

func TestFileStorageManager_Delete(t *testing.T) {
	t.Parallel()

	// Create temporary directory for test
	tmpDir := t.TempDir()

	manager := NewFileStorageManager(tmpDir)
	require.NotNil(t, manager)

	// Create and store a test registry
	testRegistry := &registry.Registry{
		Version: "1.0.0",
	}

	ctx := context.Background()
	err := manager.Store(ctx, nil, testRegistry)
	require.NoError(t, err)

	// Delete the registry
	err = manager.Delete(ctx, nil)
	require.NoError(t, err)

	// Verify file was deleted
	filePath := filepath.Join(tmpDir, RegistryFileName)
	_, err = os.Stat(filePath)
	require.True(t, os.IsNotExist(err))
}

func TestFileStorageManager_GetNonExistent(t *testing.T) {
	t.Parallel()

	// Create temporary directory for test
	tmpDir := t.TempDir()

	manager := NewFileStorageManager(tmpDir)
	require.NotNil(t, manager)

	// Try to get non-existent registry
	ctx := context.Background()
	_, err := manager.Get(ctx, nil)
	require.Error(t, err)
}
