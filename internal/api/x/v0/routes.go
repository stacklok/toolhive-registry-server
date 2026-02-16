// Package v0 provides extension API v0 endpoints for server management.
package v0

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

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
	registryName, err := common.GetAndValidateURLParam(req, "registryName")
	if err != nil {
		common.WriteErrorResponse(w, err.Error(), http.StatusBadRequest)
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
// @Description	Create a new registry or update an existing one. Only registries created via API can be updated.
// @Tags		extension
// @Accept		json
// @Produce		json
// @Param		registryName	path	string						true	"Registry Name"
// @Param		body			body	service.RegistryCreateRequest	true	"Registry configuration"
// @Success		200	{object}	service.RegistryInfo	"Registry updated"
// @Success		201	{object}	service.RegistryInfo	"Registry created"
// @Failure		400	{object}	map[string]string	"Bad request - invalid configuration"
// @Failure		401	{object}	map[string]string	"Unauthorized"
// @Failure		403	{object}	map[string]string	"Forbidden - cannot modify CONFIG registry"
// @Failure		500	{object}	map[string]string	"Internal server error"
// @Failure		501	{object}	map[string]string	"Not implemented"
// @Security	BearerAuth
// @Router		/extension/v0/registries/{registryName} [put]
func (rt *Routes) upsertRegistry(w http.ResponseWriter, r *http.Request) {
	registryName, err := common.GetAndValidateURLParam(r, "registryName")
	if err != nil {
		common.WriteErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	if r.Body == nil {
		common.WriteErrorResponse(w, "Request body is required", http.StatusBadRequest)
		return
	}

	var config service.RegistryCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
		common.WriteErrorResponse(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	isCreate := false

	// Try to update first - if registry doesn't exist, create it
	registry, err := rt.service.UpdateRegistry(ctx, registryName, &config)
	if err != nil {
		if errors.Is(err, service.ErrRegistryNotFound) {
			// Registry doesn't exist, create it
			registry, err = rt.service.CreateRegistry(ctx, registryName, &config)
			if err != nil {
				rt.handleRegistryError(w, err, registryName)
				return
			}
			isCreate = true
		} else {
			rt.handleRegistryError(w, err, registryName)
			return
		}
	}

	// For inline data registries, process the data synchronously
	if config.IsInlineData() {
		format := config.Format
		if format == "" {
			format = "upstream"
		}
		if err := rt.service.ProcessInlineRegistryData(ctx, registryName, config.File.Data, format); err != nil {
			// Processing failed - return error to client
			// The registry was created but data processing failed
			common.WriteErrorResponse(w, fmt.Sprintf("Failed to process inline data: %v", err), http.StatusBadRequest)
			return
		}

		// Re-fetch registry to get updated sync status
		registry, err = rt.service.GetRegistryByName(ctx, registryName)
		if err != nil {
			common.WriteErrorResponse(w, fmt.Sprintf("Failed to fetch registry after processing: %v", err), http.StatusInternalServerError)
			return
		}
	}

	if isCreate {
		common.WriteJSONResponse(w, registry, http.StatusCreated)
	} else {
		common.WriteJSONResponse(w, registry, http.StatusOK)
	}
}

// deleteRegistry handles DELETE /extension/v0/registries/{registryName}
//
// @Summary		Delete registry
// @Description	Delete a registry by name. Only registries created via API can be deleted.
// @Tags		extension
// @Accept		json
// @Produce		json
// @Param		registryName	path	string	true	"Registry Name"
// @Success		204	"Registry deleted"
// @Failure		400	{object}	map[string]string	"Bad request"
// @Failure		401	{object}	map[string]string	"Unauthorized"
// @Failure		403	{object}	map[string]string	"Forbidden - cannot delete CONFIG registry"
// @Failure		404	{object}	map[string]string	"Registry not found"
// @Failure		500	{object}	map[string]string	"Internal server error"
// @Failure		501	{object}	map[string]string	"Not implemented"
// @Security	BearerAuth
// @Router		/extension/v0/registries/{registryName} [delete]
func (rt *Routes) deleteRegistry(w http.ResponseWriter, r *http.Request) {
	registryName, err := common.GetAndValidateURLParam(r, "registryName")
	if err != nil {
		common.WriteErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	if err := rt.service.DeleteRegistry(ctx, registryName); err != nil {
		rt.handleRegistryError(w, err, registryName)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleRegistryError handles common registry errors and writes appropriate responses
func (*Routes) handleRegistryError(w http.ResponseWriter, err error, registryName string) {
	switch {
	case errors.Is(err, service.ErrNotImplemented):
		common.WriteErrorResponse(w, "Registry operations are not supported in file mode", http.StatusNotImplemented)
	case errors.Is(err, service.ErrRegistryNotFound):
		common.WriteErrorResponse(w, fmt.Sprintf("Registry %s not found", registryName), http.StatusNotFound)
	case errors.Is(err, service.ErrConfigRegistry):
		common.WriteErrorResponse(w, "Cannot modify registry created via config file", http.StatusForbidden)
	case errors.Is(err, service.ErrInvalidRegistryConfig):
		common.WriteErrorResponse(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, service.ErrRegistryAlreadyExists):
		common.WriteErrorResponse(w, fmt.Sprintf("Registry %s already exists", registryName), http.StatusConflict)
	default:
		common.WriteErrorResponse(w, err.Error(), http.StatusInternalServerError)
	}
}

// upsertVersion handles PUT /extension/v0/registries/{registryName}/servers/{serverName}/versions/{version}
// This endpoint is not implemented and not included in OpenAPI spec
func (*Routes) upsertVersion(w http.ResponseWriter, r *http.Request) {
	_, err := common.GetAndValidateURLParam(r, "registryName")
	if err != nil {
		common.WriteErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	_, err = common.GetAndValidateURLParam(r, "serverName")
	if err != nil {
		common.WriteErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	_, err = common.GetAndValidateURLParam(r, "version")
	if err != nil {
		common.WriteErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	common.WriteErrorResponse(w, "Creating or updating servers is not supported", http.StatusNotImplemented)
}
