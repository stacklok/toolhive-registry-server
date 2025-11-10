package v0

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/stacklok/toolhive-registry-server/internal/api/common"
	"github.com/stacklok/toolhive-registry-server/internal/service"
)

type Routes struct {
	service service.RegistryService
}

func NewRoutes(svc service.RegistryService) *Routes {
	return &Routes{
		service: svc,
	}
}

func Router(svc service.RegistryService) http.Handler {
	routes := NewRoutes(svc)

	r := chi.NewRouter()

	r.Put("/servers/{id}", routes.updateServer)
	r.Delete("/servers/{id}", routes.deleteServer)

	return r
}

// updateServer handles PUT /extension/v0/servers/{id}
//
// @Summary		Update server
// @Description	Update a server in the registry
// @Tags		extension
// @Accept		json
// @Produce		json
// @Param		id	path		string	true	"Server ID"
// @Failure		400	{object}	map[string]string	"Bad request"
// @Failure		501	{object}	map[string]string	"Not implemented"
// @Router		/extension/v0/servers/{id} [put]
func (rr *Routes) updateServer(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if strings.TrimSpace(id) == "" {
		common.WriteErrorResponse(w, "Server ID is required", http.StatusBadRequest)
		return
	}

	common.WriteErrorResponse(w, "Updating servers is not supported", http.StatusNotImplemented)
}

// deleteServer handles DELETE /extension/v0/servers/{id}
//
// @Summary		Delete server
// @Description	Delete a server from the registry
// @Tags		extension
// @Accept		json
// @Produce		json
// @Param		id	path		string	true	"Server ID"
// @Failure		400	{object}	map[string]string	"Bad request"
// @Failure		501	{object}	map[string]string	"Not implemented"
// @Router		/extension/v0/servers/{id} [delete]
func (rr *Routes) deleteServer(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if strings.TrimSpace(id) == "" {
		common.WriteErrorResponse(w, "Server ID is required", http.StatusBadRequest)
		return
	}

	common.WriteErrorResponse(w, "Deleting servers is not supported", http.StatusNotImplemented)
}
