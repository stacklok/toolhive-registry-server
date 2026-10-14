package formats

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
	"fmt"

	thvregistry "github.com/stacklok/toolhive-core/registry/types"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
)

// DecodeUpstream checks original JSON before validating and decoding a ToolHive
// upstream registry snapshot. It rejects duplicate object names (including escaped
// equivalents), invalid UTF-8, malformed JSON and trailing values without changing
// data. Core v0.0.50 supplies the bundled draft-7 MCP 2025-12-11 server schema
// (not exact parity with upstream registry v1.8.1) and mandatory ToolHive extension
// policy: publisher metadata namespaces must contain objects, not scalars.
// Dynamic metadata numbers decode as json.Number; core's extension validation
// still parses float64 and rejects overflow such as 1e400. The typed projection
// drops unknown fields, so this is not lossless storage. Imports require at least
// one server, even though an empty reconciled snapshot can be valid elsewhere.
// See docs/registry-formats.md for dependency limitations.
func DecodeUpstream(data []byte) (*model.Snapshot, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("data cannot be empty")
	}
	if ValidateRawJSON(data) != nil {
		return nil, fmt.Errorf("invalid upstream registry JSON")
	}
	if err := thvregistry.ValidateUpstreamRegistryBytes(data); err != nil {
		return nil, err
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

// ValidateRawJSON rejects malformed JSON, duplicate names, invalid UTF-8, and
// string escapes that do not decode to valid Unicode code points. It validates
// raw JSON only; callers remain responsible for schema validation.
func ValidateRawJSON(data []byte) error {
	if !jsontext.Value(data).IsValid(
		jsontext.AllowDuplicateNames(false),
		jsontext.AllowInvalidUTF8(false),
	) {
		return fmt.Errorf("invalid JSON")
	}
	return nil
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
