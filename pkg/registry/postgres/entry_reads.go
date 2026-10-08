package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	upstream "github.com/modelcontextprotocol/registry/pkg/api/v0"
	upstreammodel "github.com/modelcontextprotocol/registry/pkg/model"

	"github.com/stacklok/toolhive-registry-server/internal/db/sqlc"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

// GetEntry selects an exact stored spelling or the latest version, never
// interpreting the exact string "latest" as a selector.
func (d *Entries) GetEntry(
	ctx context.Context, sourceID string, kind persistence.EntryKind,
	name string, selector persistence.VersionSelector,
) (persistence.Entry, error) {
	if name == "" || (selector.Latest == (selector.Exact != "")) {
		return persistence.Entry{}, persistence.ErrInvalid
	}
	id, err := uuid.Parse(sourceID)
	if err != nil {
		return persistence.Entry{}, persistence.ErrInvalid
	}
	if err = kind.Validate(); err != nil {
		return persistence.Entry{}, err
	}
	params := sqlc.EntryListParams{
		SourceID: id, EntryType: sqlc.EntryType(kind), Name: &name, LatestOnly: selector.Latest, PageSize: 1,
	}
	if !selector.Latest {
		params.Version = &selector.Exact
	}
	rows, err := d.queryEntries(ctx, params)
	if err != nil {
		return persistence.Entry{}, err
	}
	if len(rows) == 0 {
		return persistence.Entry{}, persistence.ErrNotFound
	}
	return decodeEntry(sourceID, kind, rows[0])
}

// ListEntries returns a C/byte-lexically ordered page of versions belonging
// only to one source and kind. Cursors are not snapshot isolation across calls.
//
//nolint:gocyclo // Cursor and source/filter binding are validated before every read.
func (d *Entries) ListEntries(ctx context.Context, opts persistence.ListOptions) (persistence.EntryPage, error) {
	var page persistence.EntryPage
	id, err := uuid.Parse(opts.SourceID)
	if err != nil || opts.Limit < 1 || opts.Limit > 100 {
		return page, persistence.ErrInvalid
	}
	if err = opts.Kind.Validate(); err != nil {
		return page, err
	}
	params := sqlc.EntryListParams{
		SourceID: id, EntryType: sqlc.EntryType(opts.Kind), LatestOnly: opts.LatestOnly, PageSize: int64(opts.Limit + 1),
	}
	if opts.Name != "" {
		params.Name = &opts.Name
	}
	if opts.Search != "" {
		params.Search = &opts.Search
	}
	if opts.Cursor != "" {
		if len(opts.Cursor) > persistence.MaxCursorBytes {
			return page, persistence.ErrInvalid
		}
		var cur entryCursor
		decoded, e := base64.RawURLEncoding.DecodeString(opts.Cursor)
		if e != nil {
			return page, persistence.ErrInvalid
		}
		dec := json.NewDecoder(bytes.NewReader(decoded))
		dec.DisallowUnknownFields()
		if dec.Decode(&cur) != nil || dec.Decode(new(any)) != io.EOF ||
			cur.Binding != cursorBinding(opts) ||
			cur.AfterName == "" || cur.AfterVersion == "" {
			return page, persistence.ErrInvalid
		}
		params.CursorName = &cur.AfterName
		params.CursorVersion = &cur.AfterVersion
	}
	rows, err := d.queryEntries(ctx, params)
	if err != nil {
		return page, err
	}
	more := len(rows) > opts.Limit
	if more {
		rows = rows[:opts.Limit]
	}
	page.Entries = make([]persistence.Entry, 0, len(rows))
	for _, r := range rows {
		v, e := decodeEntry(opts.SourceID, opts.Kind, r)
		if e != nil {
			return persistence.EntryPage{}, e
		}
		page.Entries = append(page.Entries, v)
	}
	if more {
		last := rows[len(rows)-1]
		b, e := json.Marshal(entryCursor{Binding: cursorBinding(opts), AfterName: last.Name, AfterVersion: last.Version})
		if e != nil {
			return persistence.EntryPage{}, persistence.ErrUnavailable
		}
		page.NextCursor = base64.RawURLEncoding.EncodeToString(b)
		if len(page.NextCursor) > persistence.MaxCursorBytes {
			return persistence.EntryPage{}, fmt.Errorf("%w: cursor exceeds %d bytes", persistence.ErrInvalid, persistence.MaxCursorBytes)
		}
	}
	return page, nil
}

type entryCursor struct {
	Binding      string
	AfterName    string
	AfterVersion string
}

func cursorBinding(opts persistence.ListOptions) string {
	query, _ := json.Marshal([]any{opts.SourceID, opts.Kind, opts.Name, opts.Search, opts.LatestOnly, opts.Limit, "name-version-C"})
	sum := sha256.Sum256(query)
	return hex.EncodeToString(sum[:])
}

func (d *Entries) queryEntries(ctx context.Context, params sqlc.EntryListParams) ([]sqlc.EntryListRow, error) {
	tx, err := d.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, classify(err)
	}
	defer tx.Rollback(context.Background())
	q := sqlc.New(tx)
	exists, err := q.EntrySourceExists(ctx, params.SourceID)
	if err != nil {
		return nil, classify(err)
	}
	if !exists {
		return nil, persistence.ErrNotFound
	}
	rows, err := q.EntryList(ctx, params)
	if err != nil {
		return nil, classify(err)
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, classify(err)
	}
	return rows, nil
}

func decodeEntry(sourceID string, kind persistence.EntryKind, r sqlc.EntryListRow) (persistence.Entry, error) {
	entry := persistence.Entry{ID: r.ID, SourceID: sourceID, Kind: kind, Name: r.Name, Version: r.Version, IsLatest: r.IsLatest}
	var err error
	switch kind {
	case persistence.ServerKind:
		entry.Server, err = decodeServer(r)
	case persistence.SkillKind:
		entry.Skill, err = decodeSkill(r)
	case persistence.PluginKind:
		entry.Plugin, err = decodePlugin(r)
	}
	if err != nil {
		return persistence.Entry{}, fmt.Errorf("%w: unreadable catalog payload", persistence.ErrUnavailable)
	}
	return entry, nil
}

//nolint:gocyclo // The normalized server and related rows need independent decoding and error handling.
func decodeServer(r sqlc.EntryListRow) (*model.Server, error) {
	var data struct {
		Website             *string         `json:"website"`
		SchemaURL           *string         `json:"schema_url"`
		RepositoryURL       *string         `json:"repository_url"`
		RepositoryID        *string         `json:"repository_id"`
		RepositorySubfolder *string         `json:"repository_subfolder"`
		RepositoryType      *string         `json:"repository_type"`
		ServerMeta          json.RawMessage `json:"server_meta"`
	}
	if err := json.Unmarshal(r.ServerData, &data); err != nil {
		return nil, err
	}
	server := &model.Server{Name: r.Name, Version: r.Version, Title: deref(r.Title), Description: deref(r.Description),
		WebsiteURL: deref(data.Website), Schema: deref(data.SchemaURL)}
	if server.Schema == "" {
		server.Schema = "https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json"
	}
	if data.RepositoryURL != nil {
		server.Repository = &upstreammodel.Repository{URL: *data.RepositoryURL, ID: deref(data.RepositoryID),
			Subfolder: deref(data.RepositorySubfolder), Source: deref(data.RepositoryType)}
	}
	if len(data.ServerMeta) > 0 && string(data.ServerMeta) != nullJSON {
		server.Meta = &upstream.ServerMeta{}
		if err := json.Unmarshal(data.ServerMeta, &server.Meta.PublisherProvided); err != nil {
			return nil, err
		}
	}
	var packages []struct {
		RegistryType    string          `json:"registry_type"`
		RegistryBaseURL string          `json:"pkg_registry_url"`
		Identifier      string          `json:"pkg_identifier"`
		Version         string          `json:"pkg_version"`
		RuntimeHint     string          `json:"runtime_hint"`
		FileSHA256      string          `json:"sha256_hash"`
		Transport       string          `json:"transport"`
		URL             string          `json:"transport_url"`
		RuntimeArgs     json.RawMessage `json:"runtime_arguments"`
		PackageArgs     json.RawMessage `json:"package_arguments"`
		EnvVars         json.RawMessage `json:"env_vars"`
		Headers         json.RawMessage `json:"transport_headers"`
		Variables       json.RawMessage `json:"transport_variables"`
	}
	if err := json.Unmarshal([]byte(r.ServerPackages), &packages); err != nil {
		return nil, err
	}
	for _, p := range packages {
		v := upstreammodel.Package{
			RegistryType: p.RegistryType, RegistryBaseURL: p.RegistryBaseURL, Identifier: p.Identifier, Version: p.Version,
			RunTimeHint: p.RuntimeHint, FileSHA256: p.FileSHA256,
			Transport: upstreammodel.Transport{Type: p.Transport, URL: p.URL},
		}
		if err := unmarshalOptional(p.RuntimeArgs, &v.RuntimeArguments); err != nil {
			return nil, err
		}
		if err := unmarshalOptional(p.PackageArgs, &v.PackageArguments); err != nil {
			return nil, err
		}
		if err := unmarshalOptional(p.EnvVars, &v.EnvironmentVariables); err != nil {
			return nil, err
		}
		if err := unmarshalOptional(p.Headers, &v.Transport.Headers); err != nil {
			return nil, err
		}
		if err := unmarshalOptional(p.Variables, &v.Transport.Variables); err != nil {
			return nil, err
		}
		server.Packages = append(server.Packages, v)
	}
	var remotes []struct {
		Type      string          `json:"transport"`
		URL       string          `json:"transport_url"`
		Headers   json.RawMessage `json:"transport_headers"`
		Variables json.RawMessage `json:"transport_variables"`
	}
	if err := json.Unmarshal([]byte(r.ServerRemotes), &remotes); err != nil {
		return nil, err
	}
	for _, v := range remotes {
		remote := upstreammodel.Transport{Type: v.Type, URL: v.URL}
		if err := unmarshalOptional(v.Headers, &remote.Headers); err != nil {
			return nil, err
		}
		if err := unmarshalOptional(v.Variables, &remote.Variables); err != nil {
			return nil, err
		}
		server.Remotes = append(server.Remotes, remote)
	}
	var icons []struct {
		Src          string   `json:"source_uri"`
		MIME         string   `json:"mime_type"`
		Theme        string   `json:"theme"`
		Sizes        []string `json:"sizes"`
		ThemePresent *bool    `json:"theme_present"`
	}
	if err := json.Unmarshal([]byte(r.ServerIcons), &icons); err != nil {
		return nil, err
	}
	for _, v := range icons {
		icon := upstreammodel.Icon{Src: v.Src, Sizes: v.Sizes}
		if v.MIME != "" {
			icon.MimeType = &v.MIME
		}
		theme := strings.ToLower(v.Theme)
		if v.ThemePresent == nil || *v.ThemePresent {
			icon.Theme = &theme
		}
		server.Icons = append(server.Icons, icon)
	}
	return server, nil
}

func decodeSkill(r sqlc.EntryListRow) (*model.Skill, error) {
	var data struct {
		Namespace     string          `json:"namespace"`
		Status        string          `json:"status"`
		License       string          `json:"license"`
		Compatibility string          `json:"compatibility"`
		AllowedTools  []string        `json:"allowed_tools"`
		Repository    json.RawMessage `json:"repository"`
		Icons         json.RawMessage `json:"icons"`
		Metadata      json.RawMessage `json:"metadata"`
		Meta          json.RawMessage `json:"extension_meta"`
		Provenance    json.RawMessage `json:"provenance"`
	}
	if err := json.Unmarshal(r.SkillData, &data); err != nil {
		return nil, err
	}
	v := &model.Skill{ID: r.ID, Name: r.Name, Version: r.Version, Title: deref(r.Title), Description: deref(r.Description),
		Namespace: data.Namespace, Status: strings.ToLower(data.Status), License: data.License, Compatibility: data.Compatibility,
		AllowedTools: data.AllowedTools, IsLatest: r.IsLatest}
	if err := unmarshalOptional(data.Repository, &v.Repository); err != nil {
		return nil, err
	}
	if err := unmarshalOptional(data.Icons, &v.Icons); err != nil {
		return nil, err
	}
	if err := unmarshalOptional(data.Metadata, &v.Metadata); err != nil {
		return nil, err
	}
	if err := unmarshalOptional(data.Meta, &v.Meta); err != nil {
		return nil, err
	}
	if err := unmarshalOptional(data.Provenance, &v.Provenance); err != nil {
		return nil, err
	}
	if err := decodePackages(r.SkillOci, r.SkillGit, &v.Packages); err != nil {
		return nil, err
	}
	setTimes(r.CreatedAt, r.UpdatedAt, &v.CreatedAt, &v.UpdatedAt)
	return v, nil
}

func decodePlugin(r sqlc.EntryListRow) (*model.Plugin, error) {
	var data struct {
		Namespace  string          `json:"namespace"`
		Status     string          `json:"status"`
		License    string          `json:"license"`
		Repository json.RawMessage `json:"repository"`
		Icons      json.RawMessage `json:"icons"`
		Metadata   json.RawMessage `json:"metadata"`
		Meta       json.RawMessage `json:"extension_meta"`
	}
	if err := json.Unmarshal(r.PluginData, &data); err != nil {
		return nil, err
	}
	v := &model.Plugin{ID: r.ID, Name: r.Name, Version: r.Version, Title: deref(r.Title), Description: deref(r.Description),
		Namespace: data.Namespace, Status: strings.ToLower(data.Status), License: data.License, IsLatest: r.IsLatest}
	if err := unmarshalOptional(data.Repository, &v.Repository); err != nil {
		return nil, err
	}
	if err := unmarshalOptional(data.Icons, &v.Icons); err != nil {
		return nil, err
	}
	if err := unmarshalOptional(data.Metadata, &v.Metadata); err != nil {
		return nil, err
	}
	if err := unmarshalOptional(data.Meta, &v.Meta); err != nil {
		return nil, err
	}
	if err := decodePackages(r.PluginOci, r.PluginGit, &v.Packages); err != nil {
		return nil, err
	}
	setTimes(r.CreatedAt, r.UpdatedAt, &v.CreatedAt, &v.UpdatedAt)
	return v, nil
}

func decodePackages(oci, git string, out *[]model.SkillPackage) error {
	var a []struct {
		Identifier string `json:"identifier"`
		Digest     string `json:"digest"`
		MediaType  string `json:"media_type"`
	}
	if err := json.Unmarshal([]byte(oci), &a); err != nil {
		return err
	}
	for _, v := range a {
		*out = append(*out, model.SkillPackage{
			RegistryType: model.PackageFormatOCI, Identifier: v.Identifier, Digest: v.Digest, MediaType: v.MediaType,
		})
	}
	var b []struct {
		URL       string `json:"url"`
		Ref       string `json:"ref"`
		Commit    string `json:"commit_sha"`
		Subfolder string `json:"subfolder"`
	}
	if err := json.Unmarshal([]byte(git), &b); err != nil {
		return err
	}
	for _, v := range b {
		*out = append(*out, model.SkillPackage{
			RegistryType: model.PackageFormatGit, URL: v.URL, Ref: v.Ref, Commit: v.Commit, Subfolder: v.Subfolder,
		})
	}
	return nil
}

func unmarshalOptional[T any](raw json.RawMessage, dst *T) error {
	if len(raw) == 0 || string(raw) == nullJSON {
		return nil
	}
	return json.Unmarshal(raw, dst)
}
func deref(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
func setTimes(created, updated *time.Time, c, u *time.Time) {
	if created != nil {
		*c = *created
	}
	if updated != nil {
		*u = *updated
	}
}
