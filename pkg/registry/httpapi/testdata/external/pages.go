package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/go-chi/chi/v5"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/catalog"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/httpapi"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

// These private contexts are SYNTHETIC fixture identities, not authentication.
// Production hosts must establish their own verified evidence before admission.
type fixtureIdentityKey struct{}

func fixtureContext(ctx context.Context, identity string) context.Context {
	return context.WithValue(ctx, fixtureIdentityKey{}, identity)
}

var fixtureSequence atomic.Uint64
var errFixtureDenied = errors.New("fixture admission denied")
var errDependency = errors.New("private dependency credential diagnostic")

type continuation struct {
	query catalog.Query
	next  int
}

// replacement is a complete ConsumerPages application, not a storage adapter.
// Records have a fixed host order. Cursors record a position, never permission;
// current admission and selection are checked on every operation/page.
type replacement struct {
	mu       sync.Mutex
	id       uint64
	records  []persistence.Entry
	allowed  map[string]map[string]bool
	admitted map[string]bool
	cursors  map[string]continuation
	failure  error
	calls    atomic.Int64
}

var _ httpapi.ConsumerPages = (*replacement)(nil)

func (a *replacement) List(ctx context.Context, view string, q catalog.Query) (catalog.Page, error) {
	a.calls.Add(1)
	a.mu.Lock()
	defer a.mu.Unlock()
	identity, err := a.check(ctx, view)
	if err != nil {
		return catalog.Page{}, err
	}
	if err := q.Validate(view); err != nil {
		return catalog.Page{}, err
	}
	return a.page(identity, q)
}

func (a *replacement) Get(ctx context.Context, view string, name catalog.EntryName, selector catalog.VersionSelector) (persistence.Entry, error) {
	a.calls.Add(1)
	a.mu.Lock()
	defer a.mu.Unlock()
	identity, err := a.check(ctx, view)
	if err != nil {
		return persistence.Entry{}, err
	}
	if err := name.Validate(view, selector); err != nil {
		return persistence.Entry{}, err
	}
	if !a.allowed[identity][name.Name] {
		return persistence.Entry{}, persistence.ErrNotFound
	}
	for _, e := range a.records {
		if e.Kind == name.Kind && e.Name == name.Name && (selector.Latest && e.IsLatest || !selector.Latest && e.Version == selector.Exact) {
			return e, nil
		}
	}
	return persistence.Entry{}, persistence.ErrNotFound
}

func (a *replacement) ListVersions(ctx context.Context, view string, name catalog.EntryName, request catalog.PageRequest) (catalog.Page, error) {
	a.calls.Add(1)
	a.mu.Lock()
	defer a.mu.Unlock()
	identity, err := a.check(ctx, view)
	if err != nil {
		return catalog.Page{}, err
	}
	q := catalog.Query{Kind: name.Kind, Name: name.Name, Namespace: name.Namespace, Limit: request.Limit, Cursor: request.Cursor}
	if err := q.Validate(view); err != nil {
		return catalog.Page{}, err
	}
	if !a.allowed[identity][name.Name] {
		return catalog.Page{}, persistence.ErrNotFound
	}
	return a.page(identity, q)
}

func (a *replacement) Authorize(ctx context.Context, _ httpapi.RouteDescriptor) (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	identity, _ := ctx.Value(fixtureIdentityKey{}).(string)
	return ctx.Err() == nil && a.admitted[identity], ctx.Err()
}

func (a *replacement) check(ctx context.Context, view string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	identity, _ := ctx.Value(fixtureIdentityKey{}).(string)
	if !a.admitted[identity] {
		return "", errFixtureDenied
	}
	if view != "public" {
		return "", persistence.ErrNotFound
	}
	if a.failure != nil {
		return "", a.failure
	}
	return identity, nil
}

func (a *replacement) page(identity string, q catalog.Query) (catalog.Page, error) {
	cursor := q.Cursor
	q.Cursor = ""
	start := 0
	if cursor != "" {
		c, ok := a.cursors[cursor]
		if !ok || !reflect.DeepEqual(c.query, q) {
			return catalog.Page{}, persistence.ErrInvalid
		}
		start = c.next
	}
	var page catalog.Page
	for i := start; i < len(a.records); i++ {
		e := a.records[i]
		if !a.allowed[identity][e.Name] || e.Kind != q.Kind || q.Name != "" && e.Name != q.Name ||
			q.VersionSelector.Exact != "" && e.Version != q.VersionSelector.Exact || q.VersionSelector.Latest && !e.IsLatest ||
			!strings.Contains(strings.ToLower(e.Name+" "+e.Server.Description), strings.ToLower(q.Search)) {
			continue
		}
		if len(page.Entries) == q.Limit {
			page.NextCursor = fmt.Sprintf(" fixture %d continuation %d boundary %d +/?= ", a.id, len(a.cursors)+1, i)
			a.cursors[page.NextCursor] = continuation{query: q, next: i}
			break
		}
		page.Entries = append(page.Entries, e)
	}
	return page, nil
}

func (a *replacement) hide(identity, name string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.allowed[identity], name)
}

func (a *replacement) revoke(identity string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.admitted[identity] = false
}

func newReplacement(versions int) *replacement {
	a := &replacement{id: fixtureSequence.Add(1), cursors: map[string]continuation{},
		admitted: map[string]bool{"reader": true, "other": true},
		allowed:  map[string]map[string]bool{"reader": {"com.example/z": true, "com.example/a": true}, "other": {"com.example/private": true}}}
	appendRecord := func(name, version, description string, latest bool) {
		// Decode fixture metadata with UseNumber, just as a host JSON boundary must
		// do to retain integers outside float64's exact range.
		data := fmt.Sprintf(`{"name":%q,"version":%q,"description":%q,"_meta":{"io.modelcontextprotocol.registry/publisher-provided":{"serial":9007199254740993}}}`, name, version, description)
		var server model.Server
		decoder := json.NewDecoder(strings.NewReader(data))
		decoder.UseNumber()
		if err := decoder.Decode(&server); err != nil {
			panic(err)
		}
		a.records = append(a.records, persistence.Entry{Kind: persistence.ServerKind, Name: name, Version: version, IsLatest: latest, Server: &server})
	}
	// Deliberately descending, non-semver host order; latest is raw-0000.
	for i := versions - 1; i >= 0; i-- {
		appendRecord("com.example/z", fmt.Sprintf("raw-%04d", i), "needle z", i == 0)
	}
	appendRecord("com.example/a", "1", "needle a", true)
	appendRecord("com.example/private", "1", "private", true)
	return a
}

// mountReplacement deliberately attaches a fixed synthetic context, never an
// identity header. It is test wiring, NOT a production authentication example.
func mountReplacement(a httpapi.ConsumerPages, authorizer httpapi.Authorizer, identity string, mapper func(*http.Request, httpapi.RouteDescriptor, error) httpapi.ErrorMapping) (http.Handler, error) {
	h, err := httpapi.ConsumerServerRouter(a, httpapi.ConsumerOptions{
		StrictQueries: true, PaginateHistory: true, AllowExactName: true,
		MapError: mapper, Decorate: httpapi.Authorize(authorizer),
	})
	if err != nil {
		return nil, err
	}
	mux := chi.NewRouter()
	mux.Mount("/registry", h)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-store")
		mux.ServeHTTP(w, r.WithContext(fixtureContext(r.Context(), identity)))
	}), nil
}
