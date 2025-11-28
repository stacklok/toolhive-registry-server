// Package writer contains the SyncWriter interface and implementations
package writer

import (
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stacklok/toolhive/pkg/logger"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/sources"
)

// NewSyncWriter creates a SyncWriter based on the configured storage type.
//
// For file-based storage, it returns the provided StorageManager which
// implements the SyncWriter interface.
//
// For database storage, it creates and returns a database-backed SyncWriter
// that stores registry data directly in PostgreSQL.
func NewSyncWriter(cfg *config.Config, storageManager sources.StorageManager, pool *pgxpool.Pool) (SyncWriter, error) {
	switch cfg.GetStorageType() {
	case config.StorageTypeDatabase:
		if pool == nil {
			return nil, fmt.Errorf("database pool is required for database storage type")
		}
		logger.Info("Creating database-backed sync writer")
		return NewDBSyncWriter(pool)
	case config.StorageTypeFile:
		// StorageManager implements the SyncWriter interface via its Store method
		logger.Info("Using file-based storage manager as sync writer")
		return storageManager, nil
	default:
		// Default to file-based storage for unknown types
		logger.Infof("Unknown storage type %s, defaulting to file-based storage", cfg.GetStorageType())
		return storageManager, nil
	}
}
