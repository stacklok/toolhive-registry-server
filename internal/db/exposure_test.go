package db_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/database"
	registrydb "github.com/stacklok/toolhive-registry-server/internal/db"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/postgres"
)

const exposureRevision = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestExposureInventoryStableReadOnlyCoverage(t *testing.T) {
	t.Parallel()
	conn, _ := database.SetupTestDB(t)
	seedExposure(t, conn)
	_, err := conn.Exec(t.Context(), `SET TIME ZONE 'UTC'; SET intervalstyle = 'postgres'`)
	require.NoError(t, err)
	before := exposureCatalog(t, conn)
	inventory := exposureInventory(t, conn)
	require.Equal(t, inventory, exposureInventory(t, conn))
	_, err = conn.Exec(t.Context(), `SET TIME ZONE 'Pacific/Auckland'; SET intervalstyle = 'postgres_verbose'`)
	require.NoError(t, err)
	require.Equal(t, inventory, exposureInventory(t, conn), "session formatting must not change the fingerprint")
	_, err = conn.Exec(t.Context(), `SET TIME ZONE 'UTC'; SET intervalstyle = 'postgres'`)
	require.NoError(t, err)
	require.Equal(t, before, exposureCatalog(t, conn), "inventory must not mutate or repair anything")
	require.Equal(t, int64(30), inventory.Counts["entry_version"])
	require.Equal(t, int64(15), inventory.Counts["registry_entry"])
	classes := map[string]int{}
	for _, record := range inventory.Records {
		if record.Table == "registry_entry" {
			classes[record.Claims]++
			require.True(t, record.Unlinked, "unlinked retained entries must be inventoried")
		}
	}
	require.Equal(t, map[string]int{"sql-null": 3, "json-null": 3, "empty-object": 3, "labeled": 3, "malformed": 3}, classes)
	require.Equal(t, []int{3, 7}, []int{inventory.Links[0].Position, inventory.Links[1].Position})
	encoded, err := json.Marshal(inventory)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private-sentinel")
	var marker *string
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT acknowledgement_id::text FROM exposure_acknowledgement`).Scan(&marker))
	require.Nil(t, marker)

	// Fail when any new catalog table is introduced without explicit coverage.
	rows, err := conn.Query(t.Context(), `SELECT table_name FROM information_schema.tables
 WHERE table_schema='public' AND table_type='BASE TABLE' ORDER BY table_name`)
	require.NoError(t, err)
	var actual []string
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		if name != "schema_migrations" && name != "exposure_acknowledgement" {
			actual = append(actual, name)
		}
	}
	rows.Close()
	require.NoError(t, rows.Err())
	var covered []string
	for name, count := range inventory.Counts {
		covered = append(covered, name)
		require.Positive(t, count, "fixture must populate %s", name)
	}
	sort.Strings(covered)
	require.Equal(t, actual, covered)
}

func TestExposureSQLAndJSONNullPayloadCoverage(t *testing.T) {
	t.Parallel()
	conn, _ := database.SetupTestDB(t)
	seedExposure(t, conn)
	rows, err := conn.Query(t.Context(), `SELECT table_name,column_name FROM information_schema.columns
 WHERE table_schema='public' AND data_type='jsonb' ORDER BY table_name,column_name`)
	require.NoError(t, err)
	var columns [][2]string
	for rows.Next() {
		var column [2]string
		require.NoError(t, rows.Scan(&column[0], &column[1]))
		columns = append(columns, column)
	}
	rows.Close()
	require.NoError(t, rows.Err())
	require.NotEmpty(t, columns)
	for _, column := range columns {
		table := pgx.Identifier{column[0]}.Sanitize()
		field := pgx.Identifier{column[1]}.Sanitize()
		_, err := conn.Exec(t.Context(), "UPDATE "+table+" SET "+field+"=NULL")
		require.NoError(t, err)
		before := exposureInventory(t, conn)
		_, err = conn.Exec(t.Context(), "UPDATE "+table+" SET "+field+"='null'::jsonb")
		require.NoError(t, err)
		require.NotEqual(t, before.Digest, exposureInventory(t, conn).Digest,
			"SQL NULL and JSON null must differ for %s.%s", column[0], column[1])
	}
}

func TestExposureEveryTableMutationInvalidatesDigest(t *testing.T) {
	t.Parallel()
	conn, _ := database.SetupTestDB(t)
	seedExposure(t, conn)
	_, err := conn.Exec(t.Context(), `INSERT INTO source(name,source_type) VALUES('unreferenced','file')`)
	require.NoError(t, err)
	mutations := []string{
		`UPDATE source SET id=gen_random_uuid() WHERE name='unreferenced'`,
		`UPDATE source SET source_config='{"url":"https://changed.example.org"}' WHERE name='unlinked'`,
		`UPDATE source SET claims='{}' WHERE name='linked'`,
		`UPDATE source SET creation_type='API' WHERE name='linked'`,
		`UPDATE source SET definition_generation=definition_generation+1 WHERE name='linked'`,
		`UPDATE registry SET name='renamed'`,
		`UPDATE registry_source SET position=position+1`,
		`UPDATE registry_entry SET claims='{"private":"changed"}'`,
		`UPDATE entry_version SET description='changed'`,
		`UPDATE mcp_server SET server_meta='{"private":"changed"}'`,
		`UPDATE mcp_server_package SET env_vars='{"private":"changed"}'`,
		`UPDATE mcp_server_remote SET transport_variables='{"private":"changed"}'`,
		`UPDATE mcp_server_icon SET sizes=ARRAY['changed']`,
		`UPDATE skill SET provenance='{"private":"changed"}'`,
		`UPDATE skill_oci_package SET digest='changed'`,
		`UPDATE skill_git_package SET ref='changed'`,
		`UPDATE plugin SET extension_meta='{"private":"changed"}'`,
		`UPDATE plugin_oci_package SET digest='changed'`,
		`UPDATE plugin_git_package SET ref='changed'`,
		`UPDATE latest_entry_version SET latest_version_id=v.id,version=v.version FROM entry_version v
 JOIN registry_entry e ON e.id=v.entry_id WHERE e.source_id=latest_entry_version.source_id
 AND e.entry_type=latest_entry_version.entry_type AND e.name=latest_entry_version.name AND v.version='2'`,
		`UPDATE registry_sync SET last_sync_hash='changed'`,
		`UPDATE exposure_acknowledgement SET database_id=gen_random_uuid()`,
	}
	for _, mutation := range mutations {
		before := exposureInventory(t, conn)
		_, err := conn.Exec(t.Context(), mutation)
		require.NoError(t, err)
		after := exposureInventory(t, conn)
		require.NotEqual(t, before.Digest, after.Digest, mutation)
		_, err = registrydb.AcknowledgeExposure(t.Context(), conn, exposureRevision, before.Digest, true, true, false)
		require.ErrorContains(t, err, "digest mismatch")
		require.Equal(t, after, exposureInventory(t, conn), "mismatch must not fence or record a marker")
	}
	inventory := exposureInventory(t, conn)
	changedConfig := strings.Replace(exposureRevision, "aa", "bb", 1)
	changed, err := registrydb.InventoryExposure(t.Context(), conn, changedConfig)
	require.NoError(t, err)
	require.NotEqual(t, inventory.Digest, changed.Digest)
	otherConfig := conn.Config().Copy()
	if otherConfig.Host == "localhost" {
		otherConfig.Host = "127.0.0.1"
	} else {
		otherConfig.Host = "localhost"
	}
	other, err := pgx.ConnectConfig(t.Context(), otherConfig)
	require.NoError(t, err)
	defer other.Close(context.WithoutCancel(t.Context()))
	changed, err = registrydb.InventoryExposure(t.Context(), other, exposureRevision)
	require.NoError(t, err)
	require.NotEqual(t, inventory.Digest, changed.Digest, "bind review to the selected database target")
}

func TestExposureAcknowledgementConsentLeasesAndPreservation(t *testing.T) {
	t.Parallel()
	conn, _ := database.SetupTestDB(t)
	seedExposure(t, conn)
	inventory := exposureInventory(t, conn)
	for _, consent := range []struct{ approve, offline bool }{{false, false}, {true, false}, {false, true}} {
		_, err := registrydb.AcknowledgeExposure(t.Context(), conn, exposureRevision, inventory.Digest, consent.approve, consent.offline, false)
		require.ErrorContains(t, err, "required")
		require.Equal(t, inventory, exposureInventory(t, conn))
	}
	for _, generation := range []int{1, 0} {
		_, err := conn.Exec(t.Context(), `UPDATE registry_sync SET lease_id=gen_random_uuid(),
 lease_generation=$1,lease_expires_at=clock_timestamp()+interval '1 hour'`, generation)
		require.NoError(t, err)
		before := exposureInventory(t, conn)
		_, err = registrydb.AcknowledgeExposure(t.Context(), conn, exposureRevision, before.Digest, true, true, false)
		require.ErrorContains(t, err, "all unexpired sync leases")
		require.Equal(t, before, exposureInventory(t, conn))
	}
	_, err := conn.Exec(t.Context(), `UPDATE registry_sync SET lease_expires_at=clock_timestamp()-interval '1 hour'`)
	require.NoError(t, err)
	before := exposureCatalog(t, conn)
	inventory = exposureInventory(t, conn)
	ack, err := registrydb.AcknowledgeExposure(t.Context(), conn, exposureRevision, inventory.Digest, true, true, false)
	require.NoError(t, err)
	require.NotEmpty(t, ack.ID)
	require.Equal(t, "acknowledgement recorded; legacy protections unchanged", ack.Message)
	require.Equal(t, inventory.Digest, ack.ReviewedDigest)
	require.Equal(t, exposureInventory(t, conn).Digest, ack.PostFenceDigest)
	require.NotEqual(t, ack.ReviewedDigest, ack.PostFenceDigest)
	require.Equal(t, before, exposureCatalog(t, conn), "all values except generation/applied baseline must survive")
	var sources, generation, applied int
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*),min(definition_generation) FROM source`).Scan(&sources, &generation))
	require.Equal(t, 3, sources)
	require.Equal(t, 2, generation)
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*) FROM registry_sync WHERE applied_generation IS NOT NULL`).Scan(&applied))
	require.Zero(t, applied)
	var reviewed, post, config string
	var acknowledgedAt time.Time
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT reviewed_digest,post_fence_digest,config_revision,acknowledged_at
 FROM exposure_acknowledgement`).Scan(&reviewed, &post, &config, &acknowledgedAt))
	require.Equal(t, inventory.Digest, reviewed)
	require.Equal(t, ack.PostFenceDigest, post)
	require.Equal(t, exposureRevision, config)
	var atDBTime bool
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT acknowledged_at BETWEEN clock_timestamp()-interval '1 minute'
 AND clock_timestamp() FROM exposure_acknowledgement`).Scan(&atDBTime))
	require.True(t, atDBTime)
	pool, err := pgxpool.New(t.Context(), conn.Config().ConnString())
	require.NoError(t, err)
	defer pool.Close()
	jobs, err := postgres.NewJobs(pool, 65536)
	require.NoError(t, err)
	job, err := jobs.Acquire(t.Context(), "linked", time.Minute)
	require.NoError(t, err)
	err = jobs.CommitSnapshot(t.Context(), job, persistence.SyncResult{Hash: "new"}, model.Snapshot{})
	require.ErrorIs(t, err, persistence.ErrConflict, "acknowledgement must not disable legacy claims guards")
}

func TestExposureAcknowledgementRollsBackFenceOnMarkerFailure(t *testing.T) {
	t.Parallel()
	conn, _ := database.SetupTestDB(t)
	seedExposure(t, conn)
	_, err := conn.Exec(t.Context(), `CREATE FUNCTION reject_exposure_marker() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN RAISE EXCEPTION 'private-sentinel'; END $$;
 CREATE TRIGGER reject_exposure_marker BEFORE UPDATE ON exposure_acknowledgement
 FOR EACH ROW EXECUTE FUNCTION reject_exposure_marker()`)
	require.NoError(t, err)
	before := exposureInventory(t, conn)
	_, err = registrydb.AcknowledgeExposure(t.Context(), conn, exposureRevision, before.Digest, true, true, false)
	require.ErrorContains(t, err, "cannot record")
	require.NotContains(t, err.Error(), "private-sentinel")
	require.Equal(t, before, exposureInventory(t, conn))
	var marker *string
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT acknowledgement_id::text FROM exposure_acknowledgement`).Scan(&marker))
	require.Nil(t, marker)
}

func TestExposureEmptyRequiresAllPersistedTablesEmpty(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"empty", "source", "view", "snapshot", "orphan-snapshot"} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			conn, _ := database.SetupTestDB(t)
			if state == "source" || state == "snapshot" {
				_, err := conn.Exec(t.Context(), `INSERT INTO source(name,source_type,source_config) VALUES('only','file','{}')`)
				require.NoError(t, err)
			}
			if state == "view" {
				_, err := conn.Exec(t.Context(), `INSERT INTO registry(name) VALUES('only')`)
				require.NoError(t, err)
			}
			if state == "snapshot" {
				_, err := conn.Exec(t.Context(), `INSERT INTO registry_sync(source_id,server_count,last_sync_hash)
 SELECT id,9,'retained' FROM source`)
				require.NoError(t, err)
			}
			if state == "orphan-snapshot" {
				_, err := conn.Exec(t.Context(), `INSERT INTO registry_sync(server_count,last_sync_hash) VALUES(9,'retained')`)
				require.NoError(t, err)
			}
			before := exposureInventory(t, conn)
			ack, err := registrydb.AcknowledgeExposure(t.Context(), conn, exposureRevision, before.Digest, false, true, true)
			if state == "empty" {
				require.NoError(t, err)
				require.Equal(t, "empty", ack.Mode)
				require.Equal(t, before.Digest, ack.PostFenceDigest)
			} else {
				require.ErrorContains(t, err, "every persisted catalog table")
				require.Equal(t, before, exposureInventory(t, conn))
			}
		})
	}
}

func TestExposureFencesExpiredPreReviewJobs(t *testing.T) {
	t.Parallel()
	conn, _ := database.SetupTestDB(t)
	pool, err := pgxpool.New(t.Context(), conn.Config().ConnString())
	require.NoError(t, err)
	defer pool.Close()
	definitions, err := postgres.NewDefinitions(pool)
	require.NoError(t, err)
	source, err := definitions.CreateSource(t.Context(), persistence.SourceDefinition{Name: "fenced", API: &persistence.APISpec{Endpoint: "https://example.org"}, Schedule: "1h"})
	require.NoError(t, err)
	jobs, err := postgres.NewJobs(pool, 65536)
	require.NoError(t, err)
	job, err := jobs.Acquire(t.Context(), source.Name, time.Minute)
	require.NoError(t, err)
	_, err = conn.Exec(t.Context(), `UPDATE registry_sync SET lease_expires_at=clock_timestamp()-interval '1 second'`)
	require.NoError(t, err)
	inventory := exposureInventory(t, conn)
	_, err = registrydb.AcknowledgeExposure(t.Context(), conn, exposureRevision, inventory.Digest, true, true, false)
	require.NoError(t, err)
	// Even if the old token's expiry is extended externally, the generation fence
	// (not a caller clock or cached expiry) rejects both completion paths.
	_, err = conn.Exec(t.Context(), `UPDATE registry_sync SET lease_expires_at=clock_timestamp()+interval '1 hour'`)
	require.NoError(t, err)
	require.ErrorIs(t, jobs.Complete(t.Context(), job, persistence.SyncResult{Hash: "old"}), persistence.ErrStale)
	require.ErrorIs(t, jobs.CommitSnapshot(t.Context(), job, persistence.SyncResult{Hash: "old"}, model.Snapshot{}), persistence.ErrStale)
}

func TestExposureConcurrentMutationWhileAcknowledgementWaits(t *testing.T) {
	t.Parallel()
	conn, _ := database.SetupTestDB(t)
	seedExposure(t, conn)
	inventory := exposureInventory(t, conn)
	other, err := pgx.ConnectConfig(t.Context(), conn.Config().Copy())
	require.NoError(t, err)
	defer other.Close(context.WithoutCancel(t.Context()))
	tx, err := conn.Begin(t.Context())
	require.NoError(t, err)
	defer tx.Rollback(context.WithoutCancel(t.Context()))
	_, err = tx.Exec(t.Context(), `UPDATE source SET claims='{}' WHERE name='linked'`)
	require.NoError(t, err)
	finished := make(chan error, 1)
	go func() {
		_, err := registrydb.AcknowledgeExposure(t.Context(), other, exposureRevision, inventory.Digest, true, true, false)
		finished <- err
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		err := tx.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM pg_stat_activity
 WHERE datname=current_database() AND pid<>pg_backend_pid() AND wait_event_type='Lock')`).Scan(&waiting)
		return err == nil && waiting
	}, 5*time.Second, 10*time.Millisecond)
	require.NoError(t, tx.Commit(t.Context()))
	require.ErrorContains(t, <-finished, "digest mismatch")
	var marker *string
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT acknowledgement_id::text FROM exposure_acknowledgement`).Scan(&marker))
	require.Nil(t, marker)
}

func TestExposureInventoryConsistentDuringConcurrentWrites(t *testing.T) {
	t.Parallel()
	conn, _ := database.SetupTestDB(t)
	seedExposure(t, conn)
	before := exposureInventory(t, conn)
	reader, err := pgx.ConnectConfig(t.Context(), conn.Config().Copy())
	require.NoError(t, err)
	defer reader.Close(context.WithoutCancel(t.Context()))
	tx, err := conn.Begin(t.Context())
	require.NoError(t, err)
	defer tx.Rollback(context.WithoutCancel(t.Context()))
	_, err = tx.Exec(t.Context(), `LOCK TABLE registry_sync IN ACCESS EXCLUSIVE MODE`)
	require.NoError(t, err)
	type result struct {
		inventory registrydb.ExposureInventory
		err       error
	}
	finished := make(chan result, 1)
	go func() {
		inventory, err := registrydb.InventoryExposure(t.Context(), reader, exposureRevision)
		finished <- result{inventory, err}
	}()
	require.Eventually(t, func() bool {
		if _, err := tx.Exec(t.Context(), `SELECT pg_stat_clear_snapshot()`); err != nil {
			return false
		}
		var waiting bool
		err := tx.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM pg_stat_activity
 WHERE datname=current_database() AND pid<>pg_backend_pid() AND wait_event_type='Lock'
 AND query LIKE '%public.registry_sync%')`).Scan(&waiting)
		return err == nil && waiting
	}, 5*time.Second, 10*time.Millisecond)
	_, err = tx.Exec(t.Context(), `UPDATE source SET claims='{}';
 UPDATE entry_version SET description='concurrent'; UPDATE registry_sync SET server_count=99`)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(t.Context()))
	read := <-finished
	require.NoError(t, read.err)
	require.Equal(t, before, read.inventory, "all table reads must belong to the same read-only snapshot")
	require.NotEqual(t, before.Digest, exposureInventory(t, conn).Digest)
}

func exposureInventory(t *testing.T, conn *pgx.Conn) registrydb.ExposureInventory {
	t.Helper()
	inventory, err := registrydb.InventoryExposure(t.Context(), conn, exposureRevision)
	require.NoError(t, err)
	return inventory
}

func exposureCatalog(t *testing.T, conn *pgx.Conn) map[string][]string {
	t.Helper()
	inventory := exposureInventory(t, conn)
	out := map[string][]string{}
	for table := range inventory.Counts {
		rows, err := conn.Query(t.Context(), "SELECT row_to_json(t)::text FROM "+pgx.Identifier{table}.Sanitize()+" t ORDER BY row_to_json(t)::text COLLATE \"C\"")
		require.NoError(t, err)
		for rows.Next() {
			var data []byte
			require.NoError(t, rows.Scan(&data))
			var value map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(data, &value))
			if table == "source" {
				delete(value, "definition_generation")
			}
			if table == "registry_sync" {
				delete(value, "applied_generation")
			}
			encoded, err := json.Marshal(value)
			require.NoError(t, err)
			out[table] = append(out[table], string(encoded))
		}
		rows.Close()
		require.NoError(t, rows.Err())
		sort.Strings(out[table])
	}
	return out
}

func seedExposure(t *testing.T, conn *pgx.Conn) {
	t.Helper()
	_, err := conn.Exec(t.Context(), `INSERT INTO source(name,source_type,source_config,filter_config,sync_schedule,creation_type,claims) VALUES
 ('linked','api','{"endpoint":"https://example.org/private-sentinel"}','{}',interval '1 hour','CONFIG','{"private":"private-sentinel"}'),
 ('unlinked','file','{"data":"private-sentinel"}','{}',interval '1 hour','API',NULL),
 ('second','git','{"repository":"https://example.org"}',NULL,interval '2 hours','CONFIG','null');
 INSERT INTO registry(name,claims) VALUES('view','{}');
 INSERT INTO registry_source(registry_id,source_id,position)
 SELECT r.id,s.id,CASE s.name WHEN 'linked' THEN 7 ELSE 3 END FROM registry r,source s WHERE s.name<>'unlinked';
 INSERT INTO registry_entry(source_id,entry_type,name,claims)
 SELECT s.id,k.kind,'entry-'||c.name,c.claims FROM source s
 CROSS JOIN (VALUES ('MCP'::entry_type),('SKILL'::entry_type),('PLUGIN'::entry_type)) k(kind)
 CROSS JOIN (VALUES ('sql',NULL::jsonb),('json','null'::jsonb),('empty','{}'::jsonb),
 ('labeled','{"team":"private-sentinel"}'::jsonb),('malformed','{"team":{"private":"private-sentinel"}}'::jsonb)) c(name,claims)
 WHERE s.name='unlinked';
 INSERT INTO entry_version(entry_id,name,version,description)
 SELECT e.id,e.name,v.version,'private-sentinel' FROM registry_entry e CROSS JOIN (VALUES ('1'),('2')) v(version);
 INSERT INTO mcp_server(version_id,server_meta) SELECT v.id,'{"private":"private-sentinel"}' FROM entry_version v
 JOIN registry_entry e ON e.id=v.entry_id WHERE e.entry_type='MCP';
 INSERT INTO mcp_server_package(server_id,registry_type,pkg_registry_url,pkg_identifier,pkg_version,transport,env_vars)
 SELECT version_id,'npm','https://example.org','package','1',t.transport,'{"private":"private-sentinel"}' FROM mcp_server
 CROSS JOIN (VALUES ('stdio'),('sse')) t(transport);
 INSERT INTO mcp_server_remote(server_id,transport,transport_url,transport_headers)
 SELECT version_id,'sse','https://example.org','{"private":"private-sentinel"}' FROM mcp_server;
 INSERT INTO mcp_server_icon(server_id,source_uri,mime_type,theme) SELECT version_id,'https://example.org','image/png','LIGHT' FROM mcp_server;
 INSERT INTO skill(version_id,namespace,extension_meta) SELECT v.id,'com.example','{"private":"private-sentinel"}' FROM entry_version v
 JOIN registry_entry e ON e.id=v.entry_id WHERE e.entry_type='SKILL';
 INSERT INTO skill_oci_package(skill_id,identifier) SELECT version_id,'private-sentinel' FROM skill;
 INSERT INTO skill_git_package(skill_id,url) SELECT version_id,'https://example.org/private-sentinel' FROM skill;
 INSERT INTO plugin(version_id,namespace,extension_meta) SELECT v.id,'com.example','{"private":"private-sentinel"}' FROM entry_version v
 JOIN registry_entry e ON e.id=v.entry_id WHERE e.entry_type='PLUGIN';
 INSERT INTO plugin_oci_package(plugin_id,identifier) SELECT version_id,'private-sentinel' FROM plugin;
 INSERT INTO plugin_git_package(plugin_id,url) SELECT version_id,'https://example.org/private-sentinel' FROM plugin;
 INSERT INTO latest_entry_version(source_id,entry_type,name,version,latest_version_id)
 SELECT e.source_id,e.entry_type,e.name,v.version,v.id FROM entry_version v JOIN registry_entry e ON e.id=v.entry_id WHERE v.version='1';
 INSERT INTO registry_sync(source_id,sync_status,last_sync_hash,last_applied_filter_hash,ended_at,server_count,skill_count,plugin_count,applied_generation)
 SELECT id,'COMPLETED','private-sentinel','filter',clock_timestamp(),5,5,5,definition_generation FROM source WHERE name='unlinked'`)
	require.NoError(t, err, fmt.Sprintf("seed all exposure tables: %v", err))
}
