package state

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/status"
)

func TestInitializeReplacesManagedSource(t *testing.T) {
	t.Parallel()
	pool := repairTestPool(t)
	ctx := context.Background()
	state := NewDBStateService(pool)
	oldConfig := &config.Config{
		Sources:    []config.SourceConfig{{Name: "old", Managed: &config.ManagedConfig{}}},
		Registries: []config.RegistryConfig{{Name: "view", Sources: []string{"old"}}},
	}
	newConfig := &config.Config{
		Sources:    []config.SourceConfig{{Name: "new", Managed: &config.ManagedConfig{}}},
		Registries: []config.RegistryConfig{{Name: "view", Sources: []string{"new"}}},
	}
	require.NoError(t, state.Initialize(ctx, oldConfig))
	var oldID uuid.UUID
	require.NoError(t, pool.QueryRow(ctx, `SELECT id FROM source WHERE name='old'`).Scan(&oldID))
	_ = repairTestEntry(t, pool, oldID, "MCP", "owned", "1.0.0")
	require.NoError(t, state.UpdateSyncStatus(ctx, "old", &status.SyncStatus{Phase: status.SyncPhaseComplete, AttemptCount: 7}))

	// Even an unvalidated two-managed config cannot leave two managed rows or
	// partially remove the old source when the immediate index rejects it.
	require.Error(t, state.Initialize(ctx, &config.Config{Sources: []config.SourceConfig{
		{Name: "first", Managed: &config.ManagedConfig{}},
		{Name: "second", Managed: &config.ManagedConfig{}},
	}}))
	var remaining uuid.UUID
	require.NoError(t, pool.QueryRow(ctx, `SELECT id FROM source WHERE source_type='managed'`).Scan(&remaining))
	require.Equal(t, oldID, remaining)

	for range 2 {
		require.NoError(t, state.Initialize(ctx, newConfig))
		var managedCount, entries, oldSync int
		require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM source WHERE source_type='managed'`).Scan(&managedCount))
		require.Equal(t, 1, managedCount)
		require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM registry_entry WHERE source_id=$1`, oldID).Scan(&entries))
		require.Zero(t, entries)
		require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM registry_sync WHERE source_id=$1`, oldID).Scan(&oldSync))
		require.Zero(t, oldSync)
		var linked string
		require.NoError(t, pool.QueryRow(ctx, `SELECT s.name FROM registry_source rs JOIN source s ON s.id=rs.source_id
			JOIN registry r ON r.id=rs.registry_id WHERE r.name='view'`).Scan(&linked))
		require.Equal(t, "new", linked)
	}
}

func TestInitializeDemotesRetainedManagedSourceBeforeReplacement(t *testing.T) {
	t.Parallel()
	pool := repairTestPool(t)
	ctx := context.Background()
	state := NewDBStateService(pool)
	require.NoError(t, state.Initialize(ctx, &config.Config{
		Sources: []config.SourceConfig{{Name: "old", Managed: &config.ManagedConfig{}}},
	}))
	var oldID uuid.UUID
	require.NoError(t, pool.QueryRow(ctx, `SELECT id FROM source WHERE name='old'`).Scan(&oldID))
	_ = repairTestEntry(t, pool, oldID, "MCP", "owned", "1.0.0")
	require.NoError(t, state.UpdateSyncStatus(ctx, "old", &status.SyncStatus{Phase: status.SyncPhaseComplete, AttemptCount: 7}))
	for range 2 {
		require.NoError(t, state.Initialize(ctx, &config.Config{
			Sources: []config.SourceConfig{
				{Name: "old", Kubernetes: &config.KubernetesConfig{}},
				{Name: "new", Managed: &config.ManagedConfig{}},
			},
		}))
		var id uuid.UUID
		var kind string
		require.NoError(t, pool.QueryRow(ctx, `SELECT id,source_type FROM source WHERE name='old'`).Scan(&id, &kind))
		require.Equal(t, oldID, id)
		require.Equal(t, "kubernetes", kind)
		var count int
		require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM registry_entry WHERE source_id=$1`, oldID).Scan(&count))
		require.Equal(t, 1, count)
		var claims []byte
		require.NoError(t, pool.QueryRow(ctx, `SELECT claims FROM registry_entry WHERE source_id=$1`, oldID).Scan(&claims))
		require.JSONEq(t, `{"team":"example"}`, string(claims))
		got, err := state.GetSyncStatus(ctx, "old")
		require.NoError(t, err)
		require.Equal(t, 7, got.AttemptCount)
		require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM source WHERE source_type='managed'`).Scan(&count))
		require.Equal(t, 1, count)
	}
}

func TestInitializeManagedReplacementReferencedByAPIViewRollsBack(t *testing.T) {
	t.Parallel()
	pool := repairTestPool(t)
	ctx := context.Background()
	state := NewDBStateService(pool)
	oldConfig := &config.Config{
		Sources:    []config.SourceConfig{{Name: "old", Managed: &config.ManagedConfig{}}},
		Registries: []config.RegistryConfig{{Name: "config-view", Sources: []string{"old"}}},
	}
	require.NoError(t, state.Initialize(ctx, oldConfig))
	var oldID uuid.UUID
	require.NoError(t, pool.QueryRow(ctx, `SELECT id FROM source WHERE name='old'`).Scan(&oldID))
	_ = repairTestEntry(t, pool, oldID, "MCP", "owned", "1.0.0")
	require.NoError(t, state.UpdateSyncStatus(ctx, "old", &status.SyncStatus{Phase: status.SyncPhaseComplete, AttemptCount: 7}))
	var apiViewID uuid.UUID
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO registry(name,creation_type) VALUES('api-view','API') RETURNING id`).Scan(&apiViewID))
	_, err := pool.Exec(ctx, `INSERT INTO registry_source(registry_id,source_id) VALUES($1,$2)`, apiViewID, oldID)
	require.NoError(t, err)

	err = state.Initialize(ctx, &config.Config{
		Sources:    []config.SourceConfig{{Name: "new", Managed: &config.ManagedConfig{}}},
		Registries: []config.RegistryConfig{{Name: "config-view", Sources: []string{"new"}}},
	})
	require.Error(t, err)
	var count int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM source WHERE source_type='managed' AND id=$1`, oldID).Scan(&count))
	require.Equal(t, 1, count)
	require.ErrorIs(t, pool.QueryRow(ctx, `SELECT id FROM source WHERE name='new'`).Scan(new(uuid.UUID)), pgx.ErrNoRows)
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM registry_source WHERE source_id=$1`, oldID).Scan(&count))
	require.Equal(t, 2, count, "both CONFIG and API links must survive rollback")
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM registry_entry WHERE source_id=$1`, oldID).Scan(&count))
	require.Equal(t, 1, count)
	got, err := state.GetSyncStatus(ctx, "old")
	require.NoError(t, err)
	require.Equal(t, 7, got.AttemptCount)

	// A failed replacement leaves the old config usable on the next startup.
	require.NoError(t, state.Initialize(ctx, oldConfig))
}

func TestInitializePreservesNonSyncedEntryClaims(t *testing.T) {
	t.Parallel()
	for _, source := range []config.SourceConfig{
		{Name: "managed", Managed: &config.ManagedConfig{}, Claims: map[string]any{"source": "managed"}},
		{Name: "kubernetes", Kubernetes: &config.KubernetesConfig{}, Claims: map[string]any{"source": "kubernetes"}},
	} {
		source := source
		t.Run(source.Name, func(t *testing.T) {
			t.Parallel()
			pool := repairTestPool(t)
			ctx := context.Background()
			state := NewDBStateService(pool)
			cfg := &config.Config{Sources: []config.SourceConfig{source}}
			require.NoError(t, state.Initialize(ctx, cfg))

			var sourceID uuid.UUID
			require.NoError(t, pool.QueryRow(ctx, `SELECT id FROM source WHERE name=$1`, source.Name).Scan(&sourceID))
			for name, claims := range map[string]string{"one": `{"team":"one"}`, "two": `{"team":"two"}`} {
				_, err := pool.Exec(ctx, `INSERT INTO registry_entry(source_id,entry_type,name,claims) VALUES($1,'MCP',$2,$3::jsonb)`, sourceID, name, claims)
				require.NoError(t, err)
			}

			require.NoError(t, state.Initialize(ctx, cfg))
			rows, err := pool.Query(ctx, `SELECT claims FROM registry_entry WHERE source_id=$1 ORDER BY name`, sourceID)
			require.NoError(t, err)
			defer rows.Close()
			var claims [][]byte
			for rows.Next() {
				var claim []byte
				require.NoError(t, rows.Scan(&claim))
				claims = append(claims, claim)
			}
			require.NoError(t, rows.Err())
			require.Len(t, claims, 2)
			require.JSONEq(t, `{"team":"one"}`, string(claims[0]))
			require.JSONEq(t, `{"team":"two"}`, string(claims[1]))
		})
	}
}

func TestInitializeAPIViewNameConflictRollsBack(t *testing.T) {
	t.Parallel()
	pool := repairTestPool(t)
	ctx := context.Background()
	state := NewDBStateService(pool)

	var apiSourceID, apiViewID uuid.UUID
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO source(name,creation_type,source_type,syncable) VALUES('api-source','API','git',true) RETURNING id`).Scan(&apiSourceID))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO registry(name,claims,creation_type) VALUES('shared-view','{"team":"api"}','API') RETURNING id`).Scan(&apiViewID))
	_, err := pool.Exec(ctx, `INSERT INTO registry_source(registry_id,source_id) VALUES($1,$2)`, apiViewID, apiSourceID)
	require.NoError(t, err)

	err = state.Initialize(ctx, &config.Config{
		Sources:    []config.SourceConfig{{Name: "config-source", Kubernetes: &config.KubernetesConfig{}}},
		Registries: []config.RegistryConfig{{Name: "shared-view", Claims: map[string]any{"team": "config"}, Sources: []string{"config-source"}}},
	})
	require.Error(t, err)

	var gotID uuid.UUID
	var claims []byte
	require.NoError(t, pool.QueryRow(ctx, `SELECT id,claims FROM registry WHERE name='shared-view'`).Scan(&gotID, &claims))
	require.Equal(t, apiViewID, gotID)
	require.JSONEq(t, `{"team":"api"}`, string(claims))
	var linkedSource string
	require.NoError(t, pool.QueryRow(ctx, `SELECT s.name FROM registry_source rs JOIN source s ON s.id=rs.source_id WHERE rs.registry_id=$1`, apiViewID).Scan(&linkedSource))
	require.Equal(t, "api-source", linkedSource)
	require.ErrorIs(t, pool.QueryRow(ctx, `SELECT id FROM source WHERE name='config-source'`).Scan(new(uuid.UUID)), pgx.ErrNoRows)
}
