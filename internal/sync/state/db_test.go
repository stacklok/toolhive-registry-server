package state

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/db/sqlc"
	"github.com/stacklok/toolhive-registry-server/internal/status"
)

func TestNewDBStateService(t *testing.T) {
	t.Parallel()

	// Test with nil pool
	service := NewDBStateService(nil)
	require.NotNil(t, service)

	// Verify it's the correct type
	dbService, ok := service.(*dbStatusService)
	require.True(t, ok)
	assert.Nil(t, dbService.pool)
}

// Note: Initialize, ListSyncStatuses, GetSyncStatus, UpdateSyncStatus, and UpdateStatusAtomically
// require database integration testing with a real database connection or testcontainers.
// These are better tested as integration tests rather than unit tests with mocks.
// The helper functions and conversion logic are tested below.

func TestMapConfigTypeToDBType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		configType string
		want       sqlc.RegistryType
	}{
		{
			name:       "maps git to REMOTE",
			configType: config.SourceTypeGit,
			want:       sqlc.RegistryTypeREMOTE,
		},
		{
			name:       "maps api to REMOTE",
			configType: config.SourceTypeAPI,
			want:       sqlc.RegistryTypeREMOTE,
		},
		{
			name:       "maps file to FILE",
			configType: config.SourceTypeFile,
			want:       sqlc.RegistryTypeFILE,
		},
		{
			name:       "maps managed to LOCAL",
			configType: config.SourceTypeManaged,
			want:       sqlc.RegistryTypeLOCAL,
		},
		{
			name:       "maps unknown to LOCAL",
			configType: "unknown",
			want:       sqlc.RegistryTypeLOCAL,
		},
		{
			name:       "maps empty string to LOCAL",
			configType: "",
			want:       sqlc.RegistryTypeLOCAL,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := mapConfigTypeToDBType(tt.configType)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGetInitialSyncStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		isManaged      bool
		wantStatus     sqlc.SyncStatus
		wantErrMessage string
	}{
		{
			name:           "managed registry returns COMPLETED",
			isManaged:      true,
			wantStatus:     sqlc.SyncStatusCOMPLETED,
			wantErrMessage: "Managed registry (data managed via API)",
		},
		{
			name:           "non-managed registry returns FAILED",
			isManaged:      false,
			wantStatus:     sqlc.SyncStatusFAILED,
			wantErrMessage: "No previous sync status found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotStatus, gotMessage := getInitialSyncStatus(tt.isManaged)
			assert.Equal(t, tt.wantStatus, gotStatus)
			assert.Equal(t, tt.wantErrMessage, gotMessage)
		})
	}
}

func TestSyncPhaseToDBStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		phase status.SyncPhase
		want  sqlc.SyncStatus
	}{
		{
			name:  "converts Syncing to IN_PROGRESS",
			phase: status.SyncPhaseSyncing,
			want:  sqlc.SyncStatusINPROGRESS,
		},
		{
			name:  "converts Complete to COMPLETED",
			phase: status.SyncPhaseComplete,
			want:  sqlc.SyncStatusCOMPLETED,
		},
		{
			name:  "converts Failed to FAILED",
			phase: status.SyncPhaseFailed,
			want:  sqlc.SyncStatusFAILED,
		},
		{
			name:  "converts unknown to FAILED",
			phase: status.SyncPhase("unknown"),
			want:  sqlc.SyncStatusFAILED,
		},
		{
			name:  "converts empty string to FAILED",
			phase: status.SyncPhase(""),
			want:  sqlc.SyncStatusFAILED,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := syncPhaseToDBStatus(tt.phase)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDBSyncStatusToPhase(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		dbStatus sqlc.SyncStatus
		want     status.SyncPhase
	}{
		{
			name:     "converts IN_PROGRESS to Syncing",
			dbStatus: sqlc.SyncStatusINPROGRESS,
			want:     status.SyncPhaseSyncing,
		},
		{
			name:     "converts COMPLETED to Complete",
			dbStatus: sqlc.SyncStatusCOMPLETED,
			want:     status.SyncPhaseComplete,
		},
		{
			name:     "converts FAILED to Failed",
			dbStatus: sqlc.SyncStatusFAILED,
			want:     status.SyncPhaseFailed,
		},
		{
			name:     "converts unknown to Failed",
			dbStatus: sqlc.SyncStatus("unknown"),
			want:     status.SyncPhaseFailed,
		},
		{
			name:     "converts empty string to Failed",
			dbStatus: sqlc.SyncStatus(""),
			want:     status.SyncPhaseFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := dbSyncStatusToPhase(tt.dbStatus)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDBSyncToStatus(t *testing.T) {
	t.Parallel()

	syncTime := time.Now()
	attemptTime := time.Now().Add(-time.Hour)
	hash := "abc123"
	filterHash := "def456"
	errorMsg := "Error occurred"

	tests := []struct {
		name   string
		dbSync sqlc.RegistrySync
		want   *status.SyncStatus
	}{
		{
			name: "converts full sync status with all fields",
			dbSync: sqlc.RegistrySync{
				ID:                    uuid.New(),
				RegID:                 uuid.New(),
				SyncStatus:            sqlc.SyncStatusCOMPLETED,
				ErrorMsg:              &errorMsg,
				StartedAt:             &attemptTime,
				EndedAt:               &syncTime,
				AttemptCount:          3,
				LastSyncHash:          &hash,
				LastAppliedFilterHash: &filterHash,
				ServerCount:           10,
			},
			want: &status.SyncStatus{
				Phase:                 status.SyncPhaseComplete,
				Message:               errorMsg,
				LastAttempt:           &attemptTime,
				LastSyncTime:          &syncTime,
				AttemptCount:          3,
				LastSyncHash:          hash,
				LastAppliedFilterHash: filterHash,
				ServerCount:           10,
			},
		},
		{
			name: "converts sync status with nil optional fields",
			dbSync: sqlc.RegistrySync{
				ID:                    uuid.New(),
				RegID:                 uuid.New(),
				SyncStatus:            sqlc.SyncStatusFAILED,
				ErrorMsg:              nil,
				StartedAt:             nil,
				EndedAt:               nil,
				AttemptCount:          0,
				LastSyncHash:          nil,
				LastAppliedFilterHash: nil,
				ServerCount:           0,
			},
			want: &status.SyncStatus{
				Phase:        status.SyncPhaseFailed,
				Message:      "",
				AttemptCount: 0,
				ServerCount:  0,
			},
		},
		{
			name: "converts IN_PROGRESS status",
			dbSync: sqlc.RegistrySync{
				ID:           uuid.New(),
				RegID:        uuid.New(),
				SyncStatus:   sqlc.SyncStatusINPROGRESS,
				ErrorMsg:     nil,
				StartedAt:    &syncTime,
				EndedAt:      nil,
				AttemptCount: 1,
				ServerCount:  5,
			},
			want: &status.SyncStatus{
				Phase:        status.SyncPhaseSyncing,
				Message:      "",
				LastAttempt:  &syncTime,
				AttemptCount: 1,
				ServerCount:  5,
			},
		},
		{
			name: "handles empty string hash values",
			dbSync: sqlc.RegistrySync{
				ID:                    uuid.New(),
				RegID:                 uuid.New(),
				SyncStatus:            sqlc.SyncStatusCOMPLETED,
				LastSyncHash:          &[]string{""}[0],
				LastAppliedFilterHash: &[]string{""}[0],
				ServerCount:           0,
			},
			want: &status.SyncStatus{
				Phase:                 status.SyncPhaseComplete,
				LastSyncHash:          "",
				LastAppliedFilterHash: "",
				ServerCount:           0,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := dbSyncToStatus(tt.dbSync)
			require.NotNil(t, got)
			assert.Equal(t, tt.want.Phase, got.Phase)
			assert.Equal(t, tt.want.Message, got.Message)
			assert.Equal(t, tt.want.AttemptCount, got.AttemptCount)
			assert.Equal(t, tt.want.ServerCount, got.ServerCount)
			assert.Equal(t, tt.want.LastSyncHash, got.LastSyncHash)
			assert.Equal(t, tt.want.LastAppliedFilterHash, got.LastAppliedFilterHash)

			// Compare time pointers
			if tt.want.LastAttempt != nil {
				require.NotNil(t, got.LastAttempt)
				assert.True(t, tt.want.LastAttempt.Equal(*got.LastAttempt))
			} else {
				assert.Nil(t, got.LastAttempt)
			}

			if tt.want.LastSyncTime != nil {
				require.NotNil(t, got.LastSyncTime)
				assert.True(t, tt.want.LastSyncTime.Equal(*got.LastSyncTime))
			} else {
				assert.Nil(t, got.LastSyncTime)
			}
		})
	}
}

func TestDBSyncRowToStatus(t *testing.T) {
	t.Parallel()

	syncTime := time.Now()
	attemptTime := time.Now().Add(-time.Hour)
	hash := "abc123"
	filterHash := "def456"
	errorMsg := "Error occurred"

	tests := []struct {
		name string
		row  sqlc.ListRegistrySyncsRow
		want *status.SyncStatus
	}{
		{
			name: "converts full row with all fields",
			row: sqlc.ListRegistrySyncsRow{
				Name:                  "registry1",
				ID:                    uuid.New(),
				RegID:                 uuid.New(),
				SyncStatus:            sqlc.SyncStatusINPROGRESS,
				ErrorMsg:              &errorMsg,
				StartedAt:             &attemptTime,
				EndedAt:               &syncTime,
				AttemptCount:          2,
				LastSyncHash:          &hash,
				LastAppliedFilterHash: &filterHash,
				ServerCount:           15,
			},
			want: &status.SyncStatus{
				Phase:                 status.SyncPhaseSyncing,
				Message:               errorMsg,
				LastAttempt:           &attemptTime,
				LastSyncTime:          &syncTime,
				AttemptCount:          2,
				LastSyncHash:          hash,
				LastAppliedFilterHash: filterHash,
				ServerCount:           15,
			},
		},
		{
			name: "converts row with nil optional fields",
			row: sqlc.ListRegistrySyncsRow{
				Name:                  "registry2",
				ID:                    uuid.New(),
				RegID:                 uuid.New(),
				SyncStatus:            sqlc.SyncStatusCOMPLETED,
				ErrorMsg:              nil,
				StartedAt:             nil,
				EndedAt:               nil,
				AttemptCount:          0,
				LastSyncHash:          nil,
				LastAppliedFilterHash: nil,
				ServerCount:           0,
			},
			want: &status.SyncStatus{
				Phase:        status.SyncPhaseComplete,
				Message:      "",
				AttemptCount: 0,
				ServerCount:  0,
			},
		},
		{
			name: "converts FAILED row",
			row: sqlc.ListRegistrySyncsRow{
				Name:         "registry3",
				ID:           uuid.New(),
				RegID:        uuid.New(),
				SyncStatus:   sqlc.SyncStatusFAILED,
				ErrorMsg:     &errorMsg,
				StartedAt:    &syncTime,
				AttemptCount: 5,
				ServerCount:  0,
			},
			want: &status.SyncStatus{
				Phase:        status.SyncPhaseFailed,
				Message:      errorMsg,
				LastAttempt:  &syncTime,
				AttemptCount: 5,
				ServerCount:  0,
			},
		},
		{
			name: "handles rows with partial hash data",
			row: sqlc.ListRegistrySyncsRow{
				Name:                  "registry4",
				ID:                    uuid.New(),
				RegID:                 uuid.New(),
				SyncStatus:            sqlc.SyncStatusCOMPLETED,
				LastSyncHash:          &hash,
				LastAppliedFilterHash: nil,
				ServerCount:           20,
			},
			want: &status.SyncStatus{
				Phase:                 status.SyncPhaseComplete,
				LastSyncHash:          hash,
				LastAppliedFilterHash: "",
				ServerCount:           20,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := dbSyncRowToStatus(tt.row)
			require.NotNil(t, got)
			assert.Equal(t, tt.want.Phase, got.Phase)
			assert.Equal(t, tt.want.Message, got.Message)
			assert.Equal(t, tt.want.AttemptCount, got.AttemptCount)
			assert.Equal(t, tt.want.ServerCount, got.ServerCount)
			assert.Equal(t, tt.want.LastSyncHash, got.LastSyncHash)
			assert.Equal(t, tt.want.LastAppliedFilterHash, got.LastAppliedFilterHash)

			// Compare time pointers
			if tt.want.LastAttempt != nil {
				require.NotNil(t, got.LastAttempt)
				assert.True(t, tt.want.LastAttempt.Equal(*got.LastAttempt))
			} else {
				assert.Nil(t, got.LastAttempt)
			}

			if tt.want.LastSyncTime != nil {
				require.NotNil(t, got.LastSyncTime)
				assert.True(t, tt.want.LastSyncTime.Equal(*got.LastSyncTime))
			} else {
				assert.Nil(t, got.LastSyncTime)
			}
		})
	}
}

func TestDBSyncStatusToPhase_Consistency(t *testing.T) {
	t.Parallel()

	// Test that conversion is consistent in both directions
	tests := []struct {
		phase    status.SyncPhase
		dbStatus sqlc.SyncStatus
	}{
		{status.SyncPhaseSyncing, sqlc.SyncStatusINPROGRESS},
		{status.SyncPhaseComplete, sqlc.SyncStatusCOMPLETED},
		{status.SyncPhaseFailed, sqlc.SyncStatusFAILED},
	}

	for _, tt := range tests {
		t.Run(string(tt.phase), func(t *testing.T) {
			t.Parallel()

			// Phase -> DB -> Phase
			dbStatus := syncPhaseToDBStatus(tt.phase)
			assert.Equal(t, tt.dbStatus, dbStatus)

			phase := dbSyncStatusToPhase(dbStatus)
			assert.Equal(t, tt.phase, phase)

			// DB -> Phase -> DB
			phase2 := dbSyncStatusToPhase(tt.dbStatus)
			assert.Equal(t, tt.phase, phase2)

			dbStatus2 := syncPhaseToDBStatus(phase2)
			assert.Equal(t, tt.dbStatus, dbStatus2)
		})
	}
}

func TestMapConfigTypeToDBType_AllSourceTypes(t *testing.T) {
	t.Parallel()

	// Verify all defined source types have mappings
	sourceTypes := []string{
		config.SourceTypeGit,
		config.SourceTypeAPI,
		config.SourceTypeFile,
		config.SourceTypeManaged,
	}

	for _, sourceType := range sourceTypes {
		t.Run(sourceType, func(t *testing.T) {
			t.Parallel()

			result := mapConfigTypeToDBType(sourceType)
			// Should not panic and should return a valid registry type
			assert.NotEmpty(t, result)
			assert.Contains(t, []sqlc.RegistryType{
				sqlc.RegistryTypeLOCAL,
				sqlc.RegistryTypeFILE,
				sqlc.RegistryTypeREMOTE,
			}, result)
		})
	}
}

func TestErrRegistryNotFound(t *testing.T) {
	t.Parallel()

	// Verify the error is defined and has expected message
	assert.NotNil(t, ErrRegistryNotFound)
	assert.Contains(t, ErrRegistryNotFound.Error(), "registry")
	assert.Contains(t, ErrRegistryNotFound.Error(), "not found")
}

func TestDBSyncToStatus_PreservesAllFields(t *testing.T) {
	t.Parallel()

	// Create a sync status with all possible fields populated
	id := uuid.New()
	regID := uuid.New()
	syncTime := time.Now()
	attemptTime := time.Now().Add(-time.Hour)
	hash := "test-hash-123"
	filterHash := "filter-hash-456"
	errorMsg := "Test error message"

	dbSync := sqlc.RegistrySync{
		ID:                    id,
		RegID:                 regID,
		SyncStatus:            sqlc.SyncStatusCOMPLETED,
		ErrorMsg:              &errorMsg,
		StartedAt:             &attemptTime,
		EndedAt:               &syncTime,
		AttemptCount:          42,
		LastSyncHash:          &hash,
		LastAppliedFilterHash: &filterHash,
		ServerCount:           100,
	}

	result := dbSyncToStatus(dbSync)

	// Verify all fields are preserved
	assert.Equal(t, status.SyncPhaseComplete, result.Phase)
	assert.Equal(t, errorMsg, result.Message)
	assert.Equal(t, 42, result.AttemptCount)
	assert.Equal(t, 100, result.ServerCount)
	assert.Equal(t, hash, result.LastSyncHash)
	assert.Equal(t, filterHash, result.LastAppliedFilterHash)
	require.NotNil(t, result.LastAttempt)
	assert.True(t, attemptTime.Equal(*result.LastAttempt))
	require.NotNil(t, result.LastSyncTime)
	assert.True(t, syncTime.Equal(*result.LastSyncTime))
}

func TestDBSyncRowToStatus_PreservesAllFields(t *testing.T) {
	t.Parallel()

	// Create a row with all possible fields populated
	id := uuid.New()
	regID := uuid.New()
	syncTime := time.Now()
	attemptTime := time.Now().Add(-time.Hour)
	hash := "test-hash-789"
	filterHash := "filter-hash-012"
	errorMsg := "Test row error"

	row := sqlc.ListRegistrySyncsRow{
		Name:                  "test-registry",
		ID:                    id,
		RegID:                 regID,
		SyncStatus:            sqlc.SyncStatusFAILED,
		ErrorMsg:              &errorMsg,
		StartedAt:             &attemptTime,
		EndedAt:               &syncTime,
		AttemptCount:          7,
		LastSyncHash:          &hash,
		LastAppliedFilterHash: &filterHash,
		ServerCount:           50,
	}

	result := dbSyncRowToStatus(row)

	// Verify all fields are preserved
	assert.Equal(t, status.SyncPhaseFailed, result.Phase)
	assert.Equal(t, errorMsg, result.Message)
	assert.Equal(t, 7, result.AttemptCount)
	assert.Equal(t, 50, result.ServerCount)
	assert.Equal(t, hash, result.LastSyncHash)
	assert.Equal(t, filterHash, result.LastAppliedFilterHash)
	require.NotNil(t, result.LastAttempt)
	assert.True(t, attemptTime.Equal(*result.LastAttempt))
	require.NotNil(t, result.LastSyncTime)
	assert.True(t, syncTime.Equal(*result.LastSyncTime))
}
