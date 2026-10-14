package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/catalog"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/httpapi"
	pagesconformance "github.com/stacklok/toolhive-registry-server/pkg/registry/httpapi/conformance/pages"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

const serversPath = "/registry/public/v0.1/servers"
const historyPath = serversPath + "/com.example%2Fz/versions"

// Independent wire expectations: no production DTO or serializer is an oracle.
type wireServer struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Meta        struct {
		Publisher struct {
			Serial json.Number `json:"serial"`
		} `json:"io.modelcontextprotocol.registry/publisher-provided"`
	} `json:"_meta"`
}

type wireEnvelope struct {
	Server wireServer `json:"server"`
}
type wirePage struct {
	Servers  []wireEnvelope `json:"servers"`
	Metadata struct {
		Count  int    `json:"count"`
		Cursor string `json:"nextCursor"`
	} `json:"metadata"`
}

func fixtureHandler(t *testing.T, a *replacement, identity string, mapper func(*http.Request, httpapi.RouteDescriptor, error) httpapi.ErrorMapping) http.Handler {
	t.Helper()
	h, err := mountReplacement(a, a, identity, mapper)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func fixtureResponse(t *testing.T, h http.Handler, path string, status int) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	if w.Code != status || w.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("%s: %d / %v / %s", path, w.Code, w.Header(), w.Body.String())
	}
	return w
}

func decodeFixturePage(t *testing.T, w *httptest.ResponseRecorder) wirePage {
	t.Helper()
	var page wirePage
	decoder := json.NewDecoder(w.Body)
	decoder.UseNumber()
	if err := decoder.Decode(&page); err != nil {
		t.Fatal(err)
	}
	if len(page.Servers) != page.Metadata.Count {
		t.Fatal("incorrect page count")
	}
	return page
}

func assertWireServer(t *testing.T, server wireServer, name, version, description string) {
	t.Helper()
	if server.Name != name || server.Version != version || server.Description != description || server.Meta.Publisher.Serial.String() != "9007199254740993" {
		t.Fatalf("payload fidelity: %+v", server)
	}
}

func TestDirectConsumerPagesConformance(t *testing.T) {
	pagesconformance.Run(t, func(t *testing.T) pagesconformance.Fixture {
		a := newReplacement(3)
		return pagesconformance.Fixture{
			Pages: a, Context: fixtureContext(t.Context(), "reader"), View: "public", HiddenName: "com.example/private", MissingName: "com.example/missing", Search: "needle z",
			Records: []pagesconformance.Record{
				{Name: "com.example/z", Version: "raw-0002", Description: "needle z"},
				{Name: "com.example/z", Version: "raw-0001", Description: "needle z"},
				{Name: "com.example/z", Version: "raw-0000", Description: "needle z", Latest: true},
				{Name: "com.example/a", Version: "1", Description: "needle a", Latest: true},
			},
			Hide: func(name string) { a.hide("reader", name) }, Revoke: func() { a.revoke("reader") },
		}
	})
}

func TestReplacementHTTPTraversalAndDetails(t *testing.T) {
	a := newReplacement(1003)
	h := fixtureHandler(t, a, "reader", nil)
	if a.calls.Load() != 0 {
		t.Fatal("constructor performed application work")
	}
	// Exact three-route manifest, independently written rather than derived.
	var templates []string
	for _, route := range httpapi.ConsumerServerRoutes() {
		if route.Method != "GET" {
			t.Fatal(route.Method)
		}
		templates = append(templates, route.Template)
	}
	if !reflect.DeepEqual(templates, []string{"/{registryName}/v0.1/servers", "/{registryName}/v0.1/servers/{serverName}/versions", "/{registryName}/v0.1/servers/{serverName}/versions/{version}"}) {
		t.Fatal(templates)
	}
	for _, tc := range []struct {
		path  string
		total int
	}{
		{serversPath + "?search=needle&limit=1000", 1004},
		{historyPath + "?limit=1000", 1003},
	} {
		seen, cursor := 0, ""
		for {
			path := tc.path
			if cursor != "" {
				path += "&cursor=" + url.QueryEscape(cursor)
			}
			page := decodeFixturePage(t, fixtureResponse(t, h, path, 200))
			if page.Metadata.Count != min(1000, tc.total-seen) {
				t.Fatal("page was truncated/refilled")
			}
			for _, envelope := range page.Servers {
				name, version, description := "com.example/z", fmt.Sprintf("raw-%04d", 1002-seen), "needle z"
				if seen == 1003 {
					name, version, description = "com.example/a", "1", "needle a"
				}
				assertWireServer(t, envelope.Server, name, version, description)
				seen++
			}
			cursor = page.Metadata.Cursor
			if cursor == "" {
				break
			}
			a.mu.Lock()
			_, issued := a.cursors[cursor]
			a.mu.Unlock()
			if !issued || !strings.HasPrefix(cursor, " fixture ") || !strings.HasSuffix(cursor, " +/?= ") {
				t.Fatal("HTTP changed host cursor", cursor)
			}
			if seen >= tc.total {
				t.Fatal("nonterminating history")
			}
		}
		if seen != tc.total {
			t.Fatalf("traversed %d, want %d", seen, tc.total)
		}
	}
	for _, version := range []string{"raw-0002", "latest"} {
		w := fixtureResponse(t, h, historyPath+"/"+version, 200)
		var envelope wireEnvelope
		if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		expected := version
		if version == "latest" {
			expected = "raw-0000"
		}
		assertWireServer(t, envelope.Server, "com.example/z", expected, "needle z")
	}
	page := decodeFixturePage(t, fixtureResponse(t, h, serversPath+"?name=com.example%2Fa&version=latest", 200))
	if len(page.Servers) != 1 {
		t.Fatal(page)
	}
	assertWireServer(t, page.Servers[0].Server, "com.example/a", "1", "needle a")
	searched := decodeFixturePage(t, fixtureResponse(t, h, serversPath+"?search=needle%20a", 200))
	if len(searched.Servers) != 1 {
		t.Fatal("search ignored", searched)
	}
	assertWireServer(t, searched.Servers[0].Server, "com.example/a", "1", "needle a")
	fixtureResponse(t, h, serversPath+"?unknown=x", 400)
	fixtureResponse(t, h, "/registry/public/v0.1/x/dev.toolhive/skills", 404)
	fixtureResponse(t, h, "/v1/sources", 404)
}

func TestReplacementCurrentVisibilityAndAdmission(t *testing.T) {
	a := newReplacement(3)
	h := fixtureHandler(t, a, "reader", nil)
	first := decodeFixturePage(t, fixtureResponse(t, h, serversPath+"?limit=1&search=needle", 200))
	history := decodeFixturePage(t, fixtureResponse(t, h, historyPath+"?limit=1", 200))
	listNext := serversPath + "?limit=1&search=needle&cursor=" + url.QueryEscape(first.Metadata.Cursor)
	historyNext := historyPath + "?limit=1&cursor=" + url.QueryEscape(history.Metadata.Cursor)
	for _, suffix := range []string{"/versions/latest", "/versions/1", "/versions"} {
		hidden := fixtureResponse(t, h, serversPath+"/com.example%2Fprivate"+suffix, 404)
		missing := fixtureResponse(t, h, serversPath+"/com.example%2Fmissing"+suffix, 404)
		if hidden.Body.String() != missing.Body.String() {
			t.Fatal("inaccessible/missing are distinguishable")
		}
	}
	if page := decodeFixturePage(t, fixtureResponse(t, h, serversPath+"?name=com.example%2Fprivate", 200)); len(page.Servers) != 0 {
		t.Fatal("hidden list record")
	}
	// Cursor binding is query/fixture integrity, not authorization evidence.
	fixtureResponse(t, h, serversPath+"?limit=1&search=changed&cursor="+url.QueryEscape(first.Metadata.Cursor), 400)
	fixtureResponse(t, fixtureHandler(t, newReplacement(3), "reader", nil), listNext, 400)
	other := fixtureHandler(t, a, "other", nil)
	otherPage := decodeFixturePage(t, fixtureResponse(t, other, listNext, 200))
	if len(otherPage.Servers) != 0 {
		t.Fatal("cursor conferred reader visibility on other context")
	}
	fixtureResponse(t, other, historyNext, 404)
	fixtureResponse(t, other, historyPath+"/latest", 404)
	for _, path := range []string{serversPath, serversPath + "/com.example%2Fprivate/versions"} {
		page := decodeFixturePage(t, fixtureResponse(t, other, path, 200))
		if len(page.Servers) != 1 {
			t.Fatal("other context selection", page)
		}
		assertWireServer(t, page.Servers[0].Server, "com.example/private", "1", "private")
	}
	for _, version := range []string{"1", "latest"} {
		w := fixtureResponse(t, other, serversPath+"/com.example%2Fprivate/versions/"+version, 200)
		var detail wireEnvelope
		if err := json.Unmarshal(w.Body.Bytes(), &detail); err != nil {
			t.Fatal(err)
		}
		assertWireServer(t, detail.Server, "com.example/private", "1", "private")
	}
	// Change selection after issuing both continuations. The list continues in
	// host order with a, while exact/latest/history of z all become generic 404.
	a.hide("reader", "com.example/z")
	page := decodeFixturePage(t, fixtureResponse(t, h, listNext, 200))
	if len(page.Servers) != 1 {
		t.Fatal(page)
	}
	assertWireServer(t, page.Servers[0].Server, "com.example/a", "1", "needle a")
	for _, path := range []string{historyNext, historyPath + "/latest", historyPath + "/raw-0002"} {
		fixtureResponse(t, h, path, 404)
	}
	before := a.calls.Load()
	a.revoke("reader")
	for _, path := range []string{listNext, historyNext, historyPath + "/latest"} {
		fixtureResponse(t, h, path, 403)
	}
	if a.calls.Load() != before {
		t.Fatal("revoked admission reached application")
	}
}

func TestReplacementApplicationErrors(t *testing.T) {
	for _, custom := range []bool{false, true} {
		a := newReplacement(3)
		a.failure = errDependency
		var mapper func(*http.Request, httpapi.RouteDescriptor, error) httpapi.ErrorMapping
		mapped := 0
		if custom {
			mapper = func(_ *http.Request, _ httpapi.RouteDescriptor, err error) httpapi.ErrorMapping {
				mapped++
				if errors.Is(err, errDependency) {
					return httpapi.ErrorMapping{Status: 503, Body: map[string]string{"error": "dependency unavailable"}}
				}
				return httpapi.ErrorMapping{}
			}
		}
		h := fixtureHandler(t, a, "reader", mapper)
		status := 500
		if custom {
			status = 503
		}
		for _, path := range []string{serversPath, historyPath, historyPath + "/latest"} {
			w := fixtureResponse(t, h, path, status)
			if strings.Contains(w.Body.String(), "private") || strings.Contains(w.Body.String(), "credential") {
				t.Fatal("diagnostic leak", w.Body.String())
			}
			expected := "{\"error\":\"internal server error\"}\n"
			if custom {
				expected = "{\"error\":\"dependency unavailable\"}\n"
			}
			if w.Body.String() != expected {
				t.Fatal(w.Body.String())
			}
		}
		fixtureResponse(t, h, serversPath+"?limit=bad", 400)
		a.revoke("reader")
		fixtureResponse(t, h, serversPath, 403)
		if custom && mapped != 3 {
			t.Fatal("mapper intercepted validation/admission", mapped)
		}
	}
}

// blockingPages is lifecycle test support, not a production server abstraction.
type blockingPages struct {
	started  chan httpapi.Operation
	finished chan httpapi.Operation
}

func (b *blockingPages) wait(ctx context.Context, op httpapi.Operation) error {
	b.started <- op
	<-ctx.Done()
	b.finished <- op
	return ctx.Err()
}
func (b *blockingPages) List(ctx context.Context, _ string, _ catalog.Query) (catalog.Page, error) {
	return catalog.Page{}, b.wait(ctx, httpapi.List)
}
func (b *blockingPages) Get(ctx context.Context, _ string, _ catalog.EntryName, _ catalog.VersionSelector) (persistence.Entry, error) {
	return persistence.Entry{}, b.wait(ctx, httpapi.Get)
}
func (b *blockingPages) ListVersions(ctx context.Context, _ string, _ catalog.EntryName, _ catalog.PageRequest) (catalog.Page, error) {
	return catalog.Page{}, b.wait(ctx, httpapi.ListVersions)
}

func TestReplacementHostListenerCancelAndJoin(t *testing.T) {
	root, cancel := context.WithCancel(t.Context())
	defer cancel()
	b := &blockingPages{started: make(chan httpapi.Operation, 3), finished: make(chan httpapi.Operation, 3)}
	a := newReplacement(3)
	h, err := mountReplacement(b, a, "reader", nil)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-b.started:
		t.Fatal("construction started work")
	default:
	}
	var handlers sync.WaitGroup
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlers.Add(1)
		defer handlers.Done()
		h.ServeHTTP(w, r)
	}))
	server.Config.BaseContext = func(net.Listener) context.Context { return root }
	server.Start()
	defer func() {
		cancel()
		server.Close()
	}()
	server.Client().Timeout = 5 * time.Second
	results := make(chan error, 3)
	for _, path := range []string{serversPath, historyPath, historyPath + "/latest"} {
		go func() {
			res, err := server.Client().Get(server.URL + path)
			if err == nil {
				_, err = io.Copy(io.Discard, res.Body)
				res.Body.Close()
				if res.Header.Get("Cache-Control") != "private, no-store" {
					err = errors.New("missing cancellation cache header")
				}
			}
			results <- err
		}()
	}
	seen := map[httpapi.Operation]bool{}
	for range 3 {
		select {
		case op := <-b.started:
			seen[op] = true
		case <-time.After(5 * time.Second):
			t.Fatal("request did not start")
		}
	}
	if len(seen) != 3 {
		t.Fatal(seen)
	}
	cancel()
	server.Close() // Host stops its listener and joins handlers before dependencies.
	handlers.Wait()
	if len(b.finished) != 3 {
		t.Fatal("operation not joined")
	}
	for range 3 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if _, err := server.Client().Get(server.URL + serversPath); err == nil {
		t.Fatal("listener still accepting requests")
	}
}

func Example() {
	a := newReplacement(3)
	h, err := mountReplacement(a, a, "reader", nil)
	if err != nil {
		panic(err)
	}
	server := httptest.NewServer(h)
	defer server.Close()
	res, err := server.Client().Get(server.URL + historyPath + "/latest")
	if err != nil {
		panic(err)
	}
	defer res.Body.Close()
	var detail wireEnvelope
	if err := json.NewDecoder(res.Body).Decode(&detail); err != nil {
		panic(err)
	}
	fmt.Println(res.StatusCode, detail.Server.Name, detail.Server.Version)
	// Output: 200 com.example/z raw-0000
}
