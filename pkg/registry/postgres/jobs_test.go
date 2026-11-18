package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	thvregistry "github.com/stacklok/toolhive-core/registry/types"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence/conformance"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/postgres"
)

func TestJobDefinitionChangeWhileAcknowledging(t *testing.T) {
	t.Parallel()
	d, pool := newBackend(t)
	jobs, err := postgres.NewJobs(pool, 65536)
	if err != nil {
		t.Fatal(err)
	}
	s, err := d.CreateSource(t.Context(), persistence.SourceDefinition{Name: "reconfigure-wait", API: &persistence.APISpec{Endpoint: "https://example.org"}, Schedule: "1h"})
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
	_, err = tx.Exec(t.Context(), `UPDATE source SET sync_schedule=interval '2 hours' WHERE id=$1`, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() { finished <- jobs.Complete(t.Context(), job, persistence.SyncResult{Hash: "wrong"}) }()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		var waiting bool
		err = pool.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM pg_stat_activity
 WHERE datname=current_database() AND pid<>pg_backend_pid() AND wait_event_type='Lock')`).Scan(&waiting)
		if err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case e := <-finished:
			t.Fatalf("ack did not wait for source update: %v", e)
		case <-deadline.C:
			t.Fatal("ack did not reach source lock")
		case <-time.After(10 * time.Millisecond):
		}
	}
	if err = tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err = <-finished; !errors.Is(err, persistence.ErrStale) {
		t.Fatalf("old definition acknowledged: %v", err)
	}
	status, err := jobs.Status(t.Context(), s.Name)
	if err != nil || status.LastHash != "" {
		t.Fatalf("stale success: %+v %v", status, err)
	}
}

func TestJobOtherSourceProgressDuringCopy(t *testing.T) {
	t.Parallel()
	d, pool := newBackend(t)
	jobs, err := postgres.NewJobs(pool, 65536)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"blocked", "independent"} {
		_, err = d.CreateSource(t.Context(), persistence.SourceDefinition{Name: name, API: &persistence.APISpec{Endpoint: "https://example.org"}, Schedule: "1h"})
		if err != nil {
			t.Fatal(err)
		}
	}
	blocked, err := jobs.Acquire(t.Context(), "blocked", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := pool.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	if _, err = conn.Exec(t.Context(), `SELECT pg_advisory_lock(910,99)`); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock(910,99)`) }()
	_, err = pool.Exec(t.Context(), `CREATE FUNCTION test_block_plugin() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN PERFORM pg_advisory_xact_lock(910,99); RETURN NEW; END $$`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(t.Context(), `CREATE TRIGGER test_block_plugin BEFORE INSERT ON plugin FOR EACH ROW EXECUTE FUNCTION test_block_plugin()`)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := model.Snapshot{}
	snapshot.Data.Plugins = []thvregistry.Plugin{{Namespace: "com.example", Name: "item", Version: "1", Description: "item"}}
	finished := make(chan error, 1)
	go func() {
		finished <- jobs.CommitSnapshot(t.Context(), blocked, persistence.SyncResult{Hash: "applied"}, snapshot)
	}()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		var waiting bool
		err = pool.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM pg_locks
 WHERE locktype='advisory' AND NOT granted AND classid=910 AND objid=99)`).Scan(&waiting)
		if err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case <-deadline.C:
			t.Fatal("COPY did not reach blocked plugin stage")
		case <-time.After(10 * time.Millisecond):
		}
	}
	otherCtx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	other, err := jobs.Acquire(otherCtx, "independent", time.Minute)
	if err != nil {
		t.Fatalf("other source blocked by COPY: %v", err)
	}
	if err = jobs.CommitSnapshot(otherCtx, other, persistence.SyncResult{}, model.Snapshot{}); err != nil {
		t.Fatalf("other source ack blocked: %v", err)
	}
	if _, err = conn.Exec(t.Context(), `SELECT pg_advisory_unlock(910,99)`); err != nil {
		t.Fatal(err)
	}
	if err = <-finished; err != nil {
		t.Fatalf("blocked snapshot: %v", err)
	}
}

func TestJobLegacyDefinitionRevision(t *testing.T) {
	t.Parallel()
	d, pool := newBackend(t)
	jobs, err := postgres.NewJobs(pool, 65536)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	spec := persistence.SourceDefinition{Name: "config-job", API: &persistence.APISpec{Endpoint: "https://example.org"}, Schedule: "1h"}
	if err = d.Reconcile(ctx, []persistence.SourceDefinition{spec}, nil); err != nil {
		t.Fatal(err)
	}
	j, err := jobs.Acquire(ctx, spec.Name, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err = d.Reconcile(ctx, []persistence.SourceDefinition{spec}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = jobs.Renew(ctx, j, time.Minute); err != nil {
		t.Fatalf("no-op reconciliation: %v", err)
	}
	if _, err = pool.Exec(ctx, `UPDATE source SET sync_schedule=interval '2 hours' WHERE name=$1`, spec.Name); err != nil {
		t.Fatal(err)
	}
	if err = jobs.Complete(ctx, j, persistence.SyncResult{Hash: "stale"}); !errors.Is(err, persistence.ErrStale) {
		t.Fatalf("legacy update did not invalidate: %v", err)
	}
	if _, err = jobs.Acquire(ctx, spec.Name, time.Minute); err != nil {
		t.Fatalf("reclaim changed definition: %v", err)
	}
	k := persistence.SourceDefinition{Name: "k8s-revision", Kubernetes: &persistence.KubernetesSpec{Namespaces: []string{"a"}}}
	if err = d.Reconcile(ctx, []persistence.SourceDefinition{{Name: spec.Name, API: spec.API, Schedule: "2h"}, k}, nil); err != nil {
		t.Fatal(err)
	}
	var before, after int64
	if err = pool.QueryRow(ctx, `SELECT definition_generation FROM source WHERE name=$1`, k.Name).Scan(&before); err != nil {
		t.Fatal(err)
	}
	k.Kubernetes.Namespaces = []string{"b"}
	if err = d.Reconcile(ctx, []persistence.SourceDefinition{{Name: spec.Name, API: spec.API, Schedule: "2h"}, k}, nil); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT definition_generation FROM source WHERE name=$1`, k.Name).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before+1 {
		t.Fatalf("namespace revision %d -> %d", before, after)
	}
}

func TestJobsConformance(t *testing.T) {
	t.Parallel()
	conformance.RunJobs(t, func(t *testing.T) (persistence.Sources, persistence.Entries, persistence.Jobs) {
		t.Helper()
		d, pool := newBackend(t)
		entries, e := postgres.NewEntries(t.Context(), pool, 65536)
		if e != nil {
			t.Fatal(e)
		}
		jobs, e := postgres.NewJobs(pool, 65536)
		if e != nil {
			t.Fatal(e)
		}
		return d, entries, jobs
	})
	conformance.RunJobsMulti(t, func(t *testing.T) (persistence.Sources, persistence.Entries, persistence.Jobs, persistence.Jobs) {
		t.Helper()
		d, pool := newBackend(t)
		entries, e := postgres.NewEntries(t.Context(), pool, 65536)
		if e != nil {
			t.Fatal(e)
		}
		a, e := postgres.NewJobs(pool, 65536)
		if e != nil {
			t.Fatal(e)
		}
		other, e := pgxpool.New(t.Context(), pool.Config().ConnString())
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(other.Close)
		b, e := postgres.NewJobs(other, 65536)
		if e != nil {
			t.Fatal(e)
		}
		return d, entries, a, b
	})
}

// A plugin-stage failure occurs after the server and skill COPY/merge steps.
// The acknowledgement and the entire replacement must roll back together.
//
//nolint:paralleltest,tparallel // Cases install and drop the same trigger against one database.
func TestJobsLateFailureAndExpiry(t *testing.T) {
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
	s, err := d.CreateSource(t.Context(), persistence.SourceDefinition{Name: "late", API: &persistence.APISpec{Endpoint: "https://example.org"}, Schedule: "1h"})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := func(v string) model.Snapshot {
		var snap model.Snapshot
		snap.Data.Servers = []model.Server{{Schema: "https://example.org/schema.json", Name: "com.example/server", Version: v, Description: "server"}}
		snap.Data.Skills = []thvregistry.Skill{{Namespace: "com.example", Name: "skill", Version: v, Description: "skill"}}
		snap.Data.Plugins = []thvregistry.Plugin{{Namespace: "com.example", Name: "plugin", Version: v, Description: "plugin"}}
		return snap
	}
	first, err := jobs.Acquire(t.Context(), s.Name, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err = jobs.CommitSnapshot(t.Context(), first, persistence.SyncResult{Hash: "good"}, snapshot("1")); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, body string
		want       error
	}{
		{"late error", "RAISE EXCEPTION 'test failure' USING ERRCODE='P0001';", persistence.ErrUnavailable},
		{"expired during write", "PERFORM pg_sleep(0.35);", persistence.ErrStale},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, e := pool.Exec(t.Context(), "CREATE OR REPLACE FUNCTION test_job_plugin() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN "+tc.body+" RETURN NEW; END $$")
			if e != nil {
				t.Fatal(e)
			}
			_, e = pool.Exec(t.Context(), `CREATE TRIGGER test_job_plugin BEFORE INSERT ON plugin FOR EACH ROW EXECUTE FUNCTION test_job_plugin()`)
			if e != nil {
				t.Fatal(e)
			}
			lease := time.Minute
			if tc.want == persistence.ErrStale {
				lease = 100 * time.Millisecond
			}
			job, e := jobs.Acquire(t.Context(), s.Name, lease)
			if e != nil {
				t.Fatal(e)
			}
			e = jobs.CommitSnapshot(t.Context(), job, persistence.SyncResult{Hash: "bad"}, snapshot("2"))
			if !errors.Is(e, tc.want) {
				t.Fatalf("late commit: %v", e)
			}
			_, e = pool.Exec(t.Context(), `DROP TRIGGER test_job_plugin ON plugin`)
			if e != nil {
				t.Fatal(e)
			}
			state, e := jobs.Status(t.Context(), s.Name)
			if e != nil || state.LastHash != "good" || state.Phase != "IN_PROGRESS" || state.ServerCount != 1 || state.SkillCount != 1 || state.PluginCount != 1 {
				t.Fatalf("rolled-back status: %+v %v", state, e)
			}
			for _, v := range []struct {
				k persistence.EntryKind
				n string
			}{{persistence.ServerKind, "com.example/server"}, {persistence.SkillKind, "skill"}, {persistence.PluginKind, "plugin"}} {
				entry, e := entries.GetEntry(t.Context(), s.ID, v.k, v.n, persistence.VersionSelector{Latest: true})
				if e != nil || entry.Version != "1" {
					t.Fatalf("rolled-back %s: %+v %v", v.k, entry, e)
				}
			}
			if tc.want == persistence.ErrUnavailable {
				if e = jobs.Fail(t.Context(), job, persistence.FailureStorage); e != nil {
					t.Fatal(e)
				}
			}
		})
	}
}
