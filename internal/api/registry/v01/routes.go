package v01

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/stacklok/toolhive-registry-server/internal/api/common"
	"github.com/stacklok/toolhive-registry-server/internal/service"
)

//go:generate go tool oapi-codegen -package v01 -generate types -o types.go openapi.yml

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

	r.Get("/servers", routes.listServers)
	r.Route("/servers/{serverName}", func(r chi.Router) {
		r.Get("/versions", routes.listVersions)
		r.Get("/versions/{version}", routes.getVersion)
	})
	r.Post("/publish", routes.publish)

	return r
}

// listServers handles GET /registry/v0.1/servers
//
// @Summary		List servers
// @Description	Get a list of available servers in the registry
// @Tags		registry,official
// @Accept		json
// @Produce		json
// @Failure		501	{object}	map[string]string	"Not implemented"
// @Router		/registry/v0.1/servers [get]
func (rr *Routes) listServers(w http.ResponseWriter, r *http.Request) {
	common.WriteErrorResponse(w, "Listing servers is not supported", http.StatusNotImplemented)
}

// listVersions handles GET /registry/v0.1/servers/{serverName}/versions
//
// @Summary		List server versions
// @Description	Get a list of available versions for a specific server
// @Tags		registry,official
// @Accept		json
// @Produce		json
// @Param		serverName	path		string	true	"Server name"
// @Failure		400		{object}	map[string]string	"Bad request"
// @Failure		501		{object}	map[string]string	"Not implemented"
// @Router		/registry/v0.1/servers/{serverName}/versions [get]
func (rr *Routes) listVersions(w http.ResponseWriter, r *http.Request) {
	serverName := chi.URLParam(r, "serverName")
	if serverName == "" {
		common.WriteErrorResponse(w, "Server name is required", http.StatusBadRequest)
		return
	}

	common.WriteErrorResponse(w, "Listing versions is not supported", http.StatusNotImplemented)
}

// getVersion handles GET /registry/v0.1/servers/{serverName}/versions/{version}
//
// @Summary		Get server version
// @Description	Get details for a specific version of a server
// @Tags		registry,official
// @Accept		json
// @Produce		json
// @Param		serverName	path		string	true	"Server name"
// @Param		version		path		string	true	"Version identifier"
// @Failure		400		{object}	map[string]string	"Bad request"
// @Failure		501		{object}	map[string]string	"Not implemented"
// @Router		/registry/v0.1/servers/{serverName}/versions/{version} [get]
func (rr *Routes) getVersion(w http.ResponseWriter, r *http.Request) {
	serverName := chi.URLParam(r, "serverName")
	version := chi.URLParam(r, "version")
	if serverName == "" || version == "" {
		common.WriteErrorResponse(w, "Server name and version are required", http.StatusBadRequest)
		return
	}

	common.WriteErrorResponse(w, "Getting version details is not supported", http.StatusNotImplemented)
}

// publish handles POST /registry/v0.1/publish
//
// @Summary		Publish server
// @Description	Publish a server to the registry
// @Tags		registry,official
// @Accept		json
// @Produce		json
// @Failure		501	{object}	map[string]string	"Not implemented"
// @Router		/registry/v0.1/publish [post]
func (rr *Routes) publish(w http.ResponseWriter, r *http.Request) {
	common.WriteErrorResponse(w, "Publishing is not supported", http.StatusNotImplemented)
}
