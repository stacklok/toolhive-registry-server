// Package skills provides API types and handlers for the dev.toolhive/skills
// extension endpoints (THV-0029).
package skills

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	thvregistry "github.com/stacklok/toolhive/pkg/registry/registry"

	"github.com/stacklok/toolhive-registry-server/internal/api/common"
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

	r.Get("/", routes.listSkills)
	r.Get("/{namespace}/{name}", routes.getLatestVersion)
	r.Get("/{namespace}/{name}/versions", routes.listVersions)
	r.Get("/{namespace}/{name}/versions/{version}", routes.getVersion)
	r.Post("/", routes.publishSkill)
	r.Delete("/{namespace}/{name}/versions/{version}", routes.deleteVersion)

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
// @Param		namespace	query		string	false	"Filter by namespace"
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

	result, err := routes.service.ListSkills(r.Context(), opts...)
	if err != nil {
		writeServiceError(w, err)
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

	skill, err := routes.service.GetSkillVersion(r.Context(),
		service.WithRegistryName(registryName),
		service.WithNamespace(namespace),
		service.WithName(name),
		service.WithVersion("latest"),
	)
	if err != nil {
		writeServiceError(w, err)
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

	result, err := routes.service.ListSkills(r.Context(),
		service.WithRegistryName(registryName),
		service.WithNamespace(namespace),
		service.WithName(name),
	)
	if err != nil {
		writeServiceError(w, err)
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

	skill, err := routes.service.GetSkillVersion(r.Context(),
		service.WithRegistryName(registryName),
		service.WithNamespace(namespace),
		service.WithName(name),
		service.WithVersion(version),
	)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	common.WriteJSONResponse(w, serviceSkillToResponse(skill), http.StatusOK)
}

// publishSkill handles POST /registry/{registryName}/v0.1/x/dev.toolhive/skills
//
// @Summary		Publish skill
// @Description	Publish a skill version to the registry.
// @Tags		skills
// @Accept		json
// @Produce		json
// @Param		registryName	path		string				true	"Registry name"
// @Param		body			body		thvregistry.Skill	true	"Skill data"
// @Success		201				{object}	thvregistry.Skill	"Created"
// @Failure		400				{object}	map[string]string	"Bad request"
// @Failure		401				{object}	map[string]string	"Unauthorized"
// @Failure		403				{object}	map[string]string	"Not a managed registry"
// @Failure		409				{object}	map[string]string	"Version already exists"
// @Failure		500				{object}	map[string]string	"Internal server error"
// @Security	BearerAuth
// @Router		/registry/{registryName}/v0.1/x/dev.toolhive/skills [post]
func (routes *Routes) publishSkill(w http.ResponseWriter, r *http.Request) {
	registryName, err := common.GetAndValidateURLParam(r, "registryName")
	if err != nil {
		common.WriteErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	if r.Body == nil {
		common.WriteErrorResponse(w, "Request body is required", http.StatusBadRequest)
		return
	}

	var skill thvregistry.Skill
	if err := json.NewDecoder(r.Body).Decode(&skill); err != nil {
		common.WriteErrorResponse(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	if err := validatePublishSkillRequest(&skill); err != nil {
		common.WriteErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	// TODO: double-check this
	result, err := routes.service.PublishSkill(r.Context(),
		toService(&skill),
		service.WithRegistryName(registryName),
	)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	common.WriteJSONResponse(w, serviceSkillToResponse(result), http.StatusCreated)
}

func toService(skill *thvregistry.Skill) *service.Skill {
	svcSkill := &service.Skill{
		Namespace:     skill.Namespace,
		Name:          skill.Name,
		Description:   skill.Description,
		Version:       skill.Version,
		Status:        skill.Status,
		Title:         skill.Title,
		License:       skill.License,
		Compatibility: skill.Compatibility,
		AllowedTools:  skill.AllowedTools,
		Metadata:      skill.Metadata,
		Meta:          skill.Meta,
	}
	if skill.Repository != nil {
		svcSkill.Repository = &service.SkillRepository{
			URL:  skill.Repository.URL,
			Type: skill.Repository.Type,
		}
	}
	for _, icon := range skill.Icons {
		svcSkill.Icons = append(svcSkill.Icons, service.SkillIcon{
			Src:   icon.Src,
			Size:  icon.Size,
			Type:  icon.Type,
			Label: icon.Label,
		})
	}
	for _, pkg := range skill.Packages {
		svcSkill.Packages = append(svcSkill.Packages, service.SkillPackage{
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
	if skill.Metadata != nil {
		svcSkill.Metadata = skill.Metadata
	}
	if skill.Meta != nil {
		svcSkill.Meta = skill.Meta
	}
	return svcSkill
}

// deleteVersion handles DELETE /registry/{registryName}/v0.1/x/dev.toolhive/skills/{namespace}/{name}/versions/{version}
//
// @Summary		Delete skill version
// @Description	Delete a specific version of a skill from the registry.
// @Tags		skills
// @Accept		json
// @Produce		json
// @Param		registryName	path		string	true	"Registry name"
// @Param		namespace	path		string	true	"Skill namespace (reverse-DNS)"
// @Param		name			path		string	true	"Skill name"
// @Param		version		path		string	true	"Skill version"
// @Success		204			"No content"
// @Failure		400			{object}	map[string]string	"Bad request"
// @Failure		401			{object}	map[string]string	"Unauthorized"
// @Failure		403			{object}	map[string]string	"Not a managed registry"
// @Failure		404			{object}	map[string]string	"Skill version not found"
// @Failure		500			{object}	map[string]string	"Internal server error"
// @Security	BearerAuth
// @Router		/registry/{registryName}/v0.1/x/dev.toolhive/skills/{namespace}/{name}/versions/{version} [delete]
func (routes *Routes) deleteVersion(w http.ResponseWriter, r *http.Request) {
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

	err = routes.service.DeleteSkillVersion(r.Context(),
		service.WithRegistryName(registryName),
		service.WithNamespace(namespace),
		service.WithName(name),
		service.WithVersion(version),
	)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// parseListSkillsQuery parses and validates list skills query parameters.
func parseListSkillsQuery(r *http.Request) (*ListSkillsQuery, error) {
	q := r.URL.Query()
	query := &ListSkillsQuery{
		Search:    strings.TrimSpace(q.Get("search")),
		Namespace: strings.TrimSpace(q.Get("namespace")),
		Status:    strings.TrimSpace(q.Get("status")),
		Cursor:    strings.TrimSpace(q.Get("cursor")),
		Limit:     defaultListLimit,
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

// validatePublishSkillRequest checks required fields for publish request.
func validatePublishSkillRequest(req *thvregistry.Skill) error {
	if strings.TrimSpace(req.Namespace) == "" {
		return fmt.Errorf("namespace is required")
	}
	if strings.TrimSpace(req.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if strings.TrimSpace(req.Description) == "" {
		return fmt.Errorf("description is required")
	}
	if strings.TrimSpace(req.Version) == "" {
		return fmt.Errorf("version is required")
	}
	return nil
}

// writeServiceError maps service-layer errors to HTTP responses.
func writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrNotFound):
		common.WriteErrorResponse(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, service.ErrRegistryNotFound):
		common.WriteErrorResponse(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, service.ErrNotManagedRegistry):
		common.WriteErrorResponse(w, err.Error(), http.StatusForbidden)
	case errors.Is(err, service.ErrVersionAlreadyExists):
		common.WriteErrorResponse(w, err.Error(), http.StatusConflict)
	default:
		common.WriteErrorResponse(w, "Internal server error", http.StatusInternalServerError)
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
