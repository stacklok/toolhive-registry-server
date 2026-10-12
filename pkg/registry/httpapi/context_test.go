package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/catalog"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/httpapi"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

type contextPages struct {
	*selectedPages
	want context.Context
	t    *testing.T
}

func (p *contextPages) Get(ctx context.Context, view string, name catalog.EntryName, selector catalog.VersionSelector) (persistence.Entry, error) {
	if ctx != p.want {
		p.t.Error("application context changed")
	}
	return p.selectedPages.Get(ctx, view, name, selector)
}

func TestLatestSelectorsAndHostContext(t *testing.T) {
	t.Parallel()
	for _, family := range []struct {
		kind persistence.EntryKind
		path string
	}{
		{persistence.ServerKind, "/servers/com.example%2Fitem/versions/"},
		{persistence.SkillKind, "/x/dev.toolhive/skills/com.example/item/versions/"},
		{persistence.PluginKind, "/x/dev.toolhive/plugins/com.example/item/versions/"},
	} {
		for _, version := range []string{"latest", "LATEST", "1.2.3"} {
			fake := &selectedPages{entry: entry(family.kind, "chosen", "chosen")}
			pages := &contextPages{selectedPages: fake, t: t}
			h := mounted(t, pages, httpapi.ConsumerOptions{Middleware: []func(http.Handler) http.Handler{
				func(next http.Handler) http.Handler {
					return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						pages.want = r.Context()
						next.ServeHTTP(w, r)
					})
				},
			}})
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest("GET", "/registry/public/v0.1"+family.path+version, nil))
			want := catalog.VersionSelector{Exact: version}
			if version == "latest" {
				want = catalog.VersionSelector{Latest: true}
			}
			if w.Code != 200 || fake.calls != 1 || fake.selector != want {
				t.Fatalf("%s %s: %d / %+v", family.kind, version, w.Code, fake)
			}
		}
	}
}

func TestEncodedNamespaceAndIgnoredHistoryQuery(t *testing.T) {
	t.Parallel()
	for _, family := range []struct {
		kind persistence.EntryKind
		path string
	}{
		{persistence.SkillKind, "skills"}, {persistence.PluginKind, "plugins"},
	} {
		fake := &selectedPages{entry: entry(family.kind, "chosen", "1")}
		h := mounted(t, fake, httpapi.ConsumerOptions{})
		// Legacy namespaces are generic URL params, not server-name validation.
		base := "/registry/test%2Fregistry/v0.1/x/dev.toolhive/" + family.path + "/team%2Fnamespace/item"
		w := response(t, h, "GET", base)
		if w.Code != 200 || fake.name.Namespace != "team/namespace" || fake.view != "test/registry" {
			t.Fatal(fake, w.Code)
		}
		w = response(t, h, "GET", base+"/versions?limit=-1&cursor=%00&search=%ZZ&updated_since=bad&status=invalid")
		if w.Code != 200 || fake.calls != 2 || fake.request != (catalog.PageRequest{Limit: 30}) {
			t.Fatal(fake, w.Code)
		}
		w = response(t, h, "GET", "/registry/"+strings.Repeat("a", 4097)+"/v0.1/x/dev.toolhive/"+family.path)
		if w.Code != 400 || fake.calls != 2 {
			t.Fatal(fake, w.Code)
		}
	}
}
