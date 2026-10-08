package formats

import (
	"encoding/json"
	"fmt"

	thvregistry "github.com/stacklok/toolhive-core/registry/types"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
)

// DecodeUpstream validates and decodes an upstream registry snapshot. The
// existing import format requires at least one server, even though an empty
// reconciled snapshot can be valid in other contexts.
func DecodeUpstream(data []byte) (*model.Snapshot, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("data cannot be empty")
	}
	if err := thvregistry.ValidateUpstreamRegistryBytes(data); err != nil {
		return nil, err
	}
	var registry model.Snapshot
	if err := json.Unmarshal(data, &registry); err != nil {
		return nil, fmt.Errorf("failed to parse upstream registry format: %w", err)
	}
	if len(registry.Data.Servers) == 0 {
		return nil, fmt.Errorf("upstream registry must contain at least one server")
	}
	for i, server := range registry.Data.Servers {
		if server.Name == "" {
			return nil, fmt.Errorf("server at index %d: name is required", i)
		}
		if server.Description == "" {
			return nil, fmt.Errorf("server at index %d (%s): description is required", i, server.Name)
		}
	}
	return &registry, nil
}

// ValidatePublishedSkill retains the existing managed-publication requirements.
func ValidatePublishedSkill(skill *model.Skill) error {
	if skill == nil || skill.Namespace == "" || skill.Name == "" || skill.Version == "" {
		return fmt.Errorf("namespace, name, and version are required")
	}
	return nil
}

// ValidatePublishedPlugin retains the existing managed-publication requirements.
func ValidatePublishedPlugin(plugin *model.Plugin) error {
	if plugin == nil || plugin.Namespace == "" || plugin.Name == "" || plugin.Version == "" {
		return fmt.Errorf("namespace, name, and version are required")
	}
	return nil
}
