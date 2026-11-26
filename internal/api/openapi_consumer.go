// Code below exists only to document the consumer API for swag; the routes are
// served by pkg/registry/httpapi, mounted in server.go. Keep these annotations
// in step with the library's consumer routes.
//
//go:build ignore

package api

import (
	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	thvregistry "github.com/stacklok/toolhive-core/registry/types"
)

// SkillListMetadata is the pagination metadata of a skill list.
type SkillListMetadata struct {
	Count      int    `json:"count"`
	NextCursor string `json:"nextCursor,omitempty"`
}

// SkillListResponse is a page of skills.
type SkillListResponse struct {
	Skills   []thvregistry.Skill `json:"skills"`
	Metadata SkillListMetadata   `json:"metadata"`
}

// PluginListMetadata is the pagination metadata of a plugin list.
type PluginListMetadata struct {
	Count      int    `json:"count"`
	NextCursor string `json:"nextCursor,omitempty"`
}

// PluginListResponse is a page of plugins.
type PluginListResponse struct {
	Plugins  []thvregistry.Plugin `json:"plugins"`
	Metadata PluginListMetadata   `json:"metadata"`
}

var _ = upstreamv0.ServerResponse{}

// listServersWithRegistryName handles GET /{registryName}/v0.1/servers
//
// @Summary		List servers in specific registry
// @Description	Get a list of available servers from a specific registry
// @Tags		registry
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
func docConsumerRoute1() {}

// listVersionsWithRegistryName handles GET /{registryName}/v0.1/servers/{serverName}/versions
//
// @Summary		List all versions of an MCP server in specific registry
// @Description	Returns all available versions for a specific MCP server from a specific registry
// @Tags		registry
// @Produce		json
// @Param		registryName	path	string	true	"Registry name"
// @Param		serverName	path		string	true	"URL-encoded server name (e.g., \"com.example%2Fmy-server\")"
// @Success		200		{object}	upstreamv0.ServerListResponse	"A list of all versions for the server"
// @Failure		400		{object}	map[string]string	"Bad request"
// @Failure		401		{object}	map[string]string	"Unauthorized"
// @Failure		404		{object}	map[string]string	"Server not found"
// @Security	BearerAuth
// @Router		/registry/{registryName}/v0.1/servers/{serverName}/versions [get]
func docConsumerRoute2() {}

// getVersionWithRegistryName handles GET /{registryName}/v0.1/servers/{serverName}/versions/{version}
//
// @Summary		Get specific MCP server version in specific registry
// @Description	Returns detailed information about a specific version of an MCP server from a specific registry.
// @Description	Use the special version `latest` to get the latest version.
// @Tags		registry
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
func docConsumerRoute3() {}

// listSkills handles GET /registry/{registryName}/v0.1/x/dev.toolhive/skills
//
// @Summary		List skills in registry
// @Description	List skills in a registry (paginated, latest versions).
// @Tags		skills
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
func docConsumerRoute4() {}

// getLatestVersion handles GET /registry/{registryName}/v0.1/x/dev.toolhive/skills/{namespace}/{name}
//
// @Summary		Get latest skill version
// @Description	Get the latest version of a skill by namespace and name.
// @Tags		skills
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
func docConsumerRoute5() {}

// listVersions handles GET /registry/{registryName}/v0.1/x/dev.toolhive/skills/{namespace}/{name}/versions
//
// @Summary		List skill versions
// @Description	List all versions of a skill.
// @Tags		skills
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
func docConsumerRoute6() {}

// getVersion handles GET /registry/{registryName}/v0.1/x/dev.toolhive/skills/{namespace}/{name}/versions/{version}
//
// @Summary		Get specific skill version
// @Description	Get a specific version of a skill.
// @Tags		skills
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
func docConsumerRoute7() {}

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
func docConsumerRoute8() {}

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
func docConsumerRoute9() {}

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
func docConsumerRoute10() {}

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
func docConsumerRoute11() {}
