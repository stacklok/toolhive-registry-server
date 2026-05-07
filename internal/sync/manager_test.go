package sync

import (
	"context"
	"crypto/sha256"
	"encoding/json"
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
	"github.com/stacklok/toolhive-registry-server/internal/status"
	writermocks "github.com/stacklok/toolhive-registry-server/internal/sync/writer/mocks"
)

func ptrTime(t time.Time) *time.Time { return &t }

func TestNewDefaultSyncManager(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registryHandlerFactory := sources.NewRegistryHandlerFactory()
	mockWriter := writermocks.NewMockSyncWriter(ctrl)

	syncManager := NewDefaultSyncManager(registryHandlerFactory, mockWriter)

	assert.NotNil(t, syncManager)
	assert.IsType(t, &defaultSyncManager{}, syncManager)
}

func TestDefaultSyncManager_ShouldSync(t *testing.T) {
	t.Parallel()

	// Create temp directory and test file
	tempDir := t.TempDir()
	testFilePath := filepath.Join(tempDir, "registry.json")
	testReg := registry.NewTestUpstreamRegistry(
		registry.WithServers(registry.NewTestServer("io.test/test-server",
			registry.WithOCIPackage("test/image:latest"),
		)),
	)
	testData, err := json.Marshal(testReg)
	require.NoError(t, err)
	testHash := fmt.Sprintf("%x", sha256.Sum256(testData))

	require.NoError(t, os.WriteFile(testFilePath, testData, 0644))

	tests := []struct {
		name                string
		manualSyncRequested bool
		config              *config.SourceConfig
		syncStatus          *status.SyncStatus
		expectedReason      Reason
	}{
		{
			name:                "sync needed when registry is in failed state",
			manualSyncRequested: false,
			config: &config.SourceConfig{
				Name: "test-registry",
				File: &config.FileConfig{
					Path: testFilePath,
				},
			},
			syncStatus: &status.SyncStatus{
				Phase: status.SyncPhaseFailed,
			},
			expectedReason: ReasonRegistryNotReady,
		},
		{
			name:                "sync not needed when already syncing within grace window",
			manualSyncRequested: false,
			config: &config.SourceConfig{
				Name: "test-registry",
				File: &config.FileConfig{
					Path: testFilePath,
				},
			},
			syncStatus: &status.SyncStatus{
				Phase:       status.SyncPhaseSyncing,
				LastAttempt: ptrTime(time.Now().Add(-5 * time.Minute)), // recent — real in-flight sync
			},
			expectedReason: ReasonAlreadyInProgress,
		},
		{
			// Regression coverage for the "wedged IN_PROGRESS" bug: when a sync
			// process is killed mid-flight, its deferred status update never
			// lands and the row stays IN_PROGRESS forever. Once started_at is
			// older than orphanedSyncGracePeriod, ShouldSync must treat the row
			// as orphaned and fall through to the normal sync path, which lets
			// the coordinator's defer write a fresh terminal status.
			name:                "stale IN_PROGRESS past grace window is treated as orphaned",
			manualSyncRequested: false,
			config: &config.SourceConfig{
				Name: "test-registry",
				File: &config.FileConfig{
					Path: testFilePath,
				},
			},
			syncStatus: &status.SyncStatus{
				Phase:       status.SyncPhaseSyncing,
				LastAttempt: ptrTime(time.Now().Add(-2 * time.Hour)), // well past 1h grace
			},
			expectedReason: ReasonRegistryNotReady,
		},
		{
			// IN_PROGRESS without a started_at can only come from legacy data
			// or a code path that forgot to set it; either way we can't tell
			// when the sync started, so default to treating it as orphaned.
			name:                "IN_PROGRESS with no started_at is treated as orphaned",
			manualSyncRequested: false,
			config: &config.SourceConfig{
				Name: "test-registry",
				File: &config.FileConfig{
					Path: testFilePath,
				},
			},
			syncStatus: &status.SyncStatus{
				Phase: status.SyncPhaseSyncing,
			},
			expectedReason: ReasonRegistryNotReady,
		},
		{
			name:                "sync needed when registry is in failed state",
			manualSyncRequested: false,
			config: &config.SourceConfig{
				Name: "test-registry",
				File: &config.FileConfig{
					Path: testFilePath,
				},
			},
			syncStatus: &status.SyncStatus{
				Phase:        status.SyncPhaseFailed,
				LastSyncHash: "",
			},
			expectedReason: ReasonRegistryNotReady,
		},
		{
			name:                "manual sync not needed with new trigger value and same hash",
			manualSyncRequested: true,
			config: &config.SourceConfig{
				Name: "test-registry",
				File: &config.FileConfig{
					Path: testFilePath,
				},
			},
			syncStatus: &status.SyncStatus{
				Phase:        status.SyncPhaseComplete,
				LastSyncHash: testHash,
			},
			expectedReason: ReasonManualNoChanges, // No data changes but manual trigger
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			registryHandlerFactory := sources.NewRegistryHandlerFactory()
			mockWriter := writermocks.NewMockSyncWriter(ctrl)
			syncManager := NewDefaultSyncManager(registryHandlerFactory, mockWriter)

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			reason, _ := syncManager.ShouldSync(ctx, tt.config, tt.syncStatus, tt.manualSyncRequested)

			assert.Equal(t, tt.expectedReason, reason, "Expected specific sync reason for "+tt.name)
			assert.Equal(t, tt.expectedReason.ShouldSync(), reason.ShouldSync(), "ShouldSync() should match expected reason")
			assert.Equal(t, tt.expectedReason.String(), reason.String(), "String() should match expected reason")
		})
	}
}

func TestDefaultSyncManager_PerformSync(t *testing.T) {
	t.Parallel()

	// Create temp directory and test files for different scenarios
	tempDir := t.TempDir()
	testFilePath := filepath.Join(tempDir, "registry.json")

	// Create test file with valid registry data (1 server)
	testReg := registry.NewTestUpstreamRegistry(
		registry.WithServers(registry.NewTestServer("io.test/test-server",
			registry.WithOCIPackage("test/image:latest"),
			registry.WithTags("database"),
		)),
	)
	testData, err := json.Marshal(testReg)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(testFilePath, testData, 0644))

	emptyReg := registry.NewTestUpstreamRegistry(
		registry.WithServers(registry.NewTestServer("io.test/placeholder",
			registry.WithOCIPackage("placeholder/image:latest"),
		)),
	)
	emptyTestData, err := json.Marshal(emptyReg)
	require.NoError(t, err)
	emptyTestFilePath := filepath.Join(tempDir, "empty-registry.json")
	require.NoError(t, os.WriteFile(emptyTestFilePath, emptyTestData, 0644))

	intPtr := func(i int) *int { return &i }

	tests := []struct {
		name                string
		config              *config.SourceConfig
		expectedError       bool
		expectedServerCount *int // nil means don't validate
		errorContains       string
	}{
		{
			name: "successful sync with valid data",
			config: &config.SourceConfig{
				Name: "test-registry",
				File: &config.FileConfig{
					Path: testFilePath,
				},
			},
			expectedError:       false,
			expectedServerCount: intPtr(1), // 1 server in the registry data
		},
		{
			name: "sync fails when source file not found",
			config: &config.SourceConfig{
				Name: "test-registry",
				File: &config.FileConfig{
					Path: "/data/missing-registry.json",
				},
			},
			expectedError:       true, // PerformSync returns errors for controller to handle
			expectedServerCount: nil,  // Don't validate server count for failed sync
			errorContains:       "",
		},
		{
			name: "successful sync with minimal registry data",
			config: &config.SourceConfig{
				Name: "test-registry",
				File: &config.FileConfig{
					Path: emptyTestFilePath,
				},
			},
			expectedError:       false,
			expectedServerCount: intPtr(1), // 1 placeholder server in the registry data
		},
		{
			name: "successful sync with name filtering",
			config: &config.SourceConfig{
				Name: "test-registry",
				File: &config.FileConfig{
					Path: testFilePath,
				},
				Filter: &config.FilterConfig{
					Names: &config.NameFilterConfig{
						Include: []string{"io.test/*"},
					},
				},
			},
			expectedError:       false,
			expectedServerCount: intPtr(1), // 1 server after filtering (io.test/test-server matches include pattern)
		},
		{
			name: "successful sync with tag filtering",
			config: &config.SourceConfig{
				Name: "test-registry",
				File: &config.FileConfig{
					Path: testFilePath,
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
			config: &config.SourceConfig{
				Name: "test-registry",
				File: &config.FileConfig{
					Path: testFilePath,
				},
				Filter: &config.FilterConfig{
					Names: &config.NameFilterConfig{
						Include: []string{"io.test/*"},
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

			registryHandlerFactory := sources.NewRegistryHandlerFactory()
			mockWriter := writermocks.NewMockSyncWriter(ctrl)

			// Setup expectations for successful syncs
			if !tt.expectedError {
				mockWriter.EXPECT().
					Store(gomock.Any(), tt.config.Name, gomock.Any()).
					Return(nil).
					Times(1)
			}

			syncManager := NewDefaultSyncManager(registryHandlerFactory, mockWriter)

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			syncResult, syncErr := syncManager.PerformSync(ctx, tt.config, nil)

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

func TestDefaultSyncManager_PerformSync_WithPrefetched(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Build a FetchResult directly without a real file source
	testReg := registry.NewTestUpstreamRegistry(
		registry.WithServers(registry.NewTestServer("io.test/prefetched-server",
			registry.WithOCIPackage("test/image:latest"),
		)),
	)
	testData, err := json.Marshal(testReg)
	require.NoError(t, err)
	validator := sources.NewRegistryDataValidator()
	reg, err := validator.ValidateData(testData)
	require.NoError(t, err)
	hash := fmt.Sprintf("%x", sha256.Sum256(testData))
	prefetched := sources.NewFetchResult(reg, hash)

	mockWriter := writermocks.NewMockSyncWriter(ctrl)
	mockWriter.EXPECT().
		Store(gomock.Any(), "test-registry", gomock.Any()).
		Return(nil).
		Times(1)

	// Config points to a non-existent path — if PerformSync attempts a fetch it will fail,
	// proving the prefetched data was reused instead.
	regCfg := &config.SourceConfig{
		Name: "test-registry",
		File: &config.FileConfig{
			Path: "/nonexistent/path/registry.json",
		},
	}

	syncManager := NewDefaultSyncManager(sources.NewRegistryHandlerFactory(), mockWriter)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, syncErr := syncManager.PerformSync(ctx, regCfg, prefetched)

	require.Nil(t, syncErr)
	require.NotNil(t, result)
	assert.Equal(t, hash, result.Hash)
	assert.Equal(t, 1, result.ServerCount)
}

func TestIsManualSync(t *testing.T) {
	t.Parallel()

	tests := []struct {
		reason   Reason
		expected bool
	}{
		{ReasonManualWithChanges, true},
		{ReasonManualNoChanges, true},
		{ReasonSourceDataChanged, false},
		{ReasonRegistryNotReady, false},
		{ReasonUpToDateWithPolicy, false},
	}

	for _, tt := range tests {
		t.Run(tt.reason.String(), func(t *testing.T) {
			t.Parallel()
			result := IsManualSync(tt.reason)
			assert.Equal(t, tt.expected, result)
		})
	}
}
