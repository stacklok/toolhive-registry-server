// Package status provides sync status tracking and persistence for the registry.
package status

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

//go:generate mockgen -destination=mocks/mock_status_persistence.go -package=mocks -source=persistence.go StatusPersistence

const (
	// StatusFileName is the name of the status file
	StatusFileName = "status.json"
)

// StatusPersistence defines the interface for sync status persistence
//
//nolint:revive // This name is fine
type StatusPersistence interface {
	// SaveStatus saves the sync status to persistent storage for a specific registry
	SaveStatus(ctx context.Context, registryName string, status *SyncStatus) error

	// LoadStatus loads the sync status from persistent storage for a specific registry
	// Returns an empty SyncStatus if the file doesn't exist (first run)
	LoadStatus(ctx context.Context, registryName string) (*SyncStatus, error)

	// LoadAllStatus loads sync status for all registries
	LoadAllStatus(ctx context.Context) (map[string]*SyncStatus, error)
}

// fileStatusPersistence implements StatusPersistence using local filesystem
type fileStatusPersistence struct {
	basePath string
}

// NewFileStatusPersistence creates a new file-based status persistence
// basePath is the base directory where per-registry status files will be stored
func NewFileStatusPersistence(basePath string) StatusPersistence {
	return &fileStatusPersistence{
		basePath: basePath,
	}
}

// SaveStatus saves the sync status to a JSON file in a registry-specific directory
func (f *fileStatusPersistence) SaveStatus(_ context.Context, registryName string, status *SyncStatus) error {
	// Create registry-specific directory if it doesn't exist
	registryDir := filepath.Join(f.basePath, registryName)
	if err := os.MkdirAll(registryDir, 0750); err != nil {
		return fmt.Errorf("failed to create status directory for registry '%s': %w", registryName, err)
	}

	filePath := filepath.Join(registryDir, StatusFileName)

	// Marshal status to JSON with pretty printing for readability
	data, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal status data for registry '%s': %w", registryName, err)
	}

	// Write to temporary file first for atomic operation
	tempPath := filePath + ".tmp"
	if err := os.WriteFile(tempPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write temporary status file for registry '%s': %w", registryName, err)
	}

	// Atomic rename
	if err := os.Rename(tempPath, filePath); err != nil {
		// Clean up temp file on error
		_ = os.Remove(tempPath)
		return fmt.Errorf("failed to rename status file for registry '%s': %w", registryName, err)
	}

	return nil
}

// LoadStatus loads the sync status from a JSON file for a specific registry
// Returns an empty SyncStatus if the file doesn't exist
func (f *fileStatusPersistence) LoadStatus(_ context.Context, registryName string) (*SyncStatus, error) {
	registryDir := filepath.Join(f.basePath, registryName)
	filePath := filepath.Join(registryDir, StatusFileName)

	// Read file
	// #nosec G304 -- filePath is constructed from trusted internal sources (basePath + validated registryName)
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist - this is OK for first run
			return &SyncStatus{}, nil
		}
		return nil, fmt.Errorf("failed to read status file for registry '%s': %w", registryName, err)
	}

	// Unmarshal JSON
	var status SyncStatus
	if err := json.Unmarshal(data, &status); err != nil {
		return nil, fmt.Errorf("failed to unmarshal status data for registry '%s': %w", registryName, err)
	}

	return &status, nil
}

// LoadAllStatus loads sync status for all registries
func (f *fileStatusPersistence) LoadAllStatus(ctx context.Context) (map[string]*SyncStatus, error) {
	result := make(map[string]*SyncStatus)

	// Read all subdirectories in the base path
	entries, err := os.ReadDir(f.basePath)
	if err != nil {
		if os.IsNotExist(err) {
			// Base directory doesn't exist yet, return empty map
			return result, nil
		}
		return nil, fmt.Errorf("failed to read status directory: %w", err)
	}

	// For each subdirectory, try to load status
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		registryName := entry.Name()
		status, err := f.LoadStatus(ctx, registryName)
		if err != nil {
			// Log error but continue with other registries
			// This allows partial results if some registries fail to load
			continue
		}

		result[registryName] = status
	}

	return result, nil
}
