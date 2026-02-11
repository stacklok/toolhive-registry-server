package registry

import (
	"time"

	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
	toolhivetypes "github.com/stacklok/toolhive/pkg/registry/registry"
)

// UpstreamRegistryOption is a function that configures an UpstreamRegistry for testing
type UpstreamRegistryOption func(*toolhivetypes.UpstreamRegistry)

// ServerOption is a function that configures a ServerJSON for testing
type ServerOption func(*upstreamv0.ServerJSON)

// NewTestUpstreamRegistry creates a new UpstreamRegistry for testing with default values
// and applies any provided options
func NewTestUpstreamRegistry(opts ...UpstreamRegistryOption) *toolhivetypes.UpstreamRegistry {
	reg := &toolhivetypes.UpstreamRegistry{
		Schema:  UpstreamRegistrySchemaURL,
		Version: UpstreamRegistryVersion,
		Meta: toolhivetypes.UpstreamMeta{
			LastUpdated: time.Now().Format(time.RFC3339),
		},
		Data: toolhivetypes.UpstreamData{
			Servers: []upstreamv0.ServerJSON{},
			Groups:  []toolhivetypes.UpstreamGroup{},
		},
	}

	for _, opt := range opts {
		opt(reg)
	}

	return reg
}

// WithVersion sets the registry version
func WithVersion(version string) UpstreamRegistryOption {
	return func(reg *toolhivetypes.UpstreamRegistry) {
		reg.Version = version
	}
}

// WithLastUpdated sets the registry last updated timestamp
func WithLastUpdated(timestamp string) UpstreamRegistryOption {
	return func(reg *toolhivetypes.UpstreamRegistry) {
		reg.Meta.LastUpdated = timestamp
	}
}

// WithServers adds servers to the registry
func WithServers(servers ...upstreamv0.ServerJSON) UpstreamRegistryOption {
	return func(reg *toolhivetypes.UpstreamRegistry) {
		reg.Data.Servers = append(reg.Data.Servers, servers...)
	}
}

// NewTestServer creates a new ServerJSON for testing with default values
// and applies any provided options
func NewTestServer(name string, opts ...ServerOption) upstreamv0.ServerJSON {
	server := upstreamv0.ServerJSON{
		Schema:      "https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json",
		Name:        name,
		Description: name + " server",
		Version:     "1.0.0",
		Packages:    []model.Package{},
		Meta:        &upstreamv0.ServerMeta{},
	}

	for _, opt := range opts {
		opt(&server)
	}

	return server
}

// WithServerVersion sets the server version
func WithServerVersion(version string) ServerOption {
	return func(server *upstreamv0.ServerJSON) {
		server.Version = version
	}
}

// WithOCIPackage adds an OCI (container) package to the server
func WithOCIPackage(image string) ServerOption {
	return func(server *upstreamv0.ServerJSON) {
		server.Packages = append(server.Packages, model.Package{
			RegistryType: "oci",
			Identifier:   image,
			Transport:    model.Transport{Type: "stdio"},
		})
	}
}

// WithHTTPPackage adds an HTTP (remote) package to the server
func WithHTTPPackage(url string) ServerOption {
	return func(server *upstreamv0.ServerJSON) {
		server.Packages = append(server.Packages, model.Package{
			RegistryType: "http",
			Identifier:   url,
			Transport:    model.Transport{Type: "stdio"},
		})
	}
}

// WithTags adds tags to the server's metadata
// Tags are stored in Meta.PublisherProvided["provider"]["metadata"]["tags"]
func WithTags(tags ...string) ServerOption {
	return func(server *upstreamv0.ServerJSON) {
		if server.Meta == nil {
			server.Meta = &upstreamv0.ServerMeta{}
		}
		if server.Meta.PublisherProvided == nil {
			server.Meta.PublisherProvided = make(map[string]any)
		}

		// Convert tags to interface slice
		tagInterfaces := make([]interface{}, len(tags))
		for i, tag := range tags {
			tagInterfaces[i] = tag
		}

		// Create nested structure for tags
		server.Meta.PublisherProvided["provider"] = map[string]any{
			"metadata": map[string]any{
				"tags": tagInterfaces,
			},
		}
	}
}

// WithNamespace prepends a namespace to the server name (e.g., "io.test/")
func WithNamespace(namespace string) ServerOption {
	return func(server *upstreamv0.ServerJSON) {
		server.Name = namespace + server.Name
	}
}

// WithMetadata adds arbitrary metadata to the server
func WithMetadata(key string, value any) ServerOption {
	return func(server *upstreamv0.ServerJSON) {
		if server.Meta == nil {
			server.Meta = &upstreamv0.ServerMeta{}
		}
		if server.Meta.PublisherProvided == nil {
			server.Meta.PublisherProvided = make(map[string]any)
		}
		server.Meta.PublisherProvided[key] = value
	}
}

// WithToolHiveMetadata adds ToolHive-specific metadata in the provider.toolhive structure
// This is a helper to set nested metadata fields commonly used by ToolHive
func WithToolHiveMetadata(key string, value any) ServerOption {
	return func(server *upstreamv0.ServerJSON) {
		if server.Meta == nil {
			server.Meta = &upstreamv0.ServerMeta{}
		}
		if server.Meta.PublisherProvided == nil {
			server.Meta.PublisherProvided = make(map[string]any)
		}

		// Ensure provider.toolhive structure exists
		provider, ok := server.Meta.PublisherProvided["provider"].(map[string]any)
		if !ok {
			provider = make(map[string]any)
			server.Meta.PublisherProvided["provider"] = provider
		}

		toolhive, ok := provider["toolhive"].(map[string]any)
		if !ok {
			toolhive = make(map[string]any)
			provider["toolhive"] = toolhive
		}

		toolhive[key] = value
	}
}
