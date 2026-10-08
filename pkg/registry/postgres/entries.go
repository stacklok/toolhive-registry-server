package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	thvregistry "github.com/stacklok/toolhive-core/registry/types"

	"github.com/stacklok/toolhive-registry-server/internal/db"
	"github.com/stacklok/toolhive-registry-server/internal/db/sqlc"
	"github.com/stacklok/toolhive-registry-server/internal/sync/writer"
	"github.com/stacklok/toolhive-registry-server/internal/validators"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

// Entries borrows a migrated pool and never closes or migrates it. Authorization
// is the host's responsibility; reads expose raw source-owned records.
type Entries struct {
	pool        *pgxpool.Pool
	maxMetaSize int
}

var _ persistence.Entries = (*Entries)(nil)

// NewEntries borrows an already migrated caller-owned pool. It performs an
// explicit idempotent latest-pointer repair before returning, without migrating
// the schema or closing the pool. The host controls the pool's lifetime.
func NewEntries(ctx context.Context, pool *pgxpool.Pool, maxMetaSize int) (*Entries, error) {
	if pool == nil || maxMetaSize <= 0 {
		return nil, fmt.Errorf("%w: pool and positive metadata limit required", persistence.ErrInvalid)
	}
	if err := db.ReconcileLatestVersions(ctx, pool); err != nil {
		return nil, classify(err)
	}
	return &Entries{pool: pool, maxMetaSize: maxMetaSize}, nil
}

// ReplaceSnapshot atomically replaces all three kinds, including an empty set.
func (d *Entries) ReplaceSnapshot(ctx context.Context, source persistence.SourceDefinition, snapshot model.Snapshot) error {
	id, err := uuid.Parse(source.ID)
	if err != nil {
		return fmt.Errorf("%w: source ID", persistence.ErrInvalid)
	}
	kind, err := source.Kind()
	if err != nil {
		return err
	}
	if kind == "managed" {
		return fmt.Errorf("%w: managed source cannot be snapshotted", persistence.ErrConflict)
	}
	if err := persistence.ValidateSnapshot(&snapshot); err != nil {
		return err
	}
	if err := d.validateMetadata(snapshot); err != nil {
		return err
	}
	return (&Definitions{pool: d.pool}).write(ctx, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		row, e := q.EntrySource(ctx, id)
		if errors.Is(e, pgx.ErrNoRows) {
			return persistence.ErrNotFound
		}
		if e != nil {
			return classify(e)
		}
		if row.Name != source.Name || row.SourceType != kind {
			return fmt.Errorf("%w: source incarnation changed", persistence.ErrConflict)
		}
		if e = guardClaims(ctx, q, row); e != nil {
			return e
		}
		return classify(writer.StoreSourceTx(ctx, tx, id, &snapshot, d.maxMetaSize))
	})
}

// Publish adds exactly one managed version. An existing exact (kind,name,version)
// conflicts rather than silently replacing it, even if the payload is identical.
func (d *Entries) Publish(ctx context.Context, entry persistence.Entry) (persistence.Entry, error) {
	snapshot, err := publicationSnapshot(entry)
	if err != nil {
		return persistence.Entry{}, err
	}
	if err = d.validateMetadata(snapshot); err != nil {
		return persistence.Entry{}, err
	}
	var result persistence.Entry
	err = (&Definitions{pool: d.pool}).write(ctx, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		id, e := q.EntryManagedSource(ctx)
		if errors.Is(e, pgx.ErrNoRows) {
			return persistence.ErrNotFound
		}
		if e != nil {
			return classify(e)
		}
		row, e := q.EntrySource(ctx, id)
		if e != nil {
			return classify(e)
		}
		if e = guardClaims(ctx, q, row); e != nil {
			return e
		}
		exists, e := q.EntryVersionExists(ctx, sqlc.EntryVersionExistsParams{
			SourceID: id, EntryType: sqlc.EntryType(entry.Kind), Name: entry.Name, Version: entry.Version,
		})
		if e != nil {
			return classify(e)
		}
		if exists {
			return fmt.Errorf("%w: managed version already exists", persistence.ErrConflict)
		}
		if e = writer.PublishSourceTx(ctx, tx, id, &snapshot, d.maxMetaSize); e != nil {
			return classify(e)
		}
		if e = repointLatest(ctx, q, id, entry.Kind, entry.Name); e != nil {
			return e
		}
		rows, e := q.EntryList(ctx, sqlc.EntryListParams{SourceID: id, EntryType: sqlc.EntryType(entry.Kind),
			Name: &entry.Name, Version: &entry.Version, PageSize: 1})
		if e != nil {
			return classify(e)
		}
		if len(rows) != 1 {
			return persistence.ErrUnavailable
		}
		result, e = decodeEntry(id.String(), entry.Kind, rows[0])
		return e
	})
	if err != nil {
		return persistence.Entry{}, err
	}
	return result, nil
}

// DeleteManaged removes exactly one managed version, preserving other sources
// and recomputing latest for the affected kind/name only.
func (d *Entries) DeleteManaged(ctx context.Context, kind persistence.EntryKind, name, version string) error {
	if err := kind.Validate(); err != nil {
		return err
	}
	if name == "" || version == "" {
		return persistence.ErrInvalid
	}
	return (&Definitions{pool: d.pool}).write(ctx, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		id, e := q.EntryManagedSource(ctx)
		if errors.Is(e, pgx.ErrNoRows) {
			return persistence.ErrNotFound
		}
		if e != nil {
			return classify(e)
		}
		row, e := q.EntrySource(ctx, id)
		if e != nil {
			return classify(e)
		}
		if e = guardClaims(ctx, q, row); e != nil {
			return e
		}
		count, e := q.EntryDeleteVersion(ctx, sqlc.EntryDeleteVersionParams{
			SourceID: id, EntryType: sqlc.EntryType(kind), Name: name, Version: version,
		})
		if e != nil {
			return classify(e)
		}
		if count == 0 {
			return persistence.ErrNotFound
		}
		if e = q.EntryDeleteEmptyNames(ctx, id); e != nil {
			return classify(e)
		}
		return repointLatest(ctx, q, id, kind, name)
	})
}

func guardClaims(ctx context.Context, q *sqlc.Queries, row sqlc.EntrySourceRow) error {
	if row.HasClaims {
		return fmt.Errorf("%w: claimed legacy source", persistence.ErrConflict)
	}
	claimed, e := q.EntryHasClaims(ctx, row.ID)
	if e != nil {
		return classify(e)
	}
	if claimed {
		return fmt.Errorf("%w: claimed legacy entries", persistence.ErrConflict)
	}
	return nil
}

func repointLatest(ctx context.Context, q *sqlc.Queries, id uuid.UUID, kind persistence.EntryKind, name string) error {
	rows, e := q.EntryVersionsForName(ctx, sqlc.EntryVersionsForNameParams{
		SourceID: id, EntryType: sqlc.EntryType(kind), Name: name,
	})
	if e != nil {
		return classify(e)
	}
	if len(rows) == 0 {
		return classify(q.EntryDropLatest(ctx, sqlc.EntryDropLatestParams{SourceID: id, EntryType: sqlc.EntryType(kind), Name: name}))
	}
	best := rows[0]
	for _, v := range rows[1:] {
		if model.CompareVersions(v.Version, best.Version) > 0 {
			best = v
		}
	}
	return classify(q.RepairSetLatest(ctx, sqlc.RepairSetLatestParams{
		SourceID: id, EntryType: sqlc.EntryType(kind), Name: name, Version: best.Version, VersionID: best.ID,
	}))
}

func (d *Entries) validateMetadata(snapshot model.Snapshot) error {
	for _, server := range snapshot.Data.Servers {
		if _, err := validators.SerializeServerMeta(server.Meta, d.maxMetaSize); err != nil {
			return fmt.Errorf("%w: server metadata exceeds limit or is invalid", persistence.ErrInvalid)
		}
	}
	return nil
}

//nolint:gocyclo // Each of the three typed payloads requires independent identity checks.
func publicationSnapshot(entry persistence.Entry) (model.Snapshot, error) {
	var snapshot model.Snapshot
	if err := entry.Kind.Validate(); err != nil {
		return snapshot, err
	}
	if entry.ID != "" || entry.SourceID != "" || entry.IsLatest || entry.Name == "" || entry.Version == "" {
		return snapshot, persistence.ErrInvalid
	}
	count := 0
	if entry.Server != nil {
		count++
	}
	if entry.Skill != nil {
		count++
	}
	if entry.Plugin != nil {
		count++
	}
	if count != 1 {
		return snapshot, persistence.ErrInvalid
	}
	switch entry.Kind {
	case persistence.ServerKind:
		if entry.Server == nil || entry.Server.Name != entry.Name || entry.Server.Version != entry.Version {
			return snapshot, persistence.ErrInvalid
		}
		snapshot.Data.Servers = []model.Server{*entry.Server}
	case persistence.SkillKind:
		if entry.Skill == nil || entry.Skill.Name != entry.Name || entry.Skill.Version != entry.Version ||
			entry.Skill.ID != "" || entry.Skill.IsLatest ||
			!entry.Skill.CreatedAt.IsZero() || !entry.Skill.UpdatedAt.IsZero() {
			return snapshot, persistence.ErrInvalid
		}
		payload, e := json.Marshal(entry.Skill)
		if e != nil {
			return snapshot, fmt.Errorf("%w: skill payload", persistence.ErrInvalid)
		}
		var skill thvregistry.Skill
		if e = json.Unmarshal(payload, &skill); e != nil {
			return snapshot, fmt.Errorf("%w: skill payload", persistence.ErrInvalid)
		}
		snapshot.Data.Skills = []thvregistry.Skill{skill}
	case persistence.PluginKind:
		if entry.Plugin == nil || entry.Plugin.Name != entry.Name || entry.Plugin.Version != entry.Version ||
			entry.Plugin.ID != "" || entry.Plugin.IsLatest ||
			!entry.Plugin.CreatedAt.IsZero() || !entry.Plugin.UpdatedAt.IsZero() {
			return snapshot, persistence.ErrInvalid
		}
		payload, e := json.Marshal(entry.Plugin)
		if e != nil {
			return snapshot, fmt.Errorf("%w: plugin payload", persistence.ErrInvalid)
		}
		var plugin thvregistry.Plugin
		if e = json.Unmarshal(payload, &plugin); e != nil {
			return snapshot, fmt.Errorf("%w: plugin payload", persistence.ErrInvalid)
		}
		snapshot.Data.Plugins = []thvregistry.Plugin{plugin}
	}
	if err := persistence.ValidateSnapshot(&snapshot); err != nil {
		return snapshot, err
	}
	return snapshot, nil
}
