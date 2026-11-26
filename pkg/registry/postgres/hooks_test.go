package postgres_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	thvregistry "github.com/stacklok/toolhive-core/registry/types"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/postgres"
)

// recordingHook records each call and, after a write, tags every entry of the
// written source through the transaction, the way a host keeps its own
// per-entry data in step with adapter writes.
type recordingHook struct {
	mu        sync.Mutex
	calls     []string
	beforeErr error
	afterErr  error
	seen      map[string]int
}

func (h *recordingHook) BeforeEntryWrite(_ context.Context, _ pgx.Tx, w postgres.EntryWrite) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls = append(h.calls, "before:"+string(w.Op)+":"+w.SourceName)
	return h.beforeErr
}

func (h *recordingHook) AfterEntryWrite(ctx context.Context, tx pgx.Tx, w postgres.EntryWrite) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls = append(h.calls, "after:"+string(w.Op)+":"+w.SourceName)
	if h.afterErr != nil {
		return h.afterErr
	}
	var n int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM registry_entry WHERE source_id=$1`, w.SourceID).Scan(&n); err != nil {
		return err
	}
	if h.seen == nil {
		h.seen = map[string]int{}
	}
	h.seen[string(w.Op)] = n
	_, err := tx.Exec(ctx, `UPDATE registry_entry SET claims='{"org":"acme"}' WHERE source_id=$1`, w.SourceID)
	return err
}

func (h *recordingHook) take() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := h.calls
	h.calls = nil
	return out
}

func taggedEntries(ctx context.Context, t *testing.T, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, sourceID string) (total, tagged int) {
	t.Helper()
	err := q.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE claims IS NOT NULL) FROM registry_entry WHERE source_id=$1`,
		sourceID).Scan(&total, &tagged)
	if err != nil {
		t.Fatal(err)
	}
	return total, tagged
}

func equalCalls(t *testing.T, got []string, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("hook calls = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("hook calls = %v, want %v", got, want)
		}
	}
}

func TestEntryWriteHookRunsInsideEachWrite(t *testing.T) {
	t.Parallel()
	d, pool := newBackend(t)
	ctx := t.Context()
	hook := &recordingHook{}
	entries, err := postgres.NewEntries(ctx, pool, 65536, postgres.WithEntryWriteHook(hook))
	if err != nil {
		t.Fatal(err)
	}
	synced, err := d.CreateSource(ctx, persistence.SourceDefinition{Name: "upstream", API: &persistence.APISpec{Endpoint: "https://example.org"}, Schedule: "1h"})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := model.Snapshot{}
	snapshot.Data.Skills = append(snapshot.Data.Skills,
		thvregistry.Skill{Namespace: "com.example", Name: "one", Version: "1.0.0", Description: "skill"},
		thvregistry.Skill{Namespace: "com.example", Name: "two", Version: "1.0.0", Description: "skill"})
	if err = entries.ReplaceSnapshot(ctx, synced, snapshot); err != nil {
		t.Fatal(err)
	}
	equalCalls(t, hook.take(), "before:snapshot:upstream", "after:snapshot:upstream")
	if hook.seen["snapshot"] != 2 {
		t.Fatalf("after hook saw %d entries, want the 2 just written", hook.seen["snapshot"])
	}
	if total, tagged := taggedEntries(ctx, t, pool, synced.ID); total != 2 || tagged != 2 {
		t.Fatalf("hook writes not committed with the snapshot: total=%d tagged=%d", total, tagged)
	}

	managed, err := d.CreateSource(ctx, persistence.SourceDefinition{Name: "managed", Managed: &persistence.ManagedSpec{}})
	if err != nil {
		t.Fatal(err)
	}
	skill := persistence.Entry{Kind: persistence.SkillKind, Name: "sample", Version: "1.0.0",
		Skill: &model.Skill{Namespace: "com.example", Name: "sample", Version: "1.0.0", Description: "skill"}}
	if _, err = entries.Publish(ctx, skill); err != nil {
		t.Fatal(err)
	}
	equalCalls(t, hook.take(), "before:publish:managed", "after:publish:managed")
	if total, tagged := taggedEntries(ctx, t, pool, managed.ID); total != 1 || tagged != 1 {
		t.Fatalf("publish: total=%d tagged=%d", total, tagged)
	}

	if err = entries.DeleteManaged(ctx, persistence.SkillKind, "sample", "1.0.0"); err != nil {
		t.Fatal(err)
	}
	equalCalls(t, hook.take(), "before:delete:managed", "after:delete:managed")

	// The hook runs before the adapter's existence check, so a host gate can
	// decide whether a caller learns that a hidden entry is missing.
	if err = entries.DeleteManaged(ctx, persistence.SkillKind, "sample", "1.0.0"); !errors.Is(err, persistence.ErrNotFound) {
		t.Fatalf("delete missing: %v", err)
	}
	equalCalls(t, hook.take(), "before:delete:managed")
}

func TestEntryWriteHookErrorAbortsAndPassesThrough(t *testing.T) {
	t.Parallel()
	d, pool := newBackend(t)
	ctx := t.Context()
	hostErr := errors.New("host refused")
	for _, tc := range []struct {
		name          string
		before, after error
	}{
		{"before", hostErr, nil},
		{"after", nil, hostErr},
	} {
		hook := &recordingHook{beforeErr: tc.before, afterErr: tc.after}
		entries, err := postgres.NewEntries(ctx, pool, 65536, postgres.WithEntryWriteHook(hook))
		if err != nil {
			t.Fatal(err)
		}
		source, err := d.CreateSource(ctx, persistence.SourceDefinition{Name: "src-" + tc.name, API: &persistence.APISpec{Endpoint: "https://example.org"}, Schedule: "1h"})
		if err != nil {
			t.Fatal(err)
		}
		snapshot := model.Snapshot{}
		snapshot.Data.Skills = append(snapshot.Data.Skills, thvregistry.Skill{Namespace: "com.example", Name: "one", Version: "1.0.0", Description: "skill"})
		err = entries.ReplaceSnapshot(ctx, source, snapshot)
		if err != hostErr { //nolint:errorlint // the adapter must return the host's error value itself
			t.Fatalf("%s: err = %v, want the host error unchanged", tc.name, err)
		}
		if total, _ := taggedEntries(ctx, t, pool, source.ID); total != 0 {
			t.Fatalf("%s: aborted snapshot left %d entries", tc.name, total)
		}
	}
}

func TestEntryWriteHookOnJobCommit(t *testing.T) {
	t.Parallel()
	d, pool := newBackend(t)
	ctx := t.Context()
	hook := &recordingHook{}
	jobs, err := postgres.NewJobs(pool, 65536, postgres.WithEntryWriteHook(hook))
	if err != nil {
		t.Fatal(err)
	}
	source, err := d.CreateSource(ctx, persistence.SourceDefinition{Name: "upstream", API: &persistence.APISpec{Endpoint: "https://example.org"}, Schedule: "1h"})
	if err != nil {
		t.Fatal(err)
	}
	job, err := jobs.Acquire(ctx, source.Name, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := model.Snapshot{}
	snapshot.Data.Skills = append(snapshot.Data.Skills, thvregistry.Skill{Namespace: "com.example", Name: "one", Version: "1.0.0", Description: "skill"})
	if err = jobs.CommitSnapshot(ctx, job, persistence.SyncResult{Hash: "h", FilterHash: "f"}, snapshot); err != nil {
		t.Fatal(err)
	}
	equalCalls(t, hook.take(), "before:snapshot:upstream", "after:snapshot:upstream")
	if total, tagged := taggedEntries(ctx, t, pool, source.ID); total != 1 || tagged != 1 {
		t.Fatalf("job commit: total=%d tagged=%d", total, tagged)
	}
}

func TestEntryWriteHookOptionValidation(t *testing.T) {
	t.Parallel()
	_, pool := newBackend(t)
	var typedNil *recordingHook
	for name, opt := range map[string]postgres.Option{
		"nil option": nil,
		"typed nil":  postgres.WithEntryWriteHook(typedNil),
	} {
		if _, err := postgres.NewEntries(t.Context(), pool, 65536, opt); !errors.Is(err, persistence.ErrInvalid) {
			t.Fatalf("%s: NewEntries err = %v", name, err)
		}
		if _, err := postgres.NewJobs(pool, 65536, opt); !errors.Is(err, persistence.ErrInvalid) {
			t.Fatalf("%s: NewJobs err = %v", name, err)
		}
	}
}
