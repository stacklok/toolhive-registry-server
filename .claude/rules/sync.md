---
paths:
  - "internal/sync/**/*.go"
  - "internal/sources/**/*.go"
  - "internal/kubernetes/**/*.go"
---

# Sync Pipeline

Applies to ingestion coordination, source handlers, and the K8s watch path. These rules
keep the registry internally consistent under failure and avoid mixing synced and
non-synced source behavior.

## 1. Sync Runs in a Serializable Transaction; Failure Preserves Previous Data

Sync writes use `pgx.Serializable` isolation. Temp tables + `COPY` feed a single
transaction that commits or rolls back as a unit. A half-applied sync is worse than a
stale registry.

**What must hold:**
- Bulk insert/upsert uses temp tables populated via `COPY`, merged into target tables
  inside one serializable transaction.
- On any failure (fetch error, validation error, DB error), the transaction rolls back and
  the previous good data remains visible. The `registry_sync` row records the failure.
- Orphan cleanup runs inside the same transaction — do not commit inserts and skip
  deletions on error.

**Detect**: sync code calling `tx.Commit()` before orphan deletion; separate transactions
for different stages of a single sync; isolation level set to anything other than
`pgx.Serializable`; early returns between staging and commit that don't roll back.

**Breaks if violated:** consumers observe registries with half-imported entries or dangling
version rows; sync failure becomes an outage rather than a retry-able condition.

## 2. Non-Synced Source Types Skip the Sync Loop

`managed` and `kubernetes` sources don't fetch from external data. `managed` is an
API-written container; `kubernetes` is a controller-driven watch. Running the
hash-compare / fetch path on them is pure overhead.

**What must hold:**
- `SourceConfig.IsNonSyncedSource()` returns true for `managed` and `kubernetes`. The sync
  coordinator skips these sources entirely.
- Non-synced sources do not require `syncPolicy` or `filter` in config; if present, they
  are silently ignored.
- K8s ingestion happens via the controller-runtime reconciler in `internal/kubernetes/**`,
  not through `sync.Manager`.

**Detect**: sync coordinator code that enumerates sources without calling
`IsNonSyncedSource()`; config validation that requires `syncPolicy` on managed/K8s; a K8s
handler registered through `sources.RegistryHandlerFactory`.

**Instead**: the managed source is written only by the publish/delete/claim-update
handlers. K8s sources are populated by the reconciler using the same `SyncWriter` interface
the coordinator uses, so they share the transactional guarantees of §1.

## 3. K8s Source Claims Don't Inherit; Entries Get Claims From an Annotation

A single K8s source typically covers a namespace containing CRDs owned by different teams
with different visibility. Inheriting source claims would force one source per team.
Per-CRD claims via annotation let one source serve many claim sets.

**What must hold:**
- Source claims on `kubernetes`-type sources are used *only* for `manageSources` role
  scoping, never merged into entry claims.
- Entry claims come exclusively from the `toolhive.stacklok.dev/authz-claims` JSON
  annotation on the CRD.
- CRDs opt in to registry export via `toolhive.stacklok.dev/registry-export=true`. Without
  that annotation, the resource is ignored.
- Malformed claims JSON causes the specific CRD to be skipped with a warning log — it is
  not fatal for the source.

**Detect**: K8s sync code that reads source claims and writes them to entries; code paths
that treat a missing `authz-claims` annotation as "inherit from source"; CRD ingestion that
proceeds on malformed annotation JSON.

**Instead**: parse `authz-claims` per CRD. On parse failure, log and skip that CRD only —
other CRDs in the source continue. Missing annotation → entry has no claims (visible to
all when anonymous, invisible when authz is active — that is the intended default-deny).

## 4. Source Handlers Validate Before They Return

`sources.RegistryHandler.FetchRegistry` returns a `*FetchResult` only when the payload
parses against the upstream MCP registry schema and has at least one server. The sync
writer trusts its input.

**Detect**: handler implementations that return a `FetchResult` with a nil or empty
`Registry`; handler code that propagates raw upstream bytes without calling
`ValidateUpstreamRegistryBytes`; sync writer code that re-validates what a handler already
returned.

**Instead**: validation is the handler's responsibility. The writer assumes valid input
and focuses on transactional correctness.
