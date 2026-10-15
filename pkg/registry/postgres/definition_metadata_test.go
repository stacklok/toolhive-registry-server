package postgres_test

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/postgres"
)

func checkUnrefinedConflict(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, persistence.ErrConflict) {
		t.Fatal("missing conflict category", err)
	}
	for _, category := range []error{persistence.ErrAlreadyExists, persistence.ErrConfigOwned, persistence.ErrKindChange} {
		if errors.Is(err, category) {
			t.Fatal("legacy guard misclassified", err)
		}
	}
}

func TestHistoricalDefinitionTimestamps(t *testing.T) {
	t.Parallel()
	d, pool := newBackend(t)
	ctx := t.Context()
	_, err := pool.Exec(ctx, `INSERT INTO source(name,creation_type,source_type,source_config,created_at,updated_at,syncable)
	VALUES('historical','API','file','{"data":"{}"}',NULL,NULL,false);
	INSERT INTO registry(name,creation_type,created_at,updated_at) VALUES('historical','API',NULL,NULL);
	INSERT INTO registry_source(registry_id,source_id,position)
	SELECT r.id,s.id,0 FROM registry r,source s WHERE r.name='historical' AND s.name='historical'`)
	if err != nil {
		t.Fatal(err)
	}
	s, err := d.GetSource(ctx, "historical")
	if err != nil {
		t.Fatal(err)
	}
	v, err := d.GetView(ctx, "historical")
	if err != nil {
		t.Fatal(err)
	}
	if !s.CreatedAt.IsZero() || !s.UpdatedAt.IsZero() || !v.CreatedAt.IsZero() || !v.UpdatedAt.IsZero() {
		t.Fatal("invented historical dates")
	}
	ss, err := d.ListSources(ctx)
	if err != nil || !reflect.DeepEqual(ss, []persistence.SourceDefinition{s}) {
		t.Fatal("historical list sources")
	}
	vs, err := d.ListViews(ctx)
	if err != nil || !reflect.DeepEqual(vs, []persistence.ViewDefinition{v}) {
		t.Fatal("historical list views")
	}
	updated, err := d.UpdateSource(ctx, s.Name, s)
	if err != nil {
		t.Fatal(err)
	}
	uv, err := d.UpdateView(ctx, v.Name, v)
	if err != nil {
		t.Fatal(err)
	}
	if !updated.CreatedAt.IsZero() || !uv.CreatedAt.IsZero() || updated.UpdatedAt.IsZero() || uv.UpdatedAt.IsZero() || updated.UpdatedAt.Location() != time.UTC || uv.UpdatedAt.Location() != time.UTC {
		t.Fatal("historical update dates")
	}
	_, err = pool.Exec(ctx, `UPDATE source SET created_at='2020-02-03 04:05:06+05',updated_at='2020-02-03 07:05:06+05';
	UPDATE registry SET created_at='2020-02-03 04:05:06+05',updated_at='2020-02-03 07:05:06+05'`)
	if err != nil {
		t.Fatal(err)
	}
	s, err = d.GetSource(ctx, s.Name)
	if err != nil {
		t.Fatal(err)
	}
	v, err = d.GetView(ctx, v.Name)
	if err != nil {
		t.Fatal(err)
	}
	created := time.Date(2020, 2, 2, 23, 5, 6, 0, time.UTC)
	for _, dates := range [][2]time.Time{{s.CreatedAt, s.UpdatedAt}, {v.CreatedAt, v.UpdatedAt}} {
		if dates[0].Location() != time.UTC || dates[1].Location() != time.UTC || !dates[0].Equal(created) || !dates[1].Equal(created.Add(3*time.Hour)) {
			t.Fatal("historical instants not normalized to UTC")
		}
	}
}

func TestRetainedConfigAndPostgresTimestampTrigger(t *testing.T) {
	t.Parallel()
	d, pool := newBackend(t)
	ctx := t.Context()
	config := []persistence.SourceDefinition{{Name: "config", API: &persistence.APISpec{Endpoint: "https://example.org"}, Schedule: "1h"}}
	if err := d.Reconcile(ctx, config, nil); err != nil {
		t.Fatal(err)
	}
	jobs, err := postgres.NewJobs(pool, 65536)
	if err != nil {
		t.Fatal(err)
	}
	first, err := jobs.Acquire(ctx, "config", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	result := persistence.SyncResult{Hash: "hash", FilterHash: "filter"}
	if err = jobs.CommitSnapshot(ctx, first, result, model.Snapshot{}); err != nil {
		t.Fatal(err)
	}
	active, err := jobs.Acquire(ctx, "config", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	// Verify the actual SQL trigger also excludes timestamp-only legacy writes.
	_, err = pool.Exec(ctx, `UPDATE source SET updated_at=now() WHERE name='config'`)
	if err != nil {
		t.Fatal(err)
	}
	if err = d.Reconcile(ctx, config, nil); err != nil {
		t.Fatal(err)
	}
	var generation int64
	if err = pool.QueryRow(ctx, `SELECT definition_generation FROM source WHERE name='config'`).Scan(&generation); err != nil {
		t.Fatal(err)
	}
	if generation != active.Generation {
		t.Fatal("timestamp-only trigger or retained config advanced generation")
	}
	if _, err = jobs.Renew(ctx, active, time.Minute); err != nil {
		t.Fatal(err)
	}
	if err = jobs.Complete(ctx, active, result); err != nil {
		t.Fatal("retained config invalidated baseline", err)
	}
	active, err = jobs.Acquire(ctx, "config", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	config[0].Schedule = "2h"
	if err = d.Reconcile(ctx, config, nil); err != nil {
		t.Fatal(err)
	}
	if err = jobs.Complete(ctx, active, result); !errors.Is(err, persistence.ErrStale) {
		t.Fatal("genuine config change did not fence old job", err)
	}
}
