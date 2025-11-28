// Package writer contains the SyncWriter interface and implementations
package writer

import (
	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/sources"
)

// NewSyncWriter creates a SyncWriter based on the configured storage type.
//
// For file-based storage, it returns the provided StorageManager which
// implements the SyncWriter interface.
//
// For database storage, it currently returns the StorageManager as a
// placeholder until the database implementation is complete.
func NewSyncWriter(cfg *config.Config, storageManager sources.StorageManager) SyncWriter {
	switch cfg.GetStorageType() {
	case config.StorageTypeDatabase:
		// TODO: Return a database-backed SyncWriter implementation once available.
		// For now, use the file-based StorageManager as a placeholder.
		// The database implementation will store registry data directly in PostgreSQL
		// instead of writing to JSON files.
		return storageManager
	case config.StorageTypeFile:
		// StorageManager implements the SyncWriter interface via its Store method
		return storageManager
	default:
		// Default to file-based storage for unknown types
		return storageManager
	}
}
