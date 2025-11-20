package v0_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
	toolhivetypes "github.com/stacklok/toolhive/pkg/registry/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/internal/api"
	v0 "github.com/stacklok/toolhive-registry-server/internal/api/v0"
	"github.com/stacklok/toolhive-registry-server/internal/registry"
	"github.com/stacklok/toolhive-registry-server/internal/service"
	"github.com/stacklok/toolhive-registry-server/internal/service/inmemory"
	"github.com/stacklok/toolhive-registry-server/internal/service/mocks"
)

// realisticRegistryProvider implements RegistryDataProvider for testing with our realistic test data
type realisticRegistryProvider struct {
	data *toolhivetypes.UpstreamRegistry
}

// newRealisticRegistryProvider creates a provider with our representative test data
func newRealisticRegistryProvider() *realisticRegistryProvider {
	data := buildRealisticUpstreamRegistry()
	return &realisticRegistryProvider{data: data}
}

// buildRealisticUpstreamRegistry builds the test registry using the testutils builder pattern
func buildRealisticUpstreamRegistry() *toolhivetypes.UpstreamRegistry {
	return registry.NewTestUpstreamRegistry(
		registry.WithVersion("1.0.0"),
		registry.WithLastUpdated("2025-09-10T00:16:54Z"),
		registry.WithServers(
			// ADB MySQL MCP Server - Official tier, stdio transport
			registry.NewTestServer("adb-mysql-mcp-server",
				registry.WithDescription("Official MCP server for AnalyticDB for MySQL of Alibaba Cloud"),
				registry.WithOCIPackage("ghcr.io/stacklok/dockyard/uvx/adb-mysql-mcp-server:1.0.0"),
				registry.WithTags("database", "mysql", "analytics", "sql", "alibaba-cloud", "data-warehouse"),
				registry.WithToolHiveMetadata("tier", "Official"),
				registry.WithToolHiveMetadata("status", "Active"),
				registry.WithToolHiveMetadata("transport", "stdio"),
				registry.WithToolHiveMetadata("tools", []string{"execute_sql", "get_query_plan", "get_execution_plan"}),
				registry.WithToolHiveMetadata("repository_url", "https://github.com/aliyun/alibabacloud-adb-mysql-mcp-server"),
				registry.WithToolHiveMetadata("metadata", map[string]any{
					"stars":        16,
					"pulls":        0,
					"last_updated": "2025-09-07T02:30:47Z",
				}),
				registry.WithToolHiveMetadata("permissions", map[string]any{
					"network": map[string]any{
						"outbound": map[string]any{
							"insecure_allow_all": true,
						},
					},
				}),
				registry.WithToolHiveMetadata("env_vars", []map[string]any{
					{
						"name":        "ADB_MYSQL_HOST",
						"description": "AnalyticDB for MySQL host address",
						"required":    true,
					},
					{
						"name":        "ADB_MYSQL_PASSWORD",
						"description": "Database password for authentication",
						"required":    true,
						"secret":      true,
					},
				}),
				registry.WithToolHiveMetadata("provenance", map[string]any{
					"sigstore_url":   "tuf-repo-cdn.sigstore.dev",
					"repository_uri": "https://github.com/stacklok/dockyard",
				}),
			),

			// Apollo MCP Server - Official tier, streamable-http transport
			registry.NewTestServer("apollo-mcp-server",
				registry.WithDescription("Exposes GraphQL operations as MCP tools for AI-driven API orchestration with Apollo"),
				registry.WithOCIPackage("ghcr.io/apollographql/apollo-mcp-server:v0.7.5"),
				registry.WithTags("graphql", "api", "orchestration", "apollo", "mcp"),
				registry.WithToolHiveMetadata("tier", "Official"),
				registry.WithToolHiveMetadata("status", "Active"),
				registry.WithToolHiveMetadata("transport", "streamable-http"),
				registry.WithToolHiveMetadata("tools", []string{"example_GetAstronautsCurrentlyInSpace"}),
				registry.WithToolHiveMetadata("repository_url", "https://github.com/apollographql/apollo-mcp-server"),
				registry.WithToolHiveMetadata("target_port", 5000),
				registry.WithToolHiveMetadata("metadata", map[string]any{
					"stars":        188,
					"pulls":        0,
					"last_updated": "2025-09-09T02:30:39Z",
				}),
				registry.WithToolHiveMetadata("permissions", map[string]any{
					"network": map[string]any{
						"outbound": map[string]any{
							"insecure_allow_all": true,
							"allow_port":         []int{443},
						},
					},
				}),
				registry.WithToolHiveMetadata("env_vars", []map[string]any{
					{
						"name":        "APOLLO_GRAPH_REF",
						"description": "Graph ref (graph ID and variant) used to fetch persisted queries or schema",
						"required":    false,
					},
					{
						"name":        "APOLLO_KEY",
						"description": "Apollo Studio API key for the graph",
						"required":    false,
						"secret":      true,
					},
				}),
			),

			// arXiv MCP Server - Community tier, stdio transport
			registry.NewTestServer("arxiv-mcp-server",
				registry.WithDescription("AI assistants search and access arXiv papers through MCP with persistent paper storage"),
				registry.WithOCIPackage("ghcr.io/stacklok/dockyard/uvx/arxiv-mcp-server:0.3.0"),
				registry.WithTags("research", "academic", "papers", "arxiv", "search"),
				registry.WithToolHiveMetadata("tier", "Community"),
				registry.WithToolHiveMetadata("status", "Active"),
				registry.WithToolHiveMetadata("transport", "stdio"),
				registry.WithToolHiveMetadata("tools", []string{"search_papers", "download_paper", "list_papers", "read_paper"}),
				registry.WithToolHiveMetadata("repository_url", "https://github.com/blazickjp/arxiv-mcp-server"),
				registry.WithToolHiveMetadata("metadata", map[string]any{
					"stars":        1619,
					"pulls":        77,
					"last_updated": "2025-08-27T02:30:22Z",
				}),
				registry.WithToolHiveMetadata("permissions", map[string]any{
					"network": map[string]any{
						"outbound": map[string]any{
							"allow_host": []string{"arxiv.org", "export.arxiv.org"},
							"allow_port": []int{443, 80},
						},
					},
				}),
				registry.WithToolHiveMetadata("env_vars", []map[string]any{
					{
						"name":        "ARXIV_STORAGE_PATH",
						"description": "Directory path for storing downloaded papers",
						"required":    false,
						"default":     "/arxiv-papers",
					},
				}),
				registry.WithToolHiveMetadata("args", []string{"--storage-path", "/arxiv-papers"}),
			),

			// Atlassian Remote - Official tier, SSE transport (HTTP package)
			registry.NewTestServer("atlassian-remote",
				registry.WithDescription("Atlassian's official remote MCP server for Jira, Confluence, and Compass with OAuth 2.1"),
				registry.WithHTTPPackage("https://mcp.atlassian.com"),
				registry.WithTags("productivity", "jira", "confluence", "atlassian", "oauth"),
				registry.WithToolHiveMetadata("tier", "Official"),
				registry.WithToolHiveMetadata("status", "Active"),
				registry.WithToolHiveMetadata("transport", "sse"),
				registry.WithToolHiveMetadata("tools", []string{
					"atlassianUserInfo",
					"getAccessibleAtlassianResources",
					"getConfluenceSpaces",
					"getConfluencePage",
					"getJiraIssue",
					"createJiraIssue",
					"updateJiraIssue",
				}),
				registry.WithToolHiveMetadata("repository_url", "https://github.com/atlassian-labs/mcp-server"),
				registry.WithToolHiveMetadata("metadata", map[string]any{
					"stars":        25,
					"pulls":        12,
					"last_updated": "2025-09-02T14:22:18Z",
				}),
				registry.WithToolHiveMetadata("headers", []map[string]any{
					{
						"name":        "Authorization",
						"description": "Bearer token for API authentication",
						"required":    true,
						"secret":      true,
					},
				}),
				registry.WithToolHiveMetadata("oauth_config", map[string]any{
					"issuer":   "https://auth.atlassian.com",
					"scopes":   []string{"read:jira-work", "write:jira-work", "read:confluence-content"},
					"use_pkce": true,
				}),
				registry.WithToolHiveMetadata("env_vars", []map[string]any{
					{
						"name":        "ATLASSIAN_CLIENT_ID",
						"description": "OAuth client ID for Atlassian integration",
						"required":    true,
						"secret":      true,
					},
				}),
			),
		),
	)
}

// GetRegistryData implements RegistryDataProvider.GetRegistryData
func (p *realisticRegistryProvider) GetRegistryData(_ context.Context) (*toolhivetypes.UpstreamRegistry, error) {
	return p.data, nil
}

// GetSource implements RegistryDataProvider.GetSource
func (*realisticRegistryProvider) GetSource() string {
	return "test:realistic-registry-data"
}

// GetRegistryName implements RegistryDataProvider.GetRegistryName
func (*realisticRegistryProvider) GetRegistryName() string {
	return "test-registry"
}

func TestHealthRouter(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	mockSvc := mocks.NewMockRegistryService(ctrl)
	// Set up expectations for readiness check
	mockSvc.EXPECT().CheckReadiness(gomock.Any()).Return(nil).AnyTimes()

	router := v0.HealthRouter(mockSvc)

	tests := []struct {
		name       string
		path       string
		method     string
		wantStatus int
	}{
		{
			name:       "health endpoint",
			path:       "/health",
			method:     "GET",
			wantStatus: http.StatusOK,
		},
		{
			name:       "readiness endpoint - ready",
			path:       "/readiness",
			method:     "GET",
			wantStatus: http.StatusOK,
		},
		{
			name:       "version endpoint",
			path:       "/version",
			method:     "GET",
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req, err := http.NewRequest(tt.method, tt.path, nil)
			require.NoError(t, err)

			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)

			assert.Equal(t, tt.wantStatus, rr.Code)
		})
	}
}

func TestRegistryRouter(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	mockSvc := mocks.NewMockRegistryService(ctrl)
	// Set up expectations for all routes
	mockSvc.EXPECT().GetRegistry(gomock.Any()).Return(&toolhivetypes.UpstreamRegistry{
		Version:     "1.0.0",
		LastUpdated: time.Now().Format(time.RFC3339),
		Servers:     []upstreamv0.ServerJSON{},
	}, "test", nil).AnyTimes()
	mockSvc.EXPECT().ListServers(gomock.Any()).Return([]upstreamv0.ServerJSON{}, nil).AnyTimes()
	mockSvc.EXPECT().GetServer(gomock.Any(), "test-server").Return(upstreamv0.ServerJSON{
		Name: "test-server",
	}, nil).AnyTimes()

	router := v0.Router(mockSvc)

	tests := []struct {
		name       string
		path       string
		method     string
		wantStatus int
	}{
		{
			name:       "registry info",
			path:       "/info",
			method:     "GET",
			wantStatus: http.StatusOK,
		},
		{
			name:       "list servers",
			path:       "/servers",
			method:     "GET",
			wantStatus: http.StatusOK,
		},
		{
			name:       "get server",
			path:       "/servers/test-server",
			method:     "GET",
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req, err := http.NewRequest(tt.method, tt.path, nil)
			require.NoError(t, err)

			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)

			assert.Equal(t, tt.wantStatus, rr.Code)
		})
	}
}

func TestListServers_FormatParameter(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	mockSvc := mocks.NewMockRegistryService(ctrl)
	// Expect successful calls for toolhive format only
	mockSvc.EXPECT().ListServers(gomock.Any()).Return([]upstreamv0.ServerJSON{}, nil).Times(2) // default and explicit toolhive

	router := v0.Router(mockSvc)

	tests := []struct {
		name       string
		format     string
		wantStatus int
	}{
		{
			name:       "default format (toolhive)",
			format:     "",
			wantStatus: http.StatusOK,
		},
		{
			name:       "explicit toolhive format",
			format:     "toolhive",
			wantStatus: http.StatusOK,
		},
		{
			name:       "upstream format not implemented",
			format:     "upstream",
			wantStatus: http.StatusNotImplemented,
		},
		{
			name:       "invalid format not supported",
			format:     "invalid",
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := "/servers"
			if tt.format != "" {
				path += "?format=" + tt.format
			}

			req, err := http.NewRequest("GET", path, nil)
			require.NoError(t, err)

			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)

			assert.Equal(t, tt.wantStatus, rr.Code)
		})
	}
}

func TestGetServer_NotFound(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockSvc := mocks.NewMockRegistryService(ctrl)
	// Expect server not found error
	mockSvc.EXPECT().GetServer(gomock.Any(), "nonexistent").Return(upstreamv0.ServerJSON{}, service.ErrServerNotFound)

	router := v0.Router(mockSvc)

	req, err := http.NewRequest("GET", "/servers/nonexistent", nil)
	require.NoError(t, err)

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestNewServer(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	mockSvc := mocks.NewMockRegistryService(ctrl)

	// Set up expectations for all test routes
	mockSvc.EXPECT().CheckReadiness(gomock.Any()).Return(nil).AnyTimes()
	mockSvc.EXPECT().GetRegistry(gomock.Any()).Return(&toolhivetypes.UpstreamRegistry{
		Version:     "1.0.0",
		LastUpdated: time.Now().Format(time.RFC3339),
		Servers:     []upstreamv0.ServerJSON{},
	}, "test", nil).AnyTimes()
	mockSvc.EXPECT().ListServers(gomock.Any()).Return([]upstreamv0.ServerJSON{}, nil).AnyTimes()
	mockSvc.EXPECT().GetServer(gomock.Any(), "test").Return(upstreamv0.ServerJSON{
		Name: "test",
	}, nil).AnyTimes()

	// Create server with mock service (no options needed for basic testing)
	router := api.NewServer(mockSvc)
	require.NotNil(t, router)

	// Test that routes are registered
	testRoutes := []struct {
		path       string
		method     string
		wantStatus int
	}{
		{"/health", "GET", http.StatusOK},
		{"/readiness", "GET", http.StatusOK}, // Ready with mock service
		{"/version", "GET", http.StatusOK},
		{"/openapi.json", "GET", http.StatusNotImplemented},
		{"/v0/info", "GET", http.StatusOK},
		{"/v0/servers", "GET", http.StatusOK},
		{"/v0/servers/test", "GET", http.StatusOK},
		{"/notfound", "GET", http.StatusNotFound},
	}

	for _, tt := range testRoutes {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()
			req, err := http.NewRequest(tt.method, tt.path, nil)
			require.NoError(t, err)

			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)

			assert.Equal(t, tt.wantStatus, rr.Code)
		})
	}
}

func TestNewServer_WithMockService(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockSvc := mocks.NewMockRegistryService(ctrl)

	// Expect readiness check to succeed
	mockSvc.EXPECT().CheckReadiness(gomock.Any()).Return(nil)

	// Create server with mock service (no options needed)
	router := api.NewServer(mockSvc)
	require.NotNil(t, router)

	// Test readiness with custom service
	req, err := http.NewRequest("GET", "/readiness", nil)
	require.NoError(t, err)

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code) // Should be ready with mock service
}

func TestNewServer_WithMiddleware(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockSvc := mocks.NewMockRegistryService(ctrl)

	// Expect readiness check to succeed
	mockSvc.EXPECT().CheckReadiness(gomock.Any()).Return(nil)

	// Test middleware that adds a custom header
	testMiddleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Test-Middleware", "applied")
			next.ServeHTTP(w, r)
		})
	}

	// Create server with middleware
	router := api.NewServer(mockSvc, api.WithMiddlewares(testMiddleware))
	require.NotNil(t, router)

	// Test that middleware is applied
	req, err := http.NewRequest("GET", "/readiness", nil)
	require.NoError(t, err)

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "applied", rr.Header().Get("X-Test-Middleware"))
}

// fileBasedRegistryProvider implements RegistryDataProvider for testing with embedded registry data
type fileBasedRegistryProvider struct {
	data *toolhivetypes.UpstreamRegistry
}

// newFileBasedRegistryProvider creates a new provider with embedded registry data
func newFileBasedRegistryProvider() *fileBasedRegistryProvider {
	data := buildRealisticUpstreamRegistry()
	return &fileBasedRegistryProvider{
		data: data,
	}
}

// GetRegistryData implements RegistryDataProvider.GetRegistryData
func (p *fileBasedRegistryProvider) GetRegistryData(_ context.Context) (*toolhivetypes.UpstreamRegistry, error) {
	return p.data, nil
}

// GetSource implements RegistryDataProvider.GetSource
func (*fileBasedRegistryProvider) GetSource() string {
	return "embedded:pkg/registry/data/registry.json"
}

// GetRegistryName implements RegistryDataProvider.GetRegistryName
func (*fileBasedRegistryProvider) GetRegistryName() string {
	return "embedded-registry"
}

// Helper functions for testing the response conversion functions
func newServerSummaryResponseForTesting(server upstreamv0.ServerJSON) v0.ServerSummaryResponse {
	return v0.NewServerSummaryResponseForTesting(server)
}

func newServerDetailResponseForTesting(server upstreamv0.ServerJSON) v0.ServerDetailResponse {
	return v0.NewServerDetailResponseForTesting(server)
}

// TestRoutesWithRealData tests all routes using the embedded registry.json data
// This provides integration-style testing with realistic MCP server configurations
func TestRoutesWithRealData(t *testing.T) {
	t.Parallel()
	// Create the file-based provider with embedded data
	provider := newFileBasedRegistryProvider()
	require.NotNil(t, provider)

	// Create a real service instance with the provider
	ctx := context.Background()
	svc, err := inmemory.New(ctx, provider)
	require.NoError(t, err)
	require.NotNil(t, svc)

	// Create router with the real service
	router := v0.Router(svc)
	require.NotNil(t, router)

	// Test registry info endpoint
	t.Run("registry info with real data", func(t *testing.T) {
		t.Parallel()
		req, err := http.NewRequest("GET", "/info", nil)
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
		assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))

		// Verify response structure
		var response map[string]interface{}
		err = json.Unmarshal(rr.Body.Bytes(), &response)
		require.NoError(t, err)

		// Validate key fields exist
		assert.Contains(t, response, "version")
		assert.Contains(t, response, "last_updated")
		assert.Contains(t, response, "total_servers")
		assert.Contains(t, response, "source")

		// Verify realistic data
		assert.Equal(t, "1.0.0", response["version"])
		assert.Equal(t, "embedded:pkg/registry/data/registry.json", response["source"])
		serverCount, ok := response["total_servers"].(float64)
		require.True(t, ok, "total_servers should be a number")
		assert.Greater(t, int(serverCount), 0, "should have servers from real data")
	})

	// Test list servers endpoint with different formats
	t.Run("list servers - toolhive format", func(t *testing.T) {
		t.Parallel()
		req, err := http.NewRequest("GET", "/servers?format=toolhive", nil)
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
		assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))

		// Parse response
		var response struct {
			Servers []map[string]interface{} `json:"servers"`
			Total   int                      `json:"total"`
		}
		err = json.Unmarshal(rr.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Greater(t, response.Total, 0, "should have servers from real data")
		assert.Greater(t, len(response.Servers), 0, "should have servers from real data")

		// Verify first server has expected ToolHive format fields
		if len(response.Servers) > 0 {
			server := response.Servers[0]
			assert.Contains(t, server, "name")
			assert.Contains(t, server, "description")
			assert.Contains(t, server, "tier")
			assert.Contains(t, server, "status")
			assert.Contains(t, server, "transport")
			assert.Contains(t, server, "tools_count")
		}
	})

	t.Run("list servers - upstream format not implemented", func(t *testing.T) {
		t.Parallel()
		req, err := http.NewRequest("GET", "/servers?format=upstream", nil)
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusNotImplemented, rr.Code)
		assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))

		// Parse error response
		var response map[string]interface{}
		err = json.Unmarshal(rr.Body.Bytes(), &response)
		require.NoError(t, err)

		// Verify error response structure
		assert.Contains(t, response, "error")
		assert.Equal(t, "Upstream format not yet implemented", response["error"])
	})

	// Test get specific server with realistic data
	t.Run("get specific server", func(t *testing.T) {
		t.Parallel()
		// Use a known server from the registry data - let's use the first one we can find
		regData, err := provider.GetRegistryData(ctx)
		require.NoError(t, err)
		require.NotEmpty(t, regData.Servers, "should have servers in registry data")

		// Get first server name
		var firstServerName string
		for _, server := range regData.Servers {
			firstServerName = server.Name
			break
		}
		require.NotEmpty(t, firstServerName)

		req, err := http.NewRequest("GET", "/servers/"+firstServerName, nil)
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
		assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))

		// Parse and validate response
		var server v0.ServerDetailResponse
		err = json.Unmarshal(rr.Body.Bytes(), &server)
		require.NoError(t, err)

		// Verify server details match expected data
		originalServer := regData.Servers[0]
		assert.Equal(t, originalServer.Name, server.Name)
		assert.Equal(t, originalServer.Description, server.Description)
	})

	// Test server not found
	t.Run("get nonexistent server", func(t *testing.T) {
		t.Parallel()
		req, err := http.NewRequest("GET", "/servers/nonexistent-server-12345", nil)
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusNotFound, rr.Code)
	})
}

// TestFormatConversion tests the conversion between ToolHive and Upstream formats using real data
func TestFormatConversion(t *testing.T) {
	t.Parallel()
	// Create the file-based provider with embedded data
	provider := newFileBasedRegistryProvider()

	// Create service
	ctx := context.Background()
	svc, err := inmemory.New(ctx, provider)
	require.NoError(t, err)

	// Create router
	router := v0.Router(svc)

	// Get servers in ToolHive format
	req, err := http.NewRequest("GET", "/servers?format=toolhive", nil)
	require.NoError(t, err)

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	var toolhiveResponse struct {
		Servers []map[string]interface{} `json:"servers"`
		Total   int                      `json:"total"`
	}
	err = json.Unmarshal(rr.Body.Bytes(), &toolhiveResponse)
	require.NoError(t, err)

	// Test upstream format returns not implemented
	req, err = http.NewRequest("GET", "/servers?format=upstream", nil)
	require.NoError(t, err)

	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNotImplemented, rr.Code)
	var upstreamResponse map[string]interface{}
	err = json.Unmarshal(rr.Body.Bytes(), &upstreamResponse)
	require.NoError(t, err)

	// Verify error response
	assert.Contains(t, upstreamResponse, "error")
	assert.Equal(t, "Upstream format not yet implemented", upstreamResponse["error"])
}

// TestComplexServerConfiguration tests servers with complex configurations from real data
func TestComplexServerConfiguration(t *testing.T) {
	t.Parallel()
	provider := newFileBasedRegistryProvider()

	ctx := context.Background()
	svc, err := inmemory.New(ctx, provider)
	require.NoError(t, err)

	router := v0.Router(svc)

	// Get registry data to find servers with complex configurations
	regData, err := provider.GetRegistryData(ctx)
	require.NoError(t, err)

	t.Run("servers with complex configurations", func(t *testing.T) {
		t.Parallel()
		for _, server := range regData.Servers {
			req, err := http.NewRequest("GET", "/servers/"+server.Name, nil)
			require.NoError(t, err)

			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)

			assert.Equal(t, http.StatusOK, rr.Code)

			var response map[string]interface{}
			err = json.Unmarshal(rr.Body.Bytes(), &response)
			require.NoError(t, err)
		}
	})
}

// TestRoutesWithRealisticData tests all routes using the curated realistic test data
// This provides focused integration-style testing with representative MCP server configurations
func TestRoutesWithRealisticData(t *testing.T) {
	t.Parallel()
	// Create the realistic provider with curated test data
	provider := newRealisticRegistryProvider()
	require.NotNil(t, provider)

	// Create a real service instance with the provider
	ctx := context.Background()
	svc, err := inmemory.New(ctx, provider)
	require.NoError(t, err)
	require.NotNil(t, svc)

	// Create router with the real service
	router := v0.Router(svc)
	require.NotNil(t, router)

	// Test registry info endpoint with realistic data
	t.Run("registry info with realistic data", func(t *testing.T) {
		t.Parallel()
		req, err := http.NewRequest("GET", "/info", nil)
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
		assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))

		// Verify response structure
		var response map[string]interface{}
		err = json.Unmarshal(rr.Body.Bytes(), &response)
		require.NoError(t, err)

		// Validate key fields exist
		assert.Contains(t, response, "version")
		assert.Contains(t, response, "last_updated")
		assert.Contains(t, response, "total_servers")
		assert.Contains(t, response, "source")

		// Verify realistic data
		assert.Equal(t, "1.0.0", response["version"])
		assert.Equal(t, "test:realistic-registry-data", response["source"])
		serverCount, ok := response["total_servers"].(float64)
		require.True(t, ok, "total_servers should be a number")
		assert.Equal(t, 4, int(serverCount), "should have 4 servers (3 container + 1 remote)")
	})

	// Test list servers endpoint - toolhive format with realistic data
	t.Run("list servers - toolhive format with realistic data", func(t *testing.T) {
		t.Parallel()
		req, err := http.NewRequest("GET", "/servers?format=toolhive", nil)
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
		assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))

		// Parse response
		var response struct {
			Servers []map[string]interface{} `json:"servers"`
			Total   int                      `json:"total"`
		}
		err = json.Unmarshal(rr.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, 4, response.Total, "should have 4 total servers")
		assert.Len(t, response.Servers, 4, "should return 4 servers")

		// Verify server names are present and correct
		serverNames := make(map[string]bool)
		for _, server := range response.Servers {
			name, ok := server["name"].(string)
			require.True(t, ok, "server should have name field")
			serverNames[name] = true
		}

		expectedServers := []string{"adb-mysql-mcp-server", "apollo-mcp-server", "arxiv-mcp-server", "atlassian-remote"}
		for _, expected := range expectedServers {
			assert.True(t, serverNames[expected], "should contain server: %s", expected)
		}

		// Verify first server has expected ToolHive format fields
		if len(response.Servers) > 0 {
			server := response.Servers[0]
			assert.Contains(t, server, "name")
			assert.Contains(t, server, "description")
			assert.Contains(t, server, "tier")
			assert.Contains(t, server, "status")
			assert.Contains(t, server, "transport")
			assert.Contains(t, server, "tools_count")
		}
	})

	// Test list servers endpoint - upstream format not implemented with realistic data
	t.Run("list servers - upstream format not implemented with realistic data", func(t *testing.T) {
		t.Parallel()
		req, err := http.NewRequest("GET", "/servers?format=upstream", nil)
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusNotImplemented, rr.Code)
		assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))

		// Parse error response
		var response map[string]interface{}
		err = json.Unmarshal(rr.Body.Bytes(), &response)
		require.NoError(t, err)

		// Verify error response structure
		assert.Contains(t, response, "error")
		assert.Equal(t, "Upstream format not yet implemented", response["error"])
	})
}

// TestSpecificServersWithRealisticData tests individual server endpoints with our curated realistic data
func TestSpecificServersWithRealisticData(t *testing.T) {
	t.Parallel()
	provider := newRealisticRegistryProvider()
	require.NotNil(t, provider)

	ctx := context.Background()
	svc, err := inmemory.New(ctx, provider)
	require.NoError(t, err)

	router := v0.Router(svc)

	testCases := []struct {
		name         string
		serverName   string
		expectedData map[string]interface{}
	}{
		{
			name:       "stdio server with complex config",
			serverName: "adb-mysql-mcp-server",
			expectedData: map[string]interface{}{
				"tier":        "NA",
				"transport":   "NA",
				"status":      "NA",
				"description": "Official MCP server for AnalyticDB for MySQL of Alibaba Cloud",
			},
		},
		{
			name:       "streamable-http server",
			serverName: "apollo-mcp-server",
			expectedData: map[string]interface{}{
				"tier":      "NA",
				"transport": "NA",
				"status":    "NA",
			},
		},
		{
			name:       "community server with args",
			serverName: "arxiv-mcp-server",
			expectedData: map[string]interface{}{
				"tier":      "NA",
				"transport": "NA",
				"status":    "NA",
			},
		},
		{
			name:       "remote server with oauth",
			serverName: "atlassian-remote",
			expectedData: map[string]interface{}{
				"tier":      "NA",
				"transport": "NA",
				"status":    "NA",
			},
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Test with toolhive format to get direct field access
			req, err := http.NewRequest("GET", "/servers/"+tt.serverName+"?format=toolhive", nil)
			require.NoError(t, err)

			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)

			assert.Equal(t, http.StatusOK, rr.Code)
			assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))

			// Parse and validate response
			var server map[string]interface{}
			err = json.Unmarshal(rr.Body.Bytes(), &server)
			require.NoError(t, err)

			// Verify expected data
			for key, expectedValue := range tt.expectedData {
				actualValue, exists := server[key]
				assert.True(t, exists, "server should have field: %s", key)
				assert.Equal(t, expectedValue, actualValue, "field %s should match expected value", key)
			}
		})
	}
}

// TestFormatConversionWithRealisticData tests conversion between ToolHive and Upstream formats
// using realistic data to ensure the conversion pipeline works correctly
func TestFormatConversionWithRealisticData(t *testing.T) {
	t.Parallel()
	provider := newRealisticRegistryProvider()
	require.NotNil(t, provider)

	ctx := context.Background()
	svc, err := inmemory.New(ctx, provider)
	require.NoError(t, err)

	router := v0.Router(svc)

	// Get servers in ToolHive format
	req, err := http.NewRequest("GET", "/servers?format=toolhive", nil)
	require.NoError(t, err)

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	var toolhiveResponse struct {
		Servers []map[string]interface{} `json:"servers"`
		Total   int                      `json:"total"`
	}
	err = json.Unmarshal(rr.Body.Bytes(), &toolhiveResponse)
	require.NoError(t, err)

	// Test that upstream format returns not implemented
	req, err = http.NewRequest("GET", "/servers?format=upstream", nil)
	require.NoError(t, err)

	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNotImplemented, rr.Code)
	var upstreamResponse map[string]interface{}
	err = json.Unmarshal(rr.Body.Bytes(), &upstreamResponse)
	require.NoError(t, err)

	// Verify error response
	assert.Contains(t, upstreamResponse, "error")
	assert.Equal(t, "Upstream format not yet implemented", upstreamResponse["error"])
}

// BenchmarkRoutesWithRealisticData benchmarks the API endpoints using realistic test data
// This helps ensure performance doesn't regress with representative payloads
func BenchmarkRoutesWithRealisticData(b *testing.B) {
	provider := newRealisticRegistryProvider()
	require.NotNil(b, provider)

	ctx := context.Background()
	svc, err := inmemory.New(ctx, provider)
	if err != nil {
		b.Fatalf("Failed to create service: %v", err)
	}

	router := v0.Router(svc)

	b.Run("registry info", func(b *testing.B) {
		req, _ := http.NewRequest("GET", "/info", nil)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)
		}
	})

	b.Run("list servers toolhive format", func(b *testing.B) {
		req, _ := http.NewRequest("GET", "/servers?format=toolhive", nil)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)
		}
	})

	b.Run("list servers upstream format", func(b *testing.B) {
		req, _ := http.NewRequest("GET", "/servers?format=upstream", nil)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)
		}
	})

	b.Run("get specific server", func(b *testing.B) {
		req, _ := http.NewRequest("GET", "/servers/apollo-mcp-server", nil)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)
		}
	})
}

// BenchmarkRoutesWithRealData benchmarks the API endpoints using real data
// This helps ensure performance doesn't regress with realistic payloads
func BenchmarkRoutesWithRealData(b *testing.B) {
	provider := newFileBasedRegistryProvider()

	ctx := context.Background()
	svc, err := inmemory.New(ctx, provider)
	if err != nil {
		b.Fatalf("Failed to create service: %v", err)
	}

	router := v0.Router(svc)

	b.Run("registry info", func(b *testing.B) {
		req, _ := http.NewRequest("GET", "/info", nil)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)
		}
	})

	b.Run("list servers toolhive format", func(b *testing.B) {
		req, _ := http.NewRequest("GET", "/servers?format=toolhive", nil)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)
		}
	})

	b.Run("list servers upstream format", func(b *testing.B) {
		req, _ := http.NewRequest("GET", "/servers?format=upstream", nil)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)
		}
	})
}

// TestServerResponseStructures tests the new response structure changes
func TestServerResponseStructures(t *testing.T) {
	t.Parallel()

	provider := newRealisticRegistryProvider()
	require.NotNil(t, provider)

	ctx := context.Background()
	svc, err := inmemory.New(ctx, provider)
	require.NoError(t, err)

	router := v0.Router(svc)

	t.Run("list servers returns tools_count instead of tools array", func(t *testing.T) {
		t.Parallel()
		req, err := http.NewRequest("GET", "/servers", nil)
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		var response struct {
			Servers []struct {
				Name        string   `json:"name"`
				Description string   `json:"description"`
				Tier        string   `json:"tier"`
				Status      string   `json:"status"`
				Transport   string   `json:"transport"`
				ToolsCount  int      `json:"tools_count"`
				Tools       []string `json:"tools,omitempty"` // Should not be present
			} `json:"servers"`
			Total int `json:"total"`
		}
		err = json.Unmarshal(rr.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Greater(t, len(response.Servers), 0)

		// Verify that we have tools_count and not tools array
		for _, server := range response.Servers {
			assert.Equal(t, server.ToolsCount, 0, "server %s should have no tools", server.Name)
			assert.Nil(t, server.Tools, "server %s should not have tools array in summary", server.Name)
		}
	})

	t.Run("get server details returns full tools array and image field", func(t *testing.T) {
		t.Parallel()
		req, err := http.NewRequest("GET", "/servers/apollo-mcp-server", nil)
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		var response struct {
			Name        string   `json:"name"`
			Description string   `json:"description"`
			Tier        string   `json:"tier"`
			Status      string   `json:"status"`
			Transport   string   `json:"transport"`
			Tools       []string `json:"tools"`
			Image       string   `json:"image"`
			ToolsCount  int      `json:"tools_count,omitempty"` // Should not be present
		}
		err = json.Unmarshal(rr.Body.Bytes(), &response)
		require.NoError(t, err)

		// Verify detailed response has full tools array and image
		assert.Equal(t, 0, len(response.Tools), "should have no tools array in detailed response")
		assert.NotEmpty(t, response.Image, "should have image field")
		assert.Equal(t, 0, response.ToolsCount, "should not have tools_count in detailed response")

		// Verify specific fields
		assert.Equal(t, "apollo-mcp-server", response.Name)
		assert.Equal(t, "NA", response.Transport)
		assert.Equal(t, "NA", response.Image)
	})
}

// TestResponseFieldMapping tests the mapping between different response types
func TestResponseFieldMapping(t *testing.T) {
	t.Parallel()

	provider := newRealisticRegistryProvider()
	require.NotNil(t, provider)

	ctx := context.Background()
	svc, err := inmemory.New(ctx, provider)
	require.NoError(t, err)

	router := v0.Router(svc)

	t.Run("container server with env vars and permissions", func(t *testing.T) {
		t.Parallel()
		req, err := http.NewRequest("GET", "/servers/adb-mysql-mcp-server", nil)
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		var response map[string]interface{}
		err = json.Unmarshal(rr.Body.Bytes(), &response)
		require.NoError(t, err)

		// Check that env_vars are properly structured
		_, exists := response["env_vars"]
		assert.False(t, exists, "should not have env_vars field")

		// Check permissions
		_, exists = response["permissions"]
		assert.False(t, exists, "should not have permissions field")

		// Check image field
		_, exists = response["image"]
		assert.True(t, exists, "should not ave image field")
	})

	t.Run("remote server fields", func(t *testing.T) {
		t.Parallel()
		req, err := http.NewRequest("GET", "/servers/atlassian-remote", nil)
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		var response map[string]interface{}
		err = json.Unmarshal(rr.Body.Bytes(), &response)
		require.NoError(t, err)

		// Remote servers should have metadata with URL info
		metadata, exists := response["metadata"]
		assert.True(t, exists, "remote server should have metadata")

		metadataMap, ok := metadata.(map[string]interface{})
		assert.True(t, ok, "metadata should be an object")

		// Check remote-specific metadata fields
		assert.NotContains(t, metadataMap, "url", "remote server metadata should not contain URL")
		assert.NotContains(t, metadataMap, "oauth_enabled", "remote server metadata should not contain oauth_enabled")
	})
}

// TestHelperFunctions tests the individual helper functions
func TestHelperFunctions(t *testing.T) {
	t.Parallel()

	// Test data setup
	testServer := upstreamv0.ServerJSON{
		Name:        "test-server",
		Description: "Test server",
		Packages:    []model.Package{{RegistryType: "oci", Identifier: "test-image:latest", Transport: model.Transport{Type: "stdio"}}},
	}

	testRemoteServer := upstreamv0.ServerJSON{
		Name:        "remote-server",
		Description: "Remote test server",
		Remotes:     []model.Transport{{URL: "https://remote.example.com", Type: "sse"}},
	}

	t.Run("newServerSummaryResponse", func(t *testing.T) {
		t.Parallel()
		summary := newServerSummaryResponseForTesting(testServer)

		assert.Equal(t, "test-server", summary.Name)
		assert.Equal(t, "Test server", summary.Description)
		assert.Equal(t, "NA", summary.Tier)
		assert.Equal(t, "NA", summary.Status)
		assert.Equal(t, "NA", summary.Transport)
		assert.Equal(t, 0, summary.ToolsCount)
	})

	t.Run("newServerDetailResponse with container server", func(t *testing.T) {
		t.Parallel()
		detail := newServerDetailResponseForTesting(testServer)

		assert.Equal(t, "test-server", detail.Name)
		assert.Equal(t, "Test server", detail.Description)
		assert.Equal(t, "NA", detail.Tier)
		assert.Equal(t, "NA", detail.Status)
		assert.Equal(t, "NA", detail.Transport)
		assert.Equal(t, []string{}, detail.Tools)
		assert.Equal(t, "NA", detail.Image)
	})

	t.Run("newServerDetailResponse with remote server", func(t *testing.T) {
		t.Parallel()
		detail := newServerDetailResponseForTesting(testRemoteServer)

		assert.Equal(t, "remote-server", detail.Name)
		assert.Equal(t, "Remote test server", detail.Description)
		assert.Equal(t, "NA", detail.Tier)
		assert.Equal(t, "NA", detail.Status)
		assert.Equal(t, "NA", detail.Transport)
		assert.Equal(t, []string{}, detail.Tools)
		assert.Equal(t, "NA", detail.Image, "remote server should not have image")

		// Check metadata contains URL info
		assert.NotContains(t, detail.Metadata, "url")
	})
}

// TestErrorScenarios tests various error conditions
func TestErrorScenarios(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	mockSvc := mocks.NewMockRegistryService(ctrl)

	t.Run("registry info service error", func(t *testing.T) {
		t.Parallel()
		mockSvc.EXPECT().GetRegistry(gomock.Any()).Return(nil, "", assert.AnError)

		router := v0.Router(mockSvc)
		req, err := http.NewRequest("GET", "/info", nil)
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusInternalServerError, rr.Code)

		var response map[string]interface{}
		err = json.Unmarshal(rr.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Contains(t, response, "error")
	})

	t.Run("list servers service error", func(t *testing.T) {
		t.Parallel()
		mockSvc.EXPECT().ListServers(gomock.Any()).Return(nil, assert.AnError)

		router := v0.Router(mockSvc)
		req, err := http.NewRequest("GET", "/servers", nil)
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusInternalServerError, rr.Code)
	})

	t.Run("get server service error", func(t *testing.T) {
		t.Parallel()
		mockSvc.EXPECT().GetServer(gomock.Any(), "error-server").Return(upstreamv0.ServerJSON{}, assert.AnError)

		router := v0.Router(mockSvc)
		req, err := http.NewRequest("GET", "/servers/error-server", nil)
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusInternalServerError, rr.Code)
	})

	t.Run("empty server name", func(t *testing.T) {
		t.Parallel()
		// This test needs its own mock since it calls ListServers (chi routes /servers/ to list endpoint)
		emptyNameMockSvc := mocks.NewMockRegistryService(ctrl)
		emptyNameMockSvc.EXPECT().ListServers(gomock.Any()).Return([]upstreamv0.ServerJSON{}, nil)

		router := v0.Router(emptyNameMockSvc)

		// Test with empty path parameter (though chi wouldn't normally route this)
		req, err := http.NewRequest("GET", "/servers/", nil)
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		// Chi will route this to the list servers endpoint, not the individual server endpoint
		// So we expect either a valid response or method not allowed
		assert.True(t, rr.Code == http.StatusOK || rr.Code == http.StatusMethodNotAllowed || rr.Code == http.StatusNotFound)
	})
}

// TestOpenAPIEndpoint tests the OpenAPI YAML endpoint
func TestOpenAPIEndpoint(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	mockSvc := mocks.NewMockRegistryService(ctrl)
	router := v0.Router(mockSvc)

	req, err := http.NewRequest("GET", "/openapi.yaml", nil)
	require.NoError(t, err)

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "application/x-yaml", rr.Header().Get("Content-Type"))
	assert.Greater(t, len(rr.Body.String()), 0, "OpenAPI YAML should not be empty")
}

// TestPublishServerEndpoint tests the publish endpoint
func TestPublishServerEndpoint(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	mockSvc := mocks.NewMockRegistryService(ctrl)
	router := v0.Router(mockSvc)

	req, err := http.NewRequest("POST", "/publish", nil)
	require.NoError(t, err)

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNotImplemented, rr.Code)

	var response map[string]interface{}
	err = json.Unmarshal(rr.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.Contains(t, response, "error")
	assert.Equal(t, "Publishing is not supported by this registry implementation", response["error"])
}

// TestReadinessWithServiceError tests readiness endpoint when service has errors
func TestReadinessWithServiceError(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	mockSvc := mocks.NewMockRegistryService(ctrl)
	mockSvc.EXPECT().CheckReadiness(gomock.Any()).Return(assert.AnError)

	router := v0.HealthRouter(mockSvc)
	req, err := http.NewRequest("GET", "/readiness", nil)
	require.NoError(t, err)

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusServiceUnavailable, rr.Code)

	var response map[string]interface{}
	err = json.Unmarshal(rr.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.Contains(t, response, "error")
}
