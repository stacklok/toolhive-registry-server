package httpapi

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// Authorizer admits one HTTP/application operation using host-owned context.
// Authentication must run first via Middleware. The request context is passed
// through unchanged, so implementations must honor its cancellation. This contract
// defines no identity, tenant, policy engine or persistence policy; it does not
// authorize exposure consent. Authorizers are borrowed: routers never start or close
// them. A single decorator shares its authorizer across requests, so hosts must make
// implementations safe for concurrent use. false or any error denies access. Errors
// are never returned to the client.
type Authorizer interface {
	Authorize(context.Context, RouteDescriptor) (bool, error)
}

// Authorize decorates consumer or admin routes with a fail-closed admission gate.
// Assign it to Options.Decorate, or compose it with a host audit decorator. It
// borrows and shares authorizer across requests; the host retains its lifecycle and
// must provide a concurrency-safe implementation that honors request cancellation.
// Nil (including typed nil), unknown descriptors and method/template mismatches deny
// before calling the authorizer or application. Low-level routers with no Decorate
// remain explicitly policy-free; this helper does not authenticate requests.
func Authorize(authorizer Authorizer) func(RouteDescriptor, http.Handler) http.Handler {
	routes := append(ConsumerRoutes(), AdminRoutes()...)
	return func(route RouteDescriptor, next http.Handler) http.Handler {
		known := false
		for _, candidate := range routes {
			if candidate == route {
				known = true
				break
			}
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rctx := chi.RouteContext(r.Context())
			if !known || isNil(authorizer) || isNil(next) || r.Method != route.Method ||
				rctx == nil || len(rctx.RoutePatterns) == 0 || rctx.RoutePatterns[len(rctx.RoutePatterns)-1] != route.Template {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			allow, err := authorizer.Authorize(r.Context(), route)
			if err != nil || !allow {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
