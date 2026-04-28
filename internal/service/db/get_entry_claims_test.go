package database

import (
	"context"
	"testing"

	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/service"
)

func TestGetEntryClaims_ReturnsClaims(t *testing.T) {
	t.Parallel()

	svc, cleanup := setupTestService(t)
	defer cleanup()

	createManagedSource(t, svc, "gec-claims")

	ctx := context.Background()

	_, err := svc.PublishServerVersion(ctx,
		service.WithServerData(&upstreamv0.ServerJSON{
			Name:    "com.test/gec-claims",
			Version: "1.0.0",
		}),
		service.WithClaims(map[string]any{"org": "acme", "team": "platform"}),
		service.WithJWTClaims(map[string]any{"org": "acme", "team": []string{"platform", "ops"}}),
	)
	require.NoError(t, err)

	claims, err := svc.GetEntryClaims(ctx,
		service.WithEntryType(service.EntryTypeServer),
		service.WithName("com.test/gec-claims"),
	)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"org": "acme", "team": "platform"}, claims)
}

func TestGetEntryClaims_EmptyClaimsReturnsNonNilMap(t *testing.T) {
	t.Parallel()

	svc, cleanup := setupTestService(t)
	defer cleanup()

	createManagedSource(t, svc, "gec-empty")

	ctx := context.Background()

	// Publish a server with no claims (anonymous mode).
	_, err := svc.PublishServerVersion(ctx,
		service.WithServerData(&upstreamv0.ServerJSON{
			Name:    "com.test/gec-empty",
			Version: "1.0.0",
		}),
	)
	require.NoError(t, err)

	claims, err := svc.GetEntryClaims(ctx,
		service.WithEntryType(service.EntryTypeServer),
		service.WithName("com.test/gec-empty"),
	)
	require.NoError(t, err)
	assert.NotNil(t, claims, "claims map must be non-nil for stable JSON shape")
	assert.Empty(t, claims)
}

func TestGetEntryClaims_ClaimsInsufficient(t *testing.T) {
	t.Parallel()

	svc, cleanup := setupTestService(t)
	defer cleanup()

	createManagedSource(t, svc, "gec-insufficient")

	ctx := context.Background()

	// Publish an entry scoped to team=data.
	_, err := svc.PublishServerVersion(ctx,
		service.WithServerData(&upstreamv0.ServerJSON{
			Name:    "com.test/gec-insufficient",
			Version: "1.0.0",
		}),
		service.WithClaims(map[string]any{"org": "acme", "team": "data"}),
		service.WithJWTClaims(map[string]any{"org": "acme", "team": []string{"data"}}),
	)
	require.NoError(t, err)

	// A caller in team=platform must not be able to read its claims.
	_, err = svc.GetEntryClaims(ctx,
		service.WithEntryType(service.EntryTypeServer),
		service.WithName("com.test/gec-insufficient"),
		service.WithJWTClaims(map[string]any{"org": "acme", "team": "platform"}),
	)
	assert.ErrorIs(t, err, service.ErrClaimsInsufficient)
}

func TestGetEntryClaims_ClaimsSufficient(t *testing.T) {
	t.Parallel()

	svc, cleanup := setupTestService(t)
	defer cleanup()

	createManagedSource(t, svc, "gec-sufficient")

	ctx := context.Background()

	_, err := svc.PublishServerVersion(ctx,
		service.WithServerData(&upstreamv0.ServerJSON{
			Name:    "com.test/gec-sufficient",
			Version: "1.0.0",
		}),
		service.WithClaims(map[string]any{"org": "acme", "team": "platform"}),
		service.WithJWTClaims(map[string]any{"org": "acme", "team": []string{"platform"}}),
	)
	require.NoError(t, err)

	// A caller whose JWT covers the entry's claims must succeed.
	claims, err := svc.GetEntryClaims(ctx,
		service.WithEntryType(service.EntryTypeServer),
		service.WithName("com.test/gec-sufficient"),
		service.WithJWTClaims(map[string]any{"org": "acme", "team": []string{"platform", "ops"}}),
	)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"org": "acme", "team": "platform"}, claims)
}

func TestGetEntryClaims_EntryNotFound(t *testing.T) {
	t.Parallel()

	svc, cleanup := setupTestService(t)
	defer cleanup()

	createManagedSource(t, svc, "gec-not-found")

	ctx := context.Background()

	_, err := svc.GetEntryClaims(ctx,
		service.WithEntryType(service.EntryTypeServer),
		service.WithName("com.test/nonexistent"),
	)
	assert.ErrorIs(t, err, service.ErrNotFound)
}

func TestGetEntryClaims_WrongTypeReturnsNotFound(t *testing.T) {
	t.Parallel()

	svc, cleanup := setupTestService(t)
	defer cleanup()

	createManagedSource(t, svc, "gec-wrong-type")

	ctx := context.Background()

	// Publish a server, then try to fetch it as a skill.
	_, err := svc.PublishServerVersion(ctx,
		service.WithServerData(&upstreamv0.ServerJSON{
			Name:    "com.test/gec-wrong-type",
			Version: "1.0.0",
		}),
		service.WithClaims(map[string]any{"org": "acme"}),
	)
	require.NoError(t, err)

	_, err = svc.GetEntryClaims(ctx,
		service.WithEntryType(service.EntryTypeSkill),
		service.WithName("com.test/gec-wrong-type"),
	)
	assert.ErrorIs(t, err, service.ErrNotFound)
}

func TestGetEntryClaims_NoManagedSource(t *testing.T) {
	t.Parallel()

	svc, cleanup := setupTestService(t)
	defer cleanup()

	ctx := context.Background()

	_, err := svc.GetEntryClaims(ctx,
		service.WithEntryType(service.EntryTypeServer),
		service.WithName("com.test/no-managed-source"),
	)
	assert.ErrorIs(t, err, service.ErrNoManagedSource)
}

func TestGetEntryClaims_SkillType(t *testing.T) {
	t.Parallel()

	svc, cleanup := setupTestService(t)
	defer cleanup()

	createManagedSourceWithRegistry(t, svc, "gec-skill")

	ctx := context.Background()

	skill := &service.Skill{
		Namespace: "com.test",
		Name:      "gec-skill",
		Version:   "1.0.0",
		Title:     "Test Skill",
	}
	_, err := svc.PublishSkill(ctx, skill,
		service.WithClaims(map[string]any{"org": "acme"}),
	)
	require.NoError(t, err)

	claims, err := svc.GetEntryClaims(ctx,
		service.WithEntryType(service.EntryTypeSkill),
		service.WithName("gec-skill"),
	)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"org": "acme"}, claims)
}

func TestGetEntryClaims_InvalidEntryType(t *testing.T) {
	t.Parallel()

	svc, cleanup := setupTestService(t)
	defer cleanup()

	ctx := context.Background()

	// Bypass WithEntryType (which validates) and write directly so we exercise
	// the impl-side mapEntryType branch.
	_, err := svc.GetEntryClaims(ctx,
		func(o any) error {
			opts, ok := o.(*service.GetEntryClaimsOptions)
			if !ok {
				return nil
			}
			opts.EntryType = "widget"
			return nil
		},
		service.WithName("anything"),
	)
	assert.ErrorIs(t, err, service.ErrInvalidEntryType)
}
