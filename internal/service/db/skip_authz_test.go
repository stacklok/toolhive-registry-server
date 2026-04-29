package database

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/aws/smithy-go/ptr"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/database"
	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/db/sqlc"
	"github.com/stacklok/toolhive-registry-server/internal/service"
)

func setupTestServiceWithSkipAuthz(t *testing.T) (*dbService, func()) {
	t.Helper()

	ctx := context.Background()
	db, cleanupFunc := database.SetupTestDB(t)
	t.Cleanup(cleanupFunc)

	connStr := db.Config().ConnString()

	pool, err := pgxpool.New(ctx, connStr)
	require.NoError(t, err)

	serviceCleanup := func() {
		pool.Close()
		cleanupFunc()
	}

	svc := &dbService{
		pool:        pool,
		maxMetaSize: config.DefaultMaxMetaSize,
		skipAuthz:   true,
	}

	return svc, serviceCleanup
}

func TestListServers_SkipAuthz(t *testing.T) {
	t.Parallel()

	callerClaims := map[string]any{"org": "caller-org"}

	tests := []struct {
		name          string
		skipAuthz     bool
		entryClaims   []byte
		expectVisible bool
	}{
		{
			name:          "skipAuthz true - nil entry claims visible",
			skipAuthz:     true,
			entryClaims:   nil,
			expectVisible: true,
		},
		{
			name:          "skipAuthz true - non-matching entry claims visible",
			skipAuthz:     true,
			entryClaims:   []byte(`{"org":"other-org"}`),
			expectVisible: true,
		},
		{
			name:          "skipAuthz false - nil entry claims invisible",
			skipAuthz:     false,
			entryClaims:   nil,
			expectVisible: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var svc *dbService
			var cleanup func()
			if tt.skipAuthz {
				svc, cleanup = setupTestServiceWithSkipAuthz(t)
			} else {
				svc, cleanup = setupTestService(t)
			}
			defer cleanup()

			ctx := t.Context()
			queries := sqlc.New(svc.pool)
			now := time.Now().UTC()

			regName := "sa-srv-reg-" + tt.name
			srcName := "sa-srv-src-" + tt.name
			entryName := "com.skipauth/" + tt.name

			// Tag registry and source with claims that the caller covers so the
			// registry-access gate passes; the entry's own claims drive what we're
			// testing (visibility under skipAuthz vs. default-deny on empty).
			gateClaims, err := json.Marshal(map[string]any{"org": "caller-org"})
			require.NoError(t, err)

			src, err := queries.InsertSource(ctx, sqlc.InsertSourceParams{
				Name:         srcName,
				CreationType: sqlc.CreationTypeCONFIG,
				SourceType:   "git",
				Syncable:     true,
				Claims:       gateClaims,
				CreatedAt:    &now,
				UpdatedAt:    &now,
			})
			require.NoError(t, err)

			reg, err := queries.UpsertRegistry(ctx, sqlc.UpsertRegistryParams{
				Name:         regName,
				CreationType: sqlc.CreationTypeCONFIG,
				Claims:       gateClaims,
				CreatedAt:    &now,
				UpdatedAt:    &now,
			})
			require.NoError(t, err)

			err = queries.LinkRegistrySource(ctx, sqlc.LinkRegistrySourceParams{
				RegistryID: reg.ID,
				SourceID:   src.ID,
				Position:   0,
			})
			require.NoError(t, err)

			entryID, err := queries.InsertRegistryEntry(ctx, sqlc.InsertRegistryEntryParams{
				SourceID:  src.ID,
				EntryType: sqlc.EntryTypeMCP,
				Name:      entryName,
				Claims:    tt.entryClaims,
				CreatedAt: &now,
				UpdatedAt: &now,
			})
			require.NoError(t, err)

			versionID, err := queries.InsertEntryVersion(ctx, sqlc.InsertEntryVersionParams{
				EntryID:     entryID,
				Name:        entryName,
				Version:     "1.0.0",
				Title:       ptr.String("Test Server"),
				Description: ptr.String("A test server"),
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

			result, err := svc.ListServers(ctx,
				service.WithRegistryName(regName),
				service.WithClaims(callerClaims),
				service.WithLimit(10),
			)
			require.NoError(t, err)

			if tt.expectVisible {
				require.Len(t, result.Servers, 1)
				require.Equal(t, entryName, result.Servers[0].Name)
			} else {
				require.Empty(t, result.Servers)
			}
		})
	}
}

func TestListSkills_SkipAuthz(t *testing.T) {
	t.Parallel()

	callerClaims := map[string]any{"org": "caller-org"}

	tests := []struct {
		name          string
		skipAuthz     bool
		entryClaims   []byte
		expectVisible bool
	}{
		{
			name:          "skipAuthz true - nil entry claims visible",
			skipAuthz:     true,
			entryClaims:   nil,
			expectVisible: true,
		},
		{
			name:          "skipAuthz true - non-matching entry claims visible",
			skipAuthz:     true,
			entryClaims:   []byte(`{"org":"other-org"}`),
			expectVisible: true,
		},
		{
			name:          "skipAuthz false - nil entry claims invisible",
			skipAuthz:     false,
			entryClaims:   nil,
			expectVisible: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var svc *dbService
			var cleanup func()
			if tt.skipAuthz {
				svc, cleanup = setupTestServiceWithSkipAuthz(t)
			} else {
				svc, cleanup = setupTestService(t)
			}
			defer cleanup()

			ctx := t.Context()
			queries := sqlc.New(svc.pool)
			now := time.Now().UTC()

			regName := "sa-skl-reg-" + tt.name
			srcName := "sa-skl-src-" + tt.name
			entryName := "com.skipauth/" + tt.name

			gateClaims, err := json.Marshal(map[string]any{"org": "caller-org"})
			require.NoError(t, err)

			src, err := queries.InsertSource(ctx, sqlc.InsertSourceParams{
				Name:         srcName,
				CreationType: sqlc.CreationTypeCONFIG,
				SourceType:   "git",
				Syncable:     true,
				Claims:       gateClaims,
				CreatedAt:    &now,
				UpdatedAt:    &now,
			})
			require.NoError(t, err)

			reg, err := queries.UpsertRegistry(ctx, sqlc.UpsertRegistryParams{
				Name:         regName,
				CreationType: sqlc.CreationTypeCONFIG,
				Claims:       gateClaims,
				CreatedAt:    &now,
				UpdatedAt:    &now,
			})
			require.NoError(t, err)

			err = queries.LinkRegistrySource(ctx, sqlc.LinkRegistrySourceParams{
				RegistryID: reg.ID,
				SourceID:   src.ID,
				Position:   0,
			})
			require.NoError(t, err)

			entryID, err := queries.InsertRegistryEntry(ctx, sqlc.InsertRegistryEntryParams{
				SourceID:  src.ID,
				EntryType: sqlc.EntryTypeSKILL,
				Name:      entryName,
				Claims:    tt.entryClaims,
				CreatedAt: &now,
				UpdatedAt: &now,
			})
			require.NoError(t, err)

			versionID, err := queries.InsertEntryVersion(ctx, sqlc.InsertEntryVersionParams{
				EntryID:     entryID,
				Name:        entryName,
				Version:     "1.0.0",
				Title:       ptr.String("Test Skill"),
				Description: ptr.String("A test skill"),
				CreatedAt:   &now,
				UpdatedAt:   &now,
			})
			require.NoError(t, err)

			_, err = queries.InsertSkillVersion(ctx, sqlc.InsertSkillVersionParams{
				VersionID:     versionID,
				Namespace:     "com.skipauth",
				Status:        sqlc.NullSkillStatus{},
				AllowedTools:  nil,
				Repository:    []byte("null"),
				Icons:         []byte("null"),
				Metadata:      []byte("null"),
				ExtensionMeta: []byte("null"),
			})
			require.NoError(t, err)

			result, err := svc.ListSkills(ctx,
				service.WithRegistryName(regName),
				service.WithNamespace("com.skipauth"),
				service.WithClaims(callerClaims),
				service.WithLimit(10),
			)
			require.NoError(t, err)

			if tt.expectVisible {
				require.Len(t, result.Skills, 1)
				require.Equal(t, entryName, result.Skills[0].Name)
			} else {
				require.Empty(t, result.Skills)
			}
		})
	}
}
