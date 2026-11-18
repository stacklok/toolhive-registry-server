package httpapi_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/httpapi"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

func TestAdminRetiredClaimsPresenceAndRobustBodies(t *testing.T) {
	t.Parallel()
	for _, endpoint := range []struct{ method, path, fields string }{
		{"PUT", "/v1/sources/item", `"managed":{}`},
		{"PUT", "/v1/registries/public", `"sources":["item"]`},
		{"POST", "/v1/entries", `"server":{"name":"com.example/item","version":"1","description":"published"}`},
	} {
		for _, claim := range []string{`"claims":null`, `"claims":{}`, `"claims":[]`, `"CLAIMS":null`, `"ClAiMs":""`, `"cl\u0061ims":null`, `"claimſ":null`, `"claims":null,"claims":{}`, `"CLAIMS":{},"claims":null`, `"claims":null,"Claims":{}`} {
			t.Run(endpoint.path+claim, func(t *testing.T) {
				t.Parallel()
				f := newAdminFixture(t)
				w := adminResponse(t, mountAdmin(t, f.ops, httpapi.AdminOptions{}), endpoint.method, endpoint.path, "{"+endpoint.fields+","+claim+"}")
				if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "claims") {
					t.Fatal(w.Code, w.Body.String())
				}
			})
		}
		for _, body := range []string{"", "null", "[]", "{} {}", "{} true", "{} garbage", "{" + endpoint.fields + "} {}", "{" + endpoint.fields + "} null", "{" + endpoint.fields + "} trailing", "{" + endpoint.fields + "," + endpoint.fields + "}", "{" + endpoint.fields + ",}", `{"claims":"private"`, `{"` + strings.Repeat("a", int(httpapi.MaxAdminBodyBytes)) + `":0}`} {
			t.Run(endpoint.path+body[:min(len(body), 40)], func(t *testing.T) {
				t.Parallel()
				f := newAdminFixture(t)
				w := adminResponse(t, mountAdmin(t, f.ops, httpapi.AdminOptions{}), endpoint.method, endpoint.path, body)
				code := 400
				if len(body) > int(httpapi.MaxAdminBodyBytes) {
					code = 413
				}
				if w.Code != code || strings.Contains(w.Body.String(), "private") {
					t.Fatal(w.Code, w.Body.String())
				}
			})
		}
	}
}

func TestAdminValidationBeforeCapabilities(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ method, path, body string }{
		{"PUT", "/v1/sources/item", `{}`},
		{"PUT", "/v1/sources/item", `{"type":"managed"}`},
		{"PUT", "/v1/sources/INVALID", `{"managed":{}}`},
		{"PUT", "/v1/sources/" + strings.Repeat("a", 64), `{"managed":{}}`},
		{"PUT", "/v1/sources/item", `{"managed":{},"file":{"path":"/registry.json"}}`},
		{"PUT", "/v1/sources/item", `{"git":{"repository":"https://example.org"}}`},
		{"PUT", "/v1/sources/item", `{"git":{"repository":"https://example.org","branch":"main","tag":"v1"},"syncPolicy":{"interval":"1h"}}`},
		{"PUT", "/v1/sources/item", `{"git":{"repository":"https://example.org","auth":{"username":"user","passwordFile":"relative"}},"syncPolicy":{"interval":"1h"}}`},
		{"PUT", "/v1/sources/item", `{"api":{"endpoint":""},"syncPolicy":{"interval":"1h"}}`},
		{"PUT", "/v1/sources/item", `{"file":{"url":"file:///registry.json"},"syncPolicy":{"interval":"1h"}}`},
		{"PUT", "/v1/sources/item", `{"file":{"data":"not JSON"}}`},
		{"PUT", "/v1/sources/item", `{"file":{"data":"{}"}}`},
		{"PUT", "/v1/sources/item", `{"file":{"path":"/registry.json","data":"inline"},"syncPolicy":{"interval":"1h"}}`},
		{"PUT", "/v1/registries/public", `{"sources":[]}`},
		{"PUT", "/v1/registries/public", `{"sources":[""]}`},
		{"PUT", "/v1/registries/public", `{"sources":["item","item"]}`},
		{"PUT", "/v1/registries/public", `{"sources":["INVALID"]}`},
		{"PUT", "/v1/registries/public", `{"sources":["has space"]}`},
		{"POST", "/v1/entries", `{}`},
		{"POST", "/v1/entries", `{"skill":{"namespace":"com.example","name":"item","version":"1","description":"published"},"SKILL":null}`},
		{"POST", "/v1/entries", `{"skill":{"namespace":"com.example","name":"item","version":"1","description":"published"},"ſkill":null}`},
		{"POST", "/v1/entries", `{"server":null,"skill":null,"plugin":null}`},
		{"POST", "/v1/entries", `{"server":{},"skill":{}}`},
		{"POST", "/v1/entries", `{"server":{},"plugin":{}}`},
		{"POST", "/v1/entries", `{"skill":{},"plugin":{}}`},
		{"POST", "/v1/entries", `{"server":{},"skill":{},"plugin":{}}`},
		{"POST", "/v1/entries", `{"server":{"name":"invalid","version":"1","description":"published"}}`},
		{"POST", "/v1/entries", `{"server":{"name":"com.example/item","description":"published"}}`},
		{"POST", "/v1/entries", `{"server":{"name":"com.example/item","version":"1"}}`},
		{"POST", "/v1/entries", `{"skill":{"name":"item","version":"1","description":"published"}}`},
		{"POST", "/v1/entries", `{"plugin":{"namespace":"com.example","name":"item","description":"published"}}`},
		{"POST", "/v1/entries", `{"plugin":{"namespace":"com.example","name":"item","version":"1","id":"client-id","description":"published"}}`},
		{"DELETE", "/v1/entries/other/item/versions/1", ""},
		{"DELETE", "/v1/entries/server/not-server-name/versions/1", ""},
		{"DELETE", "/v1/entries/server/com.example%2F%2Fitem/versions/1", ""},
		{"DELETE", "/v1/entries/skill/has%20space/versions/1", ""},
		{"DELETE", "/v1/entries/plugin/item/versions/%00", ""},
		{"GET", "/v1/sources/%20", ""},
		{"GET", "/v1/registries/%00", ""},
	} {
		t.Run(tc.method+tc.path+tc.body, func(t *testing.T) {
			t.Parallel()
			f := newAdminFixture(t)
			w := adminResponse(t, mountAdmin(t, f.ops, httpapi.AdminOptions{}), tc.method, tc.path, tc.body)
			if w.Code != 400 {
				t.Fatal(w.Code, w.Body.String())
			}
		})
	}
}

// As in v1.5.4, a syncPolicy or filter a source cannot use is ignored rather
// than rejected; it never reaches storage.
func TestAdminIgnoresUnusableSourceSettings(t *testing.T) {
	t.Parallel()
	const registry = `{"version":"1.0.0","meta":{"last_updated":"2025-01-01T00:00:00Z"},"data":{"servers":[` +
		`{"$schema":"https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json",` +
		`"name":"com.example/inline","version":"1.0.0","description":"inline server"}]}}`
	quoted, err := json.Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		body string
		want persistence.SourceDefinition
	}{
		{`{"managed":{},"syncPolicy":{"interval":"1h"}}`, persistence.SourceDefinition{Name: "item", Managed: &persistence.ManagedSpec{}}},
		{`{"managed":{},"filter":{}}`, persistence.SourceDefinition{Name: "item", Managed: &persistence.ManagedSpec{}}},
		{`{"kubernetes":{"Namespaces":["team"]},"syncPolicy":{"interval":"1h"},"filter":{}}`,
			persistence.SourceDefinition{Name: "item", Kubernetes: &persistence.KubernetesSpec{Namespaces: []string{"team"}}}},
		{`{"file":{"data":` + string(quoted) + `},"syncPolicy":{"interval":"7s"}}`,
			persistence.SourceDefinition{Name: "item", File: &persistence.FileSpec{Data: registry}}},
	} {
		t.Run(tc.body[:min(len(tc.body), 60)], func(t *testing.T) {
			t.Parallel()
			f := newAdminFixture(t)
			f.sources.EXPECT().CreateSource(gomock.Any(), tc.want).Return(tc.want, nil)
			w := adminResponse(t, mountAdmin(t, f.ops, httpapi.AdminOptions{}), "PUT", "/v1/sources/item", tc.body)
			if w.Code != http.StatusCreated {
				t.Fatal(w.Code, w.Body.String())
			}
		})
	}
}
