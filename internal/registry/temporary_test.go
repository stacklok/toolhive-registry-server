package registry

import (
	"testing"

	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/stacklok/toolhive/pkg/registry/converters"
	toolhivetypes "github.com/stacklok/toolhive/pkg/registry/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpstreamRegistry_ToToolhive(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		serverReg    *toolhivetypes.UpstreamRegistry
		expectError  bool
		validateFunc func(*testing.T, *toolhivetypes.Registry)
	}{
		{
			name: "convert to toolhive with servers",
			serverReg: &toolhivetypes.UpstreamRegistry{
				Version:     "1.0.0",
				LastUpdated: "2024-01-01T00:00:00Z",
				Servers: []upstreamv0.ServerJSON{
					{
						Schema:      "https://static.modelcontextprotocol.io/schemas/2025-10-17/server.schema.json",
						Name:        "io.test/server1",
						Description: "Test server",
						Version:     "1.0.0",
						Packages: []model.Package{
							{
								RegistryType: "oci",
								Identifier:   "test/image:latest",
								Transport:    model.Transport{Type: "stdio"},
							},
						},
					},
				},
			},
			expectError: false,
			validateFunc: func(t *testing.T, reg *toolhivetypes.Registry) {
				t.Helper()
				assert.Equal(t, "1.0.0", reg.Version)
				assert.Equal(t, "2024-01-01T00:00:00Z", reg.LastUpdated)
				assert.Len(t, reg.Servers, 1)
			},
		},
		{
			name: "convert empty registry",
			serverReg: &toolhivetypes.UpstreamRegistry{
				Version:     "1.0.0",
				LastUpdated: "2024-01-01T00:00:00Z",
				Servers:     []upstreamv0.ServerJSON{},
			},
			expectError: false,
			validateFunc: func(t *testing.T, reg *toolhivetypes.Registry) {
				t.Helper()
				assert.Empty(t, reg.Servers)
				assert.Empty(t, reg.RemoteServers)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := ToToolhive(tt.serverReg)
			require.NoError(t, err)
			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				if tt.validateFunc != nil {
					tt.validateFunc(t, result)
				}
			}
		})
	}
}

func TestUpstreamRegistry_RoundTripConversion(t *testing.T) {
	t.Parallel()

	originalToolhive := &toolhivetypes.Registry{
		Version:     "1.0.0",
		LastUpdated: "2024-01-01T00:00:00Z",
		Servers: map[string]*toolhivetypes.ImageMetadata{
			"test-server": {
				BaseServerMetadata: toolhivetypes.BaseServerMetadata{
					Name:        "test-server",
					Description: "A test server",
					Tier:        "Community",
					Status:      "Active",
					Transport:   "stdio",
					Tools:       []string{"test_tool"},
				},
				Image: "test/image:latest",
			},
		},
		RemoteServers: make(map[string]*toolhivetypes.RemoteServerMetadata),
	}

	// Convert to UpstreamRegistry
	serverReg, err := converters.NewUpstreamRegistryFromToolhiveRegistry(originalToolhive)
	require.NoError(t, err)
	require.NotNil(t, serverReg)

	// Convert back to ToolHive
	convertedBack, err := ToToolhive(serverReg)
	require.NoError(t, err)
	require.NotNil(t, convertedBack)

	// Verify key fields match
	assert.Equal(t, originalToolhive.Version, convertedBack.Version)
	assert.Equal(t, originalToolhive.LastUpdated, convertedBack.LastUpdated)
	assert.Len(t, convertedBack.Servers, 1)
}
