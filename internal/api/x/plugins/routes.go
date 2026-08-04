// Package plugins provides API types and handlers for the dev.toolhive/plugins
// extension endpoints.
package plugins

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	thvregistry "github.com/stacklok/toolhive-core/registry/types"

	"github.com/stacklok/toolhive-registry-server/internal/api/common"
	auditmw "github.com/stacklok/toolhive-registry-server/internal/audit"
	"github.com/stacklok/toolhive-registry-server/internal/auth"
	"github.com/stacklok/toolhive-registry-server/internal/service"
)

const (
	defaultListLimit = 50
	maxListLimit     = 100
)

// Router returns an HTTP handler for the dev.toolhive/plugins extension routes.
func Router(svc service.RegistryService) http.Handler {
	r := chi.NewRouter()
	routes := &Routes{service: svc}

	r.Get("/", auditmw.AuditedPlugin(auditmw.EventPluginList, routes.listPlugins))
	r.Get("/{namespace}/{name}", auditmw.AuditedPlugin(auditmw.EventPluginRead, routes.getLatestVersion))
	r.Get("/{namespace}/{name}/versions", auditmw.AuditedPlugin(auditmw.EventPluginVersionsList, routes.listVersions))
	r.Get("/{namespace}/{name}/versions/{version}", auditmw.AuditedPlugin(auditmw.EventPluginVersionRead, routes.getVersion))

	return r
}

// Routes holds dependencies for plugins extension handlers.
type Routes struct {
	service service.RegistryService
}

// listPlugins handles GET /registry/{registryName}/v0.1/x/dev.toolhive/plugins
//
// @Summary		List plugins in registry
// @Description	List plugins in a registry (paginated, latest versions).
// @Tags		plugins
// @Produce		json
// @Param		registryName	path		string	true	"Registry name"
// @Param		search		query		string	false	"Filter by name/description substring"
// @Param		status		query		string	false	"Filter by status (comma-separated, e.g. active,deprecated)"
// @Param		limit		query		int		false	"Max results (default 50, max 100)"
// @Param		cursor		query		string	false	"Pagination cursor"
// @Success		200			{object}	PluginListResponse	"List of plugins"
// @Failure		400			{object}	map[string]string	"Bad request"
// @Failure		500			{object}	map[string]string	"Internal server error"
// @Security	BearerAuth
// @Router		/registry/{registryName}/v0.1/x/dev.toolhive/plugins [get]
func (routes *Routes) listPlugins(w http.ResponseWriter, r *http.Request) {
	registryName, err := common.GetAndValidateURLParam(r, "registryName")
	if err != nil {
		common.WriteErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	query, err := parseListPluginsQuery(r)
	if err != nil {
		common.WriteErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	opts := []service.Option{
		service.WithRegistryName(registryName),
		service.WithLimit(query.Limit),
	}
	if query.Search != "" {
		opts = append(opts, service.WithSearch(query.Search))
	}
	if query.Cursor != "" {
		opts = append(opts, service.WithCursor(query.Cursor))
	}
	if jwtClaims := auth.ClaimsFromContext(r.Context()); jwtClaims != nil {
		opts = append(opts, service.WithClaims(map[string]any(jwtClaims)))
	}

	result, err := routes.service.ListPlugins(r.Context(), opts...)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}

	resp := PluginListResponse{
		Plugins: servicePluginsToResponse(result.Plugins),
		Metadata: PluginListMetadata{
			Count:      len(result.Plugins),
			NextCursor: result.NextCursor,
		},
	}

	common.WriteJSONResponse(w, resp, http.StatusOK)
}

// getLatestVersion handles GET /registry/{registryName}/v0.1/x/dev.toolhive/plugins/{namespace}/{name}
//
// @Summary		Get latest plugin version
// @Description	Get the latest version of a plugin by namespace and name.
// @Tags		plugins
// @Produce		json
// @Param		registryName	path		string	true	"Registry name"
// @Param		namespace	path		string	true	"Plugin namespace (reverse-DNS)"
// @Param		name			path		string	true	"Plugin name"
// @Success		200				{object}	thvregistry.Plugin	"Plugin details"
// @Failure		400				{object}	map[string]string	"Bad request"
// @Failure		404				{object}	map[string]string	"Plugin not found"
// @Failure		500				{object}	map[string]string	"Internal server error"
// @Security	BearerAuth
// @Router		/registry/{registryName}/v0.1/x/dev.toolhive/plugins/{namespace}/{name} [get]
func (routes *Routes) getLatestVersion(w http.ResponseWriter, r *http.Request) {
	registryName, err := common.GetAndValidateURLParam(r, "registryName")
	if err != nil {
		common.WriteErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}
	namespace, err := common.GetAndValidateURLParam(r, "namespace")
	if err != nil {
		common.WriteErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}
	name, err := common.GetAndValidateURLParam(r, "name")
	if err != nil {
		common.WriteErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	pluginOpts := []service.Option{
		service.WithRegistryName(registryName),
		service.WithNamespace(namespace),
		service.WithName(name),
		service.WithVersion("latest"),
	}
	if jwtClaims := auth.ClaimsFromContext(r.Context()); jwtClaims != nil {
		pluginOpts = append(pluginOpts, service.WithClaims(map[string]any(jwtClaims)))
	}

	plugin, err := routes.service.GetPluginVersion(r.Context(), pluginOpts...)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}

	common.WriteJSONResponse(w, servicePluginToResponse(plugin), http.StatusOK)
}

// listVersions handles GET /registry/{registryName}/v0.1/x/dev.toolhive/plugins/{namespace}/{name}/versions
//
// @Summary		List plugin versions
// @Description	List all versions of a plugin.
// @Tags		plugins
// @Produce		json
// @Param		registryName	path		string	true	"Registry name"
// @Param		namespace	path		string	true	"Plugin namespace (reverse-DNS)"
// @Param		name			path		string	true	"Plugin name"
// @Success		200				{object}	PluginListResponse	"List of plugin versions"
// @Failure		400				{object}	map[string]string	"Bad request"
// @Failure		404				{object}	map[string]string	"Plugin not found"
// @Failure		500				{object}	map[string]string	"Internal server error"
// @Security	BearerAuth
// @Router		/registry/{registryName}/v0.1/x/dev.toolhive/plugins/{namespace}/{name}/versions [get]
func (routes *Routes) listVersions(w http.ResponseWriter, r *http.Request) {
	registryName, err := common.GetAndValidateURLParam(r, "registryName")
	if err != nil {
		common.WriteErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}
	namespace, err := common.GetAndValidateURLParam(r, "namespace")
	if err != nil {
		common.WriteErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}
	name, err := common.GetAndValidateURLParam(r, "name")
	if err != nil {
		common.WriteErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	listOpts := []service.Option{
		service.WithRegistryName(registryName),
		service.WithNamespace(namespace),
		service.WithName(name),
	}
	if jwtClaims := auth.ClaimsFromContext(r.Context()); jwtClaims != nil {
		listOpts = append(listOpts, service.WithClaims(map[string]any(jwtClaims)))
	}

	result, err := routes.service.ListPlugins(r.Context(), listOpts...)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}

	resp := PluginListResponse{
		Plugins: servicePluginsToResponse(result.Plugins),
		Metadata: PluginListMetadata{
			Count:      len(result.Plugins),
			NextCursor: result.NextCursor,
		},
	}

	common.WriteJSONResponse(w, resp, http.StatusOK)
}

// getVersion handles GET /registry/{registryName}/v0.1/x/dev.toolhive/plugins/{namespace}/{name}/versions/{version}
//
// @Summary		Get specific plugin version
// @Description	Get a specific version of a plugin.
// @Tags		plugins
// @Produce		json
// @Param		registryName	path		string	true	"Registry name"
// @Param		namespace	path		string	true	"Plugin namespace (reverse-DNS)"
// @Param		name			path		string	true	"Plugin name"
// @Param		version		path		string	true	"Plugin version"
// @Success		200				{object}	thvregistry.Plugin	"Plugin details"
// @Failure		400				{object}	map[string]string	"Bad request"
// @Failure		404				{object}	map[string]string	"Plugin or version not found"
// @Failure		500				{object}	map[string]string	"Internal server error"
// @Security	BearerAuth
// @Router		/registry/{registryName}/v0.1/x/dev.toolhive/plugins/{namespace}/{name}/versions/{version} [get]
func (routes *Routes) getVersion(w http.ResponseWriter, r *http.Request) {
	registryName, err := common.GetAndValidateURLParam(r, "registryName")
	if err != nil {
		common.WriteErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}
	namespace, err := common.GetAndValidateURLParam(r, "namespace")
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

	versionOpts := []service.Option{
		service.WithRegistryName(registryName),
		service.WithNamespace(namespace),
		service.WithName(name),
		service.WithVersion(version),
	}
	if jwtClaims := auth.ClaimsFromContext(r.Context()); jwtClaims != nil {
		versionOpts = append(versionOpts, service.WithClaims(map[string]any(jwtClaims)))
	}

	plugin, err := routes.service.GetPluginVersion(r.Context(), versionOpts...)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}

	common.WriteJSONResponse(w, servicePluginToResponse(plugin), http.StatusOK)
}

// parseListPluginsQuery parses and validates list plugins query parameters.
func parseListPluginsQuery(r *http.Request) (*ListPluginsQuery, error) {
	q := r.URL.Query()
	query := &ListPluginsQuery{
		Search: strings.TrimSpace(q.Get("search")),
		Status: strings.TrimSpace(q.Get("status")),
		Cursor: strings.TrimSpace(q.Get("cursor")),
		Limit:  defaultListLimit,
	}

	if limitStr := q.Get("limit"); limitStr != "" {
		limit, err := strconv.Atoi(limitStr)
		if err != nil {
			return nil, fmt.Errorf("invalid limit parameter: must be an integer")
		}
		if limit < 1 || limit > maxListLimit {
			return nil, fmt.Errorf("invalid limit parameter: must be between 1 and %d", maxListLimit)
		}
		query.Limit = limit
	}

	return query, nil
}

// writeServiceError maps service-layer errors to HTTP responses.
func writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, service.ErrClaimsInsufficient):
		common.WriteErrorResponse(w, "forbidden: insufficient claims for registry", http.StatusForbidden)
	case errors.Is(err, service.ErrNotFound):
		common.WriteErrorResponse(w, "not found", http.StatusNotFound)
	case errors.Is(err, service.ErrRegistryNotFound):
		common.WriteErrorResponse(w, "registry not found", http.StatusNotFound)
	default:
		slog.ErrorContext(r.Context(), "unexpected error", "error", err)
		common.WriteErrorResponse(w, "internal server error", http.StatusInternalServerError)
	}
}

// servicePluginToResponse maps a service.Plugin to a thvregistry.Plugin response.
func servicePluginToResponse(p *service.Plugin) thvregistry.Plugin {
	resp := thvregistry.Plugin{
		Namespace:   p.Namespace,
		Name:        p.Name,
		Description: p.Description,
		Version:     p.Version,
		Status:      p.Status,
		Title:       p.Title,
		License:     p.License,
		Metadata:    p.Metadata,
		Meta:        p.Meta,
	}
	if p.Repository != nil {
		resp.Repository = &thvregistry.SkillRepository{
			URL:  p.Repository.URL,
			Type: p.Repository.Type,
		}
	}
	for _, icon := range p.Icons {
		resp.Icons = append(resp.Icons, thvregistry.SkillIcon{
			Src:   icon.Src,
			Size:  icon.Size,
			Type:  icon.Type,
			Label: icon.Label,
		})
	}
	for _, pkg := range p.Packages {
		resp.Packages = append(resp.Packages, thvregistry.SkillPackage{
			RegistryType: pkg.RegistryType,
			Identifier:   pkg.Identifier,
			Digest:       pkg.Digest,
			MediaType:    pkg.MediaType,
			URL:          pkg.URL,
			Ref:          pkg.Ref,
			Commit:       pkg.Commit,
			Subfolder:    pkg.Subfolder,
		})
	}
	return resp
}

// servicePluginsToResponse maps a slice of service.Plugin to thvregistry.Plugin responses.
func servicePluginsToResponse(plugins []*service.Plugin) []thvregistry.Plugin {
	result := make([]thvregistry.Plugin, len(plugins))
	for i, p := range plugins {
		result[i] = servicePluginToResponse(p)
	}
	return result
}
