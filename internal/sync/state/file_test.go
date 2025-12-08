package state

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/status"
	statusmocks "github.com/stacklok/toolhive-registry-server/internal/status/mocks"
)

const testMessageModified = "Modified"

func TestNewFileStateService(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockPersistence := statusmocks.NewMockStatusPersistence(ctrl)

	service := NewFileStateService(mockPersistence)
	require.NotNil(t, service)

	// Verify it's the correct type
	fileService, ok := service.(*fileStateService)
	require.True(t, ok)
	assert.Equal(t, mockPersistence, fileService.statusPersistence)
	assert.NotNil(t, fileService.cachedStatuses)
}

func TestFileStateService_Initialize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		registryConfigs []config.RegistryConfig
		setupMocks      func(*statusmocks.MockStatusPersistence)
		wantErr         bool
		expectedCalls   int
	}{
		{
			name: "successful initialization with multiple registries",
			registryConfigs: []config.RegistryConfig{
				{Name: "registry1"},
				{Name: "registry2"},
				{Name: "registry3"},
			},
			setupMocks: func(m *statusmocks.MockStatusPersistence) {
				// Each registry should trigger loadOrInitializeRegistryStatus
				syncTime := time.Now()
				m.EXPECT().LoadStatus(gomock.Any(), "registry1").Return(&status.SyncStatus{
					Phase:        status.SyncPhaseComplete,
					LastSyncTime: &syncTime,
					ServerCount:  5,
				}, nil)
				m.EXPECT().LoadStatus(gomock.Any(), "registry2").Return(&status.SyncStatus{
					Phase:       status.SyncPhaseFailed,
					Message:     "Previous error",
					ServerCount: 0,
				}, nil)
				m.EXPECT().LoadStatus(gomock.Any(), "registry3").Return(&status.SyncStatus{
					Phase: status.SyncPhaseSyncing, // Will be reset to Failed
				}, nil)
				// Expect SaveStatus for registry3 due to interrupted sync
				m.EXPECT().SaveStatus(gomock.Any(), "registry3", gomock.Any()).Return(nil)
			},
			wantErr: false,
		},
		{
			name:            "successful initialization with empty registry list",
			registryConfigs: []config.RegistryConfig{},
			setupMocks:      func(_ *statusmocks.MockStatusPersistence) {},
			wantErr:         false,
		},
		{
			name: "handles load errors gracefully",
			registryConfigs: []config.RegistryConfig{
				{Name: "registry1"},
			},
			setupMocks: func(m *statusmocks.MockStatusPersistence) {
				m.EXPECT().LoadStatus(gomock.Any(), "registry1").Return(nil, errors.New("load error"))
				// No SaveStatus call expected - the default status with Phase="Failed" won't trigger a save
			},
			wantErr: false,
		},
		{
			name: "handles new registry (no previous status)",
			registryConfigs: []config.RegistryConfig{
				{Name: "new-registry"},
			},
			setupMocks: func(m *statusmocks.MockStatusPersistence) {
				// Return empty status indicating no file existed
				m.EXPECT().LoadStatus(gomock.Any(), "new-registry").Return(&status.SyncStatus{}, nil)
				// Should save default status
				m.EXPECT().SaveStatus(gomock.Any(), "new-registry", gomock.Any()).DoAndReturn(
					func(_ context.Context, _ string, s *status.SyncStatus) error {
						assert.Equal(t, status.SyncPhaseFailed, s.Phase)
						assert.Equal(t, "No previous sync status found", s.Message)
						return nil
					})
			},
			wantErr: false,
		},
		{
			name: "initializes managed registry with Complete status",
			registryConfigs: []config.RegistryConfig{
				{Name: "managed-registry", Managed: &config.ManagedConfig{}},
			},
			setupMocks: func(m *statusmocks.MockStatusPersistence) {
				// Return empty status indicating no file existed
				m.EXPECT().LoadStatus(gomock.Any(), "managed-registry").Return(&status.SyncStatus{}, nil)
				// Should save default status with Complete phase
				m.EXPECT().SaveStatus(gomock.Any(), "managed-registry", gomock.Any()).DoAndReturn(
					func(_ context.Context, _ string, s *status.SyncStatus) error {
						assert.Equal(t, status.SyncPhaseComplete, s.Phase)
						assert.Equal(t, "Non-synced registry (type: managed)", s.Message)
						return nil
					})
			},
			wantErr: false,
		},
		{
			name: "initializes kubernetes registry with Complete status",
			registryConfigs: []config.RegistryConfig{
				{Name: "kubernetes-registry", Kubernetes: &config.KubernetesConfig{}},
			},
			setupMocks: func(m *statusmocks.MockStatusPersistence) {
				// Return empty status indicating no file existed
				m.EXPECT().LoadStatus(gomock.Any(), "kubernetes-registry").Return(&status.SyncStatus{}, nil)
				// Should save default status with Complete phase
				m.EXPECT().SaveStatus(gomock.Any(), "kubernetes-registry", gomock.Any()).DoAndReturn(
					func(_ context.Context, _ string, s *status.SyncStatus) error {
						assert.Equal(t, status.SyncPhaseComplete, s.Phase)
						assert.Equal(t, "Non-synced registry (type: kubernetes)", s.Message)
						return nil
					})
			},
			wantErr: false,
		},
		{
			name: "handles mixed managed and non-managed registries",
			registryConfigs: []config.RegistryConfig{
				{Name: "git-registry", Git: &config.GitConfig{Repository: "https://example.com"}},
				{Name: "managed-registry", Managed: &config.ManagedConfig{}},
			},
			setupMocks: func(m *statusmocks.MockStatusPersistence) {
				// Git registry - empty status gets Failed
				m.EXPECT().LoadStatus(gomock.Any(), "git-registry").Return(&status.SyncStatus{}, nil)
				m.EXPECT().SaveStatus(gomock.Any(), "git-registry", gomock.Any()).DoAndReturn(
					func(_ context.Context, _ string, s *status.SyncStatus) error {
						assert.Equal(t, status.SyncPhaseFailed, s.Phase)
						return nil
					})
				// Managed registry - empty status gets Complete
				m.EXPECT().LoadStatus(gomock.Any(), "managed-registry").Return(&status.SyncStatus{}, nil)
				m.EXPECT().SaveStatus(gomock.Any(), "managed-registry", gomock.Any()).DoAndReturn(
					func(_ context.Context, _ string, s *status.SyncStatus) error {
						assert.Equal(t, status.SyncPhaseComplete, s.Phase)
						return nil
					})
			},
			wantErr: false,
		},
		{
			name: "handles mixed synced and non-synced registries including kubernetes",
			registryConfigs: []config.RegistryConfig{
				{Name: "git-registry", Git: &config.GitConfig{Repository: "https://example.com"}},
				{Name: "managed-registry", Managed: &config.ManagedConfig{}},
				{Name: "kubernetes-registry", Kubernetes: &config.KubernetesConfig{}},
			},
			setupMocks: func(m *statusmocks.MockStatusPersistence) {
				// Git registry - empty status gets Failed
				m.EXPECT().LoadStatus(gomock.Any(), "git-registry").Return(&status.SyncStatus{}, nil)
				m.EXPECT().SaveStatus(gomock.Any(), "git-registry", gomock.Any()).DoAndReturn(
					func(_ context.Context, _ string, s *status.SyncStatus) error {
						assert.Equal(t, status.SyncPhaseFailed, s.Phase)
						return nil
					})
				// Managed registry - empty status gets Complete
				m.EXPECT().LoadStatus(gomock.Any(), "managed-registry").Return(&status.SyncStatus{}, nil)
				m.EXPECT().SaveStatus(gomock.Any(), "managed-registry", gomock.Any()).DoAndReturn(
					func(_ context.Context, _ string, s *status.SyncStatus) error {
						assert.Equal(t, status.SyncPhaseComplete, s.Phase)
						return nil
					})
				// Kubernetes registry - empty status gets Complete
				m.EXPECT().LoadStatus(gomock.Any(), "kubernetes-registry").Return(&status.SyncStatus{}, nil)
				m.EXPECT().SaveStatus(gomock.Any(), "kubernetes-registry", gomock.Any()).DoAndReturn(
					func(_ context.Context, _ string, s *status.SyncStatus) error {
						assert.Equal(t, status.SyncPhaseComplete, s.Phase)
						return nil
					})
			},
			wantErr: false,
		},
		{
			name: "managed registry with load error gets Complete status",
			registryConfigs: []config.RegistryConfig{
				{Name: "managed-registry", Managed: &config.ManagedConfig{}},
			},
			setupMocks: func(m *statusmocks.MockStatusPersistence) {
				// Load error should initialize with Complete for managed registries
				m.EXPECT().LoadStatus(gomock.Any(), "managed-registry").Return(nil, errors.New("load error"))
			},
			wantErr: false,
		},
		{
			name: "kubernetes registry with load error gets Complete status",
			registryConfigs: []config.RegistryConfig{
				{Name: "kubernetes-registry", Kubernetes: &config.KubernetesConfig{}},
			},
			setupMocks: func(m *statusmocks.MockStatusPersistence) {
				// Load error should initialize with Complete for kubernetes registries
				m.EXPECT().LoadStatus(gomock.Any(), "kubernetes-registry").Return(nil, errors.New("load error"))
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockPersistence := statusmocks.NewMockStatusPersistence(ctrl)
			if tt.setupMocks != nil {
				tt.setupMocks(mockPersistence)
			}

			service := NewFileStateService(mockPersistence).(*fileStateService)
			ctx := context.Background()

			err := service.Initialize(ctx, tt.registryConfigs)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				// Verify all registries are in cache
				assert.Len(t, service.cachedStatuses, len(tt.registryConfigs))
			}
		})
	}
}

func TestFileStateService_ListSyncStatuses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		cachedStatuses map[string]*status.SyncStatus
		want           map[string]*status.SyncStatus
		wantErr        bool
	}{
		{
			name: "returns deep copy of all statuses",
			cachedStatuses: map[string]*status.SyncStatus{
				"registry1": {
					Phase:       status.SyncPhaseComplete,
					Message:     "Success",
					ServerCount: 10,
				},
				"registry2": {
					Phase:       status.SyncPhaseFailed,
					Message:     "Error",
					ServerCount: 0,
				},
			},
			want: map[string]*status.SyncStatus{
				"registry1": {
					Phase:       status.SyncPhaseComplete,
					Message:     "Success",
					ServerCount: 10,
				},
				"registry2": {
					Phase:       status.SyncPhaseFailed,
					Message:     "Error",
					ServerCount: 0,
				},
			},
			wantErr: false,
		},
		{
			name:           "returns empty map when no statuses cached",
			cachedStatuses: map[string]*status.SyncStatus{},
			want:           map[string]*status.SyncStatus{},
			wantErr:        false,
		},
		{
			name: "filters out nil statuses",
			cachedStatuses: map[string]*status.SyncStatus{
				"registry1": {
					Phase: status.SyncPhaseComplete,
				},
				"registry2": nil,
				"registry3": {
					Phase: status.SyncPhaseFailed,
				},
			},
			want: map[string]*status.SyncStatus{
				"registry1": {
					Phase: status.SyncPhaseComplete,
				},
				"registry3": {
					Phase: status.SyncPhaseFailed,
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockPersistence := statusmocks.NewMockStatusPersistence(ctrl)
			service := NewFileStateService(mockPersistence).(*fileStateService)
			service.cachedStatuses = tt.cachedStatuses

			ctx := context.Background()
			got, err := service.ListSyncStatuses(ctx)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.want, got)

				// Verify deep copy - modifications to returned map don't affect internal state
				if len(got) > 0 {
					for name, status := range got {
						if status != nil {
							status.Message = testMessageModified
							// Original should be unchanged
							if original := service.cachedStatuses[name]; original != nil {
								assert.NotEqual(t, testMessageModified, original.Message)
							}
						}
					}
				}
			}
		})
	}
}

func TestFileStateService_GetSyncStatus(t *testing.T) {
	t.Parallel()

	syncTime := time.Now()

	tests := []struct {
		name           string
		registryName   string
		cachedStatuses map[string]*status.SyncStatus
		want           *status.SyncStatus
		wantErr        bool
	}{
		{
			name:         "returns copy of existing status",
			registryName: "registry1",
			cachedStatuses: map[string]*status.SyncStatus{
				"registry1": {
					Phase:        status.SyncPhaseComplete,
					Message:      "Success",
					ServerCount:  10,
					LastSyncTime: &syncTime,
				},
			},
			want: &status.SyncStatus{
				Phase:        status.SyncPhaseComplete,
				Message:      "Success",
				ServerCount:  10,
				LastSyncTime: &syncTime,
			},
			wantErr: false,
		},
		{
			name:         "returns nil for non-existent registry",
			registryName: "non-existent",
			cachedStatuses: map[string]*status.SyncStatus{
				"registry1": {Phase: status.SyncPhaseComplete},
			},
			want:    nil,
			wantErr: false,
		},
		{
			name:         "returns nil for nil status",
			registryName: "registry1",
			cachedStatuses: map[string]*status.SyncStatus{
				"registry1": nil,
			},
			want:    nil,
			wantErr: false,
		},
		{
			name:           "returns nil from empty cache",
			registryName:   "registry1",
			cachedStatuses: map[string]*status.SyncStatus{},
			want:           nil,
			wantErr:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockPersistence := statusmocks.NewMockStatusPersistence(ctrl)
			service := NewFileStateService(mockPersistence).(*fileStateService)
			service.cachedStatuses = tt.cachedStatuses

			ctx := context.Background()
			got, err := service.GetSyncStatus(ctx, tt.registryName)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.want, got)

				// Verify deep copy - modifications to returned status don't affect internal state
				if got != nil {
					originalMessage := got.Message
					got.Message = testMessageModified
					if cached := service.cachedStatuses[tt.registryName]; cached != nil {
						assert.Equal(t, originalMessage, cached.Message)
					}
				}
			}
		})
	}
}

func TestFileStateService_UpdateSyncStatus(t *testing.T) {
	t.Parallel()

	syncTime := time.Now()

	tests := []struct {
		name         string
		registryName string
		newStatus    *status.SyncStatus
		setupMocks   func(*statusmocks.MockStatusPersistence)
		wantErr      bool
		errMessage   string
	}{
		{
			name:         "successfully updates status",
			registryName: "registry1",
			newStatus: &status.SyncStatus{
				Phase:        status.SyncPhaseComplete,
				Message:      "Updated",
				ServerCount:  15,
				LastSyncTime: &syncTime,
			},
			setupMocks: func(m *statusmocks.MockStatusPersistence) {
				m.EXPECT().SaveStatus(gomock.Any(), "registry1", gomock.Any()).Return(nil)
			},
			wantErr: false,
		},
		{
			name:         "handles save error",
			registryName: "registry1",
			newStatus: &status.SyncStatus{
				Phase: status.SyncPhaseFailed,
			},
			setupMocks: func(m *statusmocks.MockStatusPersistence) {
				m.EXPECT().SaveStatus(gomock.Any(), "registry1", gomock.Any()).
					Return(errors.New("save failed"))
			},
			wantErr:    true,
			errMessage: "save failed",
		},
		{
			name:         "updates cache after successful save",
			registryName: "new-registry",
			newStatus: &status.SyncStatus{
				Phase:       status.SyncPhaseComplete,
				ServerCount: 20,
			},
			setupMocks: func(m *statusmocks.MockStatusPersistence) {
				m.EXPECT().SaveStatus(gomock.Any(), "new-registry", gomock.Any()).Return(nil)
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockPersistence := statusmocks.NewMockStatusPersistence(ctrl)
			if tt.setupMocks != nil {
				tt.setupMocks(mockPersistence)
			}

			service := NewFileStateService(mockPersistence).(*fileStateService)
			ctx := context.Background()

			err := service.UpdateSyncStatus(ctx, tt.registryName, tt.newStatus)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMessage != "" {
					assert.Contains(t, err.Error(), tt.errMessage)
				}
				// Cache should not be updated on error
				_, exists := service.cachedStatuses[tt.registryName]
				assert.False(t, exists)
			} else {
				assert.NoError(t, err)
				// Verify cache was updated
				cached := service.cachedStatuses[tt.registryName]
				assert.Equal(t, tt.newStatus, cached)
			}
		})
	}
}

func TestFileStateService_loadOrInitializeRegistryStatus(t *testing.T) {
	t.Parallel()

	syncTime := time.Now()

	tests := []struct {
		name         string
		registryName string
		isNonSynced  bool
		regType      string
		setupMocks   func(*statusmocks.MockStatusPersistence)
		verifyCached func(*testing.T, *status.SyncStatus)
	}{
		{
			name:         "loads existing complete status",
			registryName: "registry1",
			isNonSynced:  false,
			regType:      config.SourceTypeGit,
			setupMocks: func(m *statusmocks.MockStatusPersistence) {
				m.EXPECT().LoadStatus(gomock.Any(), "registry1").Return(&status.SyncStatus{
					Phase:        status.SyncPhaseComplete,
					Message:      "All good",
					LastSyncTime: &syncTime,
					ServerCount:  10,
				}, nil)
			},
			verifyCached: func(t *testing.T, s *status.SyncStatus) {
				t.Helper()
				assert.Equal(t, status.SyncPhaseComplete, s.Phase)
				assert.Equal(t, "All good", s.Message)
				assert.Equal(t, 10, s.ServerCount)
			},
		},
		{
			name:         "handles load error and initializes defaults",
			registryName: "registry1",
			isNonSynced:  false,
			regType:      config.SourceTypeGit,
			setupMocks: func(m *statusmocks.MockStatusPersistence) {
				m.EXPECT().LoadStatus(gomock.Any(), "registry1").Return(nil, errors.New("load error"))
				// No SaveStatus call expected - the default status with Phase="Failed" won't trigger a save
			},
			verifyCached: func(t *testing.T, s *status.SyncStatus) {
				t.Helper()
				assert.Equal(t, status.SyncPhaseFailed, s.Phase)
				assert.Equal(t, "No previous sync status found", s.Message)
			},
		},
		{
			name:         "initializes empty status (first run)",
			registryName: "new-registry",
			isNonSynced:  false,
			regType:      config.SourceTypeGit,
			setupMocks: func(m *statusmocks.MockStatusPersistence) {
				// Return empty status (no phase, no LastSyncTime)
				m.EXPECT().LoadStatus(gomock.Any(), "new-registry").Return(&status.SyncStatus{}, nil)
				m.EXPECT().SaveStatus(gomock.Any(), "new-registry", gomock.Any()).DoAndReturn(
					func(_ context.Context, _ string, s *status.SyncStatus) error {
						assert.Equal(t, status.SyncPhaseFailed, s.Phase)
						assert.Equal(t, "No previous sync status found", s.Message)
						return nil
					})
			},
			verifyCached: func(t *testing.T, s *status.SyncStatus) {
				t.Helper()
				assert.Equal(t, status.SyncPhaseFailed, s.Phase)
				assert.Equal(t, "No previous sync status found", s.Message)
				assert.Nil(t, s.LastSyncTime)
			},
		},
		{
			name:         "resets interrupted sync (status=Syncing)",
			registryName: "registry1",
			isNonSynced:  false,
			regType:      config.SourceTypeGit,
			setupMocks: func(m *statusmocks.MockStatusPersistence) {
				m.EXPECT().LoadStatus(gomock.Any(), "registry1").Return(&status.SyncStatus{
					Phase:        status.SyncPhaseSyncing,
					Message:      "In progress",
					LastSyncTime: &syncTime,
					ServerCount:  5,
				}, nil)
				m.EXPECT().SaveStatus(gomock.Any(), "registry1", gomock.Any()).DoAndReturn(
					func(_ context.Context, _ string, s *status.SyncStatus) error {
						assert.Equal(t, status.SyncPhaseFailed, s.Phase)
						assert.Equal(t, "Previous sync was interrupted", s.Message)
						// Should preserve other fields
						assert.Equal(t, 5, s.ServerCount)
						assert.NotNil(t, s.LastSyncTime)
						return nil
					})
			},
			verifyCached: func(t *testing.T, s *status.SyncStatus) {
				t.Helper()
				assert.Equal(t, status.SyncPhaseFailed, s.Phase)
				assert.Equal(t, "Previous sync was interrupted", s.Message)
				assert.Equal(t, 5, s.ServerCount)
			},
		},
		{
			name:         "handles save error for empty status gracefully",
			registryName: "registry1",
			isNonSynced:  false,
			regType:      config.SourceTypeGit,
			setupMocks: func(m *statusmocks.MockStatusPersistence) {
				m.EXPECT().LoadStatus(gomock.Any(), "registry1").Return(&status.SyncStatus{}, nil)
				m.EXPECT().SaveStatus(gomock.Any(), "registry1", gomock.Any()).Return(errors.New("save error"))
			},
			verifyCached: func(t *testing.T, s *status.SyncStatus) {
				t.Helper()
				// Should still cache the status even if save fails
				assert.Equal(t, status.SyncPhaseFailed, s.Phase)
				assert.Equal(t, "No previous sync status found", s.Message)
			},
		},
		{
			name:         "handles save error for interrupted sync gracefully",
			registryName: "registry1",
			isNonSynced:  false,
			regType:      config.SourceTypeGit,
			setupMocks: func(m *statusmocks.MockStatusPersistence) {
				m.EXPECT().LoadStatus(gomock.Any(), "registry1").Return(&status.SyncStatus{
					Phase: status.SyncPhaseSyncing,
				}, nil)
				m.EXPECT().SaveStatus(gomock.Any(), "registry1", gomock.Any()).Return(errors.New("save error"))
			},
			verifyCached: func(t *testing.T, s *status.SyncStatus) {
				t.Helper()
				// Should still cache the corrected status even if save fails
				assert.Equal(t, status.SyncPhaseFailed, s.Phase)
				assert.Equal(t, "Previous sync was interrupted", s.Message)
			},
		},
		// Managed registry test cases
		{
			name:         "managed registry with load error initializes with Complete status",
			registryName: "managed-registry",
			isNonSynced:  true,
			regType:      config.SourceTypeManaged,
			setupMocks: func(m *statusmocks.MockStatusPersistence) {
				m.EXPECT().LoadStatus(gomock.Any(), "managed-registry").Return(nil, errors.New("load error"))
			},
			verifyCached: func(t *testing.T, s *status.SyncStatus) {
				t.Helper()
				assert.Equal(t, status.SyncPhaseComplete, s.Phase)
				assert.Equal(t, "Non-synced registry (type: managed)", s.Message)
			},
		},
		{
			name:         "managed registry with empty status initializes with Complete",
			registryName: "managed-registry",
			isNonSynced:  true,
			regType:      config.SourceTypeManaged,
			setupMocks: func(m *statusmocks.MockStatusPersistence) {
				m.EXPECT().LoadStatus(gomock.Any(), "managed-registry").Return(&status.SyncStatus{}, nil)
				m.EXPECT().SaveStatus(gomock.Any(), "managed-registry", gomock.Any()).DoAndReturn(
					func(_ context.Context, _ string, s *status.SyncStatus) error {
						assert.Equal(t, status.SyncPhaseComplete, s.Phase)
						assert.Equal(t, "Non-synced registry (type: managed)", s.Message)
						return nil
					})
			},
			verifyCached: func(t *testing.T, s *status.SyncStatus) {
				t.Helper()
				assert.Equal(t, status.SyncPhaseComplete, s.Phase)
				assert.Equal(t, "Non-synced registry (type: managed)", s.Message)
			},
		},
		{
			name:         "managed registry with Syncing status is NOT reset",
			registryName: "managed-registry",
			isNonSynced:  true,
			regType:      config.SourceTypeManaged,
			setupMocks: func(m *statusmocks.MockStatusPersistence) {
				m.EXPECT().LoadStatus(gomock.Any(), "managed-registry").Return(&status.SyncStatus{
					Phase:       status.SyncPhaseSyncing,
					Message:     "In progress",
					ServerCount: 10,
				}, nil)
				// No SaveStatus call expected - non-synced registries don't reset Syncing status
			},
			verifyCached: func(t *testing.T, s *status.SyncStatus) {
				t.Helper()
				// Should keep the Syncing status for non-synced registries
				assert.Equal(t, status.SyncPhaseSyncing, s.Phase)
				assert.Equal(t, "In progress", s.Message)
				assert.Equal(t, 10, s.ServerCount)
			},
		},
		{
			name:         "managed registry loads existing status correctly",
			registryName: "managed-registry",
			isNonSynced:  true,
			regType:      config.SourceTypeManaged,
			setupMocks: func(m *statusmocks.MockStatusPersistence) {
				m.EXPECT().LoadStatus(gomock.Any(), "managed-registry").Return(&status.SyncStatus{
					Phase:        status.SyncPhaseComplete,
					Message:      "API managed",
					LastSyncTime: &syncTime,
					ServerCount:  15,
				}, nil)
			},
			verifyCached: func(t *testing.T, s *status.SyncStatus) {
				t.Helper()
				assert.Equal(t, status.SyncPhaseComplete, s.Phase)
				assert.Equal(t, "API managed", s.Message)
				assert.Equal(t, 15, s.ServerCount)
			},
		},
		// Kubernetes registry test cases
		{
			name:         "kubernetes registry with load error initializes with Complete status",
			registryName: "kubernetes-registry",
			isNonSynced:  true,
			regType:      config.SourceTypeKubernetes,
			setupMocks: func(m *statusmocks.MockStatusPersistence) {
				m.EXPECT().LoadStatus(gomock.Any(), "kubernetes-registry").Return(nil, errors.New("load error"))
			},
			verifyCached: func(t *testing.T, s *status.SyncStatus) {
				t.Helper()
				assert.Equal(t, status.SyncPhaseComplete, s.Phase)
				assert.Equal(t, "Non-synced registry (type: kubernetes)", s.Message)
			},
		},
		{
			name:         "kubernetes registry with empty status initializes with Complete",
			registryName: "kubernetes-registry",
			isNonSynced:  true,
			regType:      config.SourceTypeKubernetes,
			setupMocks: func(m *statusmocks.MockStatusPersistence) {
				m.EXPECT().LoadStatus(gomock.Any(), "kubernetes-registry").Return(&status.SyncStatus{}, nil)
				m.EXPECT().SaveStatus(gomock.Any(), "kubernetes-registry", gomock.Any()).DoAndReturn(
					func(_ context.Context, _ string, s *status.SyncStatus) error {
						assert.Equal(t, status.SyncPhaseComplete, s.Phase)
						assert.Equal(t, "Non-synced registry (type: kubernetes)", s.Message)
						return nil
					})
			},
			verifyCached: func(t *testing.T, s *status.SyncStatus) {
				t.Helper()
				assert.Equal(t, status.SyncPhaseComplete, s.Phase)
				assert.Equal(t, "Non-synced registry (type: kubernetes)", s.Message)
			},
		},
		{
			name:         "kubernetes registry with Syncing status is NOT reset",
			registryName: "kubernetes-registry",
			isNonSynced:  true,
			regType:      config.SourceTypeKubernetes,
			setupMocks: func(m *statusmocks.MockStatusPersistence) {
				m.EXPECT().LoadStatus(gomock.Any(), "kubernetes-registry").Return(&status.SyncStatus{
					Phase:       status.SyncPhaseSyncing,
					Message:     "In progress",
					ServerCount: 10,
				}, nil)
				// No SaveStatus call expected - non-synced registries don't reset Syncing status
			},
			verifyCached: func(t *testing.T, s *status.SyncStatus) {
				t.Helper()
				// Should keep the Syncing status for non-synced registries
				assert.Equal(t, status.SyncPhaseSyncing, s.Phase)
				assert.Equal(t, "In progress", s.Message)
				assert.Equal(t, 10, s.ServerCount)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockPersistence := statusmocks.NewMockStatusPersistence(ctrl)
			if tt.setupMocks != nil {
				tt.setupMocks(mockPersistence)
			}

			service := NewFileStateService(mockPersistence).(*fileStateService)
			ctx := context.Background()

			// Call the private method
			service.loadOrInitializeRegistryStatus(ctx, tt.registryName, tt.isNonSynced, tt.regType)

			// Verify the cached status
			cached := service.cachedStatuses[tt.registryName]
			require.NotNil(t, cached)
			if tt.verifyCached != nil {
				tt.verifyCached(t, cached)
			}
		})
	}
}

func TestFileStateService_DeepCopyBehavior(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	t.Cleanup(func() { ctrl.Finish() })

	mockPersistence := statusmocks.NewMockStatusPersistence(ctrl)
	service := NewFileStateService(mockPersistence).(*fileStateService)

	// Set up initial status
	syncTime := time.Now()
	originalStatus := &status.SyncStatus{
		Phase:        status.SyncPhaseComplete,
		Message:      "Original",
		ServerCount:  10,
		LastSyncTime: &syncTime,
		LastSyncHash: "abc123",
	}
	service.cachedStatuses = map[string]*status.SyncStatus{
		"registry1": originalStatus,
	}

	ctx := context.Background()

	t.Run("GetSyncStatus returns deep copy", func(t *testing.T) {
		t.Parallel()

		retrieved, err := service.GetSyncStatus(ctx, "registry1")
		require.NoError(t, err)
		require.NotNil(t, retrieved)

		// Modify the retrieved status
		retrieved.Message = testMessageModified
		retrieved.ServerCount = 20
		retrieved.Phase = status.SyncPhaseFailed
		if retrieved.LastSyncTime != nil {
			newTime := time.Now().Add(time.Hour)
			retrieved.LastSyncTime = &newTime
		}

		// Verify original is unchanged
		assert.Equal(t, "Original", originalStatus.Message)
		assert.Equal(t, 10, originalStatus.ServerCount)
		assert.Equal(t, status.SyncPhaseComplete, originalStatus.Phase)
		assert.Equal(t, syncTime, *originalStatus.LastSyncTime)
	})

	t.Run("ListSyncStatuses returns deep copies", func(t *testing.T) {
		t.Parallel()

		statuses, err := service.ListSyncStatuses(ctx)
		require.NoError(t, err)
		require.NotEmpty(t, statuses)

		// Modify all returned statuses
		for name, s := range statuses {
			if s != nil {
				s.Message = testMessageModified + " " + name
				s.ServerCount = 999
				s.Phase = status.SyncPhaseSyncing
			}
		}

		// Verify originals are unchanged
		assert.Equal(t, "Original", service.cachedStatuses["registry1"].Message)
		assert.Equal(t, 10, service.cachedStatuses["registry1"].ServerCount)
		assert.Equal(t, status.SyncPhaseComplete, service.cachedStatuses["registry1"].Phase)
	})
}
