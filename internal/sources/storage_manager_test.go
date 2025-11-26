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
	require.Equal(t, UpstreamRegistry.Meta.LastUpdated, retrieved.Meta.LastUpdated)
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

func TestFileStorageManager_GetAll(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	manager := NewFileStorageManager(tmpDir)

	ctx := context.Background()

	// Create multiple test registries
	registry1 := registry.NewTestUpstreamRegistry(
		registry.WithVersion("1.0.0"),
		registry.WithLastUpdated("2024-01-01T00:00:00Z"),
	)
	registry2 := registry.NewTestUpstreamRegistry(
		registry.WithVersion("2.0.0"),
		registry.WithLastUpdated("2024-01-02T00:00:00Z"),
	)
	registry3 := registry.NewTestUpstreamRegistry(
		registry.WithVersion("3.0.0"),
		registry.WithLastUpdated("2024-01-03T00:00:00Z"),
	)

	// Store registries
	err := manager.Store(ctx, "registry1", registry1)
	require.NoError(t, err)
	err = manager.Store(ctx, "registry2", registry2)
	require.NoError(t, err)
	err = manager.Store(ctx, "registry3", registry3)
	require.NoError(t, err)

	// Get all registries
	result, err := manager.GetAll(ctx)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result, 3)

	// Verify all registries were retrieved
	require.Contains(t, result, "registry1")
	require.Contains(t, result, "registry2")
	require.Contains(t, result, "registry3")

	// Verify content
	require.Equal(t, "1.0.0", result["registry1"].Version)
	require.Equal(t, "2.0.0", result["registry2"].Version)
	require.Equal(t, "3.0.0", result["registry3"].Version)
}

func TestFileStorageManager_GetAll_EmptyDirectory(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	manager := NewFileStorageManager(tmpDir)

	ctx := context.Background()

	// Get all from empty directory
	result, err := manager.GetAll(ctx)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Empty(t, result)
}

func TestFileStorageManager_GetAll_NonExistentDirectory(t *testing.T) {
	t.Parallel()

	// Use a non-existent base directory
	tmpDir := filepath.Join(t.TempDir(), "nonexistent")
	manager := NewFileStorageManager(tmpDir)

	ctx := context.Background()

	// Get all should return empty result when directory doesn't exist
	result, err := manager.GetAll(ctx)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Empty(t, result)
}

func TestFileStorageManager_GetAll_PartialFailure(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	manager := NewFileStorageManager(tmpDir)

	ctx := context.Background()

	// Create one valid registry
	registry1 := registry.NewTestUpstreamRegistry(
		registry.WithVersion("1.0.0"),
	)
	err := manager.Store(ctx, "registry1", registry1)
	require.NoError(t, err)

	// Create a registry directory with invalid JSON file
	invalidDir := filepath.Join(tmpDir, "invalid-registry")
	err = os.MkdirAll(invalidDir, 0750)
	require.NoError(t, err)
	invalidFile := filepath.Join(invalidDir, RegistryFileName)
	err = os.WriteFile(invalidFile, []byte("{invalid json}"), 0644)
	require.NoError(t, err)

	// GetAll should return the valid registry and skip the invalid one
	result, err := manager.GetAll(ctx)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result, 1)
	require.Contains(t, result, "registry1")
	require.NotContains(t, result, "invalid-registry")
}
