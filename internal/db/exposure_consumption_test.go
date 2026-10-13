package db_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/database"
	"github.com/stacklok/toolhive-registry-server/internal/authorization"
	"github.com/stacklok/toolhive-registry-server/internal/config"
	registrydb "github.com/stacklok/toolhive-registry-server/internal/db"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/postgres"
)

func TestExposureConsumptionPreservesEvidenceAndCatalog(t *testing.T) {
	t.Parallel()
	conn, _ := database.SetupTestDBAt(t, 29)
	seedExposure(t, conn)
	prepareConsumption(t, conn)
	before := exposureCatalog(t, conn)
	evidence := pendingExposureEvidence(t, conn)
	archive := archiveRows(t, conn, "SELECT * FROM exposure_claims_archive")
	var generation int
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT sum(definition_generation) FROM source`).Scan(&generation))
	require.NoError(t, registrydb.ConsumeExposure(t.Context(), conn, exposureRevision))
	require.Equal(t, before, exposureCatalog(t, conn), "only generations and applied baselines may change")
	require.Equal(t, evidence, pendingExposureEvidence(t, conn))
	require.Equal(t, archive, archiveRows(t, conn, "SELECT * FROM exposure_claims_archive"))
	var currentGeneration, applied int
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT sum(definition_generation) FROM source`).Scan(&currentGeneration))
	require.Equal(t, generation+3, currentGeneration)
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*) FROM registry_sync WHERE applied_generation IS NOT NULL`).Scan(&applied))
	require.Zero(t, applied)
	var databaseID, databaseName, target string
	var dbTime bool
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT database_id::text,current_database(),consumed_target_digest,
 consumed_at BETWEEN clock_timestamp()-interval '1 minute' AND clock_timestamp() FROM exposure_acknowledgement`).Scan(
		&databaseID, &databaseName, &target, &dbTime))
	require.True(t, dbTime)
	encoded, err := json.Marshal([]any{"registry-exposure-consumed-target", 1, databaseID,
		conn.Config().Host, conn.Config().Port, databaseName})
	require.NoError(t, err)
	require.Equal(t, fmt.Sprintf("sha256:%x", sha256.Sum256(encoded)), target)
	consumed := retirementState(t, conn)
	require.NoError(t, registrydb.ConsumeExposure(t.Context(), conn, exposureRevision))
	require.Equal(t, consumed, retirementState(t, conn), "restart must not fence or rewrite the receipt")
	_, err = registrydb.AcknowledgeExposure(t.Context(), conn, exposureRevision, exposureInventory(t, conn).Digest, true, true, false)
	require.ErrorContains(t, err, "evidence cannot be replaced")
}

func TestExposureConsumptionRestartAcceptsLegitimateChanges(t *testing.T) {
	t.Parallel()
	conn, _ := database.SetupTestDBAt(t, 29)
	seedExposure(t, conn)
	prepareConsumption(t, conn)
	require.NoError(t, registrydb.ConsumeExposure(t.Context(), conn, exposureRevision))
	evidence := pendingExposureEvidence(t, conn)
	_, err := conn.Exec(t.Context(), `UPDATE entry_version SET description='normal catalog edit';
 UPDATE source SET definition_generation=definition_generation+1;
 UPDATE registry_sync SET lease_id=gen_random_uuid(),lease_generation=0,
 lease_expires_at=clock_timestamp()+interval '1 hour'`)
	require.NoError(t, err)
	before := retirementState(t, conn)
	changedConfigAndPolicy := strings.Replace(exposureRevision, "aa", "bb", 1)
	require.NoError(t, registrydb.ConsumeExposure(t.Context(), conn, changedConfigAndPolicy))
	require.Equal(t, before, retirementState(t, conn))
	require.Equal(t, evidence, pendingExposureEvidence(t, conn), "historical decision must not be rebound")
}

func TestExposureConsumptionRejectsMissingAndStaleEvidence(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"absent", "pending-labeled", "pending-unlabeled", "stale-config", "stale-policy",
		"stale-metadata", "stale-payload", "wrong-target", "dirty", "schema-28", "invalid-revision", "missing-row"} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			conn, _ := database.SetupTestDBAt(t, 29)
			seedExposure(t, conn)
			revision := exposureRevision
			switch state {
			case "absent":
			case "pending-labeled":
				retirementAck(t, conn)
			case "pending-unlabeled":
				_, err := conn.Exec(t.Context(), `UPDATE source SET claims=NULL; UPDATE registry SET claims=NULL;
 UPDATE registry_entry SET claims=NULL`)
				require.NoError(t, err)
				retirementAck(t, conn)
			default:
				prepareConsumption(t, conn)
			}
			var mutation string
			switch state {
			case "stale-config", "stale-policy":
				revision = strings.Replace(exposureRevision, "aa", "bb", 1)
			case "stale-metadata":
				mutation = `UPDATE source SET claims='{}'`
			case "stale-payload":
				mutation = `UPDATE entry_version SET description='stale'`
			case "wrong-target":
				mutation = `UPDATE exposure_acknowledgement SET database_id=gen_random_uuid()`
			case "dirty":
				mutation = `UPDATE schema_migrations SET dirty=true`
			case "schema-28":
				mutation = `UPDATE schema_migrations SET version=28`
			case "invalid-revision":
				revision = ""
			case "missing-row":
				mutation = `DELETE FROM exposure_acknowledgement`
			}
			if mutation != "" {
				_, err := conn.Exec(t.Context(), mutation)
				require.NoError(t, err)
			}
			// Do not use inventory here: dirty/missing schemas must fail that too.
			before := consumptionState(t, conn)
			err := registrydb.ConsumeExposure(t.Context(), conn, revision)
			require.Error(t, err)
			require.NotContains(t, err.Error(), "private-sentinel")
			require.Equal(t, before, consumptionState(t, conn))
		})
	}
}

func TestExposureConsumptionEmptyModeRequiresAllTablesEmpty(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"empty", "source", "view", "orphan-snapshot"} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			conn, _ := database.SetupTestDBAt(t, 29)
			inventory := exposureInventory(t, conn)
			_, err := registrydb.AcknowledgeExposure(t.Context(), conn, exposureRevision, inventory.Digest, false, true, true)
			require.NoError(t, err)
			var mutation string
			switch state {
			case "source":
				mutation = `INSERT INTO source(name,source_type) VALUES('unlinked','file')`
			case "view":
				mutation = `INSERT INTO registry(name) VALUES('unlinked')`
			case "orphan-snapshot":
				mutation = `INSERT INTO registry_sync(server_count,last_sync_hash) VALUES(9,'retained')`
			}
			if mutation != "" {
				_, err := conn.Exec(t.Context(), mutation)
				require.NoError(t, err)
			}
			before := retirementState(t, conn)
			err = registrydb.ConsumeExposure(t.Context(), conn, exposureRevision)
			if state == "empty" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, "every persisted catalog table")
				require.Equal(t, before, retirementState(t, conn))
			}
		})
	}
}

func TestExposureConsumptionRejectsEveryLiveLease(t *testing.T) {
	t.Parallel()
	conn, _ := database.SetupTestDBAt(t, 29)
	seedExposure(t, conn)
	prepareConsumption(t, conn)
	for _, generation := range []int{3, 0} {
		_, err := conn.Exec(t.Context(), `UPDATE registry_sync SET lease_id=gen_random_uuid(),
 lease_generation=$1,lease_expires_at=clock_timestamp()+interval '1 hour'`, generation)
		require.NoError(t, err)
		before := retirementState(t, conn)
		require.ErrorContains(t, registrydb.ConsumeExposure(t.Context(), conn, exposureRevision), "all unexpired sync leases")
		require.Equal(t, before, retirementState(t, conn))
	}
}

func TestExposureConsumptionTargetContinuity(t *testing.T) {
	t.Parallel()
	conn, _ := database.SetupTestDBAt(t, 29)
	inventory := exposureInventory(t, conn)
	_, err := registrydb.AcknowledgeExposure(t.Context(), conn, exposureRevision, inventory.Digest, false, true, true)
	require.NoError(t, err)
	require.NoError(t, registrydb.ConsumeExposure(t.Context(), conn, exposureRevision))
	otherConfig := conn.Config().Copy()
	if otherConfig.Host == "localhost" {
		otherConfig.Host = "127.0.0.1"
	} else {
		otherConfig.Host = "localhost"
	}
	other, err := pgx.ConnectConfig(t.Context(), otherConfig)
	require.NoError(t, err)
	defer other.Close(context.WithoutCancel(t.Context()))
	before := retirementState(t, conn)
	require.ErrorContains(t, registrydb.ConsumeExposure(t.Context(), other, exposureRevision), "consumed exposure target mismatch")
	require.Equal(t, before, retirementState(t, conn))
	_, err = conn.Exec(t.Context(), `UPDATE exposure_acknowledgement SET database_id=gen_random_uuid()`)
	require.NoError(t, err)
	before = retirementState(t, conn)
	require.ErrorContains(t, registrydb.ConsumeExposure(t.Context(), conn, exposureRevision), "consumed exposure target mismatch")
	require.Equal(t, before, retirementState(t, conn))
}

func TestExposureConsumptionRollbackAndSerializationRetry(t *testing.T) {
	t.Parallel()
	for _, failure := range []struct{ name, table, timing, statement string }{
		{"fence", "source", "BEFORE", "RAISE EXCEPTION 'private-sentinel';"},
		{"baseline", "registry_sync", "BEFORE", "RAISE EXCEPTION 'private-sentinel';"},
		{"receipt", "exposure_acknowledgement", "BEFORE", "RAISE EXCEPTION 'private-sentinel';"},
		{"exhausted-retries", "exposure_acknowledgement", "BEFORE", "RAISE SQLSTATE '40001';"},
		{"retry-once", "exposure_acknowledgement", "BEFORE", "IF nextval('consume_attempt')=1 THEN RAISE SQLSTATE '40001'; END IF;"},
		{"retry-commit", "exposure_acknowledgement", "AFTER", "IF nextval('consume_attempt')=1 THEN RAISE SQLSTATE '40001'; END IF;"},
	} {
		t.Run(failure.name, func(t *testing.T) {
			t.Parallel()
			conn, _ := database.SetupTestDBAt(t, 29)
			seedExposure(t, conn)
			prepareConsumption(t, conn)
			trigger := "CREATE TRIGGER reject_consumption " + failure.timing + " UPDATE ON " + failure.table
			if failure.timing == "AFTER" {
				trigger = "CREATE CONSTRAINT TRIGGER reject_consumption AFTER UPDATE ON " + failure.table + " DEFERRABLE INITIALLY DEFERRED"
			}
			_, err := conn.Exec(t.Context(), `CREATE SEQUENCE consume_attempt;
 CREATE FUNCTION reject_consumption() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN `+failure.statement+` RETURN NEW; END $$;
 `+trigger+` FOR EACH ROW EXECUTE FUNCTION reject_consumption()`)
			require.NoError(t, err)
			before := retirementState(t, conn)
			err = registrydb.ConsumeExposure(t.Context(), conn, exposureRevision)
			if strings.HasPrefix(failure.name, "retry-") {
				require.NoError(t, err)
				var minGeneration int
				require.NoError(t, conn.QueryRow(t.Context(), `SELECT min(definition_generation) FROM source`).Scan(&minGeneration))
				require.Equal(t, 4, minGeneration, "rolled-back fences must not persist")
			} else {
				require.Error(t, err)
				require.NotContains(t, err.Error(), "private-sentinel")
				require.Equal(t, before, retirementState(t, conn))
			}
		})
	}
}

func TestExposureConsumptionSerializesConcurrentStarts(t *testing.T) {
	t.Parallel()
	conn, _ := database.SetupTestDBAt(t, 29)
	seedExposure(t, conn)
	prepareConsumption(t, conn)
	evidence := pendingExposureEvidence(t, conn)
	start := make(chan struct{})
	finished := make(chan error, 4)
	for range 4 {
		worker, err := pgx.ConnectConfig(t.Context(), conn.Config().Copy())
		require.NoError(t, err)
		defer worker.Close(context.WithoutCancel(t.Context()))
		go func() {
			<-start
			finished <- registrydb.ConsumeExposure(t.Context(), worker, exposureRevision)
		}()
	}
	close(start)
	for range 4 {
		require.NoError(t, <-finished)
	}
	var minGeneration int
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT min(definition_generation) FROM source`).Scan(&minGeneration))
	require.Equal(t, 4, minGeneration, "one consume, not one fence per process")
	require.Equal(t, evidence, pendingExposureEvidence(t, conn))
}

func TestExposureConsumptionWaitsThenRevalidatesCatalog(t *testing.T) {
	t.Parallel()
	conn, _ := database.SetupTestDBAt(t, 29)
	seedExposure(t, conn)
	prepareConsumption(t, conn)
	other, err := pgx.ConnectConfig(t.Context(), conn.Config().Copy())
	require.NoError(t, err)
	defer other.Close(context.WithoutCancel(t.Context()))
	tx, err := conn.Begin(t.Context())
	require.NoError(t, err)
	defer tx.Rollback(context.WithoutCancel(t.Context()))
	_, err = tx.Exec(t.Context(), `UPDATE entry_version SET description='concurrent mutation'`)
	require.NoError(t, err)
	finished := make(chan error, 1)
	go func() { finished <- registrydb.ConsumeExposure(t.Context(), other, exposureRevision) }()
	require.Eventually(t, func() bool {
		var waiting bool
		err := tx.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM pg_stat_activity
 WHERE datname=current_database() AND pid<>pg_backend_pid() AND wait_event_type='Lock')`).Scan(&waiting)
		return err == nil && waiting
	}, 5*time.Second, 10*time.Millisecond)
	require.NoError(t, tx.Commit(t.Context()))
	before := retirementState(t, conn)
	require.ErrorContains(t, <-finished, "digest mismatch")
	require.Equal(t, before, retirementState(t, conn))
}

func TestExposureConsumptionFencesOldWorkAndRequiresNewBaseline(t *testing.T) {
	t.Parallel()
	conn, _ := database.SetupTestDBAt(t, 29)
	pool, err := pgxpool.New(t.Context(), conn.Config().ConnString())
	require.NoError(t, err)
	defer pool.Close()
	source := historicalSource(t, conn, "fenced")
	jobs, err := postgres.NewJobs(pool, 65536)
	require.NoError(t, err)
	result := persistence.SyncResult{Hash: "same-hash", FilterHash: "same-filter"}
	old := historicalJob(t, conn, source)
	_, err = conn.Exec(t.Context(), `UPDATE registry_sync SET last_sync_hash=$1,last_applied_filter_hash=$2,applied_generation=lease_generation`, result.Hash, result.FilterHash)
	require.NoError(t, err)
	_, err = conn.Exec(t.Context(), `UPDATE registry_sync SET lease_expires_at=clock_timestamp()-interval '1 second'`)
	require.NoError(t, err)
	prepareConsumption(t, conn)
	require.NoError(t, registrydb.ConsumeExposure(t.Context(), conn, exposureRevision))
	require.NoError(t, database.MigrateUp(t.Context(), conn))
	_, err = conn.Exec(t.Context(), `UPDATE registry_sync SET lease_expires_at=clock_timestamp()+interval '1 hour'`)
	require.NoError(t, err)
	require.ErrorIs(t, jobs.Complete(t.Context(), old, result), persistence.ErrStale)
	require.ErrorIs(t, jobs.CommitSnapshot(t.Context(), old, result, model.Snapshot{}), persistence.ErrStale)
	_, err = conn.Exec(t.Context(), `UPDATE registry_sync SET lease_expires_at=clock_timestamp()-interval '1 second'`)
	require.NoError(t, err)
	current, err := jobs.Acquire(t.Context(), source.Name, time.Minute)
	require.NoError(t, err)
	require.ErrorIs(t, jobs.Complete(t.Context(), current, result), persistence.ErrConflict,
		"matching hashes do not authorize skipping the new baseline")
	require.NoError(t, jobs.CommitSnapshot(t.Context(), current, result, model.Snapshot{}))
}

func TestExposureConsumptionBindsActualLoadedPolicy(t *testing.T) {
	t.Parallel()
	for _, timing := range []string{"before-load", "after-load"} {
		t.Run(timing, func(t *testing.T) {
			t.Parallel()
			conn, _ := database.SetupTestDBAt(t, 29)
			seedExposure(t, conn)
			dir := t.TempDir()
			policy := filepath.Join(dir, "policy.cedar")
			path := filepath.Join(dir, "config.yaml")
			require.NoError(t, os.WriteFile(policy, []byte(`permit(principal is Registry::Principal,
 action == Registry::Action::"catalog.read", resource == Registry::Instance::"catalog");`), 0600))
			require.NoError(t, os.WriteFile(path, []byte(fmt.Sprintf(`database:
 host: localhost
 port: 5432
 user: registry
 database: registry
sources:
 - name: future
   managed: {}
registries:
 - name: view
   sources: [future]
auth:
 mode: anonymous
authorization:
 policyFile: %s
 allowAnonymousCatalog: true
`, policy)), 0600))
			_, offline, err := config.LoadExposureConfig(path)
			require.NoError(t, err)
			inventory, err := registrydb.InventoryExposure(t.Context(), conn, offline.Revision)
			require.NoError(t, err)
			ack, err := registrydb.AcknowledgeExposure(t.Context(), conn, offline.Revision, inventory.Digest, true, true, false)
			require.NoError(t, err)
			_, err = registrydb.RetireExposureClaims(t.Context(), conn, offline.Revision, ack.ID, ack.PostFenceDigest, true, true)
			require.NoError(t, err)
			cfg, err := config.LoadConfig(config.WithConfigPath(path))
			require.NoError(t, err)
			if timing == "before-load" {
				require.NoError(t, os.WriteFile(policy, []byte(`forbid(principal, action, resource);`), 0600))
			}
			engine, err := authorization.Load(policy)
			require.NoError(t, err)
			if timing == "after-load" {
				require.NoError(t, os.WriteFile(policy, []byte(`forbid(principal, action, resource);`), 0600))
			}
			loaded, err := cfg.ExposureReview(engine.Revision())
			require.NoError(t, err)
			before := consumptionState(t, conn)
			err = registrydb.ConsumeExposure(t.Context(), conn, loaded.Revision)
			if timing == "before-load" {
				require.ErrorContains(t, err, "config/policy revision mismatch")
				require.Equal(t, before, consumptionState(t, conn))
			} else {
				require.NoError(t, err, "bind the running evaluator, not later file bytes")
				require.Equal(t, offline, loaded)
			}
		})
	}
}

func prepareConsumption(t *testing.T, conn *pgx.Conn) {
	t.Helper()
	ack := retirementAck(t, conn)
	_, err := registrydb.RetireExposureClaims(t.Context(), conn, exposureRevision, ack.ID, ack.PostFenceDigest, true, true)
	require.NoError(t, err)
}

func pendingExposureEvidence(t *testing.T, conn *pgx.Conn) string {
	t.Helper()
	var evidence string
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT json_build_array(database_id,inventory_version,
 acknowledgement_id,acknowledgement_mode,reviewed_digest,post_fence_digest,config_revision,acknowledged_at,
 retirement_id,post_retirement_digest,retired_at)::text FROM exposure_acknowledgement`).Scan(&evidence))
	return evidence
}

func consumptionState(t *testing.T, conn *pgx.Conn) map[string][]string {
	t.Helper()
	state := map[string][]string{}
	for _, table := range []string{"source", "registry_sync", "registry", "registry_entry", "entry_version",
		"exposure_acknowledgement", "exposure_claims_archive", "schema_migrations"} {
		state[table] = archiveRows(t, conn, "SELECT * FROM "+table)
	}
	return state
}
