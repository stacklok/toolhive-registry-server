// Package postgres implements claims-free definition persistence in the existing registry tables.
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/stacklok/toolhive-registry-server/internal/db/pgtypes"
	"github.com/stacklok/toolhive-registry-server/internal/db/sqlc"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

// Definitions stores source and view definitions in the caller's PostgreSQL pool.
type Definitions struct{ pool *pgxpool.Pool }

var _ persistence.Definitions = (*Definitions)(nil)

// NewDefinitions borrows a migrated caller-owned pool. It never migrates or closes it.
func NewDefinitions(pool *pgxpool.Pool) (*Definitions, error) {
	if pool == nil {
		return nil, fmt.Errorf("%w: nil pool", persistence.ErrInvalid)
	}
	return &Definitions{pool: pool}, nil
}

type querier = sqlc.DBTX

const nullJSON = "null"

// CreateSource creates an API-owned source.
func (d *Definitions) CreateSource(ctx context.Context, s persistence.SourceDefinition) (persistence.SourceDefinition, error) {
	if s.ID != "" || s.Origin != "" {
		return s, fmt.Errorf("%w: identity/origin is assigned by storage", persistence.ErrInvalid)
	}
	if _, err := s.Kind(); err != nil {
		return s, err
	}
	var out persistence.SourceDefinition
	err := d.write(ctx, func(tx pgx.Tx) error {
		var e error
		out, e = insertSource(ctx, tx, s)
		return e
	})
	return out, err
}

// GetSource reads a source by name.
func (d *Definitions) GetSource(ctx context.Context, name string) (persistence.SourceDefinition, error) {
	return getSource(ctx, d.pool, name, false)
}

// ListSources returns all source definitions in lexical name order.
func (d *Definitions) ListSources(ctx context.Context) ([]persistence.SourceDefinition, error) {
	rows, err := sqlc.New(d.pool).DefListSources(ctx)
	if err != nil {
		return nil, classify(err)
	}
	out := make([]persistence.SourceDefinition, 0, len(rows))
	for _, row := range rows {
		v, e := decodeSource(row.ID, row.Name, row.Origin, row.SourceType, row.SourceConfig, row.FilterConfig, row.Micros)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, nil
}

// UpdateSource replaces an API-owned source without changing its identity or kind.
//
//nolint:gocyclo // Ownership, legacy metadata, and immutable kind checks form one atomic update gate.
func (d *Definitions) UpdateSource(
	ctx context.Context, name string, s persistence.SourceDefinition,
) (persistence.SourceDefinition, error) {
	if s.Name != name || (s.Origin != "" && s.Origin != persistence.OriginAPI) {
		return s, fmt.Errorf("%w: immutable source identity/origin", persistence.ErrInvalid)
	}
	kind, err := s.Kind()
	if err != nil {
		return s, err
	}
	var out persistence.SourceDefinition
	err = d.write(ctx, func(tx pgx.Tx) error {
		old, e := getSource(ctx, tx, name, true)
		if e != nil {
			return e
		}
		if old.Origin != persistence.OriginAPI {
			return fmt.Errorf("%w: CONFIG source", persistence.ErrConflict)
		}
		if s.ID != "" && s.ID != old.ID {
			return fmt.Errorf("%w: source ID", persistence.ErrConflict)
		}
		oldKind, e := old.Kind()
		if e != nil {
			return fmt.Errorf("%w: unsupported legacy source definition", persistence.ErrConflict)
		}
		if oldKind != kind {
			return fmt.Errorf("%w: source kind immutable", persistence.ErrConflict)
		}
		if old.File != nil && fileKind(old.File) != fileKind(s.File) {
			return fmt.Errorf("%w: file location kind immutable", persistence.ErrConflict)
		}
		if e = requireNoClaims(ctx, tx, "source", name); e != nil {
			return e
		}
		if e = requireKnownFields(ctx, tx, name, old); e != nil {
			return e
		}
		spec, filter, schedule, e := encode(s)
		if e != nil {
			return e
		}
		e = sqlc.New(tx).DefUpdateSource(ctx, sqlc.DefUpdateSourceParams{
			Name: name, Column2: spec, Column3: filter, Column4: schedule, Syncable: synced(s),
		})
		if e != nil {
			return classify(e)
		}
		out, e = getSource(ctx, tx, name, false)
		return e
	})
	return out, err
}

// DeleteSource removes an unreferenced API-owned source.
func (d *Definitions) DeleteSource(ctx context.Context, name string) error {
	return d.write(ctx, func(tx pgx.Tx) error {
		old, e := getSource(ctx, tx, name, true)
		if e != nil {
			return e
		}
		if old.Origin != persistence.OriginAPI {
			return fmt.Errorf("%w: CONFIG source", persistence.ErrConflict)
		}
		if e = requireNoClaims(ctx, tx, "source", name); e != nil {
			return e
		}
		claimed, e := sqlc.New(tx).EntryHasClaims(ctx, uuid.MustParse(old.ID))
		if e != nil {
			return classify(e)
		}
		if claimed {
			return fmt.Errorf("%w: claimed legacy entries", persistence.ErrConflict)
		}
		used, e := sqlc.New(tx).DefSourceUsed(ctx, uuid.MustParse(old.ID))
		if e != nil {
			return classify(e)
		}
		if used {
			return persistence.ErrInUse
		}
		return classify(sqlc.New(tx).DefDeleteAPISource(ctx, uuid.MustParse(old.ID)))
	})
}

// CreateView creates an API-owned view and its ordered source links.
func (d *Definitions) CreateView(ctx context.Context, v persistence.ViewDefinition) (persistence.ViewDefinition, error) {
	if v.Origin != "" {
		return v, fmt.Errorf("%w: origin is assigned by storage", persistence.ErrInvalid)
	}
	if err := persistence.ValidateView(v); err != nil {
		return v, err
	}
	var out persistence.ViewDefinition
	err := d.write(ctx, func(tx pgx.Tx) error {
		id, e := sqlc.New(tx).DefInsertAPIView(ctx, v.Name)
		if e != nil {
			return classify(e)
		}
		if e = link(ctx, tx, id, v.Sources); e != nil {
			return e
		}
		out = persistence.ViewDefinition{Name: v.Name, Origin: persistence.OriginAPI, Sources: append([]string(nil), v.Sources...)}
		return nil
	})
	return out, err
}

// GetView reads a named view and its ordered source references.
func (d *Definitions) GetView(ctx context.Context, name string) (persistence.ViewDefinition, error) {
	tx, err := d.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return persistence.ViewDefinition{}, classify(err)
	}
	defer tx.Rollback(context.Background())
	v, err := getView(ctx, tx, name, false)
	if err != nil {
		return v, err
	}
	return v, classify(tx.Commit(ctx))
}

// ListViews returns all named views in lexical name order.
func (d *Definitions) ListViews(ctx context.Context) ([]persistence.ViewDefinition, error) {
	// A read-only transaction gives both the view set and ordered links one snapshot.
	tx, err := d.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, classify(err)
	}
	defer tx.Rollback(context.Background())
	names, err := sqlc.New(tx).DefListViewNames(ctx)
	if err != nil {
		return nil, classify(err)
	}
	out := make([]persistence.ViewDefinition, 0, len(names))
	for _, n := range names {
		v, e := getView(ctx, tx, n, false)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, classify(tx.Commit(ctx))
}

// UpdateView replaces the ordered source references of an API-owned view.
func (d *Definitions) UpdateView(
	ctx context.Context, name string, v persistence.ViewDefinition,
) (persistence.ViewDefinition, error) {
	if v.Name != name || (v.Origin != "" && v.Origin != persistence.OriginAPI) {
		return v, fmt.Errorf("%w: immutable view name/origin", persistence.ErrInvalid)
	}
	if e := persistence.ValidateView(v); e != nil {
		return v, e
	}
	var out persistence.ViewDefinition
	err := d.write(ctx, func(tx pgx.Tx) error {
		old, e := getView(ctx, tx, name, true)
		if e != nil {
			return e
		}
		if old.Origin != persistence.OriginAPI {
			return fmt.Errorf("%w: CONFIG view", persistence.ErrConflict)
		}
		if e = requireNoClaims(ctx, tx, "registry", name); e != nil {
			return e
		}
		id, e := sqlc.New(tx).DefViewID(ctx, name)
		if e != nil {
			return classify(e)
		}
		if e = sqlc.New(tx).DefDeleteViewLinks(ctx, uuid.MustParse(id)); e != nil {
			return classify(e)
		}
		if e = link(ctx, tx, id, v.Sources); e != nil {
			return e
		}
		e = sqlc.New(tx).DefTouchAPIView(ctx, uuid.MustParse(id))
		if e != nil {
			return classify(e)
		}
		out = persistence.ViewDefinition{Name: name, Origin: persistence.OriginAPI, Sources: append([]string(nil), v.Sources...)}
		return nil
	})
	return out, err
}

// DeleteView removes an API-owned view and its links.
func (d *Definitions) DeleteView(ctx context.Context, name string) error {
	return d.write(ctx, func(tx pgx.Tx) error {
		old, e := getView(ctx, tx, name, true)
		if e != nil {
			return e
		}
		if old.Origin != persistence.OriginAPI {
			return fmt.Errorf("%w: CONFIG view", persistence.ErrConflict)
		}
		if e = requireNoClaims(ctx, tx, "registry", name); e != nil {
			return e
		}
		return classify(sqlc.New(tx).DefDeleteAPIView(ctx, name))
	})
}

// Reconcile atomically replaces only CONFIG-owned definitions and links.
//
//nolint:gocyclo // The all-or-nothing collision, ownership, and FK checks are deliberately colocated.
func (d *Definitions) Reconcile(
	ctx context.Context, sources []persistence.SourceDefinition, views []persistence.ViewDefinition,
) error {
	if err := persistence.ValidateReconcile(sources, views); err != nil {
		return err
	}
	return d.write(ctx, func(tx pgx.Tx) error {
		// Check every collision before touching data, including names late in the input.
		for _, s := range sources {
			old, e := getSource(ctx, tx, s.Name, true)
			if e != nil && !errors.Is(e, persistence.ErrNotFound) {
				return e
			}
			if e == nil {
				if old.Origin != persistence.OriginConfig {
					return fmt.Errorf("%w: API source name", persistence.ErrConflict)
				}
				if e = requireNoClaims(ctx, tx, "source", s.Name); e != nil {
					return e
				}
				a, _ := old.Kind()
				b, _ := s.Kind()
				if a != b {
					return fmt.Errorf("%w: source kind immutable", persistence.ErrConflict)
				}
				if e = requireKnownFields(ctx, tx, s.Name, old); e != nil {
					return e
				}
				if old.File != nil && fileKind(old.File) != fileKind(s.File) {
					return fmt.Errorf("%w: file location kind immutable", persistence.ErrConflict)
				}
			}
		}
		for _, v := range views {
			old, e := getView(ctx, tx, v.Name, true)
			if e != nil && !errors.Is(e, persistence.ErrNotFound) {
				return e
			}
			if e == nil {
				if old.Origin != persistence.OriginConfig {
					return fmt.Errorf("%w: API view name", persistence.ErrConflict)
				}
				if e = requireNoClaims(ctx, tx, "registry", v.Name); e != nil {
					return e
				}
			}
		}
		keepViews := make([]string, 0, len(views))
		for _, v := range views {
			keepViews = append(keepViews, v.Name)
		}
		err := sqlc.New(tx).DefPruneViews(ctx, keepViews)
		if err != nil {
			return classify(err)
		}
		// Fail closed when a legacy claimed row would otherwise have been pruned.
		blocked, err := sqlc.New(tx).DefBlockedViews(ctx, keepViews)
		if err != nil {
			return classify(err)
		}
		if blocked {
			return fmt.Errorf("%w: claimed CONFIG view", persistence.ErrConflict)
		}
		keepSources := make([]string, 0, len(sources))
		for _, s := range sources {
			keepSources = append(keepSources, s.Name)
		}
		// Remove old CONFIG links before pruning: retained views may be relinked to
		// replacement sources. API links still restrict deletion. No intermediate
		// state is visible outside this transaction.
		err = sqlc.New(tx).DefDeleteConfigLinks(ctx)
		if err != nil {
			return classify(err)
		}
		blocked, err = sqlc.New(tx).DefBlockedSources(ctx, keepSources)
		if err != nil {
			return classify(err)
		}
		if blocked {
			return fmt.Errorf("%w: claimed CONFIG source or entries", persistence.ErrConflict)
		}
		err = sqlc.New(tx).DefPruneSources(ctx, keepSources)
		if err != nil {
			return classify(err)
		}
		for _, s := range sources {
			spec, filter, schedule, e := encode(s)
			if e != nil {
				return e
			}
			kind, _ := s.Kind()
			rows, e := sqlc.New(tx).DefUpsertConfigSource(ctx, sqlc.DefUpsertConfigSourceParams{
				Name: s.Name, SourceType: kind, Column3: spec, Column4: filter, Column5: schedule, Syncable: synced(s),
			})
			if e != nil {
				return classify(e)
			}
			if rows != 1 {
				return fmt.Errorf("%w: source name owned by another writer", persistence.ErrConflict)
			}
		}
		for _, v := range views {
			id, e := sqlc.New(tx).DefUpsertConfigView(ctx, v.Name)
			if e != nil {
				return classify(e)
			}
			if e = sqlc.New(tx).DefDeleteViewLinks(ctx, uuid.MustParse(id)); e != nil {
				return classify(e)
			}
			if e = link(ctx, tx, id, v.Sources); e != nil {
				return e
			}
		}
		return nil
	})
}

func (d *Definitions) write(ctx context.Context, fn func(pgx.Tx) error) error {
	// All writes in this adapter serialize on one transaction-scoped lock, before
	// reading any definitions. The FK and unique indexes also guard external writers.
	for attempt := 0; attempt < 5; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		tx, err := d.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
		if err != nil {
			return classify(err)
		}
		_, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(910,1)`)
		if err == nil {
			err = fn(tx)
		}
		if err == nil {
			err = tx.Commit(ctx)
		} else {
			_ = tx.Rollback(context.Background())
		}
		if err == nil {
			return nil
		}
		_ = tx.Rollback(context.Background())
		var retry retryable
		if errors.As(err, &retry) {
			continue
		}
		var pg *pgconn.PgError
		if errors.As(err, &pg) && (pg.Code == "40001" || pg.Code == "40P01") {
			continue
		}
		return classify(err)
	}
	return fmt.Errorf("%w: concurrent modification, retry", persistence.ErrConflict)
}

func insertSource(ctx context.Context, q querier, s persistence.SourceDefinition) (persistence.SourceDefinition, error) {
	kind, _ := s.Kind()
	spec, filter, schedule, e := encode(s)
	if e != nil {
		return s, e
	}
	_, e = sqlc.New(q).DefInsertSource(ctx, sqlc.DefInsertSourceParams{
		Name: s.Name, SourceType: kind, Column3: spec, Column4: filter, Column5: schedule, Syncable: synced(s),
	})
	if e != nil {
		return s, classify(e)
	}
	return getSource(ctx, q, s.Name, false)
}

func getSource(ctx context.Context, q querier, name string, lock bool) (persistence.SourceDefinition, error) {
	var (
		id, origin, kind string
		spec, filter     []byte
		micros           any
		e                error
	)
	if lock {
		row, err := sqlc.New(q).DefLockSource(ctx, name)
		id, origin, kind, spec, filter, micros, e = row.ID, row.Origin, row.SourceType,
			row.SourceConfig, row.FilterConfig, row.Micros, err
	} else {
		row, err := sqlc.New(q).DefGetSource(ctx, name)
		id, origin, kind, spec, filter, micros, e = row.ID, row.Origin, row.SourceType,
			row.SourceConfig, row.FilterConfig, row.Micros, err
	}
	if errors.Is(e, pgx.ErrNoRows) {
		return persistence.SourceDefinition{}, persistence.ErrNotFound
	}
	if e != nil {
		return persistence.SourceDefinition{}, classify(e)
	}
	return decodeSource(id, name, origin, kind, spec, filter, micros)
}

func decodeSource(id, name, origin, kind string, spec, filter []byte, micros any) (persistence.SourceDefinition, error) {
	s := persistence.SourceDefinition{ID: id, Name: name, Origin: persistence.Origin(origin)}
	var e error
	if micros != nil {
		value, ok := micros.(int64)
		if !ok {
			return s, fmt.Errorf("%w: invalid stored schedule", persistence.ErrInvalid)
		}
		if value != 0 {
			s.Schedule = (time.Duration(value) * time.Microsecond).String()
		}
	}
	switch kind {
	case "git":
		s.Git = &persistence.GitSpec{}
		e = json.Unmarshal(spec, s.Git)
	case "api":
		s.API = &persistence.APISpec{}
		e = json.Unmarshal(spec, s.API)
	case "file":
		s.File = &persistence.FileSpec{}
		e = json.Unmarshal(spec, s.File)
	case "managed":
		s.Managed = &persistence.ManagedSpec{}
	case "kubernetes":
		s.Kubernetes = &persistence.KubernetesSpec{}
		e = json.Unmarshal(spec, s.Kubernetes)
	default:
		return s, fmt.Errorf("%w: unknown stored source kind", persistence.ErrInvalid)
	}
	if e == nil && len(filter) > 0 && string(filter) != nullJSON {
		s.Filter = &persistence.Filter{}
		e = json.Unmarshal(filter, s.Filter)
	}
	if e != nil {
		return s, fmt.Errorf("%w: unreadable stored definition", persistence.ErrInvalid)
	}
	return s, nil
}

func getView(ctx context.Context, q querier, name string, lock bool) (persistence.ViewDefinition, error) {
	var v persistence.ViewDefinition
	var e error
	if lock {
		row, err := sqlc.New(q).DefLockView(ctx, name)
		v.Name, v.Origin, e = row.Name, persistence.Origin(row.Origin), err
	} else {
		row, err := sqlc.New(q).DefGetView(ctx, name)
		v.Name, v.Origin, e = row.Name, persistence.Origin(row.Origin), err
	}
	if errors.Is(e, pgx.ErrNoRows) {
		return v, persistence.ErrNotFound
	}
	if e != nil {
		return v, classify(e)
	}
	v.Sources, e = sqlc.New(q).DefViewSources(ctx, name)
	return v, classify(e)
}

func link(ctx context.Context, q querier, id string, names []string) error {
	for pos, name := range names {
		// A FK prevents concurrent external source deletion; missing references are explicit.
		rows, e := sqlc.New(q).DefLink(ctx, sqlc.DefLinkParams{Column1: uuid.MustParse(id), Name: name, Column3: int32(pos)})
		if e != nil {
			return classify(e)
		}
		if rows != 1 {
			return fmt.Errorf("%w: referenced source %q", persistence.ErrNotFound, name)
		}
	}
	return nil
}

func encode(s persistence.SourceDefinition) ([]byte, []byte, pgtypes.Interval, error) {
	var spec any
	switch {
	case s.Git != nil:
		spec = s.Git
	case s.API != nil:
		spec = s.API
	case s.File != nil:
		spec = s.File
	case s.Managed != nil:
		spec = s.Managed
	case s.Kubernetes != nil:
		spec = s.Kubernetes
	}
	raw, e := json.Marshal(spec)
	if e != nil {
		return nil, nil, pgtypes.Interval{}, fmt.Errorf("%w: source spec", persistence.ErrInvalid)
	}
	var filter []byte
	if s.Filter != nil {
		filter, e = json.Marshal(s.Filter)
		if e != nil {
			return nil, nil, pgtypes.Interval{}, fmt.Errorf("%w: filter", persistence.ErrInvalid)
		}
	}
	schedule, _ := pgtypes.ParseDuration(s.Schedule)
	return raw, filter, schedule, nil
}
func synced(s persistence.SourceDefinition) bool {
	return s.Managed == nil && s.Kubernetes == nil && (s.File == nil || s.File.Data == "")
}
func fileKind(f *persistence.FileSpec) string {
	if f.Path != "" {
		return "path"
	}
	if f.URL != "" {
		return "url"
	}
	return "data"
}

func requireKnownFields(ctx context.Context, q querier, name string, old persistence.SourceDefinition) error {
	raw, err := sqlc.New(q).DefSourceRaw(ctx, name)
	if err != nil {
		return classify(err)
	}
	spec, filter := raw.SourceConfig, raw.FilterConfig
	var target any
	switch {
	case old.Git != nil:
		target = &persistence.GitSpec{}
	case old.API != nil:
		target = &persistence.APISpec{}
	case old.File != nil:
		target = &persistence.FileSpec{}
	case old.Managed != nil:
		target = &persistence.ManagedSpec{}
	case old.Kubernetes != nil:
		target = &persistence.KubernetesSpec{}
	}
	for _, item := range []struct {
		raw  []byte
		dest any
	}{{spec, target}, {filter, &persistence.Filter{}}} {
		if len(item.raw) == 0 || string(item.raw) == nullJSON {
			continue
		}
		decoder := json.NewDecoder(strings.NewReader(string(item.raw)))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(item.dest); err != nil {
			return fmt.Errorf("%w: unsupported legacy definition fields", persistence.ErrConflict)
		}
	}
	return nil
}

func requireNoClaims(ctx context.Context, q querier, table, name string) error {
	var has any
	var e error
	switch table {
	case "source":
		has, e = sqlc.New(q).DefSourceClaims(ctx, name)
	case "registry":
		has, e = sqlc.New(q).DefViewClaims(ctx, name)
	default:
		return persistence.ErrInvalid
	}
	if e != nil {
		return classify(e)
	}
	if has == true {
		return fmt.Errorf("%w: legacy claimed definition is not owned by this adapter", persistence.ErrConflict)
	}
	return nil
}

type retryable struct{}

func (retryable) Error() string { return "concurrent definition modification" }

func classify(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	if errors.Is(err, persistence.ErrInvalid) || errors.Is(err, persistence.ErrNotFound) ||
		errors.Is(err, persistence.ErrConflict) || errors.Is(err, persistence.ErrInUse) {
		return err
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch pg.Code {
		case "23505":
			return fmt.Errorf("%w: duplicate definition", persistence.ErrConflict)
		case "23503":
			return persistence.ErrInUse
		case "40001", "40P01":
			return retryable{}
		default:
			return persistence.ErrUnavailable
		}
	}
	return persistence.ErrUnavailable
}
