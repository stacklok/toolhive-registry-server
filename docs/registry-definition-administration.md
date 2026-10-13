# Source and view definition administration

`catalog.DefinitionAdmin` composes the existing `persistence.Sources` and
`persistence.Views` method shapes and definition types. `NewDefinitionAdmin`
accepts those capabilities **independently**; no legacy service, database,
configuration, identity, HTTP or Kubernetes dependency is required. A host may
also implement `DefinitionAdmin` itself. Construction performs no IO, starts no
work, and neither owns nor closes the supplied, initialized non-nil backends.
Typed nil implementations are outside the caller contract.

Standalone mounts these capabilities behind mandatory Cedar admission after
[exposure preparation](registry-exposure-migration.md). Embedding hosts authorize calls
themselves. Config reconciliation remains the separate `persistence.Reconciler`
capability; [publication and diagnostics](registry-publication-diagnostics.md) are
separate operations, not part of `DefinitionAdmin`.
Never co-own a source with old binaries or strip historical claims automatically.

```go
admin, err := catalog.NewDefinitionAdmin(sourceStore, viewStore,
    catalog.DefinitionAdminOptions{
        SyncInline:    syncInlineByPersistedName, // optional host callback
        NotifyRefresh: notifySourceRuntime,      // optional host callback
    })
if err != nil { return err }
stored, err := admin.CreateSource(ctx, persistence.SourceDefinition{
    Name: "upstream",
    API: &persistence.APISpec{Endpoint: "https://registry.example"},
    Schedule: "1h",
})
var committed *catalog.PostCommitError
if errors.As(err, &committed) {
    // stored is committed. Rediscover persisted state before recovering follow-up.
    // Do not retry Create or treat an unwrapped cancellation/conflict as rollback.
}
```

## Storage semantics and errors

Gets/lists include both CONFIG and API origins. Create/update/delete mutate only
API-owned definitions. Names, source incarnations, source kinds and file location
kinds are immutable; views preserve caller-ordered links. Referenced source
removal fails `ErrInUse`; unreferenced deletion cascades owned entries and sync
state. Source recreation gets another incarnation. Missing managed sources do
not cause implicit creation or publication. See [persistence](registry-persistence.md)
for validation, atomic reconciliation, singleton and legacy metadata guards.

Both public definitions expose `CreatedAt`/`UpdatedAt` from existing database
columns. New rows receive nonzero UTC storage timestamps; updates and retained
CONFIG reconciliation preserve creation time and touch update time, including
unchanged successful writes/link replacement. Failed transactions leave persisted
timestamps unchanged; reads do not touch them. Historical SQL NULLs return zero,
not invented dates. Create/reconcile reject nonzero input timestamps. Update
accepts returned metadata but **ignores timestamps as inputs**: these are not CAS
revisions. Timestamp-only touches do not change ingestion generation, invalidate
an applied baseline or fence leases; genuine ingestion-definition changes do.
Returned definitions and their nested values are independent copies.

Use `errors.Is` for authoritative backend categories:

- `ErrAlreadyExists`: only a duplicate source/view name, refining `ErrConflict`.
- `ErrConfigOwned`: API mutation of a CONFIG definition, refining `ErrConflict`.
- `ErrKindChange`: source/file-location kind mutation, refining `ErrConflict`.
- Existing `ErrInvalid`, `ErrNotFound`, `ErrInUse`, `ErrConflict`, `ErrUnavailable`
  and cancellation/deadline behavior remain. Managed singleton, stale incarnation,
  and legacy claimed/unknown metadata guards are **not** `ErrAlreadyExists`.

A later PUT adapter may fall back from create to update only on
`ErrAlreadyExists`, never on general conflict or a raced pre-read. Backend errors
are sanitized; do not log full definitions or inline/credential data.

## Committed follow-up failures

After source create/update commits, an optional `SyncInline(ctx, stored.Name)`
runs **only for a persisted inline file source**. Then optional
`NotifyRefresh(ctx)` runs after every committed source create/update/delete,
even when inline synchronization failed. Views have no source-runtime callback.
Persistence failure invokes neither. No callback means no follow-up was requested,
not proof of synchronization. Callbacks use the caller context, synchronously;
there are no retries, goroutines, compensation, readiness or lifecycle ownership.

Failed follow-up returns `*catalog.PostCommitError`; create/update still return
the stored definition, and delete is already committed. `errors.Is` identifies
`catalog.ErrSyncInline` and/or `catalog.ErrNotifyRefresh`, and the original callback
errors (including context cancellation). Multiple failures use standard
`errors.Join`. `Error()` never includes callback messages; its unwrapped `Cause`
may be sensitive and must not be logged. Replacement applications may construct
this type with their own joined stage markers and causes.

**Inspect `PostCommitError` first**, before ordinary conflict/cancellation mapping;
a later HTTP adapter must report `committed: true`. Rediscover persisted
source/view state and current incarnation, then explicitly recover the failed
follow-up. Hosts' callbacks must reread current definitions and use the leased,
fenced writer; concurrent mutations may supersede the returned definition or
remove/recreate its name. Refresh must still be attempted with a cancelled context
and its failure reported honestly. Never replay a detached request as a snapshot
or automatically retry the mutation after a post-commit error.

The shared definition/job conformance suites run against PostgreSQL and an
independent memory backend. Catalog callback tests use independently injected
strict generated mocks; the executable external-module fixture exercises both
replacement capabilities and a replacement application without internal,
PostgreSQL, auth, configuration or Kubernetes imports.
