package kubernetes

import (
	"testing"

	"github.com/google/uuid"
	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	model "github.com/modelcontextprotocol/registry/pkg/model"
	mcpv1alpha1 "github.com/stacklok/toolhive/cmd/thv-operator/api/v1alpha1"
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
			wantSchema:  "https://static.modelcontextprotocol.io/schemas/2025-10-17/server.schema.json",
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

				assert.NotNil(t, mcpMetadata["metadata"])
				kubernetesMetadata := mcpMetadata["metadata"].(map[string]any)

				assert.Equal(t, "default", kubernetesMetadata["kubernetes_namespace"])
				assert.Equal(t, "test-server", kubernetesMetadata["kubernetes_name"])
				assert.Equal(t, "test/image:latest", kubernetesMetadata["kubernetes_image"])
				assert.Equal(t, "stdio", kubernetesMetadata["kubernetes_transport"])
				assert.NotEmpty(t, kubernetesMetadata["kubernetes_uid"])
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
			wantSchema:  "https://static.modelcontextprotocol.io/schemas/2025-10-17/server.schema.json",
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

				assert.NotNil(t, mcpMetadata["metadata"])
				kubernetesMetadata := mcpMetadata["metadata"].(map[string]any)

				assert.Equal(t, "custom-ns", kubernetesMetadata["kubernetes_namespace"])
				assert.Equal(t, "full-server", kubernetesMetadata["kubernetes_name"])
				assert.Equal(t, "registry.example.com/image:tag", kubernetesMetadata["kubernetes_image"])
				assert.Equal(t, "sse", kubernetesMetadata["kubernetes_transport"])
				assert.NotEmpty(t, kubernetesMetadata["kubernetes_uid"])
				require.Len(t, sj.Remotes, 1)
				assert.Equal(t, model.TransportTypeStreamableHTTP, sj.Remotes[0].Type)
				assert.Equal(t, "https://api.example.com/mcp-server", sj.Remotes[0].URL)
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
			wantSchema:  "https://static.modelcontextprotocol.io/schemas/2025-10-17/server.schema.json",
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
				kubernetesMetadata := mcpMetadata["metadata"].(map[string]any)

				assert.Equal(t, "default", kubernetesMetadata["kubernetes_namespace"])
				assert.Equal(t, "test-vmcp-server", kubernetesMetadata["kubernetes_name"])
				assert.NotEmpty(t, kubernetesMetadata["kubernetes_uid"])
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
			wantSchema:  "https://static.modelcontextprotocol.io/schemas/2025-10-17/server.schema.json",
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
				kubernetesMetadata := mcpMetadata["metadata"].(map[string]any)
				assert.Equal(t, "production", kubernetesMetadata["kubernetes_namespace"])
				require.Len(t, sj.Remotes, 1)
				assert.Equal(t, "https://api.prod.example.com/vmcp", sj.Remotes[0].URL)
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
			wantSchema:  "https://static.modelcontextprotocol.io/schemas/2025-10-17/server.schema.json",
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
				kubernetesMetadata := mcpMetadata["metadata"].(map[string]any)

				assert.Equal(t, "default", kubernetesMetadata["kubernetes_namespace"])
				assert.Equal(t, "test-proxy", kubernetesMetadata["kubernetes_name"])
				assert.NotEmpty(t, kubernetesMetadata["kubernetes_uid"])
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
			wantSchema:  "https://static.modelcontextprotocol.io/schemas/2025-10-17/server.schema.json",
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
				kubernetesMetadata := mcpMetadata["metadata"].(map[string]any)

				assert.Equal(t, "custom-ns", kubernetesMetadata["kubernetes_namespace"])
				assert.Equal(t, "proxy-server", kubernetesMetadata["kubernetes_name"])
				assert.NotEmpty(t, kubernetesMetadata["kubernetes_uid"])
				require.Len(t, sj.Remotes, 1)
				assert.Equal(t, model.TransportTypeStreamableHTTP, sj.Remotes[0].Type)
				assert.Equal(t, "https://proxy.example.com", sj.Remotes[0].URL)
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
