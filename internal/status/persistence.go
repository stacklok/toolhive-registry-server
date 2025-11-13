// Package status provides sync status tracking and persistence for the registry.
package status

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	// StatusFileName is the name of the status file
	StatusFileName = "status.json"
)

// StatusPersistence defines the interface for sync status persistence
//
//nolint:revive // This name is fine
type StatusPersistence interface {
	// SaveStatus saves the sync status to persistent storage
	SaveStatus(ctx context.Context, status *SyncStatus) error

	// LoadStatus loads the sync status from persistent storage
	// Returns an empty SyncStatus if the file doesn't exist (first run)
	LoadStatus(ctx context.Context) (*SyncStatus, error)
}

// FileStatusPersistence implements StatusPersistence using local filesystem
type FileStatusPersistence struct {
	filePath string
}

// NewFileStatusPersistence creates a new file-based status persistence
func NewFileStatusPersistence(filePath string) StatusPersistence {
	return &FileStatusPersistence{
		filePath: filePath,
	}
}

// SaveStatus saves the sync status to a JSON file
func (f *FileStatusPersistence) SaveStatus(_ context.Context, status *SyncStatus) error {
	// Create directory if it doesn't exist
	dir := filepath.Dir(f.filePath)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return fmt.Errorf("failed to create status directory: %w", err)
	}

	// Marshal status to JSON with pretty printing for readability
	data, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal status data: %w", err)
	}

	// Write to temporary file first for atomic operation
	tempPath := f.filePath + ".tmp"
	if err := os.WriteFile(tempPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write temporary status file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tempPath, f.filePath); err != nil {
		// Clean up temp file on error
		_ = os.Remove(tempPath)
		return fmt.Errorf("failed to rename status file: %w", err)
	}

	return nil
}

// LoadStatus loads the sync status from a JSON file
// Returns an empty SyncStatus if the file doesn't exist
func (f *FileStatusPersistence) LoadStatus(_ context.Context) (*SyncStatus, error) {
	// Read file
	data, err := os.ReadFile(f.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist - this is OK for first run
			return &SyncStatus{}, nil
		}
		return nil, fmt.Errorf("failed to read status file: %w", err)
	}

	// Unmarshal JSON
	var status SyncStatus
	if err := json.Unmarshal(data, &status); err != nil {
		return nil, fmt.Errorf("failed to unmarshal status data: %w", err)
	}

	return &status, nil
}
