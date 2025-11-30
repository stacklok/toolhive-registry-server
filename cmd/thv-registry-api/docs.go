// Package docs provides OpenAPI documentation for the ToolHive Registry API
//
//	@title			ToolHive Registry API
//	@version		0.1
//	@description	API for accessing MCP server registry data and deployed server information
//	@description	This API provides endpoints to query the MCP (Model Context Protocol) server registry,
//	@description	get information about available servers, and check the status of deployed servers.
//	@description
//	@description	Authentication is required by default. Use Bearer token authentication with a valid
//	@description	OAuth/OIDC access token. The /.well-known/oauth-protected-resource endpoint provides
//	@description	OAuth discovery metadata (RFC 9728).
//
//	@contact.url	https://github.com/stacklok/toolhive
//
//	@license.name	Apache 2.0
//	@license.url	http://www.apache.org/licenses/LICENSE-2.0.html
//
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description OAuth 2.0 Bearer token authentication. Format: "Bearer {token}"
//
//	@tag.name	registry
//	@tag.description	Registry metadata and information
//
//	@tag.name	servers
//	@tag.description	Server discovery and metadata
//
//	@tag.name	deployed-servers
//	@tag.description	Deployed server status and information
//
//	@tag.name	system
//	@tag.description	System health and version information
package main
