package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/catalog"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence/conformance"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/postgres"
)

func TestDiagnosticsConformance(t *testing.T) {
	t.Parallel()
	conformance.RunDiagnostics(t, func(t *testing.T) (persistence.Definitions, persistence.Entries, persistence.EntryDiagnostics) {
		t.Helper()
		defs, pool := newBackend(t)
		entries, err := postgres.NewEntries(t.Context(), pool, 65536)
		if err != nil {
			t.Fatal(err)
		}
		return defs, entries, entries
	})
}

func TestDiagnosticsLegacyTiedPositions(t *testing.T) {
	t.Parallel()
	conformance.RunDiagnosticsTiedPositions(t, func(t *testing.T) (persistence.Definitions, persistence.Entries, persistence.EntryDiagnostics, func(context.Context, string) error) {
		t.Helper()
		defs, pool := newBackend(t)
		entries, err := postgres.NewEntries(t.Context(), pool, 65536)
		if err != nil {
			t.Fatal(err)
		}
		return defs, entries, entries, func(ctx context.Context, name string) error {
			_, err := pool.Exec(ctx, `UPDATE registry_source SET position=0 WHERE registry_id=(SELECT id FROM registry WHERE name=$1)`, name)
			return err
		}
	})
}

func TestDiagnosticsRawLegacyAndStatus(t *testing.T) {
	t.Parallel()
	defs, pool := newBackend(t)
	ctx := t.Context()
	source, err := defs.CreateSource(ctx, persistence.SourceDefinition{Name: "raw", API: &persistence.APISpec{Endpoint: "https://private.example/secret"}, Schedule: "1h"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = defs.CreateView(ctx, persistence.ViewDefinition{Name: "raw", Sources: []string{source.Name}}); err != nil {
		t.Fatal(err)
	}
	entries, err := postgres.NewEntries(ctx, pool, 65536)
	if err != nil {
		t.Fatal(err)
	}
	jobs, err := postgres.NewJobs(pool, 65536)
	if err != nil {
		t.Fatal(err)
	}
	diag, err := catalog.NewDiagnostics(entries, jobs)
	if err != nil {
		t.Fatal(err)
	}
	status, err := diag.Status(ctx, source.Name)
	if err != nil || status.SourceID != source.ID || status.Phase != persistence.JobPending || status.BaselineValid {
		t.Fatalf("no attempt: %+v / %v", status, err)
	}
	job, err := jobs.Acquire(ctx, source.Name, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err = jobs.Fail(ctx, job, persistence.FailureFetch); err != nil {
		t.Fatal(err)
	}
	status, err = diag.Status(ctx, source.Name)
	if err != nil || status.Phase != persistence.JobFailed || status.Failure != persistence.FailureFetch || status.BaselineValid {
		t.Fatalf("failed/no baseline: %+v / %v", status, err)
	}
	if _, err = pool.Exec(ctx, `UPDATE registry_sync SET error_msg='private credential secret' WHERE source_id=$1`, source.ID); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []persistence.EntryKind{persistence.ServerKind, persistence.SkillKind, persistence.PluginKind} {
		var entryID string
		if err = pool.QueryRow(ctx, `INSERT INTO registry_entry(source_id,entry_type,name) VALUES($1,$2,'same') RETURNING id::text`, source.ID, kind).Scan(&entryID); err != nil {
			t.Fatal(err)
		}
		// Deliberately no payload or related tables: lightweight diagnostics must
		// not deserialize them, hide claimed rows, or synthesize NULL timestamps.
		for _, version := range []string{"a", "Z", "10", "2"} {
			if _, err = pool.Exec(ctx, `INSERT INTO entry_version(entry_id,name,version,title,description,created_at,updated_at)
			VALUES($1,'same',$2,'title','description',NULL,NULL)`, entryID, version); err != nil {
				t.Fatal(err)
			}
		}
	}
	rows, err := diag.ListSourceVersions(ctx, source.Name)
	if err != nil || len(rows) != 12 {
		t.Fatalf("raw source: %+v / %v", rows, err)
	}
	view, err := diag.ListViewVersions(ctx, "raw")
	if err != nil || len(view) != 12 {
		t.Fatalf("raw view: %+v / %v", view, err)
	}
	for i, row := range rows {
		if !row.CreatedAt.IsZero() || !row.UpdatedAt.IsZero() || row.Title != "title" || row.Description != "description" || view[i].DiagnosticVersion != row {
			t.Fatal("legacy projection changed", row)
		}
		if row.Version != []string{"10", "2", "Z", "a"}[i/3] || row.Kind != []persistence.EntryKind{persistence.ServerKind, persistence.PluginKind, persistence.SkillKind}[i%3] {
			t.Fatalf("not C/byte lexical with kind ties: %+v", row)
		}
	}
	status, err = diag.Status(ctx, source.Name)
	if err != nil || status.Failure != "" {
		t.Fatalf("legacy freeform failure leaked: %+v / %v", status, err)
	}
	data, err := json.Marshal([]any{rows, view, status})
	if err != nil || strings.Contains(string(data), "secret") || strings.Contains(string(data), "claim") || strings.Contains(string(data), "private") {
		t.Fatal("diagnostics leaked configuration or claims", err)
	}
	if _, err = entries.Publish(ctx, persistence.Entry{Kind: persistence.SkillKind, Name: "item", Version: "1"}); !errors.Is(err, persistence.ErrInvalid) {
		t.Fatal(err)
	}
	managed, err := defs.CreateSource(ctx, persistence.SourceDefinition{Name: "managed", Managed: &persistence.ManagedSpec{}})
	if err != nil {
		t.Fatal(err)
	}
	status, err = diag.Status(ctx, managed.Name)
	if err != nil || status.SourceID != managed.ID || status.Phase != persistence.JobPending || status.BaselineValid {
		t.Fatalf("managed status: %+v / %v", status, err)
	}
	// Recreate the same managed name; Status must resolve the new incarnation,
	// not retain a separate definition lookup or previous status observation.
	if err = defs.DeleteSource(ctx, managed.Name); err != nil {
		t.Fatal(err)
	}
	if _, err = diag.Status(ctx, managed.Name); !errors.Is(err, persistence.ErrNotFound) {
		t.Fatal(err)
	}
	recreated, err := defs.CreateSource(ctx, persistence.SourceDefinition{Name: managed.Name, Managed: &persistence.ManagedSpec{}})
	if err != nil {
		t.Fatal(err)
	}
	status, err = diag.Status(ctx, managed.Name)
	if err != nil || status.SourceID != recreated.ID || status.SourceID == managed.ID {
		t.Fatal("stale incarnation status", status, err)
	}
}

func TestDiagnosticsUnavailable(t *testing.T) {
	t.Parallel()
	_, pool := newBackend(t)
	entries, err := postgres.NewEntries(t.Context(), pool, 65536)
	if err != nil {
		t.Fatal(err)
	}
	pool.Close()
	if _, err = entries.ListSourceVersions(t.Context(), "source"); err != persistence.ErrUnavailable {
		t.Fatalf("source backend error leaked: %v", err)
	}
	if _, err = entries.ListViewVersions(t.Context(), "view"); err != persistence.ErrUnavailable {
		t.Fatalf("view backend error leaked: %v", err)
	}
}

func TestDiagnosticsSnapshotAcrossDefinitionMutation(t *testing.T) {
	t.Parallel()
	defs, pool := newBackend(t)
	ctx := t.Context()
	a, err := defs.CreateSource(ctx, persistence.SourceDefinition{Name: "a", API: &persistence.APISpec{Endpoint: "https://example.org"}, Schedule: "1h"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := defs.CreateSource(ctx, persistence.SourceDefinition{Name: "b", API: &persistence.APISpec{Endpoint: "https://example.org"}, Schedule: "1h"})
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range []persistence.SourceDefinition{a, b} {
		var id string
		if err = pool.QueryRow(ctx, `INSERT INTO registry_entry(source_id,entry_type,name) VALUES($1,'SKILL','item') RETURNING id::text`, source.ID).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `INSERT INTO entry_version(entry_id,name,version) VALUES($1,'item','1')`, id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = defs.CreateView(ctx, persistence.ViewDefinition{Name: "coherent", Sources: []string{a.Name}}); err != nil {
		t.Fatal(err)
	}
	entries, err := postgres.NewEntries(ctx, pool, 65536)
	if err != nil {
		t.Fatal(err)
	}
	blocker, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(context.Background())
	if _, err = blocker.Exec(ctx, `LOCK TABLE entry_version IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatal(err)
	}
	type result struct {
		rows []persistence.ViewDiagnosticVersion
		err  error
	}
	out := make(chan result, 1)
	readCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() { rows, e := entries.ListViewVersions(readCtx, "coherent"); out <- result{rows, e} }()
	deadline := time.After(5 * time.Second)
	for {
		var blocked bool
		if err = pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE relation='entry_version'::regclass AND NOT granted)`).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		select {
		case <-deadline:
			t.Fatal("diagnostic did not reach version-table lock")
		case <-time.After(10 * time.Millisecond):
		}
	}
	if _, err = defs.UpdateView(ctx, "coherent", persistence.ViewDefinition{Name: "coherent", Sources: []string{b.Name}}); err != nil {
		t.Fatal(err)
	}
	if err = blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	got := <-out
	if got.err != nil || len(got.rows) != 1 || got.rows[0].SourceID != a.ID {
		t.Fatalf("mixed existence/link/row snapshots: %+v / %v", got.rows, got.err)
	}
	current, err := entries.ListViewVersions(ctx, "coherent")
	if err != nil || len(current) != 1 || current[0].SourceID != b.ID {
		t.Fatal(current, err)
	}
	// Legacy links can have equal positions (the schema does not make them unique).
	if _, err = defs.UpdateView(ctx, "coherent", persistence.ViewDefinition{Name: "coherent", Sources: []string{b.Name, a.Name}}); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE registry_source SET position=0 WHERE registry_id=(SELECT id FROM registry WHERE name=$1)`, "coherent"); err != nil {
		t.Fatal(err)
	}
	current, err = entries.ListViewVersions(ctx, "coherent")
	if err != nil || len(current) != 2 || current[0].SourceID != a.ID || current[1].SourceID != b.ID {
		t.Fatal("legacy tied positions are not deterministically source-name ordered", current, err)
	}
}
