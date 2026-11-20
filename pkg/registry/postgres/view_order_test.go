package postgres_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	thvregistry "github.com/stacklok/toolhive-core/registry/types"

	"github.com/stacklok/toolhive-registry-server/database/dbtest"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/catalog"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/httpapi"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/postgres"
)

// TestViewListCollationOrder loads a few thousand versions with mixed-case and
// punctuated names and checks that consumer lists page in v1.5.4's ORDER BY
// name, version order under the database's default collation, that v1.5.4
// cursors resume at the right place, and that the list query still walks the
// (entry_type, name) index rather than sorting the view. It runs on the test
// server's default C collation and on ICU en-US, which is linguistic like a
// typical production database and where byte order would be wrong.
func TestViewListCollationOrder(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, collation string
		setup           func(*testing.T) (*pgx.Conn, func())
	}{
		{"test default C", "c:C", dbtest.SetupTestDB},
		{"ICU en-US", "i:en-US", func(t *testing.T) (*pgx.Conn, func()) {
			t.Helper()
			return dbtest.SetupTestDBWithICULocale(t, "en-US")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			conn, _ := tc.setup(t)
			checkViewListOrder(t, conn, tc.collation)
		})
	}
}

//nolint:gocyclo // One fixture feeds the order, cursor and plan checks.
func checkViewListOrder(t *testing.T, conn *pgx.Conn, wantCollation string) {
	t.Helper()
	tracer := &viewListTracer{}
	cfg, err := pgxpool.ParseConfig(conn.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.Tracer = tracer
	pool, err := pgxpool.NewWithConfig(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	ctx := t.Context()
	var collation string
	if err = pool.QueryRow(ctx, `SELECT datlocprovider::text||':'||coalesce(daticulocale,datcollate) FROM pg_database WHERE datname=current_database()`).Scan(&collation); err != nil {
		t.Fatal(err)
	}
	if collation != wantCollation {
		t.Fatalf("fixture collation %q", collation)
	}
	defs, err := postgres.NewDefinitions(pool)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := postgres.NewEntries(ctx, pool, 65536)
	if err != nil {
		t.Fatal(err)
	}
	source, err := defs.CreateSource(ctx, persistence.SourceDefinition{Name: "collation", API: &persistence.APISpec{Endpoint: "https://example.org"}, Schedule: "1h"})
	if err != nil {
		t.Fatal(err)
	}
	stems := []string{"a", "A", "b", "B", "ab", "a-b", "a_b", "a.b", "Zeta", "zeta", "alpha", "Alpha", "ALPHA", "al-pha", "al_pha", "al.pha", "x1", "X-1", "x_10", "x.2"}
	skillStems := []string{"a", "ab", "a-b", "alpha", "al-pha", "b", "x1", "x-1", "zeta", "z-eta"}
	versionSets := [][]string{{"1.0.0", "1.0.0-rc.1", "1.10.0"}, {"V2", "v2"}, {"1.2.0", "1.2.0_b", "1.2.0-B"}}
	snap := model.Snapshot{}
	for i := range 1200 {
		name := fmt.Sprintf("com.example/%s%d", stems[i%len(stems)], i/len(stems))
		for _, version := range versionSets[i%len(versionSets)] {
			snap.Data.Servers = append(snap.Data.Servers, model.Server{
				Schema: "https://example.org/server.schema.json", Name: name, Version: version, Description: "order " + name,
			})
		}
		// Skills exercise the same query path for another kind.
		if i < 300 {
			snap.Data.Skills = append(snap.Data.Skills, thvregistry.Skill{
				Namespace: "com.example", Name: skillStems[i%len(skillStems)] + fmt.Sprint(i), Version: "1", Description: "order",
			})
		}
	}
	loadStart := time.Now()
	if err = entries.ReplaceSnapshot(ctx, source, snap); err != nil {
		t.Fatal(err)
	}
	loadTime := time.Since(loadStart)
	if _, err = defs.CreateView(ctx, persistence.ViewDefinition{Name: "consumer", Sources: []string{source.Name}}); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `ANALYZE`); err != nil {
		t.Fatal(err)
	}
	// v1.5.4's consumer list order, under this database's default collation.
	rows, err := pool.Query(ctx, `SELECT v.name, v.version FROM entry_version v JOIN registry_entry e ON e.id=v.entry_id
		WHERE e.entry_type='MCP' ORDER BY v.name ASC, v.version ASC`)
	if err != nil {
		t.Fatal(err)
	}
	want, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (string, error) {
		var name, version string
		err := row.Scan(&name, &version)
		return name + "\x00" + version, err
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(want) != len(snap.Data.Servers) {
		t.Fatalf("fixture rows %d", len(want))
	}
	byteOrder := slices.Clone(want)
	slices.Sort(byteOrder)
	moved := 0
	for i := range want {
		if want[i] != byteOrder[i] {
			moved++
		}
	}
	if (moved == 0) != (wantCollation == "c:C") {
		t.Fatalf("%d positions differ from byte order under %s", moved, wantCollation)
	}
	walk := func(limit int, cursor string) ([]string, int, time.Duration) {
		t.Helper()
		var got []string
		pages := 0
		start := time.Now()
		for {
			page, err := entries.ListViewEntries(ctx, "consumer", persistence.ViewQuery{Kind: persistence.ServerKind, Limit: limit, Cursor: cursor})
			if err != nil {
				t.Fatal(err)
			}
			pages++
			for _, entry := range page.Entries {
				got = append(got, entry.Name+"\x00"+entry.Version)
			}
			if page.NextCursor == "" {
				return got, pages, time.Since(start)
			}
			cursor = page.NextCursor
		}
	}
	for _, limit := range []int{7, 37, 1000} {
		got, pages, elapsed := walk(limit, "")
		if !slices.Equal(got, want) {
			t.Fatalf("limit %d: order differs from ORDER BY name, version", limit)
		}
		t.Logf("limit %d: %d versions in %d pages, %s", limit, len(got), pages, elapsed)
	}
	t.Logf("%s: %d server versions (+300 skills) loaded in %s; %d of %d positions differ from byte order", wantCollation,
		len(want), loadTime, moved, len(want))
	// A cursor issued by v1.5.4 for any position resumes right after it.
	for _, k := range []int{0, 1, 2, 500, len(want) - 2} {
		name, version, _ := strings.Cut(want[k], "\x00")
		got, _, _ := walk(250, legacyCursor(name, version))
		if !slices.Equal(got, want[k+1:]) {
			t.Fatalf("v1.5.4 cursor at %d resumed at %q", k, got[:min(1, len(got))])
		}
	}
	// Skills share the walk; their first page must follow the same collation.
	skills, err := entries.ListViewEntries(ctx, "consumer", persistence.ViewQuery{Kind: persistence.SkillKind, Limit: 1000})
	if err != nil || len(skills.Entries) != 300 {
		t.Fatalf("skills: %d / %v", len(skills.Entries), err)
	}
	var skillOrder []string
	if err = pool.QueryRow(ctx, `SELECT array_agg(v.name ORDER BY v.name, v.version) FROM entry_version v JOIN registry_entry e ON e.id=v.entry_id WHERE e.entry_type='SKILL'`).Scan(&skillOrder); err != nil {
		t.Fatal(err)
	}
	for i, entry := range skills.Entries {
		if entry.Name != skillOrder[i] {
			t.Fatalf("skill %d: %s; want %s", i, entry.Name, skillOrder[i])
		}
	}
	// EXPLAIN the exact statement and arguments the list issued, for a first
	// page and a page that resumes from a cursor in the middle.
	for _, tc := range []struct {
		label  string
		cursor string
	}{
		{"first page", ""},
		{"cursor page", cursorAt(want[len(want)/2])},
	} {
		tracer.reset()
		if _, err = entries.ListViewEntries(ctx, "consumer", persistence.ViewQuery{Kind: persistence.ServerKind, Limit: 30, Cursor: tc.cursor}); err != nil {
			t.Fatal(err)
		}
		sql, args := tracer.captured()
		if sql == "" {
			t.Fatal("list statement not captured")
		}
		plan := explain(t, pool, sql, args)
		if !strings.Contains(plan, "registry_entry_type_name_idx") {
			t.Fatalf("%s does not use registry_entry_type_name_idx:\n%s", tc.label, plan)
		}
		t.Logf("%s %s plan (index walk and timing):\n%s", wantCollation, tc.label, planSummary(plan))
	}
}

// TestViewLegacyCursorHTTP checks that a cursor a v1.5.4 server issued keeps
// working over HTTP after the upgrade, for servers and skills, and that the
// client may change limit, search or version between pages.
func TestViewLegacyCursorHTTP(t *testing.T) {
	t.Parallel()
	defs, pool := newBackend(t)
	ctx := t.Context()
	entries, err := postgres.NewEntries(ctx, pool, 65536)
	if err != nil {
		t.Fatal(err)
	}
	source, err := defs.CreateSource(ctx, persistence.SourceDefinition{Name: "legacy-cursor", API: &persistence.APISpec{Endpoint: "https://example.org"}, Schedule: "1h"})
	if err != nil {
		t.Fatal(err)
	}
	snap := model.Snapshot{}
	for _, name := range []string{"alpha", "beta", "gamma"} {
		for _, version := range []string{"1.0.0", "2.0.0"} {
			snap.Data.Servers = append(snap.Data.Servers, model.Server{
				Schema: "https://example.org/server.schema.json", Name: "com.example/" + name, Version: version, Description: name + " server",
			})
			snap.Data.Skills = append(snap.Data.Skills, thvregistry.Skill{
				Namespace: "com.example", Name: name, Version: version, Description: name + " skill",
			})
		}
	}
	if err = entries.ReplaceSnapshot(ctx, source, snap); err != nil {
		t.Fatal(err)
	}
	if _, err = defs.CreateView(ctx, persistence.ViewDefinition{Name: "public", Sources: []string{source.Name}}); err != nil {
		t.Fatal(err)
	}
	reader, err := catalog.NewReader(entries)
	if err != nil {
		t.Fatal(err)
	}
	h, err := httpapi.ConsumerRouter(reader, httpapi.ConsumerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	mux := chi.NewRouter()
	mux.Mount("/registry", h)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	list := func(t *testing.T, path string) ([]string, string) {
		t.Helper()
		res, err := server.Client().Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		data, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != 200 {
			t.Fatalf("%s: %d %s", path, res.StatusCode, data)
		}
		var wire struct {
			Servers []struct {
				Server model.Server `json:"server"`
			} `json:"servers"`
			Skills   []model.Skill `json:"skills"`
			Metadata struct {
				NextCursor string `json:"nextCursor"`
			} `json:"metadata"`
		}
		if err := json.Unmarshal(data, &wire); err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, s := range wire.Servers {
			got = append(got, s.Server.Name+"@"+s.Server.Version)
		}
		for _, s := range wire.Skills {
			got = append(got, s.Name+"@"+s.Version)
		}
		return got, wire.Metadata.NextCursor
	}
	for _, tc := range []struct {
		name, base, prefix string
	}{
		{"servers", "/registry/public/v0.1/servers", "com.example/"},
		{"skills", "/registry/public/v0.1/x/dev.toolhive/skills", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cursor := url.QueryEscape(legacyCursor(tc.prefix+"alpha", "2.0.0"))
			for _, step := range []struct {
				query string
				want  []string
			}{
				{"?limit=2&cursor=" + cursor, []string{tc.prefix + "beta@1.0.0", tc.prefix + "beta@2.0.0"}},
				{"?limit=10&cursor=" + cursor, []string{tc.prefix + "beta@1.0.0", tc.prefix + "beta@2.0.0", tc.prefix + "gamma@1.0.0", tc.prefix + "gamma@2.0.0"}},
				{"?search=gamma&cursor=" + cursor, []string{tc.prefix + "gamma@1.0.0", tc.prefix + "gamma@2.0.0"}},
			} {
				if got, _ := list(t, tc.base+step.query); !slices.Equal(got, step.want) {
					t.Fatalf("%s: %v; want %v", step.query, got, step.want)
				}
			}
			// A cursor the upgraded server issued survives limit, search and
			// version changes on the next request.
			first, next := list(t, tc.base+"?limit=1")
			if !slices.Equal(first, []string{tc.prefix + "alpha@1.0.0"}) || next == "" {
				t.Fatalf("first page %v / %q", first, next)
			}
			next = url.QueryEscape(next)
			for _, step := range []struct {
				query string
				want  []string
			}{
				{"?limit=2&cursor=" + next, []string{tc.prefix + "alpha@2.0.0", tc.prefix + "beta@1.0.0"}},
				{"?limit=2&search=beta&cursor=" + next, []string{tc.prefix + "beta@1.0.0", tc.prefix + "beta@2.0.0"}},
			} {
				if got, _ := list(t, tc.base+step.query); !slices.Equal(got, step.want) {
					t.Fatalf("%s: %v; want %v", step.query, got, step.want)
				}
			}
			if tc.name == "servers" {
				for _, step := range []struct {
					query string
					want  []string
				}{
					{"?version=1.0.0&cursor=" + next, []string{tc.prefix + "beta@1.0.0", tc.prefix + "gamma@1.0.0"}},
					{"?version=latest&cursor=" + next, []string{tc.prefix + "alpha@2.0.0", tc.prefix + "beta@2.0.0", tc.prefix + "gamma@2.0.0"}},
				} {
					if got, _ := list(t, tc.base+step.query); !slices.Equal(got, step.want) {
						t.Fatalf("%s: %v; want %v", step.query, got, step.want)
					}
				}
			}
		})
	}
}

// legacyCursor encodes a position exactly as v1.5.4's service.EncodeCursor did.
func legacyCursor(name, version string) string {
	return base64.StdEncoding.EncodeToString([]byte(name + "," + version))
}

// cursorAt is legacyCursor for a "name\x00version" fixture key.
func cursorAt(key string) string {
	name, version, _ := strings.Cut(key, "\x00")
	return legacyCursor(name, version)
}

func explain(t *testing.T, pool *pgxpool.Pool, sql string, args []any) string {
	t.Helper()
	conn, err := pool.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	tx, err := conn.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err = tx.Exec(t.Context(), `SET LOCAL plan_cache_mode = force_custom_plan`); err != nil {
		t.Fatal(err)
	}
	rows, err := tx.Query(t.Context(), "EXPLAIN (ANALYZE, COSTS OFF, TIMING OFF, SUMMARY ON) "+sql, args...)
	if err != nil {
		t.Fatal(err)
	}
	lines, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	return strings.Join(lines, "\n")
}

// planSummary keeps the lines that show how the entry walk starts and ends.
func planSummary(plan string) string {
	lines := strings.Split(plan, "\n")
	var keep []string
	for i, line := range lines {
		if strings.Contains(line, "registry_entry_type_name_idx") {
			keep = append(keep, strings.TrimSpace(line))
			if i+1 < len(lines) {
				keep = append(keep, strings.TrimSpace(lines[i+1]))
			}
		}
		if strings.Contains(line, "Time:") || strings.Contains(line, "Limit (") || strings.Contains(line, "Incremental Sort") {
			keep = append(keep, strings.TrimSpace(line))
		}
	}
	return strings.Join(keep, "\n")
}

// viewListTracer records the SQL and arguments of the view list statement.
type viewListTracer struct {
	mu   sync.Mutex
	sql  string
	args []any
}

func (v *viewListTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.Contains(data.SQL, "name: ViewEntryList") {
		v.mu.Lock()
		v.sql, v.args = data.SQL, slices.Clone(data.Args)
		v.mu.Unlock()
	}
	return ctx
}

func (*viewListTracer) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func (v *viewListTracer) reset() {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.sql, v.args = "", nil
}

func (v *viewListTracer) captured() (string, []any) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.sql, v.args
}
