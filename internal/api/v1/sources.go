package v1

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/stacklok/toolhive-registry-server/internal/api/common"
	"github.com/stacklok/toolhive-registry-server/internal/service"
)

// listSources handles GET /v1/sources
//
// @Summary		List sources
// @Description	List all sources
// @Tags		v1
// @Accept		json
// @Produce		json
// @Success		200	{object}	service.SourceListResponse	"Sources list"
// @Failure		500	{object}	map[string]string			"Internal server error"
// @Router		/v1/sources [get]
func (routes *Routes) listSources(w http.ResponseWriter, r *http.Request) {
	sources, err := routes.service.ListSources(r.Context())
	if err != nil {
		slog.Error("failed to list sources", "error", err)
		common.WriteErrorResponse(w, "failed to list sources", http.StatusInternalServerError)
		return
	}

	common.WriteJSONResponse(w, service.SourceListResponse{Sources: sources}, http.StatusOK)
}

// getSource handles GET /v1/sources/{name}
//
// @Summary		Get source
// @Description	Get a source by name
// @Tags		v1
// @Accept		json
// @Produce		json
// @Param		name	path		string				true	"Source Name"
// @Success		200		{object}	service.SourceInfo	"Source details"
// @Failure		400		{object}	map[string]string	"Bad request"
// @Failure		404		{object}	map[string]string	"Source not found"
// @Failure		500		{object}	map[string]string	"Internal server error"
// @Router		/v1/sources/{name} [get]
func (routes *Routes) getSource(w http.ResponseWriter, r *http.Request) {
	name, err := common.GetAndValidateURLParam(r, "name")
	if err != nil {
		common.WriteErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	source, err := routes.service.GetSourceByName(r.Context(), name)
	if err != nil {
		writeSourceError(w, err)
		return
	}

	common.WriteJSONResponse(w, source, http.StatusOK)
}

// upsertSource handles PUT /v1/sources/{name}
//
// @Summary		Create or update source
// @Description	Create a new source or update an existing one
// @Tags		v1
// @Accept		json
// @Produce		json
// @Param		name	path		string				true	"Source Name"
// @Success		200		{object}	service.SourceInfo	"Source updated"
// @Success		201		{object}	service.SourceInfo	"Source created"
// @Failure		400		{object}	map[string]string	"Bad request"
// @Failure		403		{object}	map[string]string	"Cannot modify config-created source"
// @Failure		500		{object}	map[string]string	"Internal server error"
// @Router		/v1/sources/{name} [put]
func (routes *Routes) upsertSource(w http.ResponseWriter, r *http.Request) {
	name, err := common.GetAndValidateURLParam(r, "name")
	if err != nil {
		common.WriteErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	var req service.SourceCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteErrorResponse(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Try create first
	source, err := routes.service.CreateSource(r.Context(), name, &req)
	if err == nil {
		common.WriteJSONResponse(w, source, http.StatusCreated)
		return
	}

	// If it already exists, try update
	if errors.Is(err, service.ErrSourceAlreadyExists) {
		source, err = routes.service.UpdateSource(r.Context(), name, &req)
		if err != nil {
			writeSourceError(w, err)
			return
		}
		common.WriteJSONResponse(w, source, http.StatusOK)
		return
	}

	writeSourceError(w, err)
}

// deleteSource handles DELETE /v1/sources/{name}
//
// @Summary		Delete source
// @Description	Delete a source by name
// @Tags		v1
// @Accept		json
// @Produce		json
// @Param		name	path	string	true	"Source Name"
// @Success		204	"Source deleted"
// @Failure		400	{object}	map[string]string	"Bad request"
// @Failure		403	{object}	map[string]string	"Cannot modify config-created source"
// @Failure		404	{object}	map[string]string	"Source not found"
// @Failure		409	{object}	map[string]string	"Source in use"
// @Failure		500	{object}	map[string]string	"Internal server error"
// @Router		/v1/sources/{name} [delete]
func (routes *Routes) deleteSource(w http.ResponseWriter, r *http.Request) {
	name, err := common.GetAndValidateURLParam(r, "name")
	if err != nil {
		common.WriteErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := routes.service.DeleteSource(r.Context(), name); err != nil {
		writeSourceError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// listSourceEntries handles GET /v1/sources/{name}/entries
//
// @Summary		List source entries
// @Description	List all entries for a source
// @Tags		v1
// @Accept		json
// @Produce		json
// @Param		name	path		string						true	"Source Name"
// @Success		200		{object}	service.SourceEntriesResponse	"Source entries"
// @Failure		400		{object}	map[string]string				"Bad request"
// @Failure		404		{object}	map[string]string				"Source not found"
// @Failure		500		{object}	map[string]string				"Internal server error"
// @Router		/v1/sources/{name}/entries [get]
func (routes *Routes) listSourceEntries(w http.ResponseWriter, r *http.Request) {
	name, err := common.GetAndValidateURLParam(r, "name")
	if err != nil {
		common.WriteErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	entries, err := routes.service.ListSourceEntries(r.Context(), name)
	if err != nil {
		writeSourceError(w, err)
		return
	}

	common.WriteJSONResponse(w, service.SourceEntriesResponse{Entries: entries}, http.StatusOK)
}

// writeSourceError maps service-layer source errors to HTTP responses.
func writeSourceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrSourceNotFound):
		common.WriteErrorResponse(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, service.ErrConfigSource):
		common.WriteErrorResponse(w, err.Error(), http.StatusForbidden)
	case errors.Is(err, service.ErrInvalidSourceConfig):
		common.WriteErrorResponse(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, service.ErrSourceTypeChangeNotAllowed):
		common.WriteErrorResponse(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, service.ErrSourceInUse):
		common.WriteErrorResponse(w, err.Error(), http.StatusConflict)
	default:
		slog.Error("unexpected source error", "error", err)
		common.WriteErrorResponse(w, "internal server error", http.StatusInternalServerError)
	}
}
