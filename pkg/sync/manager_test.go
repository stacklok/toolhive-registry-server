package sync

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/stacklok/toolhive-registry-server/pkg/config"
	"github.com/stacklok/toolhive-registry-server/pkg/sources"
	"github.com/stacklok/toolhive-registry-server/pkg/sources/mocks"
	"github.com/stacklok/toolhive-registry-server/pkg/status"
	"go.uber.org/mock/gomock"
)

func TestNewDefaultSyncManager(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	sourceHandlerFactory := sources.NewSourceHandlerFactory(fakeClient)
	storageManager := sources.NewFileStorageManager("/tmp/test-storage")

	syncManager := NewDefaultSyncManager(fakeClient, scheme, sourceHandlerFactory, storageManager)

	assert.NotNil(t, syncManager)
	assert.IsType(t, &DefaultSyncManager{}, syncManager)
}

func TestDefaultSyncManager_ShouldSync(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	tests := []struct {
		name                string
		manualSyncRequested bool
		config              *config.Config
		syncStatus          *status.SyncStatus
		configMap           *corev1.ConfigMap
		expectedSyncNeeded  bool
		expectedReason      string
		expectedNextTime    bool // whether nextSyncTime should be set
	}{
		{
			name:                "sync needed when registry is in failed state",
			manualSyncRequested: false,
			config: &config.Config{
				Source: config.SourceConfig{
					Type: "configmap",
					ConfigMap: &config.ConfigMapConfig{
						Name: "test-configmap",
						Key:  "registry.json",
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
					Type: "configmap",
					ConfigMap: &config.ConfigMapConfig{
						Name: "test-configmap",
						Key:  "registry.json",
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
			name:                "sync needed when no last sync hash",
			manualSyncRequested: false,
			config: &config.Config{
				Source: config.SourceConfig{
					Type: "configmap",
					ConfigMap: &config.ConfigMapConfig{
						Name: "test-configmap",
						Key:  "registry.json",
					},
				},
			},
			syncStatus: &status.SyncStatus{
				Phase:        status.SyncPhaseComplete,
				LastSyncHash: "",
			},
			expectedSyncNeeded: true,
			expectedReason:     ReasonSourceDataChanged,
			expectedNextTime:   false,
		},
		{
			name:                "manual sync not needed with new trigger value and same hash",
			manualSyncRequested: true,
			config: &config.Config{
				Source: config.SourceConfig{
					Type: "configmap",
					ConfigMap: &config.ConfigMapConfig{
						Namespace: "test-namespace",
						Name:      "test-configmap",
						Key:       "registry.json",
					},
				},
			},
			syncStatus: &status.SyncStatus{
				Phase:        status.SyncPhaseComplete,
				LastSyncHash: "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08", // SHA256 of "test"
			},
			configMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configmap",
					Namespace: "test-namespace",
				},
				Data: map[string]string{
					"registry.json": "test", // This will produce the same hash as above
				},
			},
			expectedSyncNeeded: false,
			expectedReason:     ReasonManualNoChanges, // No data changes but manual trigger
			expectedNextTime:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			objects := []runtime.Object{}
			if tt.configMap != nil {
				objects = append(objects, tt.configMap)
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(objects...).
				Build()

			sourceHandlerFactory := sources.NewSourceHandlerFactory(fakeClient)
			storageManager := sources.NewFileStorageManager("/tmp/test-storage")
			syncManager := NewDefaultSyncManager(fakeClient, scheme, sourceHandlerFactory, storageManager)

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			syncNeeded, reason, nextSyncTime := syncManager.ShouldSync(ctx, tt.config, tt.syncStatus, tt.manualSyncRequested)

			// We expect some errors for ConfigMap not found, but that's okay for this test
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

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	intPtr := func(i int) *int { return &i }

	tests := []struct {
		name                string
		config              *config.Config
		sourceConfigMap     *corev1.ConfigMap
		existingStorageCM   *corev1.ConfigMap
		expectedError       bool
		expectedServerCount *int // nil means don't validate
		errorContains       string
	}{
		{
			name: "successful sync with valid data",
			config: &config.Config{
				Source: config.SourceConfig{
					Type:   config.SourceTypeConfigMap,
					Format: config.SourceFormatToolHive,
					ConfigMap: &config.ConfigMapConfig{
						Name:      "source-configmap",
						Namespace: "test-namespace",
						Key:       "registry.json",
					},
				},
			},
			sourceConfigMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "source-configmap",
					Namespace: "test-namespace",
				},
				Data: map[string]string{
					"registry.json": `{"version": "1.0.0", "last_updated": "2023-01-01T00:00:00Z", "servers": {"test-server": {"name": "test-server", "description": "Test server", "tier": "Community", "status": "Active", "transport": "stdio", "tools": ["test_tool"], "image": "test/image:latest"}}, "remoteServers": {}}`,
				},
			},
			expectedError:       false,
			expectedServerCount: intPtr(1), // 1 server in the registry data
		},
		{
			name: "sync fails when source configmap not found",
			config: &config.Config{
				Source: config.SourceConfig{
					Type:   config.SourceTypeConfigMap,
					Format: config.SourceFormatToolHive,
					ConfigMap: &config.ConfigMapConfig{
						Name:      "missing-configmap",
						Namespace: "test-namespace",
						Key:       "registry.json",
					},
				},
			},
			sourceConfigMap:     nil,
			expectedError:       true, // PerformSync returns errors for controller to handle
			expectedServerCount: nil,  // Don't validate server count for failed sync
			errorContains:       "",
		},
		{
			name: "successful sync with empty registry data",
			config: &config.Config{
				Source: config.SourceConfig{
					Type:   config.SourceTypeConfigMap,
					Format: config.SourceFormatToolHive,
					ConfigMap: &config.ConfigMapConfig{
						Name:      "source-configmap",
						Namespace: "test-namespace",
						Key:       "registry.json",
					},
				},
			},
			sourceConfigMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "source-configmap",
					Namespace: "test-namespace",
				},
				Data: map[string]string{
					"registry.json": `{"version": "1.0.0", "last_updated": "2023-01-01T00:00:00Z", "servers": {}, "remoteServers": {}}`,
				},
			},
			expectedError:       false,
			expectedServerCount: intPtr(0), // 0 servers in the registry data
		},
		{
			name: "successful sync with name filtering",
			config: &config.Config{
				Source: config.SourceConfig{
					Type:   config.SourceTypeConfigMap,
					Format: config.SourceFormatToolHive,
					ConfigMap: &config.ConfigMapConfig{
						Name:      "source-configmap",
						Namespace: "test-namespace",
						Key:       "registry.json",
					},
				},
				Filter: &config.FilterConfig{
					Names: &config.NameFilterConfig{
						Include: []string{"test-*"},
						Exclude: []string{"*-excluded"},
					},
				},
			},
			sourceConfigMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "source-configmap",
					Namespace: "test-namespace",
				},
				Data: map[string]string{
					"registry.json": `{"version": "1.0.0", "last_updated": "2023-01-01T00:00:00Z", "servers": {"test-server": {"name": "test-server", "description": "Test server", "tier": "Community", "status": "Active", "transport": "stdio", "tools": ["test_tool"], "image": "test/image:latest"}, "excluded-server": {"name": "excluded-server", "description": "Excluded server", "tier": "Community", "status": "Active", "transport": "stdio", "tools": ["test_tool"], "image": "test/image:latest"}, "other-server": {"name": "other-server", "description": "Other server", "tier": "Community", "status": "Active", "transport": "stdio", "tools": ["test_tool"], "image": "test/image:latest"}}, "remoteServers": {}}`,
				},
			},
			expectedError:       false,
			expectedServerCount: intPtr(1), // 1 server after filtering (test-server matches include, others excluded/don't match)
		},
		{
			name: "successful sync with tag filtering",
			config: &config.Config{
				Source: config.SourceConfig{
					Type:   config.SourceTypeConfigMap,
					Format: config.SourceFormatToolHive,
					ConfigMap: &config.ConfigMapConfig{
						Name:      "source-configmap",
						Namespace: "test-namespace",
						Key:       "registry.json",
					},
				},
				Filter: &config.FilterConfig{
					Tags: &config.TagFilterConfig{
						Include: []string{"database"},
						Exclude: []string{"deprecated"},
					},
				},
			},
			sourceConfigMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "source-configmap",
					Namespace: "test-namespace",
				},
				Data: map[string]string{
					"registry.json": `{"version": "1.0.0", "last_updated": "2023-01-01T00:00:00Z", "servers": {"db-server": {"name": "db-server", "description": "Database server", "tier": "Community", "status": "Active", "transport": "stdio", "tools": ["db_tool"], "tags": ["database", "sql"], "image": "db/image:latest"}, "old-db-server": {"name": "old-db-server", "description": "Old database server", "tier": "Community", "status": "Active", "transport": "stdio", "tools": ["db_tool"], "tags": ["database", "deprecated"], "image": "db/image:old"}, "web-server": {"name": "web-server", "description": "Web server", "tier": "Community", "status": "Active", "transport": "stdio", "tools": ["web_tool"], "tags": ["web"], "image": "web/image:latest"}}, "remoteServers": {}}`,
				},
			},
			expectedError:       false,
			expectedServerCount: intPtr(1), // 1 server after filtering (db-server has "database" tag, old-db-server excluded by "deprecated", web-server doesn't have "database")
		},
		{
			name: "successful sync with combined name and tag filtering",
			config: &config.Config{
				Source: config.SourceConfig{
					Type:   config.SourceTypeConfigMap,
					Format: config.SourceFormatToolHive,
					ConfigMap: &config.ConfigMapConfig{
						Name:      "source-configmap",
						Namespace: "test-namespace",
						Key:       "registry.json",
					},
				},
				Filter: &config.FilterConfig{
					Names: &config.NameFilterConfig{
						Include: []string{"prod-*"},
					},
					Tags: &config.TagFilterConfig{
						Include: []string{"production"},
						Exclude: []string{"experimental"},
					},
				},
			},
			sourceConfigMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "source-configmap",
					Namespace: "test-namespace",
				},
				Data: map[string]string{
					"registry.json": `{"version": "1.0.0", "last_updated": "2023-01-01T00:00:00Z", "servers": {"prod-db": {"name": "prod-db", "description": "Production database", "tier": "Official", "status": "Active", "transport": "stdio", "tools": ["db_tool"], "tags": ["database", "production"], "image": "db/image:prod"}, "prod-web": {"name": "prod-web", "description": "Production web server", "tier": "Official", "status": "Active", "transport": "stdio", "tools": ["web_tool"], "tags": ["web", "production"], "image": "web/image:prod"}, "prod-experimental": {"name": "prod-experimental", "description": "Experimental prod server", "tier": "Community", "status": "Active", "transport": "stdio", "tools": ["exp_tool"], "tags": ["production", "experimental"], "image": "exp/image:latest"}, "dev-db": {"name": "dev-db", "description": "Development database", "tier": "Community", "status": "Active", "transport": "stdio", "tools": ["db_tool"], "tags": ["database", "development"], "image": "db/image:dev"}}, "remoteServers": {}}`,
				},
			},
			expectedError:       false,
			expectedServerCount: intPtr(2), // 2 servers after filtering (prod-db and prod-web match name pattern and have "production" tag, prod-experimental excluded by "experimental", dev-db doesn't match "prod-*")
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			objects := []runtime.Object{}
			if tt.sourceConfigMap != nil {
				objects = append(objects, tt.sourceConfigMap)
			}
			if tt.existingStorageCM != nil {
				objects = append(objects, tt.existingStorageCM)
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(objects...).
				Build()

			sourceHandlerFactory := sources.NewSourceHandlerFactory(fakeClient)
			mockStorageManager := mocks.NewMockStorageManager(ctrl)

			// Setup expectations for successful syncs
			if !tt.expectedError {
				mockStorageManager.EXPECT().
					Store(gomock.Any(), tt.config, gomock.Any()).
					Return(nil).
					Times(1)
			}

			syncManager := NewDefaultSyncManager(fakeClient, scheme, sourceHandlerFactory, mockStorageManager)

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			result, syncResult, syncErr := syncManager.PerformSync(ctx, tt.config)

			if tt.expectedError {
				assert.NotNil(t, syncErr)
				if tt.errorContains != "" {
					assert.Contains(t, syncErr.Error(), tt.errorContains)
				}
			} else {
				assert.Nil(t, syncErr)
			}

			// Verify the result
			assert.NotNil(t, result)

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
