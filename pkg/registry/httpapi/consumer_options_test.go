package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/catalog"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/httpapi"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

// orderedHistory is a host application with its own ordering and cursor protocol.
// Neither the adapter nor persistence defines that protocol.
type orderedHistory struct {
	selectedPages
	entries  []persistence.Entry
	cursors  map[string]int
	requests []catalog.PageRequest
}

func (h *orderedHistory) ListVersions(_ context.Context, view string, name catalog.EntryName, request catalog.PageRequest) (catalog.Page, error) {
	h.requests = append(h.requests, request)
	if view != "public" || name.Name != h.entries[0].Name || name.Kind != h.entries[0].Kind {
		return catalog.Page{}, persistence.ErrInvalid
	}
	offset := 0
	if request.Cursor != "" {
		var ok bool
		offset, ok = h.cursors[request.Cursor]
		if !ok {
			return catalog.Page{}, persistence.ErrInvalid
		}
	}
	end := min(offset+request.Limit, len(h.entries))
	page := catalog.Page{Entries: h.entries[offset:end]}
	if end < len(h.entries) {
		page.NextCursor = fmt.Sprintf(" host boundary %d +/?= \t", end)
		h.cursors[page.NextCursor] = end
	}
	return page, nil
}

func TestPaginatedHistoryExactTraversal(t *testing.T) {
	t.Parallel()
	for _, kind := range []persistence.EntryKind{persistence.ServerKind, persistence.SkillKind, persistence.PluginKind} {
		for _, explicit := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/explicit=%t", kind, explicit), func(t *testing.T) {
				t.Parallel()
				name, path, limit := "com.example/item", "/registry/public/v0.1/servers/com.example%2Fitem/versions", 1000
				if kind != persistence.ServerKind {
					name, limit = "item", 30
					path = "/registry/public/v0.1/x/dev.toolhive/" + strings.ToLower(string(kind)) + "s/com.example/item/versions"
				}
				query := url.Values{}
				if explicit {
					limit = 100
					query.Set("limit", "100")
				}
				app := &orderedHistory{cursors: map[string]int{}}
				// Descending evens followed by ascending odds is deliberately neither
				// lexical nor semantic version order. Every exact identity is unique.
				for i := 2106; i >= 0; i -= 2 {
					app.entries = append(app.entries, entry(kind, name, fmt.Sprintf("raw-%04d", i)))
				}
				for i := 1; i < 2107; i += 2 {
					app.entries = append(app.entries, entry(kind, name, fmt.Sprintf("raw-%04d", i)))
				}
				h := mounted(t, app, httpapi.ConsumerOptions{PaginateHistory: true, StrictQueries: true})
				seen := map[string]bool{}
				offset, cursor := 0, ""
				for {
					w := response(t, h, "GET", path+"?"+query.Encode())
					require.Equal(t, 200, w.Code, w.Body.String())
					var metadata struct {
						Metadata struct {
							Count int    `json:"count"`
							Next  string `json:"nextCursor"`
						} `json:"metadata"`
					}
					require.NoError(t, json.Unmarshal(w.Body.Bytes(), &metadata))
					end := min(offset+limit, len(app.entries))
					require.Equal(t, end-offset, metadata.Metadata.Count)
					expectedCursor := ""
					if end < len(app.entries) {
						expectedCursor = fmt.Sprintf(" host boundary %d +/?= \t", end)
					}
					require.Equal(t, expectedCursor, metadata.Metadata.Next)
					assertJSON(t, w, expectedPage(kind, app.entries[offset:end], expectedCursor))
					require.Equal(t, catalog.PageRequest{Limit: limit, Cursor: cursor}, app.requests[len(app.requests)-1])
					for _, e := range app.entries[offset:end] {
						require.False(t, seen[e.Version], "duplicate exact identity %s", e.Version)
						seen[e.Version] = true
					}
					offset, cursor = end, metadata.Metadata.Next
					if cursor == "" {
						break
					}
					query.Set("cursor", cursor)
				}
				require.Len(t, seen, 2107)
				require.Len(t, app.requests, (2107+limit-1)/limit)
				require.Zero(t, app.calls, "history must not call List or Get")
			})
		}
	}
}

type queryRoute struct {
	path string
	kind persistence.EntryKind
	op   httpapi.Operation
}

func queryRoutes() []queryRoute {
	return []queryRoute{
		{"/servers", persistence.ServerKind, httpapi.List},
		{"/servers/com.example%2Fitem/versions", persistence.ServerKind, httpapi.ListVersions},
		{"/servers/com.example%2Fitem/versions/latest", persistence.ServerKind, httpapi.Get},
		{"/x/dev.toolhive/skills", persistence.SkillKind, httpapi.List},
		{"/x/dev.toolhive/skills/", persistence.SkillKind, httpapi.List},
		{"/x/dev.toolhive/skills/com.example/item", persistence.SkillKind, httpapi.GetLatest},
		{"/x/dev.toolhive/skills/com.example/item/versions", persistence.SkillKind, httpapi.ListVersions},
		{"/x/dev.toolhive/skills/com.example/item/versions/raw", persistence.SkillKind, httpapi.Get},
		{"/x/dev.toolhive/plugins", persistence.PluginKind, httpapi.List},
		{"/x/dev.toolhive/plugins/", persistence.PluginKind, httpapi.List},
		{"/x/dev.toolhive/plugins/com.example/item", persistence.PluginKind, httpapi.GetLatest},
		{"/x/dev.toolhive/plugins/com.example/item/versions", persistence.PluginKind, httpapi.ListVersions},
		{"/x/dev.toolhive/plugins/com.example/item/versions/raw", persistence.PluginKind, httpapi.Get},
	}
}

func TestConsumerQueryInventory(t *testing.T) {
	t.Parallel()
	for _, route := range queryRoutes() {
		t.Run(route.path, func(t *testing.T) {
			t.Parallel()
			for _, tc := range []struct {
				query       string
				legacy      bool
				strict      bool
				strictFlags bool
			}{
				{"", true, true, true},
				{"limit=1", true, route.op == httpapi.List, route.op == httpapi.List || route.op == httpapi.ListVersions},
				{"cursor=host", true, route.op == httpapi.List, route.op == httpapi.List || route.op == httpapi.ListVersions},
				{"search=host", true, route.op == httpapi.List, route.op == httpapi.List},
				{"version=raw", true, route.op == httpapi.List && route.kind == persistence.ServerKind, route.op == httpapi.List && route.kind == persistence.ServerKind},
				{"updated_since=2026-01-02T03%3A04%3A05Z", true, false, route.op == httpapi.List && route.kind == persistence.ServerKind},
				{"name=com.example%2Fitem", true, false, route.op == httpapi.List},
				{"include_deleted=true", true, false, false},
				{"include_deleted=false", true, false, false},
				{"status=active", true, false, false},
				{"unknown=x", true, false, false},
				{"limit=1&limit=2", true, false, false},
				{"limit=1&%6cimit=2", true, false, false},
				{"search=first&search=second", true, false, false},
				{"limit=", true, false, false},
				{"cursor=", true, false, false},
				{"search=", true, false, false},
				{"version=", true, false, false},
				{"updated_since=", true, false, false},
				{"name=", true, false, false},
				{"search=%ZZ", route.op != httpapi.List, false, false},
				{"search=a;b", route.op != httpapi.List, false, false},
				{"search=%FF", route.op != httpapi.List, false, false},
				{"search=%00", route.op != httpapi.List, false, false},
				{"unknown=" + strings.Repeat("a", 16385), route.op != httpapi.List, false, false},
			} {
				for _, mode := range []string{"legacy", "strict", "flags"} {
					t.Run(mode+"/"+tc.query[:min(len(tc.query), 70)], func(t *testing.T) {
						options := httpapi.ConsumerOptions{}
						allowed := tc.legacy
						if mode == "strict" {
							options.StrictQueries, allowed = true, tc.strict
						}
						if mode == "flags" {
							options = httpapi.ConsumerOptions{StrictQueries: true, PaginateHistory: true, AllowExactName: true, AllowUpdatedSince: true}
							allowed = tc.strictFlags
						}
						app := &selectedPages{entry: entry(route.kind, "item", "raw")}
						w := response(t, mounted(t, app, options), "GET", "/registry/public/v0.1"+route.path+"?"+tc.query)
						if !allowed {
							require.Equal(t, 400, w.Code, w.Body.String())
							require.Zero(t, app.calls)
							return
						}
						require.Equal(t, 200, w.Code, w.Body.String())
						require.Equal(t, 1, app.calls)
						if route.op == httpapi.List && tc.query == "name=com.example%2Fitem" {
							want := ""
							if options.AllowExactName {
								want = "com.example/item"
							}
							require.Equal(t, want, app.query.Name)
						}
						if route.op == httpapi.List && strings.HasPrefix(tc.query, "updated_since=") && route.kind == persistence.ServerKind && tc.query != "updated_since=" {
							require.NotNil(t, app.query.UpdatedSince)
							require.Equal(t, "2026-01-02T03:04:05Z", app.query.UpdatedSince.Format("2006-01-02T15:04:05Z07:00"))
						}
					})
				}
			}
		})
	}
}

func TestOptedInQueryControls(t *testing.T) {
	t.Parallel()
	for _, route := range queryRoutes() {
		if route.op != httpapi.List && route.op != httpapi.ListVersions {
			continue
		}
		for _, strict := range []bool{false, true} {
			for _, raw := range []string{"0", "-1", "bad", "999999999999999999999999", "", "%2B1", "1.5", "1&limit=2", "1&%6cimit=2", "1001"} {
				t.Run(fmt.Sprintf("%s/strict=%t/limit=%s", route.path, strict, raw), func(t *testing.T) {
					t.Parallel()
					options := httpapi.ConsumerOptions{StrictQueries: strict, PaginateHistory: true}
					app := &selectedPages{}
					w := response(t, mounted(t, app, options), "GET", "/registry/public/v0.1"+route.path+"?limit="+raw)
					// PaginateHistory does not change permissive list parsing.
					if !strict && route.op == httpapi.List && (raw == "" || raw == "%2B1" || strings.Contains(raw, "&") || raw == "1001" && route.kind == persistence.ServerKind) {
						require.Equal(t, 200, w.Code, w.Body.String())
					} else {
						require.Equal(t, 400, w.Code, w.Body.String())
						require.Zero(t, app.calls)
					}
				})
			}
		}
		for _, raw := range []string{"cursor=%00", "cursor=%FF", "cursor=" + strings.Repeat("a", 4097), "limit=" + strings.Repeat("1", 16385)} {
			app := &selectedPages{}
			w := response(t, mounted(t, app, httpapi.ConsumerOptions{PaginateHistory: true, StrictQueries: true}), "GET", "/registry/public/v0.1"+route.path+"?"+raw)
			require.Equal(t, 400, w.Code, route.path)
			require.Zero(t, app.calls)
		}
		maximum := "100"
		if route.kind == persistence.ServerKind {
			maximum = "1000"
		}
		for _, limit := range []string{"1", maximum, "101"} {
			app := &selectedPages{}
			w := response(t, mounted(t, app, httpapi.ConsumerOptions{PaginateHistory: true, StrictQueries: true}), "GET", "/registry/public/v0.1"+route.path+"?limit="+limit+"&cursor="+strings.Repeat("a", 4096))
			if limit == "101" && route.kind != persistence.ServerKind {
				require.Equal(t, 400, w.Code)
				require.Zero(t, app.calls)
			} else {
				require.Equal(t, 200, w.Code, w.Body.String())
				require.Equal(t, 1, app.calls)
			}
		}
	}
}

func TestConsumerSelectorsAndOpaqueValues(t *testing.T) {
	t.Parallel()
	for _, route := range queryRoutes() {
		if route.op != httpapi.List {
			continue
		}
		for _, strict := range []bool{false, true} {
			for _, rawName := range []string{"", "%00", "%FF", "white%20space", "%E2%80%83item", strings.Repeat("a", 4097), "item&%6eame=other"} {
				app := &selectedPages{}
				w := response(t, mounted(t, app, httpapi.ConsumerOptions{AllowExactName: true, StrictQueries: strict}), "GET", "/registry/public/v0.1"+route.path+"?name="+rawName)
				require.Equal(t, 400, w.Code, route.path+"?name="+rawName)
				require.Zero(t, app.calls)
			}
			name := "item"
			if route.kind == persistence.ServerKind {
				name = "com.example/item"
			}
			values := url.Values{"name": {name}, "search": {" host search +/?= "}, "cursor": {" host cursor +/?= "}}
			if route.kind == persistence.ServerKind {
				values.Set("version", "raw/+?=1")
			}
			app := &selectedPages{}
			w := response(t, mounted(t, app, httpapi.ConsumerOptions{AllowExactName: true, StrictQueries: strict}), "GET", "/registry/public/v0.1"+route.path+"?"+values.Encode())
			require.Equal(t, 200, w.Code, w.Body.String())
			require.Equal(t, name, app.query.Name)
			search, cursor := values.Get("search"), values.Get("cursor")
			if !strict && route.kind != persistence.ServerKind {
				search, cursor = strings.TrimSpace(search), strings.TrimSpace(cursor)
			}
			require.Equal(t, search, app.query.Search)
			require.Equal(t, cursor, app.query.Cursor)
			if route.kind == persistence.ServerKind {
				require.Equal(t, catalog.VersionSelector{Exact: "raw/+?=1"}, app.query.VersionSelector)
			}
		}
	}
	for _, raw := range []string{"item", "com.example%2F", "com.example%2Fitem%2Fextra", "%E2%80%83com.example%2Fitem", "com.example%2Fitem%E2%80%83"} {
		app := &selectedPages{}
		w := response(t, mounted(t, app, httpapi.ConsumerOptions{AllowExactName: true}), "GET", "/registry/public/v0.1/servers?name="+raw)
		require.Equal(t, 400, w.Code, raw)
		require.Zero(t, app.calls)
	}
	for _, raw := range []string{"version=%20", "updated_since=bad"} {
		app := &selectedPages{}
		w := response(t, mounted(t, app, httpapi.ConsumerOptions{StrictQueries: true, AllowUpdatedSince: true}), "GET", "/registry/public/v0.1/servers?"+raw)
		require.Equal(t, 400, w.Code)
		require.Zero(t, app.calls)
	}
}

func TestConsumerOptionsRemainIndependent(t *testing.T) {
	t.Parallel()
	for _, options := range []httpapi.ConsumerOptions{
		{PaginateHistory: true}, {AllowExactName: true}, {AllowUpdatedSince: true},
		{PaginateHistory: true, AllowExactName: true, AllowUpdatedSince: true},
	} {
		for _, route := range queryRoutes() {
			app := &selectedPages{entry: entry(route.kind, "item", "raw"), page: catalog.Page{NextCursor: " host next +/?= "}}
			values := url.Values{"unknown": {"ignored"}, "include_deleted": {"false"}, "search": {" kept search "}}
			if route.op == httpapi.List {
				values.Set("limit", "1")
				values.Set("cursor", " kept cursor ")
				values.Set("updated_since", "2026-01-02T03:04:05Z")
			} else {
				values.Set("limit", "2")
				values.Set("cursor", " opaque history +/?= ")
			}
			w := response(t, mounted(t, app, options), "GET", "/registry/public/v0.1"+route.path+"?"+values.Encode())
			require.Equal(t, 200, w.Code, w.Body.String())
			require.Equal(t, 1, app.calls)
			if route.op == httpapi.List {
				search, cursor := " kept search ", " kept cursor "
				if route.kind == persistence.ServerKind {
					require.NotNil(t, app.query.UpdatedSince)
				} else {
					search, cursor = strings.TrimSpace(search), strings.TrimSpace(cursor)
					require.Nil(t, app.query.UpdatedSince)
				}
				require.Equal(t, search, app.query.Search)
				require.Equal(t, cursor, app.query.Cursor)
			}
			if route.op == httpapi.ListVersions {
				limit, cursor, next := 30, "", app.page.NextCursor
				if route.kind == persistence.ServerKind {
					limit, next = 1000, ""
				}
				if options.PaginateHistory {
					limit, cursor, next = 2, values.Get("cursor"), app.page.NextCursor
				}
				require.Equal(t, catalog.PageRequest{Limit: limit, Cursor: cursor}, app.request)
				assertJSON(t, w, expectedPage(route.kind, nil, next))
			}
		}
	}
}

func TestConsumerApplicationErrorMapping(t *testing.T) {
	t.Parallel()
	for _, route := range queryRoutes() {
		for _, tc := range []struct {
			name    string
			err     error
			mapping *httpapi.ErrorMapping
			status  int
			body    string
		}{
			{"invalid", fmt.Errorf("secret: %w", persistence.ErrInvalid), nil, 400, `{"error":"invalid request"}`},
			{"not-found", persistence.ErrNotFound, nil, 404, `{"error":"not found"}`},
			{"dependency", errors.New("secret dependency URL"), nil, 500, `{"error":"internal server error"}`},
			{"unavailable", persistence.ErrUnavailable, nil, 500, `{"error":"internal server error"}`},
			{"cancel", context.Canceled, nil, 500, `{"error":"internal server error"}`},
			{"deadline", context.DeadlineExceeded, nil, 500, `{"error":"internal server error"}`},
			{"custom", persistence.ErrUnavailable, &httpapi.ErrorMapping{Status: 503, Body: map[string]string{"code": "dependency_unavailable"}}, 503, `{"code":"dependency_unavailable"}`},
			{"default", persistence.ErrNotFound, &httpapi.ErrorMapping{}, 404, `{"error":"not found"}`},
			{"success-status", persistence.ErrUnavailable, &httpapi.ErrorMapping{Status: 200, Body: map[string]string{"success": "bad"}}, 500, `{"error":"internal server error"}`},
			{"redirect-status", persistence.ErrUnavailable, &httpapi.ErrorMapping{Status: 399, Body: "bad"}, 500, `{"error":"internal server error"}`},
			{"high-status", persistence.ErrUnavailable, &httpapi.ErrorMapping{Status: 600, Body: "bad"}, 500, `{"error":"internal server error"}`},
			{"zero-status", persistence.ErrUnavailable, &httpapi.ErrorMapping{Body: "bad"}, 500, `{"error":"internal server error"}`},
			{"nil-body", persistence.ErrUnavailable, &httpapi.ErrorMapping{Status: 503}, 500, `{"error":"internal server error"}`},
			{"typed-nil-body", persistence.ErrUnavailable, &httpapi.ErrorMapping{Status: 503, Body: (*string)(nil)}, 500, `{"error":"internal server error"}`},
			{"invalid-body", persistence.ErrUnavailable, &httpapi.ErrorMapping{Status: 503, Body: make(chan int)}, 500, `{"error":"internal server error"}`},
			{"invalid-json", persistence.ErrUnavailable, &httpapi.ErrorMapping{Status: 503, Body: json.RawMessage(`{"unfinished":`)}, 500, `{"error":"internal server error"}`},
		} {
			t.Run(route.path+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				app := &selectedPages{err: tc.err}
				calls := 0
				options := httpapi.ConsumerOptions{StrictQueries: true}
				path := "/registry/public/v0.1" + route.path
				if tc.mapping != nil {
					options.MapError = func(r *http.Request, d httpapi.RouteDescriptor, err error) httpapi.ErrorMapping {
						calls++
						require.Equal(t, path, r.URL.EscapedPath())
						require.Equal(t, route.kind, d.Kind)
						require.Equal(t, route.op, d.Operation)
						require.Equal(t, tc.err, err)
						return *tc.mapping
					}
				}
				w := &countingResponse{ResponseRecorder: httptest.NewRecorder()}
				mounted(t, app, options).ServeHTTP(w, httptest.NewRequest("GET", path, nil))
				require.Equal(t, tc.status, w.Code)
				require.Equal(t, tc.body+"\n", w.Body.String())
				require.Equal(t, "application/json", w.Header().Get("Content-Type"))
				require.Equal(t, 1, w.headers)
				require.Equal(t, 1, w.writes)
				require.Equal(t, 1, app.calls)
				if tc.mapping == nil {
					require.Zero(t, calls)
				} else {
					require.Equal(t, 1, calls)
				}
			})
		}
	}
}

type countingResponse struct {
	*httptest.ResponseRecorder
	headers int
	writes  int
}

func (w *countingResponse) WriteHeader(status int) {
	w.headers++
	w.ResponseRecorder.WriteHeader(status)
}

func (w *countingResponse) Write(body []byte) (int, error) {
	w.writes++
	return w.ResponseRecorder.Write(body)
}

func TestConsumerMapperCannotInterceptBoundaryFailures(t *testing.T) {
	t.Parallel()
	unencodable := entry(persistence.ServerKind, "com.example/item", "1")
	unencodable.Server.Meta.PublisherProvided["invalid"] = make(chan int)
	for _, tc := range []struct {
		name    string
		path    string
		app     *selectedPages
		options httpapi.ConsumerOptions
		status  int
		calls   int
	}{
		{"validation", "/servers?unknown=x", &selectedPages{}, httpapi.ConsumerOptions{StrictQueries: true}, 400, 0},
		{"query-contract", "/servers?search=%00", &selectedPages{}, httpapi.ConsumerOptions{}, 400, 0},
		{"path", "/servers/invalid/versions/latest", &selectedPages{}, httpapi.ConsumerOptions{}, 400, 0},
		{"oversized-page", "/servers?limit=1", &selectedPages{page: catalog.Page{Entries: []persistence.Entry{entry(persistence.ServerKind, "a", "1"), entry(persistence.ServerKind, "b", "1")}}}, httpapi.ConsumerOptions{}, 500, 1},
		{"wrong-payload", "/servers", &selectedPages{page: catalog.Page{Entries: []persistence.Entry{entry(persistence.SkillKind, "a", "1")}}}, httpapi.ConsumerOptions{}, 500, 1},
		{"wrong-detail", "/servers/com.example%2Fitem/versions/latest", &selectedPages{entry: entry(persistence.SkillKind, "a", "1")}, httpapi.ConsumerOptions{}, 500, 1},
		{"unencodable-success", "/servers/com.example%2Fitem/versions/latest", &selectedPages{entry: unencodable}, httpapi.ConsumerOptions{}, 500, 1},
		{"success", "/servers/com.example%2Fitem/versions/latest", &selectedPages{entry: entry(persistence.ServerKind, "com.example/item", "1")}, httpapi.ConsumerOptions{}, 200, 1},
		{"authorize", "/servers", &selectedPages{err: persistence.ErrUnavailable}, httpapi.ConsumerOptions{Decorate: httpapi.Authorize(nil)}, 403, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.options.MapError = func(*http.Request, httpapi.RouteDescriptor, error) httpapi.ErrorMapping {
				t.Fatal("boundary failure reached mapper")
				return httpapi.ErrorMapping{Status: 503, Body: "bad"}
			}
			w := &countingResponse{ResponseRecorder: httptest.NewRecorder()}
			mounted(t, tc.app, tc.options).ServeHTTP(w, httptest.NewRequest("GET", "/registry/public/v0.1"+tc.path, nil))
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Equal(t, tc.calls, tc.app.calls)
			require.Equal(t, 1, w.headers)
			require.Equal(t, 1, w.writes)
		})
	}
}

func TestConsumerServerOnlyManifest(t *testing.T) {
	t.Parallel()
	want := []httpapi.RouteDescriptor{
		{Method: "GET", Template: "/{registryName}/v0.1/servers", Kind: persistence.ServerKind, Operation: httpapi.List, AuditEvent: "server.list", ResourceType: "server", ResourceParam: "serverName", RegistryParam: "registryName"},
		{Method: "GET", Template: "/{registryName}/v0.1/servers/{serverName}/versions", Kind: persistence.ServerKind, Operation: httpapi.ListVersions, AuditEvent: "server.versions.list", ResourceType: "server", ResourceParam: "serverName", RegistryParam: "registryName"},
		{Method: "GET", Template: "/{registryName}/v0.1/servers/{serverName}/versions/{version}", Kind: persistence.ServerKind, Operation: httpapi.Get, AuditEvent: "server.version.read", ResourceType: "server", ResourceParam: "serverName", RegistryParam: "registryName"},
	}
	require.Equal(t, want, httpapi.ConsumerServerRoutes())
	inventory := httpapi.ConsumerServerRoutes()
	inventory[0].Template = "changed"
	require.Equal(t, want, httpapi.ConsumerServerRoutes())
	app := &selectedPages{entry: entry(persistence.ServerKind, "com.example/item", "raw")}
	var decorated []httpapi.RouteDescriptor
	h, err := httpapi.ConsumerServerRouter(app, httpapi.ConsumerOptions{Decorate: func(d httpapi.RouteDescriptor, h http.Handler) http.Handler {
		decorated = append(decorated, d)
		return h
	}})
	require.NoError(t, err)
	require.Equal(t, want, decorated)
	actual := map[string]bool{}
	require.NoError(t, chi.Walk(h.(chi.Routes), func(method, path string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		actual[method+" "+path] = true
		return nil
	}))
	expected := map[string]bool{}
	for _, d := range want {
		expected[d.Method+" "+d.Template] = true
	}
	require.True(t, reflect.DeepEqual(expected, actual), "%v", actual)
	require.Len(t, httpapi.ConsumerRoutes(), 13)
	require.Zero(t, app.calls)
	for _, route := range queryRoutes() {
		w := response(t, h, "GET", "/public/v0.1"+route.path)
		if route.kind == persistence.ServerKind {
			require.Equal(t, 200, w.Code, w.Body.String())
		} else {
			require.Equal(t, 404, w.Code)
		}
	}
	require.Equal(t, 3, app.calls)
	var typedNil *selectedPages
	for _, pages := range []httpapi.ConsumerPages{nil, typedNil} {
		_, err := httpapi.ConsumerServerRouter(pages, httpapi.ConsumerOptions{})
		require.ErrorIs(t, err, persistence.ErrInvalid)
	}
}
