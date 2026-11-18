package claims_test

import (
	"context"
	"errors"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	thvregistry "github.com/stacklok/toolhive-core/registry/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/database/dbtest"
	"github.com/stacklok/toolhive-registry-server/internal/auth"
	"github.com/stacklok/toolhive-registry-server/internal/claims"
	"github.com/stacklok/toolhive-registry-server/internal/db/sqlc"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/postgres"
)

type readerFixture struct {
	pool   *pgxpool.Pool
	reader *claims.Reader
}

// newReaderFixture builds registry "main" over sources "high" (first) and
// "low". Both hold skill "shared"; "high" also holds "high-only" and "low"
// holds "low-only". Entry claims: high → org:acme, low → org:community.
func newReaderFixture(t *testing.T) readerFixture {
	t.Helper()
	conn, cleanup := dbtest.SetupTestDB(t)
	t.Cleanup(cleanup)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, conn.Config().ConnString())
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	defs, err := postgres.NewDefinitions(pool)
	require.NoError(t, err)
	entries, err := postgres.NewEntries(ctx, pool, 65536)
	require.NoError(t, err)
	snapshot := func(names ...string) model.Snapshot {
		var s model.Snapshot
		for _, n := range names {
			s.Data.Skills = append(s.Data.Skills, thvregistry.Skill{Namespace: "io.test", Name: n, Version: "1.0.0", Description: n})
		}
		return s
	}
	for _, src := range []struct {
		name, org string
		skills    []string
	}{{"high", "acme", []string{"shared", "high-only"}}, {"low", "community", []string{"shared", "low-only"}}} {
		def, err := defs.CreateSource(ctx, persistence.SourceDefinition{
			Name: src.name, API: &persistence.APISpec{Endpoint: "https://example.org"}, Schedule: "1h"})
		require.NoError(t, err)
		require.NoError(t, entries.ReplaceSnapshot(ctx, def, snapshot(src.skills...)))
		_, err = pool.Exec(ctx, `UPDATE registry_entry SET claims=jsonb_build_object('org',$2::text) WHERE source_id=$1`, def.ID, src.org)
		require.NoError(t, err)
	}
	_, err = defs.CreateView(ctx, persistence.ViewDefinition{Name: "main", Sources: []string{"high", "low"}})
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE registry SET claims='{"org":["acme","community"]}' WHERE name='main'`)
	require.NoError(t, err)

	reader, err := claims.NewReader(entries, sqlc.New(pool))
	require.NoError(t, err)
	return readerFixture{pool: pool, reader: reader}
}

func caller(claimSet jwt.MapClaims, roles ...auth.Role) context.Context {
	ctx := auth.ContextWithClaims(context.Background(), claimSet)
	return auth.ContextWithRoles(ctx, roles)
}

func listNames(ctx context.Context, t *testing.T, r *claims.Reader) map[string]string {
	t.Helper()
	page, err := r.ListViewEntries(ctx, "main", persistence.ViewQuery{Kind: persistence.SkillKind, Limit: 100})
	require.NoError(t, err)
	out := map[string]string{}
	for _, e := range page.Entries {
		out[e.Name] = e.Skill.Description + "@" + e.SourceID
	}
	return out
}

func TestReaderHiddenEntryFallsThrough(t *testing.T) {
	t.Parallel()
	f := newReaderFixture(t)
	var highID, lowID string
	require.NoError(t, f.pool.QueryRow(context.Background(), `SELECT id::text FROM source WHERE name='high'`).Scan(&highID))
	require.NoError(t, f.pool.QueryRow(context.Background(), `SELECT id::text FROM source WHERE name='low'`).Scan(&lowID))

	community := caller(jwt.MapClaims{"sub": "c", "org": "community"})
	got := listNames(community, t, f.reader)
	assert.Equal(t, map[string]string{"shared": "shared@" + lowID, "low-only": "low-only@" + lowID}, got,
		"hidden high entry must not hide low's entry of the same name")

	entry, err := f.reader.GetViewEntry(community, "main",
		persistence.EntryName{Kind: persistence.SkillKind, Name: "shared", Namespace: "io.test"}, persistence.VersionSelector{Latest: true})
	require.NoError(t, err)
	assert.Equal(t, lowID, entry.SourceID, "get falls through like list")

	_, err = f.reader.GetViewEntry(community, "main",
		persistence.EntryName{Kind: persistence.SkillKind, Name: "high-only", Namespace: "io.test"}, persistence.VersionSelector{Latest: true})
	assert.ErrorIs(t, err, persistence.ErrNotFound)

	acme := caller(jwt.MapClaims{"sub": "a", "org": "acme"})
	assert.Equal(t, map[string]string{"shared": "shared@" + highID, "high-only": "high-only@" + highID}, listNames(acme, t, f.reader))

	both := caller(jwt.MapClaims{"sub": "b", "org": []any{"acme", "community"}})
	assert.Equal(t, map[string]string{"shared": "shared@" + highID, "high-only": "high-only@" + highID, "low-only": "low-only@" + lowID},
		listNames(both, t, f.reader), "a caller who sees both gets the priority winner")
}

func TestReaderBypassesForAnonymousAndSuperAdmin(t *testing.T) {
	t.Parallel()
	f := newReaderFixture(t)
	anonymous := context.Background()
	assert.Len(t, listNames(anonymous, t, f.reader), 3)
	admin := caller(jwt.MapClaims{"sub": "root", "org": "nobody"}, auth.RoleSuperAdmin)
	assert.Len(t, listNames(admin, t, f.reader), 3)
}

func TestReaderRegistryGate(t *testing.T) {
	t.Parallel()
	f := newReaderFixture(t)
	ctx := context.Background()
	outsider := caller(jwt.MapClaims{"sub": "o", "org": "other"})
	_, err := f.reader.ListViewEntries(outsider, "main", persistence.ViewQuery{Kind: persistence.SkillKind, Limit: 10})
	assert.True(t, errors.Is(err, claims.ErrRegistryForbidden), "got %v", err)

	_, err = f.pool.Exec(ctx, `UPDATE registry SET claims=NULL WHERE name='main'`)
	require.NoError(t, err)
	community := caller(jwt.MapClaims{"sub": "c", "org": "community"})
	_, err = f.reader.ListViewEntries(community, "main", persistence.ViewQuery{Kind: persistence.SkillKind, Limit: 10})
	assert.ErrorIs(t, err, claims.ErrRegistryForbidden, "a registry without claims is denied to claim-bearing callers")
	assert.Len(t, listNames(context.Background(), t, f.reader), 3, "anonymous still passes")

	_, err = f.reader.ListViewEntries(community, "missing", persistence.ViewQuery{Kind: persistence.SkillKind, Limit: 10})
	assert.ErrorIs(t, err, persistence.ErrNotFound, "an unknown registry is not found, not forbidden")
}

func TestReaderEntryClaimShapes(t *testing.T) {
	t.Parallel()
	f := newReaderFixture(t)
	ctx := context.Background()
	community := caller(jwt.MapClaims{"sub": "c", "org": "community"})
	for _, tc := range []struct {
		stored  any
		visible bool
	}{
		{nil, false},           // SQL NULL hides
		{`{}`, true},           // no requirements
		{`null`, true},         // JSON null, as written by a v1.5.4 kubernetes annotation
		{`{"org":"x"}`, false}, // mismatch
	} {
		_, err := f.pool.Exec(ctx, `UPDATE registry_entry SET claims=$1::jsonb WHERE name='high-only'`, tc.stored)
		require.NoError(t, err)
		_, present := listNames(community, t, f.reader)["high-only"]
		assert.Equal(t, tc.visible, present, "stored claims %v", tc.stored)
	}
}
