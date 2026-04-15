// Package v1 provides API v1 endpoints for managing sources, registries, and entries.
package v1

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	auditmw "github.com/stacklok/toolhive-registry-server/internal/audit"
	"github.com/stacklok/toolhive-registry-server/internal/auth"
	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/service"
)

// Routes handles HTTP requests for API v1 endpoints.
type Routes struct {
	service service.RegistryService
}

// NewRoutes creates a new Routes instance with the given service.
func NewRoutes(svc service.RegistryService) *Routes {
	return &Routes{
		service: svc,
	}
}

// Router creates and configures the HTTP router for API v1 endpoints.
func Router(svc service.RegistryService, authzCfg *config.AuthzConfig) http.Handler {
	routes := NewRoutes(svc)

	r := chi.NewRouter()

	// Caller identity — authenticated only (no role requirement).
	r.Get("/me", auditmw.Audited(auditmw.EventUserInfo, auditmw.ResourceTypeUser, "", routes.getMe))

	// Source endpoints — require manageSources role
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireRole(auth.RoleManageSources, authzCfg))
		r.Get("/sources",
			auditmw.Audited(auditmw.EventSourceList, auditmw.ResourceTypeSource, "", routes.listSources))
		r.Get("/sources/{name}",
			auditmw.Audited(auditmw.EventSourceRead, auditmw.ResourceTypeSource, "name", routes.getSource))
		r.Put("/sources/{name}",
			auditmw.AuditedUpsert(auditmw.EventSourceCreate, auditmw.EventSourceUpdate,
				auditmw.ResourceTypeSource, "name", routes.upsertSource))
		r.Delete("/sources/{name}",
			auditmw.Audited(auditmw.EventSourceDelete, auditmw.ResourceTypeSource, "name", routes.deleteSource))
		r.Get("/sources/{name}/entries",
			auditmw.Audited(auditmw.EventSourceEntriesList, auditmw.ResourceTypeSource, "name",
				routes.listSourceEntries))
	})

	// Registry read endpoints — authenticated only (no role requirement).
	// GET and PUT/DELETE on /registries/{name} are in separate groups
	// because reads only need authentication while writes need manageRegistries.
	r.Get("/registries",
		auditmw.Audited(auditmw.EventRegistryList, auditmw.ResourceTypeRegistry, "", routes.listRegistries))
	r.Get("/registries/{name}",
		auditmw.Audited(auditmw.EventRegistryRead, auditmw.ResourceTypeRegistry, "name", routes.getRegistry))

	// Registry write endpoints — require manageRegistries role
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireRole(auth.RoleManageRegistries, authzCfg))
		r.Get("/registries/{name}/entries",
			auditmw.Audited(auditmw.EventRegistryEntriesList, auditmw.ResourceTypeRegistry, "name",
				routes.listRegistryEntries))
		r.Put("/registries/{name}",
			auditmw.AuditedUpsert(auditmw.EventRegistryCreate, auditmw.EventRegistryUpdate,
				auditmw.ResourceTypeRegistry, "name", routes.upsertRegistry))
		r.Delete("/registries/{name}",
			auditmw.Audited(auditmw.EventRegistryDelete, auditmw.ResourceTypeRegistry, "name",
				routes.deleteRegistry))
	})

	// Entry endpoints — require manageEntries role
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireRole(auth.RoleManageEntries, authzCfg))
		r.Post("/entries",
			auditmw.Audited(auditmw.EventEntryPublish, auditmw.ResourceTypeEntry, "", routes.publishEntry))
		r.Delete("/entries/{type}/{name}/versions/{version}",
			auditmw.AuditedEntry(auditmw.EventEntryDelete, routes.deletePublishedEntry))
		r.Put("/entries/{type}/{name}/claims",
			auditmw.AuditedEntry(auditmw.EventEntryClaims, routes.updateEntryClaims))
	})

	return r
}
