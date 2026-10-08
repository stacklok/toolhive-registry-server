package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/stacklok/toolhive-registry-server/database"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence/conformance"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/postgres"
)

func newBackend(t *testing.T) (*postgres.Definitions, *pgxpool.Pool) {
	t.Helper()
	conn, _ := database.SetupTestDB(t)
	pool, e := pgxpool.New(t.Context(), conn.Config().ConnString())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(pool.Close)
	d, e := postgres.NewDefinitions(pool)
	if e != nil {
		t.Fatal(e)
	}
	return d, pool
}
func TestConformance(t *testing.T) {
	t.Parallel()
	conformance.Run(t, func(t *testing.T) persistence.Definitions {
		t.Helper()
		d, _ := newBackend(t)
		return d
	})
	conformance.RunMulti(t, func(t *testing.T) (persistence.Definitions, persistence.Definitions) {
		t.Helper()
		a, pool := newBackend(t)
		otherPool, err := pgxpool.New(t.Context(), pool.Config().ConnString())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(otherPool.Close)
		b, err := postgres.NewDefinitions(otherPool)
		if err != nil {
			t.Fatal(err)
		}
		return a, b
	})
}
func TestBorrowedPoolAndLegacyClaims(t *testing.T) {
	t.Parallel()
	d, pool := newBackend(t)
	ctx := t.Context()
	_, e := pool.Exec(ctx, `INSERT INTO source(name,source_type,source_config,syncable,creation_type,claims)
 VALUES('claimed','managed','{}',false,'API','{"team":"a"}')`)
	if e != nil {
		t.Fatal(e)
	}
	_, e = d.UpdateSource(ctx, "claimed", persistence.SourceDefinition{Name: "claimed", Managed: &persistence.ManagedSpec{}})
	if !errors.Is(e, persistence.ErrConflict) {
		t.Fatalf("update claimed source: %v", e)
	}
	if e = d.DeleteSource(ctx, "claimed"); !errors.Is(e, persistence.ErrConflict) {
		t.Fatalf("delete claimed source: %v", e)
	}
	got, e := d.GetSource(ctx, "claimed")
	if e != nil {
		t.Fatal(e)
	}
	data, e := json.Marshal(got)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(data), "claims") || strings.Contains(string(data), "team") {
		t.Fatal(string(data))
	}
	var claims string
	e = pool.QueryRow(ctx, `SELECT claims::text FROM source WHERE name='claimed'`).Scan(&claims)
	if e != nil || claims != `{"team": "a"}` {
		t.Fatalf("claims changed: %s / %v", claims, e)
	}
	if e = pool.Ping(context.Background()); e != nil {
		t.Fatal(e)
	}
}
func TestUnsupportedLegacyFields(t *testing.T) {
	t.Parallel()
	d, pool := newBackend(t)
	ctx := t.Context()
	_, err := pool.Exec(ctx, `INSERT INTO source(name,source_type,source_config,sync_schedule,syncable,creation_type)
	 VALUES('legacy','api','{"endpoint":"https://example.org","extension":"keep"}',interval '1 second',true,'API')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = d.UpdateSource(ctx, "legacy", persistence.SourceDefinition{
		Name: "legacy", API: &persistence.APISpec{Endpoint: "https://new.example.org"}, Schedule: "1s",
	})
	if !errors.Is(err, persistence.ErrConflict) {
		t.Fatalf("expected legacy field conflict: %v", err)
	}
	var spec string
	if err = pool.QueryRow(ctx, `SELECT source_config::text FROM source WHERE name='legacy'`).Scan(&spec); err != nil {
		t.Fatal(err)
	}
	if spec != `{"endpoint": "https://example.org", "extension": "keep"}` {
		t.Fatal(spec)
	}
}

func TestDefinitionDeletePreservesClaimedEntries(t *testing.T) {
	t.Parallel()
	d, pool := newBackend(t)
	ctx := t.Context()
	source, e := d.CreateSource(ctx, persistence.SourceDefinition{Name: "claimed-entries", API: &persistence.APISpec{Endpoint: "https://example.org"}, Schedule: "1h"})
	if e != nil {
		t.Fatal(e)
	}
	_, e = pool.Exec(ctx, `INSERT INTO registry_entry(source_id,entry_type,name,claims) VALUES($1,'SKILL','owned','{"team":"a"}')`, source.ID)
	if e != nil {
		t.Fatal(e)
	}
	if e = d.DeleteSource(ctx, source.Name); !errors.Is(e, persistence.ErrConflict) {
		t.Fatalf("delete claimed entries: %v", e)
	}
	if _, e = d.GetSource(ctx, source.Name); e != nil {
		t.Fatalf("source removed: %v", e)
	}
	_, e = pool.Exec(ctx, `UPDATE source SET creation_type='CONFIG' WHERE id=$1`, source.ID)
	if e != nil {
		t.Fatal(e)
	}
	if e = d.Reconcile(ctx, nil, nil); !errors.Is(e, persistence.ErrConflict) {
		t.Fatalf("prune claimed entries: %v", e)
	}
	var claims string
	e = pool.QueryRow(ctx, `SELECT claims::text FROM registry_entry WHERE source_id=$1`, source.ID).Scan(&claims)
	if e != nil || !strings.Contains(claims, "team") {
		t.Fatalf("entries changed: %q / %v", claims, e)
	}
}

func TestReconcilePreservesLegacyClaims(t *testing.T) {
	t.Parallel()
	d, pool := newBackend(t)
	ctx := t.Context()
	_, err := pool.Exec(ctx, `INSERT INTO source(name,source_type,source_config,syncable,creation_type,claims)
	 VALUES('owned','managed','{}',false,'CONFIG','{"team":"a"}')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO registry(name,creation_type,claims) VALUES('owned-view','CONFIG','{"team":"a"}')`)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		sources []persistence.SourceDefinition
		views   []persistence.ViewDefinition
	}{
		{"prune", nil, nil},
		{"update", []persistence.SourceDefinition{{Name: "owned", Managed: &persistence.ManagedSpec{}}},
			[]persistence.ViewDefinition{{Name: "owned-view", Sources: []string{"owned"}}}},
	} {
		if err := d.Reconcile(ctx, tc.sources, tc.views); !errors.Is(err, persistence.ErrConflict) {
			t.Fatalf("%s reconcile claimed definition: %v", tc.name, err)
		}
	}
	var sourceClaims, viewClaims string
	if err = pool.QueryRow(ctx, `SELECT claims::text FROM source WHERE name='owned'`).Scan(&sourceClaims); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT claims::text FROM registry WHERE name='owned-view'`).Scan(&viewClaims); err != nil {
		t.Fatal(err)
	}
	if sourceClaims != `{"team": "a"}` || viewClaims != sourceClaims {
		t.Fatalf("claims changed: source=%s view=%s", sourceClaims, viewClaims)
	}
}

func TestReconcileUnknownFieldsRollback(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, spec, filter string
		git                bool
	}{
		{"top-level spec", `{"endpoint":"https://old.example","future":"keep"}`, `null`, false},
		{"top-level filter", `{"endpoint":"https://old.example"}`, `{"future":"keep"}`, false},
		{"nested filter", `{"endpoint":"https://old.example"}`, `{"names":{"include":["a"],"future":"keep"}}`, false},
		{"nested spec", `{"repository":"r","auth":{"username":"u","passwordFile":"/tmp/p","future":"keep"}}`, `null`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			d, pool := newBackend(t)
			ctx := t.Context()
			old := persistence.SourceDefinition{Name: "old", API: &persistence.APISpec{Endpoint: "https://old.example"}, Schedule: "1s"}
			if tc.git {
				old.API = nil
				old.Git = &persistence.GitSpec{Repository: "r", Auth: &persistence.GitAuth{Username: "u", PasswordFile: "/tmp/p"}}
			}
			other := persistence.SourceDefinition{Name: "other", File: &persistence.FileSpec{Data: "{}"}}
			if err := d.Reconcile(ctx, []persistence.SourceDefinition{old, other}, []persistence.ViewDefinition{{Name: "view", Sources: []string{"old", "other"}}}); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `UPDATE source SET source_config=$2::jsonb,filter_config=$3::jsonb WHERE name=$1`, "old", tc.spec, tc.filter); err != nil {
				t.Fatal(err)
			}
			if tc.git {
				old.Git.Repository = "changed"
			} else {
				old.API.Endpoint = "https://new.example"
			}
			other.File.Data = `{"changed":true}`
			err := d.Reconcile(ctx, []persistence.SourceDefinition{other, old}, []persistence.ViewDefinition{{Name: "view", Sources: []string{"other", "old"}}})
			if !errors.Is(err, persistence.ErrConflict) {
				t.Fatalf("reconcile: %v", err)
			}
			var spec, filter string
			if err := pool.QueryRow(ctx, `SELECT source_config::text, coalesce(filter_config::text,'null') FROM source WHERE name='old'`).Scan(&spec, &filter); err != nil {
				t.Fatal(err)
			}
			var wantSpec, wantFilter, gotSpec, gotFilter any
			for _, item := range []struct {
				raw  string
				dest *any
			}{{tc.spec, &wantSpec}, {tc.filter, &wantFilter}, {spec, &gotSpec}, {filter, &gotFilter}} {
				if err := json.Unmarshal([]byte(item.raw), item.dest); err != nil {
					t.Fatal(err)
				}
			}
			if !reflect.DeepEqual(wantSpec, gotSpec) || !reflect.DeepEqual(wantFilter, gotFilter) {
				t.Fatalf("metadata lost: %s / %s", spec, filter)
			}
			got, err := d.GetSource(ctx, "other")
			if err != nil {
				t.Fatal(err)
			}
			if got.File.Data != "{}" {
				t.Fatalf("other source changed: %+v", got)
			}
			view, err := d.GetView(ctx, "view")
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(view.Sources, []string{"old", "other"}) {
				t.Fatalf("view changed: %+v", view)
			}
		})
	}
}

func TestUnavailableSanitizesConnection(t *testing.T) {
	t.Parallel()
	pool, err := pgxpool.New(t.Context(), "postgres://user:secret-marker@127.0.0.1:1/db?connect_timeout=1")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	d, err := postgres.NewDefinitions(pool)
	if err != nil {
		t.Fatal(err)
	}
	_, err = d.GetSource(t.Context(), "missing")
	if !errors.Is(err, persistence.ErrUnavailable) || strings.Contains(err.Error(), "secret-marker") || strings.Contains(err.Error(), "127.0.0.1") {
		t.Fatalf("unsafe classification: %v", err)
	}
}

func TestActiveCancellation(t *testing.T) {
	t.Parallel()
	_, existing := newBackend(t)
	cfg := existing.Config()
	cfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	d, err := postgres.NewDefinitions(pool)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := pool.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	_, err = d.GetView(ctx, "missing")
	if !errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "postgres") {
		t.Fatalf("cancel: %v", err)
	}
}

func TestGetViewNeverMixesDeleteRecreate(t *testing.T) {
	t.Parallel()
	d, pool := newBackend(t)
	ctx := t.Context()
	for _, name := range []string{"old", "new"} {
		_, err := d.CreateSource(ctx, persistence.SourceDefinition{Name: name, File: &persistence.FileSpec{Data: "{}"}})
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err := d.CreateView(ctx, persistence.ViewDefinition{Name: "view", Sources: []string{"old"}})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		for i := 0; i < 40; i++ {
			tx, e := pool.Begin(ctx)
			if e != nil {
				done <- e
				return
			}
			_, e = tx.Exec(ctx, `DELETE FROM registry WHERE name='view'`)
			if e == nil {
				_, e = tx.Exec(ctx, `INSERT INTO registry(name,creation_type) VALUES('view','API')`)
			}
			name := "old"
			if i%2 == 0 {
				name = "new"
			}
			if e == nil {
				_, e = tx.Exec(ctx, `INSERT INTO registry_source(registry_id,source_id) SELECT r.id,s.id FROM registry r,source s WHERE r.name='view' AND s.name=$1`, name)
			}
			if e != nil {
				_ = tx.Rollback(ctx)
				done <- e
				return
			}
			if e = tx.Commit(ctx); e != nil {
				done <- e
				return
			}
		}
		done <- nil
	}()
	for i := 0; i < 100; i++ {
		v, e := d.GetView(ctx, "view")
		if e != nil || len(v.Sources) != 1 || v.Sources[0] != "old" && v.Sources[0] != "new" {
			t.Fatalf("mixed snapshot: %+v / %v", v, e)
		}
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestConstructor(t *testing.T) {
	t.Parallel()
	if _, e := postgres.NewDefinitions(nil); !errors.Is(e, persistence.ErrInvalid) {
		t.Fatal(e)
	}
}
