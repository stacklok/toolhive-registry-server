# Registry component architecture

The registry separates source-owned server, skill and plugin versions from named
consumer views. A view is an ordered selection of sources, not a tenant or privacy
boundary. Public components provide nonpersonalized catalog operations; a host owns
identity, admission and any personalized selection.

## Packages and dependency direction

All reusable components live under `pkg/registry`:

| Package | Responsibility and consumed contracts |
| --- | --- |
| `model`, `formats` | Payloads, version ordering, validation and serialization; no storage, HTTP, application or Kubernetes dependency. |
| `persistence` | Source/view definitions, entry reads and atomic writes, view selection, diagnostics and leased jobs. Contracts use public payloads, not SQL or identity types. |
| `catalog` | `Reader` over `persistence.ViewEntryReader`; `DefinitionAdmin` over `Sources` and `Views`; `Publisher` over `ManagedPublisher`; `Diagnostics` over `EntryDiagnostics` and `SourceStatusReader`. |
| `httpapi` | Consumer and admin routers over host-supplied application capabilities; request validation and wire serialization. |
| `ingest` | Git/API/file fetch adapters with replaceable transport, file and secret-loading dependencies. |
| `reconcile` | `Coordinator` consumes `persistence.Jobs` and its own `Fetcher` interface to fetch, validate, filter and commit snapshots. |
| `operator` | Optional ToolHive CRD conversion, full-scope fetching and watch runner over Kubernetes clients and `Jobs`. |
| `postgres` | Explicit implementation of persistence contracts; borrows a migrated PostgreSQL pool. SQL and transaction types stay inside the adapter. |

HTTP consumes catalog-shaped capabilities; catalog and reconciliation consume
persistence contracts, never the PostgreSQL implementation. The portable
model/formats/persistence/catalog/httpapi graph imports no registry-internal code,
standalone configuration/authentication, PostgreSQL or Kubernetes. Ingestion and
reconciliation reuse internal low-level Git, HTTP, filtering and shutdown helpers.
PostgreSQL uses `internal/db` and the caller-owned transactional machinery in
`internal/sync/writer`; Kubernetes dependencies belong to the optional operator and
standalone Kubernetes boundary.

The actual HTTP contracts are
[`ConsumerPages`](../pkg/registry/httpapi/consumer.go) (`List`, `Get`, `ListVersions`)
and [`AdminOperations`](../pkg/registry/httpapi/admin.go), whose `Sources`, `Views`,
`Publisher` and `Diagnostics` slots are independently replaceable. `catalog.Reader`
satisfies `ConsumerPages`; hosts can instead supply a coherent selection service.
Neither router constructs a backend or listener. See [consumer HTTP](registry-consumer-http.md)
and [admin HTTP](registry-admin-http.md) for signatures, routes and error mapping.

## Data and ownership invariants

- **Source-local data.** A source owns `(kind, name, version)` and related metadata.
  CONFIG/API definition ownership is immutable; config reconciliation cannot
  overwrite API-owned definitions. Individual publication/deletion targets the
  singleton managed source, not a view or an external source.
- **Whole-name priority.** The earliest view source wins every version of a
  `(kind, name)` before search, version/latest selection or pagination. Diagnostics
  show every source's versions, including shadowed duplicates. HTTP preserves the supplied page's order and supported
  cursor metadata; it never filters, deduplicates or refills it. Cursors do not
  promise snapshot isolation across requests.
- **Exact versions.** Stored spellings remain exact. `model.CompareVersions` orders
  parseable semver above non-semver, using semantic precedence then raw lexical
  spelling for ties; non-semver compares lexically. Mutations maintain source-local
  latest pointers, and `postgres.NewEntries` repairs them before returning.
- **Atomic fenced ingestion.** `Jobs.CommitSnapshot` commits one complete source
  snapshot and success acknowledgement together. Source incarnation, generation,
  lease identity and authoritative expiry reject stale work. Failure or cancellation
  preserves the last good data. A valid empty snapshot is distinct from a failed or
  incomplete fetch. Direct `SnapshotWriter.ReplaceSnapshot` is not stale-fetch
  fencing: it rejects active leases and invalidates prior tokens on success.
- **Explicit lifecycle.** Constructors start no application workers or listeners.
  Supplied pools, clients, managers and telemetry remain host-owned. Hosts start,
  cancel and join work before closing dependencies. `NewEntries` performs database
  repair during construction; it does not migrate or close the pool.

Detailed contracts: [persistence](registry-persistence.md),
[jobs/fencing](registry-persistence-jobs.md), [consumer reads](registry-consumer-reads.md),
[definition administration](registry-definition-administration.md),
[publication/diagnostics](registry-publication-diagnostics.md) and
[ingestion/operator lifecycle](registry-ingestion.md).

## Available compositions

Reuse only [formats](registry-formats.md), run reconciliation without HTTP, implement
the persistence capabilities your application needs, or mount routers over a replacement
application. Both dependency substitution and application replacement use the public
contracts above. [Embedding guidance](registry-embedding-validation.md) links executable
external-module examples and verification commands.

The standalone CLI uses `internal/app.NewCatalogApp` to assemble these same public
components with configuration, secrets, storage preparation, leased supervision,
audit and two HTTP servers. Public traffic defaults to verified OAuth with mandatory
evaluated built-in allow-all Cedar; admin is disabled by default and requires OAuth
when enabled. A custom policy replaces the built-in policy entirely and fails closed.
Operational probes and metrics use the separate internal server. Libraries do not
require Cedar: hosts own authentication and may supply `httpapi.Authorizer` through
the HTTP decorator seam.

See [standalone assembly](registry-standalone-assembly.md) for preparation and shutdown,
[Cedar authorization](registry-cedar-authorization.md) for admission, and
[contract tests](registry-contract-tests.md) for verification. Existing catalogs
require the [offline exposure migration](registry-exposure-migration.md) before
schema-30 writes; neither constructing public components nor selecting a policy
supplies exposure consent. Old and new binaries must not co-own sources.
