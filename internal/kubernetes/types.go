package kubernetes

import (
	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	model "github.com/modelcontextprotocol/registry/pkg/model"
	mcpv1alpha1 "github.com/stacklok/toolhive/cmd/thv-operator/api/v1alpha1"
)

// extractServer converts an MCPServer to a ServerJSON object
func extractServer(mcpServer *mcpv1alpha1.MCPServer) (*upstreamv0.ServerJSON, error) {
	// Build the ServerJSON from MCPServer spec
	// Note: MCPServer is a Kubernetes deployment resource, so we extract
	// what information is available and create a minimal ServerJSON
	serverJSON := &upstreamv0.ServerJSON{
		Schema:  "https://static.modelcontextprotocol.io/schemas/2025-10-17/server.schema.json",
		Name:    mcpServer.Name,
		Version: "1.0.0", // Default version, could be extracted from annotations or labels
	}

	// Extract description from annotations if available
	if annotations := mcpServer.GetAnnotations(); annotations != nil {
		if desc, ok := annotations[defaultRegistryDescriptionAnnotation]; ok {
			serverJSON.Description = desc
		}
		if website, ok := annotations[defaultRegistryURLAnnotation]; ok {
			serverJSON.WebsiteURL = website
		}
	}

	// Extract packages from MCPServer spec (using the container image)
	packages := extractPackages(mcpServer)
	serverJSON.Packages = packages

	// Extract remotes from MCPServer spec (using transport configuration)
	remotes := extractRemotes(mcpServer)
	serverJSON.Remotes = remotes

	// Initialize metadata
	serverJSON.Meta = &upstreamv0.ServerMeta{
		PublisherProvided: make(map[string]interface{}),
	}

	// Add Kubernetes metadata to publisher provided metadata
	serverJSON.Meta.PublisherProvided["kubernetes_namespace"] = mcpServer.Namespace
	serverJSON.Meta.PublisherProvided["kubernetes_name"] = mcpServer.Name
	serverJSON.Meta.PublisherProvided["kubernetes_uid"] = string(mcpServer.UID)
	serverJSON.Meta.PublisherProvided["kubernetes_image"] = mcpServer.Spec.Image
	serverJSON.Meta.PublisherProvided["kubernetes_transport"] = mcpServer.Spec.Transport

	return serverJSON, nil
}

// extractPackages extracts packages from MCPServer spec
// MCPServer uses container images, so we create an OCI package from the image
func extractPackages(mcpServer *mcpv1alpha1.MCPServer) []model.Package {
	var packages []model.Package

	transportType := mcpServer.Spec.Transport
	if transportType == "" {
		transportType = model.TransportTypeStreamableHTTP
	}

	if transportType == "stdio" {
		if mcpServer.Spec.ProxyMode != "" {
			transportType = mcpServer.Spec.ProxyMode
		} else {
			transportType = "streamable-http"
		}
	}

	packageModel := model.Package{
		RegistryType: "oci",
		Identifier:   mcpServer.Spec.Image,
		Version:      "latest", // Could be extracted from image tag
		Transport: model.Transport{
			Type: transportType,
		},
	}
	packages = append(packages, packageModel)

	return packages
}

// extractRemotes extracts remotes from MCPServer spec
// For MCPServer, remotes would be configured if the server is exposed externally
func extractRemotes(mcpServer *mcpv1alpha1.MCPServer) []model.Transport {
	var remotes []model.Transport

	if mcpServer.Spec.Transport != "" && mcpServer.Spec.Transport != "stdio" {
		transportType := mcpServer.Spec.Transport
		transport := model.Transport{
			Type: transportType,
		}

		if annotations := mcpServer.GetAnnotations(); annotations != nil {
			if url, ok := annotations["toolhive.stacklok.dev/transport-url"]; ok {
				transport.URL = url
			}
		}

		remotes = append(remotes, transport)
	}

	return remotes
}
