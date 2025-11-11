// Package v01 provides registry API v0.1 endpoints for MCP server discovery.
package v01

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"

	"github.com/stacklok/toolhive-registry-server/internal/api/common"
	"github.com/stacklok/toolhive-registry-server/internal/service"
)

// Routes handles HTTP requests for registry API v0.1 endpoints.
type Routes struct {
	service service.RegistryService
}

// NewRoutes creates a new Routes instance with the given service.
func NewRoutes(svc service.RegistryService) *Routes {
	return &Routes{
		service: svc,
	}
}

// Router creates and configures the HTTP router for registry API v0.1 endpoints.
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
// @Param		cursor			query	string	false	"Pagination cursor for retrieving next set of results"
// @Param		limit			query	int		false	"Maximum number of items to return"
// @Param		search			query	string	false	"Search servers by name (substring match)"
// @Param		updated_since	query	time	false	"Filter servers updated since timestamp (RFC3339 datetime)"
// @Param		version			query	string	false	"Filter by version ('latest' for latest version, or an exact version like '1.2.3')"
// @Success		200		{object}	upstreamv0.ServerListResponse
// @Failure		400		{object}	map[string]string	"Bad request"
// @Router		/registry/v0.1/servers [get]
func (*Routes) listServers(w http.ResponseWriter, r *http.Request) {
	// Parse query parameters
	query := r.URL.Query()

	// Parse cursor (optional string)
	cursor := query.Get("cursor")

	// Parse limit (optional integer)
	var limit *int
	if limitStr := query.Get("limit"); limitStr != "" {
		limitVal, err := strconv.Atoi(limitStr)
		if err != nil {
			common.WriteErrorResponse(w, "Invalid limit parameter: must be an integer", http.StatusBadRequest)
			return
		}
		limit = &limitVal
	}

	// Parse search (optional string)
	search := query.Get("search")

	// Parse updated_since (optional RFC3339 datetime)
	var updatedSince *time.Time
	if updatedSinceStr := query.Get("updated_since"); updatedSinceStr != "" {
		parsedTime, err := time.Parse(time.RFC3339, updatedSinceStr)
		if err != nil {
			common.WriteErrorResponse(
				w,
				"Invalid updated_since parameter: must be RFC3339 format (e.g., 2025-08-07T13:15:04.280Z)",
				http.StatusBadRequest,
			)
			return
		}
		updatedSince = &parsedTime
	}

	// Parse version (optional string)
	version := query.Get("version")

	// TODO: Use the parsed parameters in the actual implementation
	_ = cursor
	_ = limit
	_ = search
	_ = updatedSince
	_ = version

	// Placeholder response - replace with actual implementation
	common.WriteJSONResponse(w, upstreamv0.ServerListResponse{
		Servers: []upstreamv0.ServerResponse{},
		Metadata: upstreamv0.Metadata{
			Count: 0,
		},
	}, http.StatusOK)
}

// listVersions handles GET /registry/v0.1/servers/{serverName}/versions
//
// @Summary		List all versions of an MCP server
// @Description	Returns all available versions for a specific MCP server, ordered by publication date (newest first)
// @Tags		servers
// @Accept		json
// @Produce		json
// @Param		serverName	path		string	true	"URL-encoded server name (e.g., \"com.example%2Fmy-server\")"
// @Success		200		{object}	upstreamv0.ServerListResponse	"A list of all versions for the server"
// @Failure		400		{object}	map[string]string	"Bad request"
// @Failure		404		{object}	map[string]string	"Server not found"
// @Router		/registry/v0.1/servers/{serverName}/versions [get]
func (*Routes) listVersions(w http.ResponseWriter, r *http.Request) {
	serverName := chi.URLParam(r, "serverName")
	if strings.TrimSpace(serverName) == "" {
		common.WriteErrorResponse(w, "Server name is required", http.StatusBadRequest)
		return
	}

	// Return empty version list
	common.WriteJSONResponse(w, upstreamv0.ServerListResponse{
		Servers: []upstreamv0.ServerResponse{},
		Metadata: upstreamv0.Metadata{
			Count: 0,
		},
	}, http.StatusOK)
}

// getVersion handles GET /registry/v0.1/servers/{serverName}/versions/{version}
//
// @Summary		Get specific MCP server version
// @Description	Returns detailed information about a specific version of an MCP server.
// @Description	Use the special version `latest` to get the latest version.
// @Tags		servers
// @Accept		json
// @Produce		json
// @Param		serverName	path		string	true	"URL-encoded server name (e.g., \"com.example%2Fmy-server\")"
// @Param		version		path		string	true	"URL-encoded version to retrieve (e.g., \"1.0.0\")"
// @Success		200		{object}	upstreamv0.ServerResponse	"Detailed server information"
// @Failure		400		{object}	map[string]string	"Bad request"
// @Failure		404		{object}	map[string]string	"Server or version not found"
// @Router		/registry/v0.1/servers/{serverName}/versions/{version} [get]
func (*Routes) getVersion(w http.ResponseWriter, r *http.Request) {
	serverName := chi.URLParam(r, "serverName")
	version := chi.URLParam(r, "version")
	if strings.TrimSpace(serverName) == "" || strings.TrimSpace(version) == "" {
		common.WriteErrorResponse(w, "Server name and version are required", http.StatusBadRequest)
		return
	}

	common.WriteJSONResponse(w, upstreamv0.ServerResponse{}, http.StatusOK)
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
func (*Routes) publish(w http.ResponseWriter, _ *http.Request) {
	common.WriteErrorResponse(w, "Publishing is not supported", http.StatusNotImplemented)
}
