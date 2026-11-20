package cedar

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/cedar-policy/cedar-go/types"
	"github.com/go-chi/chi/v5"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/httpapi"
)

// Response bodies. They are fixed so that no policy, claim or diagnostic text
// reaches the client.
const (
	deniedBody       = `{"error":"authorization denied or unavailable"}` + "\n"
	unauthorizedBody = `{"error":"unauthorized"}` + "\n"
	wiringBody       = `{"error":"internal server error"}` + "\n"
	badViewBody      = `{"error":"invalid registry name"}` + "\n"
)

// Decision records one admission outcome, for audit logging.
type Decision struct {
	// Principal is the [PrincipalID], [AnonymousPrincipal], or "" when the
	// identity's issuer or subject is unusable.
	Principal string
	Action    Action
	// Resource is the Cedar resource UID, e.g. Registry::View::"main" or
	// Registry::Instance::"catalog".
	Resource string
	Allowed  bool
	// Revision is the policy revision, see [Engine.Revision].
	Revision string
	// Err is nil for a clean allow or deny. Otherwise it wraps
	// [ErrEvaluation], [ErrInvalidIdentity] or [ErrAuthenticationRequired].
	Err error
}

// Options configures an [Authorizer].
type Options struct {
	// Identity returns the verified caller for the request. ok=false means the
	// host's authentication did not run, which is a wiring bug: the request
	// fails with 500. Required.
	Identity func(*http.Request) (Identity, bool)
	// Mappings select the claims that become principal groups and scopes. Check
	// them with [ValidateMappings] first.
	Mappings []Mapping
	// OnDecision, if set, receives every admission outcome once the route and
	// identity are resolved: allow, deny, evaluation failure, identity failure
	// and anonymous callers rejected from authenticated-only actions. It is not
	// called for wiring faults (500) or malformed view names (400). It runs on
	// the request goroutine and must not block.
	OnDecision func(*http.Request, Decision)
	// Challenge, if set, returns the WWW-Authenticate header value for 401
	// responses, so hosts can advertise their OAuth resource metadata. When nil
	// or when it returns "", the header is "Bearer".
	Challenge func(*http.Request) string
}

// Authorizer admits HTTP requests by evaluating an [Engine] per route. It is
// immutable and safe for concurrent use.
type Authorizer struct {
	engine  *Engine
	opts    Options
	actions map[httpapi.RouteDescriptor]Action
}

type routeKey struct{ method, template string }

const (
	sourceRoute = "/sources/{name}"
	viewRoute   = "/registries/{name}"
)

// adminActions is the closed action table for httpapi.AdminRoutes. Every admin
// route must appear exactly once; NewAuthorizer enforces that.
var adminActions = map[routeKey]Action{
	{http.MethodGet, "/sources"}:                                     SourcesRead,
	{http.MethodGet, sourceRoute}:                                    SourcesRead,
	{http.MethodGet, "/sources/{name}/entries"}:                      SourcesRead,
	{http.MethodPut, sourceRoute}:                                    SourcesWrite,
	{http.MethodDelete, sourceRoute}:                                 SourcesDelete,
	{http.MethodGet, "/registries"}:                                  ViewsRead,
	{http.MethodGet, viewRoute}:                                      ViewsRead,
	{http.MethodGet, "/registries/{name}/entries"}:                   ViewsRead,
	{http.MethodPut, viewRoute}:                                      ViewsWrite,
	{http.MethodDelete, viewRoute}:                                   ViewsDelete,
	{http.MethodPost, "/entries"}:                                    EntriesPublish,
	{http.MethodDelete, "/entries/{type}/{name}/versions/{version}"}: EntriesDelete,
}

// NewAuthorizer returns an Authorizer for engine. It fails unless every route
// in httpapi.ConsumerRoutes and httpapi.AdminRoutes maps to exactly one action,
// so a library route added without an action stops the host at startup.
func NewAuthorizer(engine *Engine, opts Options) (*Authorizer, error) {
	if engine == nil || engine.policies == nil {
		return nil, errors.New("cedar: engine is required")
	}
	if opts.Identity == nil {
		return nil, errors.New("cedar: Options.Identity is required")
	}
	actions, err := routeActions(httpapi.ConsumerRoutes(), httpapi.AdminRoutes())
	if err != nil {
		return nil, err
	}
	opts.Mappings = append([]Mapping(nil), opts.Mappings...)
	return &Authorizer{engine: engine, opts: opts, actions: actions}, nil
}

// Decorate admits requests to one library route. It has the signature of
// httpapi.ConsumerOptions.Decorate and httpapi.AdminOptions.Decorate.
//
// Consumer routes are catalog.read on the view named by the route's registry
// path parameter; admin routes are the matching instance action. A descriptor
// that is not in the library's route inventory gets a handler that always
// returns 500.
func (a *Authorizer) Decorate(route httpapi.RouteDescriptor, next http.Handler) http.Handler {
	action, ok := a.actions[route]
	if !ok || next == nil {
		return http.HandlerFunc(writeWiringFault)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !matchesRoute(r, route) {
			writeWiringFault(w, r)
			return
		}
		resource := instance
		if action == CatalogRead {
			view, err := requestedView(r, route.RegistryParam)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, badViewBody)
				return
			}
			resource = types.EntityUID{Type: viewType, ID: types.String(view)}
		}
		a.admit(w, r, action, resource, next)
	})
}

// Protect admits requests to a host-owned route, such as /openapi.json, with
// an instance-scoped action. catalog.read needs a view and is not accepted;
// it, an unknown action or a nil handler yields a handler that always returns
// 500. Protect performs no route-pattern check.
func (a *Authorizer) Protect(action Action, next http.Handler) http.Handler {
	if !action.Valid() || action == CatalogRead || next == nil {
		return http.HandlerFunc(writeWiringFault)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.admit(w, r, action, instance, next)
	})
}

func routeActions(consumer, admin []httpapi.RouteDescriptor) (map[httpapi.RouteDescriptor]Action, error) {
	actions := make(map[httpapi.RouteDescriptor]Action, len(consumer)+len(admin))
	for _, route := range consumer {
		if route.RegistryParam == "" {
			return nil, fmt.Errorf("cedar: consumer route %s %s has no registry parameter", route.Method, route.Template)
		}
		if _, dup := actions[route]; dup {
			return nil, fmt.Errorf("cedar: route %s %s listed twice", route.Method, route.Template)
		}
		actions[route] = CatalogRead
	}
	used := make(map[routeKey]bool, len(adminActions))
	for _, route := range admin {
		key := routeKey{route.Method, route.Template}
		action, ok := adminActions[key]
		if !ok {
			return nil, fmt.Errorf("cedar: admin route %s %s has no action", route.Method, route.Template)
		}
		if _, dup := actions[route]; dup || used[key] {
			return nil, fmt.Errorf("cedar: route %s %s listed twice", route.Method, route.Template)
		}
		used[key] = true
		actions[route] = action
	}
	if len(used) != len(adminActions) {
		return nil, errors.New("cedar: admin action table names a route the library does not register")
	}
	return actions, nil
}

// admit runs the per-request admission flow and calls next only on allow.
func (a *Authorizer) admit(w http.ResponseWriter, r *http.Request, action Action, resource types.EntityUID, next http.Handler) {
	identity, ok := a.opts.Identity(r)
	if !ok {
		writeWiringFault(w, r)
		return
	}
	decision := Decision{Action: action, Resource: resource.String(), Revision: a.engine.revision}
	if identity.Anonymous {
		decision.Principal = AnonymousPrincipal
		if action != CatalogRead && action != MetadataRead {
			decision.Err = ErrAuthenticationRequired
			a.report(r, decision)
			a.writeUnauthorized(w, r)
			return
		}
	} else {
		decision.Principal = PrincipalID(identity.Issuer, identity.Subject)
	}
	p, err := principal(identity, a.opts.Mappings)
	if err != nil {
		decision.Err = err
		a.report(r, decision)
		a.writeUnauthorized(w, r)
		return
	}
	decision.Allowed, decision.Err = a.engine.evaluate(p, action, resource)
	a.report(r, decision)
	switch {
	case decision.Err != nil:
		writeJSON(w, http.StatusServiceUnavailable, deniedBody)
	case !decision.Allowed:
		writeJSON(w, http.StatusForbidden, deniedBody)
	default:
		next.ServeHTTP(w, r)
	}
}

func (a *Authorizer) report(r *http.Request, decision Decision) {
	if a.opts.OnDecision != nil {
		a.opts.OnDecision(r, decision)
	}
}

func (a *Authorizer) writeUnauthorized(w http.ResponseWriter, r *http.Request) {
	challenge := "Bearer"
	if a.opts.Challenge != nil {
		if c := a.opts.Challenge(r); c != "" {
			challenge = c
		}
	}
	w.Header().Set("WWW-Authenticate", challenge)
	writeJSON(w, http.StatusUnauthorized, unauthorizedBody)
}

// matchesRoute checks that chi actually dispatched the request to the route's
// method and template. RoutePatterns ends with the pattern of the innermost
// router, which is the library router, so this holds however the host mounts it.
func matchesRoute(r *http.Request, route httpapi.RouteDescriptor) bool {
	rctx := chi.RouteContext(r.Context())
	return r.Method == route.Method && rctx != nil && len(rctx.RoutePatterns) > 0 &&
		rctx.RoutePatterns[len(rctx.RoutePatterns)-1] == route.Template
}

// requestedView decodes the view name exactly as the HTTP adapter does, with no
// normalisation, so the policy sees the name the handler will serve.
func requestedView(r *http.Request, param string) (string, error) {
	view, err := url.PathUnescape(chi.URLParam(r, param))
	if err != nil || strings.TrimSpace(view) == "" || strings.ContainsAny(view, " \t\n\r\x00") ||
		len(view) > maxIdentityString || !utf8.ValidString(view) {
		return "", errors.New("invalid view name")
	}
	return view, nil
}

func writeWiringFault(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusInternalServerError, wiringBody)
}

func writeJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}
