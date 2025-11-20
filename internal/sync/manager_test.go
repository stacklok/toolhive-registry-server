package sync

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/registry"
	"github.com/stacklok/toolhive-registry-server/internal/sources"
	"github.com/stacklok/toolhive-registry-server/internal/sources/mocks"
	"github.com/stacklok/toolhive-registry-server/internal/status"
)

func TestNewDefaultSyncManager(t *testing.T) {
	t.Parallel()

	sourceHandlerFactory := sources.NewSourceHandlerFactory()
	storageManager := sources.NewFileStorageManager("/tmp/test-storage")

	syncManager := NewDefaultSyncManager(sourceHandlerFactory, storageManager)

	assert.NotNil(t, syncManager)
	assert.IsType(t, &DefaultSyncManager{}, syncManager)
}

func TestDefaultSyncManager_ShouldSync(t *testing.T) {
	t.Parallel()

	// Create temp directory and test file
	tempDir := t.TempDir()
	testFilePath := filepath.Join(tempDir, "registry.json")
	testReg := registry.NewTestToolHiveRegistry(
		registry.WithImageServer("test-server", "test/image:latest"),
	)
	testData := registry.ToolHiveRegistryToJSON(testReg)
	testHash := fmt.Sprintf("%x", sha256.Sum256(testData))

	require.NoError(t, os.WriteFile(testFilePath, testData, 0644))

	tests := []struct {
		name                string
		manualSyncRequested bool
		config              *config.Config
		syncStatus          *status.SyncStatus
		expectedSyncNeeded  bool
		expectedReason      string
		expectedNextTime    bool // whether nextSyncTime should be set
	}{
		{
			name:                "sync needed when registry is in failed state",
			manualSyncRequested: false,
			config: &config.Config{
				Source: config.SourceConfig{
					Type: "file",
					File: &config.FileConfig{
						Path: testFilePath,
					},
				},
			},
			syncStatus: &status.SyncStatus{
				Phase: status.SyncPhaseFailed,
			},
			expectedSyncNeeded: true,
			expectedReason:     ReasonRegistryNotReady,
			expectedNextTime:   false,
		},
		{
			name:                "sync not needed when already syncing",
			manualSyncRequested: false,
			config: &config.Config{
				Source: config.SourceConfig{
					Type: "file",
					File: &config.FileConfig{
						Path: testFilePath,
					},
				},
			},
			syncStatus: &status.SyncStatus{
				Phase: status.SyncPhaseSyncing,
			},
			expectedSyncNeeded: false,
			expectedReason:     ReasonAlreadyInProgress,
			expectedNextTime:   false,
		},
		{
			name:                "sync needed when registry is in failed state",
			manualSyncRequested: false,
			config: &config.Config{
				Source: config.SourceConfig{
					Type: "file",
					File: &config.FileConfig{
						Path: testFilePath,
					},
				},
			},
			syncStatus: &status.SyncStatus{
				Phase:        status.SyncPhaseFailed,
				LastSyncHash: "",
			},
			expectedSyncNeeded: true,
			expectedReason:     ReasonRegistryNotReady,
			expectedNextTime:   false,
		},
		{
			name:                "manual sync not needed with new trigger value and same hash",
			manualSyncRequested: true,
			config: &config.Config{
				Source: config.SourceConfig{
					Type: "file",
					File: &config.FileConfig{
						Path: testFilePath,
					},
				},
			},
			syncStatus: &status.SyncStatus{
				Phase:        status.SyncPhaseComplete,
				LastSyncHash: testHash,
			},
			expectedSyncNeeded: false,
			expectedReason:     ReasonManualNoChanges, // No data changes but manual trigger
			expectedNextTime:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sourceHandlerFactory := sources.NewSourceHandlerFactory()
			storageManager := sources.NewFileStorageManager("/tmp/test-storage")
			syncManager := NewDefaultSyncManager(sourceHandlerFactory, storageManager)

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			syncNeeded, reason, nextSyncTime := syncManager.ShouldSync(ctx, tt.config, tt.syncStatus, tt.manualSyncRequested)

			// We expect some errors for file source operations, but that's okay for this test
			if tt.expectedSyncNeeded {
				assert.True(t, syncNeeded, "Expected sync to be needed for "+tt.name)
				assert.Equal(t, tt.expectedReason, reason, "Expected specific sync reason")
			} else {
				assert.False(t, syncNeeded, "Expected sync not to be needed for "+tt.name)
				assert.Equal(t, tt.expectedReason, reason, "Expected specific sync reason")
			}

			if tt.expectedNextTime {
				assert.NotNil(t, nextSyncTime, "Expected next sync time to be set")
			} else {
				assert.Nil(t, nextSyncTime, "Expected next sync time to be nil")
			}
		})
	}
}

func TestDefaultSyncManager_PerformSync(t *testing.T) {
	t.Parallel()

	// Create temp directory and test files for different scenarios
	tempDir := t.TempDir()
	testFilePath := filepath.Join(tempDir, "registry.json")

	// Create test file with valid registry data (1 server)
	testReg := registry.NewTestToolHiveRegistry(
		registry.WithImageServer("test-server", "test/image:latest"),
	)
	testData := registry.ToolHiveRegistryToJSON(testReg)
	require.NoError(t, os.WriteFile(testFilePath, testData, 0644))

	emptyReg := registry.NewTestToolHiveRegistry()
	emptyTestData := registry.ToolHiveRegistryToJSON(emptyReg)
	emptyTestFilePath := filepath.Join(tempDir, "empty-registry.json")
	require.NoError(t, os.WriteFile(emptyTestFilePath, emptyTestData, 0644))

	intPtr := func(i int) *int { return &i }

	tests := []struct {
		name                string
		config              *config.Config
		expectedError       bool
		expectedServerCount *int // nil means don't validate
		errorContains       string
	}{
		{
			name: "successful sync with valid data",
			config: &config.Config{
				Source: config.SourceConfig{
					Type:   config.SourceTypeFile,
					Format: config.SourceFormatToolHive,
					File: &config.FileConfig{
						Path: testFilePath,
					},
				},
			},
			expectedError:       false,
			expectedServerCount: intPtr(1), // 1 server in the registry data
		},
		{
			name: "sync fails when source file not found",
			config: &config.Config{
				Source: config.SourceConfig{
					Type:   config.SourceTypeFile,
					Format: config.SourceFormatToolHive,
					File: &config.FileConfig{
						Path: "/data/missing-registry.json",
					},
				},
			},
			expectedError:       true, // PerformSync returns errors for controller to handle
			expectedServerCount: nil,  // Don't validate server count for failed sync
			errorContains:       "",
		},
		{
			name: "successful sync with empty registry data",
			config: &config.Config{
				Source: config.SourceConfig{
					Type:   config.SourceTypeFile,
					Format: config.SourceFormatToolHive,
					File: &config.FileConfig{
						Path: emptyTestFilePath,
					},
				},
			},
			expectedError:       false,
			expectedServerCount: intPtr(0), // 0 servers in the registry data
		},
		{
			name: "successful sync with name filtering",
			config: &config.Config{
				Source: config.SourceConfig{
					Type:   config.SourceTypeFile,
					Format: config.SourceFormatToolHive,
					File: &config.FileConfig{
						Path: testFilePath,
					},
				},
				Filter: &config.FilterConfig{
					Names: &config.NameFilterConfig{
						Include: []string{"test-*"},
					},
				},
			},
			expectedError:       false,
			expectedServerCount: intPtr(1), // 1 server after filtering (test-server matches include pattern)
		},
		{
			name: "successful sync with tag filtering",
			config: &config.Config{
				Source: config.SourceConfig{
					Type:   config.SourceTypeFile,
					Format: config.SourceFormatToolHive,
					File: &config.FileConfig{
						Path: testFilePath,
					},
				},
				Filter: &config.FilterConfig{
					Tags: &config.TagFilterConfig{
						Include: []string{"database"},
					},
				},
			},
			expectedError:       false,
			expectedServerCount: intPtr(1), // 1 server after filtering (test-server has "database" tag)
		},
		{
			name: "successful sync with combined name and tag filtering",
			config: &config.Config{
				Source: config.SourceConfig{
					Type:   config.SourceTypeFile,
					Format: config.SourceFormatToolHive,
					File: &config.FileConfig{
						Path: testFilePath,
					},
				},
				Filter: &config.FilterConfig{
					Names: &config.NameFilterConfig{
						Include: []string{"test-*"},
					},
					Tags: &config.TagFilterConfig{
						Include: []string{"database"},
					},
				},
			},
			expectedError:       false,
			expectedServerCount: intPtr(1), // 1 server after filtering (test-server matches name pattern and has "database" tag)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			sourceHandlerFactory := sources.NewSourceHandlerFactory()
			mockStorageManager := mocks.NewMockStorageManager(ctrl)

			// Setup expectations for successful syncs
			if !tt.expectedError {
				mockStorageManager.EXPECT().
					Store(gomock.Any(), tt.config, gomock.Any()).
					Return(nil).
					Times(1)
			}

			syncManager := NewDefaultSyncManager(sourceHandlerFactory, mockStorageManager)

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			syncResult, syncErr := syncManager.PerformSync(ctx, tt.config)

			if tt.expectedError {
				assert.NotNil(t, syncErr)
				if tt.errorContains != "" {
					assert.Contains(t, syncErr.Error(), tt.errorContains)
				}
			} else {
				assert.Nil(t, syncErr)
			}

			// Validate server count if expected
			if tt.expectedServerCount != nil && syncResult != nil {
				assert.Equal(t, *tt.expectedServerCount, syncResult.ServerCount, "ServerCount should match expected value after sync")
			}
		})
	}
}

func TestIsManualSync(t *testing.T) {
	t.Parallel()

	tests := []struct {
		reason   string
		expected bool
	}{
		{ReasonManualWithChanges, true},
		{ReasonManualNoChanges, true},
		{ReasonSourceDataChanged, false},
		{ReasonRegistryNotReady, false},
		{ReasonUpToDateWithPolicy, false},
	}

	for _, tt := range tests {
		t.Run(tt.reason, func(t *testing.T) {
			t.Parallel()
			result := IsManualSync(tt.reason)
			assert.Equal(t, tt.expected, result)
		})
	}
}
