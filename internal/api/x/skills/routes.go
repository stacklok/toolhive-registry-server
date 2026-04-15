// Package skills provides API types and handlers for the dev.toolhive/skills
// extension endpoints (THV-0029).
package skills

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

// Router returns an HTTP handler for the dev.toolhive/skills extension routes.
func Router(svc service.RegistryService) http.Handler {
	r := chi.NewRouter()
	routes := &Routes{service: svc}

	r.Get("/", auditmw.AuditedSkill(auditmw.EventSkillList, routes.listSkills))
	r.Get("/{namespace}/{name}", auditmw.AuditedSkill(auditmw.EventSkillRead, routes.getLatestVersion))
	r.Get("/{namespace}/{name}/versions", auditmw.AuditedSkill(auditmw.EventSkillVersionsList, routes.listVersions))
	r.Get("/{namespace}/{name}/versions/{version}", auditmw.AuditedSkill(auditmw.EventSkillVersionRead, routes.getVersion))

	return r
}

// Routes holds dependencies for skills extension handlers.
type Routes struct {
	service service.RegistryService
}

// listSkills handles GET /registry/{registryName}/v0.1/x/dev.toolhive/skills
//
// @Summary		List skills in registry
// @Description	List skills in a registry (paginated, latest versions).
// @Tags		skills
// @Accept		json
// @Produce		json
// @Param		registryName	path		string	true	"Registry name"
// @Param		search		query		string	false	"Filter by name/description substring"
// @Param		status		query		string	false	"Filter by status (comma-separated, e.g. active,deprecated)"
// @Param		limit		query		int		false	"Max results (default 50, max 100)"
// @Param		cursor		query		string	false	"Pagination cursor"
// @Success		200			{object}	SkillListResponse	"List of skills"
// @Failure		400			{object}	map[string]string	"Bad request"
// @Failure		500			{object}	map[string]string	"Internal server error"
// @Security	BearerAuth
// @Router		/registry/{registryName}/v0.1/x/dev.toolhive/skills [get]
func (routes *Routes) listSkills(w http.ResponseWriter, r *http.Request) {
	registryName, err := common.GetAndValidateURLParam(r, "registryName")
	if err != nil {
		common.WriteErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	query, err := parseListSkillsQuery(r)
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

	result, err := routes.service.ListSkills(r.Context(), opts...)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}

	resp := SkillListResponse{
		Skills: serviceSkillsToResponse(result.Skills),
		Metadata: SkillListMetadata{
			Count:      len(result.Skills),
			NextCursor: result.NextCursor,
		},
	}

	common.WriteJSONResponse(w, resp, http.StatusOK)
}

// getLatestVersion handles GET /registry/{registryName}/v0.1/x/dev.toolhive/skills/{namespace}/{name}
//
// @Summary		Get latest skill version
// @Description	Get the latest version of a skill by namespace and name.
// @Tags		skills
// @Accept		json
// @Produce		json
// @Param		registryName	path		string	true	"Registry name"
// @Param		namespace	path		string	true	"Skill namespace (reverse-DNS)"
// @Param		name			path		string	true	"Skill name"
// @Success		200				{object}	thvregistry.Skill	"Skill details"
// @Failure		400				{object}	map[string]string	"Bad request"
// @Failure		404				{object}	map[string]string	"Skill not found"
// @Failure		500				{object}	map[string]string	"Internal server error"
// @Security	BearerAuth
// @Router		/registry/{registryName}/v0.1/x/dev.toolhive/skills/{namespace}/{name} [get]
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

	skillOpts := []service.Option{
		service.WithRegistryName(registryName),
		service.WithNamespace(namespace),
		service.WithName(name),
		service.WithVersion("latest"),
	}
	if jwtClaims := auth.ClaimsFromContext(r.Context()); jwtClaims != nil {
		skillOpts = append(skillOpts, service.WithClaims(map[string]any(jwtClaims)))
	}

	skill, err := routes.service.GetSkillVersion(r.Context(), skillOpts...)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}

	common.WriteJSONResponse(w, serviceSkillToResponse(skill), http.StatusOK)
}

// listVersions handles GET /registry/{registryName}/v0.1/x/dev.toolhive/skills/{namespace}/{name}/versions
//
// @Summary		List skill versions
// @Description	List all versions of a skill.
// @Tags		skills
// @Accept		json
// @Produce		json
// @Param		registryName	path		string	true	"Registry name"
// @Param		namespace	path		string	true	"Skill namespace (reverse-DNS)"
// @Param		name			path		string	true	"Skill name"
// @Success		200				{object}	SkillListResponse	"List of skill versions"
// @Failure		400				{object}	map[string]string	"Bad request"
// @Failure		404				{object}	map[string]string	"Skill not found"
// @Failure		500				{object}	map[string]string	"Internal server error"
// @Security	BearerAuth
// @Router		/registry/{registryName}/v0.1/x/dev.toolhive/skills/{namespace}/{name}/versions [get]
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

	result, err := routes.service.ListSkills(r.Context(), listOpts...)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}

	resp := SkillListResponse{
		Skills: serviceSkillsToResponse(result.Skills),
		Metadata: SkillListMetadata{
			Count:      len(result.Skills),
			NextCursor: result.NextCursor,
		},
	}

	common.WriteJSONResponse(w, resp, http.StatusOK)
}

// getVersion handles GET /registry/{registryName}/v0.1/x/dev.toolhive/skills/{namespace}/{name}/versions/{version}
//
// @Summary		Get specific skill version
// @Description	Get a specific version of a skill.
// @Tags		skills
// @Accept		json
// @Produce		json
// @Param		registryName	path		string	true	"Registry name"
// @Param		namespace	path		string	true	"Skill namespace (reverse-DNS)"
// @Param		name			path		string	true	"Skill name"
// @Param		version		path		string	true	"Skill version"
// @Success		200				{object}	thvregistry.Skill	"Skill details"
// @Failure		400				{object}	map[string]string	"Bad request"
// @Failure		404				{object}	map[string]string	"Skill or version not found"
// @Failure		500				{object}	map[string]string	"Internal server error"
// @Security	BearerAuth
// @Router		/registry/{registryName}/v0.1/x/dev.toolhive/skills/{namespace}/{name}/versions/{version} [get]
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

	skill, err := routes.service.GetSkillVersion(r.Context(), versionOpts...)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}

	common.WriteJSONResponse(w, serviceSkillToResponse(skill), http.StatusOK)
}

// parseListSkillsQuery parses and validates list skills query parameters.
func parseListSkillsQuery(r *http.Request) (*ListSkillsQuery, error) {
	q := r.URL.Query()
	query := &ListSkillsQuery{
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

// serviceSkillToResponse maps a service.Skill to a thvregistry.Skill response.
func serviceSkillToResponse(s *service.Skill) thvregistry.Skill {
	resp := thvregistry.Skill{
		Namespace:     s.Namespace,
		Name:          s.Name,
		Description:   s.Description,
		Version:       s.Version,
		Status:        s.Status,
		Title:         s.Title,
		License:       s.License,
		Compatibility: s.Compatibility,
		AllowedTools:  s.AllowedTools,
		Metadata:      s.Metadata,
		Meta:          s.Meta,
	}
	if s.Repository != nil {
		resp.Repository = &thvregistry.SkillRepository{
			URL:  s.Repository.URL,
			Type: s.Repository.Type,
		}
	}
	for _, icon := range s.Icons {
		resp.Icons = append(resp.Icons, thvregistry.SkillIcon{
			Src:   icon.Src,
			Size:  icon.Size,
			Type:  icon.Type,
			Label: icon.Label,
		})
	}
	for _, pkg := range s.Packages {
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

// serviceSkillsToResponse maps a slice of service.Skill to thvregistry.Skill responses.
func serviceSkillsToResponse(skills []*service.Skill) []thvregistry.Skill {
	result := make([]thvregistry.Skill, len(skills))
	for i, s := range skills {
		result[i] = serviceSkillToResponse(s)
	}
	return result
}
