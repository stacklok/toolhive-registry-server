package sources

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/stacklok/toolhive-registry-server/pkg/config"
	"github.com/stacklok/toolhive/pkg/registry"
)

func TestNewTestRegistryBuilder(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		format         string
		expectedFormat string
	}{
		{
			name:           "toolhive format",
			format:         config.SourceFormatToolHive,
			expectedFormat: config.SourceFormatToolHive,
		},
		{
			name:           "upstream format",
			format:         config.SourceFormatUpstream,
			expectedFormat: config.SourceFormatUpstream,
		},
		{
			name:           "empty format defaults to toolhive",
			format:         "",
			expectedFormat: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			builder := NewTestRegistryBuilder(tt.format)

			assert.NotNil(t, builder)
			assert.Equal(t, tt.expectedFormat, builder.format)
			assert.Equal(t, 1, builder.serverCounter)

			switch tt.format {
			case config.SourceFormatToolHive, "":
				assert.NotNil(t, builder.registry)
				assert.Equal(t, "1.0.0", builder.registry.Version)
				assert.NotEmpty(t, builder.registry.LastUpdated)
				assert.NotNil(t, builder.registry.Servers)
				assert.NotNil(t, builder.registry.RemoteServers)
				assert.Nil(t, builder.upstreamData)
			case config.SourceFormatUpstream:
				assert.NotNil(t, builder.upstreamData)
				assert.Empty(t, builder.upstreamData)
				assert.Nil(t, builder.registry)
			}
		})
	}
}

func TestTestRegistryBuilder_WithServer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		format       string
		serverName   string
		expectedName string
	}{
		{
			name:         "toolhive format with explicit name",
			format:       config.SourceFormatToolHive,
			serverName:   "my-server",
			expectedName: "my-server",
		},
		{
			name:         "toolhive format with empty name",
			format:       config.SourceFormatToolHive,
			serverName:   "",
			expectedName: "test-server-1",
		},
		{
			name:         "upstream format with explicit name",
			format:       config.SourceFormatUpstream,
			serverName:   "upstream-server",
			expectedName: "upstream-server",
		},
		{
			name:         "upstream format with empty name",
			format:       config.SourceFormatUpstream,
			serverName:   "",
			expectedName: "test-server-1",
		},
		{
			name:         "empty format with server",
			format:       "",
			serverName:   "test-server",
			expectedName: "test-server",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			builder := NewTestRegistryBuilder(tt.format)
			result := builder.WithServer(tt.serverName)

			// Should return the same builder for chaining
			assert.Equal(t, builder, result)

			switch tt.format {
			case config.SourceFormatToolHive, "":
				assert.Len(t, builder.registry.Servers, 1)
				server, exists := builder.registry.Servers[tt.expectedName]
				assert.True(t, exists)
				assert.Equal(t, tt.expectedName, server.Name)
				assert.NotEmpty(t, server.Description)
				assert.Equal(t, "Community", server.Tier)
				assert.Equal(t, "Active", server.Status)
				assert.Equal(t, "stdio", server.Transport)
				assert.Equal(t, []string{"test_tool"}, server.Tools)
				assert.Equal(t, "test/image:latest", server.Image)
			case config.SourceFormatUpstream:
				assert.Len(t, builder.upstreamData, 1)
				serverDetail := builder.upstreamData[0]
				assert.Equal(t, tt.expectedName, serverDetail.Server.Name)
				assert.NotEmpty(t, serverDetail.Server.Description)
				assert.Len(t, serverDetail.Server.Packages, 1)
				pkg := serverDetail.Server.Packages[0]
				assert.Equal(t, "docker", pkg.RegistryName)
				assert.Equal(t, "test/image", pkg.Name)
				assert.Equal(t, "latest", pkg.Version)
			}
		})
	}
}

func TestTestRegistryBuilder_WithRemoteServer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		format      string
		url         string
		expectedURL string
		shouldAdd   bool
	}{
		{
			name:        "toolhive format with explicit URL",
			format:      config.SourceFormatToolHive,
			url:         "https://example.com",
			expectedURL: "https://example.com",
			shouldAdd:   true,
		},
		{
			name:        "toolhive format with empty URL",
			format:      config.SourceFormatToolHive,
			url:         "",
			expectedURL: "https://remote-server-1.example.com",
			shouldAdd:   true,
		},
		{
			name:        "empty format with URL",
			format:      "",
			url:         "https://test.com",
			expectedURL: "https://test.com",
			shouldAdd:   true,
		},
		{
			name:      "upstream format should not add remote server",
			format:    config.SourceFormatUpstream,
			url:       "https://example.com",
			shouldAdd: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			builder := NewTestRegistryBuilder(tt.format)
			result := builder.WithRemoteServer(tt.url)

			// Should return the same builder for chaining
			assert.Equal(t, builder, result)

			if tt.shouldAdd {
				assert.Len(t, builder.registry.RemoteServers, 1)
				var remoteServer *registry.RemoteServerMetadata
				for _, server := range builder.registry.RemoteServers {
					remoteServer = server
					break
				}
				assert.NotNil(t, remoteServer)
				assert.Equal(t, tt.expectedURL, remoteServer.URL)
				assert.NotEmpty(t, remoteServer.Name)
				assert.NotEmpty(t, remoteServer.Description)
				assert.Equal(t, "Community", remoteServer.Tier)
				assert.Equal(t, "Active", remoteServer.Status)
				assert.Equal(t, "sse", remoteServer.Transport)
				assert.Equal(t, []string{"remote_tool"}, remoteServer.Tools)
			} else {
				// For upstream format or formats that don't support remote servers
				if builder.registry != nil {
					assert.Empty(t, builder.registry.RemoteServers)
				}
			}
		})
	}
}

func TestTestRegistryBuilder_WithRemoteServerName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		format      string
		serverName  string
		url         string
		expectedURL string
		shouldAdd   bool
	}{
		{
			name:        "toolhive format with name and explicit URL",
			format:      config.SourceFormatToolHive,
			serverName:  "my-remote",
			url:         "https://example.com",
			expectedURL: "https://example.com",
			shouldAdd:   true,
		},
		{
			name:        "toolhive format with name and empty URL",
			format:      config.SourceFormatToolHive,
			serverName:  "my-remote",
			url:         "",
			expectedURL: "https://my-remote.example.com",
			shouldAdd:   true,
		},
		{
			name:       "upstream format should not add remote server",
			format:     config.SourceFormatUpstream,
			serverName: "my-remote",
			url:        "https://example.com",
			shouldAdd:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			builder := NewTestRegistryBuilder(tt.format)
			result := builder.WithRemoteServerName(tt.serverName, tt.url)

			// Should return the same builder for chaining
			assert.Equal(t, builder, result)

			if tt.shouldAdd {
				assert.Len(t, builder.registry.RemoteServers, 1)
				remoteServer, exists := builder.registry.RemoteServers[tt.serverName]
				assert.True(t, exists)
				assert.Equal(t, tt.expectedURL, remoteServer.URL)
				assert.Equal(t, tt.serverName, remoteServer.Name)
			} else {
				if builder.registry != nil {
					assert.Empty(t, builder.registry.RemoteServers)
				}
			}
		})
	}
}

func TestTestRegistryBuilder_WithVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		format       string
		version      string
		shouldUpdate bool
	}{
		{
			name:         "toolhive format should update version",
			format:       config.SourceFormatToolHive,
			version:      "2.0.0",
			shouldUpdate: true,
		},
		{
			name:         "empty format should update version",
			format:       "",
			version:      "3.0.0",
			shouldUpdate: true,
		},
		{
			name:         "upstream format should not update version",
			format:       config.SourceFormatUpstream,
			version:      "2.0.0",
			shouldUpdate: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			builder := NewTestRegistryBuilder(tt.format)
			result := builder.WithVersion(tt.version)

			// Should return the same builder for chaining
			assert.Equal(t, builder, result)

			if tt.shouldUpdate {
				assert.Equal(t, tt.version, builder.registry.Version)
			} else {
				// Upstream format doesn't have registry structure
				if builder.registry != nil {
					assert.NotEqual(t, tt.version, builder.registry.Version)
				}
			}
		})
	}
}

func TestTestRegistryBuilder_WithLastUpdated(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		format       string
		timestamp    string
		shouldUpdate bool
	}{
		{
			name:         "toolhive format should update timestamp",
			format:       config.SourceFormatToolHive,
			timestamp:    "2023-01-01T00:00:00Z",
			shouldUpdate: true,
		},
		{
			name:         "empty format should update timestamp",
			format:       "",
			timestamp:    "2023-01-01T00:00:00Z",
			shouldUpdate: true,
		},
		{
			name:         "upstream format should not update timestamp",
			format:       config.SourceFormatUpstream,
			timestamp:    "2023-01-01T00:00:00Z",
			shouldUpdate: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			builder := NewTestRegistryBuilder(tt.format)
			originalTimestamp := ""
			if builder.registry != nil {
				originalTimestamp = builder.registry.LastUpdated
			}

			result := builder.WithLastUpdated(tt.timestamp)

			// Should return the same builder for chaining
			assert.Equal(t, builder, result)

			if tt.shouldUpdate {
				assert.Equal(t, tt.timestamp, builder.registry.LastUpdated)
			} else {
				// Upstream format doesn't have registry structure
				if builder.registry != nil {
					assert.Equal(t, originalTimestamp, builder.registry.LastUpdated)
				}
			}
		})
	}
}

func TestTestRegistryBuilder_Empty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		format string
	}{
		{
			name:   "toolhive format",
			format: config.SourceFormatToolHive,
		},
		{
			name:   "upstream format",
			format: config.SourceFormatUpstream,
		},
		{
			name:   "empty format",
			format: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			builder := NewTestRegistryBuilder(tt.format)

			// Add some servers first
			builder.WithServer("test1").WithServer("test2")
			if tt.format != config.SourceFormatUpstream {
				builder.WithRemoteServer("https://example.com")
			}

			result := builder.Empty()

			// Should return the same builder for chaining
			assert.Equal(t, builder, result)

			switch tt.format {
			case config.SourceFormatToolHive, "":
				assert.Empty(t, builder.registry.Servers)
				assert.Empty(t, builder.registry.RemoteServers)
				// Other fields should remain
				assert.Equal(t, "1.0.0", builder.registry.Version)
				assert.NotEmpty(t, builder.registry.LastUpdated)
			case config.SourceFormatUpstream:
				assert.Empty(t, builder.upstreamData)
			}
		})
	}
}

func TestTestRegistryBuilder_BuildJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		format string
	}{
		{
			name:   "toolhive format",
			format: config.SourceFormatToolHive,
		},
		{
			name:   "upstream format",
			format: config.SourceFormatUpstream,
		},
		{
			name:   "empty format",
			format: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			builder := NewTestRegistryBuilder(tt.format)
			builder.WithServer("test-server")

			jsonData := builder.BuildJSON()

			assert.NotEmpty(t, jsonData)

			// Verify it's valid JSON
			var parsed interface{}
			err := json.Unmarshal(jsonData, &parsed)
			assert.NoError(t, err)

			switch tt.format {
			case config.SourceFormatToolHive, "":
				// Should be a registry object
				var registry registry.Registry
				err = json.Unmarshal(jsonData, &registry)
				assert.NoError(t, err)
				assert.Equal(t, "1.0.0", registry.Version)
				assert.Len(t, registry.Servers, 1)
			case config.SourceFormatUpstream:
				// Should be an array of server details
				var upstreamData []registry.UpstreamServerDetail
				err = json.Unmarshal(jsonData, &upstreamData)
				assert.NoError(t, err)
				assert.Len(t, upstreamData, 1)
			}
		})
	}
}

func TestTestRegistryBuilder_BuildPrettyJSON(t *testing.T) {
	t.Parallel()

	builder := NewTestRegistryBuilder(config.SourceFormatToolHive)
	builder.WithServer("test-server")

	prettyJSON := builder.BuildPrettyJSON()
	regularJSON := builder.BuildJSON()

	assert.NotEmpty(t, prettyJSON)
	assert.NotEqual(t, regularJSON, prettyJSON)

	// Pretty JSON should be longer due to indentation
	assert.Greater(t, len(prettyJSON), len(regularJSON))

	// Both should unmarshal to the same data
	var prettyData, regularData registry.Registry
	err1 := json.Unmarshal(prettyJSON, &prettyData)
	err2 := json.Unmarshal(regularJSON, &regularData)
	assert.NoError(t, err1)
	assert.NoError(t, err2)
	assert.Equal(t, regularData, prettyData)
}

func TestTestRegistryBuilder_GetRegistry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		format       string
		shouldReturn bool
	}{
		{
			name:         "toolhive format should return registry",
			format:       config.SourceFormatToolHive,
			shouldReturn: true,
		},
		{
			name:         "empty format should return registry",
			format:       "",
			shouldReturn: true,
		},
		{
			name:         "upstream format should return nil",
			format:       config.SourceFormatUpstream,
			shouldReturn: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			builder := NewTestRegistryBuilder(tt.format)
			builder.WithServer("test-server")

			registry := builder.GetRegistry()

			if tt.shouldReturn {
				assert.NotNil(t, registry)
				assert.Equal(t, "1.0.0", registry.Version)
				assert.Len(t, registry.Servers, 1)
			} else {
				assert.Nil(t, registry)
			}
		})
	}
}

func TestTestRegistryBuilder_GetUpstreamData(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		format       string
		shouldReturn bool
	}{
		{
			name:         "upstream format should return data",
			format:       config.SourceFormatUpstream,
			shouldReturn: true,
		},
		{
			name:         "toolhive format should return nil",
			format:       config.SourceFormatToolHive,
			shouldReturn: false,
		},
		{
			name:         "empty format should return nil",
			format:       "",
			shouldReturn: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			builder := NewTestRegistryBuilder(tt.format)
			builder.WithServer("test-server")

			upstreamData := builder.GetUpstreamData()

			if tt.shouldReturn {
				assert.NotNil(t, upstreamData)
				assert.Len(t, upstreamData, 1)
			} else {
				assert.Nil(t, upstreamData)
			}
		})
	}
}

func TestTestRegistryBuilder_ServerCount(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		format             string
		serversToAdd       int
		remoteServersToAdd int
		expectedCount      int
	}{
		{
			name:               "toolhive format with container servers",
			format:             config.SourceFormatToolHive,
			serversToAdd:       2,
			remoteServersToAdd: 0,
			expectedCount:      2,
		},
		{
			name:               "toolhive format with mixed servers",
			format:             config.SourceFormatToolHive,
			serversToAdd:       2,
			remoteServersToAdd: 1,
			expectedCount:      3,
		},
		{
			name:               "upstream format with servers",
			format:             config.SourceFormatUpstream,
			serversToAdd:       3,
			remoteServersToAdd: 0, // Remote servers not supported in upstream
			expectedCount:      3,
		},
		{
			name:               "empty format with servers",
			format:             "",
			serversToAdd:       1,
			remoteServersToAdd: 1,
			expectedCount:      2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			builder := NewTestRegistryBuilder(tt.format)

			// Add container servers
			for i := 0; i < tt.serversToAdd; i++ {
				builder.WithServer("")
			}

			// Add remote servers (only for supported formats)
			if tt.format != config.SourceFormatUpstream {
				for i := 0; i < tt.remoteServersToAdd; i++ {
					builder.WithRemoteServer("")
				}
			}

			count := builder.ServerCount()
			assert.Equal(t, tt.expectedCount, count)
		})
	}
}

func TestTestRegistryBuilder_ContainerServerCount(t *testing.T) {
	t.Parallel()

	builder := NewTestRegistryBuilder(config.SourceFormatToolHive)
	builder.WithServer("server1").WithServer("server2").WithRemoteServer("https://example.com")

	containerCount := builder.ContainerServerCount()
	assert.Equal(t, 2, containerCount)

	remoteCount := builder.RemoteServerCount()
	assert.Equal(t, 1, remoteCount)

	totalCount := builder.ServerCount()
	assert.Equal(t, 3, totalCount)
}

func TestTestRegistryBuilder_RemoteServerCount(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		format             string
		remoteServersToAdd int
		expectedCount      int
	}{
		{
			name:               "toolhive format with remote servers",
			format:             config.SourceFormatToolHive,
			remoteServersToAdd: 2,
			expectedCount:      2,
		},
		{
			name:               "empty format with remote servers",
			format:             "",
			remoteServersToAdd: 1,
			expectedCount:      1,
		},
		{
			name:               "upstream format should return 0",
			format:             config.SourceFormatUpstream,
			remoteServersToAdd: 0, // Remote servers not supported
			expectedCount:      0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			builder := NewTestRegistryBuilder(tt.format)

			// Add remote servers (only for supported formats)
			if tt.format != config.SourceFormatUpstream {
				for i := 0; i < tt.remoteServersToAdd; i++ {
					builder.WithRemoteServer("")
				}
			}

			count := builder.RemoteServerCount()
			assert.Equal(t, tt.expectedCount, count)
		})
	}
}

func TestTestRegistryBuilder_ChainedCalls(t *testing.T) {
	t.Parallel()

	// Test method chaining works correctly
	builder := NewTestRegistryBuilder(config.SourceFormatToolHive)

	result := builder.
		WithVersion("2.0.0").
		WithLastUpdated("2023-01-01T00:00:00Z").
		WithServer("server1").
		WithServer("server2").
		WithRemoteServer("https://remote1.example.com").
		WithRemoteServerName("remote2", "https://remote2.example.com")

	// Should return the same builder
	assert.Equal(t, builder, result)

	// Verify all operations were applied
	assert.Equal(t, "2.0.0", builder.registry.Version)
	assert.Equal(t, "2023-01-01T00:00:00Z", builder.registry.LastUpdated)
	assert.Len(t, builder.registry.Servers, 2)
	assert.Len(t, builder.registry.RemoteServers, 2)

	// Verify server count
	assert.Equal(t, 4, builder.ServerCount())
	assert.Equal(t, 2, builder.ContainerServerCount())
	assert.Equal(t, 2, builder.RemoteServerCount())
}

func TestInvalidJSON(t *testing.T) {
	t.Parallel()

	invalidJSON := InvalidJSON()
	assert.NotEmpty(t, invalidJSON)
	assert.Equal(t, []byte("invalid json"), invalidJSON)

	// Verify it's actually invalid JSON
	var parsed interface{}
	err := json.Unmarshal(invalidJSON, &parsed)
	assert.Error(t, err)
}

func TestEmptyJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		format       string
		expectedJSON string
	}{
		{
			name:         "toolhive format",
			format:       config.SourceFormatToolHive,
			expectedJSON: "{}",
		},
		{
			name:         "upstream format",
			format:       config.SourceFormatUpstream,
			expectedJSON: "[]",
		},
		{
			name:         "empty format",
			format:       "",
			expectedJSON: "{}",
		},
		{
			name:         "unknown format",
			format:       "unknown",
			expectedJSON: "{}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			emptyJSON := EmptyJSON(tt.format)
			assert.Equal(t, []byte(tt.expectedJSON), emptyJSON)

			// Verify it's valid JSON
			var parsed interface{}
			err := json.Unmarshal(emptyJSON, &parsed)
			assert.NoError(t, err)
		})
	}
}

func TestTestRegistryBuilder_WithServerName(t *testing.T) {
	t.Parallel()

	// Test WithServerName is an alias for WithServer
	builder := NewTestRegistryBuilder(config.SourceFormatToolHive)

	result1 := builder.WithServerName("test-server")
	result2 := builder.WithServer("test-server-2")

	// Both should return the same builder
	assert.Equal(t, builder, result1)
	assert.Equal(t, builder, result2)

	// Should have 2 servers
	assert.Len(t, builder.registry.Servers, 2)
	assert.Contains(t, builder.registry.Servers, "test-server")
	assert.Contains(t, builder.registry.Servers, "test-server-2")
}

func TestTestRegistryBuilder_PanicOnMarshalError(t *testing.T) {
	t.Parallel()

	// This is harder to test since we need to cause a marshal error
	// For now, just verify the methods don't panic with normal usage
	builder := NewTestRegistryBuilder(config.SourceFormatToolHive)
	builder.WithServer("test")

	assert.NotPanics(t, func() {
		builder.BuildJSON()
	})

	assert.NotPanics(t, func() {
		builder.BuildPrettyJSON()
	})
}
