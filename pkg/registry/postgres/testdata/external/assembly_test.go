package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/catalog"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/formats"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/httpapi"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/ingest"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/postgres"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/reconcile"
)

const upstream = `{"version":"1.0.0","meta":{"last_updated":"2025-01-15T10:30:00Z"},"data":{"servers":[{"$schema":"https://example.org/server.schema.json","name":"com.example/item","description":"winner","version":"2"},{"$schema":"https://example.org/server.schema.json","name":"com.example/item","description":"winner","version":"3"}],"skills":[{"namespace":"com.example","name":"item","description":"winner","version":"2"},{"namespace":"com.example","name":"item","description":"winner","version":"3"}],"plugins":[{"namespace":"com.example","name":"item","description":"winner","version":"2"},{"namespace":"com.example","name":"item","description":"winner","version":"3"}]}}`

// A replacement implementation, rather than replacement IO dependencies.
type hostFetcher struct {
	result  ingest.Result
	started chan struct{}
}

func (f *hostFetcher) Fetch(ctx context.Context, _ persistence.SourceDefinition) (ingest.Result, error) {
	if f.started != nil {
		close(f.started)
		<-ctx.Done()
		return ingest.Result{}, ctx.Err()
	}
	return f.result, nil
}

// This List-only replacement characterizes wire pass-through. A production host
// selection service must also supply coherent Get/history operations.
type selectedReader struct{ catalog.Reader }

func (selectedReader) List(context.Context, string, catalog.Query) (catalog.Page, error) {
	return catalog.Page{Entries: []persistence.Entry{
		{Kind: persistence.ServerKind, Server: &model.Server{Name: "com.example/z", Version: "1", Description: "host selected"}},
		{Kind: persistence.ServerKind, Server: &model.Server{Name: "com.example/a", Version: "1", Description: "host selected"}},
	}, NextCursor: "host-private:next"}, nil
}

func TestCompletePublicAssembly(t *testing.T) {
	// Only the parent test supplies an isolated schema-30 database. No migrations,
	// standalone config, registry-internal imports, or ambient production DSN.
	dsn := os.Getenv("REGISTRY_FIXTURE_DSN")
	if dsn == "" {
		t.Fatal("run through TestExternalCompleteAssembly to prepare an isolated database")
	}
	ctx := t.Context()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal("pool construction failed")
	}
	defer pool.Close() // The host owns the pool, not any public component.
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal("public operation failed:", err)
		}
	}
	defs, err := postgres.NewDefinitions(pool)
	must(err)
	entries, err := postgres.NewEntries(ctx, pool, 65536)
	must(err)
	jobs, err := postgres.NewJobs(pool, 65536)
	must(err)
	reads := 0
	readFile := func(ctx context.Context, _ string) ([]byte, error) {
		reads++
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return []byte(upstream), nil
	}
	fetcher := ingest.New(ingest.Options{ReadFile: readFile})
	options := reconcile.Options{PollInterval: time.Second, LeaseDuration: time.Minute}
	coordinator, err := reconcile.New(jobs, fetcher, options)
	must(err)
	admin, err := catalog.NewDefinitionAdmin(defs, defs, catalog.DefinitionAdminOptions{})
	must(err)
	publisher, err := catalog.NewPublisher(entries)
	must(err)
	diagnostics, err := catalog.NewDiagnostics(entries, jobs)
	must(err)
	reader, err := catalog.NewReader(entries)
	must(err)
	if reads != 0 || coordinator.Ready() {
		t.Fatal("constructor started ingestion")
	}
	mount := func(reader catalog.Reader) *httptest.Server {
		t.Helper()
		middleware := func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Host-owned admission; deliberately no runtime authentication change.
				if r.Header.Get("Host-Admission") != "fixture" {
					http.Error(w, "forbidden", http.StatusForbidden)
					return
				}
				w.Header().Set("Host-Middleware", "applied")
				next.ServeHTTP(w, r)
			})
		}
		consumer, err := httpapi.ConsumerRouter(reader, httpapi.ConsumerOptions{Middleware: []func(http.Handler) http.Handler{middleware}})
		must(err)
		management, err := httpapi.AdminRouter(httpapi.AdminOperations{Sources: admin, Views: admin, Publisher: publisher, Diagnostics: diagnostics}, httpapi.AdminOptions{Middleware: []func(http.Handler) http.Handler{middleware}})
		must(err)
		mux := chi.NewRouter()
		mux.Mount("/host/registry", consumer)
		mux.Mount("/host/v1", management)
		return httptest.NewServer(mux)
	}
	server := mount(reader)
	defer server.Close()
	request := func(method, path, body string, status int) []byte {
		t.Helper()
		r, err := http.NewRequestWithContext(ctx, method, server.URL+"/host"+path, strings.NewReader(body))
		must(err)
		r.Header.Set("Host-Admission", "fixture")
		res, err := server.Client().Do(r)
		must(err)
		defer res.Body.Close()
		data, err := io.ReadAll(res.Body)
		must(err)
		if res.StatusCode != status || res.Header.Get("Host-Middleware") != "applied" {
			t.Fatalf("%s %s: %d %s", method, path, res.StatusCode, data)
		}
		return data
	}
	res, err := server.Client().Get(server.URL + "/host/v1/sources")
	must(err)
	res.Body.Close()
	if res.StatusCode != 403 {
		t.Fatal("host middleware bypass")
	}
	request("PUT", "/v1/sources/raw", `{"file":{"path":"/host-supplied.json"},"syncPolicy":{"interval":"1h"}}`, 201)
	request("PUT", "/v1/sources/managed", `{"managed":{}}`, 201)
	request("PUT", "/v1/registries/public", `{"sources":["raw","managed"]}`, 201)
	view, err := admin.GetView(ctx, "public")
	must(err)
	if strings.Join(view.Sources, ",") != "raw,managed" {
		t.Fatal("lost ordered source links")
	}
	must(coordinator.Sync(ctx, "raw"))
	if reads != 1 {
		t.Fatal("replacement file dependency not used")
	}
	for _, version := range []string{"1", "2", "3"} {
		request("POST", "/v1/entries", fmt.Sprintf(`{"server":{"$schema":"https://example.org/server.schema.json","name":"com.example/item","description":"managed-only","version":%q}}`, version), 201)
		for _, kind := range []string{"skill", "plugin"} {
			request("POST", "/v1/entries", fmt.Sprintf(`{%q:{"namespace":"com.example","name":"item","description":"managed-only","version":%q}}`, kind, version), 201)
		}
	}
	raw, err := admin.GetSource(ctx, "raw")
	must(err)
	managed, err := admin.GetSource(ctx, "managed")
	must(err)
	checkLastGood := func() {
		t.Helper()
		for _, kind := range []persistence.EntryKind{persistence.ServerKind, persistence.SkillKind, persistence.PluginKind} {
			name := "item"
			if kind == persistence.ServerKind {
				name = "com.example/item"
			}
			got, err := entries.GetEntry(ctx, raw.ID, kind, name, persistence.VersionSelector{Latest: true})
			must(err)
			if got.Version != "3" {
				t.Fatal("last good latest lost")
			}
			page, err := entries.ListEntries(ctx, persistence.ListOptions{SourceID: raw.ID, Kind: kind, Limit: 30})
			must(err)
			if len(page.Entries) != 2 {
				t.Fatal("last good history lost")
			}
		}
	}
	for _, family := range []struct {
		route, name, kind string
		storage           persistence.EntryKind
	}{
		{"servers", "com.example%2Fitem", "mcp", persistence.ServerKind},
		{"x/dev.toolhive/skills", "com.example/item", "skill", persistence.SkillKind},
		{"x/dev.toolhive/plugins", "com.example/item", "plugin", persistence.PluginKind},
	} {
		base := "/registry/public/v0.1/" + family.route
		request("GET", base+"/"+family.name+"/versions/1", "", 404)
		data := request("GET", base+"?search=managed-only", "", 200)
		var page struct{ Metadata struct{ Count int } }
		must(json.Unmarshal(data, &page))
		if page.Metadata.Count != 0 {
			t.Fatal("search promoted lower-priority source")
		}
		request("GET", base+"/"+family.name+"/versions", "", 200)
		name := "item"
		if family.storage == persistence.ServerKind {
			name = "com.example/item"
		}
		must(publisher.DeleteManaged(ctx, family.storage, name, "1"))
		must(publisher.DeleteManaged(ctx, family.storage, name, "3"))
		got, err := entries.GetEntry(ctx, managed.ID, family.storage, name, persistence.VersionSelector{Latest: true})
		must(err)
		if got.Version != "2" {
			t.Fatal("managed latest not recomputed")
		}
	}
	checkLastGood()
	var diagnosticWire struct{ Entries []json.RawMessage }
	must(json.Unmarshal(request("GET", "/v1/registries/public/entries", "", 200), &diagnosticWire))
	if len(diagnosticWire.Entries) != 9 {
		t.Fatal("diagnostics shadowed source history")
	}
	baseline, err := jobs.Status(ctx, "raw")
	must(err)
	snapshot, err := formats.DecodeUpstream([]byte(upstream))
	must(err)
	invalid := *snapshot
	invalid.Data.Plugins = append(invalid.Data.Plugins, invalid.Data.Plugins[0])
	replacement := &hostFetcher{result: ingest.Result{Snapshot: invalid, Hash: "invalid"}}
	other, err := reconcile.New(jobs, replacement, options)
	must(err)
	if err := other.Sync(ctx, "raw"); !errors.Is(err, persistence.ErrInvalid) {
		t.Fatalf("invalid snapshot: %v", err)
	}
	checkLastGood()
	failed, err := jobs.Status(ctx, "raw")
	must(err)
	if !failed.BaselineValid || failed.LastHash != baseline.LastHash {
		t.Fatal("failure changed applied baseline")
	}
	replacement.result = ingest.Result{Snapshot: *snapshot, Hash: "host-implementation"}
	must(other.Sync(ctx, "raw"))
	checkLastGood()
	// Cancellation joins active work; the next successful empty snapshot is an
	// explicit replacement, not a cancellation interpreted as empty data.
	replacement.started = make(chan struct{})
	work, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- other.Sync(work, "raw") }()
	<-replacement.started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	checkLastGood()
	replacement.started = nil
	replacement.result = ingest.Result{Hash: "valid-empty"}
	must(other.Sync(ctx, "raw"))
	remaining, err := diagnostics.ListSourceVersions(ctx, "raw")
	must(err)
	if len(remaining) != 0 {
		t.Fatal("valid empty did not clear source")
	}
	remaining, err = diagnostics.ListSourceVersions(ctx, "managed")
	must(err)
	if len(remaining) != 3 {
		t.Fatal("empty replacement affected managed history")
	}
	stopped, stop := context.WithCancel(ctx)
	stop()
	must(other.Run(stopped))
	must(pool.Ping(ctx))
	_, err = fetcher.Fetch(ctx, raw) // Borrowed file client still belongs to host.
	must(err)
	selected := mount(selectedReader{Reader: reader})
	defer selected.Close()
	r, err := http.NewRequestWithContext(ctx, "GET", selected.URL+"/host/registry/public/v0.1/servers", nil)
	must(err)
	r.Header.Set("Host-Admission", "fixture")
	response, err := selected.Client().Do(r)
	must(err)
	defer response.Body.Close()
	var wire struct {
		Servers  []struct{ Server struct{ Name string } }
		Metadata struct {
			Count      int
			NextCursor string
		}
	}
	must(json.NewDecoder(response.Body).Decode(&wire))
	if response.StatusCode != 200 || len(wire.Servers) != 2 || wire.Servers[0].Server.Name != "com.example/z" || wire.Servers[1].Server.Name != "com.example/a" || wire.Metadata.Count != 2 || wire.Metadata.NextCursor != "host-private:next" {
		t.Fatal("HTTP changed host-selected page")
	}
}
