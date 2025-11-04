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
	"github.com/stacklok/toolhive-registry-server/pkg/status"
)

func TestDefaultDataChangeDetector_IsDataChanged(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	tests := []struct {
		name            string
		config          *config.Config
		status          *status.SyncStatus
		configMap       *corev1.ConfigMap
		expectedChanged bool
		expectError     bool
	}{
		{
			name: "data changed when no last sync hash",
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
			status: &status.SyncStatus{
				LastSyncHash: "", // No hash means data changed
			},
			configMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configmap",
					Namespace: "test-namespace",
				},
				Data: map[string]string{
					"registry.json": "test data",
				},
			},
			expectedChanged: true,
			expectError:     false,
		},
		{
			name: "data unchanged when hash matches",
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
			status: &status.SyncStatus{
				LastSyncHash: "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08", // SHA256 of "test"
			},
			configMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configmap",
					Namespace: "test-namespace",
				},
				Data: map[string]string{
					"registry.json": "test", // Same content
				},
			},
			expectedChanged: false,
			expectError:     false,
		},
		{
			name: "data changed when hash differs",
			config: &config.Config{
				Source: config.SourceConfig{
					Type:   "configmap",
					Format: config.SourceFormatToolHive,
					ConfigMap: &config.ConfigMapConfig{
						Namespace: "test-namespace",
						Name:      "test-configmap",
						Key:       "registry.json",
					},
				},
			},
			status: &status.SyncStatus{
				LastSyncHash: "old-hash",
			},
			configMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configmap",
					Namespace: "test-namespace",
				},
				Data: map[string]string{
					"registry.json": "new data",
				},
			},
			expectedChanged: true,
			expectError:     false,
		},
		{
			name: "error when configmap not found",
			config: &config.Config{
				Source: config.SourceConfig{
					Type:   "configmap",
					Format: config.SourceFormatToolHive,
					ConfigMap: &config.ConfigMapConfig{
						Name: "missing-configmap",
						Key:  "registry.json",
					},
				},
			},
			status: &status.SyncStatus{
				LastSyncHash: "some-hash",
			},
			configMap:       nil,  // ConfigMap doesn't exist
			expectedChanged: true, // Should return true on error
			expectError:     true,
		},
	}

	for _, tt := range tests {
		tt := tt
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
			detector := &DefaultDataChangeDetector{
				sourceHandlerFactory: sourceHandlerFactory,
			}

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			changed, err := detector.IsDataChanged(ctx, tt.config, tt.status)

			assert.Equal(t, tt.expectedChanged, changed, "Data change detection result should match expected")

			if tt.expectError {
				assert.Error(t, err, "Expected an error")
			} else {
				assert.NoError(t, err, "Should not have an error")
			}
		})
	}
}

func TestDefaultAutomaticSyncChecker_IsIntervalSyncNeeded(t *testing.T) {
	t.Parallel()

	now := time.Now()
	oneHourAgo := now.Add(-time.Hour)
	thirtyMinutesAgo := now.Add(-30 * time.Minute)

	tests := []struct {
		name                 string
		config               *config.Config
		status               *status.SyncStatus
		expectedSyncNeeded   bool
		expectedNextTimeFunc func(time.Time) bool // Function to verify nextSyncTime
		expectError          bool
	}{
		{
			name: "invalid interval format",
			config: &config.Config{
				SyncPolicy: &config.SyncPolicyConfig{
					Interval: "invalid-duration",
				},
			},
			status: &status.SyncStatus{
				LastSyncTime: nil,
			},
			expectedSyncNeeded: false,
			expectError:        true,
		},
		{
			name: "no last sync time - sync needed",
			config: &config.Config{
				SyncPolicy: &config.SyncPolicyConfig{
					Interval: "1h",
				},
			},
			status: &status.SyncStatus{
				LastSyncTime: nil,
			},
			expectedSyncNeeded: true,
			expectedNextTimeFunc: func(nextTime time.Time) bool {
				// Should be approximately now + 1 hour
				expected := now.Add(time.Hour)
				return nextTime.After(expected.Add(-time.Minute)) && nextTime.Before(expected.Add(time.Minute))
			},
			expectError: false,
		},
		{
			name: "last sync time in past - sync needed",
			config: &config.Config{
				SyncPolicy: &config.SyncPolicyConfig{
					Interval: "30m",
				},
			},
			status: &status.SyncStatus{
				LastSyncTime: &oneHourAgo, // 1 hour ago
			},
			expectedSyncNeeded: true,
			expectedNextTimeFunc: func(nextTime time.Time) bool {
				// Should be approximately now + 30 minutes
				expected := now.Add(30 * time.Minute)
				return nextTime.After(expected.Add(-time.Minute)) && nextTime.Before(expected.Add(time.Minute))
			},
			expectError: false,
		},
		{
			name: "last sync time recent - sync not needed",
			config: &config.Config{
				SyncPolicy: &config.SyncPolicyConfig{
					Interval: "1h",
				},
			},
			status: &status.SyncStatus{
				LastAttempt:  &thirtyMinutesAgo, // 30 minutes ago
				LastSyncTime: &thirtyMinutesAgo, // 30 minutes ago
			},
			expectedSyncNeeded: false,
			expectedNextTimeFunc: func(nextTime time.Time) bool {
				// Should be approximately now + 30 minutes (lastSync + 1h)
				expected := now.Add(30 * time.Minute)
				return nextTime.After(expected.Add(-time.Minute)) && nextTime.Before(expected.Add(time.Minute))
			},
			expectError: false,
		},
		{
			name: "last sync time exactly at interval - sync needed",
			config: &config.Config{
				SyncPolicy: &config.SyncPolicyConfig{
					Interval: "1h",
				},
			},
			status: &status.SyncStatus{
				LastSyncTime: &oneHourAgo, // Exactly 1 hour ago
			},
			expectedSyncNeeded: true,
			expectedNextTimeFunc: func(nextTime time.Time) bool {
				// Should be approximately now + 1 hour
				expected := now.Add(time.Hour)
				return nextTime.After(expected.Add(-time.Minute)) && nextTime.Before(expected.Add(time.Minute))
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			checker := &DefaultAutomaticSyncChecker{}
			syncNeeded, nextSyncTime, err := checker.IsIntervalSyncNeeded(tt.config, tt.status)

			assert.Equal(t, tt.expectedSyncNeeded, syncNeeded, "Sync needed result should match expected")

			if tt.expectError {
				assert.Error(t, err, "Expected an error")
			} else {
				assert.NoError(t, err, "Should not have an error")

				if tt.expectedNextTimeFunc != nil {
					assert.True(t, tt.expectedNextTimeFunc(nextSyncTime),
						"Next sync time should be within expected range. Got: %v", nextSyncTime)
				}

				// Verify nextSyncTime is always in the future (this was the bug we fixed)
				assert.True(t, nextSyncTime.After(time.Now()),
					"Next sync time should always be in the future. Got: %v", nextSyncTime)
			}
		})
	}
}
