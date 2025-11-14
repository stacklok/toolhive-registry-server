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

	r.Get("/registries", routes.listRegistries)

	r.Get("/registries/{registryName}", routes.getRegistry)
	r.Put("/registries/{registryName}", routes.upsertRegistry)
	r.Delete("/registries/{registryName}", routes.deleteRegistry)

	r.Put("/registries/{registryName}/servers/{serverName}/versions/{version}", routes.upsertVersion)
	r.Delete("/registries/{registryName}/servers/{serverName}/versions/{version}", routes.deleteVersion)

	return r
}

// listRegistries handles GET /extension/v0/registries
//
// @Summary		List registries
// @Description	List all registries
// @Tags		extension
// @Accept		json
// @Produce		json
// @Failure		501	{object}	map[string]string	"Not implemented"
// @Router		/extension/v0/registries [get]
func (*Routes) listRegistries(w http.ResponseWriter, _ *http.Request) {
	common.WriteErrorResponse(w, "Listing registries is not supported", http.StatusNotImplemented)
}

// getRegistry handles GET /extension/v0/registries/{registryName}
//
// @Summary		Get registry
// @Description	Get a registry by name
// @Tags		extension
// @Accept		json
// @Produce		json
// @Param		registryName	path	string	true	"Registry Name"
// @Failure		400	{object}	map[string]string	"Bad request"
// @Failure		501	{object}	map[string]string	"Not implemented"
// @Router		/extension/v0/registries/{registryName} [get]
func (*Routes) getRegistry(w http.ResponseWriter, r *http.Request) {
	registryName := chi.URLParam(r, "registryName")
	if strings.TrimSpace(registryName) == "" {
		common.WriteErrorResponse(w, "Registry name is required", http.StatusBadRequest)
		return
	}

	common.WriteErrorResponse(w, "Getting registry is not supported", http.StatusNotImplemented)
}

// upsertRegistry handles PUT /extension/v0/registries/{registryName}
//
// @Summary		Create or update registry
// @Description	Create or update a registry
// @Tags		extension
// @Accept		json
// @Produce		json
// @Param		registryName	path	string	true	"Registry Name"
// @Failure		400	{object}	map[string]string	"Bad request"
// @Failure		501	{object}	map[string]string	"Not implemented"
// @Router		/extension/v0/registries/{registryName} [put]
func (*Routes) upsertRegistry(w http.ResponseWriter, r *http.Request) {
	registryName := chi.URLParam(r, "registryName")
	if strings.TrimSpace(registryName) == "" {
		common.WriteErrorResponse(w, "Registry name is required", http.StatusBadRequest)
		return
	}

	common.WriteErrorResponse(w, "Creating or updating registry is not supported", http.StatusNotImplemented)
}

// deleteRegistry handles DELETE /extension/v0/registries/{registryName}
//
// @Summary		Delete registry
// @Description	Delete a registry
// @Tags		extension
// @Accept		json
// @Produce		json
// @Param		registryName	path	string	true	"Registry Name"
// @Failure		400	{object}	map[string]string	"Bad request"
// @Failure		501	{object}	map[string]string	"Not implemented"
// @Router		/extension/v0/registries/{registryName} [delete]
func (*Routes) deleteRegistry(w http.ResponseWriter, r *http.Request) {
	registryName := chi.URLParam(r, "registryName")
	if strings.TrimSpace(registryName) == "" {
		common.WriteErrorResponse(w, "Registry name is required", http.StatusBadRequest)
		return
	}

	common.WriteErrorResponse(w, "Deleting registry is not supported", http.StatusNotImplemented)
}

// upsertVersion handles PUT /extension/v0/registries/{registryName}/servers/{serverName}/versions/{version}
//
// @Summary		Create or update server
// @Description	Create or update a server in the registry
// @Tags		extension
// @Accept		json
// @Produce		json
// @Param		registryName	path	string	true	"Registry Name"
// @Param		serverName		path	string	true	"URL-encoded server name (e.g., \"com.example%2Fmy-server\")"
// @Param		version			path	string	true	"URL-encoded version to retrieve (e.g., \"1.0.0\")"
// @Failure		400	{object}	map[string]string	"Bad request"
// @Failure		501	{object}	map[string]string	"Not implemented"
// @Router		/extension/v0/registries/{registryName}/servers/{serverName}/versions/{version} [put]
func (*Routes) upsertVersion(w http.ResponseWriter, r *http.Request) {
	registryName := chi.URLParam(r, "registryName")
	serverName := chi.URLParam(r, "serverName")
	version := chi.URLParam(r, "version")

	if strings.TrimSpace(registryName) == "" {
		common.WriteErrorResponse(w, "Registry name is required", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(serverName) == "" {
		common.WriteErrorResponse(w, "Server ID is required", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(version) == "" {
		common.WriteErrorResponse(w, "Version is required", http.StatusBadRequest)
		return
	}

	common.WriteErrorResponse(w, "Creating or updating servers is not supported", http.StatusNotImplemented)
}

// deleteVersion handles DELETE /extension/v0/registries/{registryName}/servers/{serverName}/versions/{version}
//
// @Summary		Delete server
// @Description	Delete a server from the registry
// @Tags		extension
// @Accept		json
// @Produce		json
// @Param		registryName	path	string	true	"Registry Name"
// @Param		serverName		path	string	true	"URL-encoded server name (e.g., \"com.example%2Fmy-server\")"
// @Param		version			path	string	true	"URL-encoded version to retrieve (e.g., \"1.0.0\")"
// @Failure		400	{object}	map[string]string	"Bad request"
// @Failure		501	{object}	map[string]string	"Not implemented"
// @Router		/extension/v0/registries/{registryName}/servers/{serverName}/versions/{version} [delete]
func (*Routes) deleteVersion(w http.ResponseWriter, r *http.Request) {
	registryName := chi.URLParam(r, "registryName")
	serverName := chi.URLParam(r, "serverName")
	version := chi.URLParam(r, "version")

	if strings.TrimSpace(registryName) == "" {
		common.WriteErrorResponse(w, "Registry name is required", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(serverName) == "" {
		common.WriteErrorResponse(w, "Server ID is required", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(version) == "" {
		common.WriteErrorResponse(w, "Version is required", http.StatusBadRequest)
		return
	}

	common.WriteErrorResponse(w, "Deleting servers is not supported", http.StatusNotImplemented)
}
