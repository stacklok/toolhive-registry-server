// Package registry provides the unified internal registry format.
//
// UpstreamRegistry is the single source of truth for registry data, storing servers
// in upstream MCP ServerJSON format while maintaining ToolHive-compatible metadata
// fields for backward compatibility and versioning.
package registry

import (
	"fmt"

	"github.com/stacklok/toolhive/pkg/registry/converters"
	toolhivetypes "github.com/stacklok/toolhive/pkg/registry/types"
)

// ToToolhive converts UpstreamRegistry back to ToolHive Registry format.
// Used for backward compatibility with v0 API.
func ToToolhive(upstreamRegistry *toolhivetypes.UpstreamRegistry) (*toolhivetypes.Registry, error) {
	if upstreamRegistry == nil {
		return nil, fmt.Errorf("upstream registry cannot be nil")
	}

	toolhiveReg := &toolhivetypes.Registry{
		Version:       upstreamRegistry.Version,
		LastUpdated:   upstreamRegistry.LastUpdated,
		Servers:       make(map[string]*toolhivetypes.ImageMetadata),
		RemoteServers: make(map[string]*toolhivetypes.RemoteServerMetadata),
	}

	for i := range upstreamRegistry.Servers {
		serverJSON := &upstreamRegistry.Servers[i]
		name := serverJSON.Name

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
