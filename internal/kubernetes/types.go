package kubernetes

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	model "github.com/modelcontextprotocol/registry/pkg/model"
	mcpv1alpha1 "github.com/stacklok/toolhive/cmd/thv-operator/api/v1alpha1"
	registry "github.com/stacklok/toolhive/pkg/registry/registry"
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
		Schema:  "https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json",
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
		PublisherProvided: make(map[string]any),
	}

	// Create ServerExtensions with Kubernetes metadata
	extensions := &registry.ServerExtensions{
		Status: "active", // Default status
		Metadata: &registry.Metadata{
			Kubernetes: &registry.KubernetesMetadata{
				Kind:      mcpServer.Kind,
				Namespace: mcpServer.Namespace,
				Name:      mcpServer.Name,
				Image:     mcpServer.Spec.Image,
				UID:       string(mcpServer.UID),
				Transport: mcpServer.Spec.Transport,
			},
		},
	}

	// Add tool definitions if present
	if toolDefs := extractToolDefinitions(annotations, mcpServer.Name, mcpServer.Namespace); toolDefs != nil {
		extensions.ToolDefinitions = toolDefs
	}

	// Add tools if present
	if tools := extractTools(annotations, mcpServer.Name, mcpServer.Namespace); tools != nil {
		extensions.Tools = tools
	}

	// Convert extensions struct to map[string]any for JSON serialization
	extensionsMap, err := structToMap(extensions)
	if err != nil {
		return nil, fmt.Errorf("failed to convert extensions to map: %w", err)
	}

	serverJSON.Meta.PublisherProvided[registry.ToolHivePublisherNamespace] = map[string]any{
		transportURL: extensionsMap,
	}

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
		Schema:  "https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json",
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
		PublisherProvided: make(map[string]any),
	}

	// Create ServerExtensions with Kubernetes metadata
	extensions := &registry.ServerExtensions{
		Status: "active", // Default status
		Metadata: &registry.Metadata{
			Kubernetes: &registry.KubernetesMetadata{
				Kind:      virtualMCPServer.Kind,
				Namespace: virtualMCPServer.Namespace,
				Name:      virtualMCPServer.Name,
				UID:       string(virtualMCPServer.UID),
			},
		},
	}

	// Add tool definitions if present
	if toolDefs := extractToolDefinitions(annotations, virtualMCPServer.Name, virtualMCPServer.Namespace); toolDefs != nil {
		extensions.ToolDefinitions = toolDefs
	}

	// Add tools if present
	if tools := extractTools(annotations, virtualMCPServer.Name, virtualMCPServer.Namespace); tools != nil {
		extensions.Tools = tools
	}

	// Convert extensions struct to map[string]any for JSON serialization
	extensionsMap, err := structToMap(extensions)
	if err != nil {
		return nil, fmt.Errorf("failed to convert extensions to map: %w", err)
	}

	serverJSON.Meta.PublisherProvided[registry.ToolHivePublisherNamespace] = map[string]any{
		transportURL: extensionsMap,
	}

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
		Schema:  "https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json",
		Name:    serverName,
		Version: "1.0.0", // Default version, could be extracted from annotations or labels
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
		PublisherProvided: make(map[string]any),
	}

	// Create ServerExtensions with Kubernetes metadata
	extensions := &registry.ServerExtensions{
		Status: "active", // Default status
		Metadata: &registry.Metadata{
			Kubernetes: &registry.KubernetesMetadata{
				Kind:      mcpRemoteProxy.Kind,
				Namespace: mcpRemoteProxy.Namespace,
				Name:      mcpRemoteProxy.Name,
				UID:       string(mcpRemoteProxy.UID),
			},
		},
	}

	// Add tool definitions if present
	if toolDefs := extractToolDefinitions(annotations, mcpRemoteProxy.Name, mcpRemoteProxy.Namespace); toolDefs != nil {
		extensions.ToolDefinitions = toolDefs
	}

	// Add tools if present
	if tools := extractTools(annotations, mcpRemoteProxy.Name, mcpRemoteProxy.Namespace); tools != nil {
		extensions.Tools = tools
	}

	// Convert extensions struct to map[string]any for JSON serialization
	extensionsMap, err := structToMap(extensions)
	if err != nil {
		return nil, fmt.Errorf("failed to convert extensions to map: %w", err)
	}

	serverJSON.Meta.PublisherProvided[registry.ToolHivePublisherNamespace] = map[string]any{
		transportURL: extensionsMap,
	}

	return serverJSON, nil
}

// extractToolDefinitions extracts and parses tool definitions from annotations.
// It validates JSON syntax but does not validate the schema (operator's responsibility).
// Returns nil if the annotation is not present, empty, or contains invalid JSON.
func extractToolDefinitions(annotations map[string]string, name, namespace string) []mcp.Tool {
	toolDefsStr, ok := annotations[defaultRegistryToolDefinitionsAnnotation]
	if !ok || toolDefsStr == "" {
		return nil
	}

	var toolDefs []mcp.Tool
	if err := json.Unmarshal([]byte(toolDefsStr), &toolDefs); err != nil {
		slog.Warn("tool_definitions annotation is not valid JSON, skipping",
			"error", err,
			"server", name,
			"namespace", namespace)
		return nil
	}

	return toolDefs
}

// extractTools extracts and parses tools from annotations.
// It expects a JSON array of strings (tool names).
// Returns nil if the annotation is not present, empty, or contains invalid JSON.
func extractTools(annotations map[string]string, name, namespace string) []string {
	toolsStr, ok := annotations[defaultRegistryToolsAnnotation]
	if !ok || toolsStr == "" {
		return nil
	}

	var tools []string
	if err := json.Unmarshal([]byte(toolsStr), &tools); err != nil {
		slog.Warn("tools annotation is not valid JSON, skipping",
			"error", err,
			"server", name,
			"namespace", namespace)
		return nil
	}

	return tools
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

// structToMap converts a struct to map[string]any using JSON marshaling.
// This is needed to convert typed structs (like ServerExtensions) into the generic map
// structure expected by the PublisherProvided field, while preserving proper JSON serialization.
//
// Note: JSON numbers unmarshal to float64 in map[string]any. This is fine for the current
// use case (only string fields are set from Kubernetes metadata), but callers should be
// aware that any integer fields (e.g., ProxyPort, Stars) will become float64 in the result.
func structToMap(v any) (map[string]any, error) {
	// Marshal the struct to JSON
	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal struct: %w", err)
	}

	// Unmarshal JSON into map[string]any
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal into map: %w", err)
	}

	return result, nil
}
