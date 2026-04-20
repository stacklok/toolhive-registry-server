package database

import (
	"context"
	"testing"

	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/auth"
	"github.com/stacklok/toolhive-registry-server/internal/service"
)

func TestUpdateEntryClaims_Success(t *testing.T) {
	t.Parallel()

	svc, cleanup := setupTestService(t)
	defer cleanup()

	sourceName := "uec-success"
	createManagedSource(t, svc, sourceName)

	ctx := context.Background()

	// Publish a server with claims {"org": "acme"}
	_, err := svc.PublishServerVersion(ctx,
		service.WithServerData(&upstreamv0.ServerJSON{
			Name:    "com.test/uec-success",
			Version: "1.0.0",
		}),
		service.WithClaims(map[string]any{"org": "acme"}),
		service.WithJWTClaims(map[string]any{"org": "acme", "team": []string{"eng", "ops"}}),
	)
	require.NoError(t, err)

	// Update claims to {"org": "acme", "team": "eng"} with a JWT that covers both
	err = svc.UpdateEntryClaims(ctx,
		service.WithEntryType("server"),
		service.WithName("com.test/uec-success"),
		service.WithClaims(map[string]any{"org": "acme", "team": "eng"}),
		service.WithJWTClaims(map[string]any{"org": "acme", "team": []string{"eng", "ops"}}),
	)
	require.NoError(t, err)
}

func TestUpdateEntryClaims_CallerMustCoverExistingClaims(t *testing.T) {
	t.Parallel()

	svc, cleanup := setupTestService(t)
	defer cleanup()

	sourceName := "uec-cover-existing"
	createManagedSource(t, svc, sourceName)

	ctx := context.Background()

	// Publish a server with claims {"org": "acme"}
	_, err := svc.PublishServerVersion(ctx,
		service.WithServerData(&upstreamv0.ServerJSON{
			Name:    "com.test/uec-cover-existing",
			Version: "1.0.0",
		}),
		service.WithClaims(map[string]any{"org": "acme"}),
		service.WithJWTClaims(map[string]any{"org": "acme"}),
	)
	require.NoError(t, err)

	// Attempt to update with a JWT that does NOT cover existing claims
	err = svc.UpdateEntryClaims(ctx,
		service.WithEntryType("server"),
		service.WithName("com.test/uec-cover-existing"),
		service.WithClaims(map[string]any{"org": "contoso"}),
		service.WithJWTClaims(map[string]any{"org": "contoso"}),
	)
	assert.ErrorIs(t, err, service.ErrClaimsInsufficient)
}

func TestUpdateEntryClaims_CallerMustCoverNewClaims(t *testing.T) {
	t.Parallel()

	svc, cleanup := setupTestService(t)
	defer cleanup()

	sourceName := "uec-cover-new"
	createManagedSource(t, svc, sourceName)

	ctx := context.Background()

	// Publish a server with claims {"org": "acme"}
	_, err := svc.PublishServerVersion(ctx,
		service.WithServerData(&upstreamv0.ServerJSON{
			Name:    "com.test/uec-cover-new",
			Version: "1.0.0",
		}),
		service.WithClaims(map[string]any{"org": "acme"}),
		service.WithJWTClaims(map[string]any{"org": "acme"}),
	)
	require.NoError(t, err)

	// Attempt to update with new claims that the JWT doesn't cover
	err = svc.UpdateEntryClaims(ctx,
		service.WithEntryType("server"),
		service.WithName("com.test/uec-cover-new"),
		service.WithClaims(map[string]any{"org": "acme", "team": "eng"}),
		service.WithJWTClaims(map[string]any{"org": "acme"}),
	)
	assert.ErrorIs(t, err, service.ErrClaimsInsufficient)
}

func TestUpdateEntryClaims_NilJWTSkipsValidation(t *testing.T) {
	t.Parallel()

	svc, cleanup := setupTestService(t)
	defer cleanup()

	sourceName := "uec-nil-jwt"
	createManagedSource(t, svc, sourceName)

	ctx := context.Background()

	// Publish a server with claims {"org": "acme"}
	_, err := svc.PublishServerVersion(ctx,
		service.WithServerData(&upstreamv0.ServerJSON{
			Name:    "com.test/uec-nil-jwt",
			Version: "1.0.0",
		}),
		service.WithClaims(map[string]any{"org": "acme"}),
	)
	require.NoError(t, err)

	// Update without passing WithJWTClaims (nil JWT means anonymous mode)
	err = svc.UpdateEntryClaims(ctx,
		service.WithEntryType("server"),
		service.WithName("com.test/uec-nil-jwt"),
		service.WithClaims(map[string]any{"org": "acme", "team": "eng"}),
	)
	require.NoError(t, err)
}

func TestUpdateEntryClaims_EntryNotFound(t *testing.T) {
	t.Parallel()

	svc, cleanup := setupTestService(t)
	defer cleanup()

	sourceName := "uec-not-found"
	createManagedSource(t, svc, sourceName)

	ctx := context.Background()

	// Attempt to update claims on an entry that doesn't exist
	err := svc.UpdateEntryClaims(ctx,
		service.WithEntryType("server"),
		service.WithName("com.test/nonexistent-server"),
		service.WithClaims(map[string]any{"org": "acme"}),
		service.WithJWTClaims(map[string]any{"org": "acme"}),
	)
	assert.ErrorIs(t, err, service.ErrNotFound)
}

func TestUpdateEntryClaims_ClearClaims(t *testing.T) {
	t.Parallel()

	svc, cleanup := setupTestService(t)
	defer cleanup()

	sourceName := "uec-clear"
	createManagedSource(t, svc, sourceName)

	ctx := context.Background()

	// Publish a server with claims {"org": "acme"}
	_, err := svc.PublishServerVersion(ctx,
		service.WithServerData(&upstreamv0.ServerJSON{
			Name:    "com.test/uec-clear",
			Version: "1.0.0",
		}),
		service.WithClaims(map[string]any{"org": "acme"}),
	)
	require.NoError(t, err)

	// Update without passing WithClaims (should clear claims)
	err = svc.UpdateEntryClaims(ctx,
		service.WithEntryType("server"),
		service.WithName("com.test/uec-clear"),
	)
	require.NoError(t, err)
}

func TestUpdateEntryClaims_NoManagedSource(t *testing.T) {
	t.Parallel()

	svc, cleanup := setupTestService(t)
	defer cleanup()

	ctx := context.Background()

	// Do NOT call createManagedSource - attempt to update claims
	err := svc.UpdateEntryClaims(ctx,
		service.WithEntryType("server"),
		service.WithName("com.test/no-managed-source"),
		service.WithClaims(map[string]any{"org": "acme"}),
	)
	assert.ErrorIs(t, err, service.ErrNoManagedSource)
}

func TestUpdateEntryClaims_SuperAdminBypass(t *testing.T) {
	t.Parallel()

	svc, cleanup := setupTestService(t)
	defer cleanup()

	sourceName := "uec-superadmin"
	createManagedSource(t, svc, sourceName)

	ctx := context.Background()

	// Publish a server with claims {"org": "acme"}
	_, err := svc.PublishServerVersion(ctx,
		service.WithServerData(&upstreamv0.ServerJSON{
			Name:    "com.test/uec-superadmin",
			Version: "1.0.0",
		}),
		service.WithClaims(map[string]any{"org": "acme"}),
	)
	require.NoError(t, err)

	// Update with non-matching JWT but with super-admin context
	saCtx := auth.ContextWithRoles(ctx, []auth.Role{auth.RoleSuperAdmin})

	err = svc.UpdateEntryClaims(saCtx,
		service.WithEntryType("server"),
		service.WithName("com.test/uec-superadmin"),
		service.WithClaims(map[string]any{"org": "other"}),
		service.WithJWTClaims(map[string]any{"org": "other"}),
	)
	require.NoError(t, err)
}

func TestUpdateEntryClaims_SkillType(t *testing.T) {
	t.Parallel()

	svc, cleanup := setupTestService(t)
	defer cleanup()

	registryName := "uec-skill"
	createManagedSourceWithRegistry(t, svc, registryName)

	ctx := context.Background()

	// Publish a skill with claims {"org": "acme"}
	skill := &service.Skill{
		Namespace: "com.test",
		Name:      "uec-skill",
		Version:   "1.0.0",
		Title:     "Test Skill",
	}
	_, err := svc.PublishSkill(ctx, skill,
		service.WithClaims(map[string]any{"org": "acme"}),
		service.WithJWTClaims(map[string]any{"org": "acme", "team": []string{"eng", "ops"}}),
	)
	require.NoError(t, err)

	// Update the skill's claims
	err = svc.UpdateEntryClaims(ctx,
		service.WithEntryType("skill"),
		service.WithName("uec-skill"),
		service.WithClaims(map[string]any{"org": "acme", "team": "eng"}),
		service.WithJWTClaims(map[string]any{"org": "acme", "team": []string{"eng", "ops"}}),
	)
	require.NoError(t, err)
}
