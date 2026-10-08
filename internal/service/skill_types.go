package service

import "github.com/stacklok/toolhive-registry-server/pkg/registry/model"

// Skill and its related types are retained as aliases for existing service callers.
type Skill = model.Skill

// SkillRepository is retained for service callers.
type SkillRepository = model.SkillRepository

// SkillPackage is retained for service callers.
type SkillPackage = model.SkillPackage

// SkillIcon is retained for service callers.
type SkillIcon = model.SkillIcon

// ListSkillsResult contains the result of a ListSkills operation with pagination.
type ListSkillsResult struct {
	Skills     []*Skill `json:"skills"`
	NextCursor string   `json:"-"`
}
