// Package v1 provides API v1 endpoints for managing sources, registries, and entries.
package v1

import (
	"net/http"

	"github.com/go-chi/chi/v5"

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
func Router(svc service.RegistryService) http.Handler {
	routes := NewRoutes(svc)

	r := chi.NewRouter()

	// Source endpoints
	r.Get("/sources", routes.listSources)
	r.Get("/sources/{name}", routes.getSource)
	r.Put("/sources/{name}", routes.upsertSource)
	r.Delete("/sources/{name}", routes.deleteSource)
	r.Get("/sources/{name}/entries", routes.listSourceEntries)

	// Registry endpoints
	r.Get("/registries", routes.listRegistries)
	r.Get("/registries/{name}", routes.getRegistry)
	r.Put("/registries/{name}", routes.upsertRegistry)
	r.Delete("/registries/{name}", routes.deleteRegistry)
	r.Get("/registries/{name}/entries", routes.listRegistryEntries)

	// Entry endpoints
	r.Post("/entries", routes.publishEntry)
	r.Delete("/entries/{type}/{name}/versions/{version}", routes.deletePublishedEntry)
	r.Put("/entries/{type}/{name}/claims", routes.updateEntryClaims)

	return r
}
