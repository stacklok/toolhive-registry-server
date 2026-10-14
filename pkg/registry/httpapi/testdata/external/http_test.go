package host

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/catalog"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/httpapi"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

// application is a small admission/pass-through test double. The complete,
// coherent replacement ConsumerPages example is replacement in pages.go.
type application struct{ calls int }

var _ httpapi.ConsumerPages = (*application)(nil)
var _ httpapi.ConsumerPages = catalog.Reader(nil)

func (a *application) List(_ context.Context, _ string, q catalog.Query) (catalog.Page, error) {
	a.calls++
	if q.Limit != 30 {
		panic("HTTP failed to bound the query")
	}
	return catalog.Page{Entries: []persistence.Entry{
		{Kind: persistence.ServerKind, Server: &model.Server{Name: "com.example/z", Version: "1", Description: "host selection"}},
		{Kind: persistence.ServerKind, Server: &model.Server{Name: "com.example/a", Version: "1", Description: "host selection"}},
	}, NextCursor: "host-private:next"}, nil
}
func (a *application) Get(context.Context, string, catalog.EntryName, catalog.VersionSelector) (persistence.Entry, error) {
	a.calls++
	return persistence.Entry{}, persistence.ErrNotFound
}
func (a *application) ListVersions(context.Context, string, catalog.EntryName, catalog.PageRequest) (catalog.Page, error) {
	a.calls++
	return catalog.Page{}, nil
}

func TestHostMountsReplacementApplication(t *testing.T) {
	a := &application{}
	h, err := httpapi.ConsumerRouter(a, httpapi.ConsumerOptions{Decorate: func(d httpapi.RouteDescriptor, next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Host-Operation", string(d.Operation))
			next.ServeHTTP(w, r)
		})
	}})
	if err != nil || a.calls != 0 {
		t.Fatalf("constructor: %v / %d", err, a.calls)
	}
	mux := chi.NewRouter()
	mux.Mount("/registry", h)
	server := httptest.NewServer(mux)
	defer server.Close()
	res, err := server.Client().Get(server.URL + "/registry/public/v0.1/servers")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var wire struct {
		Servers []struct {
			Server wireServer `json:"server"`
		} `json:"servers"`
		Metadata struct {
			Count  int    `json:"count"`
			Cursor string `json:"nextCursor"`
		} `json:"metadata"`
	}
	if err := json.NewDecoder(res.Body).Decode(&wire); err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 200 || res.Header.Get("Host-Operation") != "list" || a.calls != 1 || len(wire.Servers) != 2 || wire.Servers[0].Server.Name != "com.example/z" || wire.Metadata.Count != 2 || wire.Metadata.Cursor != "host-private:next" {
		t.Fatalf("host selection: %+v / %d", wire, res.StatusCode)
	}
}
