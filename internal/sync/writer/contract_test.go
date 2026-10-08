package writer

import (
	"context"
	"testing"
	"time"

	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	toolhivetypes "github.com/stacklok/toolhive-core/registry/types"
	"github.com/stretchr/testify/require"
)

// TestStoreContractAllKindsAtomicity exercises a failure in the last kind, after
// the server and skill upserts and orphan cleanup have run in the transaction.
func TestStoreContractAllKindsAtomicity(t *testing.T) {
	t.Parallel()
	pool, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()
	first := createTestRegistry(t, pool, "first-source")
	second := createTestRegistry(t, pool, "second-source")
	w, err := NewDBSyncWriter(pool, testMaxMetaSize)
	require.NoError(t, err)
	snapshot := func(suffix string) *toolhivetypes.UpstreamRegistry {
		server := createTestServerWithPackages("example.com/server", "1.0.0")
		server.Description = suffix
		skill := createTestSkillWithPackages("com.example", "skill", "1.0.0")
		skill.Description = suffix
		plugin := createTestPluginWithPackages("com.example", "plugin", "1.0.0")
		plugin.Description = suffix
		reg := createTestUpstreamRegistry([]upstreamv0.ServerJSON{server})
		reg.Data.Skills = []toolhivetypes.Skill{skill}
		reg.Data.Plugins = []toolhivetypes.Plugin{plugin}
		return reg
	}
	old := snapshot("original")
	require.NoError(t, w.Store(ctx, "first-source", old))
	require.NoError(t, w.Store(ctx, "second-source", old))
	// Include version rows, payloads, related packages, and latest pointers in
	// the same exact comparison; counts alone would miss partial overwrites.
	const stateQuery = `SELECT jsonb_build_object(
  'versions', (SELECT coalesce(jsonb_agg(jsonb_build_object('kind',e.entry_type,'name',e.name,'version',v.version,'description',v.description,'title',v.title) ORDER BY e.entry_type,e.name,v.version),'[]'::jsonb) FROM registry_entry e JOIN entry_version v ON v.entry_id=e.id WHERE e.source_id=$1),
  'latest', (SELECT coalesce(jsonb_agg(jsonb_build_object('kind',e.entry_type,'name',e.name,'version',v.version) ORDER BY e.entry_type,e.name),'[]'::jsonb) FROM registry_entry e JOIN latest_entry_version l ON l.source_id=e.source_id AND l.name=e.name JOIN entry_version v ON v.id=l.latest_version_id AND v.entry_id=e.id WHERE e.source_id=$1),
  'server_payloads', (SELECT coalesce(jsonb_agg(to_jsonb(s) ORDER BY e.name,v.version),'[]'::jsonb) FROM mcp_server s JOIN entry_version v ON v.id=s.version_id JOIN registry_entry e ON e.id=v.entry_id WHERE e.source_id=$1),
  'skill_payloads', (SELECT coalesce(jsonb_agg(to_jsonb(s) ORDER BY e.name,v.version),'[]'::jsonb) FROM skill s JOIN entry_version v ON v.id=s.version_id JOIN registry_entry e ON e.id=v.entry_id WHERE e.source_id=$1),
  'plugin_payloads', (SELECT coalesce(jsonb_agg(to_jsonb(s) ORDER BY e.name,v.version),'[]'::jsonb) FROM plugin s JOIN entry_version v ON v.id=s.version_id JOIN registry_entry e ON e.id=v.entry_id WHERE e.source_id=$1),
  'server_packages', (SELECT coalesce(jsonb_agg(to_jsonb(p) ORDER BY e.name,v.version),'[]'::jsonb) FROM mcp_server_package p JOIN entry_version v ON v.id=p.server_id JOIN registry_entry e ON e.id=v.entry_id WHERE e.source_id=$1),
  'skill_packages', (SELECT coalesce(jsonb_agg(to_jsonb(p) ORDER BY e.name,v.version),'[]'::jsonb) FROM skill_oci_package p JOIN skill s ON s.version_id=p.skill_id JOIN entry_version v ON v.id=s.version_id JOIN registry_entry e ON e.id=v.entry_id WHERE e.source_id=$1),
  'skill_git_packages', (SELECT coalesce(jsonb_agg(to_jsonb(p) ORDER BY e.name,v.version),'[]'::jsonb) FROM skill_git_package p JOIN skill s ON s.version_id=p.skill_id JOIN entry_version v ON v.id=s.version_id JOIN registry_entry e ON e.id=v.entry_id WHERE e.source_id=$1),
  'plugin_packages', (SELECT coalesce(jsonb_agg(to_jsonb(p) ORDER BY e.name,v.version),'[]'::jsonb) FROM plugin_oci_package p JOIN plugin s ON s.version_id=p.plugin_id JOIN entry_version v ON v.id=s.version_id JOIN registry_entry e ON e.id=v.entry_id WHERE e.source_id=$1),
  'plugin_git_packages', (SELECT coalesce(jsonb_agg(to_jsonb(p) ORDER BY e.name,v.version),'[]'::jsonb) FROM plugin_git_package p JOIN plugin s ON s.version_id=p.plugin_id JOIN entry_version v ON v.id=s.version_id JOIN registry_entry e ON e.id=v.entry_id WHERE e.source_id=$1))`
	state := func(id any) string {
		t.Helper()
		var got string
		require.NoError(t, pool.QueryRow(ctx, stateQuery, id).Scan(&got))
		return got
	}
	beforeFirst, beforeSecond := state(first.sourceID), state(second.sourceID)
	// The trigger is test-local and fires only in the final plugin stage.
	_, err = pool.Exec(ctx, `CREATE FUNCTION fail_plugin_contract() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'late plugin failure'; END $$`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `CREATE TRIGGER fail_plugin_contract BEFORE INSERT OR UPDATE ON plugin FOR EACH ROW EXECUTE FUNCTION fail_plugin_contract()`)
	require.NoError(t, err)
	changed := snapshot("attempted")
	changed.Data.Servers = []upstreamv0.ServerJSON{createTestServer("example.com/new", "2.0.0")}
	changed.Data.Skills = []toolhivetypes.Skill{createTestSkill("com.example", "new", "2.0.0")}
	err = w.Store(ctx, "first-source", changed)
	require.ErrorContains(t, err, "late plugin failure")
	require.Equal(t, beforeFirst, state(first.sourceID))
	require.Equal(t, beforeSecond, state(second.sourceID))
	_, err = pool.Exec(ctx, `DROP TRIGGER fail_plugin_contract ON plugin`)
	require.NoError(t, err)

	// Hold the plugin table lock on another connection. The writer reaches this
	// last stage with a live transaction; cancel only after PostgreSQL reports
	// the writer blocked on the lock, not before Store starts.
	blocker, err := pool.Acquire(ctx)
	require.NoError(t, err)
	defer blocker.Release()
	tx, err := blocker.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(context.Background()) }()
	_, err = tx.Exec(ctx, `LOCK TABLE plugin IN ACCESS EXCLUSIVE MODE`)
	require.NoError(t, err)
	var blockerPID int
	require.NoError(t, tx.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&blockerPID))
	active, cancel := context.WithCancel(ctx)
	cancelledSnapshot := snapshot("attempted")
	cancelledSnapshot.Data.Servers = append(cancelledSnapshot.Data.Servers, createTestServer("example.com/new", "2.0.0"))
	cancelledSnapshot.Data.Skills = append(cancelledSnapshot.Data.Skills, createTestSkill("com.example", "new", "2.0.0"))
	finished := make(chan struct{})
	var storeErr error
	go func() {
		storeErr = w.Store(active, "first-source", cancelledSnapshot)
		close(finished)
	}()
	defer func() {
		cancel()
		select {
		case <-finished:
		case <-time.After(20 * time.Second):
			t.Error("store did not stop after cancellation")
		}
	}()
	waitCtx, stopWaiting := context.WithTimeout(ctx, 30*time.Second)
	defer stopWaiting()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	var waiting bool
	for !waiting {
		select {
		case <-finished:
			require.FailNow(t, "store ended before plugin lock wait", storeErr)
		case <-waitCtx.Done():
			require.FailNow(t, "store did not reach plugin lock wait", waitCtx.Err())
		case <-ticker.C:
			// Match only this database's plugin query blocked by our connection.
			require.NoError(t, pool.QueryRow(waitCtx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity a WHERE a.datname=current_database() AND a.wait_event_type='Lock' AND a.query LIKE '%plugin%' AND $1=ANY(pg_blocking_pids(a.pid)))`, blockerPID).Scan(&waiting))
		}
	}
	cancel()
	select {
	case <-finished:
		require.Error(t, storeErr)
	case <-time.After(20 * time.Second):
		require.FailNow(t, "store did not stop after cancellation")
	}
	require.NoError(t, tx.Rollback(ctx))
	require.Equal(t, beforeFirst, state(first.sourceID))
	require.Equal(t, beforeSecond, state(second.sourceID))

	require.NoError(t, w.Store(ctx, "first-source", createTestUpstreamRegistry(nil)))
	require.NotEqual(t, beforeFirst, state(first.sourceID))
	require.Equal(t, beforeSecond, state(second.sourceID))
	for _, kind := range []string{"MCP", "SKILL", "PLUGIN"} {
		var count int
		require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM registry_entry WHERE source_id=$1 AND entry_type=$2`, first.sourceID, kind).Scan(&count))
		require.Zero(t, count, kind)
	}
}
