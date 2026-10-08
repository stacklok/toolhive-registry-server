package state

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/stacklok/toolhive-registry-server/internal/db"
)

// ReconcileLatestVersions repairs persisted kind/name pointers before standalone
// services or HTTP listeners are constructed. The caller retains the pool.
func ReconcileLatestVersions(ctx context.Context, pool *pgxpool.Pool) error {
	return db.ReconcileLatestVersions(ctx, pool)
}

func reconcileLatestVersions(ctx context.Context, tx pgx.Tx) error {
	return db.ReconcileLatestVersionsTx(ctx, tx)
}
