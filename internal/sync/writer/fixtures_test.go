package writer

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	toolhivetypes "github.com/stacklok/toolhive-core/registry/types"

	"github.com/stacklok/toolhive-registry-server/internal/db/sqlc"
)

// snapshotFixture owns only test transactions. Production snapshot writes are
// fenced by Jobs; these tests exercise payload COPY/upsert/rollback in isolation.
type snapshotFixture struct {
	pool  *pgxpool.Pool
	limit int
}

func newSnapshotFixture(pool *pgxpool.Pool, limit int) (*snapshotFixture, error) {
	if pool == nil {
		return nil, fmt.Errorf("pgx pool is required")
	}
	return &snapshotFixture{pool: pool, limit: limit}, nil
}

func (f *snapshotFixture) store(ctx context.Context, name string, snapshot *toolhivetypes.UpstreamRegistry) error {
	if snapshot == nil {
		return fmt.Errorf("registry data is required")
	}
	tx, err := f.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	source, err := sqlc.New(tx).GetSourceByName(ctx, name)
	if err != nil {
		return fmt.Errorf("registry not found: %w", err)
	}
	if err = StoreSourceTx(ctx, tx, source.ID, snapshot, f.limit); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
