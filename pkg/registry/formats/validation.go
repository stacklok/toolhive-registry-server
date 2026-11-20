package formats

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"

	thvregistry "github.com/stacklok/toolhive-core/registry/types"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/internal/digestset"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
)

// upstreamSchemaPassed remembers documents that passed the upstream schema.
// toolhive-core compiles the schema on every call, and an unchanged file or Git
// source re-validates the same bytes on every resync.
var upstreamSchemaPassed = digestset.New(64)

// DecodeUpstream validates and decodes an upstream registry snapshot. It uses the
// existing core validator and preserves encoding/json decoding semantics while
// retaining dynamic metadata numbers. The import format requires at least one
// server, even though an empty reconciled snapshot can be valid elsewhere.
// This is v1.5.4's acceptance policy for file and Git sources.
func DecodeUpstream(data []byte) (*model.Snapshot, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("data cannot be empty")
	}
	if !json.Valid(data) {
		return nil, fmt.Errorf("invalid upstream registry JSON")
	}
	digest := sha256.Sum256(data)
	if !upstreamSchemaPassed.Contains(digest) {
		if err := thvregistry.ValidateUpstreamRegistryBytes(data); err != nil {
			return nil, err
		}
		upstreamSchemaPassed.Add(digest)
	}
	var registry model.Snapshot
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&registry); err != nil {
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
