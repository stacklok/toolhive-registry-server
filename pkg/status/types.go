package status

import "time"

// SyncPhase represents the current phase of a synchronization operation
type SyncPhase string

const (
	// SyncPhaseSyncing means sync is currently in progress
	SyncPhaseSyncing SyncPhase = "Syncing"

	// SyncPhaseComplete means sync completed successfully
	SyncPhaseComplete SyncPhase = "Complete"

	// SyncPhaseFailed means sync failed
	SyncPhaseFailed SyncPhase = "Failed"
)

// SyncStatus represents the current state of registry synchronization
type SyncStatus struct {
	// Phase represents the current synchronization phase
	Phase SyncPhase `yaml:"phase"`

	// Message provides additional information about the sync status
	Message string `yaml:"message,omitempty"`

	// LastAttempt is the timestamp of the last sync attempt
	LastAttempt *time.Time `yaml:"lastAttempt,omitempty"`

	// AttemptCount is the number of sync attempts since last success
	AttemptCount int `yaml:"attemptCount,omitempty"`

	// LastSyncTime is the timestamp of the last successful sync
	LastSyncTime *time.Time `yaml:"lastSyncTime,omitempty"`

	// LastSyncHash is the hash of the last successfully synced data
	// Used to detect changes in source data
	LastSyncHash string `yaml:"lastSyncHash,omitempty"`

	// LastAppliedFilterHash is the hash of the last applied filter
	LastAppliedFilterHash string `yaml:"lastAppliedFilterHash,omitempty"`

	// ServerCount is the total number of servers in the registry
	ServerCount int `yaml:"serverCount,omitempty"`
}
