package v1

import (
	"net/http"

	"github.com/stacklok/toolhive-registry-server/internal/api/common"
)

// publishEntry handles POST /v1/publish
//
// @Summary		Publish entry
// @Description	Publish a new entry
// @Tags		v1
// @Accept		json
// @Produce		json
// @Failure		501	{object}	map[string]string	"Not implemented"
// @Router		/v1/publish [post]
func (*Routes) publishEntry(w http.ResponseWriter, _ *http.Request) {
	common.WriteErrorResponse(w, "Publishing entry is not yet implemented", http.StatusNotImplemented)
}

// deletePublishedEntry handles DELETE /v1/publish/{name}/versions/{version}
//
// @Summary		Delete published entry
// @Description	Delete a published entry version
// @Tags		v1
// @Accept		json
// @Produce		json
// @Param		name	path	string	true	"Entry Name"
// @Param		version	path	string	true	"Version"
// @Failure		400	{object}	map[string]string	"Bad request"
// @Failure		501	{object}	map[string]string	"Not implemented"
// @Router		/v1/publish/{name}/versions/{version} [delete]
func (*Routes) deletePublishedEntry(w http.ResponseWriter, r *http.Request) {
	if _, err := common.GetAndValidateURLParam(r, "name"); err != nil {
		common.WriteErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	if _, err := common.GetAndValidateURLParam(r, "version"); err != nil {
		common.WriteErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	common.WriteErrorResponse(w, "Deleting published entry is not yet implemented", http.StatusNotImplemented)
}

// updateEntryClaims handles PUT /v1/publish/{name}/versions/{version}/claims
//
// @Summary		Update entry claims
// @Description	Update claims for a published entry version
// @Tags		v1
// @Accept		json
// @Produce		json
// @Param		name	path	string	true	"Entry Name"
// @Param		version	path	string	true	"Version"
// @Failure		400	{object}	map[string]string	"Bad request"
// @Failure		501	{object}	map[string]string	"Not implemented"
// @Router		/v1/publish/{name}/versions/{version}/claims [put]
func (*Routes) updateEntryClaims(w http.ResponseWriter, r *http.Request) {
	if _, err := common.GetAndValidateURLParam(r, "name"); err != nil {
		common.WriteErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	if _, err := common.GetAndValidateURLParam(r, "version"); err != nil {
		common.WriteErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	common.WriteErrorResponse(w, "Updating entry claims is not yet implemented", http.StatusNotImplemented)
}
