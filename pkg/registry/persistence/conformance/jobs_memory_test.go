package conformance

import (
	"context"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

type memJob struct {
	Lease      string
	Generation int64
	Expiry     time.Time
	Observed   int64
	Applied    int64
	Status     persistence.JobStatus
}

var _ persistence.Jobs = (*memory)(nil)

func (m *memory) ClaimNext(ctx context.Context, duration time.Duration) (persistence.Job, bool, error) {
	if err := persistence.ValidateLeaseDuration(duration); err != nil {
		return persistence.Job{}, false, err
	}
	var job persistence.Job
	found := false
	err := m.transact(ctx, func(n *memory) error {
		names := make([]string, 0, len(n.sources))
		for name := range n.sources {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			source := n.sources[name]
			if source.Schedule == "" || source.Managed != nil || source.Kubernetes != nil || source.File != nil && source.File.Data != "" {
				continue
			}
			state := n.jobs[source.ID]
			if state.Lease != "" && state.Expiry.After(n.clock()) && state.Generation == n.generations[source.ID] {
				continue
			}
			period, err := time.ParseDuration(source.Schedule)
			if err != nil {
				return err
			}
			if state.Status.EndedAt != nil && state.Observed == n.generations[source.ID] && state.Status.EndedAt.Add(period).After(n.clock()) {
				continue
			}
			job, err = n.acquire(name, duration)
			if err != nil {
				return err
			}
			found = true
			return nil
		}
		return nil
	})
	return job, found, err
}
func (m *memory) Acquire(ctx context.Context, name string, duration time.Duration) (persistence.Job, error) {
	if err := persistence.ValidateLeaseDuration(duration); err != nil {
		return persistence.Job{}, err
	}
	var job persistence.Job
	err := m.transact(ctx, func(n *memory) error { var e error; job, e = n.acquire(name, duration); return e })
	return job, err
}
func (m *memory) Renew(ctx context.Context, job persistence.Job, duration time.Duration) (persistence.Job, error) {
	if err := persistence.ValidateLeaseDuration(duration); err != nil {
		return persistence.Job{}, err
	}
	err := m.transact(ctx, func(n *memory) error {
		state, e := n.fence(job)
		if e != nil {
			return e
		}
		state.Expiry = n.clock().Add(duration)
		state.Status.LeaseExpiresAt = &state.Expiry
		n.jobs[job.Source.ID] = state
		job.ExpiresAt = state.Expiry
		return nil
	})
	return job, err
}
func (m *memory) Complete(ctx context.Context, job persistence.Job, result persistence.SyncResult) error {
	return m.transact(ctx, func(n *memory) error {
		state, e := n.fence(job)
		if e != nil {
			return e
		}
		if state.Applied != state.Generation || state.Status.LastHash != result.Hash || state.Status.LastFilterHash != result.FilterHash {
			return persistence.ErrConflict
		}
		n.succeed(job.Source.ID, state, result)
		return nil
	})
}
func (m *memory) CommitSnapshot(ctx context.Context, job persistence.Job, result persistence.SyncResult, snapshot model.Snapshot) error {
	if err := persistence.ValidateSnapshot(&snapshot); err != nil {
		return err
	}
	return m.transact(ctx, func(n *memory) error {
		state, e := n.fence(job)
		if e != nil {
			return e
		}
		if e = n.applySnapshot(job.Source, snapshot); e != nil {
			return e
		}
		state.Applied = state.Generation
		state.Status.ServerCount = int64(len(snapshot.Data.Servers))
		state.Status.SkillCount = int64(len(snapshot.Data.Skills))
		state.Status.PluginCount = int64(len(snapshot.Data.Plugins))
		n.succeed(job.Source.ID, state, result)
		return nil
	})
}
func (m *memory) Fail(ctx context.Context, job persistence.Job, category persistence.FailureCategory) error {
	if err := persistence.ValidateFailureCategory(category); err != nil {
		return err
	}
	return m.transact(ctx, func(n *memory) error {
		state, e := n.fence(job)
		if e != nil {
			return e
		}
		state.Lease = ""
		state.Status.Phase = persistence.JobFailed
		state.Status.Failure = category
		now := n.clock()
		state.Status.EndedAt = &now
		state.Status.LeaseExpiresAt = nil
		n.jobs[job.Source.ID] = state
		return nil
	})
}
func (m *memory) Status(ctx context.Context, name string) (persistence.JobStatus, error) {
	m.Lock()
	defer m.Unlock()
	if err := ctx.Err(); err != nil {
		return persistence.JobStatus{}, err
	}
	s, ok := m.sources[name]
	if !ok {
		return persistence.JobStatus{}, persistence.ErrNotFound
	}
	state, ok := m.jobs[s.ID]
	if !ok {
		return persistence.JobStatus{SourceID: s.ID, Phase: persistence.JobPending}, nil
	}
	state.Status.BaselineValid = state.Applied != 0 && state.Applied == m.generations[s.ID]
	return clone(state.Status), nil
}
func (n *memory) acquire(name string, duration time.Duration) (persistence.Job, error) {
	s, ok := n.sources[name]
	if !ok {
		return persistence.Job{}, persistence.ErrNotFound
	}
	if s.Managed != nil {
		return persistence.Job{}, persistence.ErrConflict
	}
	state := n.jobs[s.ID]
	if state.Lease != "" && state.Expiry.After(n.clock()) && state.Generation == n.generations[s.ID] {
		return persistence.Job{}, persistence.ErrBusy
	}
	now := n.clock()
	state.Lease = uuid.NewString()
	state.Generation = n.generations[s.ID]
	state.Observed = state.Generation
	state.Expiry = now.Add(duration)
	state.Status.SourceID = s.ID
	state.Status.Phase = persistence.JobInProgress
	state.Status.Attempts++
	state.Status.StartedAt = &now
	state.Status.EndedAt = nil
	state.Status.LeaseExpiresAt = &state.Expiry
	state.Status.Failure = ""
	n.jobs[s.ID] = state
	return persistence.Job{Source: clone(s), LeaseID: state.Lease, Generation: state.Generation, ExpiresAt: state.Expiry}, nil
}
func (n *memory) fence(job persistence.Job) (memJob, error) {
	s, ok := n.sources[job.Source.Name]
	if !ok || s.ID != job.Source.ID {
		return memJob{}, persistence.ErrStale
	}
	state := n.jobs[s.ID]
	if job.LeaseID == "" || state.Lease != job.LeaseID || state.Generation != job.Generation ||
		n.generations[s.ID] != job.Generation || !state.Expiry.After(n.clock()) || state.Status.Phase != persistence.JobInProgress {
		return memJob{}, persistence.ErrStale
	}
	return state, nil
}
func (n *memory) succeed(id string, state memJob, result persistence.SyncResult) {
	state.Lease = ""
	state.Status.Phase = persistence.JobCompleted
	state.Status.BaselineValid = state.Applied == state.Generation
	state.Status.Failure = ""
	state.Status.LastHash = result.Hash
	state.Status.LastFilterHash = result.FilterHash
	now := n.clock()
	state.Status.EndedAt = &now
	state.Status.LeaseExpiresAt = nil
	n.jobs[id] = state
}
