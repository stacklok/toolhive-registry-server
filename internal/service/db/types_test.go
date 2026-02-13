package database

import (
	"testing"

	"github.com/aws/smithy-go/ptr"
	"github.com/google/uuid"
	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/db/sqlc"
)

func TestHelperToServer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		dbServer    helper
		packages    []sqlc.ListServerPackagesRow
		remotes     []sqlc.McpServerRemote
		wantName    string
		wantVersion string
		wantDesc    string
		wantRepo    bool
		wantMetaVal map[string]any
		wantErr     bool
	}{
		{
			name: "minimal server with no optional fields",
			dbServer: helper{
				ID:      uuid.New(),
				Name:    "test-server",
				Version: "1.0.0",
			},
			wantName:    "test-server",
			wantVersion: "1.0.0",
		},
		{
			name: "server with all optional fields",
			dbServer: helper{
				ID:          uuid.New(),
				Name:        "full-server",
				Version:     "2.0.0",
				Description: ptr.String("A full server"),
				Title:       ptr.String("Full Server"),
				Website:     ptr.String("https://example.com"),
			},
			wantName:    "full-server",
			wantVersion: "2.0.0",
			wantDesc:    "A full server",
		},
		{
			name: "server with repository",
			dbServer: helper{
				ID:                  uuid.New(),
				Name:                "repo-server",
				Version:             "1.0.0",
				RepositoryUrl:       ptr.String("https://github.com/example/repo"),
				RepositoryType:      ptr.String("github"),
				RepositoryID:        ptr.String("example/repo"),
				RepositorySubfolder: ptr.String("src"),
			},
			wantName:    "repo-server",
			wantVersion: "1.0.0",
			wantRepo:    true,
		},
		{
			name: "server with valid meta JSON",
			dbServer: helper{
				ID:         uuid.New(),
				Name:       "meta-server",
				Version:    "1.0.0",
				ServerMeta: []byte(`{"category":"tools"}`),
			},
			wantName:    "meta-server",
			wantVersion: "1.0.0",
			wantMetaVal: map[string]any{"category": "tools"},
		},
		{
			name: "server with invalid meta JSON returns error",
			dbServer: helper{
				ID:         uuid.New(),
				Name:       "bad-meta",
				Version:    "1.0.0",
				ServerMeta: []byte(`{invalid json`),
			},
			wantErr: true,
		},
		{
			name: "server with packages and remotes",
			dbServer: helper{
				ID:      uuid.New(),
				Name:    "pkg-server",
				Version: "1.0.0",
			},
			packages: []sqlc.ListServerPackagesRow{
				{
					EntryID:        uuid.New(),
					RegistryType:   "npm",
					PkgRegistryUrl: "https://registry.npmjs.org",
					PkgIdentifier:  "@example/mcp",
					PkgVersion:     "1.0.0",
					Transport:      "stdio",
				},
			},
			remotes: []sqlc.McpServerRemote{
				{
					EntryID:      uuid.New(),
					Transport:    "sse",
					TransportUrl: "https://example.com/sse",
				},
			},
			wantName:    "pkg-server",
			wantVersion: "1.0.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := helperToServer(tt.dbServer, tt.packages, tt.remotes)

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			assert.Equal(t, tt.wantName, got.Name)
			assert.Equal(t, tt.wantVersion, got.Version)
			assert.Equal(t, "https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json", got.Schema)
			assert.NotNil(t, got.Meta)

			if tt.wantDesc != "" {
				assert.Equal(t, tt.wantDesc, got.Description)
			}

			if tt.wantRepo {
				require.NotNil(t, got.Repository)
				assert.Equal(t, ptr.ToString(tt.dbServer.RepositoryUrl), got.Repository.URL)
				assert.Equal(t, ptr.ToString(tt.dbServer.RepositoryType), got.Repository.Source)
			} else {
				assert.Nil(t, got.Repository)
			}

			if tt.wantMetaVal != nil {
				assert.Equal(t, tt.wantMetaVal, got.Meta.PublisherProvided)
			}

			if tt.packages != nil {
				assert.Len(t, got.Packages, len(tt.packages))
			}
			if tt.remotes != nil {
				assert.Len(t, got.Remotes, len(tt.remotes))
			}
		})
	}
}

func TestSerializePublisherProvidedMeta(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		meta        *upstreamv0.ServerMeta
		maxMetaSize int
		expectNil   bool
		expectError bool
	}{
		{
			name:      "nil meta",
			meta:      nil,
			expectNil: true,
		},
		{
			name: "nil PublisherProvided",
			meta: &upstreamv0.ServerMeta{
				PublisherProvided: nil,
			},
			expectNil: true,
		},
		{
			name: "empty PublisherProvided",
			meta: &upstreamv0.ServerMeta{
				PublisherProvided: map[string]interface{}{},
			},
			expectNil: true,
		},
		{
			name: "with data within limit",
			meta: &upstreamv0.ServerMeta{
				PublisherProvided: map[string]interface{}{
					"key": "value",
				},
			},
			maxMetaSize: 1000,
			expectNil:   false,
		},
		{
			name: "with data exceeding limit",
			meta: &upstreamv0.ServerMeta{
				PublisherProvided: map[string]interface{}{
					"key": "value",
				},
			},
			maxMetaSize: 5,
			expectError: true,
		},
		{
			name: "with nested data within limit",
			meta: &upstreamv0.ServerMeta{
				PublisherProvided: map[string]interface{}{
					"nested": map[string]interface{}{
						"key": "value",
					},
				},
			},
			maxMetaSize: 65536,
			expectNil:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := serializePublisherProvidedMeta(tt.meta, tt.maxMetaSize)

			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				if tt.expectNil {
					assert.Nil(t, result)
				} else {
					assert.NotNil(t, result)
					assert.Greater(t, len(result), 0)
				}
			}
		})
	}
}
