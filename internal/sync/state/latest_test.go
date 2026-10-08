package state

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/database"
	"github.com/stacklok/toolhive-registry-server/internal/config"
)

func TestInitializeReconcilesExistingLatest(t *testing.T) {
	t.Parallel()
	db, cleanup := database.SetupTestDB(t)
	defer cleanup()
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, db.Config().ConnString())
	require.NoError(t, err)
	defer pool.Close()
	var source uuid.UUID
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO source (name, creation_type, source_type, syncable)
		VALUES ('existing', 'API', 'managed', false) RETURNING id`).Scan(&source))
	latestIDs := make(map[string]uuid.UUID)
	for _, tc := range []struct{ kind, name string }{
		{"MCP", "example.com/server"}, {"SKILL", "com.example/skill"}, {"PLUGIN", "com.example/plugin"},
	} {
		var entry, oldID, newID uuid.UUID
		require.NoError(t, pool.QueryRow(ctx, `INSERT INTO registry_entry (source_id, entry_type, name)
			VALUES ($1, $2, $3) RETURNING id`, source, tc.kind, tc.name).Scan(&entry))
		require.NoError(t, pool.QueryRow(ctx, `INSERT INTO entry_version (entry_id, name, version)
			VALUES ($1, $2, '1.0.0') RETURNING id`, entry, tc.name).Scan(&oldID))
		require.NoError(t, pool.QueryRow(ctx, `INSERT INTO entry_version (entry_id, name, version)
			VALUES ($1, $2, 'v1.0.0') RETURNING id`, entry, tc.name).Scan(&newID))
		_, err = pool.Exec(ctx, `INSERT INTO latest_entry_version (source_id, entry_type, name, version, latest_version_id)
			VALUES ($1, $2, $3, '1.0.0', $4)`, source, tc.kind, tc.name, oldID)
		require.NoError(t, err)
		latestIDs[tc.name] = newID
	}
	state := NewDBStateService(pool)
	require.NoError(t, state.Initialize(ctx, &config.Config{}))
	var before string
	require.NoError(t, pool.QueryRow(ctx, `SELECT jsonb_build_object(
		'entries', (SELECT jsonb_agg(to_jsonb(e) ORDER BY e.entry_type,e.name) FROM registry_entry e WHERE e.source_id=$1),
		'versions', (SELECT jsonb_agg(to_jsonb(v) ORDER BY e.entry_type,e.name,v.version) FROM entry_version v JOIN registry_entry e ON e.id=v.entry_id WHERE e.source_id=$1))`, source).Scan(&before))
	for range 2 {
		require.NoError(t, ReconcileLatestVersions(ctx, pool))
		for name, want := range latestIDs {
			var got uuid.UUID
			require.NoError(t, pool.QueryRow(ctx, `SELECT latest_version_id FROM latest_entry_version
				WHERE source_id=$1 AND name=$2`, source, name).Scan(&got))
			require.Equal(t, want, got, name)
		}
		var after string
		require.NoError(t, pool.QueryRow(ctx, `SELECT jsonb_build_object(
			'entries', (SELECT jsonb_agg(to_jsonb(e) ORDER BY e.entry_type,e.name) FROM registry_entry e WHERE e.source_id=$1),
			'versions', (SELECT jsonb_agg(to_jsonb(v) ORDER BY e.entry_type,e.name,v.version) FROM entry_version v JOIN registry_entry e ON e.id=v.entry_id WHERE e.source_id=$1))`, source).Scan(&after))
		require.Equal(t, before, after, "repair may only update latest pointers")
	}
}
