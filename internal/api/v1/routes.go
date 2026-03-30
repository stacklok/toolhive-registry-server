// Package v1 provides API v1 endpoints for managing sources, registries, and entries.
package v1

import (
	"net/http"

	"github.com/go-chi/chi/v5"

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

	// Source endpoints — require manageSources role
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireRole(auth.RoleManageSources, authzCfg))
		r.Get("/sources", routes.listSources)
		r.Get("/sources/{name}", routes.getSource)
		r.Put("/sources/{name}", routes.upsertSource)
		r.Delete("/sources/{name}", routes.deleteSource)
		r.Get("/sources/{name}/entries", routes.listSourceEntries)
	})

	// Registry read endpoints — authenticated only (no role requirement).
	// GET and PUT/DELETE on /registries/{name} are in separate groups
	// because reads only need authentication while writes need manageRegistries.
	r.Get("/registries", routes.listRegistries)
	r.Get("/registries/{name}", routes.getRegistry)

	// Registry write endpoints — require manageRegistries role
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireRole(auth.RoleManageRegistries, authzCfg))
		r.Get("/registries/{name}/entries", routes.listRegistryEntries)
		r.Put("/registries/{name}", routes.upsertRegistry)
		r.Delete("/registries/{name}", routes.deleteRegistry)
	})

	// Entry endpoints — require manageEntries role
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireRole(auth.RoleManageEntries, authzCfg))
		r.Post("/entries", routes.publishEntry)
		r.Delete("/entries/{type}/{name}/versions/{version}", routes.deletePublishedEntry)
		r.Put("/entries/{type}/{name}/claims", routes.updateEntryClaims)
	})

	return r
}
