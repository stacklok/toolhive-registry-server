package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/catalog"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/httpapi"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/postgres"
)

// Real HTTP -> default application capabilities -> PostgreSQL, without legacy
// production assembly or consumer selection inside the admin router.
func TestAdminHTTPPostgres(t *testing.T) {
	t.Parallel()
	defs, pool := newBackend(t)
	ctx := t.Context()
	store, err := postgres.NewEntries(ctx, pool, 65536)
	if err != nil {
		t.Fatal(err)
	}
	jobs, err := postgres.NewJobs(pool, 65536)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := catalog.NewDefinitionAdmin(defs, defs, catalog.DefinitionAdminOptions{})
	if err != nil {
		t.Fatal(err)
	}
	publisher, err := catalog.NewPublisher(store)
	if err != nil {
		t.Fatal(err)
	}
	diagnostics, err := catalog.NewDiagnostics(store, jobs)
	if err != nil {
		t.Fatal(err)
	}
	h, err := httpapi.AdminRouter(httpapi.AdminOperations{Sources: admin, Views: admin, Publisher: publisher, Diagnostics: diagnostics}, httpapi.AdminOptions{})
	if err != nil {
		t.Fatal(err)
	}
	mux := chi.NewRouter()
	mux.Mount("/v1", h)
	server := httptest.NewServer(mux)
	defer server.Close()
	request := func(method, path, body string, code int) []byte {
		t.Helper()
		r, err := http.NewRequestWithContext(ctx, method, server.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		res, err := server.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		data, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != code {
			t.Fatalf("%s %s: %d %s; want %d", method, path, res.StatusCode, data, code)
		}
		return data
	}
	request("POST", "/v1/entries", `{"skill":{"namespace":"com.example","name":"item","version":"1","description":"published"}}`, 500)
	request("PUT", "/v1/sources/managed", `{"managed":{}}`, 201)
	request("PUT", "/v1/sources/managed", `{"managed":{}}`, 200)
	request("PUT", "/v1/sources/second-managed", `{"managed":{}}`, 409)
	request("PUT", "/v1/registries/public", `{"sources":["absent"]}`, 400)
	request("PUT", "/v1/sources/raw", `{"file":{"path":"/registry.json"},"syncPolicy":{"interval":"1h"}}`, 201)
	request("PUT", "/v1/sources/raw", `{"file":{"url":"https://example.org/registry.json"},"syncPolicy":{"interval":"1h"}}`, 400)
	request("PUT", "/v1/sources/raw", `{"api":{"endpoint":"https://example.org"},"syncPolicy":{"interval":"1h"}}`, 400)
	request("PUT", "/v1/registries/public", `{"sources":["raw","managed"]}`, 201)
	request("PUT", "/v1/registries/public", `{"sources":["raw","managed"]}`, 200)
	request("DELETE", "/v1/sources/raw", "", 409)
	if err := defs.Reconcile(ctx, []persistence.SourceDefinition{{Name: "configured", File: &persistence.FileSpec{Path: "/registry.json"}, Schedule: "1h"}}, nil); err != nil {
		t.Fatal(err)
	}
	request("PUT", "/v1/sources/configured", `{"file":{"path":"/registry.json"},"syncPolicy":{"interval":"1h"}}`, 403)
	request("DELETE", "/v1/sources/configured", "", 403)
	for _, version := range []string{"1", "2", "3"} {
		request("POST", "/v1/entries", `{"server":{"$schema":"https://example.org/server.schema.json","name":"com.example/item","description":"managed","version":"`+version+`"}}`, 201)
		request("POST", "/v1/entries", `{"skill":{"namespace":"com.example","name":"item","description":"managed","version":"`+version+`","metadata":{"claims":null}}}`, 201)
		request("POST", "/v1/entries", `{"plugin":{"namespace":"com.example","name":"item","description":"managed","version":"`+version+`","_meta":{"claims":{}}}}`, 201)
	}
	request("POST", "/v1/entries", `{"skill":{"namespace":"com.example","name":"item","description":"duplicate","version":"1"}}`, 409)
	raw, err := defs.GetSource(ctx, "raw")
	if err != nil {
		t.Fatal(err)
	}
	snapshot := model.Snapshot{}
	snapshot.Data.Servers = []model.Server{{Schema: "https://example.org/server.schema.json", Name: "com.example/item", Version: "9", Description: "higher priority"}}
	if err := store.ReplaceSnapshot(ctx, raw, snapshot); err != nil {
		t.Fatal(err)
	}
	var view struct {
		Entries []struct {
			EntryType, Name, Version, SourceName string
			Position                             int32
		}
	}
	if err := json.Unmarshal(request("GET", "/v1/registries/public/entries", "", 200), &view); err != nil {
		t.Fatal(err)
	}
	if len(view.Entries) != 10 {
		t.Fatalf("shadowed versions missing: %+v", view)
	}
	var source struct {
		Entries []struct {
			EntryType, Name string
			Versions        []struct{ Version string }
		}
	}
	if err := json.Unmarshal(request("GET", "/v1/sources/managed/entries", "", 200), &source); err != nil {
		t.Fatal(err)
	}
	if len(source.Entries) != 3 {
		t.Fatal(source)
	}
	for _, entry := range source.Entries {
		if len(entry.Versions) != 3 {
			t.Fatal(entry)
		}
	}
	managed, err := defs.GetSource(ctx, "managed")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		kind             persistence.EntryKind
		wire, name, path string
	}{
		{persistence.ServerKind, "server", "com.example/item", "com.example%2Fitem"},
		{persistence.SkillKind, "skill", "item", "item"},
		{persistence.PluginKind, "plugin", "item", "item"},
	} {
		request("DELETE", "/v1/entries/"+tc.wire+"/"+tc.path+"/versions/3", "", 204)
		request("DELETE", "/v1/entries/"+tc.wire+"/"+tc.path+"/versions/3", "", 404)
		latest, err := store.GetEntry(ctx, managed.ID, tc.kind, tc.name, persistence.VersionSelector{Latest: true})
		if err != nil || latest.Version != "2" {
			t.Fatal("managed latest wrong", latest, err)
		}
		if _, err := store.GetEntry(ctx, managed.ID, tc.kind, tc.name, persistence.VersionSelector{Exact: "1"}); err != nil {
			t.Fatal("history lost", err)
		}
	}
	if _, err := store.GetEntry(ctx, raw.ID, persistence.ServerKind, "com.example/item", persistence.VersionSelector{Exact: "9"}); err != nil {
		t.Fatal("other source changed", err)
	}
	// Every malformed/retired-field body is rejected before any persistent call.
	before, err := store.ListSourceVersions(ctx, "managed")
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{
		`{"claims":null,"skill":{"namespace":"com.example","name":"forbidden","version":"1","description":"invalid"}}`,
		`{"CLAIMS":{},"claims":null,"plugin":{}}`,
		`{"server":{},"skill":{}}`,
		`{"skill":{"namespace":"com.example","name":"forbidden","version":"1","description":"invalid"}} {}`,
		`{"skill":`,
	} {
		request("POST", "/v1/entries", body, 400)
	}
	request("POST", "/v1/entries", `{"unknown":"`+strings.Repeat("a", int(httpapi.MaxAdminBodyBytes))+`"}`, 413)
	request("PUT", "/v1/sources/forbidden", `{"managed":{},"Claims":null}`, 400)
	request("PUT", "/v1/registries/forbidden", `{"sources":["raw"],"claims":{}}`, 400)
	after, err := store.ListSourceVersions(ctx, "managed")
	if err != nil || len(after) != len(before) {
		t.Fatal("bad body mutated entries", before, after, err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM source WHERE name='forbidden') + (SELECT count(*) FROM registry WHERE name='forbidden')`).Scan(&count); err != nil || count != 0 {
		t.Fatal("bad body mutated definitions", count, err)
	}

	// A real committed definition followed by a canceled refresh must not be
	// reported as rollback or reclassified through the wrapped context error.
	partialAdmin, err := catalog.NewDefinitionAdmin(defs, defs, catalog.DefinitionAdminOptions{
		NotifyRefresh: func(context.Context) error {
			return errors.Join(context.Canceled, errors.New("private callback password"))
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	observed := make(chan httpapi.MutationResult, 1)
	partialHandler, err := httpapi.AdminRouter(httpapi.AdminOperations{Sources: partialAdmin, Views: partialAdmin, Publisher: publisher, Diagnostics: diagnostics}, httpapi.AdminOptions{
		Decorate: func(_ httpapi.RouteDescriptor, next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				next.ServeHTTP(w, r.WithContext(httpapi.WithMutationObserver(r.Context(), func(result httpapi.MutationResult) { observed <- result })))
			})
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	partialMux := chi.NewRouter()
	partialMux.Mount("/v1", partialHandler)
	partialServer := httptest.NewServer(partialMux)
	defer partialServer.Close()
	req, err := http.NewRequestWithContext(ctx, "PUT", partialServer.URL+"/v1/sources/committed", strings.NewReader(`{"api":{"endpoint":"https://example.org"},"syncPolicy":{"interval":"1h"}}`))
	if err != nil {
		t.Fatal(err)
	}
	res, err := partialServer.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	var committed struct {
		Committed bool
		Resource  struct{ Name, CreationType string }
	}
	if err := json.Unmarshal(data, &committed); err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 503 || !committed.Committed || committed.Resource.Name != "committed" || committed.Resource.CreationType != "API" || strings.Contains(string(data), "private callback") {
		t.Fatal(res.StatusCode, string(data))
	}
	if stored, err := defs.GetSource(ctx, "committed"); err != nil || stored.Origin != persistence.OriginAPI {
		t.Fatal(stored, err)
	}
	select {
	case outcome := <-observed:
		if !outcome.Committed || outcome.AuditEvent != "source.create" {
			t.Fatal(outcome)
		}
	default:
		t.Fatal("committed mutation missing audit observation")
	}
}
