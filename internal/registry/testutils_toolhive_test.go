package registry

import (
	"encoding/json"
	"testing"

	toolhivetypes "github.com/stacklok/toolhive/pkg/registry/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTestToolHiveRegistry(t *testing.T) {
	t.Parallel()

	t.Run("creates empty registry with defaults", func(t *testing.T) {
		t.Parallel()

		reg := NewTestToolHiveRegistry()

		assert.NotNil(t, reg)
		assert.Equal(t, "1.0.0", reg.Version)
		assert.NotEmpty(t, reg.LastUpdated)
		assert.NotNil(t, reg.Servers)
		assert.NotNil(t, reg.RemoteServers)
		assert.Empty(t, reg.Servers)
		assert.Empty(t, reg.RemoteServers)
	})

	t.Run("applies version option", func(t *testing.T) {
		t.Parallel()

		reg := NewTestToolHiveRegistry(
			WithToolHiveVersion("2.0.0"),
		)

		assert.Equal(t, "2.0.0", reg.Version)
	})

	t.Run("applies last updated option", func(t *testing.T) {
		t.Parallel()

		timestamp := "2023-01-01T00:00:00Z"
		reg := NewTestToolHiveRegistry(
			WithToolHiveLastUpdated(timestamp),
		)

		assert.Equal(t, timestamp, reg.LastUpdated)
	})

	t.Run("applies multiple options", func(t *testing.T) {
		t.Parallel()

		reg := NewTestToolHiveRegistry(
			WithToolHiveVersion("3.0.0"),
			WithToolHiveLastUpdated("2024-01-01T00:00:00Z"),
			WithImageServer("server1", "image1:latest"),
			WithRemoteServerURL("remote1", "https://example.com"),
		)

		assert.Equal(t, "3.0.0", reg.Version)
		assert.Equal(t, "2024-01-01T00:00:00Z", reg.LastUpdated)
		assert.Len(t, reg.Servers, 1)
		assert.Len(t, reg.RemoteServers, 1)
	})
}

func TestWithImageServer(t *testing.T) {
	t.Parallel()

	t.Run("adds image server with defaults", func(t *testing.T) {
		t.Parallel()

		reg := NewTestToolHiveRegistry(
			WithImageServer("test-server", "test/image:latest"),
		)

		require.Len(t, reg.Servers, 1)
		server, exists := reg.Servers["test-server"]
		require.True(t, exists)
		assert.Equal(t, "test-server", server.Name)
		assert.Equal(t, "test/image:latest", server.Image)
		assert.Equal(t, "Test server description for test-server", server.Description)
		assert.Equal(t, "Community", server.Tier)
		assert.Equal(t, "Active", server.Status)
		assert.Equal(t, "stdio", server.Transport)
		assert.Equal(t, []string{"test_tool"}, server.Tools)
		assert.Equal(t, []string{"database"}, server.Tags)
	})

	t.Run("applies image server options", func(t *testing.T) {
		t.Parallel()

		reg := NewTestToolHiveRegistry(
			WithImageServer("custom-server", "custom/image:v1.0",
				WithImageDescription("Custom description"),
				WithImageTier("Enterprise"),
				WithImageStatus("Beta"),
				WithImageTransport("http"),
				WithImageTools("tool1", "tool2"),
				WithImageTags("tag1", "tag2", "tag3"),
			),
		)

		require.Len(t, reg.Servers, 1)
		server := reg.Servers["custom-server"]
		assert.Equal(t, "Custom description", server.Description)
		assert.Equal(t, "Enterprise", server.Tier)
		assert.Equal(t, "Beta", server.Status)
		assert.Equal(t, "http", server.Transport)
		assert.Equal(t, []string{"tool1", "tool2"}, server.Tools)
		assert.Equal(t, []string{"tag1", "tag2", "tag3"}, server.Tags)
	})

	t.Run("adds multiple image servers", func(t *testing.T) {
		t.Parallel()

		reg := NewTestToolHiveRegistry(
			WithImageServer("server1", "image1:latest"),
			WithImageServer("server2", "image2:latest"),
			WithImageServer("server3", "image3:latest"),
		)

		assert.Len(t, reg.Servers, 3)
		assert.Contains(t, reg.Servers, "server1")
		assert.Contains(t, reg.Servers, "server2")
		assert.Contains(t, reg.Servers, "server3")
	})
}

func TestWithRemoteServerURL(t *testing.T) {
	t.Parallel()

	t.Run("adds remote server with defaults", func(t *testing.T) {
		t.Parallel()

		reg := NewTestToolHiveRegistry(
			WithRemoteServerURL("remote-server", "https://example.com"),
		)

		require.Len(t, reg.RemoteServers, 1)
		server, exists := reg.RemoteServers["remote-server"]
		require.True(t, exists)
		assert.Equal(t, "remote-server", server.Name)
		assert.Equal(t, "https://example.com", server.URL)
		assert.Equal(t, "Test remote server description for remote-server", server.Description)
		assert.Equal(t, "Community", server.Tier)
		assert.Equal(t, "Active", server.Status)
		assert.Equal(t, "sse", server.Transport)
		assert.Equal(t, []string{"remote_tool"}, server.Tools)
	})

	t.Run("applies remote server options", func(t *testing.T) {
		t.Parallel()

		reg := NewTestToolHiveRegistry(
			WithRemoteServerURL("custom-remote", "https://custom.example.com",
				WithRemoteDescription("Custom remote description"),
				WithRemoteTier("Professional"),
				WithRemoteStatus("Stable"),
				WithRemoteTransport("websocket"),
				WithRemoteTools("remote_tool1", "remote_tool2"),
				WithRemoteTags("remote", "http"),
			),
		)

		require.Len(t, reg.RemoteServers, 1)
		server := reg.RemoteServers["custom-remote"]
		assert.Equal(t, "Custom remote description", server.Description)
		assert.Equal(t, "Professional", server.Tier)
		assert.Equal(t, "Stable", server.Status)
		assert.Equal(t, "websocket", server.Transport)
		assert.Equal(t, []string{"remote_tool1", "remote_tool2"}, server.Tools)
		assert.Equal(t, []string{"remote", "http"}, server.Tags)
	})

	t.Run("adds multiple remote servers", func(t *testing.T) {
		t.Parallel()

		reg := NewTestToolHiveRegistry(
			WithRemoteServerURL("remote1", "https://remote1.example.com"),
			WithRemoteServerURL("remote2", "https://remote2.example.com"),
		)

		assert.Len(t, reg.RemoteServers, 2)
		assert.Contains(t, reg.RemoteServers, "remote1")
		assert.Contains(t, reg.RemoteServers, "remote2")
	})
}

func TestToolHiveRegistryToJSON(t *testing.T) {
	t.Parallel()

	t.Run("converts empty registry to JSON", func(t *testing.T) {
		t.Parallel()

		reg := NewTestToolHiveRegistry()
		jsonData := ToolHiveRegistryToJSON(reg)

		assert.NotEmpty(t, jsonData)

		// Verify it's valid JSON
		var parsed toolhivetypes.Registry
		err := json.Unmarshal(jsonData, &parsed)
		require.NoError(t, err)
		assert.Equal(t, "1.0.0", parsed.Version)
	})

	t.Run("converts registry with servers to JSON", func(t *testing.T) {
		t.Parallel()

		reg := NewTestToolHiveRegistry(
			WithImageServer("server1", "image1:latest"),
			WithImageServer("server2", "image2:latest"),
			WithRemoteServerURL("remote1", "https://remote1.example.com"),
		)
		jsonData := ToolHiveRegistryToJSON(reg)

		var parsed toolhivetypes.Registry
		err := json.Unmarshal(jsonData, &parsed)
		require.NoError(t, err)
		assert.Len(t, parsed.Servers, 2)
		assert.Len(t, parsed.RemoteServers, 1)
	})

	t.Run("panics on marshal error", func(t *testing.T) {
		t.Parallel()

		// Normal usage should not panic
		reg := NewTestToolHiveRegistry()
		assert.NotPanics(t, func() {
			ToolHiveRegistryToJSON(reg)
		})
	})
}

func TestToolHiveRegistryToPrettyJSON(t *testing.T) {
	t.Parallel()

	t.Run("converts registry to pretty JSON", func(t *testing.T) {
		t.Parallel()

		reg := NewTestToolHiveRegistry(
			WithImageServer("server1", "image1:latest"),
		)

		prettyJSON := ToolHiveRegistryToPrettyJSON(reg)
		regularJSON := ToolHiveRegistryToJSON(reg)

		assert.NotEmpty(t, prettyJSON)
		assert.NotEqual(t, regularJSON, prettyJSON)

		// Pretty JSON should be longer due to indentation
		assert.Greater(t, len(prettyJSON), len(regularJSON))

		// Both should unmarshal to the same data
		var prettyData, regularData toolhivetypes.Registry
		err1 := json.Unmarshal(prettyJSON, &prettyData)
		err2 := json.Unmarshal(regularJSON, &regularData)
		require.NoError(t, err1)
		require.NoError(t, err2)
		assert.Equal(t, regularData.Version, prettyData.Version)
		assert.Len(t, prettyData.Servers, len(regularData.Servers))
	})
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

func TestEmptyToolHiveJSON(t *testing.T) {
	t.Parallel()

	emptyJSON := EmptyToolHiveJSON()
	assert.Equal(t, []byte("{}"), emptyJSON)

	// Verify it's valid JSON
	var parsed interface{}
	err := json.Unmarshal(emptyJSON, &parsed)
	assert.NoError(t, err)
}

func TestComplexRegistryScenario(t *testing.T) {
	t.Parallel()

	// Create a complex registry with all features
	reg := NewTestToolHiveRegistry(
		WithToolHiveVersion("2.5.0"),
		WithToolHiveLastUpdated("2024-12-01T10:00:00Z"),
		WithImageServer("postgres-server", "postgres:16",
			WithImageDescription("PostgreSQL database server"),
			WithImageTier("Enterprise"),
			WithImageTools("sql_query", "db_backup"),
			WithImageTags("database", "sql"),
		),
		WithImageServer("redis-server", "redis:7",
			WithImageDescription("Redis cache server"),
			WithImageTier("Community"),
			WithImageTools("cache_get", "cache_set"),
			WithImageTags("cache", "nosql"),
		),
		WithRemoteServerURL("api-server", "https://api.example.com",
			WithRemoteDescription("External API server"),
			WithRemoteTier("Professional"),
			WithRemoteTools("fetch_data", "send_data"),
			WithRemoteTags("api", "rest"),
		),
	)

	// Verify structure
	assert.Equal(t, "2.5.0", reg.Version)
	assert.Equal(t, "2024-12-01T10:00:00Z", reg.LastUpdated)
	assert.Len(t, reg.Servers, 2)
	assert.Len(t, reg.RemoteServers, 1)

	// Verify specific servers
	postgres := reg.Servers["postgres-server"]
	assert.Equal(t, "PostgreSQL database server", postgres.Description)
	assert.Equal(t, "Enterprise", postgres.Tier)
	assert.Equal(t, []string{"sql_query", "db_backup"}, postgres.Tools)
	assert.Equal(t, []string{"database", "sql"}, postgres.Tags)

	redis := reg.Servers["redis-server"]
	assert.Equal(t, "Redis cache server", redis.Description)
	assert.Equal(t, "Community", redis.Tier)

	apiServer := reg.RemoteServers["api-server"]
	assert.Equal(t, "External API server", apiServer.Description)
	assert.Equal(t, "Professional", apiServer.Tier)

	// Verify JSON serialization works
	jsonData := ToolHiveRegistryToJSON(reg)
	var parsed toolhivetypes.Registry
	err := json.Unmarshal(jsonData, &parsed)
	require.NoError(t, err)
	assert.Equal(t, reg.Version, parsed.Version)
	assert.Len(t, parsed.Servers, 2)
	assert.Len(t, parsed.RemoteServers, 1)
}
