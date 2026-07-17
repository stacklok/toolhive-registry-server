package database

import (
	"context"
	"testing"
	"time"

	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/auth"
	"github.com/stacklok/toolhive-registry-server/internal/db"
	"github.com/stacklok/toolhive-registry-server/internal/db/sqlc"
	"github.com/stacklok/toolhive-registry-server/internal/service"
)

// createManagedSource creates a managed source tagged with the default test
// claims ({org: acme}). Publishing now gates on the source's claims (#845), so
// the source must carry a claim the publisher's JWT covers; every publish setup
// in these tests uses an org:acme (or nil) JWT. Use createManagedSourceWithClaims
// to control the source's claims explicitly.
func createManagedSource(t *testing.T, svc *dbService, name string) {
	t.Helper()
	createManagedSourceWithClaims(t, svc, name, map[string]any{"org": "acme"})
}

// createManagedSourceWithClaims creates a managed source with the given claims
// (pass nil for an untagged source).
func createManagedSourceWithClaims(t *testing.T, svc *dbService, name string, claims map[string]any) {
	t.Helper()

	ctx := context.Background()
	queries := sqlc.New(svc.pool)

	_, err := queries.InsertSource(ctx, sqlc.InsertSourceParams{
		Name:         name,
		CreationType: sqlc.CreationTypeCONFIG,
		SourceType:   "managed",
		Syncable:     false,
		Claims:       db.SerializeClaims(claims),
	})
	require.NoError(t, err)
}

// createManagedSourceWithRegistry creates a managed source (tagged {org: acme})
// and a registry linked to it. This is needed for skill operations which require
// a registry.
func createManagedSourceWithRegistry(t *testing.T, svc *dbService, name string) {
	t.Helper()
	createManagedSourceWithRegistryClaims(t, svc, name, map[string]any{"org": "acme"})
}

// createManagedSourceWithRegistryClaims is createManagedSourceWithRegistry with
// explicit source claims (pass nil for an untagged source).
func createManagedSourceWithRegistryClaims(t *testing.T, svc *dbService, name string, claims map[string]any) {
	t.Helper()

	ctx := context.Background()
	queries := sqlc.New(svc.pool)

	src, err := queries.InsertSource(ctx, sqlc.InsertSourceParams{
		Name:         name,
		CreationType: sqlc.CreationTypeCONFIG,
		SourceType:   "managed",
		Syncable:     false,
		Claims:       db.SerializeClaims(claims),
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

// TestPublishServerVersion_SourceClaimsGate covers #845: publishing must also
// verify the caller's JWT covers the managed source's claims (visibility / OR),
// independent of the entry-claims subset check.
func TestPublishServerVersion_SourceClaimsGate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		slug         string
		sourceClaims map[string]any
		entryClaims  map[string]any
		jwtClaims    map[string]any
		superAdmin   bool
		wantErr      error
	}{
		{
			name:         "caller covers source claims succeeds",
			slug:         "covered",
			sourceClaims: map[string]any{"org": "acme"},
			entryClaims:  map[string]any{"org": "acme"},
			jwtClaims:    map[string]any{"org": "acme", "team": "eng"},
			wantErr:      nil,
		},
		{
			name:         "caller does not cover source claims returns ErrClaimsInsufficient",
			slug:         "uncovered",
			sourceClaims: map[string]any{"org": "acme"},
			entryClaims:  map[string]any{"org": "contoso"},
			jwtClaims:    map[string]any{"org": "contoso"},
			wantErr:      service.ErrClaimsInsufficient,
		},
		{
			name:         "untagged source denies claim-bearing caller (default-deny)",
			slug:         "untagged",
			sourceClaims: nil,
			entryClaims:  map[string]any{"org": "acme"},
			jwtClaims:    map[string]any{"org": "acme"},
			wantErr:      service.ErrClaimsInsufficient,
		},
		{
			name:         "untagged source with nil JWT (anonymous/skipAuthz) succeeds",
			slug:         "untagged-anon",
			sourceClaims: nil,
			entryClaims:  map[string]any{"org": "acme"},
			jwtClaims:    nil,
			wantErr:      nil,
		},
		{
			name:         "caller shares one value of source array claim succeeds (OR)",
			slug:         "array-or",
			sourceClaims: map[string]any{"org": "acme", "team": []any{"platform", teamDataClaim}},
			entryClaims:  map[string]any{"org": "acme", "team": "platform"},
			jwtClaims:    map[string]any{"org": "acme", "team": "platform"},
			wantErr:      nil,
		},
		{
			name:         "super-admin bypasses source claims gate",
			slug:         "superadmin",
			sourceClaims: map[string]any{"org": "acme"},
			entryClaims:  map[string]any{"org": "contoso"},
			jwtClaims:    map[string]any{"org": "contoso"},
			superAdmin:   true,
			wantErr:      nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc, cleanup := setupTestService(t)
			defer cleanup()

			createManagedSourceWithClaims(t, svc, "pubsrc-"+tt.slug, tt.sourceClaims)

			ctx := context.Background()
			if tt.superAdmin {
				ctx = auth.ContextWithRoles(ctx, []auth.Role{auth.RoleSuperAdmin})
			}

			opts := []service.Option{
				service.WithServerData(&upstreamv0.ServerJSON{
					Name:    "com.test/pubsrc-" + tt.slug,
					Version: "1.0.0",
				}),
			}
			if tt.entryClaims != nil {
				opts = append(opts, service.WithClaims(tt.entryClaims))
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
			}
		})
	}
}

// TestPublishSkill_SourceClaimsGate is the skill counterpart of the #845 gate.
func TestPublishSkill_SourceClaimsGate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		slug         string
		sourceClaims map[string]any
		entryClaims  map[string]any
		jwtClaims    map[string]any
		wantErr      error
	}{
		{
			name:         "caller covers source claims succeeds",
			slug:         "covered",
			sourceClaims: map[string]any{"org": "acme"},
			entryClaims:  map[string]any{"org": "acme"},
			jwtClaims:    map[string]any{"org": "acme", "team": "eng"},
			wantErr:      nil,
		},
		{
			name:         "caller does not cover source claims returns ErrClaimsInsufficient",
			slug:         "uncovered",
			sourceClaims: map[string]any{"org": "acme"},
			entryClaims:  map[string]any{"org": "contoso"},
			jwtClaims:    map[string]any{"org": "contoso"},
			wantErr:      service.ErrClaimsInsufficient,
		},
		{
			name:         "untagged source denies claim-bearing caller (default-deny)",
			slug:         "untagged",
			sourceClaims: nil,
			entryClaims:  map[string]any{"org": "acme"},
			jwtClaims:    map[string]any{"org": "acme"},
			wantErr:      service.ErrClaimsInsufficient,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc, cleanup := setupTestService(t)
			defer cleanup()

			createManagedSourceWithRegistryClaims(t, svc, "pubskill-"+tt.slug, tt.sourceClaims)

			ctx := context.Background()
			skill := &service.Skill{
				Namespace: "com.test",
				Name:      "pubskill-" + tt.slug,
				Version:   "1.0.0",
				Title:     "Test Skill",
			}

			opts := []service.Option{}
			if tt.entryClaims != nil {
				opts = append(opts, service.WithClaims(tt.entryClaims))
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
