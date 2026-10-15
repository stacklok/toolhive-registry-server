package postgres_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/postgres"
)

func TestRawViewKindsAndLongFilterTokens(t *testing.T) {
	t.Parallel()
	defs, pool := newBackend(t)
	ctx := t.Context()
	a, e := defs.CreateSource(ctx, persistence.SourceDefinition{Name: "raw-a", API: &persistence.APISpec{Endpoint: "https://example.org"}, Schedule: "1h"})
	if e != nil {
		t.Fatal(e)
	}
	b, e := defs.CreateSource(ctx, persistence.SourceDefinition{Name: "raw-b", API: &persistence.APISpec{Endpoint: "https://example.org"}, Schedule: "1h"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = defs.CreateView(ctx, persistence.ViewDefinition{Name: "raw", Sources: []string{a.Name, b.Name}}); e != nil {
		t.Fatal(e)
	}
	long := strings.Repeat("x", 5000)
	for _, kind := range []persistence.EntryKind{persistence.ServerKind, persistence.SkillKind, persistence.PluginKind} {
		for _, source := range []persistence.SourceDefinition{a, b} {
			// Plugin exists only in B, despite A owning both other kinds by the same name.
			if kind == persistence.PluginKind && source.ID == a.ID {
				continue
			}
			var entryID string
			if e = pool.QueryRow(ctx, `INSERT INTO registry_entry(source_id,entry_type,name) VALUES($1,$2,'item') RETURNING id::text`, source.ID, kind).Scan(&entryID); e != nil {
				t.Fatal(e)
			}
			versions := []string{"2", "3"}
			if source.ID == b.ID {
				versions = []string{"1", "9"}
			}
			for _, version := range versions {
				var id string
				if e = pool.QueryRow(ctx, `INSERT INTO entry_version(entry_id,name,version,description) VALUES($1,'item',$2,$3) RETURNING id::text`, entryID, version, long).Scan(&id); e != nil {
					t.Fatal(e)
				}
				switch kind {
				case persistence.ServerKind:
					_, e = pool.Exec(ctx, `INSERT INTO mcp_server(version_id) VALUES($1)`, id)
				case persistence.SkillKind:
					_, e = pool.Exec(ctx, `INSERT INTO skill(version_id,namespace) VALUES($1,'com.example')`, id)
				case persistence.PluginKind:
					_, e = pool.Exec(ctx, `INSERT INTO plugin(version_id,namespace) VALUES($1,'com.example')`, id)
				}
				if e != nil {
					t.Fatal(e)
				}
			}
		}
	}
	// Reconcile latest pointers for this deliberately legacy-shaped raw fixture.
	entries, e := postgres.NewEntries(ctx, pool, 65536)
	if e != nil {
		t.Fatal(e)
	}
	for _, kind := range []persistence.EntryKind{persistence.ServerKind, persistence.SkillKind, persistence.PluginKind} {
		winner, best := a.ID, "3"
		if kind == persistence.PluginKind {
			winner, best = b.ID, "9"
		}
		got, e := entries.GetViewEntry(ctx, "raw", persistence.EntryName{Kind: kind, Name: "item"}, persistence.VersionSelector{Latest: true})
		if e != nil || got.SourceID != winner || got.Version != best {
			t.Fatalf("raw kind %s: %+v / %v", kind, got, e)
		}
		q := persistence.ViewQuery{Kind: kind, Search: long, Limit: 1}
		page, e := entries.ListViewEntries(ctx, "raw", q)
		if e != nil || len(page.Entries) != 1 || page.Entries[0].SourceID != winner || page.NextCursor == "" || len(page.NextCursor) > 4096 {
			t.Fatalf("long filter %s: %+v / %v", kind, page, e)
		}
		q.Cursor = page.NextCursor
		page, e = entries.ListViewEntries(ctx, "raw", q)
		if e != nil || len(page.Entries) != 1 || page.NextCursor != "" {
			t.Fatalf("long filter tail: %+v / %v", page, e)
		}
	}
}

func TestViewPageCoherentWithLinkMutation(t *testing.T) {
	t.Parallel()
	defs, pool := newBackend(t)
	ctx := t.Context()
	a, e := defs.CreateSource(ctx, persistence.SourceDefinition{Name: "coherent-a", API: &persistence.APISpec{Endpoint: "https://example.org"}, Schedule: "1h"})
	if e != nil {
		t.Fatal(e)
	}
	b, e := defs.CreateSource(ctx, persistence.SourceDefinition{Name: "coherent-b", API: &persistence.APISpec{Endpoint: "https://example.org"}, Schedule: "1h"})
	if e != nil {
		t.Fatal(e)
	}
	for _, source := range []persistence.SourceDefinition{a, b} {
		var entryID, id string
		if e = pool.QueryRow(ctx, `INSERT INTO registry_entry(source_id,entry_type,name) VALUES($1,'SKILL','item') RETURNING id::text`, source.ID).Scan(&entryID); e != nil {
			t.Fatal(e)
		}
		if e = pool.QueryRow(ctx, `INSERT INTO entry_version(entry_id,name,version,description) VALUES($1,'item','1','coherent') RETURNING id::text`, entryID).Scan(&id); e != nil {
			t.Fatal(e)
		}
		if _, e = pool.Exec(ctx, `INSERT INTO skill(version_id,namespace) VALUES($1,'com.example')`, id); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = defs.CreateView(ctx, persistence.ViewDefinition{Name: "coherent", Sources: []string{a.Name}}); e != nil {
		t.Fatal(e)
	}
	entries, e := postgres.NewEntries(ctx, pool, 65536)
	if e != nil {
		t.Fatal(e)
	}
	blocker, e := pool.BeginTx(ctx, pgx.TxOptions{})
	if e != nil {
		t.Fatal(e)
	}
	defer blocker.Rollback(context.Background())
	if _, e = blocker.Exec(ctx, `LOCK TABLE entry_version IN ACCESS EXCLUSIVE MODE`); e != nil {
		t.Fatal(e)
	}
	type result struct {
		page persistence.EntryPage
		err  error
	}
	out := make(chan result, 1)
	go func() {
		page, err := entries.ListViewEntries(ctx, "coherent", persistence.ViewQuery{Kind: persistence.SkillKind, Limit: 1})
		out <- result{page, err}
	}()
	deadline := time.After(5 * time.Second)
	for {
		var blocked bool
		e = pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE relation='entry_version'::regclass AND NOT granted)`).Scan(&blocked)
		if e != nil {
			t.Fatal(e)
		}
		if blocked {
			break
		}
		select {
		case <-deadline:
			t.Fatal("view query never reached payload read")
		case <-time.After(10 * time.Millisecond):
		}
	}
	if _, e = defs.UpdateView(ctx, "coherent", persistence.ViewDefinition{Name: "coherent", Sources: []string{b.Name}}); e != nil {
		t.Fatal(e)
	}
	if e = blocker.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	got := <-out
	if got.err != nil || len(got.page.Entries) != 1 || got.page.Entries[0].SourceID != a.ID {
		t.Fatalf("mixed definition/payload snapshot: %+v / %v", got.page, got.err)
	}
	page, e := entries.ListViewEntries(ctx, "coherent", persistence.ViewQuery{Kind: persistence.SkillKind, Limit: 1})
	if e != nil || len(page.Entries) != 1 || page.Entries[0].SourceID != b.ID {
		t.Fatalf("later call did not see mutation: %+v / %v", page, e)
	}
	// A raw kind row whose payload disappeared must not produce a synthetic value.
	if _, e = pool.Exec(ctx, `DELETE FROM skill`); e != nil {
		t.Fatal(e)
	}
	_, e = entries.ListViewEntries(ctx, "coherent", persistence.ViewQuery{Kind: persistence.SkillKind, Limit: 1})
	if !errors.Is(e, persistence.ErrUnavailable) {
		t.Fatalf("missing payload: %v", e)
	}
}
