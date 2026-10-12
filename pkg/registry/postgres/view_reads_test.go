package postgres_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/catalog"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence/conformance"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/postgres"
)

func TestViewEntriesConformance(t *testing.T) {
	t.Parallel()
	conformance.RunViewEntries(t, func(t *testing.T) (persistence.Definitions, persistence.Entries, persistence.ViewEntryReader) {
		t.Helper()
		defs, pool := newBackend(t)
		entries, e := postgres.NewEntries(t.Context(), pool, 65536)
		if e != nil {
			t.Fatal(e)
		}
		return defs, entries, entries
	})
}

func TestViewReadFailures(t *testing.T) {
	t.Parallel()
	defs, pool := newBackend(t)
	ctx := t.Context()
	entries, e := postgres.NewEntries(ctx, pool, 65536)
	if e != nil {
		t.Fatal(e)
	}
	source, e := defs.CreateSource(ctx, persistence.SourceDefinition{Name: "view-failures", API: &persistence.APISpec{Endpoint: "https://example.org"}, Schedule: "1h"})
	if e != nil {
		t.Fatal(e)
	}
	snap := model.Snapshot{}
	snap.Data.Servers = []model.Server{{Schema: "https://example.org/server.schema.json", Name: "com.example/item", Version: "1", Description: "valid"}}
	if e = entries.ReplaceSnapshot(ctx, source, snap); e != nil {
		t.Fatal(e)
	}
	if _, e = defs.CreateView(ctx, persistence.ViewDefinition{Name: "consumer", Sources: []string{source.Name}}); e != nil {
		t.Fatal(e)
	}
	// A legacy orphan name without versions must not shadow a populated name.
	empty, e := defs.CreateSource(ctx, persistence.SourceDefinition{Name: "empty-priority", API: &persistence.APISpec{Endpoint: "https://example.org"}, Schedule: "1h"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, `INSERT INTO registry_entry(source_id,entry_type,name) VALUES($1,'MCP','com.example/item')`, empty.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = defs.UpdateView(ctx, "consumer", persistence.ViewDefinition{Name: "consumer", Sources: []string{empty.Name, source.Name}}); e != nil {
		t.Fatal(e)
	}
	reader, e := catalog.NewReader(entries)
	if e != nil {
		t.Fatal(e)
	}
	identity := catalog.EntryName{Kind: persistence.ServerKind, Name: "com.example/item"}
	got, e := reader.Get(ctx, "consumer", identity, catalog.VersionSelector{Latest: true})
	if e != nil || got.SourceID != source.ID {
		t.Fatalf("orphan won: %+v / %v", got, e)
	}
	// SQL errors and corrupt payload errors expose only the domain category.
	if _, e = pool.Exec(ctx, `UPDATE mcp_server SET server_meta='[]'`); e != nil {
		t.Fatal(e)
	}
	_, e = reader.Get(ctx, "consumer", identity, catalog.VersionSelector{Latest: true})
	if !errors.Is(e, persistence.ErrUnavailable) || strings.Contains(e.Error(), "server_meta") {
		t.Fatalf("corrupt payload: %v", e)
	}
	pool.Close()
	for _, operation := range []func() error{
		func() error {
			_, e := reader.List(ctx, "consumer", catalog.Query{Kind: persistence.ServerKind, Limit: 1})
			return e
		},
		func() error {
			_, e := reader.Get(ctx, "consumer", identity, catalog.VersionSelector{Latest: true})
			return e
		},
		func() error {
			_, e := reader.ListVersions(ctx, "consumer", identity, catalog.PageRequest{Limit: 1})
			return e
		},
	} {
		if e := operation(); !errors.Is(e, persistence.ErrUnavailable) || e.Error() != persistence.ErrUnavailable.Error() {
			t.Fatalf("closed backend: %v", e)
		}
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, e = reader.List(canceled, "consumer", catalog.Query{Kind: persistence.ServerKind, Limit: 1})
	if !errors.Is(e, context.Canceled) {
		t.Fatalf("cancel with closed backend: %v", e)
	}
}

func TestViewCursorHugePosition(t *testing.T) {
	t.Parallel()
	defs, pool := newBackend(t)
	ctx := t.Context()
	entries, e := postgres.NewEntries(ctx, pool, 65536)
	if e != nil {
		t.Fatal(e)
	}
	source, e := defs.CreateSource(ctx, persistence.SourceDefinition{Name: "huge-position", API: &persistence.APISpec{Endpoint: "https://example.org"}, Schedule: "1h"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = defs.CreateView(ctx, persistence.ViewDefinition{Name: "consumer", Sources: []string{source.Name}}); e != nil {
		t.Fatal(e)
	}
	var entryID string
	if e = pool.QueryRow(ctx, `INSERT INTO registry_entry(source_id,entry_type,name) VALUES($1,'SKILL','item') RETURNING id::text`, source.ID).Scan(&entryID); e != nil {
		t.Fatal(e)
	}
	for _, version := range []string{strings.Repeat("<", 800), strings.Repeat("<", 800) + "a"} {
		var id string
		if e = pool.QueryRow(ctx, `INSERT INTO entry_version(entry_id,name,version,description) VALUES($1,'item',$2,'legacy') RETURNING id::text`, entryID, version).Scan(&id); e != nil {
			t.Fatal(e)
		}
		if _, e = pool.Exec(ctx, `INSERT INTO skill(version_id,namespace) VALUES($1,'com.example')`, id); e != nil {
			t.Fatal(e)
		}
	}
	page, e := entries.ListViewEntries(ctx, "consumer", persistence.ViewQuery{Kind: persistence.SkillKind, Limit: 1})
	if !errors.Is(e, persistence.ErrInvalid) || page.NextCursor != "" || len(page.Entries) != 0 {
		t.Fatalf("truncated huge identity: %+v / %v", page, e)
	}
}
