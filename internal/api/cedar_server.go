package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	auditmw "github.com/stacklok/toolhive-registry-server/internal/audit"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/cedar"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/httpapi"
)

// NewCedarServer builds the public router for Cedar mode. Every route is the
// library's (consumer routes under /registry, admin routes under /v1 when
// admin is not nil) and is admitted by the Cedar authorizer; /openapi.json
// needs metadata.read. Only /.well-known/oauth-protected-resource is open.
// Claims, roles, /v1/me and the entry-claims endpoints do not exist in this
// mode.
func NewCedarServer(
	pages httpapi.ConsumerPages, admin *httpapi.AdminOperations, authz *cedar.Authorizer, opts ...ServerOption,
) (*chi.Mux, error) {
	cfg := &serverConfig{}
	for _, opt := range opts {
		opt(cfg)
	}
	r := chi.NewRouter()
	for _, mw := range cfg.middlewares {
		r.Use(mw)
	}
	r.Method(http.MethodGet, "/openapi.json", authz.Protect(cedar.MetadataRead, http.HandlerFunc(openAPIHandler)))
	if cfg.authInfoHandler != nil {
		r.Handle("/.well-known/oauth-protected-resource", cfg.authInfoHandler)
	}

	consumer, err := httpapi.ConsumerRouter(compatPages{next: pages, managed: cfg.managedSources}, httpapi.ConsumerOptions{
		Decorate: func(route httpapi.RouteDescriptor, next http.Handler) http.Handler {
			return auditConsumerRoute(route, authz.Decorate(route, next))
		},
	})
	if err != nil {
		return nil, err
	}
	r.Mount("/registry", consumer)

	if admin != nil {
		adminRouter, err := httpapi.AdminRouter(*admin, httpapi.AdminOptions{
			Decorate: func(route httpapi.RouteDescriptor, next http.Handler) http.Handler {
				return auditAdminRoute(route, authz.Decorate(route, next))
			},
		})
		if err != nil {
			return nil, err
		}
		r.Mount("/v1", adminRouter)
	}
	return r, nil
}

// auditAdminRoute records a library admin route with the server's audit
// wrappers. Upserts resolve create or update from the mutation the library
// reports, and a committed mutation is marked even if the response fails.
func auditAdminRoute(route httpapi.RouteDescriptor, next http.Handler) http.Handler {
	observed := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := httpapi.WithMutationObserver(r.Context(), func(result httpapi.MutationResult) {
			if info := auditmw.RouteInfoFromContext(r.Context()); info != nil {
				info.EventType = result.AuditEvent
				info.OnCreate, info.OnUpdate = "", ""
				info.MutationCommitted = result.Committed
			}
		})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
	if route.AuditCreateEvent != "" {
		return auditmw.AuditedUpsert(route.AuditCreateEvent, route.AuditUpdateEvent, route.ResourceType,
			route.ResourceParam, observed.ServeHTTP)
	}
	if route.ResourceType == auditmw.ResourceTypeEntry {
		return auditmw.AuditedEntry(route.AuditEvent, observed.ServeHTTP)
	}
	return auditmw.Audited(route.AuditEvent, route.ResourceType, route.ResourceParam, observed.ServeHTTP)
}
