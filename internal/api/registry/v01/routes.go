// Package v01 provides registry API v0.1 endpoints for MCP server discovery.
package v01

import (
	"errors"
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

	r.Get("/v0.1/servers", routes.listServers)
	r.Route("/v0.1/servers/{serverName}", func(r chi.Router) {
		r.Get("/versions", routes.listVersions)
		r.Get("/versions/{version}", routes.getVersion)
	})
	r.Post("/v0.1/publish", routes.publish)

	r.Get("/{registryName}/v0.1/servers", routes.listServersWithRegistryName)
	r.Route("/{registryName}/v0.1/servers/{serverName}", func(r chi.Router) {
		r.Get("/versions", routes.listVersionsWithRegistryName)
		r.Get("/versions/{version}", routes.getVersionWithRegistryName)
		r.Delete("/versions/{version}", routes.deleteVersionWithRegistryName)
	})
	r.Post("/{registryName}/v0.1/publish", routes.publishWithRegistryName)

	return r
}

// handleListServers is a shared helper that handles listing servers with an optional registry name.
func (routes *Routes) handleListServers(w http.ResponseWriter, r *http.Request, registryName string) {
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

	opts := []service.Option[service.ListServersOptions]{}
	if cursor != "" {
		opts = append(opts, service.WithCursor(cursor))
	}
	if limit != nil {
		opts = append(opts, service.WithLimit[service.ListServersOptions](*limit))
	}
	if search != "" {
		opts = append(opts, service.WithSearch(search))
	}
	if updatedSince != nil {
		opts = append(opts, service.WithUpdatedSince(*updatedSince))
	}
	if version != "" {
		opts = append(opts, service.WithVersion[service.ListServersOptions](version))
	}
	if registryName != "" {
		opts = append(opts, service.WithRegistryName[service.ListServersOptions](registryName))
	}

	servers, err := routes.service.ListServers(r.Context(), opts...)
	if err != nil {
		common.WriteErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	serverResponses := make([]upstreamv0.ServerResponse, len(servers))
	for i, server := range servers {
		serverResponses[i] = upstreamv0.ServerResponse{
			Server: *server,
			Meta:   upstreamv0.ResponseMeta{},
		}
	}

	result := upstreamv0.ServerListResponse{
		Servers: serverResponses,
		Metadata: upstreamv0.Metadata{
			NextCursor: "",
			Count:      len(servers),
		},
	}

	common.WriteJSONResponse(w, result, http.StatusOK)
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
func (routes *Routes) listServers(w http.ResponseWriter, r *http.Request) {
	routes.handleListServers(w, r, "")
}

// listServersWithRegistryName handles GET /{registryName}/v0.1/servers
//
// @Summary		List servers
// @Description	Get a list of available servers in the registry
// @Tags		registry,official
// @Accept		json
// @Produce		json
// @Param		registryName	path	string	true	"Registry name"
// @Param		cursor			query	string	false	"Pagination cursor for retrieving next set of results"
// @Param		limit			query	int		false	"Maximum number of items to return"
// @Param		search			query	string	false	"Search servers by name (substring match)"
// @Param		updated_since	query	time	false	"Filter servers updated since timestamp (RFC3339 datetime)"
// @Param		version			query	string	false	"Filter by version ('latest' for latest version, or an exact version like '1.2.3')"
// @Success		200		{object}	upstreamv0.ServerListResponse
// @Failure		400		{object}	map[string]string	"Bad request"
// @Router		/registry/{registryName}/v0.1/servers [get]
func (routes *Routes) listServersWithRegistryName(w http.ResponseWriter, r *http.Request) {
	registryName := chi.URLParam(r, "registryName")
	routes.handleListServers(w, r, registryName)
}

// handleListVersions is a shared helper that handles listing versions with an optional registry name.
func (routes *Routes) handleListVersions(w http.ResponseWriter, r *http.Request, registryName string) {
	serverName := chi.URLParam(r, "serverName")
	if strings.TrimSpace(serverName) == "" {
		common.WriteErrorResponse(w, "Server name is required", http.StatusBadRequest)
		return
	}

	opts := []service.Option[service.ListServerVersionsOptions]{
		// Note: Upstream API does not support pagination for versions,
		// so we return an arbitrary large number of records.
		service.WithLimit[service.ListServerVersionsOptions](1000),
	}
	if registryName != "" {
		opts = append(opts, service.WithRegistryName[service.ListServerVersionsOptions](registryName))
	}
	if serverName != "" {
		opts = append(opts, service.WithName[service.ListServerVersionsOptions](serverName))
	}

	versions, err := routes.service.ListServerVersions(r.Context(), opts...)
	if err != nil {
		common.WriteErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	serverResponses := make([]upstreamv0.ServerResponse, len(versions))
	for i, version := range versions {
		serverResponses[i] = upstreamv0.ServerResponse{
			Server: *version,
			Meta:   upstreamv0.ResponseMeta{},
		}
	}

	result := upstreamv0.ServerListResponse{
		Servers: serverResponses,
		Metadata: upstreamv0.Metadata{
			NextCursor: "",
			Count:      len(versions),
		},
	}

	common.WriteJSONResponse(w, result, http.StatusOK)
}

// listVersions handles GET /registry/v0.1/servers/{serverName}/versions
//
// @Summary		List all versions of an MCP server
// @Description	Returns all available versions for a specific MCP server, ordered by publication date (newest first)
// @Tags		registry,official
// @Accept		json
// @Produce		json
// @Param		serverName	path		string	true	"URL-encoded server name (e.g., \"com.example%2Fmy-server\")"
// @Success		200		{object}	upstreamv0.ServerListResponse	"A list of all versions for the server"
// @Failure		400		{object}	map[string]string	"Bad request"
// @Failure		404		{object}	map[string]string	"Server not found"
// @Router		/registry/v0.1/servers/{serverName}/versions [get]
func (routes *Routes) listVersions(w http.ResponseWriter, r *http.Request) {
	routes.handleListVersions(w, r, "")
}

// listVersionsWithRegistryName handles GET /{registryName}/v0.1/servers/{serverName}/versions
//
// @Summary		List all versions of an MCP server
// @Description	Returns all available versions for a specific MCP server, ordered by publication date (newest first)
// @Tags		registry,official
// @Accept		json
// @Produce		json
// @Param		registryName	path	string	true	"Registry name"
// @Param		serverName	path		string	true	"URL-encoded server name (e.g., \"com.example%2Fmy-server\")"
// @Success		200		{object}	upstreamv0.ServerListResponse	"A list of all versions for the server"
// @Failure		400		{object}	map[string]string	"Bad request"
// @Failure		404		{object}	map[string]string	"Server not found"
// @Router		/registry/{registryName}/v0.1/servers/{serverName}/versions [get]
func (routes *Routes) listVersionsWithRegistryName(w http.ResponseWriter, r *http.Request) {
	registryName := chi.URLParam(r, "registryName")
	routes.handleListVersions(w, r, registryName)
}

// handleGetVersion is a shared helper that handles getting a version with an optional registry name.
func (routes *Routes) handleGetVersion(w http.ResponseWriter, r *http.Request, registryName string) {
	serverName := chi.URLParam(r, "serverName")
	version := chi.URLParam(r, "version")
	if strings.TrimSpace(serverName) == "" || strings.TrimSpace(version) == "" {
		common.WriteErrorResponse(w, "Server name and version are required", http.StatusBadRequest)
		return
	}

	opts := []service.Option[service.GetServerVersionOptions]{}
	if registryName != "" {
		opts = append(opts, service.WithRegistryName[service.GetServerVersionOptions](registryName))
	}
	if serverName != "" {
		opts = append(opts, service.WithName[service.GetServerVersionOptions](serverName))
	}
	if version != "" {
		opts = append(opts, service.WithVersion[service.GetServerVersionOptions](version))
	}

	server, err := routes.service.GetServerVersion(r.Context(), opts...)
	if err != nil {
		common.WriteErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if server == nil {
		common.WriteErrorResponse(w, "Server not found", http.StatusNotFound)
		return
	}

	serverResponse := upstreamv0.ServerResponse{
		Server: *server,
		Meta:   upstreamv0.ResponseMeta{},
	}
	common.WriteJSONResponse(w, serverResponse, http.StatusOK)
}

// getVersion handles GET /registry/v0.1/servers/{serverName}/versions/{version}
//
// @Summary		Get specific MCP server version
// @Description	Returns detailed information about a specific version of an MCP server.
// @Description	Use the special version `latest` to get the latest version.
// @Tags		registry,official
// @Accept		json
// @Produce		json
// @Param		serverName	path	string	true	"URL-encoded server name (e.g., \"com.example%2Fmy-server\")"
// @Param		version		path	string	true	"URL-encoded version to retrieve (e.g., \"1.0.0\")"
// @Success		200		{object}	upstreamv0.ServerResponse	"Detailed server information"
// @Failure		400		{object}	map[string]string	"Bad request"
// @Failure		404		{object}	map[string]string	"Server or version not found"
// @Router		/registry/v0.1/servers/{serverName}/versions/{version} [get]
func (routes *Routes) getVersion(w http.ResponseWriter, r *http.Request) {
	routes.handleGetVersion(w, r, "")
}

// getVersionWithRegistryName handles GET /{registryName}/v0.1/servers/{serverName}/versions/{version}
//
// @Summary		Get specific MCP server version
// @Description	Returns detailed information about a specific version of an MCP server.
// @Description	Use the special version `latest` to get the latest version.
// @Tags		registry,official
// @Accept		json
// @Produce		json
// @Param		registryName	path		string	true	"Registry name"
// @Param		serverName		path		string	true	"URL-encoded server name (e.g., \"com.example%2Fmy-server\")"
// @Param		version			path		string	true	"URL-encoded version to retrieve (e.g., \"1.0.0\")"
// @Success		200				{object}	upstreamv0.ServerResponse	"Detailed server information"
// @Failure		400				{object}	map[string]string	"Bad request"
// @Failure		404				{object}	map[string]string	"Server or version not found"
// @Router		/registry/{registryName}/v0.1/servers/{serverName}/versions/{version} [get]
func (routes *Routes) getVersionWithRegistryName(w http.ResponseWriter, r *http.Request) {
	registryName := chi.URLParam(r, "registryName")
	routes.handleGetVersion(w, r, registryName)
}

// deleteVersionWithRegistryName handles DELETE /{registryName}/v0.1/servers/{serverName}/versions/{version}
//
// @Summary      Delete server version from specific registry
// @Description  Delete a server version from a specific managed registry
// @Tags         registry,official
// @Accept       json
// @Produce      json
// @Param        registryName  path  string  true  "Registry name"
// @Param        serverName    path  string  true  "Server name (URL-encoded)"
// @Param        version       path  string  true  "Version (URL-encoded)"
// @Success      204  "No content"
// @Failure      400  {object}  map[string]string  "Bad request"
// @Failure      403  {object}  map[string]string  "Not a managed registry"
// @Failure      404  {object}  map[string]string  "Server version not found"
// @Failure      500  {object}  map[string]string  "Internal server error"
// @Router       /{registryName}/v0.1/servers/{serverName}/versions/{version} [delete]
func (routes *Routes) deleteVersionWithRegistryName(w http.ResponseWriter, r *http.Request) {
	// Extract URL parameters
	registryName := chi.URLParam(r, "registryName")
	serverName := chi.URLParam(r, "serverName")
	version := chi.URLParam(r, "version")

	if strings.TrimSpace(registryName) == "" {
		common.WriteErrorResponse(w, "Registry name is required", http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(serverName) == "" {
		common.WriteErrorResponse(w, "Server name is required", http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(version) == "" {
		common.WriteErrorResponse(w, "Version is required", http.StatusBadRequest)
		return
	}

	// Call service layer
	err := routes.service.DeleteServerVersion(
		r.Context(),
		service.WithDeleteRegistryName(registryName),
		service.WithDeleteServerName(serverName),
		service.WithDeleteVersion(version),
	)

	if err != nil {
		// Check for specific error types
		if errors.Is(err, service.ErrRegistryNotFound) || errors.Is(err, service.ErrServerNotFound) {
			common.WriteErrorResponse(w, err.Error(), http.StatusNotFound)
			return
		}
		if errors.Is(err, service.ErrNotManagedRegistry) {
			common.WriteErrorResponse(w, err.Error(), http.StatusForbidden)
			return
		}
		// All other errors
		common.WriteErrorResponse(w, "Failed to delete server version", http.StatusInternalServerError)
		return
	}

	// Success - return 204 No Content
	w.WriteHeader(http.StatusNoContent)
}

// handlePublish is a shared helper that handles publishing with an optional registry name.
func (*Routes) handlePublish(w http.ResponseWriter, r *http.Request, registryName string) {
	// TODO: Use registryName in the actual implementation
	_ = registryName
	_ = r

	common.WriteErrorResponse(w, "Publishing is not supported", http.StatusNotImplemented)
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
func (routes *Routes) publish(w http.ResponseWriter, r *http.Request) {
	routes.handlePublish(w, r, "")
}

// publishWithRegistryName handles POST /{registryName}/v0.1/publish
//
// @Summary		Publish server
// @Description	Publish a server to the registry
// @Tags		registry,official
// @Accept		json
// @Produce		json
// @Param		registryName	path		string	true	"Registry name"
// @Failure		501	{object}	map[string]string	"Not implemented"
// @Router		/registry/{registryName}/v0.1/publish [post]
func (routes *Routes) publishWithRegistryName(w http.ResponseWriter, r *http.Request) {
	registryName := chi.URLParam(r, "registryName")
	routes.handlePublish(w, r, registryName)
}
