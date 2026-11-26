package writer

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	toolhivetypes "github.com/stacklok/toolhive-core/registry/types"

	"github.com/stacklok/toolhive-registry-server/internal/db/sqlc"
)

// NewDBSyncWriter creates a new dbSyncWriter with the given connection pool.
// The caller is responsible for closing the pool when done.
// maxMetaSize specifies the maximum allowed size in bytes for publisher-provided
// metadata extensions and must be greater than zero.
func NewDBSyncWriter(pool *pgxpool.Pool, maxMetaSize int) (SyncWriter, error) {
	if pool == nil {
		return nil, fmt.Errorf("pgx pool is required")
	}
	return &dbSyncWriter{pool: pool, maxMetaSize: maxMetaSize}, nil
}

// Store saves a UpstreamRegistry instance to database storage for a specific registry.
//
// It opens its own serializable transaction, resolves the source by name and
// stamps entries with the source claims (or per-entry claims when provided),
// then runs the same staging pipeline as StoreSourceTx:
//  1. Upserts registry entries (one per name), entry versions (one per name+version),
//     and mcp_server rows via temp tables and COPY to preserve existing UUIDs
//  2. Deletes orphaned servers that no longer exist in upstream (CASCADE cleans related data)
//  3. For packages/remotes/icons: creates temp tables, copies data, bulk upserts, deletes orphans
//  4. Updates the latest_entry_version table for each unique name and kind
//
// Temp tables are automatically dropped at transaction end (ON COMMIT DROP).
func (d *dbSyncWriter) Store(
	ctx context.Context,
	registryName string,
	reg *toolhivetypes.UpstreamRegistry,
	opts ...StoreOption,
) error {
	if reg == nil {
		return fmt.Errorf("registry data is required")
	}

	tx, err := d.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.Serializable,
		AccessMode: pgx.ReadWrite,
	})
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			_ = rollbackErr
		}
	}()

	source, err := sqlc.New(tx).GetSourceByName(ctx, registryName)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("registry not found: %s", registryName)
		}
		return fmt.Errorf("failed to get registry: %w", err)
	}

	storeOpts, err := parseStoreOptions(opts)
	if err != nil {
		return err
	}

	staged := &dbSyncWriter{
		maxMetaSize:    d.maxMetaSize,
		sourceClaims:   source.Claims,
		perEntryClaims: storeOpts.PerEntryClaims,
	}
	if err := staged.storeSourceTx(ctx, tx, source.ID, reg); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	return nil
}
