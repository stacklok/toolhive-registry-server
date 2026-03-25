package database

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/aws/smithy-go/ptr"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/db/sqlc"
	"github.com/stacklok/toolhive-registry-server/internal/service"
)

// TestGetSkillVersionClaimsVisibility checks that GetSkillVersion applies
// claims-based promotion across all sources in priority order, mirroring the
// behaviour of the List functions. If the highest-priority source (source-A)
// fails the claims check, the next source in line (B, then C) is tried.
func TestGetSkillVersionClaimsVisibility(t *testing.T) {
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
		expectVisible bool   // false → expect errors.Is(err, service.ErrNotFound)
		expectDesc    string // which source won; only checked when expectVisible is true
	}{
		{
			name:          "no source has claims - ErrNotFound",
			srcAHasClaims: false,
			srcBHasClaims: false,
			srcCHasClaims: false,
			expectVisible: false,
		},
		{
			// A and B filtered; C promoted.
			name:          "only source-C matches - source-C promoted",
			srcAHasClaims: false,
			srcBHasClaims: false,
			srcCHasClaims: true,
			expectVisible: true,
			expectDesc:    descFromSrcC,
		},
		{
			// A filtered; B promoted; C shadowed by B.
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

		// source-A HAS claims → always visible from source-A.
		{
			name:          "only source-A matches - visible from source-A",
			srcAHasClaims: true,
			srcBHasClaims: false,
			srcCHasClaims: false,
			expectVisible: true,
			expectDesc:    descFromSrcA,
		},
		{
			name:          "source-A and source-C match - visible from source-A",
			srcAHasClaims: true,
			srcBHasClaims: false,
			srcCHasClaims: true,
			expectVisible: true,
			expectDesc:    descFromSrcA,
		},
		{
			name:          "source-A and source-B match - visible from source-A",
			srcAHasClaims: true,
			srcBHasClaims: true,
			srcCHasClaims: false,
			expectVisible: true,
			expectDesc:    descFromSrcA,
		},
		{
			name:          "all sources match - visible from source-A",
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

			srcA, srcB, srcC := setupShadowingRegistry(t, svc)

			// insertEntry creates a minimal skill entry under the given source.
			// desc is stored as the entry's description so tests can identify which
			// source's copy survived dedup. claims is nil for an unclaimed entry.
			insertEntry := func(src sqlc.Source, desc string, claims []byte) {
				//nolint:thelper // We want to see these lines in the test output
				entryID, err := queries.InsertRegistryEntry(ctx, sqlc.InsertRegistryEntryParams{
					SourceID:  src.ID,
					EntryType: sqlc.EntryTypeSKILL,
					Name:      entryName,
					Claims:    claims,
					CreatedAt: &now,
					UpdatedAt: &now,
				})
				require.NoError(t, err)

				versionID, err := queries.InsertEntryVersion(ctx, sqlc.InsertEntryVersionParams{
					EntryID:     entryID,
					Version:     "1.0.0",
					Title:       ptr.String(entryName),
					Description: ptr.String(desc),
					CreatedAt:   &now,
					UpdatedAt:   &now,
				})
				require.NoError(t, err)

				_, err = queries.InsertSkillVersion(ctx, sqlc.InsertSkillVersionParams{
					VersionID:     versionID,
					Namespace:     "com.example",
					Status:        sqlc.NullSkillStatus{},
					AllowedTools:  nil,
					Repository:    []byte("null"),
					Icons:         []byte("null"),
					Metadata:      []byte("null"),
					ExtensionMeta: []byte("null"),
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

			result, err := svc.GetSkillVersion(
				ctx,
				service.WithName(entryName),
				service.WithVersion("1.0.0"),
				service.WithNamespace("com.example"),
				service.WithRegistryName("claims-registry"),
				service.WithClaims(callerClaims),
			)

			if !tt.expectVisible {
				require.Error(t, err)
				require.True(t, errors.Is(err, service.ErrNotFound))
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.expectDesc, result.Description)
		})
	}
}
