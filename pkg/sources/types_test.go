package sources

import (
	"testing"

	"github.com/stacklok/toolhive/pkg/registry"
	"github.com/stretchr/testify/assert"

	"github.com/stacklok/toolhive-registry-server/pkg/config"
)

func TestNewFetchResult(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		registryData *registry.Registry
		hash         string
		format       string
	}{
		{
			name: "empty registry",
			registryData: &registry.Registry{
				Version:       "1.0.0",
				Servers:       make(map[string]*registry.ImageMetadata),
				RemoteServers: make(map[string]*registry.RemoteServerMetadata),
			},
			hash:   "abcd1234",
			format: config.SourceFormatToolHive,
		},
		{
			name: "registry with servers",
			registryData: &registry.Registry{
				Version: "1.0.0",
				Servers: map[string]*registry.ImageMetadata{
					"server1": {},
					"server2": {},
				},
				RemoteServers: make(map[string]*registry.RemoteServerMetadata),
			},
			hash:   "efgh5678",
			format: config.SourceFormatToolHive,
		},
		{
			name: "registry with remote servers",
			registryData: &registry.Registry{
				Version: "1.0.0",
				Servers: make(map[string]*registry.ImageMetadata),
				RemoteServers: map[string]*registry.RemoteServerMetadata{
					"remote1": {},
				},
			},
			hash:   "ijkl9012",
			format: config.SourceFormatUpstream,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := NewFetchResult(tt.registryData, tt.hash, tt.format)

			expectedServerCount := len(tt.registryData.Servers) + len(tt.registryData.RemoteServers)
			assert.Equal(t, expectedServerCount, result.ServerCount)
			assert.Equal(t, tt.hash, result.Hash)
			assert.Equal(t, tt.format, result.Format)
			assert.Equal(t, tt.registryData, result.Registry)
		})
	}
}

func TestFetchResultHashConsistency(t *testing.T) {
	t.Parallel()

	registryData := &registry.Registry{
		Version: "1.0.0",
		Servers: map[string]*registry.ImageMetadata{
			"server1": {},
			"server2": {},
			"server3": {},
			"server4": {},
			"server5": {},
		},
		RemoteServers: make(map[string]*registry.RemoteServerMetadata),
	}
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

	registryData1 := &registry.Registry{
		Version: "1.0.0",
		Servers: map[string]*registry.ImageMetadata{
			"server1": {},
		},
		RemoteServers: make(map[string]*registry.RemoteServerMetadata),
	}

	registryData2 := &registry.Registry{
		Version: "1.0.0",
		Servers: map[string]*registry.ImageMetadata{
			"server2": {},
		},
		RemoteServers: make(map[string]*registry.RemoteServerMetadata),
	}

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

func TestNewSourceDataValidator(t *testing.T) {
	t.Parallel()

	validator := NewSourceDataValidator()
	assert.NotNil(t, validator)
	assert.IsType(t, &DefaultSourceDataValidator{}, validator)
}

func TestDefaultSourceDataValidator_ValidateData(t *testing.T) {
	t.Parallel()

	validator := NewSourceDataValidator()

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

	validUpstreamData := []byte(`[{
		"server": {
			"name": "test-server",
			"description": "A test server for validation",
			"status": "Active",
			"version_detail": {
				"version": "1.0.0"
			},
			"packages": [{
				"registry_name": "docker",
				"name": "test/image",
				"version": "latest"
			}]
		},
		"x-publisher": {
			"x-dev.toolhive": {
				"tier": "Community",
				"transport": "stdio",
				"tools": ["test_tool"]
			}
		}
	}]`)

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
			errorContains: "invalid upstream format",
		},
		{
			name:          "empty upstream array",
			data:          []byte("[]"),
			format:        config.SourceFormatUpstream,
			expectError:   true,
			errorContains: "must contain at least one server",
		},
		{
			name: "upstream server missing name",
			data: []byte(`[{
				"server": {
					"description": "Test server"
				}
			}]`),
			format:        config.SourceFormatUpstream,
			expectError:   true,
			errorContains: "name is required",
		},
		{
			name: "upstream server missing description",
			data: []byte(`[{
				"server": {
					"name": "test-server"
				}
			}]`),
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
