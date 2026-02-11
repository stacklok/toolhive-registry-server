// Package writer contains the SyncWriter interface and implementations
package writer

import (
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
)

// NewSyncWriter creates a database-backed SyncWriter.
// The pool parameter must not be nil.
// maxMetaSize specifies the maximum allowed size in bytes for publisher-provided
// metadata extensions. Set to 0 to disable the size check.
func NewSyncWriter(pool *pgxpool.Pool, maxMetaSize int) (SyncWriter, error) {
	if pool == nil {
		return nil, fmt.Errorf("database pool is required")
	}
	slog.Info("Creating database-backed sync writer", "maxMetaSize", maxMetaSize)
	return NewDBSyncWriter(pool, maxMetaSize)
}
