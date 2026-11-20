package database

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/service"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

// TestRepublishKeepsStoredClaims covers F6: re-publishing a name with claims
// equivalent to the stored ones (array order differs) keeps the stored bytes,
// as v1.5.4 did; only the first publish of a name stores claims.
func TestRepublishKeepsStoredClaims(t *testing.T) {
	t.Parallel()

	svc, cleanup := setupTestService(t)
	t.Cleanup(cleanup) // after the parallel subtests, unlike defer
	createManagedSource(t, svc, "republish-claims")

	first := map[string]any{"org": "acme", "team": []any{"a", "b"}}
	again := map[string]any{"team": []any{"b", "a"}, "org": "acme"}

	tests := []struct {
		name    string
		kind    string
		publish func(ctx context.Context, version string, claims map[string]any) error
	}{
		{name: "server", kind: "MCP", publish: func(ctx context.Context, version string, claims map[string]any) error {
			_, err := svc.PublishServerVersion(ctx, service.WithClaims(claims), service.WithServerData(&upstreamv0.ServerJSON{
				Name: "io.test/republish", Version: version, Description: "republish",
			}))
			return err
		}},
		{name: "skill", kind: "SKILL", publish: func(ctx context.Context, version string, claims map[string]any) error {
			_, err := svc.PublishSkill(ctx, &service.Skill{
				Namespace: "com.test", Name: "republish", Version: version, Description: "republish",
			}, service.WithClaims(claims))
			return err
		}},
		{name: "plugin", kind: "PLUGIN", publish: func(ctx context.Context, version string, claims map[string]any) error {
			_, err := svc.PublishPlugin(ctx, &service.Plugin{
				Namespace: "com.test", Name: "republish", Version: version, Description: "republish",
			}, service.WithClaims(claims))
			return err
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			require.NoError(t, tt.publish(ctx, "1.0.0", first))
			require.NoError(t, tt.publish(ctx, "2.0.0", again))

			var raw []byte
			require.NoError(t, svc.pool.QueryRow(ctx,
				`SELECT claims FROM registry_entry WHERE entry_type = $1 AND name LIKE '%republish'`,
				tt.kind).Scan(&raw))
			var stored map[string]any
			require.NoError(t, json.Unmarshal(raw, &stored))
			assert.Equal(t, []any{"a", "b"}, stored["team"], "re-publish must not rewrite the stored claims")
		})
	}
}

// TestLibraryWriteError pins the service errors, and so the v1 status codes
// and texts, that library write errors map to (F3, F4).
func TestLibraryWriteError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		kind     persistence.EntryKind
		err      error
		wantIs   error
		wantNot  error
		wantText string
	}{
		{name: "server duplicate keeps @", kind: persistence.ServerKind, err: persistence.ErrConflict,
			wantIs: service.ErrVersionAlreadyExists, wantText: "version already exists: n@1.0.0"},
		{name: "skill duplicate uses a space", kind: persistence.SkillKind, err: persistence.ErrConflict,
			wantIs: service.ErrVersionAlreadyExists, wantText: "version already exists: n 1.0.0"},
		{name: "plugin duplicate uses a space", kind: persistence.PluginKind, err: persistence.ErrConflict,
			wantIs: service.ErrVersionAlreadyExists, wantText: "version already exists: n 1.0.0"},
		{name: "exhausted retries are not a duplicate", kind: persistence.SkillKind, err: persistence.ErrContention,
			wantIs: persistence.ErrContention, wantNot: service.ErrVersionAlreadyExists},
		{name: "not found", kind: persistence.SkillKind, err: persistence.ErrNotFound,
			wantIs: service.ErrNotFound, wantText: "not found: n@1.0.0"},
		{name: "no managed source", kind: persistence.ServerKind, err: persistence.ErrNoManagedSource,
			wantIs: service.ErrNoManagedSource},
		{name: "claims errors pass through", kind: persistence.ServerKind, err: service.ErrClaimsMismatch,
			wantIs: service.ErrClaimsMismatch},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := libraryWriteError(tt.err, tt.kind, "n", "1.0.0")
			require.ErrorIs(t, got, tt.wantIs)
			if tt.wantNot != nil {
				assert.False(t, errors.Is(got, tt.wantNot))
			}
			if tt.wantText != "" {
				assert.Equal(t, tt.wantText, got.Error())
			}
		})
	}
}
