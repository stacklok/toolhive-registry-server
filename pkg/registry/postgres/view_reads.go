package postgres

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/stacklok/toolhive-registry-server/internal/db"
	"github.com/stacklok/toolhive-registry-server/internal/db/sqlc"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

var _ persistence.ViewEntryReader = (*Entries)(nil)

// GetViewEntry never fills a missing version from a lower-priority source.
func (d *Entries) GetViewEntry(
	ctx context.Context, view string, name persistence.EntryName, selector persistence.VersionSelector,
) (persistence.Entry, error) {
	if err := name.Validate(view, selector); err != nil {
		return persistence.Entry{}, err
	}
	page, err := d.ListViewEntries(ctx, view, persistence.ViewQuery{
		Kind: name.Kind, Name: name.Name, Namespace: name.Namespace, VersionSelector: selector, Limit: 1,
	})
	if err != nil {
		return persistence.Entry{}, err
	}
	if len(page.Entries) == 0 {
		return persistence.Entry{}, persistence.ErrNotFound
	}
	return page.Entries[0], nil
}

// ListViewEntries resolves the view and a bounded winner page in one read-only
// repeatable-read transaction. Subsequent calls recompute winners against live
// data; no history is pinned by a cursor. Names and versions are ordered in the
// database's default collation, as v1.5.4 ordered them.
//
// Cursors are positional: they carry only the entry kind and the last (name,
// version) returned, so a client may change the limit, filters, view sources or
// even the view between pages, as with v1.5.4. v1.5.4 cursors (standard base64
// of "name,version") are accepted for every kind.
func (d *Entries) ListViewEntries(ctx context.Context, view string, query persistence.ViewQuery) (persistence.EntryPage, error) {
	if err := query.Validate(view); err != nil {
		return persistence.EntryPage{}, err
	}
	if err := ctx.Err(); err != nil {
		return persistence.EntryPage{}, err
	}
	params, err := viewListParams(query)
	if err != nil {
		return persistence.EntryPage{}, err
	}
	tx, err := d.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return persistence.EntryPage{}, viewReadError(err)
	}
	defer tx.Rollback(context.Background())
	// The list query's optional filters are "param IS NULL OR ..." guards. A
	// cached generic plan cannot prune them and falls back to scanning every
	// entry, so plan each call with its actual parameters.
	if _, err = tx.Exec(ctx, `SET LOCAL plan_cache_mode = force_custom_plan`); err != nil {
		return persistence.EntryPage{}, viewReadError(err)
	}
	q := sqlc.New(tx)
	rawID, err := q.DefViewID(ctx, view)
	if errors.Is(err, pgx.ErrNoRows) {
		return persistence.EntryPage{}, persistence.ErrViewNotFound
	}
	if err != nil {
		return persistence.EntryPage{}, viewReadError(err)
	}
	id, err := uuid.Parse(rawID)
	if err != nil {
		return persistence.EntryPage{}, persistence.ErrUnavailable
	}
	params.ViewID = id
	// Set only by the server's claims mode, through a module-internal context
	// key; library callers always read unfiltered.
	params.Caller = db.ViewCaller(ctx)
	rows, err := q.ViewEntryList(ctx, params)
	if err != nil {
		return persistence.EntryPage{}, viewReadError(err)
	}
	page, err := decodeViewPage(rows, query)
	if err != nil {
		return persistence.EntryPage{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return persistence.EntryPage{}, viewReadError(err)
	}
	return page, nil
}

func viewReadError(err error) error {
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return unavailable(err)
}

func viewListParams(query persistence.ViewQuery) (sqlc.ViewEntryListParams, error) {
	params := sqlc.ViewEntryListParams{
		EntryType: sqlc.EntryType(query.Kind), LatestOnly: query.VersionSelector.Latest, PageSize: int64(query.Limit + 1),
	}
	if query.Name != "" {
		params.Name = &query.Name
	}
	if query.Namespace != "" {
		params.Namespace = &query.Namespace
	}
	if query.Search != "" {
		params.Search = &query.Search
	}
	if query.VersionSelector.Exact != "" {
		params.Version = &query.VersionSelector.Exact
	}
	if query.UpdatedSince != nil {
		params.UpdatedSince = pgtype.Timestamptz{Time: *query.UpdatedSince, Valid: true}
	}
	if query.Cursor != "" {
		cur, err := decodeViewCursor(query.Cursor, query.Kind)
		if err != nil {
			return sqlc.ViewEntryListParams{}, err
		}
		params.CursorName, params.CursorVersion = &cur.AfterName, &cur.AfterVersion
		if cur.AfterNamespace != "" {
			params.CursorNamespace = &cur.AfterNamespace
		}
	}
	return params, nil
}

func decodeViewPage(rows []sqlc.ViewEntryListRow, query persistence.ViewQuery) (persistence.EntryPage, error) {
	page := persistence.EntryPage{Entries: make([]persistence.Entry, 0, min(len(rows), query.Limit))}
	more := len(rows) > query.Limit
	if more {
		rows = rows[:query.Limit]
	}
	for _, r := range rows {
		var payload []byte
		switch query.Kind {
		case persistence.ServerKind:
			payload = r.ServerData
		case persistence.SkillKind:
			payload = r.SkillData
		case persistence.PluginKind:
			payload = r.PluginData
		}
		if len(payload) == 0 || string(payload) == nullJSON {
			return persistence.EntryPage{}, undecodableEntry(query.Kind, r.Name, r.Version, errMissingPayload)
		}
		entry, e := decodeEntry(r.SourceID, query.Kind, sqlc.EntryListRow{
			ID: r.ID, Name: r.Name, Version: r.Version, Title: r.Title, Description: r.Description,
			CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, IsLatest: r.IsLatest,
			ServerData: r.ServerData, SkillData: r.SkillData, PluginData: r.PluginData,
			ServerPackages: r.ServerPackages, ServerRemotes: r.ServerRemotes, ServerIcons: r.ServerIcons,
			SkillOci: r.SkillOci, SkillGit: r.SkillGit, PluginOci: r.PluginOci, PluginGit: r.PluginGit,
		})
		if e != nil {
			return persistence.EntryPage{}, e
		}
		page.Entries = append(page.Entries, entry)
	}
	if more {
		last := rows[len(rows)-1]
		data, e := json.Marshal(viewCursor{
			Version: viewCursorVersion, Kind: query.Kind, AfterName: last.Name, AfterVersion: last.Version,
			AfterNamespace: last.Namespace,
		})
		if e != nil {
			return persistence.EntryPage{}, persistence.ErrUnavailable
		}
		page.NextCursor = base64.RawURLEncoding.EncodeToString(data)
		if len(page.NextCursor) > persistence.MaxCursorBytes {
			return persistence.EntryPage{}, persistence.ErrInvalid
		}
	}
	return page, nil
}

// viewCursorVersion is the format of the cursors this backend emits. Version 1
// (never released) bound a cursor to the whole query and is rejected.
const viewCursorVersion = 2

// legacyCursorSeparator splits v1.5.4's base64("name,version") cursor.
const legacyCursorSeparator = ","

// viewCursor is a position in a view listing: the kind it walks and the last
// (name, version) returned. It binds nothing else, so it stays valid when the
// limit, filters or view sources change between pages.
type viewCursor struct {
	Version      int
	Kind         persistence.EntryKind
	AfterName    string
	AfterVersion string
	// AfterNamespace breaks name/version ties between skills or plugins of
	// different namespaces; empty for servers and for older cursors.
	AfterNamespace string `json:",omitempty"`
}

// decodeViewCursor accepts this backend's cursor (base64url JSON object) for
// the same kind, or a v1.5.4 cursor (standard base64 of "name,version") for any
// kind. v1.5.4 names never start with '{', so the two cannot be confused.
func decodeViewCursor(token string, kind persistence.EntryKind) (viewCursor, error) {
	var cur viewCursor
	if data, err := base64.RawURLEncoding.DecodeString(token); err == nil && len(data) > 0 && data[0] == '{' {
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&cur) != nil || decoder.Decode(new(any)) != io.EOF ||
			cur.Version != viewCursorVersion || cur.Kind != kind {
			return viewCursor{}, persistence.ErrInvalid
		}
	} else {
		data, err := base64.StdEncoding.DecodeString(token)
		if err != nil {
			return viewCursor{}, persistence.ErrInvalid
		}
		// v1.5.4 required exactly one comma, so splitting at the first comma
		// accepts every cursor v1.5.4 accepted.
		name, version, found := strings.Cut(string(data), legacyCursorSeparator)
		if !found {
			return viewCursor{}, persistence.ErrInvalid
		}
		cur = viewCursor{Version: viewCursorVersion, Kind: kind, AfterName: name, AfterVersion: version}
	}
	if cur.AfterName == "" || cur.AfterVersion == "" {
		return viewCursor{}, persistence.ErrInvalid
	}
	name := persistence.EntryName{Kind: persistence.ServerKind, Name: cur.AfterName}
	if err := name.Validate("cursor", persistence.VersionSelector{Exact: cur.AfterVersion}); err != nil {
		return viewCursor{}, err
	}
	return cur, nil
}
