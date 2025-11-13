// Package registry provides the unified internal registry format.
//
// ServerRegistry is the single source of truth for registry data, storing servers
// in upstream MCP ServerJSON format while maintaining ToolHive-compatible metadata
// fields for backward compatibility and versioning.
package registry

import (
	"fmt"
	"strings"
	"time"

	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/stacklok/toolhive/pkg/registry/converters"
	toolhivetypes "github.com/stacklok/toolhive/pkg/registry/types"
)

// ServerRegistry is the unified internal registry format.
// It stores servers in upstream ServerJSON format while maintaining
// ToolHive-compatible metadata fields for backward compatibility.
type ServerRegistry struct {
	// Version is the schema version (ToolHive compatibility)
	Version string `json:"version"`

	// LastUpdated is the timestamp when registry was last updated (ToolHive compatibility)
	LastUpdated string `json:"last_updated"`

	// Servers contains the server definitions in upstream MCP format
	Servers []upstreamv0.ServerJSON `json:"servers"`
}

// NewServerRegistryFromUpstream creates a ServerRegistry from upstream ServerJSON array.
// This is used when ingesting data from upstream MCP Registry API endpoints.
func NewServerRegistryFromUpstream(servers []upstreamv0.ServerJSON) *ServerRegistry {
	return &ServerRegistry{
		Version:     "1.0.0",
		LastUpdated: time.Now().Format(time.RFC3339),
		Servers:     servers,
	}
}

// NewServerRegistryFromToolhive creates a ServerRegistry from ToolHive Registry.
// This converts ToolHive format to upstream ServerJSON using the converters package.
// Used when ingesting data from ToolHive-format sources (Git, File, API).
func NewServerRegistryFromToolhive(toolhiveReg *toolhivetypes.Registry) (*ServerRegistry, error) {
	if toolhiveReg == nil {
		return nil, fmt.Errorf("toolhive registry cannot be nil")
	}

	servers := make([]upstreamv0.ServerJSON, 0, len(toolhiveReg.Servers)+len(toolhiveReg.RemoteServers))

	// Convert container servers using converters package
	for name, imgMeta := range toolhiveReg.Servers {
		serverJSON, err := converters.ImageMetadataToServerJSON(name, imgMeta)
		if err != nil {
			return nil, fmt.Errorf("failed to convert server %s: %w", name, err)
		}
		servers = append(servers, *serverJSON)
	}

	// Convert remote servers using converters package
	for name, remoteMeta := range toolhiveReg.RemoteServers {
		serverJSON, err := converters.RemoteServerMetadataToServerJSON(name, remoteMeta)
		if err != nil {
			return nil, fmt.Errorf("failed to convert remote server %s: %w", name, err)
		}
		servers = append(servers, *serverJSON)
	}

	return &ServerRegistry{
		Version:     toolhiveReg.Version,
		LastUpdated: toolhiveReg.LastUpdated,
		Servers:     servers,
	}, nil
}

// ToToolhive converts ServerRegistry back to ToolHive Registry format.
// Used for backward compatibility with v0 API.
func (sr *ServerRegistry) ToToolhive() (*toolhivetypes.Registry, error) {
	if sr == nil {
		return nil, fmt.Errorf("server registry cannot be nil")
	}

	toolhiveReg := &toolhivetypes.Registry{
		Version:       sr.Version,
		LastUpdated:   sr.LastUpdated,
		Servers:       make(map[string]*toolhivetypes.ImageMetadata),
		RemoteServers: make(map[string]*toolhivetypes.RemoteServerMetadata),
	}

	for i := range sr.Servers {
		serverJSON := &sr.Servers[i]
		name := extractSimpleName(serverJSON.Name)

		// Detect server type by presence of packages vs remotes
		if len(serverJSON.Packages) > 0 {
			// Container server
			imgMeta, err := converters.ServerJSONToImageMetadata(serverJSON)
			if err != nil {
				return nil, fmt.Errorf("failed to convert server %s: %w", serverJSON.Name, err)
			}
			toolhiveReg.Servers[name] = imgMeta
		} else if len(serverJSON.Remotes) > 0 {
			// Remote server
			remoteMeta, err := converters.ServerJSONToRemoteServerMetadata(serverJSON)
			if err != nil {
				return nil, fmt.Errorf("failed to convert remote server %s: %w", serverJSON.Name, err)
			}
			toolhiveReg.RemoteServers[name] = remoteMeta
		}
		// Note: Servers with neither packages nor remotes are skipped
		// This shouldn't happen with valid ServerJSON data
	}

	return toolhiveReg, nil
}

// GetServerByName retrieves a server by its name.
// Supports both reverse-DNS format (e.g., "io.github.user/server") and simple names (e.g., "server").
func (sr *ServerRegistry) GetServerByName(name string) (*upstreamv0.ServerJSON, bool) {
	if sr == nil {
		return nil, false
	}

	for i := range sr.Servers {
		serverName := sr.Servers[i].Name
		if serverName == name || extractSimpleName(serverName) == name {
			return &sr.Servers[i], true
		}
	}
	return nil, false
}

// extractSimpleName extracts the simple server name from reverse-DNS format.
// Examples:
//   - "io.github.user/server" -> "server"
//   - "com.example/my-server" -> "my-server"
//   - "simple-name" -> "simple-name" (no change if not reverse-DNS)
func extractSimpleName(reverseDNS string) string {
	idx := strings.LastIndex(reverseDNS, "/")
	if idx >= 0 && idx < len(reverseDNS)-1 {
		return reverseDNS[idx+1:]
	}
	return reverseDNS
}
