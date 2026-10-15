package db_test

import (
	"context"
	"encoding/json"
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

func TestExposureRetirementArchiveFidelityAndPreservation(t *testing.T) {
	t.Parallel()
	conn, _ := database.SetupTestDBAt(t, 29)
	seedExposure(t, conn)
	// Exercise managed payload extras and ownership as well as all three kinds.
	_, err := conn.Exec(t.Context(), `UPDATE source SET source_type='managed',syncable=false WHERE name='unlinked';
 UPDATE mcp_server SET upstream_meta='{"private":"private-sentinel"}';
 UPDATE skill SET provenance='{"private":"private-sentinel"}',metadata='null';
 UPDATE plugin SET metadata='{"private":"private-sentinel"}'`)
	require.NoError(t, err)
	before := retirementCatalog(t, conn)
	claims := archiveRows(t, conn, `SELECT 'source' AS row_table,id AS row_id,name,id AS source_id,name AS source_name,
 source_type AS kind,creation_type::text AS ownership,claims IS NULL AS claims_sql_null,claims FROM source
 UNION ALL SELECT 'registry',id,name,NULL,NULL,NULL,creation_type::text,claims IS NULL,claims FROM registry
 UNION ALL SELECT 'registry_entry',e.id,e.name,e.source_id,s.name,e.entry_type::text,s.creation_type::text,e.claims IS NULL,e.claims
 FROM registry_entry e JOIN source s ON s.id=e.source_id`)
	ack := retirementAck(t, conn)
	var originalState string
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT json_build_array(acknowledgement_id,acknowledgement_mode,
 reviewed_digest,post_fence_digest,config_revision,acknowledged_at)::text FROM exposure_acknowledgement`).Scan(&originalState))
	var generation int
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT sum(definition_generation) FROM source`).Scan(&generation))
	receipt, err := registrydb.RetireExposureClaims(t.Context(), conn, exposureRevision, ack.ID, ack.PostFenceDigest, true, true)
	require.NoError(t, err)
	require.Equal(t, "metadata retired; runtime cutover not yet available in this slice", receipt.Message)
	require.Equal(t, ack.ReviewedDigest, receipt.ReviewedDigest)
	require.Equal(t, ack.PostFenceDigest, receipt.PostFenceDigest)
	require.Equal(t, exposureInventory(t, conn).Digest, receipt.PostRetirementDigest)
	require.NotEqual(t, ack.PostFenceDigest, receipt.PostRetirementDigest)
	require.NotEmpty(t, receipt.RetirementID)
	require.Equal(t, claims, archiveRows(t, conn, `SELECT row_table,row_id,name,source_id,source_name,kind,ownership,
 claims_sql_null,claims FROM exposure_claims_archive`))
	require.Equal(t, before, retirementCatalog(t, conn), "only claims/generation/applied baseline may change")
	for _, table := range []string{"source", "registry", "registry_entry"} {
		var nonnull int
		require.NoError(t, conn.QueryRow(t.Context(), "SELECT count(*) FROM "+table+" WHERE claims IS NOT NULL").Scan(&nonnull))
		require.Zero(t, nonnull)
	}
	var afterGeneration, applied int
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT sum(definition_generation) FROM source`).Scan(&afterGeneration))
	require.Equal(t, generation+3, afterGeneration)
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*) FROM registry_sync WHERE applied_generation IS NOT NULL`).Scan(&applied))
	require.Zero(t, applied)
	var currentState string
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT json_build_array(acknowledgement_id,acknowledgement_mode,
 reviewed_digest,post_fence_digest,config_revision,acknowledged_at)::text FROM exposure_acknowledgement`).Scan(&currentState))
	require.Equal(t, originalState, currentState)
	var retired, dormant bool
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT retired_at BETWEEN clock_timestamp()-interval '1 minute'
 AND clock_timestamp(),consumed_at IS NULL FROM exposure_acknowledgement`).Scan(&retired, &dormant))
	require.True(t, retired)
	require.True(t, dormant)
	encoded, err := json.Marshal(exposureInventory(t, conn))
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private-sentinel")
	require.NotContains(t, string(encoded), "exposure_claims_archive")
	post := retirementState(t, conn)
	_, err = registrydb.RetireExposureClaims(t.Context(), conn, exposureRevision, ack.ID, ack.PostFenceDigest, true, true)
	require.ErrorContains(t, err, "already transitioned")
	_, err = registrydb.RetireExposureClaims(t.Context(), conn, exposureRevision, ack.ID, receipt.PostRetirementDigest, true, true)
	require.ErrorContains(t, err, "already transitioned")
	_, err = registrydb.AcknowledgeExposure(t.Context(), conn, exposureRevision, receipt.PostRetirementDigest, true, true, false)
	require.ErrorContains(t, err, "evidence cannot be replaced")
	require.Equal(t, post, retirementState(t, conn))

	// Raw persistence has no old claims left to protect, but serving is NOT activated.
	pool, err := pgxpool.New(t.Context(), conn.Config().ConnString())
	require.NoError(t, err)
	defer pool.Close()
	jobs, err := postgres.NewJobs(pool, 65536)
	require.NoError(t, err)
	_, err = jobs.Acquire(t.Context(), "linked", time.Minute)
	require.ErrorIs(t, err, persistence.ErrUnavailable, "retirement alone must not enable schema-29 writers")
	require.Equal(t, claims, archiveRows(t, conn, `SELECT row_table,row_id,name,source_id,source_name,kind,ownership,
 claims_sql_null,claims FROM exposure_claims_archive`), "live deletions cannot cascade into evidence")
}

func TestExposureRetirementRejectsInvalidEvidenceAndConsent(t *testing.T) {
	t.Parallel()
	conn, _ := database.SetupTestDBAt(t, 29)
	seedExposure(t, conn)
	before := retirementState(t, conn)
	inventory := exposureInventory(t, conn)
	_, err := registrydb.RetireExposureClaims(t.Context(), conn, exposureRevision, "not-an-ack", inventory.Digest, true, true)
	require.ErrorContains(t, err, "acknowledgement mismatch")
	require.Equal(t, before, retirementState(t, conn))
	ack := retirementAck(t, conn)
	before = retirementState(t, conn)
	for _, test := range []struct {
		name, revision, id, digest string
		approve, offline           bool
	}{
		{"no-approval", exposureRevision, ack.ID, ack.PostFenceDigest, false, true},
		{"no-offline", exposureRevision, ack.ID, ack.PostFenceDigest, true, false},
		{"no-id", exposureRevision, "", ack.PostFenceDigest, true, true},
		{"wrong-id", exposureRevision, "not-an-ack", ack.PostFenceDigest, true, true},
		{"no-digest", exposureRevision, ack.ID, "", true, true},
		{"reviewed-not-post-fence", exposureRevision, ack.ID, ack.ReviewedDigest, true, true},
		{"wrong-config", strings.Replace(exposureRevision, "aa", "bb", 1), ack.ID, ack.PostFenceDigest, true, true},
	} {
		_, err := registrydb.RetireExposureClaims(t.Context(), conn, test.revision, test.id, test.digest, test.approve, test.offline)
		require.Error(t, err, test.name)
		require.NotContains(t, err.Error(), "private-sentinel", test.name)
		require.Equal(t, before, retirementState(t, conn), test.name)
	}
	_, err = conn.Exec(t.Context(), `UPDATE entry_version SET description='changed-after-review'`)
	require.NoError(t, err)
	before = retirementState(t, conn)
	_, err = registrydb.RetireExposureClaims(t.Context(), conn, exposureRevision, ack.ID, ack.PostFenceDigest, true, true)
	require.ErrorContains(t, err, "digest mismatch")
	require.Equal(t, before, retirementState(t, conn))
}

func TestExposureRetirementRejectsEveryLiveLease(t *testing.T) {
	t.Parallel()
	conn, _ := database.SetupTestDBAt(t, 29)
	seedExposure(t, conn)
	ack := retirementAck(t, conn)
	for _, generation := range []int{2, 0} {
		_, err := conn.Exec(t.Context(), `UPDATE registry_sync SET lease_id=gen_random_uuid(),
 lease_generation=$1,lease_expires_at=clock_timestamp()+interval '1 hour'`, generation)
		require.NoError(t, err)
		before := retirementState(t, conn)
		_, err = registrydb.RetireExposureClaims(t.Context(), conn, exposureRevision, ack.ID, ack.PostFenceDigest, true, true)
		require.ErrorContains(t, err, "all unexpired sync leases")
		require.Equal(t, before, retirementState(t, conn))
	}
}

func TestExposureRetirementAtomicRollback(t *testing.T) {
	t.Parallel()
	for _, failure := range []struct{ name, table, event string }{
		{"archive", "exposure_claims_archive", "INSERT"},
		{"clear", "registry_entry", "UPDATE"},
		{"marker", "exposure_acknowledgement", "UPDATE"},
	} {
		t.Run(failure.name, func(t *testing.T) {
			t.Parallel()
			conn, _ := database.SetupTestDBAt(t, 29)
			seedExposure(t, conn)
			ack := retirementAck(t, conn)
			_, err := conn.Exec(t.Context(), `CREATE FUNCTION reject_retirement() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN RAISE EXCEPTION 'private-sentinel'; END $$;
 CREATE TRIGGER reject_retirement BEFORE `+failure.event+` ON `+failure.table+`
 FOR EACH ROW EXECUTE FUNCTION reject_retirement()`)
			require.NoError(t, err)
			before := retirementState(t, conn)
			_, err = registrydb.RetireExposureClaims(t.Context(), conn, exposureRevision, ack.ID, ack.PostFenceDigest, true, true)
			require.Error(t, err)
			require.NotContains(t, err.Error(), "private-sentinel")
			require.Equal(t, before, retirementState(t, conn))
		})
	}
}

func TestExposureRetirementEmptyMarkerNeedsNoArchive(t *testing.T) {
	t.Parallel()
	conn, _ := database.SetupTestDBAt(t, 29)
	inventory := exposureInventory(t, conn)
	ack, err := registrydb.AcknowledgeExposure(t.Context(), conn, exposureRevision, inventory.Digest, false, true, true)
	require.NoError(t, err)
	before := retirementState(t, conn)
	_, err = registrydb.RetireExposureClaims(t.Context(), conn, exposureRevision, ack.ID, ack.PostFenceDigest, true, true)
	require.ErrorContains(t, err, "whole-catalog acknowledgement mismatch")
	require.Equal(t, before, retirementState(t, conn))
}

func TestExposureRetirementFencesExpiredJobs(t *testing.T) {
	t.Parallel()
	conn, _ := database.SetupTestDBAt(t, 29)
	pool, err := pgxpool.New(t.Context(), conn.Config().ConnString())
	require.NoError(t, err)
	defer pool.Close()
	source := historicalSource(t, conn, "fenced")
	jobs, err := postgres.NewJobs(pool, 65536)
	require.NoError(t, err)
	ack := retirementAck(t, conn)
	job := historicalJob(t, conn, source)
	_, err = conn.Exec(t.Context(), `UPDATE registry_sync SET lease_expires_at=clock_timestamp()-interval '1 second'`)
	require.NoError(t, err)
	// The expired lease write invalidates review; fresh review never rebinds the old ack.
	_, err = registrydb.RetireExposureClaims(t.Context(), conn, exposureRevision, ack.ID, ack.PostFenceDigest, true, true)
	require.ErrorContains(t, err, "digest mismatch")
	ack = retirementAck(t, conn)
	_, err = registrydb.RetireExposureClaims(t.Context(), conn, exposureRevision, ack.ID, ack.PostFenceDigest, true, true)
	require.NoError(t, err)
	_, err = conn.Exec(t.Context(), `UPDATE registry_sync SET lease_expires_at=clock_timestamp()+interval '1 hour'`)
	require.NoError(t, err)
	require.ErrorIs(t, jobs.Complete(t.Context(), job, persistence.SyncResult{Hash: "old"}), persistence.ErrUnavailable)
	require.ErrorIs(t, jobs.CommitSnapshot(t.Context(), job, persistence.SyncResult{Hash: "old"}, model.Snapshot{}), persistence.ErrUnavailable)
}

func TestExposureRetirementConcurrentMutation(t *testing.T) {
	t.Parallel()
	conn, _ := database.SetupTestDBAt(t, 29)
	seedExposure(t, conn)
	ack := retirementAck(t, conn)
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
		_, err := registrydb.RetireExposureClaims(t.Context(), other, exposureRevision, ack.ID, ack.PostFenceDigest, true, true)
		finished <- err
	}()
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

func TestExposureRetirementSerializesConcurrentAttempts(t *testing.T) {
	t.Parallel()
	conn, _ := database.SetupTestDBAt(t, 29)
	seedExposure(t, conn)
	ack := retirementAck(t, conn)
	first, err := pgx.ConnectConfig(t.Context(), conn.Config().Copy())
	require.NoError(t, err)
	defer first.Close(context.WithoutCancel(t.Context()))
	second, err := pgx.ConnectConfig(t.Context(), conn.Config().Copy())
	require.NoError(t, err)
	defer second.Close(context.WithoutCancel(t.Context()))
	start := make(chan struct{})
	finished := make(chan error, 2)
	for _, worker := range []*pgx.Conn{first, second} {
		go func() {
			<-start
			_, err := registrydb.RetireExposureClaims(t.Context(), worker, exposureRevision, ack.ID, ack.PostFenceDigest, true, true)
			finished <- err
		}()
	}
	close(start)
	results := []error{<-finished, <-finished}
	if results[0] != nil {
		results[0], results[1] = results[1], results[0]
	}
	require.NoError(t, results[0])
	require.ErrorContains(t, results[1], "already transitioned")
	var operations, rows int
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(DISTINCT retirement_id),count(*) FROM exposure_claims_archive`).Scan(&operations, &rows))
	require.Equal(t, 1, operations)
	require.Equal(t, 19, rows)
}

func retirementAck(t *testing.T, conn *pgx.Conn) registrydb.ExposureAcknowledgement {
	t.Helper()
	inventory := exposureInventory(t, conn)
	ack, err := registrydb.AcknowledgeExposure(t.Context(), conn, exposureRevision, inventory.Digest, true, true, false)
	require.NoError(t, err)
	return ack
}

func archiveRows(t *testing.T, conn *pgx.Conn, query string) []string {
	t.Helper()
	rows, err := conn.Query(t.Context(), "SELECT row_to_json(t)::text FROM ("+query+") t")
	require.NoError(t, err)
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var data string
		require.NoError(t, rows.Scan(&data))
		out = append(out, data)
	}
	require.NoError(t, rows.Err())
	sort.Strings(out)
	return out
}

func retirementCatalog(t *testing.T, conn *pgx.Conn) map[string][]string {
	t.Helper()
	catalog := exposureCatalog(t, conn)
	for _, table := range []string{"source", "registry", "registry_entry"} {
		for i, row := range catalog[table] {
			var value map[string]json.RawMessage
			require.NoError(t, json.Unmarshal([]byte(row), &value))
			delete(value, "claims")
			data, err := json.Marshal(value)
			require.NoError(t, err)
			catalog[table][i] = string(data)
		}
		sort.Strings(catalog[table])
	}
	return catalog
}

func retirementState(t *testing.T, conn *pgx.Conn) map[string][]string {
	t.Helper()
	catalog := exposureCatalog(t, conn)
	// Unlike exposureCatalog, include the exact generations/baselines for rollback.
	for _, table := range []string{"source", "registry_sync", "exposure_acknowledgement", "exposure_claims_archive"} {
		catalog[table] = archiveRows(t, conn, "SELECT * FROM "+table)
	}
	return catalog
}
