// Package skills provides API types and handlers for the dev.toolhive/skills
// extension endpoints (THV-0029).
package skills

import "github.com/stacklok/toolhive-registry-server/pkg/registry/formats"

// ListSkillsQuery holds parsed query parameters for GET /skills (list).
type ListSkillsQuery struct {
	Search string
	Status string // comma-separated for IN filtering, e.g. "active,deprecated"
	Limit  int    // default 50, max 100
	Cursor string
}

// SkillListMetadata is the metadata object in list responses.
type SkillListMetadata = formats.ExtensionMetadata

// SkillListResponse is the response for GET /skills (list).
type SkillListResponse = formats.SkillListResponse
