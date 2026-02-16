// Package skills provides API types and handlers for the dev.toolhive/skills
// extension endpoints (THV-0029).
package skills

import thvregistry "github.com/stacklok/toolhive/pkg/registry/registry"

// ListSkillsQuery holds parsed query parameters for GET /skills (list).
type ListSkillsQuery struct {
	Search    string
	Namespace string
	Status    string // comma-separated for IN filtering, e.g. "active,deprecated"
	Limit     int    // default 50, max 100
	Cursor    string
}

// SkillListMetadata is the metadata object in list responses.
type SkillListMetadata struct {
	Count      int    `json:"count"`
	NextCursor string `json:"nextCursor,omitempty"`
}

// SkillListResponse is the response for GET /skills (list).
type SkillListResponse struct {
	Skills   []thvregistry.Skill `json:"skills"`
	Metadata SkillListMetadata   `json:"metadata"`
}
