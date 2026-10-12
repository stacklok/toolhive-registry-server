package api

import (
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/audit"
	"github.com/stacklok/toolhive-registry-server/internal/authorization"
)

func TestAdmissionClassificationRejectsMissingUnknownAndNil(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name         string
		declarations map[string]routeAdmission
		nilHandler   bool
	}{
		{"missing classification", nil, false},
		{"unknown action", map[string]routeAdmission{"GET /route": {
			method: "GET", template: "/route", action: authorization.Action("unknown"),
			audit: routeAudit{event: audit.EventServerList, resource: audit.ResourceTypeServer},
		}}, false},
		{"missing audit classification", map[string]routeAdmission{"GET /route": {
			method: "GET", template: "/route", action: authorization.CatalogRead,
		}}, false},
		{"nil handler", map[string]routeAdmission{"GET /route": {method: "GET", template: "/route", action: authorization.CatalogRead}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var handler http.Handler = http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("unclassified downstream handler called") })
			if tc.nilHandler {
				handler = nil
			}
			gate := admissionTestGate(t, admissionTestPermit)
			walk := admissionWalker(chi.NewRouter(), gate, tc.declarations, func(next http.Handler) http.Handler { return next })
			require.Error(t, walk("GET", "/route", handler))
		})
	}
}
