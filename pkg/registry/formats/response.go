package formats

import (
	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	thvregistry "github.com/stacklok/toolhive-core/registry/types"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
)

// ExtensionMetadata is the existing skills/plugins response metadata. Cursor
// interpretation and result selection belong to the caller.
type ExtensionMetadata struct {
	Count      int    `json:"count"`
	NextCursor string `json:"nextCursor,omitempty"`
}

// SkillListResponse is the dev.toolhive/skills extension response.
type SkillListResponse struct {
	Skills   []thvregistry.Skill `json:"skills"`
	Metadata ExtensionMetadata   `json:"metadata"`
}

// PluginListResponse is the dev.toolhive/plugins extension response.
type PluginListResponse struct {
	Plugins  []thvregistry.Plugin `json:"plugins"`
	Metadata ExtensionMetadata    `json:"metadata"`
}

// ServerPayload wraps a selected server in the upstream response envelope.
func ServerPayload(server *model.Server) upstreamv0.ServerResponse {
	return upstreamv0.ServerResponse{Server: *server, Meta: upstreamv0.ResponseMeta{}}
}

// ServerPage formats exactly the supplied server versions and opaque cursor.
func ServerPage(servers []*model.Server, cursor string) upstreamv0.ServerListResponse {
	responses := make([]upstreamv0.ServerResponse, len(servers))
	for i, server := range servers {
		responses[i] = ServerPayload(server)
	}
	return upstreamv0.ServerListResponse{
		Servers:  responses,
		Metadata: upstreamv0.Metadata{NextCursor: cursor, Count: len(servers)},
	}
}

// SkillPayload converts catalog metadata to the existing ToolHive wire payload.
func SkillPayload(s *model.Skill) thvregistry.Skill {
	resp := thvregistry.Skill{
		Namespace: s.Namespace, Name: s.Name, Description: s.Description,
		Version: s.Version, Status: s.Status, Title: s.Title,
		License: s.License, Compatibility: s.Compatibility,
		AllowedTools: s.AllowedTools, Metadata: s.Metadata, Meta: s.Meta,
		Provenance: s.Provenance,
	}
	if s.Repository != nil {
		resp.Repository = &thvregistry.SkillRepository{URL: s.Repository.URL, Type: s.Repository.Type}
	}
	for _, icon := range s.Icons {
		resp.Icons = append(resp.Icons, thvregistry.SkillIcon{
			Src: icon.Src, Size: icon.Size, Type: icon.Type, Label: icon.Label,
		})
	}
	for _, pkg := range s.Packages {
		resp.Packages = append(resp.Packages, thvregistry.SkillPackage{
			RegistryType: pkg.RegistryType, Identifier: pkg.Identifier, Digest: pkg.Digest,
			MediaType: pkg.MediaType, URL: pkg.URL, Ref: pkg.Ref, Commit: pkg.Commit, Subfolder: pkg.Subfolder,
		})
	}
	return resp
}

// SkillPage formats exactly the supplied skill versions and opaque cursor.
func SkillPage(skills []*model.Skill, cursor string) SkillListResponse {
	result := make([]thvregistry.Skill, len(skills))
	for i, skill := range skills {
		result[i] = SkillPayload(skill)
	}
	return SkillListResponse{Skills: result, Metadata: ExtensionMetadata{Count: len(skills), NextCursor: cursor}}
}

// PluginPayload converts catalog metadata to the existing ToolHive wire payload.
func PluginPayload(p *model.Plugin) thvregistry.Plugin {
	resp := thvregistry.Plugin{
		Namespace: p.Namespace, Name: p.Name, Description: p.Description,
		Version: p.Version, Status: p.Status, Title: p.Title,
		License: p.License, Metadata: p.Metadata, Meta: p.Meta,
	}
	if p.Repository != nil {
		resp.Repository = &thvregistry.SkillRepository{URL: p.Repository.URL, Type: p.Repository.Type}
	}
	for _, icon := range p.Icons {
		resp.Icons = append(resp.Icons, thvregistry.SkillIcon{
			Src: icon.Src, Size: icon.Size, Type: icon.Type, Label: icon.Label,
		})
	}
	for _, pkg := range p.Packages {
		resp.Packages = append(resp.Packages, thvregistry.SkillPackage{
			RegistryType: pkg.RegistryType, Identifier: pkg.Identifier, Digest: pkg.Digest,
			MediaType: pkg.MediaType, URL: pkg.URL, Ref: pkg.Ref, Commit: pkg.Commit, Subfolder: pkg.Subfolder,
		})
	}
	return resp
}

// PluginPage formats exactly the supplied plugin versions and opaque cursor.
func PluginPage(plugins []*model.Plugin, cursor string) PluginListResponse {
	result := make([]thvregistry.Plugin, len(plugins))
	for i, plugin := range plugins {
		result[i] = PluginPayload(plugin)
	}
	return PluginListResponse{Plugins: result, Metadata: ExtensionMetadata{Count: len(plugins), NextCursor: cursor}}
}
