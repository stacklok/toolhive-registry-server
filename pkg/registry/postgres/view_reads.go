package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

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

// ListViewEntries resolves view/link identities, cursor binding and a bounded
// winner page in one read-only repeatable-read transaction. Subsequent calls
// recompute winners against live data; no history is pinned by a cursor.
func (d *Entries) ListViewEntries(ctx context.Context, view string, query persistence.ViewQuery) (persistence.EntryPage, error) {
	if err := query.Validate(view); err != nil {
		return persistence.EntryPage{}, err
	}
	if err := ctx.Err(); err != nil {
		return persistence.EntryPage{}, err
	}
	tx, err := d.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return persistence.EntryPage{}, viewReadError(err)
	}
	defer tx.Rollback(context.Background())
	q := sqlc.New(tx)
	rawID, err := q.DefViewID(ctx, view)
	if errors.Is(err, pgx.ErrNoRows) {
		return persistence.EntryPage{}, persistence.ErrNotFound
	}
	if err != nil {
		return persistence.EntryPage{}, viewReadError(err)
	}
	id, err := uuid.Parse(rawID)
	if err != nil {
		return persistence.EntryPage{}, persistence.ErrUnavailable
	}
	sources, err := q.ViewEntrySources(ctx, id)
	if err != nil {
		return persistence.EntryPage{}, viewReadError(err)
	}
	binding := viewBinding(view, rawID, sources, query)
	params, err := viewListParams(id, query, binding)
	if err != nil {
		return persistence.EntryPage{}, err
	}
	rows, err := q.ViewEntryList(ctx, params)
	if err != nil {
		return persistence.EntryPage{}, viewReadError(err)
	}
	page, err := decodeViewPage(rows, query, binding)
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
	return persistence.ErrUnavailable
}

func viewListParams(id uuid.UUID, query persistence.ViewQuery, binding string) (sqlc.ViewEntryListParams, error) {
	params := sqlc.ViewEntryListParams{
		ViewID: id, EntryType: sqlc.EntryType(query.Kind),
		LatestOnly: query.VersionSelector.Latest, PageSize: int64(query.Limit + 1),
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
		cur, err := decodeViewCursor(query.Cursor, binding)
		if err != nil {
			return sqlc.ViewEntryListParams{}, err
		}
		params.CursorName, params.CursorVersion = &cur.AfterName, &cur.AfterVersion
	}
	return params, nil
}

func decodeViewPage(rows []sqlc.ViewEntryListRow, query persistence.ViewQuery, binding string) (persistence.EntryPage, error) {
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
			return persistence.EntryPage{}, persistence.ErrUnavailable
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
		data, e := json.Marshal(viewCursor{Version: 1, Binding: binding, AfterName: last.Name, AfterVersion: last.Version})
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

type viewCursor struct {
	Version      int
	Binding      string
	AfterName    string
	AfterVersion string
}

func viewBinding(view, incarnation string, sources []string, query persistence.ViewQuery) string {
	query.Cursor = ""
	if query.UpdatedSince != nil {
		t := query.UpdatedSince.UTC()
		query.UpdatedSince = &t
	}
	data, _ := json.Marshal([]any{"postgres-view-v1", view, incarnation, sources, query, "name-version-C"})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func decodeViewCursor(token, binding string) (viewCursor, error) {
	var cur viewCursor
	data, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return cur, persistence.ErrInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&cur) != nil || decoder.Decode(new(any)) != io.EOF || cur.Version != 1 ||
		cur.Binding != binding || cur.AfterName == "" || cur.AfterVersion == "" {
		return viewCursor{}, persistence.ErrInvalid
	}
	name := persistence.EntryName{Kind: persistence.ServerKind, Name: cur.AfterName}
	if err := name.Validate("cursor", persistence.VersionSelector{Exact: cur.AfterVersion}); err != nil {
		return viewCursor{}, err
	}
	return cur, nil
}
