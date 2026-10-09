package conformance

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

// JobsFactory creates isolated definitions, entries and jobs per test.
type JobsFactory func(*testing.T) (persistence.Sources, persistence.Entries, persistence.Jobs)

// JobsPairFactory creates distinct job handles sharing a fresh backend.
type JobsPairFactory func(*testing.T) (persistence.Sources, persistence.Entries, persistence.Jobs, persistence.Jobs)

// RunJobs validates the storage-neutral lifecycle and catalog/status atomicity.
//
//nolint:gocyclo,lll // Independent lifecycle cases verify all three kinds and failure transitions.
func RunJobs(t *testing.T, factory JobsFactory) {
	t.Helper()
	t.Run("lifecycle and all kinds", func(t *testing.T) {
		t.Parallel()
		sources, entries, jobs := factory(t)
		ctx := t.Context()
		s := newEntrySource(t, sources, "lifecycle")
		state, err := jobs.Status(ctx, s.Name)
		if err != nil || state.Phase != "PENDING" || state.SourceID != s.ID {
			t.Fatalf("initial: %+v %v", state, err)
		}
		j, ok, err := jobs.ClaimNext(ctx, time.Minute)
		if err != nil || !ok || j.Source.ID != s.ID || j.LeaseID == "" || j.Generation <= 0 {
			t.Fatalf("claim: %+v %t %v", j, ok, err)
		}
		if _, err = jobs.Acquire(ctx, s.Name, time.Minute); !errors.Is(err, persistence.ErrBusy) {
			t.Fatalf("busy: %v", err)
		}
		snapshot := model.Snapshot{}
		snapshot.Data.Servers = []model.Server{conformanceServer("1"), conformanceServer("2")}
		snapshot.Data.Skills = append(snapshot.Data.Skills, conformanceSkill("1"))
		snapshot.Data.Plugins = append(snapshot.Data.Plugins, conformancePlugin("1"))
		r := persistence.SyncResult{Hash: "hash-1", FilterHash: "filter-1"}
		if err = jobs.Complete(ctx, j, persistence.SyncResult{Hash: "wrong", FilterHash: r.FilterHash}); !errors.Is(err, persistence.ErrConflict) {
			t.Fatalf("changed hash acknowledged: %v", err)
		}
		if err = jobs.Complete(ctx, j, persistence.SyncResult{Hash: r.Hash, FilterHash: "wrong"}); !errors.Is(err, persistence.ErrConflict) {
			t.Fatalf("changed filter hash acknowledged: %v", err)
		}
		if err = jobs.CommitSnapshot(ctx, j, r, snapshot); err != nil {
			t.Fatal(err)
		}
		state, err = jobs.Status(ctx, s.Name)
		if err != nil || state.Phase != "COMPLETED" || state.LastHash != r.Hash || state.LastFilterHash != r.FilterHash || state.ServerCount != 2 || state.SkillCount != 1 || state.PluginCount != 1 || state.LeaseExpiresAt != nil {
			t.Fatalf("success: %+v %v", state, err)
		}
		for _, v := range []struct {
			k persistence.EntryKind
			n string
		}{{persistence.ServerKind, testServerName}, {persistence.SkillKind, testSkillName}, {persistence.PluginKind, testSkillName}} {
			entry, e := entries.GetEntry(ctx, s.ID, v.k, v.n, persistence.VersionSelector{Latest: true})
			if e != nil || !entry.IsLatest || entry.SourceID != s.ID || (v.k == persistence.ServerKind && entry.Version != "2") {
				t.Fatalf("latest %s: %+v %v", v.k, entry, e)
			}
		}
		if err = jobs.Fail(ctx, j, persistence.FailureFetch); !errors.Is(err, persistence.ErrStale) {
			t.Fatalf("replay: %v", err)
		}
		next, err := jobs.Acquire(ctx, s.Name, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if err = jobs.Fail(ctx, next, persistence.FailureFetch); err != nil {
			t.Fatal(err)
		}
		state, err = jobs.Status(ctx, s.Name)
		if err != nil || state.Phase != "FAILED" || state.Failure != "fetch" || state.LastHash != r.Hash || state.ServerCount != 2 {
			t.Fatalf("failure preserved: %+v %v", state, err)
		}
		if err = jobs.Complete(ctx, next, r); !errors.Is(err, persistence.ErrStale) {
			t.Fatalf("failed replay: %v", err)
		}
		next, err = jobs.Acquire(ctx, s.Name, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if err = jobs.Complete(ctx, next, r); err != nil {
			t.Fatal(err)
		}
		state, err = jobs.Status(ctx, s.Name)
		if err != nil || state.LastHash != r.Hash || state.ServerCount != 2 || state.Failure != "" || !state.BaselineValid {
			t.Fatalf("no-change: %+v %v", state, err)
		}
	})
	t.Run("reconfiguration and direct writes", func(t *testing.T) {
		t.Parallel()
		sources, entries, jobs := factory(t)
		ctx := t.Context()
		s := newEntrySource(t, sources, "reconfigure")
		j, err := jobs.Acquire(ctx, s.Name, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = sources.UpdateSource(ctx, s.Name, s); err != nil {
			t.Fatal(err)
		}
		if _, err = jobs.Renew(ctx, j, time.Minute); err != nil {
			t.Fatalf("no-op update invalidated lease: %v", err)
		}
		if err = entries.ReplaceSnapshot(ctx, s, model.Snapshot{}); !errors.Is(err, persistence.ErrBusy) {
			t.Fatalf("direct busy: %v", err)
		}
		changed := s
		changed.Filter = &persistence.Filter{Names: &persistence.NameFilter{Include: []string{testSkillName}}}
		s, err = sources.UpdateSource(ctx, s.Name, changed)
		if err != nil {
			t.Fatal(err)
		}
		if err = jobs.Complete(ctx, j, persistence.SyncResult{}); !errors.Is(err, persistence.ErrStale) {
			t.Fatalf("reconfigured: %v", err)
		}
		fresh, err := jobs.Acquire(ctx, s.Name, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if fresh.Generation == j.Generation {
			t.Fatal("generation unchanged")
		}
		if err = jobs.Complete(ctx, fresh, persistence.SyncResult{}); !errors.Is(err, persistence.ErrConflict) {
			t.Fatalf("changed definition accepted empty baseline: %v", err)
		}
		if err = jobs.CommitSnapshot(ctx, fresh, persistence.SyncResult{Hash: "fresh-hash", FilterHash: "filter"}, model.Snapshot{}); err != nil {
			t.Fatal(err)
		}
		if err = entries.ReplaceSnapshot(ctx, s, model.Snapshot{}); err != nil {
			t.Fatal(err)
		}
		afterDirect, e := jobs.Acquire(ctx, s.Name, time.Minute)
		if e != nil {
			t.Fatal(e)
		}
		if afterDirect.Generation <= fresh.Generation {
			t.Fatalf("direct snapshot did not advance revision: %d <= %d", afterDirect.Generation, fresh.Generation)
		}
		if e = jobs.Complete(ctx, afterDirect, persistence.SyncResult{Hash: "fresh-hash", FilterHash: "filter"}); !errors.Is(e, persistence.ErrConflict) {
			t.Fatalf("direct replacement retained baseline: %v", e)
		}
		state, e := jobs.Status(ctx, s.Name)
		if e != nil || state.BaselineValid || state.LastHash != "" || state.ServerCount != 0 {
			t.Fatalf("direct replacement metadata: %+v %v", state, e)
		}
		if e = jobs.Fail(ctx, afterDirect, persistence.FailureCancelled); e != nil {
			t.Fatal(e)
		}
		if err = sources.DeleteSource(ctx, s.Name); err != nil {
			t.Fatal(err)
		}
		recreated := newEntrySource(t, sources, s.Name)
		if recreated.ID == s.ID {
			t.Fatal("reused source incarnation")
		}
		if err = jobs.Fail(ctx, j, persistence.FailureFetch); !errors.Is(err, persistence.ErrStale) {
			t.Fatalf("deleted job: %v", err)
		}
	})
	t.Run("cancelled operations and non-polling", func(t *testing.T) {
		t.Parallel()
		sources, _, jobs := factory(t)
		ctx := t.Context()
		s := newEntrySource(t, sources, "cancel")
		cancelled, cancel := context.WithCancel(ctx)
		if _, err := jobs.Acquire(ctx, s.Name, 0); !errors.Is(err, persistence.ErrInvalid) {
			t.Fatalf("zero lease: %v", err)
		}
		if _, err := jobs.Acquire(ctx, s.Name, 25*time.Hour); !errors.Is(err, persistence.ErrInvalid) {
			t.Fatalf("oversized lease: %v", err)
		}
		cancel()
		if _, err := jobs.Acquire(cancelled, s.Name, time.Minute); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel claim: %v", err)
		}
		j, err := jobs.Acquire(ctx, s.Name, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if err = jobs.Fail(ctx, j, persistence.FailureCategory("token=secret")); !errors.Is(err, persistence.ErrInvalid) {
			t.Fatalf("arbitrary failure: %v", err)
		}
		if err = jobs.CommitSnapshot(cancelled, j, persistence.SyncResult{}, model.Snapshot{}); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel commit: %v", err)
		}
		if err = jobs.Complete(ctx, j, persistence.SyncResult{}); !errors.Is(err, persistence.ErrConflict) {
			t.Fatalf("initial empty fetch acknowledged without snapshot: %v", err)
		}
		if err = jobs.CommitSnapshot(ctx, j, persistence.SyncResult{}, model.Snapshot{}); err != nil {
			t.Fatalf("cancellation lost job: %v", err)
		}
		managed, err := sources.CreateSource(ctx, persistence.SourceDefinition{Name: testManagedKind, Managed: &persistence.ManagedSpec{}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = jobs.Acquire(ctx, managed.Name, time.Minute); !errors.Is(err, persistence.ErrConflict) {
			t.Fatalf("managed acquired: %v", err)
		}
		operator, err := sources.CreateSource(ctx, persistence.SourceDefinition{Name: "operator", Kubernetes: &persistence.KubernetesSpec{Namespaces: []string{"a"}}})
		if err != nil {
			t.Fatal(err)
		}
		opJob, err := jobs.Acquire(ctx, operator.Name, time.Minute)
		if err != nil {
			t.Fatalf("operator acquire: %v", err)
		}
		operator.Kubernetes.Namespaces = []string{"b"}
		operator, err = sources.UpdateSource(ctx, operator.Name, operator)
		if err != nil {
			t.Fatal(err)
		}
		if err = jobs.CommitSnapshot(ctx, opJob, persistence.SyncResult{}, model.Snapshot{}); !errors.Is(err, persistence.ErrStale) {
			t.Fatalf("old namespace fetch: %v", err)
		}
		opJob, err = jobs.Acquire(ctx, operator.Name, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if err = jobs.CommitSnapshot(ctx, opJob, persistence.SyncResult{}, model.Snapshot{}); err != nil {
			t.Fatal(err)
		}
		inline, err := sources.CreateSource(ctx, persistence.SourceDefinition{Name: testInlineName, File: &persistence.FileSpec{Data: "{}"}})
		if err != nil {
			t.Fatal(err)
		}
		inlineJob, err := jobs.Acquire(ctx, inline.Name, time.Minute)
		if err != nil {
			t.Fatalf("manual inline acquire: %v", err)
		}
		if err = jobs.CommitSnapshot(ctx, inlineJob, persistence.SyncResult{}, model.Snapshot{}); err != nil {
			t.Fatal(err)
		}
		_, ok, err := jobs.ClaimNext(ctx, time.Minute)
		if err != nil || ok {
			t.Fatalf("unexpected due job %t %v", ok, err)
		}
	})
	t.Run("direct replacement cannot inherit prior fetch", func(t *testing.T) {
		t.Parallel()
		sources, entries, jobs := factory(t)
		ctx := t.Context()
		s := newEntrySource(t, sources, "replacement")
		original := model.Snapshot{}
		original.Data.Servers = []model.Server{conformanceServer("1")}
		j, err := jobs.Acquire(ctx, s.Name, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		result := persistence.SyncResult{Hash: "H", FilterHash: "F"}
		if err = jobs.CommitSnapshot(ctx, j, result, original); err != nil {
			t.Fatal(err)
		}
		replacement := model.Snapshot{}
		replacement.Data.Servers = []model.Server{conformanceServer("2")}
		if err = entries.ReplaceSnapshot(ctx, s, replacement); err != nil {
			t.Fatal(err)
		}
		j, err = jobs.Acquire(ctx, s.Name, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if err = jobs.Complete(ctx, j, result); !errors.Is(err, persistence.ErrConflict) {
			t.Fatalf("old hash accepted for new catalog: %v", err)
		}
		entry, err := entries.GetEntry(ctx, s.ID, persistence.ServerKind, testServerName, persistence.VersionSelector{Latest: true})
		if err != nil || entry.Version != "2" {
			t.Fatalf("replacement rolled back: %+v %v", entry, err)
		}
		if err = jobs.CommitSnapshot(ctx, j, result, replacement); err != nil {
			t.Fatal(err)
		}
		state, err := jobs.Status(ctx, s.Name)
		if err != nil || !state.BaselineValid || state.LastHash != "H" || state.ServerCount != 1 {
			t.Fatalf("commit after conflict: %+v %v", state, err)
		}
	})
	t.Run("definition change invalidates prior baseline", func(t *testing.T) {
		t.Parallel()
		sources, _, jobs := factory(t)
		ctx := t.Context()
		s := newEntrySource(t, sources, "definition-baseline")
		original := persistence.SyncResult{Hash: "old-hash", FilterHash: "old-filter"}
		j, err := jobs.Acquire(ctx, s.Name, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if err = jobs.CommitSnapshot(ctx, j, original, model.Snapshot{}); err != nil {
			t.Fatal(err)
		}
		s.Filter = &persistence.Filter{Names: &persistence.NameFilter{Include: []string{testServerName}}}
		if _, err = sources.UpdateSource(ctx, s.Name, s); err != nil {
			t.Fatal(err)
		}
		state, err := jobs.Status(ctx, s.Name)
		if err != nil || state.LastHash != original.Hash || state.BaselineValid {
			t.Fatalf("definition status: %+v %v", state, err)
		}
		j, err = jobs.Acquire(ctx, s.Name, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if err = jobs.Complete(ctx, j, original); !errors.Is(err, persistence.ErrConflict) {
			t.Fatalf("old definition acknowledged: %v", err)
		}
		if err = jobs.Fail(ctx, j, persistence.FailureFetch); err != nil {
			t.Fatal(err)
		}
		state, err = jobs.Status(ctx, s.Name)
		if err != nil || state.LastHash != original.Hash || state.BaselineValid {
			t.Fatalf("failed status: %+v %v", state, err)
		}
	})
}

// RunJobsMulti checks worker contention and cross-handle fencing.
//
//nolint:gocyclo,lll // Multi-worker assertions exercise complete shared-state transitions.
func RunJobsMulti(t *testing.T, factory JobsPairFactory) {
	t.Helper()
	t.Run("one winner", func(t *testing.T) {
		t.Parallel()
		sources, _, a, b := factory(t)
		s := newEntrySource(t, sources, "race")
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		errs := make([]error, 2)
		jobs := make([]persistence.Job, 2)
		for i, handle := range []persistence.Jobs{a, b} {
			go func() { defer wg.Done(); <-start; jobs[i], errs[i] = handle.Acquire(t.Context(), s.Name, time.Minute) }()
		}
		close(start)
		wg.Wait()
		winners := 0
		for _, e := range errs {
			if e == nil {
				winners++
			} else if !errors.Is(e, persistence.ErrBusy) {
				t.Fatalf("unexpected acquire: %v", e)
			}
		}
		if winners != 1 {
			t.Fatalf("winners=%d errors=%v", winners, errs)
		}
		other := newEntrySource(t, sources, "other")
		independent, e := b.Acquire(t.Context(), other.Name, time.Minute)
		if e != nil {
			t.Fatalf("other source blocked: %v", e)
		}
		if e = a.CommitSnapshot(t.Context(), independent, persistence.SyncResult{Hash: "other"}, model.Snapshot{}); e != nil {
			t.Fatal(e)
		}
		first := jobs[0]
		if first.LeaseID == "" {
			first = jobs[1]
		}
		if err := b.Fail(t.Context(), first, persistence.FailureCancelled); err != nil {
			t.Fatal(err)
		}
		if err := a.Complete(t.Context(), first, persistence.SyncResult{}); !errors.Is(err, persistence.ErrStale) {
			t.Fatalf("replay: %v", err)
		}
	})
	t.Run("expired worker and recovery", func(t *testing.T) {
		t.Parallel()
		sources, entries, a, b := factory(t)
		s := newEntrySource(t, sources, "recovery")
		old, err := a.Acquire(t.Context(), s.Name, 200*time.Millisecond)
		if err != nil {
			t.Fatal(err)
		}
		renewed, err := b.Renew(t.Context(), old, 200*time.Millisecond)
		if err != nil {
			t.Fatal(err)
		}
		if renewed.LeaseID != old.LeaseID {
			t.Fatal("renew changed identity")
		}
		if _, err = b.Acquire(t.Context(), s.Name, time.Minute); !errors.Is(err, persistence.ErrBusy) {
			t.Fatalf("renewed lease: %v", err)
		}
		deadline := time.NewTimer(5 * time.Second)
		defer deadline.Stop()
		var next persistence.Job
		for {
			next, err = b.Acquire(t.Context(), s.Name, time.Minute)
			if err == nil {
				break
			}
			if !errors.Is(err, persistence.ErrBusy) {
				t.Fatal(err)
			}
			select {
			case <-deadline.C:
				t.Fatal("lease never expired")
			case <-time.After(20 * time.Millisecond):
			}
		}
		if err = a.CommitSnapshot(t.Context(), old, persistence.SyncResult{Hash: "stale"}, model.Snapshot{}); !errors.Is(err, persistence.ErrStale) {
			t.Fatalf("old commit: %v", err)
		}
		if err = a.Fail(t.Context(), old, persistence.FailureFetch); !errors.Is(err, persistence.ErrStale) {
			t.Fatalf("old fail: %v", err)
		}
		if _, err = a.Renew(t.Context(), old, time.Minute); !errors.Is(err, persistence.ErrStale) {
			t.Fatalf("old renew: %v", err)
		}
		status, err := a.Status(t.Context(), s.Name)
		if err != nil || status.LastHash != "" || status.Attempts != 2 {
			t.Fatalf("recovery status: %+v %v", status, err)
		}
		if err = entries.ReplaceSnapshot(t.Context(), s, model.Snapshot{}); !errors.Is(err, persistence.ErrBusy) {
			t.Fatalf("active replacement: %v", err)
		}
		if err = b.CommitSnapshot(t.Context(), next, persistence.SyncResult{Hash: "current"}, model.Snapshot{}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("polling contention and expiry", func(t *testing.T) {
		t.Parallel()
		sources, _, a, b := factory(t)
		s := newEntrySource(t, sources, "poll-race")
		start := make(chan struct{})
		var wg sync.WaitGroup
		var jobs [2]persistence.Job
		var found [2]bool
		var errs [2]error
		for i, handle := range []persistence.Jobs{a, b} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				jobs[i], found[i], errs[i] = handle.ClaimNext(t.Context(), 200*time.Millisecond)
			}()
		}
		close(start)
		wg.Wait()
		if errs[0] != nil || errs[1] != nil || found[0] == found[1] {
			t.Fatalf("poll winners: %v %v, errors %v", found[0], found[1], errs)
		}
		old := jobs[0]
		if !found[0] {
			old = jobs[1]
		}
		deadline := time.NewTimer(5 * time.Second)
		defer deadline.Stop()
		for {
			next, ok, err := b.ClaimNext(t.Context(), time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			if ok {
				if next.Source.ID != s.ID || next.LeaseID == old.LeaseID {
					t.Fatalf("bad reclaim: %+v", next)
				}
				if err = b.CommitSnapshot(t.Context(), next, persistence.SyncResult{Hash: "current"}, model.Snapshot{}); err != nil {
					t.Fatal(err)
				}
				if err = a.Fail(t.Context(), old, persistence.FailureFetch); !errors.Is(err, persistence.ErrStale) {
					t.Fatalf("old lease: %v", err)
				}
				break
			}
			select {
			case <-deadline.C:
				t.Fatal("poll did not reclaim expired job")
			case <-time.After(20 * time.Millisecond):
			}
		}
		if _, ok, err := a.ClaimNext(t.Context(), time.Minute); err != nil || ok {
			t.Fatalf("premature due: %t %v", ok, err)
		}
	})
}
