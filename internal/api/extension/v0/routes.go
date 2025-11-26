// Package v0 provides extension API v0 endpoints for server management.
package v0

import (
	"errors"
	"fmt"
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

	r.Route("/registries/{registryName}/servers/{serverName}", func(r chi.Router) {
		r.Put("/versions/{version}", routes.upsertVersion)
	})

	return r
}

// listRegistries handles GET /extension/v0/registries
//
// @Summary		List registries
// @Description	List all registries
// @Tags		extension
// @Accept		json
// @Produce		json
// @Success		200	{object}	service.RegistryListResponse	"List of registries"
// @Failure		401	{object}	map[string]string	"Unauthorized"
// @Failure		500	{object}	map[string]string	"Internal server error"
// @Security	BearerAuth
// @Router		/extension/v0/registries [get]
func (r *Routes) listRegistries(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()

	registries, err := r.service.ListRegistries(ctx)
	if err != nil {
		if err == service.ErrNotImplemented {
			common.WriteErrorResponse(w, "Listing registries is not supported in file mode", http.StatusNotImplemented)
			return
		}
		common.WriteErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	response := service.RegistryListResponse{
		Registries: registries,
	}

	common.WriteJSONResponse(w, response, http.StatusOK)
}

// getRegistry handles GET /extension/v0/registries/{registryName}
//
// @Summary		Get registry
// @Description	Get a registry by name
// @Tags		extension
// @Accept		json
// @Produce		json
// @Param		registryName	path	string	true	"Registry Name"
// @Success		200	{object}	service.RegistryInfo	"Registry details"
// @Failure		400	{object}	map[string]string	"Bad request"
// @Failure		401	{object}	map[string]string	"Unauthorized"
// @Failure		404	{object}	map[string]string	"Registry not found"
// @Failure		500	{object}	map[string]string	"Internal server error"
// @Failure		501	{object}	map[string]string	"Not implemented"
// @Security	BearerAuth
// @Router		/extension/v0/registries/{registryName} [get]
func (r *Routes) getRegistry(w http.ResponseWriter, req *http.Request) {
	registryName := chi.URLParam(req, "registryName")
	if strings.TrimSpace(registryName) == "" {
		common.WriteErrorResponse(w, "Registry name is required", http.StatusBadRequest)
		return
	}

	ctx := req.Context()
	registry, err := r.service.GetRegistryByName(ctx, registryName)
	if err != nil {
		if errors.Is(err, service.ErrNotImplemented) {
			common.WriteErrorResponse(w, "Getting registry is not supported in file mode", http.StatusNotImplemented)
			return
		}
		if errors.Is(err, service.ErrRegistryNotFound) {
			common.WriteErrorResponse(w, fmt.Sprintf("Registry %s not found", registryName), http.StatusNotFound)
			return
		}
		common.WriteErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	common.WriteJSONResponse(w, registry, http.StatusOK)
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
// @Failure		401	{object}	map[string]string	"Unauthorized"
// @Failure		501	{object}	map[string]string	"Not implemented"
// @Security	BearerAuth
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
// @Failure		401	{object}	map[string]string	"Unauthorized"
// @Failure		501	{object}	map[string]string	"Not implemented"
// @Security	BearerAuth
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
// @Failure		401	{object}	map[string]string	"Unauthorized"
// @Failure		501	{object}	map[string]string	"Not implemented"
// @Security	BearerAuth
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
