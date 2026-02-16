// Package skills provides API types and handlers for the dev.toolhive/skills
// extension endpoints (THV-0029).
package skills

import (
	"encoding/json"
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
// All handlers return 501 Not Implemented; request parsing is performed and
// 400 Bad Request is returned on parse failure.
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
// @Description	List skills in a registry (paginated, latest versions). Not implemented.
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
// @Failure		501			{object}	map[string]string	"Not implemented"
// @Security	BearerAuth
// @Router		/registry/{registryName}/v0.1/x/dev.toolhive/skills [get]
func (*Routes) listSkills(w http.ResponseWriter, r *http.Request) {
	if _, err := common.GetAndValidateURLParam(r, "registryName"); err != nil {
		common.WriteErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	query, err := parseListSkillsQuery(r)
	if err != nil {
		common.WriteErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	_ = query // unused until implemented
	common.WriteErrorResponse(w, "Skills list is not implemented", http.StatusNotImplemented)
}

// getLatestVersion handles GET /registry/{registryName}/v0.1/x/dev.toolhive/skills/{namespace}/{name}
//
// @Summary		Get latest skill version
// @Description	Get the latest version of a skill by namespace and name. Not implemented.
// @Tags		skills
// @Accept		json
// @Produce		json
// @Param		registryName	path		string	true	"Registry name"
// @Param		namespace	path		string	true	"Skill namespace (reverse-DNS)"
// @Param		name			path		string	true	"Skill name"
// @Success		200				{object}	thvregistry.Skill	"Skill details"
// @Failure		400				{object}	map[string]string	"Bad request"
// @Failure		404				{object}	map[string]string	"Skill not found"
// @Failure		501				{object}	map[string]string	"Not implemented"
// @Security	BearerAuth
// @Router		/registry/{registryName}/v0.1/x/dev.toolhive/skills/{namespace}/{name} [get]
func (*Routes) getLatestVersion(w http.ResponseWriter, r *http.Request) {
	if _, err := common.GetAndValidateURLParam(r, "registryName"); err != nil {
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
	_, _ = namespace, name
	common.WriteErrorResponse(w, "Get latest skill version is not implemented", http.StatusNotImplemented)
}

// listVersions handles GET /registry/{registryName}/v0.1/x/dev.toolhive/skills/{namespace}/{name}/versions
//
// @Summary		List skill versions
// @Description	List all versions of a skill. Not implemented.
// @Tags		skills
// @Accept		json
// @Produce		json
// @Param		registryName	path		string	true	"Registry name"
// @Param		namespace	path		string	true	"Skill namespace (reverse-DNS)"
// @Param		name			path		string	true	"Skill name"
// @Success		200				{object}	SkillListResponse	"List of skill versions"
// @Failure		400				{object}	map[string]string	"Bad request"
// @Failure		404				{object}	map[string]string	"Skill not found"
// @Failure		501				{object}	map[string]string	"Not implemented"
// @Security	BearerAuth
// @Router		/registry/{registryName}/v0.1/x/dev.toolhive/skills/{namespace}/{name}/versions [get]
func (*Routes) listVersions(w http.ResponseWriter, r *http.Request) {
	if _, err := common.GetAndValidateURLParam(r, "registryName"); err != nil {
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
	_, _ = namespace, name
	common.WriteErrorResponse(w, "List skill versions is not implemented", http.StatusNotImplemented)
}

// getVersion handles GET /registry/{registryName}/v0.1/x/dev.toolhive/skills/{namespace}/{name}/versions/{version}
//
// @Summary		Get specific skill version
// @Description	Get a specific version of a skill. Not implemented.
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
// @Failure		501				{object}	map[string]string	"Not implemented"
// @Security	BearerAuth
// @Router		/registry/{registryName}/v0.1/x/dev.toolhive/skills/{namespace}/{name}/versions/{version} [get]
func (*Routes) getVersion(w http.ResponseWriter, r *http.Request) {
	if _, err := common.GetAndValidateURLParam(r, "registryName"); err != nil {
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
	_, _, _ = namespace, name, version
	common.WriteErrorResponse(w, "Get skill version is not implemented", http.StatusNotImplemented)
}

// publishSkill handles POST /registry/{registryName}/v0.1/x/dev.toolhive/skills
//
// @Summary		Publish skill
// @Description	Publish a skill version to the registry. Not implemented.
// @Tags		skills
// @Accept		json
// @Produce		json
// @Param		registryName	path		string				true	"Registry name"
// @Param		body			body		thvregistry.Skill	true	"Skill data"
// @Success		201				{object}	thvregistry.Skill	"Created"
// @Failure		400				{object}	map[string]string	"Bad request"
// @Failure		401				{object}	map[string]string	"Unauthorized"
// @Failure		403				{object}	map[string]string	"Not a managed registry"
// @Failure		501				{object}	map[string]string	"Not implemented"
// @Security	BearerAuth
// @Router		/registry/{registryName}/v0.1/x/dev.toolhive/skills [post]
func (*Routes) publishSkill(w http.ResponseWriter, r *http.Request) {
	if _, err := common.GetAndValidateURLParam(r, "registryName"); err != nil {
		common.WriteErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	if r.Body == nil {
		common.WriteErrorResponse(w, "Request body is required", http.StatusBadRequest)
		return
	}

	var body thvregistry.Skill
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		common.WriteErrorResponse(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	if err := validatePublishSkillRequest(&body); err != nil {
		common.WriteErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	common.WriteErrorResponse(w, "Publish skill is not implemented", http.StatusNotImplemented)
}

// deleteVersion handles DELETE /registry/{registryName}/v0.1/x/dev.toolhive/skills/{namespace}/{name}/versions/{version}
//
// @Summary		Delete skill version
// @Description	Delete a specific version of a skill from the registry. Not implemented.
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
// @Failure		501			{object}	map[string]string	"Not implemented"
// @Security	BearerAuth
// @Router		/registry/{registryName}/v0.1/x/dev.toolhive/skills/{namespace}/{name}/versions/{version} [delete]
func (*Routes) deleteVersion(w http.ResponseWriter, r *http.Request) {
	if _, err := common.GetAndValidateURLParam(r, "registryName"); err != nil {
		common.WriteErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}
	if _, err := common.GetAndValidateURLParam(r, "namespace"); err != nil {
		common.WriteErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}
	if _, err := common.GetAndValidateURLParam(r, "name"); err != nil {
		common.WriteErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}
	if _, err := common.GetAndValidateURLParam(r, "version"); err != nil {
		common.WriteErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}
	common.WriteErrorResponse(w, "Delete skill version is not implemented", http.StatusNotImplemented)
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
