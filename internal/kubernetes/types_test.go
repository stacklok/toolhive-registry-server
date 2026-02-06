package kubernetes

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	model "github.com/modelcontextprotocol/registry/pkg/model"
	mcpv1alpha1 "github.com/stacklok/toolhive/cmd/thv-operator/api/v1alpha1"
	"github.com/stacklok/toolhive/pkg/registry/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// createTestMCPServer creates a test MCPServer with the given configuration
func createTestMCPServer(name, namespace string, annotations map[string]string, spec mcpv1alpha1.MCPServerSpec) *mcpv1alpha1.MCPServer {
	return &mcpv1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			UID:         types.UID(uuid.New().String()),
			Annotations: annotations,
		},
		Spec: spec,
	}
}

func TestExtractServer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		mcpServer   *mcpv1alpha1.MCPServer
		wantSchema  string
		wantName    string
		wantVersion string
		wantErr     bool
		errContains string
		checkMeta   func(*testing.T, *upstreamv0.ServerJSON)
	}{
		{
			name: "valid MCPServer with all required annotations",
			mcpServer: createTestMCPServer(
				"test-server",
				"default",
				map[string]string{
					defaultRegistryDescriptionAnnotation: "A test MCP server",
					defaultRegistryURLAnnotation:         "https://example.com/mcp",
				},
				mcpv1alpha1.MCPServerSpec{
					Image:     "test/image:latest",
					Transport: "stdio",
				},
			),
			wantSchema:  "https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json",
			wantName:    "com.toolhive.k8s.default/test-server",
			wantVersion: "1.0.0",
			wantErr:     false,
			//nolint:thelper // We want to see these lines in the test output
			checkMeta: func(t *testing.T, sj *upstreamv0.ServerJSON) {
				assert.Equal(t, "A test MCP server", sj.Description)
				assert.NotNil(t, sj.Meta.PublisherProvided["io.github.stacklok"])
				ioStacklok := sj.Meta.PublisherProvided["io.github.stacklok"].(map[string]any)

				assert.NotNil(t, ioStacklok["https://example.com/mcp"])
				mcpMetadata := ioStacklok["https://example.com/mcp"].(map[string]any)

				// Deserialize to ServerExtensions to check typed fields
				data, err := json.Marshal(mcpMetadata)
				require.NoError(t, err)
				var ext registry.ServerExtensions
				require.NoError(t, json.Unmarshal(data, &ext))

				require.NotNil(t, ext.Metadata)
				require.NotNil(t, ext.Metadata.Kubernetes)
				assert.Equal(t, "default", ext.Metadata.Kubernetes.Namespace)
				assert.Equal(t, "test-server", ext.Metadata.Kubernetes.Name)
				assert.Equal(t, "test/image:latest", ext.Metadata.Kubernetes.Image)
				assert.Equal(t, "stdio", ext.Metadata.Kubernetes.Transport)
				assert.NotEmpty(t, ext.Metadata.Kubernetes.UID)
				require.Len(t, sj.Remotes, 1)
				assert.Equal(t, model.TransportTypeStreamableHTTP, sj.Remotes[0].Type)
				assert.Equal(t, "https://example.com/mcp", sj.Remotes[0].URL)
			},
		},
		{
			name: "MCPServer with nil annotations",
			mcpServer: createTestMCPServer(
				"test-server",
				"default",
				nil,
				mcpv1alpha1.MCPServerSpec{
					Image:     "test/image:latest",
					Transport: "sse",
				},
			),
			wantErr:     true,
			errContains: "annotations not found",
		},
		{
			name: "MCPServer with empty annotations map",
			mcpServer: createTestMCPServer(
				"test-server",
				"default",
				map[string]string{},
				mcpv1alpha1.MCPServerSpec{
					Image:     "test/image:latest",
					Transport: "streamable-http",
				},
			),
			wantErr:     true,
			errContains: "description not found in annotations",
		},
		{
			name: "MCPServer missing description annotation",
			mcpServer: createTestMCPServer(
				"test-server",
				"default",
				map[string]string{
					defaultRegistryURLAnnotation: "https://example.com/mcp",
				},
				mcpv1alpha1.MCPServerSpec{
					Image:     "test/image:latest",
					Transport: "sse",
				},
			),
			wantErr:     true,
			errContains: "description not found in annotations",
		},
		{
			name: "MCPServer missing URL annotation",
			mcpServer: createTestMCPServer(
				"test-server",
				"default",
				map[string]string{
					defaultRegistryDescriptionAnnotation: "A test server",
				},
				mcpv1alpha1.MCPServerSpec{
					Image:     "test/image:latest",
					Transport: "sse",
				},
			),
			wantErr:     true,
			errContains: "URL not found in annotations",
		},
		{
			name: "MCPServer with all annotations and different transport",
			mcpServer: createTestMCPServer(
				"full-server",
				"custom-ns",
				map[string]string{
					defaultRegistryDescriptionAnnotation: "Full featured server",
					defaultRegistryURLAnnotation:         "https://api.example.com/mcp-server",
				},
				mcpv1alpha1.MCPServerSpec{
					Image:     "registry.example.com/image:tag",
					Transport: "sse",
				},
			),
			wantSchema:  "https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json",
			wantName:    "com.toolhive.k8s.custom-ns/full-server",
			wantVersion: "1.0.0",
			wantErr:     false,
			//nolint:thelper // We want to see these lines in the test output
			checkMeta: func(t *testing.T, sj *upstreamv0.ServerJSON) {
				assert.Equal(t, "Full featured server", sj.Description)
				assert.NotNil(t, sj.Meta.PublisherProvided["io.github.stacklok"])
				ioStacklok := sj.Meta.PublisherProvided["io.github.stacklok"].(map[string]any)

				assert.NotNil(t, ioStacklok["https://api.example.com/mcp-server"])
				mcpMetadata := ioStacklok["https://api.example.com/mcp-server"].(map[string]any)

				// Deserialize to ServerExtensions to check typed fields
				data, err := json.Marshal(mcpMetadata)
				require.NoError(t, err)
				var ext registry.ServerExtensions
				require.NoError(t, json.Unmarshal(data, &ext))

				require.NotNil(t, ext.Metadata)
				require.NotNil(t, ext.Metadata.Kubernetes)
				assert.Equal(t, "custom-ns", ext.Metadata.Kubernetes.Namespace)
				assert.Equal(t, "full-server", ext.Metadata.Kubernetes.Name)
				assert.Equal(t, "registry.example.com/image:tag", ext.Metadata.Kubernetes.Image)
				assert.Equal(t, "sse", ext.Metadata.Kubernetes.Transport)
				assert.NotEmpty(t, ext.Metadata.Kubernetes.UID)
				require.Len(t, sj.Remotes, 1)
				assert.Equal(t, model.TransportTypeStreamableHTTP, sj.Remotes[0].Type)
				assert.Equal(t, "https://api.example.com/mcp-server", sj.Remotes[0].URL)
			},
		},
		{
			name: "MCPServer with valid tool_definitions",
			mcpServer: createTestMCPServer(
				"server-with-tools",
				"default",
				map[string]string{
					defaultRegistryDescriptionAnnotation:     "Server with tool definitions",
					defaultRegistryURLAnnotation:             "https://example.com/tools",
					defaultRegistryToolDefinitionsAnnotation: `[{"name":"test_tool","description":"A test tool","inputSchema":{"type":"object","properties":{"param":{"type":"string"}}},"annotations":{"readOnlyHint":true}}]`,
				},
				mcpv1alpha1.MCPServerSpec{
					Image:     "test/tools:v1",
					Transport: "sse",
				},
			),
			wantSchema:  "https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json",
			wantName:    "com.toolhive.k8s.default/server-with-tools",
			wantVersion: "1.0.0",
			wantErr:     false,
			//nolint:thelper // We want to see these lines in the test output
			checkMeta: func(t *testing.T, sj *upstreamv0.ServerJSON) {
				assert.Equal(t, "Server with tool definitions", sj.Description)
				ioStacklok := sj.Meta.PublisherProvided["io.github.stacklok"].(map[string]any)
				mcpMetadata := ioStacklok["https://example.com/tools"].(map[string]any)

				// Check tool_definitions is present and parsed as array
				require.NotNil(t, mcpMetadata["tool_definitions"])
				toolDefs, ok := mcpMetadata["tool_definitions"].([]interface{})
				require.True(t, ok, "tool_definitions should be an array")
				require.Len(t, toolDefs, 1)

				// Check first tool definition
				tool := toolDefs[0].(map[string]interface{})
				assert.Equal(t, "test_tool", tool["name"])
				assert.Equal(t, "A test tool", tool["description"])
				assert.NotNil(t, tool["inputSchema"])
				assert.NotNil(t, tool["annotations"])

				// Check annotations
				annotations := tool["annotations"].(map[string]interface{})
				assert.Equal(t, true, annotations["readOnlyHint"])
			},
		},
		{
			name: "MCPServer with invalid JSON in tool_definitions",
			mcpServer: createTestMCPServer(
				"server-invalid-json",
				"default",
				map[string]string{
					defaultRegistryDescriptionAnnotation:     "Server with invalid JSON",
					defaultRegistryURLAnnotation:             "https://example.com/invalid",
					defaultRegistryToolDefinitionsAnnotation: `{invalid json}`,
				},
				mcpv1alpha1.MCPServerSpec{
					Image:     "test/invalid:v1",
					Transport: "sse",
				},
			),
			wantSchema:  "https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json",
			wantName:    "com.toolhive.k8s.default/server-invalid-json",
			wantVersion: "1.0.0",
			wantErr:     false,
			//nolint:thelper // We want to see these lines in the test output
			checkMeta: func(t *testing.T, sj *upstreamv0.ServerJSON) {
				ioStacklok := sj.Meta.PublisherProvided["io.github.stacklok"].(map[string]any)
				mcpMetadata := ioStacklok["https://example.com/invalid"].(map[string]any)

				// tool_definitions should not be present when JSON is invalid
				assert.Nil(t, mcpMetadata["tool_definitions"])
			},
		},
		{
			name: "MCPServer with empty tool_definitions",
			mcpServer: createTestMCPServer(
				"server-empty-tools",
				"default",
				map[string]string{
					defaultRegistryDescriptionAnnotation:     "Server with empty tools",
					defaultRegistryURLAnnotation:             "https://example.com/empty",
					defaultRegistryToolDefinitionsAnnotation: "",
				},
				mcpv1alpha1.MCPServerSpec{
					Image:     "test/empty:v1",
					Transport: "sse",
				},
			),
			wantSchema:  "https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json",
			wantName:    "com.toolhive.k8s.default/server-empty-tools",
			wantVersion: "1.0.0",
			wantErr:     false,
			//nolint:thelper // We want to see these lines in the test output
			checkMeta: func(t *testing.T, sj *upstreamv0.ServerJSON) {
				ioStacklok := sj.Meta.PublisherProvided["io.github.stacklok"].(map[string]any)
				mcpMetadata := ioStacklok["https://example.com/empty"].(map[string]any)

				// Empty string should be ignored
				assert.Nil(t, mcpMetadata["tool_definitions"])
			},
		},
		{
			name: "MCPServer with multiple tools in tool_definitions",
			mcpServer: createTestMCPServer(
				"server-multi-tools",
				"default",
				map[string]string{
					defaultRegistryDescriptionAnnotation:     "Server with multiple tools",
					defaultRegistryURLAnnotation:             "https://example.com/multi",
					defaultRegistryToolDefinitionsAnnotation: `[{"name":"tool_one","description":"First tool"},{"name":"tool_two","description":"Second tool","inputSchema":{"type":"object"},"outputSchema":{"type":"object"},"annotations":{"destructiveHint":true}}]`,
				},
				mcpv1alpha1.MCPServerSpec{
					Image:     "test/multi:v1",
					Transport: "sse",
				},
			),
			wantSchema:  "https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json",
			wantName:    "com.toolhive.k8s.default/server-multi-tools",
			wantVersion: "1.0.0",
			wantErr:     false,
			//nolint:thelper // We want to see these lines in the test output
			checkMeta: func(t *testing.T, sj *upstreamv0.ServerJSON) {
				ioStacklok := sj.Meta.PublisherProvided["io.github.stacklok"].(map[string]any)
				mcpMetadata := ioStacklok["https://example.com/multi"].(map[string]any)

				// Check tool_definitions has multiple tools
				require.NotNil(t, mcpMetadata["tool_definitions"])
				toolDefs, ok := mcpMetadata["tool_definitions"].([]interface{})
				require.True(t, ok)
				require.Len(t, toolDefs, 2)

				// Check first tool
				tool1 := toolDefs[0].(map[string]interface{})
				assert.Equal(t, "tool_one", tool1["name"])
				assert.Equal(t, "First tool", tool1["description"])

				// Check second tool
				tool2 := toolDefs[1].(map[string]interface{})
				assert.Equal(t, "tool_two", tool2["name"])
				assert.Equal(t, "Second tool", tool2["description"])
				assert.NotNil(t, tool2["inputSchema"])
				assert.NotNil(t, tool2["outputSchema"])
				annotations := tool2["annotations"].(map[string]interface{})
				assert.Equal(t, true, annotations["destructiveHint"])
			},
		},
		{
			name: "MCPServer with valid tools annotation",
			mcpServer: createTestMCPServer(
				"server-with-tools-list",
				"default",
				map[string]string{
					defaultRegistryDescriptionAnnotation: "Server with tools list",
					defaultRegistryURLAnnotation:         "https://example.com/tools-list",
					defaultRegistryToolsAnnotation:       `["get_weather","get_forecast"]`,
				},
				mcpv1alpha1.MCPServerSpec{
					Image:     "test/tools-list:v1",
					Transport: "sse",
				},
			),
			wantSchema:  "https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json",
			wantName:    "com.toolhive.k8s.default/server-with-tools-list",
			wantVersion: "1.0.0",
			wantErr:     false,
			//nolint:thelper // We want to see these lines in the test output
			checkMeta: func(t *testing.T, sj *upstreamv0.ServerJSON) {
				ioStacklok := sj.Meta.PublisherProvided["io.github.stacklok"].(map[string]any)
				mcpMetadata := ioStacklok["https://example.com/tools-list"].(map[string]any)

				// Check tools is present and is a string array
				// After JSON marshaling/unmarshaling, []string becomes []interface{}
				require.NotNil(t, mcpMetadata["tools"])
				tools, ok := mcpMetadata["tools"].([]interface{})
				require.True(t, ok, "tools should be an array")
				require.Len(t, tools, 2)
				assert.Equal(t, "get_weather", tools[0])
				assert.Equal(t, "get_forecast", tools[1])
			},
		},
		{
			name: "MCPServer with invalid JSON in tools",
			mcpServer: createTestMCPServer(
				"server-invalid-tools",
				"default",
				map[string]string{
					defaultRegistryDescriptionAnnotation: "Server with invalid tools JSON",
					defaultRegistryURLAnnotation:         "https://example.com/invalid-tools",
					defaultRegistryToolsAnnotation:       `[invalid]`,
				},
				mcpv1alpha1.MCPServerSpec{
					Image:     "test/invalid-tools:v1",
					Transport: "sse",
				},
			),
			wantSchema:  "https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json",
			wantName:    "com.toolhive.k8s.default/server-invalid-tools",
			wantVersion: "1.0.0",
			wantErr:     false,
			//nolint:thelper // We want to see these lines in the test output
			checkMeta: func(t *testing.T, sj *upstreamv0.ServerJSON) {
				ioStacklok := sj.Meta.PublisherProvided["io.github.stacklok"].(map[string]any)
				mcpMetadata := ioStacklok["https://example.com/invalid-tools"].(map[string]any)

				// Invalid JSON should be skipped
				assert.Nil(t, mcpMetadata["tools"])
			},
		},
		{
			name: "MCPServer with empty tools annotation",
			mcpServer: createTestMCPServer(
				"server-empty-tools-list",
				"default",
				map[string]string{
					defaultRegistryDescriptionAnnotation: "Server with empty tools",
					defaultRegistryURLAnnotation:         "https://example.com/empty-tools",
					defaultRegistryToolsAnnotation:       "",
				},
				mcpv1alpha1.MCPServerSpec{
					Image:     "test/empty-tools:v1",
					Transport: "sse",
				},
			),
			wantSchema:  "https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json",
			wantName:    "com.toolhive.k8s.default/server-empty-tools-list",
			wantVersion: "1.0.0",
			wantErr:     false,
			//nolint:thelper // We want to see these lines in the test output
			checkMeta: func(t *testing.T, sj *upstreamv0.ServerJSON) {
				ioStacklok := sj.Meta.PublisherProvided["io.github.stacklok"].(map[string]any)
				mcpMetadata := ioStacklok["https://example.com/empty-tools"].(map[string]any)

				// Empty string should be ignored
				assert.Nil(t, mcpMetadata["tools"])
			},
		},
		{
			name: "MCPServer with both tool_definitions and tools",
			mcpServer: createTestMCPServer(
				"server-with-both",
				"default",
				map[string]string{
					defaultRegistryDescriptionAnnotation:     "Server with both annotations",
					defaultRegistryURLAnnotation:             "https://example.com/both",
					defaultRegistryToolDefinitionsAnnotation: `[{"name":"get_weather","description":"Get weather data"}]`,
					defaultRegistryToolsAnnotation:           `["get_weather","get_forecast"]`,
				},
				mcpv1alpha1.MCPServerSpec{
					Image:     "test/both:v1",
					Transport: "sse",
				},
			),
			wantSchema:  "https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json",
			wantName:    "com.toolhive.k8s.default/server-with-both",
			wantVersion: "1.0.0",
			wantErr:     false,
			//nolint:thelper // We want to see these lines in the test output
			checkMeta: func(t *testing.T, sj *upstreamv0.ServerJSON) {
				ioStacklok := sj.Meta.PublisherProvided["io.github.stacklok"].(map[string]any)
				mcpMetadata := ioStacklok["https://example.com/both"].(map[string]any)

				// Both should be present
				require.NotNil(t, mcpMetadata["tool_definitions"])
				toolDefs, ok := mcpMetadata["tool_definitions"].([]interface{})
				require.True(t, ok)
				require.Len(t, toolDefs, 1)

				// After JSON marshaling/unmarshaling, []string becomes []interface{}
				require.NotNil(t, mcpMetadata["tools"])
				tools, ok := mcpMetadata["tools"].([]interface{})
				require.True(t, ok)
				require.Len(t, tools, 2)
				assert.Equal(t, "get_weather", tools[0])
				assert.Equal(t, "get_forecast", tools[1])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := extractServer(tt.mcpServer)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				assert.Nil(t, result)
			} else {
				require.NoError(t, err)
				require.NotNil(t, result)
				assert.Equal(t, tt.wantSchema, result.Schema)
				assert.Equal(t, tt.wantName, result.Name)
				assert.Equal(t, tt.wantVersion, result.Version)
				assert.NotNil(t, result.Meta)
				assert.NotNil(t, result.Meta.PublisherProvided)
				assert.NotNil(t, result.Packages)

				if tt.checkMeta != nil {
					tt.checkMeta(t, result)
				}
			}
		})
	}
}

func TestExtractPackages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		mcpServer        *mcpv1alpha1.MCPServer
		wantCount        int
		wantRegistryType string
		wantIdentifier   string
		wantVersion      string
		wantTransport    string
	}{
		{
			name: "empty transport defaults to streamable-http",
			mcpServer: createTestMCPServer(
				"test",
				"default",
				nil,
				mcpv1alpha1.MCPServerSpec{
					Image:     "test/image:latest",
					Transport: "",
				},
			),
			wantCount:        1,
			wantRegistryType: "oci",
			wantIdentifier:   "test/image:latest",
			wantVersion:      "latest",
			wantTransport:    model.TransportTypeStreamableHTTP,
		},
		{
			name: "stdio transport with no proxy mode defaults to streamable-http",
			mcpServer: createTestMCPServer(
				"test",
				"default",
				nil,
				mcpv1alpha1.MCPServerSpec{
					Image:     "test/image:latest",
					Transport: "stdio",
				},
			),
			wantCount:        1,
			wantRegistryType: "oci",
			wantIdentifier:   "test/image:latest",
			wantVersion:      "latest",
			wantTransport:    "streamable-http",
		},
		{
			name: "stdio transport with proxy mode uses proxy mode",
			mcpServer: createTestMCPServer(
				"test",
				"default",
				nil,
				mcpv1alpha1.MCPServerSpec{
					Image:     "test/image:latest",
					Transport: "stdio",
					ProxyMode: "sse",
				},
			),
			wantCount:        1,
			wantRegistryType: "oci",
			wantIdentifier:   "test/image:latest",
			wantVersion:      "latest",
			wantTransport:    "sse",
		},
		{
			name: "sse transport",
			mcpServer: createTestMCPServer(
				"test",
				"default",
				nil,
				mcpv1alpha1.MCPServerSpec{
					Image:     "registry.example.com/image:v1.2.3",
					Transport: "sse",
				},
			),
			wantCount:        1,
			wantRegistryType: "oci",
			wantIdentifier:   "registry.example.com/image:v1.2.3",
			wantVersion:      "v1.2.3",
			wantTransport:    "sse",
		},
		{
			name: "streamable-http transport",
			mcpServer: createTestMCPServer(
				"test",
				"default",
				nil,
				mcpv1alpha1.MCPServerSpec{
					Image:     "test/image:tag",
					Transport: "streamable-http",
				},
			),
			wantCount:        1,
			wantRegistryType: "oci",
			wantIdentifier:   "test/image:tag",
			wantVersion:      "tag",
			wantTransport:    "streamable-http",
		},
		{
			name: "custom transport type",
			mcpServer: createTestMCPServer(
				"test",
				"default",
				nil,
				mcpv1alpha1.MCPServerSpec{
					Image:     "test/image:latest",
					Transport: "custom-transport",
				},
			),
			wantCount:        1,
			wantRegistryType: "oci",
			wantIdentifier:   "test/image:latest",
			wantVersion:      "latest",
			wantTransport:    "custom-transport",
		},
		{
			name: "image with sha256 digest instead of tag",
			mcpServer: createTestMCPServer(
				"test",
				"default",
				nil,
				mcpv1alpha1.MCPServerSpec{
					Image:     "registry.example.com/image@sha256:abc123def4567890abcdef1234567890abcdef1234567890abcdef1234567890",
					Transport: "streamable-http",
				},
			),
			wantCount:        1,
			wantRegistryType: "oci",
			wantIdentifier:   "registry.example.com/image@sha256:abc123def4567890abcdef1234567890abcdef1234567890abcdef1234567890",
			wantVersion:      "sha256:abc123def4567890abcdef1234567890abcdef1234567890abcdef1234567890",
			wantTransport:    "streamable-http",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := extractPackages(tt.mcpServer)

			require.Len(t, result, tt.wantCount)
			if tt.wantCount > 0 {
				pkg := result[0]
				assert.Equal(t, tt.wantRegistryType, pkg.RegistryType)
				assert.Equal(t, tt.wantIdentifier, pkg.Identifier)
				assert.Equal(t, tt.wantVersion, pkg.Version)
				assert.Equal(t, tt.wantTransport, pkg.Transport.Type)
			}
		})
	}
}

// createTestVirtualMCPServer creates a test VirtualMCPServer with the given configuration
func createTestVirtualMCPServer(name, namespace string, annotations map[string]string) *mcpv1alpha1.VirtualMCPServer {
	return &mcpv1alpha1.VirtualMCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			UID:         types.UID(uuid.New().String()),
			Annotations: annotations,
		},
	}
}

func TestExtractVirtualMCPServer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		vmcpServer  *mcpv1alpha1.VirtualMCPServer
		wantSchema  string
		wantName    string
		wantVersion string
		wantErr     bool
		errContains string
		checkMeta   func(*testing.T, *upstreamv0.ServerJSON)
	}{
		{
			name: "valid VirtualMCPServer with all required annotations",
			vmcpServer: createTestVirtualMCPServer(
				"test-vmcp-server",
				"default",
				map[string]string{
					defaultRegistryDescriptionAnnotation: "A test Virtual MCP server",
					defaultRegistryURLAnnotation:         "https://example.com/vmcp",
				},
			),
			wantSchema:  "https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json",
			wantName:    "com.toolhive.k8s.default/test-vmcp-server",
			wantVersion: "1.0.0",
			wantErr:     false,
			//nolint:thelper // We want to see these lines in the test output
			checkMeta: func(t *testing.T, sj *upstreamv0.ServerJSON) {
				assert.Equal(t, "A test Virtual MCP server", sj.Description)
				assert.NotNil(t, sj.Meta.PublisherProvided["io.github.stacklok"])
				ioStacklok := sj.Meta.PublisherProvided["io.github.stacklok"].(map[string]any)

				assert.NotNil(t, ioStacklok["https://example.com/vmcp"])
				mcpMetadata := ioStacklok["https://example.com/vmcp"].(map[string]any)

				assert.NotNil(t, mcpMetadata["metadata"])
				metadata := mcpMetadata["metadata"].(map[string]any)

				assert.NotNil(t, metadata["kubernetes"])
				kubernetes := metadata["kubernetes"].(map[string]any)

				assert.Equal(t, "default", kubernetes["namespace"])
				assert.Equal(t, "test-vmcp-server", kubernetes["name"])
				assert.NotEmpty(t, kubernetes["uid"])
				require.Len(t, sj.Remotes, 1)
				assert.Equal(t, model.TransportTypeStreamableHTTP, sj.Remotes[0].Type)
				assert.Equal(t, "https://example.com/vmcp", sj.Remotes[0].URL)
			},
		},
		{
			name: "VirtualMCPServer with nil annotations",
			vmcpServer: createTestVirtualMCPServer(
				"test-server",
				"default",
				nil,
			),
			wantErr:     true,
			errContains: "annotations not found",
		},
		{
			name: "VirtualMCPServer with empty annotations map",
			vmcpServer: createTestVirtualMCPServer(
				"test-server",
				"default",
				map[string]string{},
			),
			wantErr:     true,
			errContains: "description not found in annotations",
		},
		{
			name: "VirtualMCPServer missing description annotation",
			vmcpServer: createTestVirtualMCPServer(
				"test-server",
				"default",
				map[string]string{
					defaultRegistryURLAnnotation: "https://example.com/mcp",
				},
			),
			wantErr:     true,
			errContains: "description not found in annotations",
		},
		{
			name: "VirtualMCPServer missing URL annotation",
			vmcpServer: createTestVirtualMCPServer(
				"test-server",
				"default",
				map[string]string{
					defaultRegistryDescriptionAnnotation: "A test server",
				},
			),
			wantErr:     true,
			errContains: "URL not found in annotations",
		},
		{
			name: "VirtualMCPServer with custom namespace",
			vmcpServer: createTestVirtualMCPServer(
				"vmcp-server",
				"production",
				map[string]string{
					defaultRegistryDescriptionAnnotation: "Production Virtual MCP server",
					defaultRegistryURLAnnotation:         "https://api.prod.example.com/vmcp",
				},
			),
			wantSchema:  "https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json",
			wantName:    "com.toolhive.k8s.production/vmcp-server",
			wantVersion: "1.0.0",
			wantErr:     false,
			//nolint:thelper // We want to see these lines in the test output
			checkMeta: func(t *testing.T, sj *upstreamv0.ServerJSON) {
				assert.Equal(t, "Production Virtual MCP server", sj.Description)
				assert.NotNil(t, sj.Meta.PublisherProvided["io.github.stacklok"])
				ioStacklok := sj.Meta.PublisherProvided["io.github.stacklok"].(map[string]any)

				assert.NotNil(t, ioStacklok["https://api.prod.example.com/vmcp"])
				mcpMetadata := ioStacklok["https://api.prod.example.com/vmcp"].(map[string]any)

				assert.NotNil(t, mcpMetadata["metadata"])
				metadata := mcpMetadata["metadata"].(map[string]any)

				assert.NotNil(t, metadata["kubernetes"])
				kubernetes := metadata["kubernetes"].(map[string]any)
				assert.Equal(t, "production", kubernetes["namespace"])
				require.Len(t, sj.Remotes, 1)
				assert.Equal(t, "https://api.prod.example.com/vmcp", sj.Remotes[0].URL)
			},
		},
		{
			name: "VirtualMCPServer with valid tool_definitions",
			vmcpServer: createTestVirtualMCPServer(
				"vmcp-with-tools",
				"default",
				map[string]string{
					defaultRegistryDescriptionAnnotation:     "Virtual MCP with tools",
					defaultRegistryURLAnnotation:             "https://example.com/vmcp-tools",
					defaultRegistryToolDefinitionsAnnotation: `[{"name":"search_files","description":"Search for files","inputSchema":{"type":"object","properties":{"pattern":{"type":"string"}}},"annotations":{"readOnlyHint":true}}]`,
				},
			),
			wantSchema:  "https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json",
			wantName:    "com.toolhive.k8s.default/vmcp-with-tools",
			wantVersion: "1.0.0",
			wantErr:     false,
			//nolint:thelper // We want to see these lines in the test output
			checkMeta: func(t *testing.T, sj *upstreamv0.ServerJSON) {
				ioStacklok := sj.Meta.PublisherProvided["io.github.stacklok"].(map[string]any)
				mcpMetadata := ioStacklok["https://example.com/vmcp-tools"].(map[string]any)

				// Check tool_definitions is parsed correctly
				require.NotNil(t, mcpMetadata["tool_definitions"])
				toolDefs, ok := mcpMetadata["tool_definitions"].([]interface{})
				require.True(t, ok)
				require.Len(t, toolDefs, 1)

				tool := toolDefs[0].(map[string]interface{})
				assert.Equal(t, "search_files", tool["name"])
				assert.Equal(t, "Search for files", tool["description"])
			},
		},
		{
			name: "VirtualMCPServer with invalid JSON in tool_definitions",
			vmcpServer: createTestVirtualMCPServer(
				"vmcp-invalid",
				"default",
				map[string]string{
					defaultRegistryDescriptionAnnotation:     "VirtualMCP with invalid JSON",
					defaultRegistryURLAnnotation:             "https://example.com/vmcp-invalid",
					defaultRegistryToolDefinitionsAnnotation: `not valid json`,
				},
			),
			wantSchema:  "https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json",
			wantName:    "com.toolhive.k8s.default/vmcp-invalid",
			wantVersion: "1.0.0",
			wantErr:     false,
			//nolint:thelper // We want to see these lines in the test output
			checkMeta: func(t *testing.T, sj *upstreamv0.ServerJSON) {
				ioStacklok := sj.Meta.PublisherProvided["io.github.stacklok"].(map[string]any)
				mcpMetadata := ioStacklok["https://example.com/vmcp-invalid"].(map[string]any)

				// Invalid JSON should be skipped
				assert.Nil(t, mcpMetadata["tool_definitions"])
			},
		},
		{
			name: "VirtualMCPServer with valid tools annotation",
			vmcpServer: createTestVirtualMCPServer(
				"vmcp-with-tools-list",
				"default",
				map[string]string{
					defaultRegistryDescriptionAnnotation: "VirtualMCP with tools list",
					defaultRegistryURLAnnotation:         "https://example.com/vmcp-tools-list",
					defaultRegistryToolsAnnotation:       `["search_files","execute_command"]`,
				},
			),
			wantSchema:  "https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json",
			wantName:    "com.toolhive.k8s.default/vmcp-with-tools-list",
			wantVersion: "1.0.0",
			wantErr:     false,
			//nolint:thelper // We want to see these lines in the test output
			checkMeta: func(t *testing.T, sj *upstreamv0.ServerJSON) {
				ioStacklok := sj.Meta.PublisherProvided["io.github.stacklok"].(map[string]any)
				mcpMetadata := ioStacklok["https://example.com/vmcp-tools-list"].(map[string]any)

				// Check tools is present
				// After JSON marshaling/unmarshaling, []string becomes []interface{}
				require.NotNil(t, mcpMetadata["tools"])
				tools, ok := mcpMetadata["tools"].([]interface{})
				require.True(t, ok)
				require.Len(t, tools, 2)
				assert.Equal(t, "search_files", tools[0])
				assert.Equal(t, "execute_command", tools[1])
			},
		},
		{
			name: "VirtualMCPServer with both tool_definitions and tools",
			vmcpServer: createTestVirtualMCPServer(
				"vmcp-with-both",
				"default",
				map[string]string{
					defaultRegistryDescriptionAnnotation:     "VirtualMCP with both",
					defaultRegistryURLAnnotation:             "https://example.com/vmcp-both",
					defaultRegistryToolDefinitionsAnnotation: `[{"name":"search_files","description":"Search files"}]`,
					defaultRegistryToolsAnnotation:           `["search_files","execute_command"]`,
				},
			),
			wantSchema:  "https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json",
			wantName:    "com.toolhive.k8s.default/vmcp-with-both",
			wantVersion: "1.0.0",
			wantErr:     false,
			//nolint:thelper // We want to see these lines in the test output
			checkMeta: func(t *testing.T, sj *upstreamv0.ServerJSON) {
				ioStacklok := sj.Meta.PublisherProvided["io.github.stacklok"].(map[string]any)
				mcpMetadata := ioStacklok["https://example.com/vmcp-both"].(map[string]any)

				// Both should be present
				require.NotNil(t, mcpMetadata["tool_definitions"])
				// After JSON marshaling/unmarshaling, []string becomes []interface{}
				require.NotNil(t, mcpMetadata["tools"])
				tools, ok := mcpMetadata["tools"].([]interface{})
				require.True(t, ok)
				require.Len(t, tools, 2)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := extractVirtualMCPServer(tt.vmcpServer)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				assert.Nil(t, result)
			} else {
				require.NoError(t, err)
				require.NotNil(t, result)
				assert.Equal(t, tt.wantSchema, result.Schema)
				assert.Equal(t, tt.wantName, result.Name)
				assert.Equal(t, tt.wantVersion, result.Version)
				assert.NotNil(t, result.Meta)
				assert.NotNil(t, result.Meta.PublisherProvided)

				if tt.checkMeta != nil {
					tt.checkMeta(t, result)
				}
			}
		})
	}
}

// createTestMCPRemoteProxy creates a test MCPRemoteProxy with the given configuration
func createTestMCPRemoteProxy(name, namespace string, annotations map[string]string) *mcpv1alpha1.MCPRemoteProxy {
	return &mcpv1alpha1.MCPRemoteProxy{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			UID:         types.UID(uuid.New().String()),
			Annotations: annotations,
		},
	}
}

func TestExtractMCPRemoteProxy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		mcpRemoteProxy *mcpv1alpha1.MCPRemoteProxy
		wantSchema     string
		wantName       string
		wantVersion    string
		wantErr        bool
		errContains    string
		checkMeta      func(*testing.T, *upstreamv0.ServerJSON)
	}{
		{
			name: "valid MCPRemoteProxy with all required annotations",
			mcpRemoteProxy: createTestMCPRemoteProxy(
				"test-proxy",
				"default",
				map[string]string{
					defaultRegistryDescriptionAnnotation: "A test MCP Remote Proxy",
					defaultRegistryURLAnnotation:         "https://example.com/proxy",
				},
			),
			wantSchema:  "https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json",
			wantName:    "com.toolhive.k8s.default/test-proxy",
			wantVersion: "1.0.0",
			wantErr:     false,
			//nolint:thelper // We want to see these lines in the test output
			checkMeta: func(t *testing.T, sj *upstreamv0.ServerJSON) {
				assert.Equal(t, "A test MCP Remote Proxy", sj.Description)
				assert.NotNil(t, sj.Meta.PublisherProvided["io.github.stacklok"])
				ioStacklok := sj.Meta.PublisherProvided["io.github.stacklok"].(map[string]any)

				assert.NotNil(t, ioStacklok["https://example.com/proxy"])
				mcpMetadata := ioStacklok["https://example.com/proxy"].(map[string]any)

				assert.NotNil(t, mcpMetadata["metadata"])
				metadata := mcpMetadata["metadata"].(map[string]any)

				assert.NotNil(t, metadata["kubernetes"])
				kubernetes := metadata["kubernetes"].(map[string]any)

				assert.Equal(t, "default", kubernetes["namespace"])
				assert.Equal(t, "test-proxy", kubernetes["name"])
				assert.NotEmpty(t, kubernetes["uid"])
				require.Len(t, sj.Remotes, 1)
				assert.Equal(t, model.TransportTypeStreamableHTTP, sj.Remotes[0].Type)
				assert.Equal(t, "https://example.com/proxy", sj.Remotes[0].URL)
			},
		},
		{
			name: "MCPRemoteProxy with nil annotations",
			mcpRemoteProxy: createTestMCPRemoteProxy(
				"test-proxy",
				"default",
				nil,
			),
			wantErr:     true,
			errContains: "annotations not found",
		},
		{
			name: "MCPRemoteProxy with empty annotations map",
			mcpRemoteProxy: createTestMCPRemoteProxy(
				"test-proxy",
				"default",
				map[string]string{},
			),
			wantErr:     true,
			errContains: "description not found in annotations",
		},
		{
			name: "MCPRemoteProxy missing description annotation",
			mcpRemoteProxy: createTestMCPRemoteProxy(
				"test-proxy",
				"default",
				map[string]string{
					defaultRegistryURLAnnotation: "https://example.com/proxy",
				},
			),
			wantErr:     true,
			errContains: "description not found in annotations",
		},
		{
			name: "MCPRemoteProxy missing URL annotation",
			mcpRemoteProxy: createTestMCPRemoteProxy(
				"test-proxy",
				"default",
				map[string]string{
					defaultRegistryDescriptionAnnotation: "A test proxy",
				},
			),
			wantErr:     true,
			errContains: "URL not found in annotations",
		},
		{
			name: "MCPRemoteProxy with description set before required check",
			mcpRemoteProxy: createTestMCPRemoteProxy(
				"proxy-server",
				"custom-ns",
				map[string]string{
					defaultRegistryDescriptionAnnotation: "Proxy with description",
					defaultRegistryURLAnnotation:         "https://proxy.example.com",
				},
			),
			wantSchema:  "https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json",
			wantName:    "com.toolhive.k8s.custom-ns/proxy-server",
			wantVersion: "1.0.0",
			wantErr:     false,
			//nolint:thelper // We want to see these lines in the test output
			checkMeta: func(t *testing.T, sj *upstreamv0.ServerJSON) {
				assert.Equal(t, "Proxy with description", sj.Description)
				assert.NotNil(t, sj.Meta.PublisherProvided["io.github.stacklok"])
				ioStacklok := sj.Meta.PublisherProvided["io.github.stacklok"].(map[string]any)

				assert.NotNil(t, ioStacklok["https://proxy.example.com"])
				mcpMetadata := ioStacklok["https://proxy.example.com"].(map[string]any)

				assert.NotNil(t, mcpMetadata["metadata"])
				metadata := mcpMetadata["metadata"].(map[string]any)

				assert.NotNil(t, metadata["kubernetes"])
				kubernetes := metadata["kubernetes"].(map[string]any)

				assert.Equal(t, "custom-ns", kubernetes["namespace"])
				assert.Equal(t, "proxy-server", kubernetes["name"])
				assert.NotEmpty(t, kubernetes["uid"])
				require.Len(t, sj.Remotes, 1)
				assert.Equal(t, model.TransportTypeStreamableHTTP, sj.Remotes[0].Type)
				assert.Equal(t, "https://proxy.example.com", sj.Remotes[0].URL)
			},
		},
		{
			name: "MCPRemoteProxy with valid tool_definitions",
			mcpRemoteProxy: createTestMCPRemoteProxy(
				"proxy-with-tools",
				"default",
				map[string]string{
					defaultRegistryDescriptionAnnotation:     "Proxy with database tools",
					defaultRegistryURLAnnotation:             "https://example.com/proxy-tools",
					defaultRegistryToolDefinitionsAnnotation: `[{"name":"query_database","description":"Execute SQL query","inputSchema":{"type":"object","properties":{"query":{"type":"string"},"database":{"type":"string"}}},"outputSchema":{"type":"object"},"annotations":{"readOnlyHint":true}}]`,
				},
			),
			wantSchema:  "https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json",
			wantName:    "com.toolhive.k8s.default/proxy-with-tools",
			wantVersion: "1.0.0",
			wantErr:     false,
			//nolint:thelper // We want to see these lines in the test output
			checkMeta: func(t *testing.T, sj *upstreamv0.ServerJSON) {
				ioStacklok := sj.Meta.PublisherProvided["io.github.stacklok"].(map[string]any)
				mcpMetadata := ioStacklok["https://example.com/proxy-tools"].(map[string]any)

				// Check tool_definitions is parsed correctly
				require.NotNil(t, mcpMetadata["tool_definitions"])
				toolDefs, ok := mcpMetadata["tool_definitions"].([]interface{})
				require.True(t, ok)
				require.Len(t, toolDefs, 1)

				tool := toolDefs[0].(map[string]interface{})
				assert.Equal(t, "query_database", tool["name"])
				assert.Equal(t, "Execute SQL query", tool["description"])
				assert.NotNil(t, tool["inputSchema"])
				assert.NotNil(t, tool["outputSchema"])
				annotations := tool["annotations"].(map[string]interface{})
				assert.Equal(t, true, annotations["readOnlyHint"])
			},
		},
		{
			name: "MCPRemoteProxy with invalid JSON in tool_definitions",
			mcpRemoteProxy: createTestMCPRemoteProxy(
				"proxy-invalid",
				"default",
				map[string]string{
					defaultRegistryDescriptionAnnotation:     "Proxy with invalid JSON",
					defaultRegistryURLAnnotation:             "https://example.com/proxy-invalid",
					defaultRegistryToolDefinitionsAnnotation: `[invalid]`,
				},
			),
			wantSchema:  "https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json",
			wantName:    "com.toolhive.k8s.default/proxy-invalid",
			wantVersion: "1.0.0",
			wantErr:     false,
			//nolint:thelper // We want to see these lines in the test output
			checkMeta: func(t *testing.T, sj *upstreamv0.ServerJSON) {
				ioStacklok := sj.Meta.PublisherProvided["io.github.stacklok"].(map[string]any)
				mcpMetadata := ioStacklok["https://example.com/proxy-invalid"].(map[string]any)

				// Invalid JSON should be skipped
				assert.Nil(t, mcpMetadata["tool_definitions"])
			},
		},
		{
			name: "MCPRemoteProxy with valid tools annotation",
			mcpRemoteProxy: createTestMCPRemoteProxy(
				"proxy-with-tools-list",
				"default",
				map[string]string{
					defaultRegistryDescriptionAnnotation: "Proxy with tools list",
					defaultRegistryURLAnnotation:         "https://example.com/proxy-tools-list",
					defaultRegistryToolsAnnotation:       `["query_database","update_table"]`,
				},
			),
			wantSchema:  "https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json",
			wantName:    "com.toolhive.k8s.default/proxy-with-tools-list",
			wantVersion: "1.0.0",
			wantErr:     false,
			//nolint:thelper // We want to see these lines in the test output
			checkMeta: func(t *testing.T, sj *upstreamv0.ServerJSON) {
				ioStacklok := sj.Meta.PublisherProvided["io.github.stacklok"].(map[string]any)
				mcpMetadata := ioStacklok["https://example.com/proxy-tools-list"].(map[string]any)

				// Check tools is present
				// After JSON marshaling/unmarshaling, []string becomes []interface{}
				require.NotNil(t, mcpMetadata["tools"])
				tools, ok := mcpMetadata["tools"].([]interface{})
				require.True(t, ok)
				require.Len(t, tools, 2)
				assert.Equal(t, "query_database", tools[0])
				assert.Equal(t, "update_table", tools[1])
			},
		},
		{
			name: "MCPRemoteProxy with both tool_definitions and tools",
			mcpRemoteProxy: createTestMCPRemoteProxy(
				"proxy-with-both",
				"default",
				map[string]string{
					defaultRegistryDescriptionAnnotation:     "Proxy with both",
					defaultRegistryURLAnnotation:             "https://example.com/proxy-both",
					defaultRegistryToolDefinitionsAnnotation: `[{"name":"query_database","description":"Query DB"}]`,
					defaultRegistryToolsAnnotation:           `["query_database","update_table"]`,
				},
			),
			wantSchema:  "https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json",
			wantName:    "com.toolhive.k8s.default/proxy-with-both",
			wantVersion: "1.0.0",
			wantErr:     false,
			//nolint:thelper // We want to see these lines in the test output
			checkMeta: func(t *testing.T, sj *upstreamv0.ServerJSON) {
				ioStacklok := sj.Meta.PublisherProvided["io.github.stacklok"].(map[string]any)
				mcpMetadata := ioStacklok["https://example.com/proxy-both"].(map[string]any)

				// Both should be present
				require.NotNil(t, mcpMetadata["tool_definitions"])
				// After JSON marshaling/unmarshaling, []string becomes []interface{}
				require.NotNil(t, mcpMetadata["tools"])
				tools, ok := mcpMetadata["tools"].([]interface{})
				require.True(t, ok)
				require.Len(t, tools, 2)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := extractMCPRemoteProxy(tt.mcpRemoteProxy)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				assert.Nil(t, result)
			} else {
				require.NoError(t, err)
				require.NotNil(t, result)
				assert.Equal(t, tt.wantSchema, result.Schema)
				assert.Equal(t, tt.wantName, result.Name)
				assert.Equal(t, tt.wantVersion, result.Version)

				if tt.checkMeta != nil {
					tt.checkMeta(t, result)
				}
			}
		})
	}
}
