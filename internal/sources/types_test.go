package sources

import (
	"os"
	"path/filepath"
	"testing"

	toolhivetypes "github.com/stacklok/toolhive/pkg/registry/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/registry"
)

func TestNewFetchResult(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                string
		registryData        *toolhivetypes.UpstreamRegistry
		hash                string
		format              string
		expectedServerCount int
	}{
		{
			name:                "empty registry",
			registryData:        registry.NewTestUpstreamRegistry(),
			hash:                "abcd1234",
			format:              config.SourceFormatToolHive,
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
			format:              config.SourceFormatToolHive,
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
			format:              config.SourceFormatUpstream,
			expectedServerCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := NewFetchResult(tt.registryData, tt.hash, tt.format)

			assert.Equal(t, tt.expectedServerCount, result.ServerCount)
			assert.Equal(t, tt.hash, result.Hash)
			assert.Equal(t, tt.format, result.Format)
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
	format := config.SourceFormatToolHive

	result1 := NewFetchResult(registryData, hash, format)
	result2 := NewFetchResult(registryData, hash, format)

	// Same data should produce same results
	assert.Equal(t, result1.Hash, result2.Hash)
	assert.Equal(t, result1.ServerCount, result2.ServerCount)
	assert.Equal(t, result1.Format, result2.Format)
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
	format := config.SourceFormatToolHive

	result1 := NewFetchResult(registryData1, hash1, format)
	result2 := NewFetchResult(registryData2, hash2, format)

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

	validToolhiveData := []byte(`{
		"version": "1.0.0",
		"last_updated": "2025-01-15T10:30:00Z",
		"servers": {
			"test-server": {
				"name": "test-server",
				"description": "A test server for validation",
				"image": "test/image:latest",
				"tier": "Community",
				"status": "Active",
				"transport": "stdio",
				"tools": ["test_tool"]
			}
		}
	}`)

	validUpstreamData := []byte(`{
		"$schema": "https://raw.githubusercontent.com/stacklok/toolhive/main/pkg/registry/data/upstream-registry.schema.json",
		"version": "1.0.0",
		"meta": {
			"last_updated": "2025-01-15T10:30:00Z"
		},
		"data": {
			"servers": [{
				"$schema": "https://static.modelcontextprotocol.io/schemas/2025-10-17/server.schema.json",
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
		format        string
		expectError   bool
		errorContains string
	}{
		{
			name:        "valid toolhive format",
			data:        validToolhiveData,
			format:      config.SourceFormatToolHive,
			expectError: false,
		},
		{
			name:        "valid upstream format",
			data:        validUpstreamData,
			format:      config.SourceFormatUpstream,
			expectError: false,
		},
		{
			name:          "empty data",
			data:          []byte{},
			format:        config.SourceFormatToolHive,
			expectError:   true,
			errorContains: "data cannot be empty",
		},
		{
			name:          "unsupported format",
			data:          validToolhiveData,
			format:        "unsupported",
			expectError:   true,
			errorContains: "unsupported format",
		},
		{
			name:          "invalid json for toolhive",
			data:          []byte("invalid json"),
			format:        config.SourceFormatToolHive,
			expectError:   true,
			errorContains: "invalid",
		},
		{
			name:          "invalid json for upstream",
			data:          []byte("invalid json"),
			format:        config.SourceFormatUpstream,
			expectError:   true,
			errorContains: "validation failed",
		},
		{
			name: "empty upstream servers array",
			data: []byte(`{
				"version": "1.0.0",
				"meta": {"last_updated": "2025-01-15T10:30:00Z"},
				"data": {"servers": []}
			}`),
			format:        config.SourceFormatUpstream,
			expectError:   true,
			errorContains: "must contain at least one server",
		},
		{
			name: "upstream server missing name",
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
			format:        config.SourceFormatUpstream,
			expectError:   true,
			errorContains: "name is required",
		},
		{
			name: "upstream server missing description",
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
			format:        config.SourceFormatUpstream,
			expectError:   true,
			errorContains: "description is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := validator.ValidateData(tt.data, tt.format)

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
		format             string
		expectedServers    int
		validateServerName func(*testing.T, *toolhivetypes.UpstreamRegistry)
	}{
		{
			name:            "toolhive-registry.json",
			filename:        "toolhive-registry.json",
			format:          config.SourceFormatToolHive,
			expectedServers: 12,
			validateServerName: func(t *testing.T, reg *toolhivetypes.UpstreamRegistry) {
				t.Helper()
				// Verify some expected servers are present
				serverNames := make(map[string]bool)
				for _, server := range reg.Data.Servers {
					serverNames[server.Name] = true
				}

				// Check a few key servers (converted to upstream format with io.github.stacklok prefix)
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
		{
			name:            "upstream-registry.json",
			filename:        "upstream-registry.json",
			format:          config.SourceFormatUpstream,
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
			reg, err := validator.ValidateData(data, tt.format)
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

// TestExampleFilesCorrespondence verifies that the toolhive and upstream example files
// contain the same logical servers (matching by title/name)
func TestExampleFilesCorrespondence(t *testing.T) {
	t.Parallel()

	validator := NewRegistryDataValidator()
	repoRoot := filepath.Join("..", "..")
	examplesDir := filepath.Join(repoRoot, "examples")

	// Load toolhive format
	toolhivePath := filepath.Join(examplesDir, "toolhive-registry.json")
	toolhiveData, err := os.ReadFile(toolhivePath)
	require.NoError(t, err)

	toolhiveReg, err := validator.ValidateData(toolhiveData, config.SourceFormatToolHive)
	require.NoError(t, err)

	// Load upstream format
	upstreamPath := filepath.Join(examplesDir, "upstream-registry.json")
	upstreamData, err := os.ReadFile(upstreamPath)
	require.NoError(t, err)

	upstreamReg, err := validator.ValidateData(upstreamData, config.SourceFormatUpstream)
	require.NoError(t, err)

	// Both should have the same number of servers
	require.Equal(t, len(toolhiveReg.Data.Servers), len(upstreamReg.Data.Servers),
		"Toolhive and upstream example files have different server counts")

	// Extract server names from both (for comparison)
	// Toolhive format gets converted to upstream format with io.github.stacklok/ prefix
	toolhiveNames := make(map[string]bool)
	for _, server := range toolhiveReg.Data.Servers {
		toolhiveNames[server.Name] = true
	}

	upstreamNames := make(map[string]bool)
	for _, server := range upstreamReg.Data.Servers {
		upstreamNames[server.Name] = true
	}

	// Verify the sets match
	assert.Equal(t, toolhiveNames, upstreamNames,
		"Toolhive and upstream example files should contain the same servers")
}
