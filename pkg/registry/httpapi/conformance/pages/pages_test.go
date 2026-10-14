package pages

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/catalog"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

var errTestDenied = errors.New("test admission denied")

type testPages struct {
	entries []persistence.Entry
	hidden  bool
	revoked bool
}

func (p *testPages) List(ctx context.Context, _ string, q catalog.Query) (catalog.Page, error) {
	return p.list(ctx, q)
}

func (p *testPages) Get(ctx context.Context, _ string, name catalog.EntryName, selector catalog.VersionSelector) (persistence.Entry, error) {
	if err := p.check(ctx); err != nil {
		return persistence.Entry{}, err
	}
	if p.hidden && name.Name == "com.example/z" {
		return persistence.Entry{}, persistence.ErrNotFound
	}
	for _, entry := range p.entries {
		if entry.Name == name.Name && (selector.Latest && entry.IsLatest || selector.Exact == entry.Version) {
			return entry, nil
		}
	}
	return persistence.Entry{}, persistence.ErrNotFound
}

func (p *testPages) ListVersions(ctx context.Context, _ string, name catalog.EntryName, request catalog.PageRequest) (catalog.Page, error) {
	if err := p.check(ctx); err != nil {
		return catalog.Page{}, err
	}
	if p.hidden && name.Name == "com.example/z" {
		return catalog.Page{}, persistence.ErrNotFound
	}
	for _, entry := range p.entries {
		if entry.Name == name.Name {
			return p.list(ctx, catalog.Query{Kind: name.Kind, Name: name.Name, Limit: request.Limit, Cursor: request.Cursor})
		}
	}
	return catalog.Page{}, persistence.ErrNotFound
}

func (p *testPages) list(ctx context.Context, q catalog.Query) (catalog.Page, error) {
	if err := p.check(ctx); err != nil {
		return catalog.Page{}, err
	}
	var entries []persistence.Entry
	for _, entry := range p.entries {
		if p.hidden && entry.Name == "com.example/z" || q.Name != "" && entry.Name != q.Name ||
			q.Search != "" && !strings.Contains(strings.ToLower(entry.Name+" "+entry.Server.Description), strings.ToLower(q.Search)) {
			continue
		}
		entries = append(entries, entry)
	}
	offset := 0
	if !p.hidden && q.Cursor != "" {
		var err error
		offset, err = strconv.Atoi(q.Cursor)
		if err != nil || offset >= len(entries) {
			return catalog.Page{}, persistence.ErrInvalid
		}
	}
	end := min(offset+q.Limit, len(entries))
	page := catalog.Page{Entries: entries[offset:end]}
	if end < len(entries) {
		page.NextCursor = strconv.Itoa(end)
	}
	return page, nil
}

func (p *testPages) check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if p.revoked {
		return errTestDenied
	}
	return nil
}

//nolint:tparallel // Run owns synchronous subtest sequencing for fixture isolation.
func TestRun(t *testing.T) {
	t.Parallel()
	Run(t, func(_ *testing.T) Fixture {
		entries := []persistence.Entry{
			testEntry("com.example/z", "2", "needle z", false),
			testEntry("com.example/z", "1", "needle z", true),
			testEntry("com.example/a", "1", "needle a", true),
		}
		pages := &testPages{entries: entries}
		return Fixture{
			Pages: pages, Context: t.Context(), View: "public", HiddenName: "com.example/private", MissingName: "com.example/missing", Search: "needle z",
			Records: []Record{
				{Name: "com.example/z", Version: "2", Description: "needle z"},
				{Name: "com.example/z", Version: "1", Description: "needle z", Latest: true},
				{Name: "com.example/a", Version: "1", Description: "needle a", Latest: true},
			},
			Hide: func(string) { pages.hidden = true }, Revoke: func() { pages.revoked = true },
		}
	})
}

func testEntry(name, version, description string, latest bool) persistence.Entry {
	return persistence.Entry{Kind: persistence.ServerKind, Name: name, Version: version, IsLatest: latest,
		Server: &model.Server{Name: name, Version: version, Description: description}}
}
