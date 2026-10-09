package postgres_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	thvregistry "github.com/stacklok/toolhive-core/registry/types"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/postgres"
)

func TestJobRenewCancelsWhileWaitingForLock(t *testing.T) {
	t.Parallel()
	d, pool := newBackend(t)
	jobs, err := postgres.NewJobs(pool, 65536)
	if err != nil {
		t.Fatal(err)
	}
	s, err := d.CreateSource(t.Context(), persistence.SourceDefinition{Name: "renew-stop", API: &persistence.APISpec{Endpoint: "https://example.org"}, Schedule: "1h"})
	if err != nil {
		t.Fatal(err)
	}
	job, err := jobs.Acquire(t.Context(), s.Name, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err = tx.Exec(t.Context(), `SELECT id FROM source WHERE id=$1 FOR UPDATE`, s.ID); err != nil {
		t.Fatal(err)
	}
	workCtx, cancelWork := context.WithCancel(t.Context())
	defer cancelWork()
	renewed := make(chan error, 1)
	go func() { _, e := jobs.Renew(workCtx, job, time.Minute); renewed <- e }()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		var waiting bool
		if err = pool.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND wait_event_type='Lock')`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case e := <-renewed:
			t.Fatalf("renew did not wait: %v", e)
		case <-deadline.C:
			t.Fatal("renew did not reach lock")
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancelWork()
	select {
	case e := <-renewed:
		if !errors.Is(e, context.Canceled) {
			t.Fatalf("renew cancellation: %v", e)
		}
	case <-deadline.C:
		t.Fatal("renew did not stop before lock released")
	}
	if err = tx.Rollback(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err = jobs.Fail(t.Context(), job, persistence.FailureCancelled); err != nil {
		t.Fatalf("renew cancellation consumed lease: %v", err)
	}
}

func TestJobClaimNextDiscardsRolledBackClaim(t *testing.T) {
	t.Parallel()
	d, pool := newBackend(t)
	jobs, err := postgres.NewJobs(pool, 65536)
	if err != nil {
		t.Fatal(err)
	}
	s, err := d.CreateSource(t.Context(), persistence.SourceDefinition{Name: "retry-claim", API: &persistence.APISpec{Endpoint: "https://example.org"}, Schedule: "1h"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(t.Context(), `CREATE FUNCTION reject_job_commit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE SQLSTATE '40001'; END $$`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(t.Context(), `CREATE CONSTRAINT TRIGGER reject_job_commit AFTER INSERT OR UPDATE ON registry_sync DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION reject_job_commit()`)
	if err != nil {
		t.Fatal(err)
	}
	job, found, err := jobs.ClaimNext(t.Context(), time.Minute)
	if !errors.Is(err, persistence.ErrConflict) || found || job.LeaseID != "" || job.Source.ID != "" {
		t.Fatalf("rolled-back claim exposed: %+v %t %v", job, found, err)
	}
	status, err := jobs.Status(t.Context(), s.Name)
	if err != nil || status.Phase != persistence.JobPending || status.Attempts != 0 {
		t.Fatalf("rolled-back claim persisted: %+v %v", status, err)
	}
}

func TestJobClaimNextSkipsLockedHead(t *testing.T) {
	t.Parallel()
	d, pool := newBackend(t)
	jobs, err := postgres.NewJobs(pool, 65536)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a-locked", "b-ready"} {
		if _, err = d.CreateSource(t.Context(), persistence.SourceDefinition{Name: name, API: &persistence.APISpec{Endpoint: "https://example.org"}, Schedule: "1h"}); err != nil {
			t.Fatal(err)
		}
	}
	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err = tx.Exec(t.Context(), `SELECT id FROM source WHERE name='a-locked' FOR UPDATE`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	job, ok, err := jobs.ClaimNext(ctx, time.Minute)
	if err != nil || !ok || job.Source.Name != "b-ready" {
		t.Fatalf("locked head blocked next source: %+v %t %v", job, ok, err)
	}
	if _, ok, err = jobs.ClaimNext(ctx, time.Minute); err != nil || ok {
		t.Fatalf("locked and leased candidates: %t %v", ok, err)
	}
	if err = tx.Rollback(t.Context()); err != nil {
		t.Fatal(err)
	}
	job, ok, err = jobs.ClaimNext(ctx, time.Minute)
	if err != nil || !ok || job.Source.Name != "a-locked" {
		t.Fatalf("unlocked head lost: %+v %t %v", job, ok, err)
	}
}

func TestJobExplicitAcquireRejectsUnknownFields(t *testing.T) {
	t.Parallel()
	_, pool := newBackend(t)
	jobs, err := postgres.NewJobs(pool, 65536)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, spec, filter string }{
		{"unknown-spec", `{"endpoint":"https://example.org","legacy":true}`, `{}`},
		{"unknown-filter", `{"endpoint":"https://example.org"}`, `{"legacy":true}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, e := pool.Exec(t.Context(), `INSERT INTO source(name,source_type,source_config,filter_config,syncable,sync_schedule) VALUES($1,'api',$2::jsonb,$3::jsonb,true,interval '1 hour')`, tc.name, tc.spec, tc.filter)
			if e != nil {
				t.Fatal(e)
			}
			job, e := jobs.Acquire(t.Context(), tc.name, time.Minute)
			if !errors.Is(e, persistence.ErrConflict) || job.LeaseID != "" {
				t.Fatalf("unknown fields leased: %+v %v", job, e)
			}
			status, e := jobs.Status(t.Context(), tc.name)
			if e != nil || status.Phase != persistence.JobPending || status.Attempts != 0 || status.LastHash != "" {
				t.Fatalf("unknown fields mutated status: %+v %v", status, e)
			}
			var spec, filter string
			if e = pool.QueryRow(t.Context(), `SELECT source_config::text,filter_config::text FROM source WHERE name=$1`, tc.name).Scan(&spec, &filter); e != nil || !strings.Contains(spec+filter, `"legacy": true`) {
				t.Fatalf("unknown fields mutated definition: %s %s %v", spec, filter, e)
			}
		})
	}
}

func TestJobCompleteExpiredWhileWaitingForFence(t *testing.T) {
	t.Parallel()
	d, pool := newBackend(t)
	jobs, err := postgres.NewJobs(pool, 65536)
	if err != nil {
		t.Fatal(err)
	}
	s, err := d.CreateSource(t.Context(), persistence.SourceDefinition{Name: "expiring-ack", API: &persistence.APISpec{Endpoint: "https://example.org"}, Schedule: "1h"})
	if err != nil {
		t.Fatal(err)
	}
	job, err := jobs.Acquire(t.Context(), s.Name, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err = tx.Exec(t.Context(), `LOCK TABLE registry_entry IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() { finished <- jobs.Complete(t.Context(), job, persistence.SyncResult{}) }()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		var waiting bool
		if err = pool.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND wait_event_type='Lock')`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case e := <-finished:
			t.Fatalf("ack never waited: %v", e)
		case <-deadline.C:
			t.Fatal("ack never reached lock")
		case <-time.After(10 * time.Millisecond):
		}
	}
	<-time.After(time.Until(job.ExpiresAt) + 50*time.Millisecond)
	if err = tx.Rollback(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err = <-finished; !errors.Is(err, persistence.ErrStale) {
		t.Fatalf("expired acknowledgement: %v", err)
	}
	status, err := jobs.Status(t.Context(), s.Name)
	if err != nil || status.Phase != persistence.JobInProgress || status.LastHash != "" {
		t.Fatalf("expired acknowledgement mutated status: %+v %v", status, err)
	}
}

func TestJobCompleteRejectsLegacyClaims(t *testing.T) {
	t.Parallel()
	for _, claimed := range []string{"source before acquire", "source during lease", "entry before acquire", "entry during lease"} {
		t.Run(claimed, func(t *testing.T) {
			t.Parallel()
			d, pool := newBackend(t)
			jobs, err := postgres.NewJobs(pool, 65536)
			if err != nil {
				t.Fatal(err)
			}
			s, err := d.CreateSource(t.Context(), persistence.SourceDefinition{Name: "claims-job", API: &persistence.APISpec{Endpoint: "https://example.org"}, Schedule: "1h"})
			if err != nil {
				t.Fatal(err)
			}
			snap := model.Snapshot{}
			snap.Data.Servers = []model.Server{{Schema: "https://example.org/schema.json", Name: "com.example/server", Version: "1", Description: "server"}}
			first, err := jobs.Acquire(t.Context(), s.Name, time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			result := persistence.SyncResult{Hash: "H", FilterHash: "F"}
			if err = jobs.CommitSnapshot(t.Context(), first, result, snap); err != nil {
				t.Fatal(err)
			}
			addClaims := func() {
				var query string
				if claimed == "source before acquire" || claimed == "source during lease" {
					query = `UPDATE source SET claims='{"role":"reader"}'::jsonb WHERE id=$1`
				} else {
					query = `UPDATE registry_entry SET claims='{"role":"reader"}'::jsonb WHERE source_id=$1`
				}
				if _, e := pool.Exec(t.Context(), query, s.ID); e != nil {
					t.Fatal(e)
				}
			}
			if claimed == "source before acquire" || claimed == "entry before acquire" {
				addClaims()
			}
			j, err := jobs.Acquire(t.Context(), s.Name, time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			if claimed == "source during lease" || claimed == "entry during lease" {
				addClaims()
			}
			if err = jobs.Complete(t.Context(), j, result); !errors.Is(err, persistence.ErrConflict) {
				t.Fatalf("claimed source advanced: %v", err)
			}
			state, err := jobs.Status(t.Context(), s.Name)
			if err != nil || state.LastHash != "H" || state.Phase != persistence.JobInProgress {
				t.Fatalf("metadata: %+v %v", state, err)
			}
			if err = jobs.Fail(t.Context(), j, persistence.FailureValidation); err != nil {
				t.Fatalf("cannot end claimed attempt: %v", err)
			}
		})
	}
}

func TestJobClaimNextSkipsLegacyCandidates(t *testing.T) {
	t.Parallel()
	d, pool := newBackend(t)
	jobs, err := postgres.NewJobs(pool, 65536)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	for _, row := range []struct{ name, kind, config string }{
		{"a-inline", "file", `{"data":"{}"}`},
		{"b-unknown", "api", `{"endpoint":"https://example.org","legacy":true}`},
		{"c-invalid", "api", `{"endpoint":""}`},
		{"d-claimed", "api", `{"endpoint":"https://example.org"}`},
	} {
		_, err = pool.Exec(ctx, `INSERT INTO source(name,source_type,source_config,syncable,sync_schedule) VALUES($1,$2,$3::jsonb,true,interval '1 hour')`, row.name, row.kind, row.config)
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err = pool.Exec(ctx, `UPDATE source SET claims='{"role":"reader"}'::jsonb WHERE name='d-claimed'`); err != nil {
		t.Fatal(err)
	}
	valid, err := d.CreateSource(ctx, persistence.SourceDefinition{Name: "z-valid", API: &persistence.APISpec{Endpoint: "https://example.org"}, Schedule: "1h"})
	if err != nil {
		t.Fatal(err)
	}
	j, ok, err := jobs.ClaimNext(ctx, time.Minute)
	if err != nil || !ok || j.Source.ID != valid.ID {
		t.Fatalf("skipped valid source: %+v %t %v", j, ok, err)
	}
	if err = jobs.CommitSnapshot(ctx, j, persistence.SyncResult{Hash: "H"}, model.Snapshot{}); err != nil {
		t.Fatal(err)
	}
	if _, ok, err = jobs.ClaimNext(ctx, time.Minute); err != nil || ok {
		t.Fatalf("poisoned exhaustion: %t %v", ok, err)
	}
	var raw string
	if err = pool.QueryRow(ctx, `SELECT source_config::text FROM source WHERE name='b-unknown'`).Scan(&raw); err != nil || !strings.Contains(raw, `"legacy": true`) {
		t.Fatalf("legacy config changed: %q %v", raw, err)
	}
}

func TestJobPollingAfterCompletionBecomesDue(t *testing.T) {
	t.Parallel()
	d, pool := newBackend(t)
	jobs, err := postgres.NewJobs(pool, 65536)
	if err != nil {
		t.Fatal(err)
	}
	s, err := d.CreateSource(t.Context(), persistence.SourceDefinition{Name: "due-again", API: &persistence.APISpec{Endpoint: "https://example.org"}, Schedule: "1h"})
	if err != nil {
		t.Fatal(err)
	}
	j, ok, err := jobs.ClaimNext(t.Context(), time.Minute)
	if err != nil || !ok || j.Source.ID != s.ID {
		t.Fatalf("first poll: %+v %t %v", j, ok, err)
	}
	result := persistence.SyncResult{Hash: "H", FilterHash: "F"}
	if err = jobs.CommitSnapshot(t.Context(), j, result, model.Snapshot{}); err != nil {
		t.Fatal(err)
	}
	if _, ok, err = jobs.ClaimNext(t.Context(), time.Minute); err != nil || ok {
		t.Fatalf("early poll: %t %v", ok, err)
	}
	if _, err = pool.Exec(t.Context(), `UPDATE registry_sync SET ended_at=clock_timestamp()-interval '2 hours' WHERE source_id=$1`, s.ID); err != nil {
		t.Fatal(err)
	}
	j, ok, err = jobs.ClaimNext(t.Context(), time.Minute)
	if err != nil || !ok || j.Source.ID != s.ID {
		t.Fatalf("due poll: %+v %t %v", j, ok, err)
	}
	if err = jobs.Complete(t.Context(), j, result); err != nil {
		t.Fatalf("unchanged due fetch: %v", err)
	}
	if _, ok, err = jobs.ClaimNext(t.Context(), time.Minute); err != nil || ok {
		t.Fatalf("immediately due after complete: %t %v", ok, err)
	}
}

func TestJobStatusLegacyUnknownValues(t *testing.T) {
	t.Parallel()
	d, pool := newBackend(t)
	jobs, err := postgres.NewJobs(pool, 65536)
	if err != nil {
		t.Fatal(err)
	}
	s, err := d.CreateSource(t.Context(), persistence.SourceDefinition{Name: "legacy-status", API: &persistence.APISpec{Endpoint: "https://example.org"}, Schedule: "1h"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(t.Context(), `INSERT INTO registry_sync(source_id,sync_status,error_msg) VALUES($1,'FAILED','credentials:secret')`, s.ID); err != nil {
		t.Fatal(err)
	}
	status, err := jobs.Status(t.Context(), s.Name)
	if err != nil || status.Phase != persistence.JobFailed || status.Failure != "" || status.BaselineValid {
		t.Fatalf("unsanitized status: %+v %v", status, err)
	}
	pool.Close()
	if _, _, err = jobs.ClaimNext(t.Context(), time.Minute); !errors.Is(err, persistence.ErrUnavailable) {
		t.Fatalf("lost DB error: %v", err)
	}
}

func TestJobCancelledDuringSnapshotRollsBack(t *testing.T) {
	t.Parallel()
	d, pool := newBackend(t)
	entries, err := postgres.NewEntries(t.Context(), pool, 65536)
	if err != nil {
		t.Fatal(err)
	}
	jobs, err := postgres.NewJobs(pool, 65536)
	if err != nil {
		t.Fatal(err)
	}
	s, err := d.CreateSource(t.Context(), persistence.SourceDefinition{Name: "cancel-copy", API: &persistence.APISpec{Endpoint: "https://example.org"}, Schedule: "1h"})
	if err != nil {
		t.Fatal(err)
	}
	snap := model.Snapshot{}
	snap.Data.Servers = []model.Server{{Schema: "https://example.org/schema.json", Name: "com.example/server", Version: "1", Description: "server"}}
	first, err := jobs.Acquire(t.Context(), s.Name, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err = jobs.CommitSnapshot(t.Context(), first, persistence.SyncResult{Hash: "good"}, snap); err != nil {
		t.Fatal(err)
	}
	j, err := jobs.Acquire(t.Context(), s.Name, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	// Block the write after the previous server version is staged, then cancel.
	blocker, err := pool.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Release()
	if _, err = blocker.Exec(t.Context(), `SELECT pg_advisory_lock(910, 73)`); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = blocker.Exec(context.Background(), `SELECT pg_advisory_unlock(910,73)`) }()
	if _, err = pool.Exec(t.Context(), `CREATE FUNCTION test_cancel_skill() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_advisory_xact_lock(910,73); RETURN NEW; END $$`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(t.Context(), `CREATE TRIGGER test_cancel_skill BEFORE INSERT ON skill FOR EACH ROW EXECUTE FUNCTION test_cancel_skill()`); err != nil {
		t.Fatal(err)
	}
	snap.Data.Servers[0].Version = "2"
	snap.Data.Skills = []thvregistry.Skill{{Namespace: "com.example", Name: "skill", Version: "2", Description: "skill"}}
	writeCtx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- jobs.CommitSnapshot(writeCtx, j, persistence.SyncResult{Hash: "wrong"}, snap) }()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		var waiting bool
		if err = pool.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE locktype='advisory' AND NOT granted AND classid=910 AND objid=73)`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case e := <-done:
			t.Fatalf("write did not reach locked skill stage: %v", e)
		case <-deadline.C:
			t.Fatal("write never reached skill stage")
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	select {
	case err = <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled write: %v", err)
		}
	case <-deadline.C:
		t.Fatal("cancelled write did not return")
	}
	state, err := jobs.Status(t.Context(), s.Name)
	if err != nil || state.Phase != persistence.JobInProgress || state.LastHash != "good" || state.ServerCount != 1 || state.SkillCount != 0 {
		t.Fatalf("cancelled status: %+v %v", state, err)
	}
	entry, err := entries.GetEntry(t.Context(), s.ID, persistence.ServerKind, "com.example/server", persistence.VersionSelector{Latest: true})
	if err != nil || entry.Version != "1" {
		t.Fatalf("cancelled payload: %+v %v", entry, err)
	}
	if err = jobs.Fail(t.Context(), j, persistence.FailureCancelled); err != nil {
		t.Fatal(err)
	}
}
