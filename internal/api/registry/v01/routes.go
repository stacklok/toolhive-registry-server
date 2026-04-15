// Package v01 provides registry API v0.1 endpoints for MCP server discovery.
package v01

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"

	"github.com/stacklok/toolhive-registry-server/internal/api/common"
	"github.com/stacklok/toolhive-registry-server/internal/api/x/skills"
	auditmw "github.com/stacklok/toolhive-registry-server/internal/audit"
	"github.com/stacklok/toolhive-registry-server/internal/auth"
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

	r.Mount("/{registryName}/v0.1", registryRouter(routes))
	r.Mount("/{registryName}/v0.1/x/dev.toolhive/skills", skills.Router(svc))

	return r
}

func registryRouter(routes *Routes) http.Handler {
	r := chi.NewRouter()

	r.Get("/servers", auditmw.AuditedServer(auditmw.EventServerList, routes.listServersWithRegistryName))
	r.Route("/servers/{serverName}", func(r chi.Router) {
		r.Get("/versions", auditmw.AuditedServer(auditmw.EventServerVersionsList, routes.listVersionsWithRegistryName))
		r.Get("/versions/{version}", auditmw.AuditedServer(auditmw.EventServerVersionRead, routes.getVersionWithRegistryName))
	})

	return r
}

// handleListServers is a shared helper that handles listing servers with a registry name.
//
//nolint:gocyclo // complexity driven by many optional query parameters
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

	opts := []service.Option{}
	if cursor != "" {
		opts = append(opts, service.WithCursor(cursor))
	}
	if limit != nil {
		opts = append(opts, service.WithLimit(*limit))
	}
	if search != "" {
		opts = append(opts, service.WithSearch(search))
	}
	if updatedSince != nil {
		opts = append(opts, service.WithUpdatedSince(*updatedSince))
	}
	if version != "" {
		opts = append(opts, service.WithVersion(version))
	}
	if registryName != "" {
		opts = append(opts, service.WithRegistryName(registryName))
	}
	if jwtClaims := auth.ClaimsFromContext(r.Context()); jwtClaims != nil {
		opts = append(opts, service.WithClaims(map[string]any(jwtClaims)))
	}

	listResult, err := routes.service.ListServers(r.Context(), opts...)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}

	slog.InfoContext(r.Context(), "List servers completed",
		"result_count", len(listResult.Servers),
		"has_more", listResult.NextCursor != "",
		"request_id", middleware.GetReqID(r.Context()),
	)

	serverResponses := make([]upstreamv0.ServerResponse, len(listResult.Servers))
	for i, server := range listResult.Servers {
		serverResponses[i] = upstreamv0.ServerResponse{
			Server: *server,
			Meta:   upstreamv0.ResponseMeta{},
		}
	}

	result := upstreamv0.ServerListResponse{
		Servers: serverResponses,
		Metadata: upstreamv0.Metadata{
			NextCursor: listResult.NextCursor,
			Count:      len(listResult.Servers),
		},
	}

	common.WriteJSONResponse(w, result, http.StatusOK)
}

// listServersWithRegistryName handles GET /{registryName}/v0.1/servers
//
// @Summary		List servers in specific registry
// @Description	Get a list of available servers from a specific registry
// @Tags		registry
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
// @Failure		401		{object}	map[string]string	"Unauthorized"
// @Failure		404		{object}	map[string]string	"Registry not found"
// @Security	BearerAuth
// @Router		/registry/{registryName}/v0.1/servers [get]
func (routes *Routes) listServersWithRegistryName(w http.ResponseWriter, r *http.Request) {
	registryName, err := common.GetAndValidateURLParam(r, "registryName")
	if err != nil {
		common.WriteErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	routes.handleListServers(w, r, registryName)
}

// handleListVersions is a shared helper that handles listing versions with a registry name.
func (routes *Routes) handleListVersions(w http.ResponseWriter, r *http.Request, registryName string) {
	serverName, err := common.GetAndValidateServerNameParam(r, "serverName")
	if err != nil {
		common.WriteErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	opts := []service.Option{
		// Note: Upstream API does not support pagination for versions,
		// so we return an arbitrary large number of records.
		service.WithLimit(1000),
	}
	if registryName != "" {
		opts = append(opts, service.WithRegistryName(registryName))
	}
	if serverName != "" {
		opts = append(opts, service.WithName(serverName))
	}
	if jwtClaims := auth.ClaimsFromContext(r.Context()); jwtClaims != nil {
		opts = append(opts, service.WithClaims(map[string]any(jwtClaims)))
	}

	versions, err := routes.service.ListServerVersions(r.Context(), opts...)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}

	slog.InfoContext(r.Context(), "List versions completed",
		"result_count", len(versions),
		"server_name", serverName,
		"request_id", middleware.GetReqID(r.Context()),
	)

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

// listVersionsWithRegistryName handles GET /{registryName}/v0.1/servers/{serverName}/versions
//
// @Summary		List all versions of an MCP server in specific registry
// @Description	Returns all available versions for a specific MCP server from a specific registry
// @Tags		registry
// @Accept		json
// @Produce		json
// @Param		registryName	path	string	true	"Registry name"
// @Param		serverName	path		string	true	"URL-encoded server name (e.g., \"com.example%2Fmy-server\")"
// @Success		200		{object}	upstreamv0.ServerListResponse	"A list of all versions for the server"
// @Failure		400		{object}	map[string]string	"Bad request"
// @Failure		401		{object}	map[string]string	"Unauthorized"
// @Failure		404		{object}	map[string]string	"Server not found"
// @Security	BearerAuth
// @Router		/registry/{registryName}/v0.1/servers/{serverName}/versions [get]
func (routes *Routes) listVersionsWithRegistryName(w http.ResponseWriter, r *http.Request) {
	registryName, err := common.GetAndValidateURLParam(r, "registryName")
	if err != nil {
		common.WriteErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	routes.handleListVersions(w, r, registryName)
}

// handleGetVersion is a shared helper that handles getting a version with a registry name.
func (routes *Routes) handleGetVersion(w http.ResponseWriter, r *http.Request, registryName string) {
	serverName, err := common.GetAndValidateServerNameParam(r, "serverName")
	if err != nil {
		common.WriteErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	version, err := common.GetAndValidateURLParam(r, "version")
	if err != nil {
		common.WriteErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	opts := []service.Option{}
	if registryName != "" {
		opts = append(opts, service.WithRegistryName(registryName))
	}
	if serverName != "" {
		opts = append(opts, service.WithName(serverName))
	}
	if version != "" {
		opts = append(opts, service.WithVersion(version))
	}
	if jwtClaims := auth.ClaimsFromContext(r.Context()); jwtClaims != nil {
		opts = append(opts, service.WithClaims(map[string]any(jwtClaims)))
	}

	server, err := routes.service.GetServerVersion(r.Context(), opts...)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	if server == nil {
		slog.ErrorContext(r.Context(), "GetServerVersion returned nil without error")
		common.WriteErrorResponse(w, "internal server error", http.StatusInternalServerError)
		return
	}

	serverResponse := upstreamv0.ServerResponse{
		Server: *server,
		Meta:   upstreamv0.ResponseMeta{},
	}
	common.WriteJSONResponse(w, serverResponse, http.StatusOK)
}

// getVersionWithRegistryName handles GET /{registryName}/v0.1/servers/{serverName}/versions/{version}
//
// @Summary		Get specific MCP server version in specific registry
// @Description	Returns detailed information about a specific version of an MCP server from a specific registry.
// @Description	Use the special version `latest` to get the latest version.
// @Tags		registry
// @Accept		json
// @Produce		json
// @Param		registryName	path		string	true	"Registry name"
// @Param		serverName		path		string	true	"URL-encoded server name (e.g., \"com.example%2Fmy-server\")"
// @Param		version			path		string	true	"URL-encoded version to retrieve (e.g., \"1.0.0\")"
// @Success		200				{object}	upstreamv0.ServerResponse	"Detailed server information"
// @Failure		400				{object}	map[string]string	"Bad request"
// @Failure		401				{object}	map[string]string	"Unauthorized"
// @Failure		404				{object}	map[string]string	"Server or version not found"
// @Security	BearerAuth
// @Router		/registry/{registryName}/v0.1/servers/{serverName}/versions/{version} [get]
func (routes *Routes) getVersionWithRegistryName(w http.ResponseWriter, r *http.Request) {
	registryName, err := common.GetAndValidateURLParam(r, "registryName")
	if err != nil {
		common.WriteErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	routes.handleGetVersion(w, r, registryName)
}

// writeServiceError maps service-layer errors to HTTP responses for upstream API handlers.
func writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, service.ErrClaimsInsufficient):
		common.WriteErrorResponse(w, "forbidden: insufficient claims for registry", http.StatusForbidden)
	case errors.Is(err, service.ErrRegistryNotFound):
		common.WriteErrorResponse(w, "registry not found", http.StatusNotFound)
	case errors.Is(err, service.ErrNotFound):
		common.WriteErrorResponse(w, "not found", http.StatusNotFound)
	default:
		slog.ErrorContext(r.Context(), "unexpected error", "error", err)
		common.WriteErrorResponse(w, "internal server error", http.StatusInternalServerError)
	}
}
