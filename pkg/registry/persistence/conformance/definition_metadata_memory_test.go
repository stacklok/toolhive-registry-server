package conformance

import (
	"testing"
	"time"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

func TestMemoryRetainedConfigTimestampsDoNotFenceJobs(t *testing.T) {
	t.Parallel()
	m, ctx := fresh(), t.Context()
	config := []persistence.SourceDefinition{{Name: "config", API: &persistence.APISpec{Endpoint: "https://example.org"}, Schedule: "1h"}}
	must(t, m.Reconcile(ctx, config, nil))
	first, err := m.Acquire(ctx, "config", time.Minute)
	must(t, err)
	result := persistence.SyncResult{Hash: "hash", FilterHash: "filter"}
	must(t, m.CommitSnapshot(ctx, first, result, model.Snapshot{}))
	active, err := m.Acquire(ctx, "config", time.Minute)
	must(t, err)
	must(t, m.Reconcile(ctx, config, nil))
	renewed, err := m.Renew(ctx, active, time.Minute)
	must(t, err)
	if renewed.Generation != active.Generation {
		t.Fatal("metadata advanced generation")
	}
	must(t, m.Complete(ctx, active, result))
	active, err = m.Acquire(ctx, "config", time.Minute)
	must(t, err)
	config[0].Schedule = "2h"
	must(t, m.Reconcile(ctx, config, nil))
	want(t, m.Complete(ctx, active, result), persistence.ErrStale)
}
