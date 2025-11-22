package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	toolhivetypes "github.com/stacklok/toolhive/pkg/registry/registry"
)

const (
	// RegistryFileName is the name of the registry data file
	RegistryFileName = "registry.json"
)

//go:generate mockgen -destination=mocks/mock_storage_manager.go -package=mocks -source=storage_manager.go StorageManager

// StorageManager defines the interface for registry data persistence
type StorageManager interface {
	// Store saves a UpstreamRegistry instance to persistent storage for a specific registry
	Store(ctx context.Context, registryName string, reg *toolhivetypes.UpstreamRegistry) error

	// Get retrieves and parses registry data from persistent storage for a specific registry
	Get(ctx context.Context, registryName string) (*toolhivetypes.UpstreamRegistry, error)

	// GetAll retrieves and parses registry data from all registries
	GetAll(ctx context.Context) (map[string]*toolhivetypes.UpstreamRegistry, error)

	// Delete removes registry data from persistent storage for a specific registry
	Delete(ctx context.Context, registryName string) error
}

// fileStorageManager implements StorageManager using local filesystem
type fileStorageManager struct {
	basePath string
}

// NewFileStorageManager creates a new file-based storage manager
func NewFileStorageManager(basePath string) StorageManager {
	return &fileStorageManager{
		basePath: basePath,
	}
}

// Store saves the registry data to a JSON file in a registry-specific subdirectory
func (f *fileStorageManager) Store(_ context.Context, registryName string, reg *toolhivetypes.UpstreamRegistry) error {
	// Create registry-specific directory if it doesn't exist
	registryDir := filepath.Join(f.basePath, registryName)
	if err := os.MkdirAll(registryDir, 0750); err != nil {
		return fmt.Errorf("failed to create registry directory: %w", err)
	}

	filePath := filepath.Join(registryDir, RegistryFileName)

	// Marshal UpstreamRegistry to JSON with pretty printing for readability
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

// Get retrieves and parses registry data from the JSON file for a specific registry
func (f *fileStorageManager) Get(_ context.Context, registryName string) (*toolhivetypes.UpstreamRegistry, error) {
	registryDir := filepath.Join(f.basePath, registryName)
	filePath := filepath.Join(registryDir, RegistryFileName)

	// Read file
	//nolint:gosec // File path is internally managed by StorageManager, not user input
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("registry file not found for registry '%s': %w", registryName, err)
		}
		return nil, fmt.Errorf("failed to read registry file for registry '%s': %w", registryName, err)
	}

	// Unmarshal JSON to UpstreamRegistry
	var reg toolhivetypes.UpstreamRegistry
	if err := json.Unmarshal(data, &reg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal registry data for registry '%s': %w", registryName, err)
	}

	return &reg, nil
}

// GetAll retrieves registry data from all registries in the base path
func (f *fileStorageManager) GetAll(ctx context.Context) (map[string]*toolhivetypes.UpstreamRegistry, error) {
	result := make(map[string]*toolhivetypes.UpstreamRegistry)

	// Read all subdirectories in the base path
	entries, err := os.ReadDir(f.basePath)
	if err != nil {
		if os.IsNotExist(err) {
			// Base directory doesn't exist yet, return empty map
			return result, nil
		}
		return nil, fmt.Errorf("failed to read storage directory: %w", err)
	}

	// For each subdirectory, try to load registry data
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		registryName := entry.Name()
		reg, err := f.Get(ctx, registryName)
		if err != nil {
			// Log error but continue with other registries
			// This allows partial results if some registries fail to load
			continue
		}

		result[registryName] = reg
	}

	return result, nil
}

// Delete removes the registry data file for a specific registry
func (f *fileStorageManager) Delete(_ context.Context, registryName string) error {
	registryDir := filepath.Join(f.basePath, registryName)

	// Remove the entire registry directory
	if err := os.RemoveAll(registryDir); err != nil {
		if os.IsNotExist(err) {
			// Directory doesn't exist, nothing to delete
			return nil
		}
		return fmt.Errorf("failed to delete registry directory for registry '%s': %w", registryName, err)
	}

	return nil
}
