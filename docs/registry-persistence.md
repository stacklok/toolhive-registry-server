# Definition persistence (first slice)

`pkg/registry/persistence` defines storage-independent `Sources`, `Views`, and
`Reconciler` capabilities. `pkg/registry/postgres.NewDefinitions(pool)` implements
all three against the existing `source`, `registry`, and `registry_source` tables.
Pass a **migrated, caller-owned** `*pgxpool.Pool`; the constructor checks for nil
but does not connect, migrate, or close the pool. The host retains responsibility
for its lifetime. There is no built-in tenant, user, auth, visibility, or claims
policy. **Do not mount these methods directly as unauthenticated endpoints.**
The existing standalone HTTP service and its authorization are unchanged; this
adapter is not wired to it. Hosts must authorize every call and control which
principals can list or mutate definitions.

```go
// pool is created, migrated, and eventually closed by the host.
store, err := postgres.NewDefinitions(pool)
if err != nil { return err }
source, err := store.CreateSource(ctx, persistence.SourceDefinition{
    Name: "upstream", API: &persistence.APISpec{Endpoint: "https://registry.example"},
    Schedule: "1h",
})
if err != nil { return err }
_ = source // ID is a stable, generated UUID; Origin is API.
```

Source kind is inferred from exactly one of Git, API, File, Managed, or
Kubernetes (no separate type field). The spec is stored in the existing
`source_config` JSON object with the legacy source kind and `filter_config`
columns. `Schedule` maps to `sync_schedule` (PostgreSQL interval): git, API,
and file path/URL sources need a positive microsecond-resolution Go duration;
managed, Kubernetes, and inline file data have no schedule/filter and are
not syncable. These normalized, config-shaped definitions are **not** a
drop-in replacement for legacy YAML: non-synced legacy fields are ignored at
runtime while this API rejects schedule/filter on those kinds; inline file data
is unscheduled here even though legacy YAML requires a policy. Git credentials
are represented only by an absolute password-file path; hosts must validate
and load that file themselves when executing a sync. Inline file data can be sensitive: callers should avoid
passing secrets and must not log full definitions. The adapter never logs
configuration. Reads return source names in lexical order, views in lexical
order, and view references in caller-provided priority order. Returned values
and input slices do not alias persisted data. Structural spec validation
is performed here; payload format validation belongs to format/ingestion code.

Get/List source and view reads include both API- and CONFIG-owned definitions;
`Origin` identifies which writer owns each row. Reads do not filter CONFIG rows
and do not return legacy claims. Create and update are **API-owned only**.
Create requires empty ID/Origin; Update accepts empty metadata or the matching
ID/Origin returned by Get.
Update takes the immutable name separately and a replacement with the same
name. Source kind (including file location kind) and CONFIG/API ownership cannot
change. CONFIG-owned definitions can only be changed by `Reconcile`, which
accepts the complete desired CONFIG source/view set. Its input definitions have
empty ID/Origin; views reference only sources in the same CONFIG set. Reconcile
checks names and references before writing, rejects collisions with API rows,
prunes only CONFIG rows, and rolls back if a removed CONFIG source is still used
by an API view. API definitions and links remain untouched. Deleting any
referenced source returns `ErrInUse` (including references from CONFIG views).
Deleting a source also cascades its owned catalog/entry and sync state in the
existing schema; hosts must account for this data loss before deleting.
Use `errors.Is` with `ErrInvalid`, `ErrNotFound`, `ErrConflict`, `ErrInUse`,
and `ErrUnavailable` (backend details and connection strings are not exposed);
context cancellation/deadline errors propagate. This slice has no CAS revisions:
mutations of a given definition are serialized and last committed update wins.

**Legacy interoperability boundary:** definitions carrying non-null legacy
claims cannot be updated, deleted, or pruned by this claims-free adapter; those
operations return `ErrConflict` rather than erasing authorization metadata.
Source updates and retained CONFIG reconciliation also reject unknown legacy
spec/filter fields instead of dropping them. Read-only inspection does not
return claims. Do not have the legacy service and this adapter concurrently
**own** the same definition: the legacy service can update claims and source
config outside this adapter's contract. Existing
standalone claim enforcement remains in the legacy service. The adapter's
writes use serializable transactions, a transaction-scoped advisory lock in a
consistent order, retries on serialization/deadlock failures, conditional
ownership writes, a RESTRICT source-link FK, and a partial unique index for the
single managed source across concurrent writers. Migration 000024 requires
repairing any pre-existing database with multiple managed sources before upgrade.
The caller pool remains usable after successful or failed operations.

Backend implementers can run the same reusable tests used for PostgreSQL and
the independent test-only copy-on-write map backend:

```go
func TestMyDefinitions(t *testing.T) {
    conformance.Run(t, func(t *testing.T) persistence.Definitions {
        return newIsolatedBackend(t) // fresh, empty state per case
    })
    conformance.RunMulti(t, func(t *testing.T) (persistence.Definitions, persistence.Definitions) {
        return newTwoHandlesSharingFreshState(t) // distinct handles, same database
    })
}
```

This is **definitions only**: entries, latest-version records, jobs, sync
results, lease fencing, and HTTP integration are not part of this capability
and are reserved for subsequent slices. No generic transaction API is exposed.
