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
- A `kubernetes` worker syncs on watch events and otherwise re-lists only every
  `operator.DefaultResyncInterval`; a failed sync retries at the poll interval. The API
  server ends watches routinely, so `internal/kubernetes.OperatorWorker` restarts the
  runner (with backoff) on every failure that belongs to the source: a watch ending, the
  API refusing a list or watch (Forbidden, CRDs missing, unreachable), before or after the
  first sync. Ownership errors (`ErrStale`, `ErrNotFound`) go to the runtime to replace the
  worker, and `ErrUnavailable` (storage) stops the runtime.
- Errors that belong to one source never stop sync for the others: a worker the
  runtime cannot build (a stored Kubernetes definition the operator rejects) is replaced by
  a stand-in that records the failure on that source and retries the build with backoff;
  `standalone.Runtime.Run` does not preflight. Only storage-wide failures end `Run`, and
  the server's supervisor restarts it.
- A config Kubernetes source the server cannot list at startup is logged (source, kind,
  namespace, reason) and startup continues, as in v1.5.4; invalid configuration (an
  invalid namespace) still stops startup. Admin-API Kubernetes sources get the config
  sources' namespace scoping (`THV_REGISTRY_WATCH_NAMESPACE` when none are given) and
  preflight; an invalid namespace or failed preflight is a 400.
- `managed` sources are written only by the publish/delete/claim-update handlers.
- A `syncPolicy` or `filter` on a source that cannot use it is ignored with a warning, as
  in v1.5.4 — not rejected: config sources in `internal/app/catalog.go`, admin requests and
  stored definitions through `persistence.SourceDefinition.WithoutUnusableSync`.
- One invalid stored definition is logged and skipped; it never stops sync for the other
  sources (`standalone.Runtime`).

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
- Annotations are recorded per namespace and kind (reset when the worker is rebuilt), and only
  for resources the fetcher exports, so a multi-namespace source keeps every namespace's
  overrides. When several resources annotate the same server name, the last kind wins in
  v1.5.4's order MCPServer, VirtualMCPServer, MCPRemoteProxy (namespace order within a kind). The snapshot hash covers server data only, so `annotationJobs` stores a digest
  of the written annotation claims with the sync result and forces a full commit when the
  current digest differs; otherwise an annotation-only change would complete as unchanged
  and the claims hook would never run. The digest lives in storage, not process memory, so
  every replica compares against what was actually written.

**Detect**: CRD ingestion that proceeds on a malformed annotation; claims written for K8s
entries outside the claims hook.

## 4. Validation Is Split: Source Policy, Then Storage Safety

Synced data keeps v1.5.4's per-source acceptance. Tightening it silently drops or
fails catalogs that upgraded users relied on, so the strict publication rules
(`persistence.ValidateSnapshot`: server name format, required description, no
duplicate packages/remotes/icons, full wire schemas) apply to publish only.

**What must hold:**
- Fetchers apply their source's policy and nothing stricter. File and Git:
  `formats.DecodeUpstream` (toolhive-core wire schema, at least one server, name and
  description required). API: decode the v0.1 pages only; no schema; zero servers is a
  valid, catalog-emptying result. Kubernetes: no schema; every resource with the export,
  URL and description annotations is exported; conversion failures are skipped with a
  warning naming the resource.
- Storage safety (`persistence.ValidateStorableSnapshot`) runs on the *filtered*
  snapshot in `reconcile.Coordinator` and again in `CommitSnapshot`/`ReplaceSnapshot`.
  It checks only what the database and read routes need: non-empty name/version within
  the route limit, per-kind uniqueness, a `namespace/name` server shape, enum-valid
  status, valid UTF-8 without NUL. The writer collapses duplicate packages, remotes and
  icons to the first.
- Content the database rejects (SQLSTATE class 22, 54000) is `ErrInvalid` for that
  source, never `ErrUnavailable`: a data problem must not stop the sync runtime.
- Every failure is logged once by the coordinator (`Sync failed`, with category and
  cause); a validation cause names the record (kind, name, version). Returned errors and
  stored status stay categories. Fetch causes travel via `ingest.WithCause`, with URL
  userinfo, query strings and fragments removed; never log headers or credentials.

**Detect**: `ValidateSnapshot` called on a sync path; a schema check on API or
Kubernetes data; storage checks on the unfiltered snapshot; a data-dependent database
error classified as unavailable; a fetch error that drops its cause without
`WithCause`, or one whose message includes it.

**Instead**: put a new acceptance rule in the one source's fetcher, and a new storage
requirement in `ValidateStorableSnapshot` with the database or route reason beside it.
