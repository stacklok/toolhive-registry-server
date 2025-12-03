package kubernetes

import (
	"fmt"
	"strings"

	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	model "github.com/modelcontextprotocol/registry/pkg/model"
	mcpv1alpha1 "github.com/stacklok/toolhive/cmd/thv-operator/api/v1alpha1"
)

// extractServer converts an MCPServer to a ServerJSON object
//
//nolint:unparam
func extractServer(mcpServer *mcpv1alpha1.MCPServer) (*upstreamv0.ServerJSON, error) {
	// Generate reverse-DNS formatted server name
	serverName, err := GenerateServerName(mcpServer.Namespace, mcpServer.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to generate server name: %w", err)
	}

	// Build the ServerJSON from MCPServer spec
	// Note: MCPServer is a Kubernetes deployment resource, so we extract
	// what information is available and create a minimal ServerJSON
	serverJSON := &upstreamv0.ServerJSON{
		Schema:  "https://static.modelcontextprotocol.io/schemas/2025-10-17/server.schema.json",
		Name:    serverName,
		Version: "1.0.0", // Default version, could be extracted from annotations or labels
	}

	// Extract packages from MCPServer spec (using the container image)
	packages := extractPackages(mcpServer)
	serverJSON.Packages = packages

	annotations := mcpServer.GetAnnotations()
	if annotations == nil {
		return nil, fmt.Errorf("annotations not found")
	}

	desc, ok := annotations[defaultRegistryDescriptionAnnotation]
	if !ok {
		return nil, fmt.Errorf("description not found in annotations")
	}
	serverJSON.Description = desc

	transportURL, ok := annotations[defaultRegistryURLAnnotation]
	if !ok {
		return nil, fmt.Errorf("URL not found in annotations")
	}
	serverJSON.Remotes = []model.Transport{
		{
			Type: model.TransportTypeStreamableHTTP,
			URL:  transportURL,
		},
	}

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

// extractVirtualMCPServer converts a VirtualMCPServer to a ServerJSON object
//
//nolint:unparam
func extractVirtualMCPServer(virtualMCPServer *mcpv1alpha1.VirtualMCPServer) (*upstreamv0.ServerJSON, error) {
	// Generate reverse-DNS formatted server name
	serverName, err := GenerateServerName(virtualMCPServer.Namespace, virtualMCPServer.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to generate server name: %w", err)
	}

	// Build the ServerJSON from VirtualMCPServer spec
	// Note: VirtualMCPServer is a Kubernetes deployment resource, so we extract
	// what information is available and create a minimal ServerJSON
	serverJSON := &upstreamv0.ServerJSON{
		Schema:  "https://static.modelcontextprotocol.io/schemas/2025-10-17/server.schema.json",
		Name:    serverName,
		Version: "1.0.0", // Default version, could be extracted from annotations or labels
	}

	annotations := virtualMCPServer.GetAnnotations()
	if annotations == nil {
		return nil, fmt.Errorf("annotations not found")
	}

	desc, ok := annotations[defaultRegistryDescriptionAnnotation]
	if !ok {
		return nil, fmt.Errorf("description not found in annotations")
	}
	serverJSON.Description = desc

	transportURL, ok := annotations[defaultRegistryURLAnnotation]
	if !ok {
		return nil, fmt.Errorf("URL not found in annotations")
	}
	serverJSON.Remotes = []model.Transport{
		{
			Type: model.TransportTypeStreamableHTTP,
			URL:  transportURL,
		},
	}

	// Initialize metadata
	serverJSON.Meta = &upstreamv0.ServerMeta{
		PublisherProvided: make(map[string]interface{}),
	}
	// Add Kubernetes metadata to publisher provided metadata
	serverJSON.Meta.PublisherProvided["kubernetes_namespace"] = virtualMCPServer.Namespace
	serverJSON.Meta.PublisherProvided["kubernetes_name"] = virtualMCPServer.Name
	serverJSON.Meta.PublisherProvided["kubernetes_uid"] = string(virtualMCPServer.UID)

	return serverJSON, nil
}

// extractMCPRemoteProxy converts a MCPRemoteProxy to a ServerJSON object
//
//nolint:unparam
func extractMCPRemoteProxy(mcpRemoteProxy *mcpv1alpha1.MCPRemoteProxy) (*upstreamv0.ServerJSON, error) {
	// Generate reverse-DNS formatted server name
	serverName, err := GenerateServerName(mcpRemoteProxy.Namespace, mcpRemoteProxy.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to generate server name: %w", err)
	}

	// Build the ServerJSON from MCPRemoteProxy spec
	// Note: MCPRemoteProxy is a Kubernetes deployment resource, so we extract
	// what information is available and create a minimal ServerJSON
	serverJSON := &upstreamv0.ServerJSON{
		Schema:  "https://static.modelcontextprotocol.io/schemas/2025-10-17/server.schema.json",
		Name:    serverName,
		Version: "1.0.0", // Default version, could be extracted from annotations or labels
	}

	// Extract description from annotations if available
	if annotations := mcpRemoteProxy.GetAnnotations(); annotations != nil {
		if desc, ok := annotations[defaultRegistryDescriptionAnnotation]; ok {
			serverJSON.Description = desc
		}
	}

	annotations := mcpRemoteProxy.GetAnnotations()
	if annotations == nil {
		return nil, fmt.Errorf("annotations not found")
	}

	desc, ok := annotations[defaultRegistryDescriptionAnnotation]
	if !ok {
		return nil, fmt.Errorf("description not found in annotations")
	}
	serverJSON.Description = desc

	transportURL, ok := annotations[defaultRegistryURLAnnotation]
	if !ok {
		return nil, fmt.Errorf("URL not found in annotations")
	}
	serverJSON.Remotes = []model.Transport{
		{
			Type: model.TransportTypeStreamableHTTP,
			URL:  transportURL,
		},
	}

	// Initialize metadata
	serverJSON.Meta = &upstreamv0.ServerMeta{
		PublisherProvided: make(map[string]interface{}),
	}
	// Add Kubernetes metadata to publisher provided metadata
	serverJSON.Meta.PublisherProvided["kubernetes_namespace"] = mcpRemoteProxy.Namespace
	serverJSON.Meta.PublisherProvided["kubernetes_name"] = mcpRemoteProxy.Name
	serverJSON.Meta.PublisherProvided["kubernetes_uid"] = string(mcpRemoteProxy.UID)

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

	version := parseImageTagOrDigest(mcpServer.Spec.Image)
	packageModel := model.Package{
		RegistryType: "oci",
		Identifier:   mcpServer.Spec.Image,
		Version:      version,
		Transport: model.Transport{
			Type: transportType,
		},
	}
	packages = append(packages, packageModel)

	return packages
}

// parseImageTagOrDigest is a rudimentary parser that parses the tag or digest
// from an image string. It returns the tag or digest, or an error if the image
// is invalid.
func parseImageTagOrDigest(image string) string {
	var potentialTag string
	var potentialDigest string

	parts := strings.Split(image, ":")
	if len(parts) >= 2 {
		potentialTag = parts[len(parts)-1]
	}
	parts = strings.Split(image, "@")
	if len(parts) >= 2 {
		potentialDigest = parts[len(parts)-1]

	}

	if potentialDigest != "" {
		return potentialDigest
	}
	if potentialTag != "" {
		return potentialTag
	}

	return "latest"
}
