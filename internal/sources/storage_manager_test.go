package sources

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stacklok/toolhive/pkg/registry/converters"
	toolhivetypes "github.com/stacklok/toolhive/pkg/registry/registry"
	"github.com/stretchr/testify/require"
)

func TestFileStorageManager_StoreAndGet(t *testing.T) {
	t.Parallel()

	// Create temporary directory for test
	tmpDir := t.TempDir()

	manager := NewFileStorageManager(tmpDir)
	require.NotNil(t, manager)

	// Create a test registry
	testRegistry := &toolhivetypes.Registry{
		Version:     "1.0.0",
		LastUpdated: "2024-01-01T00:00:00Z",
		Servers:     make(map[string]*toolhivetypes.ImageMetadata),
	}

	// Convert to UpstreamRegistry
	UpstreamRegistry, err := converters.NewUpstreamRegistryFromToolhiveRegistry(testRegistry)
	require.NoError(t, err)

	// Store the registry
	ctx := context.Background()
	err = manager.Store(ctx, nil, UpstreamRegistry)
	require.NoError(t, err)

	// Verify file was created
	filePath := filepath.Join(tmpDir, RegistryFileName)
	_, err = os.Stat(filePath)
	require.NoError(t, err)

	// Get the registry back
	retrieved, err := manager.Get(ctx, nil)
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

	// Create and store a test registry
	testRegistry := &toolhivetypes.Registry{
		Version: "1.0.0",
	}

	// Convert to UpstreamRegistry
	UpstreamRegistry, err := converters.NewUpstreamRegistryFromToolhiveRegistry(testRegistry)
	require.NoError(t, err)

	ctx := context.Background()
	err = manager.Store(ctx, nil, UpstreamRegistry)
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

func TestFileStorageManager_Delete_PermissionDenied(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	manager := NewFileStorageManager(tmpDir)

	// Create and store a file
	testRegistry := &toolhivetypes.Registry{Version: "1.0.0"}

	// Convert to UpstreamRegistry
	UpstreamRegistry, err := converters.NewUpstreamRegistryFromToolhiveRegistry(testRegistry)
	require.NoError(t, err)

	ctx := context.Background()
	err = manager.Store(ctx, nil, UpstreamRegistry)
	require.NoError(t, err)

	// Make directory read-only to prevent deletion
	err = os.Chmod(tmpDir, 0555) // Read + execute only, no write
	require.NoError(t, err)

	// Try to delete - should fail with permission error
	err = manager.Delete(ctx, nil)
	require.Error(t, err) //
	require.Contains(t, err.Error(), "failed to delete registry file")

	_ = os.Chmod(tmpDir, 0755) // Restore permissions
}

func TestFileStorageManager_Delete_NonExistent(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	manager := NewFileStorageManager(tmpDir)

	// Try to delete file that doesn't exist
	ctx := context.Background()
	err := manager.Delete(ctx, nil)

	// Should succeed (idempotent operation)
	require.NoError(t, err)
}
