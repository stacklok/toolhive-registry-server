package v1

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"

	"github.com/stacklok/toolhive-registry-server/internal/api/common"
	"github.com/stacklok/toolhive-registry-server/internal/service"
)

// publishEntryRequest is the request body for publishing a server entry.
type publishEntryRequest struct {
	Server *upstreamv0.ServerJSON `json:"server,omitempty"`
}

// publishEntry handles POST /v1/entries
//
// @Summary		Publish entry
// @Description	Publish a new server entry.
// @Tags		v1
// @Accept		json
// @Produce		json
// @Param		request	body		publishEntryRequest	true	"Entry to publish (server)"
// @Success		201	{object}	interface{}	"Published entry"
// @Failure		400	{object}	map[string]string	"Bad request"
// @Failure		409	{object}	map[string]string	"Conflict"
// @Failure		500	{object}	map[string]string	"Internal server error"
// @Router		/v1/entries [post]
func (routes *Routes) publishEntry(w http.ResponseWriter, r *http.Request) {
	var req publishEntryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteErrorResponse(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.Server == nil {
		common.WriteErrorResponse(w, "server field is required", http.StatusBadRequest)
		return
	}

	published, err := routes.service.PublishServerVersion(r.Context(),
		service.WithServerData(req.Server),
	)
	if err != nil {
		if errors.Is(err, service.ErrVersionAlreadyExists) {
			common.WriteErrorResponse(w, err.Error(), http.StatusConflict)
			return
		}
		if errors.Is(err, service.ErrNoManagedSource) {
			common.WriteErrorResponse(w, "no managed source available for publishing", http.StatusInternalServerError)
			return
		}
		slog.ErrorContext(r.Context(), "failed to publish entry", "error", err)
		common.WriteErrorResponse(w, "failed to publish entry", http.StatusInternalServerError)
		return
	}
	common.WriteJSONResponse(w, published, http.StatusCreated)
}

// deletePublishedEntry handles DELETE /v1/entries/{type}/{name}/versions/{version}
//
// @Summary		Delete published entry
// @Description	Delete a published entry version
// @Tags		v1
// @Accept		json
// @Produce		json
// @Param		type	path	string	true	"Entry Type (server or skill)"
// @Param		name	path	string	true	"Entry Name"
// @Param		version	path	string	true	"Version"
// @Success		204	"No Content"
// @Failure		400	{object}	map[string]string	"Bad request"
// @Failure		404	{object}	map[string]string	"Not found"
// @Failure		500	{object}	map[string]string	"Internal server error"
// @Router		/v1/entries/{type}/{name}/versions/{version} [delete]
func (routes *Routes) deletePublishedEntry(w http.ResponseWriter, r *http.Request) {
	entryType, err := common.GetAndValidateURLParam(r, "type")
	if err != nil {
		common.WriteErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	name, err := common.GetAndValidateURLParam(r, "name")
	if err != nil {
		common.WriteErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	version, err := common.GetAndValidateURLParam(r, "version")
	if err != nil {
		common.WriteErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	switch entryType {
	case "server":
		err = routes.service.DeleteServerVersion(r.Context(), service.WithName(name), service.WithVersion(version))
	case "skill":
		err = routes.service.DeleteSkillVersion(r.Context(), service.WithName(name), service.WithVersion(version))
	default:
		common.WriteErrorResponse(w, "unsupported entry type: must be 'server' or 'skill'", http.StatusBadRequest)
		return
	}

	if err != nil {
		if errors.Is(err, service.ErrNotFound) {
			common.WriteErrorResponse(w, err.Error(), http.StatusNotFound)
			return
		}
		if errors.Is(err, service.ErrNoManagedSource) {
			common.WriteErrorResponse(w, "no managed source available for deletion", http.StatusInternalServerError)
			return
		}
		slog.ErrorContext(r.Context(), "failed to delete entry", "error", err, "type", entryType)
		common.WriteErrorResponse(w, "failed to delete entry", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// updateEntryClaims handles PUT /v1/entries/{type}/{name}/claims
//
// @Summary		Update entry claims
// @Description	Update claims for a published entry name
// @Tags		v1
// @Accept		json
// @Produce		json
// @Param		type	path	string	true	"Entry Type (server or skill)"
// @Param		name	path	string	true	"Entry Name"
// @Failure		400	{object}	map[string]string	"Bad request"
// @Failure		501	{object}	map[string]string	"Not implemented"
// @Router		/v1/entries/{type}/{name}/claims [put]
func (*Routes) updateEntryClaims(w http.ResponseWriter, r *http.Request) {
	if _, err := common.GetAndValidateURLParam(r, "type"); err != nil {
		common.WriteErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	if _, err := common.GetAndValidateURLParam(r, "name"); err != nil {
		common.WriteErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	common.WriteErrorResponse(w, "Updating entry claims is not yet implemented", http.StatusNotImplemented)
}
