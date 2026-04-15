package filtering

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	toolhivetypes "github.com/stacklok/toolhive-core/registry/types"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/registry"
)

// FilterService coordinates name and tag filtering to apply registry filters
type FilterService interface {
	// ApplyFilters filters the registry based on filter configuration
	ApplyFilters(
		ctx context.Context,
		reg *toolhivetypes.UpstreamRegistry,
		filter *config.FilterConfig,
	) (*toolhivetypes.UpstreamRegistry, error)
}

// defaultFilterService implements filtering coordination using name and tag filters
type defaultFilterService struct {
	nameFilter NameFilter
	tagFilter  TagFilter
}

// NewDefaultFilterService creates a new defaultFilterService with default filter implementations
func NewDefaultFilterService() FilterService {
	return &defaultFilterService{
		nameFilter: NewDefaultNameFilter(),
		tagFilter:  NewDefaultTagFilter(),
	}
}

// NewFilterService creates a new defaultFilterService with custom filter implementations
func NewFilterService(nameFilter NameFilter, tagFilter TagFilter) FilterService {
	return &defaultFilterService{
		nameFilter: nameFilter,
		tagFilter:  tagFilter,
	}
}

// ApplyFilters filters the registry based on filter configuration
//
// The filtering process:
// 1. If no filter is specified, return the original registry unchanged
// 2. Create a new registry with the same metadata but empty server maps
// 3. For each server (both container and remote), apply name and tag filtering
// 4. Only include servers that pass both name and tag filters
// 5. Return the filtered registry
func (s *defaultFilterService) ApplyFilters(
	_ context.Context,
	reg *toolhivetypes.UpstreamRegistry,
	filter *config.FilterConfig) (*toolhivetypes.UpstreamRegistry, error) {
	// If no filter is specified, return original registry
	if filter == nil {
		slog.Info("No filter specified, returning original registry")
		return reg, nil
	}

	slog.Info("Applying registry filters",
		"originalServerCount", len(reg.Data.Servers))

	// Create a new filtered registry with same metadata
	filteredRegistry := &toolhivetypes.UpstreamRegistry{
		Schema:  reg.Schema,
		Version: reg.Version,
		Meta: toolhivetypes.UpstreamMeta{
			LastUpdated: reg.Meta.LastUpdated,
		},
		Data: toolhivetypes.UpstreamData{
			Servers: make([]upstreamv0.ServerJSON, 0),
			Groups:  reg.Data.Groups, // Preserve groups if any
			Skills:  make([]toolhivetypes.Skill, 0),
		},
	}

	// Extract filter criteria
	var nameInclude, nameExclude, tagInclude, tagExclude []string
	if filter.Names != nil {
		nameInclude = filter.Names.Include
		nameExclude = filter.Names.Exclude
	}
	if filter.Tags != nil {
		tagInclude = filter.Tags.Include
		tagExclude = filter.Tags.Exclude
	}

	includedCount := 0
	excludedCount := 0

	// Filter container servers
	for _, server := range reg.Data.Servers {
		serverName := server.Name
		tags := registry.ExtractTags(&server)
		included, reason := s.shouldIncludeServerWithReason(
			serverName,
			tags,
			nameInclude,
			nameExclude,
			tagInclude,
			tagExclude,
		)
		if included {
			filteredRegistry.Data.Servers = append(filteredRegistry.Data.Servers, server)
			includedCount++
			slog.Info("Including container server",
				"name", serverName,
				"tags", tags,
				"reason", reason)
		} else {
			excludedCount++
			slog.Info("Excluding container server",
				"name", serverName,
				"tags", tags,
				"reason", reason)
		}
	}

	// Filter skills by name (namespace/name matches server name patterns)
	skillIncluded := 0
	skillExcluded := 0
	for _, skill := range reg.Data.Skills {
		skillName := skill.Namespace + "/" + skill.Name
		included, reason := s.nameFilter.ShouldInclude(skillName, nameInclude, nameExclude)
		if included {
			filteredRegistry.Data.Skills = append(filteredRegistry.Data.Skills, skill)
			skillIncluded++
			slog.Info("Including skill",
				"name", skillName,
				"reason", reason)
		} else {
			skillExcluded++
			slog.Info("Excluding skill",
				"name", skillName,
				"reason", reason)
		}
	}

	slog.Info("Registry filtering completed",
		"includedServers", includedCount,
		"excludedServers", excludedCount,
		"filteredServerCount", len(filteredRegistry.Data.Servers),
		"includedSkills", skillIncluded,
		"excludedSkills", skillExcluded,
		"filteredSkillCount", len(filteredRegistry.Data.Skills))

	return filteredRegistry, nil
}

// shouldIncludeServerWithReason determines if a server should be included and provides detailed reasoning
// Both name and tag filters must pass for a server to be included
func (s *defaultFilterService) shouldIncludeServerWithReason(
	serverName string,
	serverTags []string,
	nameInclude, nameExclude, tagInclude, tagExclude []string) (bool, string) {
	// Apply name filtering first
	nameIncluded, nameReason := s.nameFilter.ShouldInclude(serverName, nameInclude, nameExclude)
	if !nameIncluded {
		return false, fmt.Sprintf("name filter: %s", nameReason)
	}

	// Apply tag filtering
	tagIncluded, tagReason := s.tagFilter.ShouldInclude(serverTags, tagInclude, tagExclude)
	if !tagIncluded {
		return false, fmt.Sprintf("tag filter: %s", tagReason)
	}

	// Both filters passed - determine the inclusion reason
	inclusionReasons := []string{}
	if len(nameInclude) > 0 || len(nameExclude) > 0 {
		inclusionReasons = append(inclusionReasons, fmt.Sprintf("name filter: %s", nameReason))
	}
	if len(tagInclude) > 0 || len(tagExclude) > 0 {
		inclusionReasons = append(inclusionReasons, fmt.Sprintf("tag filter: %s", tagReason))
	}

	if len(inclusionReasons) == 0 {
		return true, "no filters specified, default include"
	}

	return true, "passed all filters: " + strings.Join(inclusionReasons, " AND ")
}
