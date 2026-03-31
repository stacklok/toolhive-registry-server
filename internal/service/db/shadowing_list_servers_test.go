package database

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/aws/smithy-go/ptr"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/db/sqlc"
	"github.com/stacklok/toolhive-registry-server/internal/service"
)

// TestListServersClaimsVisibility checks that three entries with the same name —
// one each on source-A (position 0), source-B (position 1), and source-C
// (position 2), all exposed via a single registry — shadow one another
// correctly when claims filtering is applied. The caller always carries claims,
// so the 8 test cases cover all 2^3 combinations of {srcAHasClaims,
// srcBHasClaims, srcCHasClaims}.
//
// The claims filter runs before dedup: a higher-priority source whose entry
// does not match the caller's claims is dropped, promoting the next source in
// line. The cases demonstrate the full cascade: A filtered → B promoted; A and
// B both filtered → C promoted; all filtered → entry invisible.
func TestListServersClaimsVisibility(t *testing.T) {
	t.Parallel()

	const (
		entryName    = "com.claims/the-entry"
		descFromSrcA = "from source-A"
		descFromSrcB = "from source-B"
		descFromSrcC = "from source-C"
	)

	callerClaims := map[string]any{"sub": "claims-test-user"}
	callerClaimsJSON, err := json.Marshal(callerClaims)
	require.NoError(t, err)

	tests := []struct {
		name          string
		srcAHasClaims bool
		srcBHasClaims bool
		srcCHasClaims bool
		// expectVisible is false when no entry survives the claims filter.
		expectVisible bool
		// expectDesc identifies which source won; only checked when expectVisible is true.
		expectDesc string
	}{
		{
			name:          "no source has claims - entry invisible",
			srcAHasClaims: false,
			srcBHasClaims: false,
			srcCHasClaims: false,
			expectVisible: false,
		},
		{
			// A and B filtered; C, normally shadowed by both, is promoted.
			name:          "only source-C matches - source-C promoted",
			srcAHasClaims: false,
			srcBHasClaims: false,
			srcCHasClaims: true,
			expectVisible: true,
			expectDesc:    descFromSrcC,
		},
		{
			// A filtered; B, normally shadowed by A, is promoted; C shadowed by B.
			name:          "only source-B matches - source-B promoted",
			srcAHasClaims: false,
			srcBHasClaims: true,
			srcCHasClaims: false,
			expectVisible: true,
			expectDesc:    descFromSrcB,
		},
		{
			// A filtered; B promoted; C shadowed by B even though C also matches.
			name:          "source-B and source-C match - source-B promoted, source-C shadowed",
			srcAHasClaims: false,
			srcBHasClaims: true,
			srcCHasClaims: true,
			expectVisible: true,
			expectDesc:    descFromSrcB,
		},
		{
			// A wins outright; B and C are shadowed.
			name:          "only source-A matches - source-A wins",
			srcAHasClaims: true,
			srcBHasClaims: false,
			srcCHasClaims: false,
			expectVisible: true,
			expectDesc:    descFromSrcA,
		},
		{
			// A wins; C filtered; B shadowed by A even though B does not match.
			name:          "source-A and source-C match - source-A wins, source-B and source-C shadowed",
			srcAHasClaims: true,
			srcBHasClaims: false,
			srcCHasClaims: true,
			expectVisible: true,
			expectDesc:    descFromSrcA,
		},
		{
			name:          "source-A and source-B match - source-A wins, source-B and source-C shadowed",
			srcAHasClaims: true,
			srcBHasClaims: true,
			srcCHasClaims: false,
			expectVisible: true,
			expectDesc:    descFromSrcA,
		},
		{
			name:          "all sources match - source-A wins via dedup, source-B and source-C shadowed",
			srcAHasClaims: true,
			srcBHasClaims: true,
			srcCHasClaims: true,
			expectVisible: true,
			expectDesc:    descFromSrcA,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc, cleanup := setupTestService(t)
			defer cleanup()

			ctx := context.Background()
			queries := sqlc.New(svc.pool)
			now := time.Now().UTC()

			// Create three sources.
			srcA, err := queries.InsertSource(ctx, sqlc.InsertSourceParams{
				Name:         "claims-src-a",
				CreationType: sqlc.CreationTypeCONFIG,
				SourceType:   "git",
				Syncable:     true,
				CreatedAt:    &now,
				UpdatedAt:    &now,
			})
			require.NoError(t, err)

			srcB, err := queries.InsertSource(ctx, sqlc.InsertSourceParams{
				Name:         "claims-src-b",
				CreationType: sqlc.CreationTypeCONFIG,
				SourceType:   "git",
				Syncable:     true,
				CreatedAt:    &now,
				UpdatedAt:    &now,
			})
			require.NoError(t, err)

			srcC, err := queries.InsertSource(ctx, sqlc.InsertSourceParams{
				Name:         "claims-src-c",
				CreationType: sqlc.CreationTypeCONFIG,
				SourceType:   "git",
				Syncable:     true,
				CreatedAt:    &now,
				UpdatedAt:    &now,
			})
			require.NoError(t, err)

			// Create a single registry linked to all three sources.
			reg, err := queries.UpsertRegistry(ctx, sqlc.UpsertRegistryParams{
				Name:         "claims-registry",
				CreationType: sqlc.CreationTypeCONFIG,
				CreatedAt:    &now,
				UpdatedAt:    &now,
			})
			require.NoError(t, err)

			for i, srcID := range []uuid.UUID{srcA.ID, srcB.ID, srcC.ID} {
				err = queries.LinkRegistrySource(ctx, sqlc.LinkRegistrySourceParams{
					RegistryID: reg.ID,
					SourceID:   srcID,
					Position:   int32(i),
				})
				require.NoError(t, err)
			}

			// insertEntry creates a minimal MCP server entry under the given source.
			// desc is stored as the entry's description so tests can identify which
			// source's copy survived dedup. claims is nil for an unclaimed entry.
			insertEntry := func(src sqlc.Source, desc string, claims []byte) {
				//nolint:thelper // We want to see these lines in the test output
				entryID, err := queries.InsertRegistryEntry(ctx, sqlc.InsertRegistryEntryParams{
					SourceID:  src.ID,
					EntryType: sqlc.EntryTypeMCP,
					Name:      entryName,
					Claims:    claims,
					CreatedAt: &now,
					UpdatedAt: &now,
				})
				require.NoError(t, err)

				versionID, err := queries.InsertEntryVersion(ctx, sqlc.InsertEntryVersionParams{
					EntryID:     entryID,
					Name:        entryName,
					Version:     "1.0.0",
					Title:       ptr.String(entryName),
					Description: ptr.String(desc),
					CreatedAt:   &now,
					UpdatedAt:   &now,
				})
				require.NoError(t, err)

				_, err = queries.InsertServerVersion(ctx, sqlc.InsertServerVersionParams{
					VersionID:    versionID,
					UpstreamMeta: []byte(`{}`),
					ServerMeta:   []byte(`{}`),
				})
				require.NoError(t, err)
			}

			// claimsFor returns callerClaimsJSON when hasClaims is true, nil otherwise.
			// An entry with nil claims is invisible to any credentialed caller.
			claimsFor := func(hasClaims bool) []byte {
				if hasClaims {
					return callerClaimsJSON
				}
				return nil
			}

			// All three entries share the same name; they shadow one another via dedup.
			insertEntry(srcA, descFromSrcA, claimsFor(tt.srcAHasClaims))
			insertEntry(srcB, descFromSrcB, claimsFor(tt.srcBHasClaims))
			insertEntry(srcC, descFromSrcC, claimsFor(tt.srcCHasClaims))

			result, err := svc.ListServers(
				ctx,
				service.WithRegistryName("claims-registry"),
				service.WithClaims(callerClaims),
				service.WithLimit(10),
			)
			require.NoError(t, err)

			if !tt.expectVisible {
				require.Empty(t, result.Servers)
				return
			}

			require.Len(t, result.Servers, 1)
			require.Equal(t, entryName, result.Servers[0].Name)
			require.Equal(t, tt.expectDesc, result.Servers[0].Description)
		})
	}
}
