package postgres_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	thvregistry "github.com/stacklok/toolhive-core/registry/types"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/catalog"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/httpapi"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/postgres"
)

// This deliberately lives with the backend tests, not in the HTTP package's
// dependency graph. It exercises real HTTP, default catalog and real PostgreSQL.
//
//nolint:paralleltest,tparallel // Subtests share a live HTTP fixture and its parent-scoped assertion helpers.
func TestConsumerHTTPPostgres(t *testing.T) {
	t.Parallel()
	defs, pool := newBackend(t)
	store, err := postgres.NewEntries(t.Context(), pool, 65536)
	if err != nil {
		t.Fatal(err)
	}
	create := func(name string) persistence.SourceDefinition {
		t.Helper()
		s, err := defs.CreateSource(t.Context(), persistence.SourceDefinition{Name: name, API: &persistence.APISpec{Endpoint: "https://example.org"}, Schedule: "1h"})
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	winner, loser := create("http-winner"), create("http-loser")
	winning, losing := model.Snapshot{}, model.Snapshot{}
	appendVersion := func(s *model.Snapshot, kind persistence.EntryKind, name, version, namespace, description string) {
		switch kind {
		case persistence.ServerKind:
			s.Data.Servers = append(s.Data.Servers, model.Server{Schema: "https://example.org/server.schema.json", Name: "com.example/" + name, Version: version, Description: description})
		case persistence.SkillKind:
			s.Data.Skills = append(s.Data.Skills, thvregistry.Skill{Namespace: namespace, Name: name, Version: version, Description: description, Status: "deprecated"})
		case persistence.PluginKind:
			s.Data.Plugins = append(s.Data.Plugins, thvregistry.Plugin{Namespace: namespace, Name: name, Version: version, Description: description, Status: "deprecated"})
		}
	}
	for _, kind := range []persistence.EntryKind{persistence.ServerKind, persistence.SkillKind, persistence.PluginKind} {
		for _, version := range []string{"2", "3"} {
			appendVersion(&winning, kind, "item", version, "com.example", "winner")
		}
		for _, version := range []string{"1", "9"} {
			appendVersion(&losing, kind, "item", version, "org.loser", "lower-only")
		}
		counts := []int{29, 30, 31}
		if kind == persistence.ServerKind {
			counts = []int{999, 1000, 1001}
		}
		for _, count := range counts {
			for i := range count {
				appendVersion(&winning, kind, fmt.Sprintf("history-%d", count), fmt.Sprintf("%04d", i), "com.example", "bounded")
			}
		}
	}
	if err := store.ReplaceSnapshot(t.Context(), winner, winning); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceSnapshot(t.Context(), loser, losing); err != nil {
		t.Fatal(err)
	}
	if _, err := defs.CreateView(t.Context(), persistence.ViewDefinition{Name: "public", Sources: []string{winner.Name, loser.Name}}); err != nil {
		t.Fatal(err)
	}
	reader, err := catalog.NewReader(store)
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
	defer server.Close()
	get := func(path string, code int) []byte {
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
		if res.StatusCode != code {
			t.Fatalf("%s: %d %s; want %d", path, res.StatusCode, data, code)
		}
		return data
	}
	decodePage := func(data []byte, key string) ([]model.Server, int, string) {
		t.Helper()
		var wire map[string]json.RawMessage
		if err := json.Unmarshal(data, &wire); err != nil {
			t.Fatal(err)
		}
		var meta struct {
			Count  int    `json:"count"`
			Cursor string `json:"nextCursor"`
		}
		if err := json.Unmarshal(wire["metadata"], &meta); err != nil {
			t.Fatal(err)
		}
		var items []model.Server
		if key == "servers" {
			var envelopes []struct {
				Server model.Server `json:"server"`
			}
			if err := json.Unmarshal(wire[key], &envelopes); err != nil {
				t.Fatal(err)
			}
			for _, envelope := range envelopes {
				items = append(items, envelope.Server)
			}
		} else if err := json.Unmarshal(wire[key], &items); err != nil {
			t.Fatal(err)
		}
		if len(items) != meta.Count {
			t.Fatalf("count mismatch: %s", data)
		}
		return items, meta.Count, meta.Cursor
	}
	for _, family := range []struct {
		key, path string
		kind      persistence.EntryKind
	}{
		{"servers", "/servers", persistence.ServerKind},
		{"skills", "/x/dev.toolhive/skills", persistence.SkillKind},
		{"plugins", "/x/dev.toolhive/plugins", persistence.PluginKind},
	} {
		t.Run(family.key, func(t *testing.T) {
			base := "/registry/public/v0.1" + family.path
			namePath := "/com.example/item"
			if family.kind == persistence.ServerKind {
				namePath = "/com.example%2Fitem"
			}
			get(base+namePath+"/versions/1", 404)
			latest := get(base+namePath+"/versions/latest", 200)
			var payload model.Server
			if family.kind == persistence.ServerKind {
				var envelope struct {
					Server model.Server `json:"server"`
				}
				if err := json.Unmarshal(latest, &envelope); err != nil {
					t.Fatal(err)
				}
				payload = envelope.Server
			} else {
				if err := json.Unmarshal(latest, &payload); err != nil {
					t.Fatal(err)
				}
				if string(get(base+namePath, 200)) != string(latest) {
					t.Fatal("ordinary GET differs from latest")
				}
				get(base+"/org.loser/item", 404)
			}
			if payload.Version != "3" || payload.Description != "winner" {
				t.Fatalf("latest promoted loser: %s", latest)
			}
			get(base+namePath+"/versions/2", 200)
			items, count, cursor := decodePage(get(base+namePath+"/versions?limit=bad&cursor=ignored&version=1&search=lower-only&updated_since=bad", 200), family.key)
			if count != 2 || items[0].Version != "2" || items[1].Version != "3" || cursor != "" {
				t.Fatalf("history: %+v / %s", items, cursor)
			}
			_, count, _ = decodePage(get(base+"?search=lower-only", 200), family.key)
			if count != 0 {
				t.Fatal("search promoted loser")
			}
			if family.kind == persistence.ServerKind {
				_, count, _ = decodePage(get(base+"?search=winner&version=1", 200), family.key)
				if count != 0 {
					t.Fatal("exact filter promoted loser")
				}
			}
			first, count, cursor := decodePage(get(base+"?search=winner&limit=1", 200), family.key)
			if count != 1 || first[0].Version != "2" || cursor == "" {
				t.Fatalf("first: %+v / %s", first, cursor)
			}
			second, count, tail := decodePage(get(base+"?search=winner&limit=1&cursor="+url.QueryEscape(cursor), 200), family.key)
			if count != 1 || second[0].Version != "3" || tail != "" {
				t.Fatalf("second: %+v / %s", second, tail)
			}
			get(base+"?search=changed&limit=1&cursor="+url.QueryEscape(cursor), 400)
			get(base+"?cursor=malformed", 400)
			counts, bound := []int{29, 30, 31}, 30
			if family.kind == persistence.ServerKind {
				counts, bound = []int{999, 1000, 1001}, 1000
			}
			for _, total := range counts {
				name := fmt.Sprintf("history-%d", total)
				historyPath := "/com.example/" + name
				if family.kind == persistence.ServerKind {
					historyPath = "/com.example%2F" + name
				}
				items, count, cursor := decodePage(get(base+historyPath+"/versions?limit=1&cursor=malformed", 200), family.key)
				if count != min(total, bound) || items[0].Version != "0000" || items[len(items)-1].Version != fmt.Sprintf("%04d", min(total, bound)-1) {
					t.Fatalf("history %d: %d", total, count)
				}
				if (cursor != "") != (total > bound && family.kind != persistence.ServerKind) {
					t.Fatalf("history cursor %d: %s", total, cursor)
				}
			}
			get(strings.Replace(base, "/public/", "/missing/", 1), 404)
		})
	}
}
