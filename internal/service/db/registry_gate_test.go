package database

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/auth"
	"github.com/stacklok/toolhive-registry-server/internal/db/sqlc"
	"github.com/stacklok/toolhive-registry-server/internal/service"
)

// createRegistryWithClaims is a test helper that creates a registry with the
// given name and JSON claims, links a source to it, and returns the registry ID.
func createRegistryWithClaims(t *testing.T, svc *dbService, name string, claims []byte) uuid.UUID {
	t.Helper()

	ctx := t.Context()
	queries := sqlc.New(svc.pool)
	now := time.Now().UTC()

	reg, err := queries.UpsertRegistry(ctx, sqlc.UpsertRegistryParams{
		Name:         name,
		Claims:       claims,
		CreationType: sqlc.CreationTypeAPI,
		CreatedAt:    &now,
		UpdatedAt:    &now,
	})
	require.NoError(t, err)

	srcID, err := queries.UpsertSource(ctx, sqlc.UpsertSourceParams{
		Name:         name + "-source",
		CreationType: sqlc.CreationTypeCONFIG,
		SourceType:   "file",
		Syncable:     false,
	})
	require.NoError(t, err)

	err = queries.LinkRegistrySource(ctx, sqlc.LinkRegistrySourceParams{
		RegistryID: reg.ID,
		SourceID:   srcID,
		Position:   0,
	})
	require.NoError(t, err)

	return reg.ID
}

// mustMarshalGate is a test helper that marshals v to JSON or fails the test.
func mustMarshalGate(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}

func TestLookupRegistryIDWithGate(t *testing.T) {
	t.Parallel()

	svc, cleanup := setupTestService(t)
	t.Cleanup(cleanup)

	tests := []struct {
		name         string
		claims       []byte         // registry claims (nil = open)
		callerClaims map[string]any // JWT claims (nil = anonymous)
		superAdmin   bool
		wantErr      error
	}{
		{
			name:         "nil registry claims with caller claims returns ErrClaimsInsufficient (default-deny)",
			claims:       nil,
			callerClaims: map[string]any{"org": "acme"},
			wantErr:      service.ErrClaimsInsufficient,
		},
		{
			name:         "nil registry claims with nil caller claims passes",
			claims:       nil,
			callerClaims: nil,
			wantErr:      nil,
		},
		{
			name:         "matching claims passes",
			claims:       mustMarshalGate(t, map[string]any{"org": "acme"}),
			callerClaims: map[string]any{"org": "acme"},
			wantErr:      nil,
		},
		{
			name:         "non-matching claims returns ErrClaimsInsufficient",
			claims:       mustMarshalGate(t, map[string]any{"org": "acme"}),
			callerClaims: map[string]any{"org": "contoso"},
			wantErr:      service.ErrClaimsInsufficient,
		},
		{
			name:         "nil caller claims (anonymous) skips check",
			claims:       mustMarshalGate(t, map[string]any{"org": "acme"}),
			callerClaims: nil,
			wantErr:      nil,
		},
		{
			name:         "super-admin bypasses claim check",
			claims:       mustMarshalGate(t, map[string]any{"org": "acme"}),
			callerClaims: map[string]any{"org": "contoso"},
			superAdmin:   true,
			wantErr:      nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()
			regName := "gate-lookup-" + t.Name()

			expectedID := createRegistryWithClaims(t, svc, regName, tt.claims)

			if tt.superAdmin {
				ctx = auth.ContextWithRoles(ctx, []auth.Role{auth.RoleSuperAdmin})
			}

			gotID, err := svc.lookupRegistryIDWithGate(ctx, svc.pool, regName, tt.callerClaims)

			if tt.wantErr != nil {
				require.Error(t, err)
				assert.True(t, errors.Is(err, tt.wantErr), "expected %v, got %v", tt.wantErr, err)
				assert.Equal(t, uuid.Nil, gotID)
			} else {
				require.NoError(t, err)
				assert.Equal(t, expectedID, gotID)
			}
		})
	}

	t.Run("non-existent registry returns ErrRegistryNotFound", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		gotID, err := svc.lookupRegistryIDWithGate(ctx, svc.pool, "does-not-exist", nil)
		require.Error(t, err)
		assert.True(t, errors.Is(err, service.ErrRegistryNotFound), "expected ErrRegistryNotFound, got %v", err)
		assert.Equal(t, uuid.Nil, gotID)
	})
}

func TestCheckRegistryExistsWithGate(t *testing.T) {
	t.Parallel()

	svc, cleanup := setupTestService(t)
	t.Cleanup(cleanup)

	tests := []struct {
		name         string
		claims       []byte
		callerClaims map[string]any
		wantErr      error
	}{
		{
			name:         "open registry with caller claims returns ErrClaimsInsufficient (default-deny)",
			claims:       nil,
			callerClaims: map[string]any{"org": "acme"},
			wantErr:      service.ErrClaimsInsufficient,
		},
		{
			name:         "matching claims passes",
			claims:       mustMarshalGate(t, map[string]any{"org": "acme"}),
			callerClaims: map[string]any{"org": "acme"},
			wantErr:      nil,
		},
		{
			name:         "non-matching claims returns ErrClaimsInsufficient",
			claims:       mustMarshalGate(t, map[string]any{"org": "acme"}),
			callerClaims: map[string]any{"org": "contoso"},
			wantErr:      service.ErrClaimsInsufficient,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()
			regName := "gate-exists-" + t.Name()

			createRegistryWithClaims(t, svc, regName, tt.claims)

			err := svc.checkRegistryExistsWithGate(ctx, svc.pool, regName, tt.callerClaims)

			if tt.wantErr != nil {
				require.Error(t, err)
				assert.True(t, errors.Is(err, tt.wantErr), "expected %v, got %v", tt.wantErr, err)
			} else {
				require.NoError(t, err)
			}
		})
	}

	t.Run("non-existent registry returns ErrRegistryNotFound", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		err := svc.checkRegistryExistsWithGate(ctx, svc.pool, "does-not-exist-check", nil)
		require.Error(t, err)
		assert.True(t, errors.Is(err, service.ErrRegistryNotFound), "expected ErrRegistryNotFound, got %v", err)
	})
}
