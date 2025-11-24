package status

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const testRegistryName = "test-registry"

func TestFileStatusPersistence_SaveAndLoad(t *testing.T) {
	t.Parallel()

	// Create temporary directory for test
	tmpDir := t.TempDir()

	persistence := NewFileStatusPersistence(tmpDir)
	require.NotNil(t, persistence)

	registryName := testRegistryName
	// Create a test status
	now := time.Now()
	testStatus := &SyncStatus{
		Phase:        SyncPhaseComplete,
		Message:      "Test sync completed",
		LastAttempt:  &now,
		AttemptCount: 1,
		LastSyncTime: &now,
		LastSyncHash: "abc123",
		ServerCount:  5,
	}

	// Save the status
	ctx := context.Background()
	err := persistence.SaveStatus(ctx, registryName, testStatus)
	require.NoError(t, err)

	// Verify file was created
	expectedPath := filepath.Join(tmpDir, registryName, StatusFileName)
	_, err = os.Stat(expectedPath)
	require.NoError(t, err)

	// Load the status back
	loaded, err := persistence.LoadStatus(ctx, registryName)
	require.NoError(t, err)
	require.NotNil(t, loaded)
	require.Equal(t, testStatus.Phase, loaded.Phase)
	require.Equal(t, testStatus.Message, loaded.Message)
	require.Equal(t, testStatus.AttemptCount, loaded.AttemptCount)
	require.Equal(t, testStatus.LastSyncHash, loaded.LastSyncHash)
	require.Equal(t, testStatus.ServerCount, loaded.ServerCount)
}

func TestFileStatusPersistence_LoadNonExistent(t *testing.T) {
	t.Parallel()

	// Create temporary directory for test
	tmpDir := t.TempDir()

	persistence := NewFileStatusPersistence(tmpDir)
	require.NotNil(t, persistence)

	registryName := testRegistryName

	// Load non-existent status should return empty status
	ctx := context.Background()
	loaded, err := persistence.LoadStatus(ctx, registryName)
	require.NoError(t, err)
	require.NotNil(t, loaded)
	require.Equal(t, SyncPhase(""), loaded.Phase)
	require.Equal(t, "", loaded.Message)
}

func TestFileStatusPersistence_UpdateStatus(t *testing.T) {
	t.Parallel()

	// Create temporary directory for test
	tmpDir := t.TempDir()

	persistence := NewFileStatusPersistence(tmpDir)
	require.NotNil(t, persistence)

	registryName := testRegistryName
	ctx := context.Background()

	// Save initial status
	now1 := time.Now()
	initialStatus := &SyncStatus{
		Phase:        SyncPhaseSyncing,
		Message:      "Syncing...",
		LastAttempt:  &now1,
		AttemptCount: 1,
	}
	err := persistence.SaveStatus(ctx, registryName, initialStatus)
	require.NoError(t, err)

	// Update status
	now2 := time.Now()
	updatedStatus := &SyncStatus{
		Phase:        SyncPhaseComplete,
		Message:      "Sync completed",
		LastAttempt:  &now2,
		AttemptCount: 0,
		LastSyncTime: &now2,
		LastSyncHash: "xyz789",
		ServerCount:  10,
	}
	err = persistence.SaveStatus(ctx, registryName, updatedStatus)
	require.NoError(t, err)

	// Load and verify it was updated
	loaded, err := persistence.LoadStatus(ctx, registryName)
	require.NoError(t, err)
	require.NotNil(t, loaded)
	require.Equal(t, SyncPhaseComplete, loaded.Phase)
	require.Equal(t, "Sync completed", loaded.Message)
	require.Equal(t, 0, loaded.AttemptCount)
	require.Equal(t, "xyz789", loaded.LastSyncHash)
	require.Equal(t, 10, loaded.ServerCount)
}

func TestFileStatusPersistence_AtomicWrite(t *testing.T) {
	t.Parallel()

	// Create temporary directory for test
	tmpDir := t.TempDir()

	persistence := NewFileStatusPersistence(tmpDir)
	require.NotNil(t, persistence)

	registryName := testRegistryName
	ctx := context.Background()

	// Save status
	now := time.Now()
	testStatus := &SyncStatus{
		Phase:       SyncPhaseComplete,
		LastAttempt: &now,
	}
	err := persistence.SaveStatus(ctx, registryName, testStatus)
	require.NoError(t, err)

	// Verify temporary file was cleaned up
	statusPath := filepath.Join(tmpDir, registryName, StatusFileName)
	tempPath := statusPath + ".tmp"
	_, err = os.Stat(tempPath)
	require.True(t, os.IsNotExist(err), "Temporary file should not exist after save")
}

func TestFileStatusPersistence_LoadAllStatus(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	persistence := NewFileStatusPersistence(tmpDir)

	ctx := context.Background()

	// Create multiple test statuses
	now := time.Now()
	status1 := &SyncStatus{
		Phase:        SyncPhaseComplete,
		Message:      "Registry 1 sync completed",
		LastAttempt:  &now,
		AttemptCount: 1,
		LastSyncHash: "hash1",
		ServerCount:  5,
	}
	status2 := &SyncStatus{
		Phase:        SyncPhaseSyncing,
		Message:      "Registry 2 syncing",
		LastAttempt:  &now,
		AttemptCount: 2,
		LastSyncHash: "hash2",
		ServerCount:  10,
	}
	status3 := &SyncStatus{
		Phase:        SyncPhaseFailed,
		Message:      "Registry 3 failed",
		LastAttempt:  &now,
		AttemptCount: 3,
		ServerCount:  0,
	}

	// Save statuses for multiple registries
	err := persistence.SaveStatus(ctx, "registry1", status1)
	require.NoError(t, err)
	err = persistence.SaveStatus(ctx, "registry2", status2)
	require.NoError(t, err)
	err = persistence.SaveStatus(ctx, "registry3", status3)
	require.NoError(t, err)

	// Load all statuses
	result, err := persistence.LoadAllStatus(ctx)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result, 3)

	// Verify all statuses were retrieved
	require.Contains(t, result, "registry1")
	require.Contains(t, result, "registry2")
	require.Contains(t, result, "registry3")

	// Verify content
	require.Equal(t, SyncPhaseComplete, result["registry1"].Phase)
	require.Equal(t, "Registry 1 sync completed", result["registry1"].Message)
	require.Equal(t, 5, result["registry1"].ServerCount)

	require.Equal(t, SyncPhaseSyncing, result["registry2"].Phase)
	require.Equal(t, "Registry 2 syncing", result["registry2"].Message)
	require.Equal(t, 10, result["registry2"].ServerCount)

	require.Equal(t, SyncPhaseFailed, result["registry3"].Phase)
	require.Equal(t, "Registry 3 failed", result["registry3"].Message)
	require.Equal(t, 0, result["registry3"].ServerCount)
}

func TestFileStatusPersistence_LoadAllStatus_EmptyDirectory(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	persistence := NewFileStatusPersistence(tmpDir)

	ctx := context.Background()

	// Load all from empty directory
	result, err := persistence.LoadAllStatus(ctx)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Empty(t, result)
}

func TestFileStatusPersistence_LoadAllStatus_NonExistentDirectory(t *testing.T) {
	t.Parallel()

	// Use a non-existent base directory
	tmpDir := filepath.Join(t.TempDir(), "nonexistent")
	persistence := NewFileStatusPersistence(tmpDir)

	ctx := context.Background()

	// Load all should return empty result when directory doesn't exist
	result, err := persistence.LoadAllStatus(ctx)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Empty(t, result)
}

func TestFileStatusPersistence_LoadAllStatus_PartialFailure(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	persistence := NewFileStatusPersistence(tmpDir)

	ctx := context.Background()

	// Create one valid status
	now := time.Now()
	status1 := &SyncStatus{
		Phase:       SyncPhaseComplete,
		LastAttempt: &now,
		ServerCount: 5,
	}
	err := persistence.SaveStatus(ctx, "registry1", status1)
	require.NoError(t, err)

	// Create a registry directory with invalid JSON file
	invalidDir := filepath.Join(tmpDir, "invalid-registry")
	err = os.MkdirAll(invalidDir, 0750)
	require.NoError(t, err)
	invalidFile := filepath.Join(invalidDir, StatusFileName)
	err = os.WriteFile(invalidFile, []byte("{invalid json}"), 0644)
	require.NoError(t, err)

	// LoadAllStatus should return the valid status and skip the invalid one
	result, err := persistence.LoadAllStatus(ctx)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result, 1)
	require.Contains(t, result, "registry1")
	require.NotContains(t, result, "invalid-registry")
}
