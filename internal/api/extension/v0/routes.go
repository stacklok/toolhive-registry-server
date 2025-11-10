// Package v0 provides extension API v0 endpoints for server management.
package v0

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/stacklok/toolhive-registry-server/internal/api/common"
	"github.com/stacklok/toolhive-registry-server/internal/service"
)

// Routes handles HTTP requests for extension API v0 endpoints.
type Routes struct {
	service service.RegistryService
}

// NewRoutes creates a new Routes instance with the given service.
func NewRoutes(svc service.RegistryService) *Routes {
	return &Routes{
		service: svc,
	}
}

// Router creates and configures the HTTP router for extension API v0 endpoints.
func Router(svc service.RegistryService) http.Handler {
	routes := NewRoutes(svc)

	r := chi.NewRouter()

	r.Put("/servers/{id}", routes.upsertServer)
	r.Delete("/servers/{id}", routes.deleteServer)

	return r
}

// upsertServer handles PUT /extension/v0/servers/{id}
//
// @Summary		Create or update server
// @Description	Create or update a server in the registry
// @Tags		extension
// @Accept		json
// @Produce		json
// @Param		id	path		string	true	"Server ID"
// @Failure		400	{object}	map[string]string	"Bad request"
// @Failure		501	{object}	map[string]string	"Not implemented"
// @Router		/extension/v0/servers/{id} [put]
func (*Routes) upsertServer(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if strings.TrimSpace(id) == "" {
		common.WriteErrorResponse(w, "Server ID is required", http.StatusBadRequest)
		return
	}

	common.WriteErrorResponse(w, "Creating or updating servers is not supported", http.StatusNotImplemented)
}

// deleteServer handles DELETE /extension/v0/servers/{id}
//
// @Summary		Delete server
// @Description	Delete a server from the registry
// @Tags		extension
// @Accept		json
// @Produce		json
// @Param		id	path		string	true	"Server ID"
// @Failure		400	{object}	map[string]string	"Bad request"
// @Failure		501	{object}	map[string]string	"Not implemented"
// @Router		/extension/v0/servers/{id} [delete]
func (*Routes) deleteServer(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if strings.TrimSpace(id) == "" {
		common.WriteErrorResponse(w, "Server ID is required", http.StatusBadRequest)
		return
	}

	common.WriteErrorResponse(w, "Deleting servers is not supported", http.StatusNotImplemented)
}
