package v1

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"

	"github.com/stacklok/toolhive-registry-server/internal/api/common"
	"github.com/stacklok/toolhive-registry-server/internal/auth"
	"github.com/stacklok/toolhive-registry-server/internal/service"
)

// publishEntryRequest is the request body for publishing an entry.
// Exactly one of Server or Skill must be provided.
type publishEntryRequest struct {
	Claims map[string]any         `json:"claims,omitempty"`
	Server *upstreamv0.ServerJSON `json:"server,omitempty"`
	Skill  *service.Skill         `json:"skill,omitempty"`
}

// publishEntry handles POST /v1/entries
//
// @Summary		Publish entry
// @Description	Publish a new server or skill entry. Exactly one of 'server' or 'skill' must be provided.
// @Tags		v1
// @Accept		json
// @Produce		json
// @Param		request	body		publishEntryRequest	true	"Entry to publish (server or skill)"
// @Success		201	{object}	interface{}	"Published entry (server or skill)"
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

	hasServer := req.Server != nil
	hasSkill := req.Skill != nil
	if hasServer == hasSkill {
		common.WriteErrorResponse(w, "exactly one of 'server' or 'skill' must be provided", http.StatusBadRequest)
		return
	}

	if routes.authzEnabled && len(req.Claims) == 0 {
		common.WriteErrorResponse(w, "claims are required when authorization is enabled", http.StatusBadRequest)
		return
	}

	// Extract JWT claims for authorization (subset validation)
	var jwtOpts []service.Option
	if jwtClaims := auth.ClaimsFromContext(r.Context()); jwtClaims != nil {
		jwtOpts = append(jwtOpts, service.WithJWTClaims(map[string]any(jwtClaims)))
	}

	if req.Server != nil {
		opts := append([]service.Option{service.WithServerData(req.Server)}, jwtOpts...)
		if req.Claims != nil {
			opts = append(opts, service.WithClaims(req.Claims))
		}
		published, err := routes.service.PublishServerVersion(r.Context(), opts...)
		if err != nil {
			writePublishError(w, r, err)
			return
		}
		common.WriteJSONResponse(w, published, http.StatusCreated)
		return
	}

	if req.Skill != nil {
		opts := append([]service.Option{}, jwtOpts...)
		if req.Claims != nil {
			opts = append(opts, service.WithClaims(req.Claims))
		}
		published, err := routes.service.PublishSkill(r.Context(), req.Skill, opts...)
		if err != nil {
			writePublishError(w, r, err)
			return
		}
		common.WriteJSONResponse(w, published, http.StatusCreated)
		return
	}
}

// writePublishError maps service-layer publish errors to HTTP responses.
func writePublishError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, service.ErrInvalidServerName) {
		common.WriteErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}
	if errors.Is(err, service.ErrClaimsInsufficient) {
		common.WriteErrorResponse(w, err.Error(), http.StatusForbidden)
		return
	}
	if errors.Is(err, service.ErrVersionAlreadyExists) {
		common.WriteErrorResponse(w, err.Error(), http.StatusConflict)
		return
	}
	if errors.Is(err, service.ErrClaimsMismatch) {
		common.WriteErrorResponse(w, err.Error(), http.StatusConflict)
		return
	}
	if errors.Is(err, service.ErrNoManagedSource) {
		common.WriteErrorResponse(w, "no managed source available for publishing", http.StatusInternalServerError)
		return
	}
	slog.ErrorContext(r.Context(), "failed to publish entry", "error", err)
	common.WriteErrorResponse(w, "failed to publish entry", http.StatusInternalServerError)
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

	// Build options with JWT claims for authorization
	opts := []service.Option{service.WithName(name), service.WithVersion(version)}
	if jwtClaims := auth.ClaimsFromContext(r.Context()); jwtClaims != nil {
		opts = append(opts, service.WithJWTClaims(map[string]any(jwtClaims)))
	}

	switch entryType {
	case "server":
		err = routes.service.DeleteServerVersion(r.Context(), opts...)
	case "skill":
		err = routes.service.DeleteSkillVersion(r.Context(), opts...)
	default:
		common.WriteErrorResponse(w, "unsupported entry type: must be 'server' or 'skill'", http.StatusBadRequest)
		return
	}

	if err != nil {
		if errors.Is(err, service.ErrClaimsInsufficient) {
			common.WriteErrorResponse(w, err.Error(), http.StatusForbidden)
			return
		}
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

// updateEntryClaimsRequest is the request body for updating entry claims.
type updateEntryClaimsRequest struct {
	Claims map[string]any `json:"claims"`
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
// @Param		request	body	updateEntryClaimsRequest	true	"Claims to set"
// @Success		204	"No Content"
// @Failure		400	{object}	map[string]string	"Bad request"
// @Failure		403	{object}	map[string]string	"Forbidden"
// @Failure		404	{object}	map[string]string	"Not found"
// @Failure		500	{object}	map[string]string	"Internal server error"
// @Failure		503	{object}	map[string]string	"No managed source available"
// @Router		/v1/entries/{type}/{name}/claims [put]
func (routes *Routes) updateEntryClaims(w http.ResponseWriter, r *http.Request) {
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

	var req updateEntryClaimsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteErrorResponse(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	opts := []service.Option{
		service.WithEntryType(entryType),
		service.WithName(name),
	}
	if req.Claims != nil {
		opts = append(opts, service.WithClaims(req.Claims))
	}
	if jwtClaims := auth.ClaimsFromContext(r.Context()); jwtClaims != nil {
		opts = append(opts, service.WithJWTClaims(map[string]any(jwtClaims)))
	}

	if err := routes.service.UpdateEntryClaims(r.Context(), opts...); err != nil {
		if errors.Is(err, service.ErrInvalidEntryType) {
			common.WriteErrorResponse(w, err.Error(), http.StatusBadRequest)
			return
		}
		if errors.Is(err, service.ErrClaimsInsufficient) {
			common.WriteErrorResponse(w, err.Error(), http.StatusForbidden)
			return
		}
		if errors.Is(err, service.ErrNotFound) {
			common.WriteErrorResponse(w, err.Error(), http.StatusNotFound)
			return
		}
		if errors.Is(err, service.ErrNoManagedSource) {
			common.WriteErrorResponse(w, "no managed source available for updating claims", http.StatusServiceUnavailable)
			return
		}
		slog.ErrorContext(r.Context(), "failed to update entry claims", "error", err, "type", entryType)
		common.WriteErrorResponse(w, "failed to update entry claims", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
