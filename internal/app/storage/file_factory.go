package storage

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/service"
	"github.com/stacklok/toolhive-registry-server/internal/service/factory"
	"github.com/stacklok/toolhive-registry-server/internal/service/inmemory"
	"github.com/stacklok/toolhive-registry-server/internal/sources"
	"github.com/stacklok/toolhive-registry-server/internal/status"
	"github.com/stacklok/toolhive-registry-server/internal/sync/state"
	"github.com/stacklok/toolhive-registry-server/internal/sync/writer"
)

// FileFactory creates file-based storage components.
// All components created by this factory use the local filesystem for persistence.
type FileFactory struct {
	config  *config.Config
	dataDir string

	// File-mode dependencies (created once, shared by all components)
	storageManager    sources.StorageManager
	statusPersistence status.StatusPersistence
}

var _ Factory = (*FileFactory)(nil)

// NewFileFactory creates a new file-based storage factory.
// It initializes the file storage manager and status persistence,
// ensuring the necessary directories exist.
func NewFileFactory(cfg *config.Config, dataDir string) (*FileFactory, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	// Use config's file storage base directory (defaults to "./data")
	baseDir := cfg.GetFileStorageBaseDir()

	// Ensure data directory exists
	if err := os.MkdirAll(baseDir, 0750); err != nil {
		return nil, fmt.Errorf("failed to create data directory %s: %w", baseDir, err)
	}

	slog.Info("Creating file-based storage factory", "base_dir", baseDir, "data_dir", dataDir)

	return &FileFactory{
		config:            cfg,
		dataDir:           dataDir,
		storageManager:    sources.NewFileStorageManager(baseDir),
		statusPersistence: status.NewFileStatusPersistence(dataDir),
	}, nil
}

// CreateStateService creates a file-based state service for sync status tracking.
func (f *FileFactory) CreateStateService(_ context.Context) (state.RegistryStateService, error) {
	slog.Debug("Creating file-based state service")
	return state.NewStateService(f.config, f.statusPersistence, nil)
}

// CreateSyncWriter creates a file-based sync writer for storing registry data.
func (f *FileFactory) CreateSyncWriter(_ context.Context) (writer.SyncWriter, error) {
	slog.Debug("Creating file-based sync writer")
	return writer.NewSyncWriter(f.config, f.storageManager, nil)
}

// CreateRegistryService creates a file-based registry service.
// The service reads registry data from files via the storage manager.
func (f *FileFactory) CreateRegistryService(ctx context.Context) (service.RegistryService, error) {
	slog.Debug("Creating file-based registry service")

	// Create the registry data provider that reads from file storage
	provider := inmemory.NewFileRegistryDataProvider(f.storageManager, f.config)

	// Use the service factory to create the in-memory service with file provider
	return factory.NewRegistryService(ctx, f.config, nil, provider)
}

// Cleanup releases resources held by the file factory.
// For file storage, there are no resources to clean up (no connection pools, etc.).
func (*FileFactory) Cleanup() {
	slog.Debug("Cleaning up file storage factory (no-op)")
	// No resources to clean up for file storage
}
