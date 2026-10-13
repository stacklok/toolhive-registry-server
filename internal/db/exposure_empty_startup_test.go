package db_test

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/database"
	registrydb "github.com/stacklok/toolhive-registry-server/internal/db"
)

func TestEmptyBootstrapFailsClosed(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"source-null", "source-json-null", "source-empty-claims", "source-malformed", "view", "snapshot", "live-lease", "pending-existing", "retired-existing", "pending-changed-config", "pending-changed-target", "pending-written", "consumed-other-target"} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			conn, _ := database.SetupTestDB(t)
			revision := exposureRevision
			var mutation string
			switch state {
			case "source-null":
				mutation = `INSERT INTO source(name,source_type) VALUES('unlinked','managed')`
			case "source-json-null":
				mutation = `INSERT INTO source(name,source_type,claims) VALUES('unlinked','managed','null')`
			case "source-empty-claims":
				mutation = `INSERT INTO source(name,source_type,claims) VALUES('unlinked','managed','{}')`
			case "source-malformed":
				mutation = `INSERT INTO source(name,source_type,claims) VALUES('unlinked','managed','[17]')`
			case "view":
				mutation = `INSERT INTO registry(name) VALUES('unlinked')`
			case "snapshot":
				mutation = `INSERT INTO registry_sync(server_count,last_sync_hash) VALUES(9,'retained')`
			case "live-lease":
				mutation = `INSERT INTO registry_sync(lease_id,lease_generation,lease_expires_at) VALUES(gen_random_uuid(),0,clock_timestamp()+interval '1 hour')`
			case "pending-existing", "retired-existing":
				seedExposure(t, conn)
				if state == "retired-existing" {
					prepareConsumption(t, conn)
				} else {
					retirementAck(t, conn)
				}
			case "consumed-other-target":
				require.NoError(t, registrydb.ConsumeEmptyExposure(t.Context(), conn, revision))
				mutation = `UPDATE exposure_acknowledgement SET database_id=gen_random_uuid()`
			default:
				inventory := exposureInventory(t, conn)
				_, err := registrydb.AcknowledgeExposure(t.Context(), conn, revision, inventory.Digest, false, true, true)
				require.NoError(t, err)
				switch state {
				case "pending-changed-config":
					revision = strings.Replace(revision, "aa", "bb", 1)
				case "pending-changed-target":
					mutation = `UPDATE exposure_acknowledgement SET database_id=gen_random_uuid()`
				case "pending-written":
					mutation = `INSERT INTO registry_sync(server_count) VALUES(9)`
				}
			}
			if mutation != "" {
				_, err := conn.Exec(t.Context(), mutation)
				require.NoError(t, err)
			}
			before := consumptionState(t, conn)
			require.Error(t, registrydb.ConsumeEmptyExposure(t.Context(), conn, revision))
			require.Equal(t, before, consumptionState(t, conn), "failure must not acknowledge, reset or rebind")
		})
	}
}

func TestEmptyBootstrapReusesPendingEmptyAndConsumedEvidence(t *testing.T) {
	t.Parallel()
	conn, _ := database.SetupTestDB(t)
	inventory := exposureInventory(t, conn)
	_, err := registrydb.AcknowledgeExposure(t.Context(), conn, exposureRevision, inventory.Digest, false, true, true)
	require.NoError(t, err)
	pending := pendingExposureEvidence(t, conn)
	require.NoError(t, registrydb.ConsumeEmptyExposure(t.Context(), conn, exposureRevision))
	require.Equal(t, pending, pendingExposureEvidence(t, conn))
	_, err = conn.Exec(t.Context(), `INSERT INTO source(name,source_type) VALUES('normal-edit','managed')`)
	require.NoError(t, err)
	before := consumptionState(t, conn)
	require.NoError(t, registrydb.ConsumeEmptyExposure(t.Context(), conn, strings.Replace(exposureRevision, "aa", "bb", 1)))
	require.Equal(t, before, consumptionState(t, conn), "restart accepts legitimate config/policy/catalog edits without rewriting evidence")
}

func TestEmptyBootstrapConcurrentPodsReconcileBeforeOtherConsumption(t *testing.T) {
	t.Parallel()
	conn, _ := database.SetupTestDB(t)
	start := make(chan struct{})
	finished := make(chan error, 4)
	for range 4 {
		worker, err := pgx.ConnectConfig(t.Context(), conn.Config().Copy())
		require.NoError(t, err)
		go func() {
			defer worker.Close(context.WithoutCancel(t.Context()))
			<-start
			err := registrydb.ConsumeEmptyExposure(t.Context(), worker, exposureRevision)
			if err == nil {
				_, err = worker.Exec(t.Context(), `INSERT INTO source(name,source_type) VALUES('reconciled','managed') ON CONFLICT DO NOTHING`)
			}
			finished <- err
		}()
	}
	close(start)
	for range 4 {
		require.NoError(t, <-finished)
	}
	var count int
	var mode string
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*) FROM source`).Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT acknowledgement_mode FROM exposure_acknowledgement WHERE consumed_at IS NOT NULL`).Scan(&mode))
	require.Equal(t, "empty", mode)
	before := consumptionState(t, conn)
	require.NoError(t, registrydb.ConsumeEmptyExposure(t.Context(), conn, exposureRevision))
	require.Equal(t, before, consumptionState(t, conn))
}

func TestEmptyBootstrapAcknowledgementAndConsumptionRollbackTogether(t *testing.T) {
	t.Parallel()
	conn, _ := database.SetupTestDB(t)
	_, err := conn.Exec(t.Context(), `CREATE FUNCTION reject_empty_consumption() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN IF NEW.consumed_at IS NOT NULL THEN RAISE EXCEPTION 'private-sentinel'; END IF; RETURN NEW; END $$;
 CREATE TRIGGER reject_empty_consumption BEFORE UPDATE ON exposure_acknowledgement
 FOR EACH ROW EXECUTE FUNCTION reject_empty_consumption()`)
	require.NoError(t, err)
	before := consumptionState(t, conn)
	err = registrydb.ConsumeEmptyExposure(t.Context(), conn, exposureRevision)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "private-sentinel")
	require.Equal(t, before, consumptionState(t, conn), "empty acknowledgement must not survive failed first consumption")
}
