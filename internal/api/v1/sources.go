package v1

import (
	"net/http"

	"github.com/stacklok/toolhive-registry-server/internal/api/common"
)

// listSources handles GET /v1/sources
//
// @Summary		List sources
// @Description	List all sources
// @Tags		v1
// @Accept		json
// @Produce		json
// @Failure		501	{object}	map[string]string	"Not implemented"
// @Router		/v1/sources [get]
func (*Routes) listSources(w http.ResponseWriter, _ *http.Request) {
	common.WriteErrorResponse(w, "Listing sources is not yet implemented", http.StatusNotImplemented)
}

// getSource handles GET /v1/sources/{name}
//
// @Summary		Get source
// @Description	Get a source by name
// @Tags		v1
// @Accept		json
// @Produce		json
// @Param		name	path	string	true	"Source Name"
// @Failure		400	{object}	map[string]string	"Bad request"
// @Failure		501	{object}	map[string]string	"Not implemented"
// @Router		/v1/sources/{name} [get]
func (*Routes) getSource(w http.ResponseWriter, r *http.Request) {
	if _, err := common.GetAndValidateURLParam(r, "name"); err != nil {
		common.WriteErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	common.WriteErrorResponse(w, "Getting source is not yet implemented", http.StatusNotImplemented)
}

// upsertSource handles PUT /v1/sources/{name}
//
// @Summary		Create or update source
// @Description	Create a new source or update an existing one
// @Tags		v1
// @Accept		json
// @Produce		json
// @Param		name	path	string	true	"Source Name"
// @Failure		400	{object}	map[string]string	"Bad request"
// @Failure		501	{object}	map[string]string	"Not implemented"
// @Router		/v1/sources/{name} [put]
func (*Routes) upsertSource(w http.ResponseWriter, r *http.Request) {
	if _, err := common.GetAndValidateURLParam(r, "name"); err != nil {
		common.WriteErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	common.WriteErrorResponse(w, "Creating or updating source is not yet implemented", http.StatusNotImplemented)
}

// deleteSource handles DELETE /v1/sources/{name}
//
// @Summary		Delete source
// @Description	Delete a source by name
// @Tags		v1
// @Accept		json
// @Produce		json
// @Param		name	path	string	true	"Source Name"
// @Failure		400	{object}	map[string]string	"Bad request"
// @Failure		501	{object}	map[string]string	"Not implemented"
// @Router		/v1/sources/{name} [delete]
func (*Routes) deleteSource(w http.ResponseWriter, r *http.Request) {
	if _, err := common.GetAndValidateURLParam(r, "name"); err != nil {
		common.WriteErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	common.WriteErrorResponse(w, "Deleting source is not yet implemented", http.StatusNotImplemented)
}

// listSourceEntries handles GET /v1/sources/{name}/entries
//
// @Summary		List source entries
// @Description	List all entries for a source
// @Tags		v1
// @Accept		json
// @Produce		json
// @Param		name	path	string	true	"Source Name"
// @Failure		400	{object}	map[string]string	"Bad request"
// @Failure		501	{object}	map[string]string	"Not implemented"
// @Router		/v1/sources/{name}/entries [get]
func (*Routes) listSourceEntries(w http.ResponseWriter, r *http.Request) {
	if _, err := common.GetAndValidateURLParam(r, "name"); err != nil {
		common.WriteErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	common.WriteErrorResponse(w, "Listing source entries is not yet implemented", http.StatusNotImplemented)
}
