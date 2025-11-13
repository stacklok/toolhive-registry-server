package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/stacklok/toolhive/pkg/registry"

	"github.com/stacklok/toolhive-registry-server/internal/config"
)

const (
	// RegistryFileName is the name of the registry data file
	RegistryFileName = "registry.json"
)

//go:generate mockgen -destination=mocks/mock_storage_manager.go -package=mocks -source=storage_manager.go StorageManager

// StorageManager defines the interface for registry data persistence
type StorageManager interface {
	// Store saves a Registry instance to persistent storage
	Store(ctx context.Context, cfg *config.Config, reg *registry.Registry) error

	// Get retrieves and parses registry data from persistent storage
	Get(ctx context.Context, cfg *config.Config) (*registry.Registry, error)

	// Delete removes registry data from persistent storage
	Delete(ctx context.Context, cfg *config.Config) error
}

// FileStorageManager implements StorageManager using local filesystem
type FileStorageManager struct {
	basePath string
}

// NewFileStorageManager creates a new file-based storage manager
func NewFileStorageManager(basePath string) StorageManager {
	return &FileStorageManager{
		basePath: basePath,
	}
}

// Store saves the registry data to a JSON file
func (f *FileStorageManager) Store(_ context.Context, _ *config.Config, reg *registry.Registry) error {
	// Create base directory if it doesn't exist
	if err := os.MkdirAll(f.basePath, 0750); err != nil {
		return fmt.Errorf("failed to create storage directory: %w", err)
	}

	filePath := filepath.Join(f.basePath, RegistryFileName)

	// Marshal registry to JSON with pretty printing for readability
	data, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal registry data: %w", err)
	}

	// Write to temporary file first for atomic operation
	tempPath := filePath + ".tmp"
	if err := os.WriteFile(tempPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write temporary registry file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tempPath, filePath); err != nil {
		// Clean up temp file on error
		_ = os.Remove(tempPath)
		return fmt.Errorf("failed to rename registry file: %w", err)
	}

	return nil
}

// Get retrieves and parses registry data from the JSON file
func (f *FileStorageManager) Get(_ context.Context, _ *config.Config) (*registry.Registry, error) {
	filePath := filepath.Join(f.basePath, RegistryFileName)

	// Read file
	//nolint:gosec // File path is internally managed by StorageManager, not user input
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("registry file not found: %w", err)
		}
		return nil, fmt.Errorf("failed to read registry file: %w", err)
	}

	// Unmarshal JSON
	var reg registry.Registry
	if err := json.Unmarshal(data, &reg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal registry data: %w", err)
	}

	return &reg, nil
}

// Delete removes the registry data file
func (f *FileStorageManager) Delete(_ context.Context, _ *config.Config) error {
	filePath := filepath.Join(f.basePath, RegistryFileName)

	if err := os.Remove(filePath); err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist, nothing to delete
			return nil
		}
		return fmt.Errorf("failed to delete registry file: %w", err)
	}

	return nil
}
