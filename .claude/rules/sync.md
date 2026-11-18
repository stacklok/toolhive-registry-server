---
paths:
  - "internal/sync/**/*.go"
  - "internal/sources/**/*.go"
  - "internal/kubernetes/**/*.go"
  - "internal/app/catalog*.go"
  - "pkg/registry/reconcile/**/*.go"
  - "pkg/registry/ingest/**/*.go"
  - "pkg/registry/operator/**/*.go"
---

# Sync Pipeline

Applies to ingestion: the library's leased sync jobs (`pkg/registry/reconcile`,
`pkg/registry/ingest`, `pkg/registry/operator`), the server's runtime that supervises them
(`internal/sync/standalone`), and the shared transactional writer (`internal/sync/writer`).
These rules keep the registry internally consistent under failure and avoid mixing synced
and non-synced source behavior.

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
- Anything the server keeps per entry (claims) is written in that same transaction, through
  the library's `postgres.EntryWriteHook` (`internal/service/db.ClaimsHook`), never in a
  separate step after commit.

**Detect**: sync code calling `tx.Commit()` before orphan deletion; separate transactions
for different stages of a single sync; isolation level set to anything other than
`pgx.Serializable`; early returns between staging and commit that don't roll back.

**Breaks if violated:** consumers observe registries with half-imported entries or dangling
version rows; sync failure becomes an outage rather than a retry-able condition.

## 2. Each Source Type Has One Ingestion Path

The library's runtime owns all ingestion. There is no second writer for synced data.

**What must hold:**
- git, api and file (path or URL) sources are polled on their `syncPolicy` interval by
  leased jobs (`reconcile.Coordinator`, claimed through `postgres.Jobs`); a lease fences
  concurrent replicas.
- Inline `file.data` sources and `kubernetes` sources run as dedicated workers started by
  `standalone.Runtime`; the runtime refreshes them when definitions change.
- `managed` sources are written only by the publish/delete/claim-update handlers.
- A `syncPolicy` or `filter` on a source that cannot use it is ignored with a warning, as
  in v1.5.4 — not rejected (`internal/app/catalog.go`).

**Detect**: code that writes synced entries outside `postgres.Jobs`/`Entries`; a new
polling loop or leader election outside the runtime; config validation that rejects
`syncPolicy` on managed/K8s sources.

## 3. K8s Entry Claims Come From the Annotation, Else the Source

v1.5.4's code gave a Kubernetes entry the `toolhive.stacklok.dev/authz-claims` annotation's
claims when present and the source's claims otherwise; the server keeps that behaviour.

**What must hold:**
- CRDs opt in to registry export via `toolhive.stacklok.dev/registry-export=true`. Without
  that annotation, the resource is ignored.
- A valid annotation replaces the source's claims for that entry; `{}` and `null` are
  stored as written. `internal/kubernetes.AnnotationClaims` records them during a fetch and
  the claims hook applies them at commit.
- Malformed claims JSON (or a nested value) causes that CRD to be skipped with a warning
  log — it is not fatal for the source.
- A change to a Kubernetes source's config claims forces a full resync, so annotated
  entries keep their own claims.

**Detect**: CRD ingestion that proceeds on a malformed annotation; claims written for K8s
entries outside the claims hook.

## 4. Fetchers Validate Before They Return

`pkg/registry/ingest` and `pkg/registry/operator` return a snapshot only when it parses and
passes `persistence.ValidateSnapshot`. The writer trusts its input.

**Detect**: a fetcher that returns an unvalidated snapshot; writer code that re-validates
what a fetcher already returned.

**Instead**: validation is the fetcher's responsibility. The writer assumes valid input
and focuses on transactional correctness. Registry files may omit a server's `$schema`;
validation accepts that, as v1.5.4 did.
