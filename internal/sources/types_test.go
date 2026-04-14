package sources

import (
	"os"
	"path/filepath"
	"testing"

	toolhivetypes "github.com/stacklok/toolhive-core/registry/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/registry"
)

func TestNewFetchResult(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                string
		registryData        *toolhivetypes.UpstreamRegistry
		hash                string
		expectedServerCount int
	}{
		{
			name:                "empty registry",
			registryData:        registry.NewTestUpstreamRegistry(),
			hash:                "abcd1234",
			expectedServerCount: 0,
		},
		{
			name: "registry with OCI servers",
			registryData: registry.NewTestUpstreamRegistry(
				registry.WithServers(
					registry.NewTestServer("server1",
						registry.WithOCIPackage("image1:latest"),
					),
					registry.NewTestServer("server2",
						registry.WithOCIPackage("image2:latest"),
					),
				),
			),
			hash:                "efgh5678",
			expectedServerCount: 2,
		},
		{
			name: "registry with HTTP server",
			registryData: registry.NewTestUpstreamRegistry(
				registry.WithServers(
					registry.NewTestServer("remote1",
						registry.WithHTTPPackage("https://example.com"),
					),
				),
			),
			hash:                "ijkl9012",
			expectedServerCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := NewFetchResult(tt.registryData, tt.hash)

			assert.Equal(t, tt.expectedServerCount, result.ServerCount)
			assert.Equal(t, tt.hash, result.Hash)
			assert.Equal(t, tt.registryData, result.Registry)
		})
	}
}

func TestFetchResultHashConsistency(t *testing.T) {
	t.Parallel()

	registryData := registry.NewTestUpstreamRegistry(
		registry.WithServers(
			registry.NewTestServer("server1", registry.WithOCIPackage("image1:latest")),
			registry.NewTestServer("server2", registry.WithOCIPackage("image2:latest")),
			registry.NewTestServer("server3", registry.WithOCIPackage("image3:latest")),
			registry.NewTestServer("server4", registry.WithOCIPackage("image4:latest")),
			registry.NewTestServer("server5", registry.WithOCIPackage("image5:latest")),
		),
	)
	hash := "consistent-hash-value"

	result1 := NewFetchResult(registryData, hash)
	result2 := NewFetchResult(registryData, hash)

	// Same data should produce same results
	assert.Equal(t, result1.Hash, result2.Hash)
	assert.Equal(t, result1.ServerCount, result2.ServerCount)
	assert.Equal(t, result1.Registry, result2.Registry)
}

func TestFetchResultHashDifference(t *testing.T) {
	t.Parallel()

	registryData1 := registry.NewTestUpstreamRegistry(
		registry.WithServers(
			registry.NewTestServer("server1",
				registry.WithOCIPackage("image1:latest"),
			),
		),
	)

	registryData2 := registry.NewTestUpstreamRegistry(
		registry.WithServers(
			registry.NewTestServer("server2",
				registry.WithOCIPackage("image2:latest"),
			),
		),
	)

	hash1 := "hash-for-data1"
	hash2 := "hash-for-data2"

	result1 := NewFetchResult(registryData1, hash1)
	result2 := NewFetchResult(registryData2, hash2)

	// Different data should produce different hashes
	assert.NotEqual(t, result1.Hash, result2.Hash)
	assert.Equal(t, result1.ServerCount, result2.ServerCount) // Both have 1 server
	assert.NotEqual(t, result1.Registry, result2.Registry)    // Different registries
}

func TestNewRegistryDataValidator(t *testing.T) {
	t.Parallel()

	validator := NewRegistryDataValidator()
	assert.NotNil(t, validator)
}

func TestDefaultRegistryDataValidator_ValidateData(t *testing.T) {
	t.Parallel()

	validator := NewRegistryDataValidator()

	validUpstreamData := []byte(`{
		"$schema": "https://raw.githubusercontent.com/stacklok/toolhive-core/main/registry/types/data/upstream-registry.schema.json",
		"version": "1.0.0",
		"meta": {
			"last_updated": "2025-01-15T10:30:00Z"
		},
		"data": {
			"servers": [{
				"$schema": "https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json",
				"name": "io.github.test/test-server",
				"description": "A test server for validation",
				"title": "test-server",
				"version": "1.0.0",
				"packages": [{
					"registryType": "oci",
					"identifier": "test/image:latest",
					"transport": {
						"type": "stdio"
					}
				}],
				"_meta": {
					"io.modelcontextprotocol.registry/publisher-provided": {
						"io.github.test": {
							"test/image:latest": {
								"tier": "Community",
								"status": "Active",
								"tools": ["test_tool"]
							}
						}
					}
				}
			}]
		}
	}`)

	tests := []struct {
		name          string
		data          []byte
		expectError   bool
		errorContains string
	}{
		{
			name:        "valid upstream format",
			data:        validUpstreamData,
			expectError: false,
		},
		{
			name:          "empty data",
			data:          []byte{},
			expectError:   true,
			errorContains: "data cannot be empty",
		},
		{
			name:          "invalid json",
			data:          []byte("invalid json"),
			expectError:   true,
			errorContains: "validation failed",
		},
		{
			name: "empty servers array",
			data: []byte(`{
				"version": "1.0.0",
				"meta": {"last_updated": "2025-01-15T10:30:00Z"},
				"data": {"servers": []}
			}`),
			expectError:   true,
			errorContains: "must contain at least one server",
		},
		{
			name: "server missing name",
			data: []byte(`{
				"version": "1.0.0",
				"meta": {"last_updated": "2025-01-15T10:30:00Z"},
				"data": {
					"servers": [{
						"description": "Test server",
						"version": "1.0.0",
						"packages": []
					}]
				}
			}`),
			expectError:   true,
			errorContains: "name is required",
		},
		{
			name: "server missing description",
			data: []byte(`{
				"version": "1.0.0",
				"meta": {"last_updated": "2025-01-15T10:30:00Z"},
				"data": {
					"servers": [{
						"name": "test-server",
						"version": "1.0.0",
						"packages": []
					}]
				}
			}`),
			expectError:   true,
			errorContains: "description is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := validator.ValidateData(tt.data)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestExampleFiles validates the actual example files in the examples directory
// This ensures our documentation examples stay valid and both formats work end-to-end
func TestExampleFiles(t *testing.T) {
	t.Parallel()

	validator := NewRegistryDataValidator()

	// Get the repository root (assuming tests run from internal/sources)
	repoRoot := filepath.Join("..", "..")
	examplesDir := filepath.Join(repoRoot, "examples")

	tests := []struct {
		name               string
		filename           string
		expectedServers    int
		validateServerName func(*testing.T, *toolhivetypes.UpstreamRegistry)
	}{
		{
			name:            "upstream-registry.json",
			filename:        "upstream-registry.json",
			expectedServers: 12,
			validateServerName: func(t *testing.T, reg *toolhivetypes.UpstreamRegistry) {
				t.Helper()
				// Verify some expected servers are present
				serverNames := make(map[string]bool)
				for _, server := range reg.Data.Servers {
					serverNames[server.Name] = true
				}

				// Check a few key servers (already in upstream format)
				expectedNames := []string{
					"io.github.stacklok/adb-mysql-mcp-server",
					"io.github.stacklok/github",
					"io.github.stacklok/filesystem",
				}
				for _, name := range expectedNames {
					assert.True(t, serverNames[name], "Expected server %s not found", name)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Read the example file
			examplePath := filepath.Join(examplesDir, tt.filename)
			data, err := os.ReadFile(examplePath)
			require.NoError(t, err, "Failed to read example file %s", tt.filename)
			require.NotEmpty(t, data, "Example file %s is empty", tt.filename)

			// Validate the data
			reg, err := validator.ValidateData(data)
			require.NoError(t, err, "Example file %s failed validation", tt.filename)
			require.NotNil(t, reg, "Validator returned nil registry")

			// Verify expected server count
			assert.Len(t, reg.Data.Servers, tt.expectedServers,
				"Example file %s has unexpected server count", tt.filename)

			// Verify all servers have required fields
			for i, server := range reg.Data.Servers {
				assert.NotEmpty(t, server.Name,
					"Server at index %d in %s has empty name", i, tt.filename)
				assert.NotEmpty(t, server.Description,
					"Server at index %d in %s has empty description", i, tt.filename)
				assert.NotEmpty(t, server.Packages,
					"Server at index %d (%s) in %s has no packages", i, server.Name, tt.filename)
			}

			// Run custom validation if provided
			if tt.validateServerName != nil {
				tt.validateServerName(t, reg)
			}

			// Verify metadata is present
			assert.NotEmpty(t, reg.Version, "Example file %s has no version", tt.filename)
			assert.NotEmpty(t, reg.Meta.LastUpdated, "Example file %s has no last_updated", tt.filename)
		})
	}
}
