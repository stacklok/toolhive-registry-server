package v1

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/stacklok/toolhive-registry-server/internal/api/common"
	"github.com/stacklok/toolhive-registry-server/internal/service"
)

// registryListResponse is the JSON envelope for listing registries.
type registryListResponse struct {
	Registries []service.RegistryInfo `json:"registries"`
}

// listRegistries handles GET /v1/registries
//
// @Summary		List registries
// @Description	List all registries
// @Tags		v1
// @Accept		json
// @Produce		json
// @Success		200	{object}	registryListResponse	"Registries list"
// @Failure		500	{object}	map[string]string		"Internal server error"
// @Router		/v1/registries [get]
func (routes *Routes) listRegistries(w http.ResponseWriter, r *http.Request) {
	registries, err := routes.service.ListRegistries(r.Context())
	if err != nil {
		slog.Error("failed to list registries", "error", err)
		common.WriteErrorResponse(w, "failed to list registries", http.StatusInternalServerError)
		return
	}

	common.WriteJSONResponse(w, registryListResponse{Registries: registries}, http.StatusOK)
}

// getRegistry handles GET /v1/registries/{name}
//
// @Summary		Get registry
// @Description	Get a registry by name
// @Tags		v1
// @Accept		json
// @Produce		json
// @Param		name	path		string					true	"Registry Name"
// @Success		200		{object}	service.RegistryInfo	"Registry details"
// @Failure		400		{object}	map[string]string		"Bad request"
// @Failure		404		{object}	map[string]string		"Registry not found"
// @Failure		500		{object}	map[string]string		"Internal server error"
// @Router		/v1/registries/{name} [get]
func (routes *Routes) getRegistry(w http.ResponseWriter, r *http.Request) {
	name, err := common.GetAndValidateURLParam(r, "name")
	if err != nil {
		common.WriteErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	registry, err := routes.service.GetRegistryByName(r.Context(), name)
	if err != nil {
		writeRegistryError(w, err)
		return
	}

	common.WriteJSONResponse(w, registry, http.StatusOK)
}

// upsertRegistry handles PUT /v1/registries/{name}
//
// @Summary		Create or update registry
// @Description	Create a new registry or update an existing one
// @Tags		v1
// @Accept		json
// @Produce		json
// @Param		name	path		string					true	"Registry Name"
// @Success		200		{object}	service.RegistryInfo	"Registry updated"
// @Success		201		{object}	service.RegistryInfo	"Registry created"
// @Failure		400		{object}	map[string]string		"Bad request"
// @Failure		403		{object}	map[string]string		"Cannot modify config-created registry"
// @Failure		500		{object}	map[string]string		"Internal server error"
// @Router		/v1/registries/{name} [put]
func (routes *Routes) upsertRegistry(w http.ResponseWriter, r *http.Request) {
	name, err := common.GetAndValidateURLParam(r, "name")
	if err != nil {
		common.WriteErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	var req service.RegistryCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteErrorResponse(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Try create first
	registry, err := routes.service.CreateRegistry(r.Context(), name, &req)
	if err == nil {
		common.WriteJSONResponse(w, registry, http.StatusCreated)
		return
	}

	// If it already exists, try update
	if errors.Is(err, service.ErrRegistryAlreadyExists) {
		registry, err = routes.service.UpdateRegistry(r.Context(), name, &req)
		if err != nil {
			writeRegistryError(w, err)
			return
		}
		common.WriteJSONResponse(w, registry, http.StatusOK)
		return
	}

	writeRegistryError(w, err)
}

// deleteRegistry handles DELETE /v1/registries/{name}
//
// @Summary		Delete registry
// @Description	Delete a registry by name
// @Tags		v1
// @Accept		json
// @Produce		json
// @Param		name	path	string	true	"Registry Name"
// @Success		204	"Registry deleted"
// @Failure		400	{object}	map[string]string	"Bad request"
// @Failure		403	{object}	map[string]string	"Cannot modify config-created registry"
// @Failure		404	{object}	map[string]string	"Registry not found"
// @Failure		500	{object}	map[string]string	"Internal server error"
// @Router		/v1/registries/{name} [delete]
func (routes *Routes) deleteRegistry(w http.ResponseWriter, r *http.Request) {
	name, err := common.GetAndValidateURLParam(r, "name")
	if err != nil {
		common.WriteErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := routes.service.DeleteRegistry(r.Context(), name); err != nil {
		writeRegistryError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// listRegistryEntries handles GET /v1/registries/{name}/entries
//
// @Summary		List registry entries
// @Description	List all entries for a registry
// @Tags		v1
// @Accept		json
// @Produce		json
// @Param		name	path		string							true	"Registry Name"
// @Success		200		{object}	service.RegistryEntriesResponse	"Registry entries"
// @Failure		400		{object}	map[string]string				"Bad request"
// @Failure		404		{object}	map[string]string				"Registry not found"
// @Failure		500		{object}	map[string]string				"Internal server error"
// @Router		/v1/registries/{name}/entries [get]
func (routes *Routes) listRegistryEntries(w http.ResponseWriter, r *http.Request) {
	name, err := common.GetAndValidateURLParam(r, "name")
	if err != nil {
		common.WriteErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	entries, err := routes.service.ListRegistryEntries(r.Context(), name)
	if err != nil {
		writeRegistryError(w, err)
		return
	}

	common.WriteJSONResponse(w, service.RegistryEntriesResponse{Entries: entries}, http.StatusOK)
}

// writeRegistryError maps service-layer registry errors to HTTP responses.
func writeRegistryError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrRegistryNotFound):
		common.WriteErrorResponse(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, service.ErrConfigRegistry):
		common.WriteErrorResponse(w, err.Error(), http.StatusForbidden)
	case errors.Is(err, service.ErrInvalidRegistryConfig):
		common.WriteErrorResponse(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, service.ErrRegistryAlreadyExists):
		common.WriteErrorResponse(w, err.Error(), http.StatusConflict)
	case errors.Is(err, service.ErrSourceNotFound):
		common.WriteErrorResponse(w, err.Error(), http.StatusBadRequest)
	default:
		slog.Error("unexpected registry error", "error", err)
		common.WriteErrorResponse(w, "internal server error", http.StatusInternalServerError)
	}
}
