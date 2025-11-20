package registry

import (
	"encoding/json"
	"fmt"
	"time"

	toolhivetypes "github.com/stacklok/toolhive/pkg/registry/registry"
)

// ToolHiveRegistryOption is a function that configures a ToolHive Registry for testing
type ToolHiveRegistryOption func(*toolhivetypes.Registry)

// ImageServerOption is a function that configures an ImageMetadata (OCI server) for testing
type ImageServerOption func(*toolhivetypes.ImageMetadata)

// RemoteServerOption is a function that configures a RemoteServerMetadata for testing
type RemoteServerOption func(*toolhivetypes.RemoteServerMetadata)

// NewTestToolHiveRegistry creates a new ToolHive Registry for testing with default values
// and applies any provided options
func NewTestToolHiveRegistry(opts ...ToolHiveRegistryOption) *toolhivetypes.Registry {
	reg := &toolhivetypes.Registry{
		Version:       "1.0.0",
		LastUpdated:   time.Now().Format(time.RFC3339),
		Servers:       make(map[string]*toolhivetypes.ImageMetadata),
		RemoteServers: make(map[string]*toolhivetypes.RemoteServerMetadata),
	}

	for _, opt := range opts {
		opt(reg)
	}

	return reg
}

// WithToolHiveVersion sets the registry version
func WithToolHiveVersion(version string) ToolHiveRegistryOption {
	return func(reg *toolhivetypes.Registry) {
		reg.Version = version
	}
}

// WithToolHiveLastUpdated sets the registry last updated timestamp
func WithToolHiveLastUpdated(timestamp string) ToolHiveRegistryOption {
	return func(reg *toolhivetypes.Registry) {
		reg.LastUpdated = timestamp
	}
}

// WithImageServer adds an OCI/container image server to the registry
func WithImageServer(name, image string, opts ...ImageServerOption) ToolHiveRegistryOption {
	return func(reg *toolhivetypes.Registry) {
		server := &toolhivetypes.ImageMetadata{
			BaseServerMetadata: toolhivetypes.BaseServerMetadata{
				Name:        name,
				Description: fmt.Sprintf("Test server description for %s", name),
				Tier:        "Community",
				Status:      "Active",
				Transport:   "stdio",
				Tools:       []string{"test_tool"},
				Tags:        []string{"database"},
			},
			Image: image,
		}

		for _, opt := range opts {
			opt(server)
		}

		reg.Servers[name] = server
	}
}

// WithRemoteServerURL adds a remote (HTTP/SSE) server to the registry
func WithRemoteServerURL(name, url string, opts ...RemoteServerOption) ToolHiveRegistryOption {
	return func(reg *toolhivetypes.Registry) {
		server := &toolhivetypes.RemoteServerMetadata{
			BaseServerMetadata: toolhivetypes.BaseServerMetadata{
				Name:        name,
				Description: fmt.Sprintf("Test remote server description for %s", name),
				Tier:        "Community",
				Status:      "Active",
				Transport:   "sse",
				Tools:       []string{"remote_tool"},
			},
			URL: url,
		}

		for _, opt := range opts {
			opt(server)
		}

		reg.RemoteServers[name] = server
	}
}

// ImageServerOption helpers

// WithImageDescription sets the server description
func WithImageDescription(description string) ImageServerOption {
	return func(server *toolhivetypes.ImageMetadata) {
		server.Description = description
	}
}

// WithImageTier sets the server tier
func WithImageTier(tier string) ImageServerOption {
	return func(server *toolhivetypes.ImageMetadata) {
		server.Tier = tier
	}
}

// WithImageStatus sets the server status
func WithImageStatus(status string) ImageServerOption {
	return func(server *toolhivetypes.ImageMetadata) {
		server.Status = status
	}
}

// WithImageTransport sets the server transport
func WithImageTransport(transport string) ImageServerOption {
	return func(server *toolhivetypes.ImageMetadata) {
		server.Transport = transport
	}
}

// WithImageTools sets the server tools
func WithImageTools(tools ...string) ImageServerOption {
	return func(server *toolhivetypes.ImageMetadata) {
		server.Tools = tools
	}
}

// WithImageTags sets the server tags
func WithImageTags(tags ...string) ImageServerOption {
	return func(server *toolhivetypes.ImageMetadata) {
		server.Tags = tags
	}
}

// RemoteServerOption helpers

// WithRemoteDescription sets the remote server description
func WithRemoteDescription(description string) RemoteServerOption {
	return func(server *toolhivetypes.RemoteServerMetadata) {
		server.Description = description
	}
}

// WithRemoteTier sets the remote server tier
func WithRemoteTier(tier string) RemoteServerOption {
	return func(server *toolhivetypes.RemoteServerMetadata) {
		server.Tier = tier
	}
}

// WithRemoteStatus sets the remote server status
func WithRemoteStatus(status string) RemoteServerOption {
	return func(server *toolhivetypes.RemoteServerMetadata) {
		server.Status = status
	}
}

// WithRemoteTransport sets the remote server transport
func WithRemoteTransport(transport string) RemoteServerOption {
	return func(server *toolhivetypes.RemoteServerMetadata) {
		server.Transport = transport
	}
}

// WithRemoteTools sets the remote server tools
func WithRemoteTools(tools ...string) RemoteServerOption {
	return func(server *toolhivetypes.RemoteServerMetadata) {
		server.Tools = tools
	}
}

// WithRemoteTags sets the remote server tags
func WithRemoteTags(tags ...string) RemoteServerOption {
	return func(server *toolhivetypes.RemoteServerMetadata) {
		server.Tags = tags
	}
}

// Helper functions for JSON generation

// ToolHiveRegistryToJSON converts a ToolHive Registry to JSON bytes
func ToolHiveRegistryToJSON(reg *toolhivetypes.Registry) []byte {
	data, err := json.Marshal(reg)
	if err != nil {
		panic(fmt.Sprintf("Failed to marshal ToolHive registry: %v", err))
	}
	return data
}

// ToolHiveRegistryToPrettyJSON converts a ToolHive Registry to pretty-printed JSON bytes
func ToolHiveRegistryToPrettyJSON(reg *toolhivetypes.Registry) []byte {
	data, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		panic(fmt.Sprintf("Failed to marshal ToolHive registry: %v", err))
	}
	return data
}

// Helper functions for common test scenarios

// InvalidJSON returns intentionally malformed JSON for testing error cases
func InvalidJSON() []byte {
	return []byte("invalid json")
}

// EmptyToolHiveJSON returns an empty ToolHive registry JSON object
func EmptyToolHiveJSON() []byte {
	return []byte("{}")
}
