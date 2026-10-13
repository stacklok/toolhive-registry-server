# Registry persistence (definitions, entries, jobs)

## Definitions (first slice)

`pkg/registry/persistence` defines storage-independent `Sources`, `Views`, and
`Reconciler` capabilities. `pkg/registry/postgres.NewDefinitions(pool)` implements
all three against the existing `source`, `registry`, and `registry_source` tables.
Pass a **migrated, caller-owned** `*pgxpool.Pool`; the constructor checks for nil
but does not connect, migrate, or close the pool. The host retains responsibility
for its lifetime. There is no built-in tenant, user, auth, visibility, or claims
policy. **Do not mount these methods directly as unauthenticated endpoints.**
The standalone assembly uses these same public adapters with verified authentication
and Cedar admission. Embedding hosts must authorize every call and control which
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
// source.ID is an opaque incarnation ID; Origin is API.
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
and `ErrUnavailable` (backend details and connection strings are not exposed).
`ErrAlreadyExists` (same source/view name), `ErrConfigOwned` (API mutation of
CONFIG), and `ErrKindChange` (source/file-location kind) refine `ErrConflict`
without conflating singleton, stale-ID or legacy metadata guards. Context
cancellation/deadline errors propagate. This slice has no CAS revisions:
mutations of a given definition are serialized and last committed update wins.
`CreatedAt`/`UpdatedAt` are storage-assigned UTC metadata from existing columns;
new rows are nonzero, historical NULLs read as zero, and successful writes touch
update time without changing creation time. Create/reconcile reject supplied
nonzero timestamps; updates ignore them as inputs. Timestamp-only touches do not
invalidate jobs. See [definition administration](registry-definition-administration.md)
for the public catalog wrapper and explicit committed follow-up failure contract.

**Schema compatibility boundary:** every public PostgreSQL write and `NewEntries`
repair requires clean schema 30. Historical, dirty, or newer schemas return
`ErrUnavailable` (backend maintenance, not a caller resource conflict). Complete
[offline review, finalization and cleanup](registry-exposure-migration.md) before
using these adapters. Unknown historical spec/filter fields are still rejected
instead of being dropped. Read-only inspection does not return claims. Never
co-own definitions with an old binary. The adapter's
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

## Entries and atomic source snapshots (second slice)

`persistence.EntryReader`, `ManagedPublisher`, and `SnapshotWriter` are narrow
capabilities (or use the combined `Entries` interface). `postgres.NewEntries(ctx, pool,
maxMetaSize)` borrows the same **already migrated caller-owned** PostgreSQL pool
as `NewDefinitions`; it neither migrates nor closes it. Supply a positive byte
limit for server publisher metadata. The two constructors are independent:
create source definitions via `NewDefinitions`, pass the returned
`SourceDefinition` (including its opaque incarnation ID) into `ReplaceSnapshot`, and
read with that ID. Reusing a name after deletion generates another ID;
replacement using the old ID cannot retarget the new source. An empty snapshot
atomically removes its owned entries. `Publish` inserts one managed version
(conflicts on an existing exact kind/name/version), and `DeleteManaged` removes
only that version and recomputes its kind/name latest; neither operation
replaces the managed source's history or touches external sources. Missing
managed-singleton lookup returns `ErrNoManagedSource`, refining `ErrNotFound`;
a missing exact version with an existing singleton remains ordinary `ErrNotFound`.
The public [publication/diagnostics operations](registry-publication-diagnostics.md)
reuse these atomic writes without changing standalone assembly.

```go
entries, err := postgres.NewEntries(ctx, pool, 65536)
if err != nil { return err }
snapshot := model.Snapshot{}
snapshot.Data.Servers = []model.Server{{
    Schema: "https://example.org/server.schema.json", Name: "com.example/item",
    Version: "1.0.0", Description: "Example server",
}}
err = entries.ReplaceSnapshot(ctx, source, snapshot) // source is from CreateSource
if err != nil { return err }
page, err := entries.ListEntries(ctx, persistence.ListOptions{
    SourceID: source.ID, Kind: persistence.ServerKind, Limit: 50,
})
if err != nil { return err }
_ = page // raw source-owned records, not an authorized consumer view
```

These methods are **raw catalog storage primitives, not an authorization
boundary**. The host must apply its own policy before allowing callers to
read or mutate entries; no claims, tenant, user, JWT, or visibility policy is
in the public contract. Standalone consumer HTTP uses the separate
[consumer-view reader](registry-consumer-reads.md), which implements whole-name winning-source selection before search/version/latest/paging;
[unshadowed diagnostics](registry-publication-diagnostics.md) deliberately include all sources' versions. `GetEntry` takes
`VersionSelector{Exact: "latest"}` for the literal stored version, and
`VersionSelector{Latest: true}` for the highest under `model.CompareVersions`.
Lists use a source- and filter-bound opaque cursor with C/byte-lexical
`(name, version)` ordering, limit 1–100, and a 4096-byte maximum for both
consumed and emitted cursors; cursor filter binding uses a fixed-size digest so
long search strings do not make generated cursors unusable. A legacy entry whose
`(name, version)` position cannot be encoded within that bound is not pageable
at that boundary: `ListEntries` returns classifiable `ErrInvalid` instead of an
unusable continuation. It does not prune or truncate entries, and does not use a
surrogate cursor store. Paging across concurrent writes is
not a frozen snapshot. Responses do not alias caller-owned payloads. Snapshot
format validation accepts empty catalog data, rejects duplicate version keys,
and uses the upstream/ToolHive payload format rules.

Writes reuse the existing normalized entry/version/skill/plugin/MCP server
and related metadata tables, COPY staging, and serializable transaction core.
Replacing a snapshot preserves opaque version IDs for retained keys and deletes
orphans only from its source. Migration 000025 keys latest pointers by
`(source_id, entry_type, name)` and adds columns for previously dropped
server schema/icon sizes/transport variables and skill provenance. It retains
old latest pointers; Migration 000025 is **offline maintenance**: stop and drain **all** old-binary
readers and writers, back up the database, apply the migration, then initialize
the new backend and complete its pointer repair **before serving reads**. Old
writers are incompatible with the new non-null kind column; this is not an
online rolling-upgrade barrier. `NewEntries(ctx, pool, maxMetaSize)` explicitly repairs
and backfills every kind/name from entry versions with the approved Go total
comparator before the adapter serves reads. The standalone assembly performs
the same repair during startup. Hosts must first reach **clean schema 30** through
the [offline exposure workflow](registry-exposure-migration.md), then construct
`NewEntries(ctx, pool, maxMetaSize)` rather than importing an internal package.
Schema 25 alone is not supported by the final adapter. No schema
migration or pool close is hidden inside `NewEntries`. The down migration
**refuses** to discard cross-kind versions (even without latest pointers) or
nonempty skill provenance, server schema URLs, package/remote transport
variables, or icon sizes/theme presence; remove/migrate that data explicitly
first. There is no SQL-only
lexical approximation of latest order.

Writes and latest repair reject anything other than clean schema 30 with
`ErrUnavailable`; they never erase historical claims to make an operation eligible.
Complete the offline workflow first. Source replacement serializes source writes and rolls back
all three kinds on failure/cancellation. This unleased method is **not**
stale-fetch fencing: it returns `ErrBusy` while a live job lease owns that
source, and after its own successful replacement it invalidates previously
issued tokens for the source. Old binaries are unsupported concurrent owners
and do not enforce these leases. A private internal writer transaction seam composes the fenced
snapshot and job acknowledgement without exporting a driver `Tx` through public
interfaces. The snapshot envelope (`$schema`,
registry `version`, `meta.last_updated`) is validated but not cataloged as an
entry: only the contained entry payloads are persisted. This is not a
round-trip API for the source's original registry-file envelope.

Backend implementers can run `conformance.RunEntries` and
`conformance.RunEntriesMulti` against a fresh source/entry backend. They run
against PostgreSQL and an independent copy-on-write test-only memory backend.

## Leased synchronization jobs (third slice)

`persistence.Jobs` and `postgres.NewJobs(pool, maxMetaSize)` add the storage-neutral
job/status/lease capability. The pool remains host-owned; no constructor starts
a poller or runs global latest repair. `ClaimNext` atomically claims a due,
pollable source; `Acquire` explicitly claims any non-managed source (including
Kubernetes operator/manual ingestion); `Renew` extends
an active lease; `CommitSnapshot` atomically writes snapshot and success metadata;
`Complete` acknowledges no change; `Fail` stores only a safe failure category;
`Status` inspects the last attempt and last successful counts/hashes. Durations
are positive microseconds up to 24h; DB time is authoritative. Definition
changes and source reincarnations invalidate fetched jobs. Active leases block
unfenced `ReplaceSnapshot`. Job mutations require clean schema 30; the old
standalone coordinator is removed and old binaries must remain offline. See
[jobs and lifecycle](registry-persistence-jobs.md) for migration, retry/error
semantics, full example and conformance. [Standalone assembly](registry-standalone-assembly.md)
uses these same capabilities behind verified authentication and Cedar admission.
