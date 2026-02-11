// Package writer contains the SyncWriter interface and implementations
package writer

import (
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
)

// NewSyncWriter creates a database-backed SyncWriter.
// The pool parameter must not be nil.
func NewSyncWriter(pool *pgxpool.Pool) (SyncWriter, error) {
	if pool == nil {
		return nil, fmt.Errorf("database pool is required")
	}
	slog.Info("Creating database-backed sync writer")
	return NewDBSyncWriter(pool)
}
