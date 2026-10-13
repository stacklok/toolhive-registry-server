package api

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/stacklok/toolhive-registry-server/internal/audit"
	"github.com/stacklok/toolhive-registry-server/internal/authorization"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/catalog"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/httpapi"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

// NewCatalogServer mounts only public HTTP adapters over guarded capabilities.
// The legacy route manifest supplies stable closed action/audit classifications,
// not legacy handlers, role middleware or a RegistryService adapter.
//
//nolint:gocyclo // Construct the complete closed manifest atomically before returning a handler.
func NewCatalogServer(reader catalog.Reader, adminOps httpapi.AdminOperations, registrar *authorization.Registrar,
	admin bool, authenticate func(http.Handler) http.Handler, opts ...ServerOption) (*chi.Mux, error) {
	if registrar == nil || authenticate == nil {
		return nil, authorization.ErrEvidence
	}
	consumer, operations, err := registrar.GuardCatalog(reader, adminOps)
	if err != nil {
		return nil, err
	}
	cfg := &serverConfig{}
	for _, opt := range opts {
		opt(cfg)
	}
	router := chi.NewRouter()
	for _, mw := range cfg.middlewares {
		router.Use(mw)
	}
	manifest := map[string]routeAdmission{}
	for _, route := range admissionManifest(admin) {
		manifest[route.method+" "+route.template] = route
	}
	var decorationErr error
	decorator := func(prefix string) func(httpapi.RouteDescriptor, http.Handler) http.Handler {
		return func(d httpapi.RouteDescriptor, h http.Handler) http.Handler {
			template := prefix + d.Template
			key := d.Method + " " + template
			expected, ok := manifest[key]
			if !ok || expected.retired || !matchesCatalogDescriptor(d, expected) {
				decorationErr = fmt.Errorf("unclassified public HTTP descriptor: %s", key)
				return h
			}
			delete(manifest, key)
			wrapped := catalogAudit(d, h)
			wrapped, err = registrar.DecorateCatalog(d.Method, template, expected.action, wrapped, authenticate)
			if err != nil {
				decorationErr = err
				return h
			}
			return wrapped
		}
	}
	consumerRouter, err := httpapi.ConsumerRouter(consumer, httpapi.ConsumerOptions{Decorate: decorator("/registry")})
	if err != nil {
		return nil, err
	}
	router.Mount("/registry", consumerRouter)
	if admin {
		adminRouter, err := httpapi.AdminRouter(operations, httpapi.AdminOptions{Decorate: decorator("/v1")})
		if err != nil {
			return nil, err
		}
		router.Mount("/v1", adminRouter)
	}
	if decorationErr != nil {
		return nil, decorationErr
	}
	for key, route := range manifest {
		if route.retired {
			router.Method(route.method, route.template, authenticate(route.audit.wrap(http.HandlerFunc(retiredAdmissionHandler))))
		} else if route.action == authorization.MetadataRead {
			if err := registrar.Register(router, route.method, route.template, route.action,
				http.HandlerFunc(openAPIHandler), authenticate); err != nil {
				return nil, err
			}
		} else {
			return nil, fmt.Errorf("missing public HTTP descriptor: %s", key)
		}
	}
	if cfg.authInfoHandler != nil {
		router.Method(http.MethodGet, "/.well-known/oauth-protected-resource", cfg.authInfoHandler)
	}
	return router, nil
}

//nolint:gocyclo // A single closed descriptor validation gate covers all consumer and admin fields.
func matchesCatalogDescriptor(d httpapi.RouteDescriptor, e routeAdmission) bool {
	if d.ResourceType != e.audit.resource || d.ResourceParam != e.audit.param {
		return false
	}
	if d.AuditCreateEvent != "" {
		if d.AuditCreateEvent != e.audit.create || d.AuditUpdateEvent != e.audit.event || d.AuditEvent != "" {
			return false
		}
	} else if d.AuditEvent != e.audit.event || d.AuditUpdateEvent != "" {
		return false
	}
	expected := httpapi.Operation(httpapi.Get)
	switch d.Method {
	case http.MethodPut:
		expected = httpapi.Upsert
	case http.MethodDelete:
		expected = httpapi.Delete
	case http.MethodPost:
		expected = httpapi.Publish
	case http.MethodGet:
		switch {
		case strings.HasSuffix(d.Template, "/entries"):
			expected = httpapi.ListEntries
		case strings.HasSuffix(d.Template, "/versions"):
			expected = httpapi.ListVersions
		case strings.HasSuffix(d.Template, "/{namespace}/{name}"):
			expected = httpapi.GetLatest
		case d.ResourceParam == "" || strings.HasSuffix(d.Template, "/servers") ||
			strings.HasSuffix(strings.TrimSuffix(d.Template, "/"), "/skills") ||
			strings.HasSuffix(strings.TrimSuffix(d.Template, "/"), "/plugins"):
			expected = httpapi.List
		}
	default:
		return false
	}
	if d.Operation != expected {
		return false
	}
	if e.action == authorization.CatalogRead {
		kind := map[string]persistence.EntryKind{
			"server": persistence.ServerKind, "skill": persistence.SkillKind, "plugin": persistence.PluginKind,
		}[d.ResourceType]
		namespace := ""
		if kind != persistence.ServerKind {
			namespace = "namespace"
		}
		return d.Kind == kind && d.RegistryParam == "registryName" && d.NamespaceParam == namespace && d.KindParam == ""
	}
	return d.Kind == "" && d.RegistryParam == "" && d.NamespaceParam == "" &&
		(d.KindParam == "" || d.KindParam == "type" && e.action == authorization.EntriesDelete)
}

func catalogAudit(d httpapi.RouteDescriptor, h http.Handler) http.Handler {
	observed := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := httpapi.WithMutationObserver(r.Context(), func(result httpapi.MutationResult) {
			if info := audit.RouteInfoFromContext(r.Context()); info != nil {
				info.EventType = result.AuditEvent
				info.OnCreate = ""
				info.OnUpdate = ""
				info.MutationCommitted = result.Committed
			}
		})
		h.ServeHTTP(w, r.WithContext(ctx))
	})
	if d.AuditCreateEvent != "" {
		return audit.AuditedUpsert(d.AuditCreateEvent, d.AuditUpdateEvent, d.ResourceType, d.ResourceParam, observed.ServeHTTP)
	}
	switch d.ResourceType {
	case "server":
		return audit.AuditedServer(d.AuditEvent, observed.ServeHTTP)
	case "skill":
		return audit.AuditedSkill(d.AuditEvent, observed.ServeHTTP)
	case "plugin":
		return audit.AuditedPlugin(d.AuditEvent, observed.ServeHTTP)
	case "entry":
		return audit.AuditedEntry(d.AuditEvent, observed.ServeHTTP)
	default:
		return audit.Audited(d.AuditEvent, d.ResourceType, d.ResourceParam, observed.ServeHTTP)
	}
}
