# Leased registry synchronization jobs

`persistence.Jobs` is independent of `RegistryService`, HTTP, secrets and
identity. `postgres.NewJobs(pool, maxMetaSize)` borrows the same migrated,
caller-owned PostgreSQL pool. It neither migrates, repairs latest pointers,
starts a poller/listener nor closes the pool. Construct `NewEntries` once after
the **offline** migration/repair barrier described in
[registry persistence](registry-persistence.md#entries-and-atomic-source-snapshots-second-slice)
before serving reads;
construct `NewJobs` independently. Migration 000026 adds durable
`source.definition_generation` and lease/observed-generation columns to the
existing `registry_sync` row (not a second catalog). Existing standalone sync
rows work without leases; they are not fenced. PostgreSQL's definition trigger
advances generation on actual config, filter, schedule, kind, or syncable
changes, including legacy writes; an explicit direct snapshot also advances it. The
separate applied generation marks whether the recorded hashes describe the
catalog at the current definition. Source IDs prevent delete/recreate retargeting.
A successful unfenced `ReplaceSnapshot` refuses a current live lease, bumps
generation and clears the old hashes/counts/applied baseline in the same
transaction; its catalog is retained. It is for exclusive standalone/manual ownership, not a way to
complete a leased job. The old coordinator/writer remains an **exclusive**
source owner until it is replaced in #911/#914; do not run both on one source.

`ClaimNext(ctx, duration)` atomically picks one due scheduled git/API/file
source. It skips managed, Kubernetes, inline-file, unsupported legacy and
claimed sources and source rows locked by another transaction, returning
`(Job{}, false, nil)` when no eligible source is available.
`Acquire(ctx, sourceName, duration)` explicitly
starts any **non-managed** source (including Kubernetes operator and inline-file
manual snapshots) even when not due; only `ClaimNext` requires polling. Explicit
`Acquire` can wait for a source row lock, unlike polling. A live
lease gives `ErrBusy`. The job
captures a copied source definition, its immutable incarnation ID (`Job.Source.ID`,
not the name used to look up `Acquire`/`Status`), a persisted generation,
an opaque lease identity and informational expiry. Hosts must authorize source
selection and keep definitions/inline data out of logs. A restarted process may
reclaim an expired attempt. Durations must be positive whole microseconds up to
24 hours, never zero/indefinite; use `Renew` before expiry for long fetches.
Renew returns a new informational expiry with the **same** lease identity; old
copies of the handle remain valid if the lease is still live. Database time
(`clock_timestamp`) controls acquisition, renewal, expiry and final writes,
including after COPY/merge and row-lock waits; no caller wall clock is trusted.
Expired/reconfigured/consumed tokens produce `ErrStale` for renew, finish, or
failure, never overwrite a successor. A definition change invalidates an
in-flight fetch even if its lease has not expired.

`CommitSnapshot(ctx, job, result, snapshot)` validates and atomically replaces
all three source entry kinds and records success/hash/filter hash/counts and
applied generation in one serializable transaction. `Complete` acknowledges a
**genuine unchanged** fetch only when both hashes match a snapshot applied at
the current definition generation. Otherwise `ErrConflict` leaves the lease
live: use `CommitSnapshot` (even for an initial empty catalog). `Fail`
only accepts a `FailureCategory` (`fetch`, `validation`, `storage`, `cancelled`),
never an arbitrary error/secret-bearing string; it retains the last successful
metadata, but a definition change makes `BaselineValid` false until a new
snapshot is committed. `Status(ctx, sourceName)` returns `JobPending` for a source
without attempts, otherwise the latest `JobInProgress`, `JobCompleted` or
`JobFailed` phase, timestamps, `BaselineValid`, sanitized `FailureCategory`,
counts and last successful hashes. A zero `Failure` means no categorized error.
Unknown legacy phases become `JobPending`; legacy freeform errors are not
returned. Validation, canceled writes,
late SQL failures and expiry during a snapshot roll back **both** catalog and
acknowledgement; an unacknowledged attempt stays active until explicitly failed
or expired. Classify `ErrInvalid`, `ErrNotFound`, `ErrConflict`, `ErrBusy`,
`ErrStale`, `ErrUnavailable` with `errors.Is`; context errors propagate. A
storage error is not itself a status update: call `Fail` with a safe category
if the lease remains live. Retry serialization conflicts inside the backend;
retry fetches with a **new** job after a consumed/expired lease. Source jobs use
source-row locks so different sources can make progress independently.

```go
// pool is migrated and owned by the embedding application.
entries, err := postgres.NewEntries(ctx, pool, 65536)
if err != nil { return err }
_ = entries // Raw reads require host authorization.
jobs, err := postgres.NewJobs(pool, 65536)
if err != nil { return err }
job, ok, err := jobs.ClaimNext(ctx, time.Minute)
if err != nil || !ok { return err }

workCtx, cancelWork := context.WithCancel(ctx)
stopRenew := make(chan struct{})
renewed := make(chan error, 1)
go func() {
    tick := time.NewTicker(20 * time.Second)
    defer tick.Stop()
    for {
        select {
        case <-stopRenew: renewed <- nil; return
        case <-workCtx.Done(): renewed <- workCtx.Err(); return
        case <-tick.C:
            // Old job copies retain their token; expiry returned by Renew is informational.
            if _, err := jobs.Renew(workCtx, job, time.Minute); err != nil {
                renewed <- err
                cancelWork()
                return
            }
        }
    }
}()
// fetch is host-provided; unchanged must mean the exact applied hashes and
// definition generation are still current (not merely an empty fetch).
snapshot, result, unchanged, runErr := fetch(workCtx, job.Source)
if runErr == nil {
    if unchanged {
        runErr = jobs.Complete(workCtx, job, result)
    } else {
        runErr = jobs.CommitSnapshot(workCtx, job, result, snapshot)
    }
}
cancelWork() // unblock an in-flight Renew before joining; it uses workCtx
close(stopRenew)
renewErr := <-renewed // join before returning; never leave a renewing worker behind
if runErr == nil { return nil } // stopping Renew can report context.Canceled after a successful commit
if errors.Is(runErr, context.Canceled) && ctx.Err() == nil && renewErr != nil && !errors.Is(renewErr, context.Canceled) {
    runErr = renewErr
}
if errors.Is(runErr, persistence.ErrStale) { return runErr } // retry via a new claim
// A conflicting no-change result needs a real snapshot; this example ends the
// attempt instead of acknowledging it. Host may commit a fully fetched snapshot.
category := persistence.FailureStorage
if errors.Is(runErr, context.Canceled) { category = persistence.FailureCancelled }
if errors.Is(runErr, persistence.ErrConflict) { category = persistence.FailureValidation }
cleanup, done := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
defer done()
if failErr := jobs.Fail(cleanup, job, category); failErr != nil && !errors.Is(failErr, persistence.ErrStale) {
    return errors.Join(runErr, failErr)
}
return runErr
```

Backend authors can run exported `conformance.RunJobs` and
`conformance.RunJobsMulti` with separate handles over shared state. PostgreSQL
and an independent copy-on-write test backend use the same suite. This closes
the **persistence** jobs capability, not all of #910: portable consumer-view
selection/querying and standalone app wiring remain for #913/#914. Standalone
claim-aware API/auth behavior is unchanged; do not mount these raw methods as
unauthenticated endpoints.
