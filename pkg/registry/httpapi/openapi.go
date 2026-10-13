//go:build ignore

package httpapi

// Swagger indexes these declarations only, not legacy routers. Runtime dispatch
// remains descriptor-driven; OpenAPI parity tests check the actual route inventory.

import (
	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	thvregistry "github.com/stacklok/toolhive-core/registry/types"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/formats"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
)

// @Summary List server versions
// @Description Nonpersonalized upstream page. Requires Cedar catalog.read. All versions are included unless version is specified.
// @Tags servers
// @Produce json
// @Security BearerAuth
// @Param registryName path string true "Named view"
// @Param cursor query string false "Opaque continuation cursor"
// @Param limit query int false "Page size; values above 1000 are clamped" minimum(1) default(30)
// @Param search query string false "Substring search"
// @Param version query string false "Exact version or latest"
// @Param updated_since query string false "RFC3339 timestamp"
// @Success 200 {object} upstreamv0.ServerListResponse
// @Failure 400,401,403,404,500 {object} ErrorResponse
// @Router /registry/{registryName}/v0.1/servers [get]
func docServers() {}

// @Summary List a server's version history
// @Description Bounded to 1000 versions; no wire continuation or query pagination.
// @Tags servers
// @Produce json
// @Security BearerAuth
// @Param registryName path string true "Named view"
// @Param serverName path string true "URL-encoded reverse-DNS server name"
// @Success 200 {object} upstreamv0.ServerListResponse
// @Failure 400,401,403,404,500 {object} ErrorResponse
// @Router /registry/{registryName}/v0.1/servers/{serverName}/versions [get]
func docServerHistory() {}

// @Summary Get a server version
// @Tags servers
// @Produce json
// @Security BearerAuth
// @Param registryName path string true "Named view"
// @Param serverName path string true "URL-encoded reverse-DNS server name"
// @Param version path string true "Exact version or latest"
// @Success 200 {object} upstreamv0.ServerResponse
// @Failure 400,401,403,404,500 {object} ErrorResponse
// @Router /registry/{registryName}/v0.1/servers/{serverName}/versions/{version} [get]
func docServerVersion() {}

// @Summary List skill versions
// @Description All versions; status, version and updated_since query fields are not selection filters.
// @Tags skills
// @Produce json
// @Security BearerAuth
// @Param registryName path string true "Named view"
// @Param cursor query string false "Opaque continuation cursor"
// @Param limit query int false "Page size" minimum(1) maximum(100) default(50)
// @Param search query string false "Substring search"
// @Success 200 {object} formats.SkillListResponse
// @Failure 400,401,403,404,500 {object} ErrorResponse
// @Router /registry/{registryName}/v0.1/x/dev.toolhive/skills [get]
// @Router /registry/{registryName}/v0.1/x/dev.toolhive/skills/ [get]
func docSkills() {}

// @Summary Get the latest skill version
// @Tags skills
// @Produce json
// @Security BearerAuth
// @Param registryName path string true "Named view"
// @Param namespace path string true "Skill namespace"
// @Param name path string true "Skill name"
// @Success 200 {object} thvregistry.Skill
// @Failure 400,401,403,404,500 {object} ErrorResponse
// @Router /registry/{registryName}/v0.1/x/dev.toolhive/skills/{namespace}/{name} [get]
func docSkillLatest() {}

// @Summary List a skill's version history
// @Description Bounded to 30 versions with extension continuation metadata; query pagination is not accepted.
// @Tags skills
// @Produce json
// @Security BearerAuth
// @Param registryName path string true "Named view"
// @Param namespace path string true "Skill namespace"
// @Param name path string true "Skill name"
// @Success 200 {object} formats.SkillListResponse
// @Failure 400,401,403,404,500 {object} ErrorResponse
// @Router /registry/{registryName}/v0.1/x/dev.toolhive/skills/{namespace}/{name}/versions [get]
func docSkillHistory() {}

// @Summary Get a skill version
// @Tags skills
// @Produce json
// @Security BearerAuth
// @Param registryName path string true "Named view"
// @Param namespace path string true "Skill namespace"
// @Param name path string true "Skill name"
// @Param version path string true "Exact version or latest"
// @Success 200 {object} thvregistry.Skill
// @Failure 400,401,403,404,500 {object} ErrorResponse
// @Router /registry/{registryName}/v0.1/x/dev.toolhive/skills/{namespace}/{name}/versions/{version} [get]
func docSkillVersion() {}

// @Summary List plugin versions
// @Description All versions; status, version and updated_since query fields are not selection filters.
// @Tags plugins
// @Produce json
// @Security BearerAuth
// @Param registryName path string true "Named view"
// @Param cursor query string false "Opaque continuation cursor"
// @Param limit query int false "Page size" minimum(1) maximum(100) default(50)
// @Param search query string false "Substring search"
// @Success 200 {object} formats.PluginListResponse
// @Failure 400,401,403,404,500 {object} ErrorResponse
// @Router /registry/{registryName}/v0.1/x/dev.toolhive/plugins [get]
// @Router /registry/{registryName}/v0.1/x/dev.toolhive/plugins/ [get]
func docPlugins() {}

// @Summary Get the latest plugin version
// @Tags plugins
// @Produce json
// @Security BearerAuth
// @Param registryName path string true "Named view"
// @Param namespace path string true "Plugin namespace"
// @Param name path string true "Plugin name"
// @Success 200 {object} thvregistry.Plugin
// @Failure 400,401,403,404,500 {object} ErrorResponse
// @Router /registry/{registryName}/v0.1/x/dev.toolhive/plugins/{namespace}/{name} [get]
func docPluginLatest() {}

// @Summary List a plugin's version history
// @Description Bounded to 30 versions with extension continuation metadata; query pagination is not accepted.
// @Tags plugins
// @Produce json
// @Security BearerAuth
// @Param registryName path string true "Named view"
// @Param namespace path string true "Plugin namespace"
// @Param name path string true "Plugin name"
// @Success 200 {object} formats.PluginListResponse
// @Failure 400,401,403,404,500 {object} ErrorResponse
// @Router /registry/{registryName}/v0.1/x/dev.toolhive/plugins/{namespace}/{name}/versions [get]
func docPluginHistory() {}

// @Summary Get a plugin version
// @Tags plugins
// @Produce json
// @Security BearerAuth
// @Param registryName path string true "Named view"
// @Param namespace path string true "Plugin namespace"
// @Param name path string true "Plugin name"
// @Param version path string true "Exact version or latest"
// @Success 200 {object} thvregistry.Plugin
// @Failure 400,401,403,404,500 {object} ErrorResponse
// @Router /registry/{registryName}/v0.1/x/dev.toolhive/plugins/{namespace}/{name}/versions/{version} [get]
func docPluginVersion() {}

// @Summary List source definitions
// @Tags sources
// @Produce json
// @Security BearerAuth
// @Success 200 {object} SourcesResponse
// @Failure 401,403,500,503 {object} ErrorResponse
// @Router /v1/sources [get]
func docSources() {}

// @Summary Get a source definition and status
// @Tags sources
// @Produce json
// @Security BearerAuth
// @Param name path string true "Source name"
// @Success 200 {object} sourceInfo
// @Failure 400,401,403,404,500,503 {object} ErrorResponse
// @Router /v1/sources/{name} [get]
func docSource() {}

// @Summary Create or update a source
// @Description API-owned definitions only; retired top-level claims are rejected, including null. Callback failure returns 503 with committed=true.
// @Tags sources
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param name path string true "Source name"
// @Param source body sourceRequest true "Exactly one source specification"
// @Success 200,201 {object} sourceInfo
// @Failure 400,401,403,404,409,413,500 {object} ErrorResponse
// @Failure 503 {object} CommittedErrorResponse
// @Router /v1/sources/{name} [put]
func docUpsertSource() {}

// @Summary Delete an API-owned source
// @Tags sources
// @Security BearerAuth
// @Param name path string true "Source name"
// @Success 204
// @Failure 400,401,403,404,409,500 {object} ErrorResponse
// @Failure 503 {object} CommittedErrorResponse
// @Router /v1/sources/{name} [delete]
func docDeleteSource() {}

// @Summary List unshadowed source version diagnostics
// @Tags sources
// @Produce json
// @Security BearerAuth
// @Param name path string true "Source name"
// @Success 200 {object} SourceEntriesResponse
// @Failure 400,401,403,404,500 {object} ErrorResponse
// @Router /v1/sources/{name}/entries [get]
func docSourceEntries() {}

// @Summary List named views
// @Tags registries
// @Produce json
// @Security BearerAuth
// @Success 200 {object} ViewsResponse
// @Failure 401,403,500 {object} ErrorResponse
// @Router /v1/registries [get]
func docViews() {}

// @Summary Get a named view
// @Tags registries
// @Produce json
// @Security BearerAuth
// @Param name path string true "View name"
// @Success 200 {object} viewInfo
// @Failure 400,401,403,404,500 {object} ErrorResponse
// @Router /v1/registries/{name} [get]
func docView() {}

// @Summary Create or update a named view
// @Description API-owned definitions only; ordered source references. Retired top-level claims are rejected, including null.
// @Tags registries
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param name path string true "View name"
// @Param view body ViewRequest true "Ordered source references"
// @Success 200,201 {object} viewInfo
// @Failure 400,401,403,404,409,413,500 {object} ErrorResponse
// @Failure 503 {object} CommittedErrorResponse
// @Router /v1/registries/{name} [put]
func docUpsertView() {}

// @Summary Delete an API-owned view
// @Tags registries
// @Security BearerAuth
// @Param name path string true "View name"
// @Success 204
// @Failure 400,401,403,404,409,500 {object} ErrorResponse
// @Failure 503 {object} CommittedErrorResponse
// @Router /v1/registries/{name} [delete]
func docDeleteView() {}

// @Summary List unshadowed view version diagnostics
// @Tags registries
// @Produce json
// @Security BearerAuth
// @Param name path string true "View name"
// @Success 200 {object} ViewEntriesResponse
// @Failure 400,401,403,404,500 {object} ErrorResponse
// @Router /v1/registries/{name}/entries [get]
func docViewEntries() {}

// @Summary Publish one managed entry version
// @Description Exactly one of server, skill or plugin. No top-level claims. Returns that raw payload, not a consumer envelope.
// @Tags entries
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param entry body publishRequest true "One entry payload"
// @Success 201 {object} model.Server
// @Success 201 {object} model.Skill
// @Success 201 {object} model.Plugin
// @Failure 400,401,403,404,409,413,500 {object} ErrorResponse
// @Failure 503 {object} CommittedErrorResponse
// @Router /v1/entries [post]
func docPublish() {}

// @Summary Delete a managed entry version
// @Tags entries
// @Security BearerAuth
// @Param type path string true "Entry kind" Enums(server,skill,plugin)
// @Param name path string true "URL-encoded entry name"
// @Param version path string true "Exact version"
// @Success 204
// @Failure 400,401,403,404,409,500 {object} ErrorResponse
// @Failure 503 {object} CommittedErrorResponse
// @Router /v1/entries/{type}/{name}/versions/{version} [delete]
func docDeleteEntry() {}
