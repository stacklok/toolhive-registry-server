package v1

import (
	"net/http"

	"github.com/stacklok/toolhive-registry-server/internal/api/common"
)

// listRegistries handles GET /v1/registries
//
// @Summary		List registries
// @Description	List all registries
// @Tags		v1
// @Accept		json
// @Produce		json
// @Failure		501	{object}	map[string]string	"Not implemented"
// @Router		/v1/registries [get]
func (*Routes) listRegistries(w http.ResponseWriter, _ *http.Request) {
	common.WriteErrorResponse(w, "Listing registries is not yet implemented", http.StatusNotImplemented)
}

// getRegistry handles GET /v1/registries/{name}
//
// @Summary		Get registry
// @Description	Get a registry by name
// @Tags		v1
// @Accept		json
// @Produce		json
// @Param		name	path	string	true	"Registry Name"
// @Failure		400	{object}	map[string]string	"Bad request"
// @Failure		501	{object}	map[string]string	"Not implemented"
// @Router		/v1/registries/{name} [get]
func (*Routes) getRegistry(w http.ResponseWriter, r *http.Request) {
	if _, err := common.GetAndValidateURLParam(r, "name"); err != nil {
		common.WriteErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	common.WriteErrorResponse(w, "Getting registry is not yet implemented", http.StatusNotImplemented)
}

// upsertRegistry handles PUT /v1/registries/{name}
//
// @Summary		Create or update registry
// @Description	Create a new registry or update an existing one
// @Tags		v1
// @Accept		json
// @Produce		json
// @Param		name	path	string	true	"Registry Name"
// @Failure		400	{object}	map[string]string	"Bad request"
// @Failure		501	{object}	map[string]string	"Not implemented"
// @Router		/v1/registries/{name} [put]
func (*Routes) upsertRegistry(w http.ResponseWriter, r *http.Request) {
	if _, err := common.GetAndValidateURLParam(r, "name"); err != nil {
		common.WriteErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	common.WriteErrorResponse(w, "Creating or updating registry is not yet implemented", http.StatusNotImplemented)
}

// deleteRegistry handles DELETE /v1/registries/{name}
//
// @Summary		Delete registry
// @Description	Delete a registry by name
// @Tags		v1
// @Accept		json
// @Produce		json
// @Param		name	path	string	true	"Registry Name"
// @Failure		400	{object}	map[string]string	"Bad request"
// @Failure		501	{object}	map[string]string	"Not implemented"
// @Router		/v1/registries/{name} [delete]
func (*Routes) deleteRegistry(w http.ResponseWriter, r *http.Request) {
	if _, err := common.GetAndValidateURLParam(r, "name"); err != nil {
		common.WriteErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	common.WriteErrorResponse(w, "Deleting registry is not yet implemented", http.StatusNotImplemented)
}

// listRegistryEntries handles GET /v1/registries/{name}/entries
//
// @Summary		List registry entries
// @Description	List all entries for a registry
// @Tags		v1
// @Accept		json
// @Produce		json
// @Param		name	path	string	true	"Registry Name"
// @Failure		400	{object}	map[string]string	"Bad request"
// @Failure		501	{object}	map[string]string	"Not implemented"
// @Router		/v1/registries/{name}/entries [get]
func (*Routes) listRegistryEntries(w http.ResponseWriter, r *http.Request) {
	if _, err := common.GetAndValidateURLParam(r, "name"); err != nil {
		common.WriteErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	common.WriteErrorResponse(w, "Listing registry entries is not yet implemented", http.StatusNotImplemented)
}
