package database

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/db/sqlc"
)

// setupShadowingRegistry creates three CONFIG/git sources named "claims-src-a",
// "claims-src-b", and "claims-src-c", a CONFIG registry named
// "claims-registry", and links the three sources at positions 0, 1, 2. It
// returns the three sources in priority order (A, B, C).
func setupShadowingRegistry(t *testing.T, svc *dbService) (sqlc.Source, sqlc.Source, sqlc.Source) {
	t.Helper()

	ctx := context.Background()
	queries := sqlc.New(svc.pool)
	now := time.Now().UTC()

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

	return srcA, srcB, srcC
}
