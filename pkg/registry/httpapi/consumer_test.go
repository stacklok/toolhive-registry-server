package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/catalog"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/formats"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/httpapi"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

type selectedPages struct {
	calls     int
	operation httpapi.Operation
	view      string
	query     catalog.Query
	name      catalog.EntryName
	selector  catalog.VersionSelector
	request   catalog.PageRequest
	page      catalog.Page
	entry     persistence.Entry
	err       error
}

func (s *selectedPages) List(_ context.Context, view string, query catalog.Query) (catalog.Page, error) {
	s.calls++
	s.operation = httpapi.List
	s.view = view
	s.query = query
	return s.page, s.err
}
func (s *selectedPages) Get(_ context.Context, view string, name catalog.EntryName, selector catalog.VersionSelector) (persistence.Entry, error) {
	s.calls++
	s.operation = httpapi.Get
	s.view = view
	s.name = name
	s.selector = selector
	return s.entry, s.err
}
func (s *selectedPages) ListVersions(_ context.Context, view string, name catalog.EntryName, page catalog.PageRequest) (catalog.Page, error) {
	s.calls++
	s.operation = httpapi.ListVersions
	s.view = view
	s.name = name
	s.request = page
	return s.page, s.err
}

func mounted(t *testing.T, pages httpapi.ConsumerPages, options httpapi.ConsumerOptions) http.Handler {
	t.Helper()
	h, err := httpapi.ConsumerRouter(pages, options)
	if err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	r.Mount("/registry", h)
	return r
}
func response(t *testing.T, h http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(method, path, nil))
	return w
}
func entry(kind persistence.EntryKind, name, version string) persistence.Entry {
	e := persistence.Entry{Kind: kind, Name: name, Version: version}
	switch kind {
	case persistence.ServerKind:
		e.Server = &model.Server{Name: name, Version: version, Description: "selected", Meta: &upstreamv0.ServerMeta{PublisherProvided: map[string]any{"host": "kept"}}}
	case persistence.SkillKind:
		e.Skill = &model.Skill{Namespace: "com.example", Name: name, Version: version, ID: "private", IsLatest: true, Status: "deprecated", Metadata: map[string]any{"host": "kept"}}
	case persistence.PluginKind:
		e.Plugin = &model.Plugin{Namespace: "com.example", Name: name, Version: version, ID: "private", Status: "deprecated", Meta: map[string]any{"host": "kept"}}
	}
	return e
}
func expectedPage(kind persistence.EntryKind, entries []persistence.Entry, cursor string) any {
	switch kind {
	case persistence.ServerKind:
		items := make([]*model.Server, len(entries))
		for i := range entries {
			items[i] = entries[i].Server
		}
		return formats.ServerPage(items, cursor)
	case persistence.SkillKind:
		items := make([]*model.Skill, len(entries))
		for i := range entries {
			items[i] = entries[i].Skill
		}
		return formats.SkillPage(items, cursor)
	case persistence.PluginKind:
		items := make([]*model.Plugin, len(entries))
		for i := range entries {
			items[i] = entries[i].Plugin
		}
		return formats.PluginPage(items, cursor)
	}
	panic("unknown test kind")
}
func expectedEntry(e persistence.Entry) any {
	switch e.Kind {
	case persistence.ServerKind:
		return formats.ServerPayload(e.Server)
	case persistence.SkillKind:
		return formats.SkillPayload(e.Skill)
	case persistence.PluginKind:
		return formats.PluginPayload(e.Plugin)
	}
	panic("unknown test kind")
}
func assertJSON(t *testing.T, w *httptest.ResponseRecorder, value any) {
	t.Helper()
	want, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || w.Header().Get("Content-Type") != "application/json" || w.Body.String() != string(want)+"\n" {
		t.Fatalf("response %d %s; want %s", w.Code, w.Body.String(), want)
	}
}

func TestAllRoutesPreserveHostSelection(t *testing.T) {
	t.Parallel()
	routes := httpapi.ConsumerRoutes()
	if len(routes) != 13 {
		t.Fatal(routes)
	}
	for _, route := range routes {
		t.Run(route.Template, func(t *testing.T) {
			t.Parallel()
			items := []persistence.Entry{entry(route.Kind, "z", "raw-9"), entry(route.Kind, "a", "1")}
			fake := &selectedPages{page: catalog.Page{Entries: items, NextCursor: "host cursor +/?="}, entry: items[0]}
			decorations, invocations := 0, 0
			h := mounted(t, fake, httpapi.ConsumerOptions{
				Middleware: []func(http.Handler) http.Handler{func(next http.Handler) http.Handler {
					return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						w.Header().Set("Host-Middleware", "yes")
						next.ServeHTTP(w, r)
					})
				}},
				Decorate: func(d httpapi.RouteDescriptor, next http.Handler) http.Handler {
					decorations++
					return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						invocations++
						if d != route {
							t.Errorf("wrong classification: %+v", d)
						}
						next.ServeHTTP(w, r)
					})
				},
			})
			if fake.calls != 0 || decorations != len(routes) {
				t.Fatalf("construction: %d / %d", fake.calls, decorations)
			}
			path := "/registry" + strings.NewReplacer("{registryName}", "public", "{serverName}", "com.example%2Fitem", "{namespace}", "com%2Eexample", "{name}", "item", "{version}", "1.0%2Fraw").Replace(route.Template)
			query := "?cursor=ignored&limit=bad&search=ignored&updated_since=bad&version=ignored"
			if route.Operation == httpapi.List {
				query = ""
			}
			w := response(t, h, "GET", path+query)
			if fake.calls != 1 || invocations != 1 || w.Header().Get("Host-Middleware") != "yes" || fake.view != "public" {
				t.Fatalf("dispatch: %+v / %d", fake, invocations)
			}
			switch route.Operation {
			case httpapi.List:
				assertJSON(t, w, expectedPage(route.Kind, items, fake.page.NextCursor))
				limit := 50
				if route.Kind == persistence.ServerKind {
					limit = 30
				}
				if fake.query != (catalog.Query{Kind: route.Kind, Limit: limit}) {
					t.Fatal(fake.query)
				}
			case httpapi.ListVersions:
				cursor, limit := fake.page.NextCursor, 30
				if route.Kind == persistence.ServerKind {
					cursor = ""
					limit = 1000
				}
				if fake.request != (catalog.PageRequest{Limit: limit}) {
					t.Fatal(fake.request)
				}
				assertJSON(t, w, expectedPage(route.Kind, items, cursor))
			case httpapi.Get, httpapi.GetLatest:
				assertJSON(t, w, expectedEntry(fake.entry))
				selector := catalog.VersionSelector{Exact: "1.0/raw"}
				if route.Operation == httpapi.GetLatest {
					selector = catalog.VersionSelector{Latest: true}
				}
				if fake.selector != selector {
					t.Fatal(fake.selector)
				}
			}
			if route.Operation != httpapi.List {
				name := catalog.EntryName{Kind: route.Kind, Namespace: "com.example", Name: "item"}
				if route.Kind == persistence.ServerKind {
					name.Namespace = ""
					name.Name = "com.example/item"
				}
				if fake.name != name {
					t.Fatal(fake.name)
				}
			}
		})
	}
}

func TestRouteInventoryAndUnsupportedMethods(t *testing.T) {
	t.Parallel()
	fake := &selectedPages{}
	h, err := httpapi.ConsumerRouter(fake, httpapi.ConsumerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	inventory := map[string]httpapi.RouteDescriptor{}
	for _, d := range httpapi.ConsumerRoutes() {
		inventory[d.Method+" "+d.Template] = d
	}
	err = chi.Walk(h.(chi.Routes), func(method, path string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		key := method + " " + path
		d, ok := inventory[key]
		if !ok || d.AuditEvent == "" || d.Operation == "" || d.ResourceType == "" {
			return fmt.Errorf("unclassified: %s", key)
		}
		delete(inventory, key)
		return nil
	})
	if err != nil || len(inventory) != 0 {
		t.Fatalf("manifest: %v / %v", err, inventory)
	}
	for _, method := range []string{"HEAD", "OPTIONS", "POST", "PUT", "DELETE"} {
		for _, d := range httpapi.ConsumerRoutes() {
			path := strings.NewReplacer("{registryName}", "public", "{serverName}", "com.example%2Fitem", "{namespace}", "com.example", "{name}", "item", "{version}", "latest").Replace(d.Template)
			w := response(t, h, method, path)
			if w.Code != 405 {
				t.Fatalf("%s %s: %d", method, path, w.Code)
			}
		}
	}
	for _, path := range []string{"/v1/sources", "/v1/entries", "/public/v0.1/unknown", "/health", "/public/v0.1/servers/com.example/item/versions"} {
		if w := response(t, h, "GET", path); w.Code != 404 {
			t.Fatalf("%s: %d", path, w.Code)
		}
	}
	if fake.calls != 0 {
		t.Fatal(fake.calls)
	}
}

func TestConstructorValidation(t *testing.T) {
	t.Parallel()
	var typedNil *selectedPages
	for _, pages := range []httpapi.ConsumerPages{nil, typedNil} {
		if _, err := httpapi.ConsumerRouter(pages, httpapi.ConsumerOptions{}); !errors.Is(err, persistence.ErrInvalid) {
			t.Fatal(err)
		}
	}
	for _, options := range []httpapi.ConsumerOptions{
		{Middleware: []func(http.Handler) http.Handler{nil}},
		{Decorate: func(httpapi.RouteDescriptor, http.Handler) http.Handler { return nil }},
	} {
		if _, err := httpapi.ConsumerRouter(&selectedPages{}, options); !errors.Is(err, persistence.ErrInvalid) {
			t.Fatal(err)
		}
	}
}

func TestListQueryCompatibility(t *testing.T) {
	t.Parallel()
	for _, kind := range []persistence.EntryKind{persistence.ServerKind, persistence.SkillKind, persistence.PluginKind} {
		base := "/registry/public/v0.1/servers"
		def := 30
		if kind == persistence.SkillKind {
			base = "/registry/public/v0.1/x/dev.toolhive/skills"
			def = 50
		}
		if kind == persistence.PluginKind {
			base = "/registry/public/v0.1/x/dev.toolhive/plugins"
			def = 50
		}
		for _, tc := range []struct {
			query string
			limit int
			code  int
		}{
			{"", def, 200}, {"?limit=1", 1, 200}, {"?limit=100", 100, 200},
			{"?limit=0", 0, 400}, {"?limit=-1", 0, 400}, {"?limit=oops", 0, 400}, {"?limit=999999999999999999999999", 0, 400},
			{"?search=%00", 0, 400}, {"?search=%FF", 0, 400}, {"?cursor=%00", 0, 400}, {"?cursor=" + strings.Repeat("a", 4097), 0, 400}, {"?search=%ZZ", 0, 400},
			{"?limit=1001", 1000, 200}, {"?search=" + strings.Repeat("a", 16385), 0, 400},
		} {
			if tc.limit == 1000 && kind != persistence.ServerKind {
				tc.code = 400
			}
			t.Run(string(kind)+tc.query[:min(len(tc.query), 50)], func(t *testing.T) {
				t.Parallel()
				fake := &selectedPages{}
				h := mounted(t, fake, httpapi.ConsumerOptions{})
				w := response(t, h, "GET", base+tc.query)
				if w.Code != tc.code {
					t.Fatalf("%s: %d %s", tc.query, w.Code, w.Body.String())
				}
				if tc.code == 400 {
					if fake.calls != 0 {
						t.Fatal(fake.calls)
					}
					return
				}
				if fake.calls != 1 || fake.query.Limit != tc.limit {
					t.Fatal(fake)
				}
			})
		}
		fake := &selectedPages{}
		h := mounted(t, fake, httpapi.ConsumerOptions{})
		w := response(t, h, "GET", base+"?search=%20literal%25_%20&cursor=%20host%20token%20&version=latest&updated_since=2025-01-01T01:02:03.123Z&status=active,deprecated")
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
		if kind == persistence.ServerKind {
			stamp, _ := time.Parse(time.RFC3339, "2025-01-01T01:02:03.123Z")
			if fake.query.Search != " literal%_ " || fake.query.Cursor != " host token " || !fake.query.VersionSelector.Latest || !fake.query.UpdatedSince.Equal(stamp) {
				t.Fatal(fake.query)
			}
		} else if fake.query.Search != "literal%_" || fake.query.Cursor != "host token" || fake.query.VersionSelector != (catalog.VersionSelector{}) || fake.query.UpdatedSince != nil {
			t.Fatal(fake.query)
		}
	}
	for _, version := range []string{"latest", "LATEST", "1.2.3"} {
		fake := &selectedPages{entry: entry(persistence.ServerKind, "com.example/item", "chosen")}
		h := mounted(t, fake, httpapi.ConsumerOptions{})
		for _, path := range []string{"/registry/public/v0.1/servers?version=" + version, "/registry/public/v0.1/servers/com.example%2Fitem/versions/" + version} {
			w := response(t, h, "GET", path)
			if w.Code != 200 {
				t.Fatal(w.Body.String())
			}
			want := catalog.VersionSelector{Exact: version}
			if version == "latest" {
				want = catalog.VersionSelector{Latest: true}
			}
			if fake.operation == httpapi.Get && fake.selector != want || fake.operation == httpapi.List && fake.query.VersionSelector != want {
				t.Fatal(fake)
			}
		}
	}
}

func TestPathValidationAndErrors(t *testing.T) {
	t.Parallel()
	for _, path := range []string{
		"/%20/v0.1/servers", "/public/v0.1/servers/no-slash/versions", "/public/v0.1/servers/com.example%2Fbad%40/versions", "/public/v0.1/servers/com.example%2Fitem/versions/%20", "/public/v0.1/servers/com.example%2Fitem/versions/%00", "/public/v0.1/servers/com.example%2Fitem/versions/%FF", "/public/v0.1/servers?updated_since=bad", "/public/v0.1/servers?version=%00",
		"/public/v0.1/x/dev.toolhive/skills/com%20example/item", "/public/v0.1/x/dev.toolhive/plugins/com.example/%09", "/public/v0.1/x/dev.toolhive/plugins/com.example/item/versions/%20",
	} {
		fake := &selectedPages{}
		h := mounted(t, fake, httpapi.ConsumerOptions{})
		w := response(t, h, "GET", "/registry"+path)
		if w.Code != 400 || fake.calls != 0 {
			t.Fatalf("%s: %d %s calls %d", path, w.Code, w.Body.String(), fake.calls)
		}
	}
	for _, route := range httpapi.ConsumerRoutes() {
		for _, tc := range []struct {
			err     error
			code    int
			message string
		}{
			{fmt.Errorf("host secret: %w", persistence.ErrInvalid), 400, "invalid request"},
			{fmt.Errorf("host secret: %w", persistence.ErrNotFound), 404, "not found"},
			{persistence.ErrUnavailable, 500, "internal server error"}, {errors.New("host secret"), 500, "internal server error"},
			{context.Canceled, 500, "internal server error"}, {context.DeadlineExceeded, 500, "internal server error"},
		} {
			fake := &selectedPages{err: tc.err}
			h := mounted(t, fake, httpapi.ConsumerOptions{})
			path := "/registry" + strings.NewReplacer("{registryName}", "public", "{serverName}", "com.example%2Fitem", "{namespace}", "com.example", "{name}", "item", "{version}", "latest").Replace(route.Template)
			w := response(t, h, "GET", path)
			want, _ := json.Marshal(map[string]string{"error": tc.message})
			if w.Code != tc.code || w.Body.String() != string(want)+"\n" || fake.calls != 1 {
				t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
			}
		}
	}
}

func TestBadHostPayloadDoesNotPanicOrLeak(t *testing.T) {
	t.Parallel()
	for _, page := range []catalog.Page{
		{Entries: []persistence.Entry{{Kind: persistence.ServerKind}}},
		{Entries: []persistence.Entry{entry(persistence.SkillKind, "item", "1")}},
		{Entries: make([]persistence.Entry, 31)},
		{Entries: []persistence.Entry{{Kind: persistence.ServerKind, Server: &model.Server{Meta: &upstreamv0.ServerMeta{PublisherProvided: map[string]any{"bad": make(chan int)}}}}}},
	} {
		fake := &selectedPages{page: page}
		h := mounted(t, fake, httpapi.ConsumerOptions{})
		w := response(t, h, "GET", "/registry/public/v0.1/servers")
		if w.Code != 500 || strings.Contains(w.Body.String(), "bad") || fake.calls != 1 {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
	}
	fake := &selectedPages{}
	h := mounted(t, fake, httpapi.ConsumerOptions{})
	w := response(t, h, "GET", "/registry/public/v0.1/servers")
	assertJSON(t, w, formats.ServerPage(nil, ""))
	if !reflect.DeepEqual(fake.page, catalog.Page{}) {
		t.Fatal("page mutated")
	}
}
