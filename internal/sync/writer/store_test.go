package writer

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/db/sqlc"
)

// TestNewDBSyncWriter tests the constructor
func TestNewDBSyncWriter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		pool          *pgxpool.Pool
		expectError   bool
		errorContains string
	}{
		{
			name:        "valid pool",
			pool:        &pgxpool.Pool{}, // Non-nil pool
			expectError: false,
		},
		{
			name:          "nil pool",
			pool:          nil,
			expectError:   true,
			errorContains: "pgx pool is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			writer, err := NewDBSyncWriter(tt.pool, 0)

			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorContains)
				assert.Nil(t, writer)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, writer)
			}
		})
	}
}

// TestDbSyncWriter_Store_PerEntryClaims tests that WithPerEntryClaims overrides
// source-level claims for specific entries while other entries fall back to source claims.
func TestDbSyncWriter_Store_PerEntryClaims(t *testing.T) {
	t.Parallel()

	// Helper to marshal claims JSON for test inputs
	mustMarshal := func(t *testing.T, v any) []byte {
		t.Helper()
		b, err := json.Marshal(v)
		require.NoError(t, err)
		return b
	}

	type entryExpectation struct {
		name      string
		claimsNil bool
		claimsMap map[string]string
	}

	tests := []struct {
		name           string
		registryName   string
		servers        []upstreamv0.ServerJSON
		sourceClaims   []byte // nil means no source-level claims
		perEntryClaims map[string][]byte
		useOption      bool // whether to pass WithPerEntryClaims at all
		expectations   []entryExpectation
	}{
		{
			name:         "per-entry claims override source claims",
			registryName: "per-entry-override",
			servers: []upstreamv0.ServerJSON{
				createTestServer("test.org/server1", "1.0.0"),
				createTestServer("test.org/server2", "1.0.0"),
			},
			sourceClaims: mustMarshal(t, map[string]string{"org": "acme"}),
			perEntryClaims: map[string][]byte{
				"test.org/server1": mustMarshal(t, map[string]string{"org": "acme", "team": "platform"}),
			},
			useOption: true,
			expectations: []entryExpectation{
				{
					name:      "test.org/server1",
					claimsMap: map[string]string{"org": "acme", "team": "platform"},
				},
				{
					name:      "test.org/server2",
					claimsMap: map[string]string{"org": "acme"},
				},
			},
		},
		{
			name:         "per-entry claims with no source claims",
			registryName: "per-entry-no-source",
			servers: []upstreamv0.ServerJSON{
				createTestServer("test.org/server1", "1.0.0"),
				createTestServer("test.org/server2", "1.0.0"),
			},
			sourceClaims: nil,
			perEntryClaims: map[string][]byte{
				"test.org/server1": mustMarshal(t, map[string]string{"team": "data"}),
			},
			useOption: true,
			expectations: []entryExpectation{
				{
					name:      "test.org/server1",
					claimsMap: map[string]string{"team": "data"},
				},
				{
					name:      "test.org/server2",
					claimsNil: true,
				},
			},
		},
		{
			name:         "all entries have per-entry claims",
			registryName: "per-entry-all",
			servers: []upstreamv0.ServerJSON{
				createTestServer("test.org/server1", "1.0.0"),
				createTestServer("test.org/server2", "1.0.0"),
			},
			sourceClaims: mustMarshal(t, map[string]string{"org": "acme"}),
			perEntryClaims: map[string][]byte{
				"test.org/server1": mustMarshal(t, map[string]string{"team": "alpha"}),
				"test.org/server2": mustMarshal(t, map[string]string{"team": "beta"}),
			},
			useOption: true,
			expectations: []entryExpectation{
				{
					name:      "test.org/server1",
					claimsMap: map[string]string{"team": "alpha"},
				},
				{
					name:      "test.org/server2",
					claimsMap: map[string]string{"team": "beta"},
				},
			},
		},
		{
			name:         "empty per-entry claims map uses source claims",
			registryName: "per-entry-empty-map",
			servers: []upstreamv0.ServerJSON{
				createTestServer("test.org/server1", "1.0.0"),
				createTestServer("test.org/server2", "1.0.0"),
			},
			sourceClaims:   mustMarshal(t, map[string]string{"org": "acme"}),
			perEntryClaims: map[string][]byte{},
			useOption:      true,
			expectations: []entryExpectation{
				{
					name:      "test.org/server1",
					claimsMap: map[string]string{"org": "acme"},
				},
				{
					name:      "test.org/server2",
					claimsMap: map[string]string{"org": "acme"},
				},
			},
		},
		{
			name:         "no StoreOption uses source claims",
			registryName: "per-entry-no-option",
			servers: []upstreamv0.ServerJSON{
				createTestServer("test.org/server1", "1.0.0"),
				createTestServer("test.org/server2", "1.0.0"),
			},
			sourceClaims:   mustMarshal(t, map[string]string{"org": "acme"}),
			perEntryClaims: nil,
			useOption:      false,
			expectations: []entryExpectation{
				{
					name:      "test.org/server1",
					claimsMap: map[string]string{"org": "acme"},
				},
				{
					name:      "test.org/server2",
					claimsMap: map[string]string{"org": "acme"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pool, cleanup := setupTestDB(t)
			defer cleanup()

			ctx := context.Background()
			ids := createTestRegistry(t, pool, tt.registryName)

			// Set source-level claims if provided
			if tt.sourceClaims != nil {
				_, err := pool.Exec(ctx, "UPDATE source SET claims = $1 WHERE id = $2", tt.sourceClaims, ids.sourceID)
				require.NoError(t, err)
			}

			writer, err := NewDBSyncWriter(pool, testMaxMetaSize)
			require.NoError(t, err)

			reg := createTestUpstreamRegistry(tt.servers)

			// Call Store with or without the option
			var storeOpts []StoreOption
			if tt.useOption {
				storeOpts = append(storeOpts, WithPerEntryClaims(tt.perEntryClaims))
			}
			err = writer.Store(ctx, tt.registryName, reg, storeOpts...)
			require.NoError(t, err)

			// Read back entries and verify claims
			queries := sqlc.New(pool)
			entries, err := queries.ListEntriesBySource(ctx, ids.sourceID)
			require.NoError(t, err)

			// Build a map of entry name -> claims for easy lookup
			entryClaims := make(map[string][]byte, len(entries))
			for _, entry := range entries {
				entryClaims[entry.Name] = entry.Claims
			}

			for _, exp := range tt.expectations {
				claims, ok := entryClaims[exp.name]
				require.True(t, ok, "entry %q should exist", exp.name)

				if exp.claimsNil {
					assert.Nil(t, claims, "entry %q should have nil claims", exp.name)
				} else {
					require.NotNil(t, claims, "entry %q should have non-nil claims", exp.name)
					var got map[string]string
					err := json.Unmarshal(claims, &got)
					require.NoError(t, err, "entry %q claims should be valid JSON", exp.name)
					assert.Equal(t, exp.claimsMap, got, "entry %q claims mismatch", exp.name)
				}
			}
		})
	}
}
