package sources

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/registry"
)

const testRegistryName = "test-registry"

func TestFileStorageManager_StoreAndGet(t *testing.T) {
	t.Parallel()

	// Create temporary directory for test
	tmpDir := t.TempDir()

	manager := NewFileStorageManager(tmpDir)
	require.NotNil(t, manager)

	// Create UpstreamRegistry directly
	UpstreamRegistry := registry.NewTestUpstreamRegistry(
		registry.WithVersion("1.0.0"),
		registry.WithLastUpdated("2024-01-01T00:00:00Z"),
	)

	// Store the registry
	ctx := context.Background()
	registryName := testRegistryName
	err := manager.Store(ctx, registryName, UpstreamRegistry)
	require.NoError(t, err)

	// Verify file was created in registry-specific directory
	filePath := filepath.Join(tmpDir, registryName, RegistryFileName)
	_, err = os.Stat(filePath)
	require.NoError(t, err)

	// Get the registry back
	retrieved, err := manager.Get(ctx, registryName)
	require.NoError(t, err)
	require.NotNil(t, retrieved)
	require.Equal(t, UpstreamRegistry.Version, retrieved.Version)
	require.Equal(t, UpstreamRegistry.LastUpdated, retrieved.LastUpdated)
}

func TestFileStorageManager_Delete(t *testing.T) {
	t.Parallel()

	// Create temporary directory for test
	tmpDir := t.TempDir()

	manager := NewFileStorageManager(tmpDir)
	require.NotNil(t, manager)

	// Create UpstreamRegistry directly
	UpstreamRegistry := registry.NewTestUpstreamRegistry(
		registry.WithVersion("1.0.0"),
	)

	ctx := context.Background()
	registryName := testRegistryName
	err := manager.Store(ctx, registryName, UpstreamRegistry)
	require.NoError(t, err)

	// Delete the registry
	err = manager.Delete(ctx, registryName)
	require.NoError(t, err)

	// Verify entire registry directory was deleted
	registryDir := filepath.Join(tmpDir, registryName)
	_, err = os.Stat(registryDir)
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
	registryName := "nonexistent-registry"
	_, err := manager.Get(ctx, registryName)
	require.Error(t, err)
}

func TestFileStorageManager_Delete_PermissionDenied(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	manager := NewFileStorageManager(tmpDir)

	// Create UpstreamRegistry directly
	UpstreamRegistry := registry.NewTestUpstreamRegistry(
		registry.WithVersion("1.0.0"),
	)

	ctx := context.Background()
	registryName := testRegistryName
	err := manager.Store(ctx, registryName, UpstreamRegistry)
	require.NoError(t, err)

	// Make directory read-only to prevent deletion
	err = os.Chmod(tmpDir, 0555) // Read + execute only, no write
	require.NoError(t, err)

	// Try to delete - should fail with permission error
	err = manager.Delete(ctx, registryName)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to delete registry directory")

	_ = os.Chmod(tmpDir, 0755) // Restore permissions
}

func TestFileStorageManager_Delete_NonExistent(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	manager := NewFileStorageManager(tmpDir)

	// Try to delete registry that doesn't exist
	ctx := context.Background()
	registryName := "nonexistent-registry"
	err := manager.Delete(ctx, registryName)

	// Should succeed (idempotent operation)
	require.NoError(t, err)
}
