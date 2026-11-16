package postgres

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/stacklok/toolhive-registry-server/internal/db/sqlc"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

var _ persistence.EntryDiagnostics = (*Entries)(nil)

// ListSourceVersions resolves existence and all raw version projections in one
// read-only repeatable-read transaction. It never loads payloads or claims.
func (d *Entries) ListSourceVersions(ctx context.Context, name string) ([]persistence.DiagnosticVersion, error) {
	if name == "" {
		return nil, persistence.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	tx, err := d.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, viewReadError(err)
	}
	defer tx.Rollback(context.Background())
	q := sqlc.New(tx)
	id, err := q.DiagnosticSourceID(ctx, name)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, persistence.ErrNotFound
	}
	if err != nil {
		return nil, viewReadError(err)
	}
	rows, err := q.DiagnosticSourceVersions(ctx, id)
	if err != nil {
		return nil, viewReadError(err)
	}
	result := make([]persistence.DiagnosticVersion, 0, len(rows))
	for _, row := range rows {
		result = append(result, persistence.DiagnosticVersion{
			Kind: persistence.EntryKind(row.EntryType), Name: row.Name, Version: row.Version,
			Title: deref(row.Title), Description: deref(row.Description),
			CreatedAt: definitionTime(row.CreatedAt), UpdatedAt: definitionTime(row.UpdatedAt),
		})
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, viewReadError(err)
	}
	return result, nil
}

// ListViewVersions includes every linked source's history, even shadowed names
// and identical versions. View existence, links and rows share one snapshot.
func (d *Entries) ListViewVersions(ctx context.Context, name string) ([]persistence.ViewDiagnosticVersion, error) {
	if name == "" {
		return nil, persistence.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	tx, err := d.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, viewReadError(err)
	}
	defer tx.Rollback(context.Background())
	q := sqlc.New(tx)
	rawID, err := q.DefViewID(ctx, name)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, persistence.ErrNotFound
	}
	if err != nil {
		return nil, viewReadError(err)
	}
	id, err := uuid.Parse(rawID)
	if err != nil {
		return nil, persistence.ErrUnavailable
	}
	rows, err := q.DiagnosticViewVersions(ctx, id)
	if err != nil {
		return nil, viewReadError(err)
	}
	result := make([]persistence.ViewDiagnosticVersion, 0, len(rows))
	for _, row := range rows {
		result = append(result, persistence.ViewDiagnosticVersion{
			DiagnosticVersion: persistence.DiagnosticVersion{
				Kind: persistence.EntryKind(row.EntryType), Name: row.Name, Version: row.Version,
				Title: deref(row.Title), Description: deref(row.Description),
				CreatedAt: definitionTime(row.CreatedAt), UpdatedAt: definitionTime(row.UpdatedAt),
			},
			SourceName: row.SourceName, SourceID: row.SourceID, Position: row.Position,
		})
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, viewReadError(err)
	}
	return result, nil
}
