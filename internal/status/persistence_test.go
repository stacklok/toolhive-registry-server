package status

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFileStatusPersistence_SaveAndLoad(t *testing.T) {
	t.Parallel()

	// Create temporary directory for test
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, StatusFileName)

	persistence := NewFileStatusPersistence(filePath)
	require.NotNil(t, persistence)

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
	err := persistence.SaveStatus(ctx, testStatus)
	require.NoError(t, err)

	// Verify file was created
	_, err = os.Stat(filePath)
	require.NoError(t, err)

	// Load the status back
	loaded, err := persistence.LoadStatus(ctx)
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
	filePath := filepath.Join(tmpDir, StatusFileName)

	persistence := NewFileStatusPersistence(filePath)
	require.NotNil(t, persistence)

	// Load non-existent status should return empty status
	ctx := context.Background()
	loaded, err := persistence.LoadStatus(ctx)
	require.NoError(t, err)
	require.NotNil(t, loaded)
	require.Equal(t, SyncPhase(""), loaded.Phase)
	require.Equal(t, "", loaded.Message)
}

func TestFileStatusPersistence_UpdateStatus(t *testing.T) {
	t.Parallel()

	// Create temporary directory for test
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, StatusFileName)

	persistence := NewFileStatusPersistence(filePath)
	require.NotNil(t, persistence)

	ctx := context.Background()

	// Save initial status
	now1 := time.Now()
	initialStatus := &SyncStatus{
		Phase:        SyncPhaseSyncing,
		Message:      "Syncing...",
		LastAttempt:  &now1,
		AttemptCount: 1,
	}
	err := persistence.SaveStatus(ctx, initialStatus)
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
	err = persistence.SaveStatus(ctx, updatedStatus)
	require.NoError(t, err)

	// Load and verify it was updated
	loaded, err := persistence.LoadStatus(ctx)
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
	filePath := filepath.Join(tmpDir, StatusFileName)

	persistence := NewFileStatusPersistence(filePath)
	require.NotNil(t, persistence)

	ctx := context.Background()

	// Save status
	now := time.Now()
	testStatus := &SyncStatus{
		Phase:       SyncPhaseComplete,
		LastAttempt: &now,
	}
	err := persistence.SaveStatus(ctx, testStatus)
	require.NoError(t, err)

	// Verify temporary file was cleaned up
	tempPath := filePath + ".tmp"
	_, err = os.Stat(tempPath)
	require.True(t, os.IsNotExist(err), "Temporary file should not exist after save")
}
