package database

import (
	"context"
	"testing"
	"time"

	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/db/sqlc"
	"github.com/stacklok/toolhive-registry-server/internal/service"
)

// createManagedSource is a helper that creates a managed source for publish tests.
func createManagedSource(t *testing.T, svc *dbService, name string) {
	t.Helper()

	ctx := context.Background()
	queries := sqlc.New(svc.pool)

	_, err := queries.InsertSource(ctx, sqlc.InsertSourceParams{
		Name:         name,
		CreationType: sqlc.CreationTypeCONFIG,
		SourceType:   "managed",
		Syncable:     false,
	})
	require.NoError(t, err)
}

// createManagedSourceWithRegistry creates a managed source and a registry linked to it.
// This is needed for skill operations which require a registry.
func createManagedSourceWithRegistry(t *testing.T, svc *dbService, name string) {
	t.Helper()

	ctx := context.Background()
	queries := sqlc.New(svc.pool)

	src, err := queries.InsertSource(ctx, sqlc.InsertSourceParams{
		Name:         name,
		CreationType: sqlc.CreationTypeCONFIG,
		SourceType:   "managed",
		Syncable:     false,
	})
	require.NoError(t, err)

	now := time.Now()
	reg, err := queries.UpsertRegistry(ctx, sqlc.UpsertRegistryParams{
		Name:         name,
		CreationType: sqlc.CreationTypeCONFIG,
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
}

func TestPublishServerVersion_ClaimsSubset(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		serverSlug string
		claims     map[string]any
		jwtClaims  map[string]any
		wantErr    error
	}{
		{
			name:       "JWT superset of request claims succeeds",
			serverSlug: "jwt-superset",
			claims:     map[string]any{"org": "acme"},
			jwtClaims:  map[string]any{"org": "acme", "team": "eng"},
			wantErr:    nil,
		},
		{
			name:       "JWT does not cover request claims returns ErrClaimsInsufficient",
			serverSlug: "jwt-insufficient",
			claims:     map[string]any{"org": "acme", "team": "eng"},
			jwtClaims:  map[string]any{"org": "acme"},
			wantErr:    service.ErrClaimsInsufficient,
		},
		{
			name:       "nil JWT claims skips validation and succeeds",
			serverSlug: "nil-jwt",
			claims:     map[string]any{"org": "acme"},
			jwtClaims:  nil,
			wantErr:    nil,
		},
		{
			name:       "nil request claims with any JWT returns ErrClaimsInsufficient (default-deny)",
			serverSlug: "nil-req-claims",
			claims:     nil,
			jwtClaims:  map[string]any{"org": "acme", "team": "eng"},
			wantErr:    service.ErrClaimsInsufficient,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc, cleanup := setupTestService(t)
			defer cleanup()

			sourceName := "pub-srv-" + tt.serverSlug
			createManagedSource(t, svc, sourceName)

			ctx := context.Background()

			opts := []service.Option{
				service.WithServerData(&upstreamv0.ServerJSON{
					Name:    "com.test/pub-srv-" + tt.serverSlug,
					Version: "1.0.0",
				}),
			}
			if tt.claims != nil {
				opts = append(opts, service.WithClaims(tt.claims))
			}
			if tt.jwtClaims != nil {
				opts = append(opts, service.WithJWTClaims(tt.jwtClaims))
			}

			result, err := svc.PublishServerVersion(ctx, opts...)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				require.Nil(t, result)
			} else {
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Equal(t, "1.0.0", result.Version)
			}
		})
	}
}

func TestPublishSkill_ClaimsSubset(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		claims    map[string]any
		jwtClaims map[string]any
		wantErr   error
	}{
		{
			name:      "JWT superset of request claims succeeds",
			claims:    map[string]any{"org": "acme"},
			jwtClaims: map[string]any{"org": "acme", "team": "eng"},
			wantErr:   nil,
		},
		{
			name:      "JWT does not cover request claims returns ErrClaimsInsufficient",
			claims:    map[string]any{"org": "acme", "team": "eng"},
			jwtClaims: map[string]any{"org": "acme"},
			wantErr:   service.ErrClaimsInsufficient,
		},
		{
			name:      "nil JWT claims skips validation and succeeds",
			claims:    map[string]any{"org": "acme"},
			jwtClaims: nil,
			wantErr:   nil,
		},
		{
			name:      "nil request claims with any JWT returns ErrClaimsInsufficient (default-deny)",
			claims:    nil,
			jwtClaims: map[string]any{"org": "acme", "team": "eng"},
			wantErr:   service.ErrClaimsInsufficient,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc, cleanup := setupTestService(t)
			defer cleanup()

			registryName := "pub-skill-claims-" + tt.name
			createManagedSourceWithRegistry(t, svc, registryName)

			ctx := context.Background()

			skill := &service.Skill{
				Namespace: "com.test",
				Name:      "skill-" + tt.name,
				Version:   "1.0.0",
				Title:     "Test Skill",
			}

			opts := []service.Option{}
			if tt.claims != nil {
				opts = append(opts, service.WithClaims(tt.claims))
			}
			if tt.jwtClaims != nil {
				opts = append(opts, service.WithJWTClaims(tt.jwtClaims))
			}

			result, err := svc.PublishSkill(ctx, skill, opts...)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				require.Nil(t, result)
			} else {
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Equal(t, "1.0.0", result.Version)
			}
		})
	}
}

func TestDeleteServerVersion_ClaimsAuthorization(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		slug            string
		publishClaims   map[string]any
		deleteJWTClaims map[string]any
		wantErr         error
	}{
		{
			name:            "matching JWT covers stored claims and succeeds",
			slug:            "jwt-match",
			publishClaims:   map[string]any{"org": "acme"},
			deleteJWTClaims: map[string]any{"org": "acme", "team": "eng"},
			wantErr:         nil,
		},
		{
			name:            "non-matching JWT returns ErrClaimsInsufficient",
			slug:            "jwt-mismatch",
			publishClaims:   map[string]any{"org": "acme"},
			deleteJWTClaims: map[string]any{"org": "contoso"},
			wantErr:         service.ErrClaimsInsufficient,
		},
		{
			name:            "nil JWT claims skips validation and succeeds",
			slug:            "nil-jwt",
			publishClaims:   map[string]any{"org": "acme"},
			deleteJWTClaims: nil,
			wantErr:         nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc, cleanup := setupTestService(t)
			defer cleanup()

			sourceName := "del-srv-" + tt.slug
			createManagedSource(t, svc, sourceName)

			ctx := context.Background()
			serverName := "com.test/del-srv-" + tt.slug

			// Publish a server version with claims
			publishOpts := []service.Option{
				service.WithServerData(&upstreamv0.ServerJSON{
					Name:    serverName,
					Version: "1.0.0",
				}),
			}
			if tt.publishClaims != nil {
				publishOpts = append(publishOpts, service.WithClaims(tt.publishClaims))
			}

			_, err := svc.PublishServerVersion(ctx, publishOpts...)
			require.NoError(t, err)

			// Attempt to delete with the given JWT claims
			deleteOpts := []service.Option{
				service.WithName(serverName),
				service.WithVersion("1.0.0"),
			}
			if tt.deleteJWTClaims != nil {
				deleteOpts = append(deleteOpts, service.WithJWTClaims(tt.deleteJWTClaims))
			}

			deleteErr := svc.DeleteServerVersion(ctx, deleteOpts...)

			if tt.wantErr != nil {
				require.ErrorIs(t, deleteErr, tt.wantErr)
			} else {
				require.NoError(t, deleteErr)
			}
		})
	}
}

func TestDeleteSkillVersion_ClaimsAuthorization(t *testing.T) {
	t.Parallel()

	const namespace = "com.test"

	tests := []struct {
		name            string
		slug            string
		publishClaims   map[string]any
		deleteJWTClaims map[string]any
		wantErr         error
	}{
		{
			name:            "matching JWT covers stored claims and succeeds",
			slug:            "jwt-match",
			publishClaims:   map[string]any{"org": "acme"},
			deleteJWTClaims: map[string]any{"org": "acme", "team": "eng"},
			wantErr:         nil,
		},
		{
			name:            "non-matching JWT returns ErrClaimsInsufficient",
			slug:            "jwt-mismatch",
			publishClaims:   map[string]any{"org": "acme"},
			deleteJWTClaims: map[string]any{"org": "contoso"},
			wantErr:         service.ErrClaimsInsufficient,
		},
		{
			name:            "nil JWT claims skips validation and succeeds",
			slug:            "nil-jwt",
			publishClaims:   map[string]any{"org": "acme"},
			deleteJWTClaims: nil,
			wantErr:         nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc, cleanup := setupTestService(t)
			defer cleanup()

			registryName := "del-skill-" + tt.slug
			createManagedSourceWithRegistry(t, svc, registryName)

			ctx := context.Background()
			skillName := "del-skill-" + tt.slug

			// Publish a skill version with claims
			skill := &service.Skill{
				Namespace: namespace,
				Name:      skillName,
				Version:   "1.0.0",
				Title:     "Test Skill",
			}
			publishOpts := []service.Option{}
			if tt.publishClaims != nil {
				publishOpts = append(publishOpts, service.WithClaims(tt.publishClaims))
			}

			_, err := svc.PublishSkill(ctx, skill, publishOpts...)
			require.NoError(t, err)

			// Attempt to delete with the given JWT claims
			deleteOpts := []service.Option{
				service.WithName(skillName),
				service.WithVersion("1.0.0"),
				service.WithNamespace(namespace),
			}
			if tt.deleteJWTClaims != nil {
				deleteOpts = append(deleteOpts, service.WithJWTClaims(tt.deleteJWTClaims))
			}

			deleteErr := svc.DeleteSkillVersion(ctx, deleteOpts...)

			if tt.wantErr != nil {
				require.ErrorIs(t, deleteErr, tt.wantErr)
			} else {
				require.NoError(t, deleteErr)
			}
		})
	}
}
