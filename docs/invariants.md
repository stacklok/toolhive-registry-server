# Registry Server — Design Invariants

This document lists the design decisions that shaped the current codebase and that should
survive future refactors. Each invariant names a decision, states *why* it was made, and
flags what breaks if it is undone. It is not a reference manual — it is the shortlist of
constraints a reviewer should check against before approving a change.

Use this doc when:
- You are about to change something that feels arbitrary — check here first. It may be
  load-bearing.
- You are reviewing a PR that touches layering, authorization, the upstream API shape, or
  the sync path.
- You are designing a new feature and want to know which existing guarantees constrain you.

This document is intentionally terse. Point-form rules, one-paragraph rationale each.

---

## Data model at a glance

Enough context to read the rest of this doc. Detail lives in code.

```
External data                         Consumers
─────────────                         ─────────
  Git repo ───→ ┌──────────┐
  API ────────→ │          │
  File ───────→ │ Sources  │ ──→ ┌────────────┐    ┌──────────────┐
  K8s CRDs ──→  │ (ingest) │     │ Entry Pool │ ──→│  Registries  │ ──→ Clients
  API publish → │          │ ──→ │   (DB)     │    │   (views)    │
                └──────────┘     └────────────┘    └──────────────┘
```

- **Source** — a named ingestion pipeline. Types: `git`, `api`, `file`, `kubernetes`,
  `managed`. Only operators/admins interact with sources.
- **Entry pool** — two tables: `registry_entry` (the `(type, name, source)` triple, with
  claims) and `entry_version` (per-version payload). `type` is `server` or `skill`.
- **Registry** — a named, consumer-facing view. Lists an ordered set of sources. Access-
  gated by claims. All customer traffic hits `/registry/{name}/v0.1/...`.
- **Claims** — `map[string]any` where values are scalars or flat arrays of scalars.
  Attached to sources, registries, and entry names. Drive visibility.
- **Managed source** — special source type that is a container for entries published via
  the API. Exactly one allowed per instance.
- **Creation type** — `CONFIG` (declared in YAML) or `API` (created via PUT). A resource is
  one or the other, never both.

---

## 1. Layering and boundaries

### 1.1 Three-layer data model: sources → entry pool → registries

**Why:** Decouples ingestion from presentation. Operators manage sources; consumers see
registries; the entry pool in the middle is the only shared state.

**What must hold:**
- Sources are never exposed to consumer-facing paths (`/registry/...`). They live under
  `/v1/sources` (admin).
- Registries are the only consumer-facing concept. They reference sources by name and
  produce the view clients query.
- A single entry pool (the `registry_entry` + `entry_version` tables) feeds every registry —
  no per-registry tables.

**Breaks if violated:** admin concerns (sync config, source CRUD, source filters) leak into
consumer URLs and JSON responses. CDN/caching story gets worse. The upstream-compliance
surface (§4.1) becomes unmaintainable.

### 1.2 Layer direction: API → service → storage/sources

**Why:** Keeps HTTP concerns out of business logic and lets the service layer be tested
without spinning up a web server or a database.

**What must hold:**
- `internal/api/**` owns request/response shapes, auth middleware wiring, URL routing. It
  must not call the database or `sources.*` directly — only via `service.RegistryService`.
- `internal/service/**` is the business-logic seam. It owns claim checks, dedup, name
  allocation, cursor decoding. It depends on storage interfaces, not concrete DB drivers.
- `internal/sources/**` owns *only* ingestion — fetching + validating upstream-format data.
  Sources must not know about registries, claims filtering, or HTTP.
- Kubernetes client-go imports live under `internal/kubernetes/**` and the K8s sync path.
  They must not leak into the service or API packages.

**Breaks if violated:** unit tests grow dependencies on Postgres / K8s / HTTP; refactors
become global; swapping storage backends becomes infeasible.

### 1.3 Two HTTP servers: public and internal

**Why:** Kubernetes probes and operational endpoints must not be behind JWT auth, must not
be rate-limited alongside traffic, and must not show up in consumer API docs.

**What must hold:**
- Public server (default `:8080`): all `/v1/`, `/registry/`, `/openapi.json`,
  `/.well-known/...` routes. Carries auth and audit middleware.
- Internal server (default `:8081`): `/health`, `/readiness`, `/version` only. No auth, no
  audit, no rate limiting.
- Internal endpoints are never reachable from the public port.

**Breaks if violated:** probes fail under load, probes require auth (breaks K8s), operational
endpoints become part of the customer-facing API contract.

---

## 2. Data model

### 2.1 Claims live on entry **names**, not entry versions

**Why:** Filtering every paginated query against millions of per-version claim blobs is
expensive. Names are a much lower-cardinality set, so name-level claims make per-user
filtering tractable without indexes on JSONB.

**What must hold:**
- `registry_entry.claims` (the `(type, name, source)` row) is the claims source of truth.
  `entry_version` has no claims column.
- All versions of a given `(type, name, source)` share one claim set.
- Changing claims on an entry name affects every version at once (that is the intent of
  `PUT /v1/entries/{type}/{name}/claims`).

**Breaks if violated:** per-user filtering cost explodes; the rule that "a name's first
publish defines its claims for every future version" (§3.5) silently diverges across rows.

### 2.2 Source type is inferred from which config block is present

**Why:** Avoids a redundant `type:` field that can disagree with the config shape. Having
exactly one block makes the type unambiguous and removes a whole class of validation bug.

**What must hold:**
- `SourceConfig` has exactly one non-nil type block (`git`, `api`, `file`, `managed`,
  `kubernetes`) — enforced by `validateSourceTypeCount`.
- `SourceConfig.GetType()` is the single arbiter of source type at runtime. All code that
  needs the type must call it — never reimplement the dispatch.
- Adding a new source type requires updating, at minimum: `SourceType` constants,
  `SourceConfig` struct, `GetType()`, `validateSourceTypeCount`, `validateSourceSpecificConfig`,
  the handler factory (`sources.CreateHandler`), and `IsNonSyncedSource()` if applicable.

**Breaks if violated:** a `type:` field that can disagree with the block shape; handler
dispatch that drifts from validation; the config surface becomes harder to document.

### 2.3 At most one managed source per instance

**Why:** The managed source is the single container for API-published entries. Allowing
multiple would make "where does a publish go?" ambiguous and fork the publish code path.

**What must hold:**
- Config-load validation rejects more than one `managed: {}` block
  (`config.go` — `managedCount > 1` check).
- Runtime `getManagedSource()` errors if the DB contains more than one managed source (defense
  in depth — the check exists even though config validation should prevent it).
- Publishing is **disabled** (returns `ErrNoManagedSource`, 500) when no managed source is
  configured.

**Breaks if violated:** publish becomes non-deterministic; migrations that add a second
managed source silently succeed and diverge.

### 2.4 Claim values must be flat: scalar or flat array of scalars

**Why:** Keeps claim matching expressible as simple Boolean statements. Supporting nested
objects would force recursive traversal and break the "AND across keys, OR within arrays"
contract that the whole authz story rests on.

**What must hold:**
- `ValidateClaimValues` rejects nested objects at every write boundary (config load, source
  create/update, registry create/update, entry publish).
- Anywhere a map value is consumed as a claim, treat it as `string | []string` only.

**Breaks if violated:** claim matching semantics become undefined; validation drifts from
runtime; JWT-vs-claim comparison has to grow a mini query language.

### 2.5 Config-managed and API-managed resources are mutually exclusive

**Why:** Two writers for the same row is an operational trap. YAML is the operator's source
of truth; the API is the dynamic/team-owned surface. Mixing the two leads to silent
overwrites on restart.

**What must hold:**
- `creation_type` (`CONFIG` | `API`) is set at create time and never changes.
- API writes to a `CONFIG` resource return `ErrConfigSource` / `ErrConfigRegistry` (403).
- Config-load reconciliation only touches `CONFIG`-typed rows. It must not upsert over an
  `API`-typed resource with the same name.

**Breaks if violated:** operators lose their config edits on restart; API users lose their
resource to an unrelated config change.

### 2.6 Source names must be valid DNS subdomains (RFC 1123)

**Why:** Source names are used as suffixes for Kubernetes lease names during leader
election. Invalid DNS labels cause lease creation to fail silently in some K8s versions.

**What must hold:**
- `IsValidDNSSubdomain` gates every source name at write time (config + API).
- Maximum 63 chars, lowercase alphanumeric + hyphens, no leading/trailing hyphen.

**Breaks if violated:** leader election fails on sources with awkward names; the failure
mode is opaque ("no leader") rather than a validation error.

---

## 3. Authorization

### 3.1 Two-step gate: authentication then authorization

**Why:** "Who are you?" (JWT validation) is independent from "what can you do?" (role +
claim matching). Conflating them produces a config surface that is hard to reason about
and defaults that are hard to secure.

**What must hold:**
- `auth.mode` controls authentication: `anonymous` or `oauth`.
- `auth.authz` (optional, separate block) controls authorization: roles and their claim
  maps.
- Default `auth.mode` is **oauth** — secure-by-default. Switching to `anonymous` is an
  explicit choice.
- Without `auth.authz`, authenticated users implicitly get all roles (open mode). Role
  enforcement is opt-in.

**Breaks if violated:** a single `auth:` block conflating identity and permissions; a
default that allows anonymous access; config migrations that silently drop role gates.

### 3.2 Roles come from the IdP — no local role storage

**Why:** Avoids a parallel RBAC system that drifts from the customer's source of truth.
Roles are mapped from JWT claims at request time.

**What must hold:**
- Four roles, fixed in config: `superAdmin`, `manageSources`, `manageRegistries`,
  `manageEntries`.
- Each role is a list of claim maps (OR across maps, AND within a map, OR within an array).
- No role CRUD API, no role table. Adding a role requires a code change.
- `ResolveRolesMiddleware` runs after JWT extraction and injects resolved roles into the
  request context.

**Breaks if violated:** role state forks between the IdP and a local store; admins
"promote" users only to find the JWT still says otherwise; audit trails lose the link
between authentication and role assignment.

### 3.3 Claim matching is uniform across all use sites

**Why:** Five different matching algorithms for "does this JWT satisfy these claims?" is a
recipe for subtle bypass bugs. Use one.

**What must hold:**
- **AND** across keys, **OR** within array values, **absent key = not checked** — the same
  rule is used for: registry access gate, per-user entry filtering, role resolution, and
  write-path subset validation.
- Use `claimsContain` / `validateClaimsSubset` / `validateClaimsSubsetBytes` (in
  `internal/auth/claims.go`) — do not reimplement containment checks ad hoc.
- Super-admin bypasses every claim check, uniformly.

**Breaks if violated:** a user sees an entry via one endpoint but not another; a user can
create a resource they cannot read; audit reports diverge from runtime behavior.

### 3.4 Default-deny when authz is enabled

**Why:** If an entry has claims the caller's JWT doesn't cover, not seeing it is the safe
default. The opposite (default-allow on missing data) turns misconfiguration into a data
leak.

**What must hold:**
- Per-user filtering on a row with non-empty claims fails closed — the row is invisible to
  callers whose JWT doesn't cover those claims.
- Empty-claims rows are visible to everyone authenticated (no gate). This is deliberate —
  operators can create "open" entries — but publishing with empty claims is **forbidden**
  when authz is active (see §3.5).
- Registry access gate returns 403 (not 404) when the caller fails it. 404 would leak
  registry existence.

**Breaks if violated:** operator mis-tags a source → org-confidential entries become visible
to the whole tenant.

### 3.5 First publish owns the name; claims immutable on re-publish

**Why:** Entry names are allocated on a first-come, first-served basis. Allowing later
versions to ship with different claims would let a second publisher change the name's
visibility without explicit intent, and would contradict §2.1 ("all versions share one
claim set").

**What must hold:**
- `POST /v1/entries` with empty/missing `claims` returns 400 when the caller has JWT claims
  in the context (auth is active).
- In anonymous mode, claims remain optional — the filter is bypassed so "no claims" means
  "open", which is the only sensible interpretation.
- Subsequent publishes of an existing name must match the allocated claims exactly
  (`ErrClaimsMismatch`, 409). Claim changes go through
  `PUT /v1/entries/{type}/{name}/claims`, not through re-publish.

**Breaks if violated:** writers accidentally publish world-visible entries; the per-name
claim invariant drifts across versions; clients observe inconsistent visibility depending
on which version row they land on.

### 3.6 Subset validation on every write

**Why:** Prevents privilege escalation. A user cannot create a source/registry/entry whose
claims are broader than the user's own JWT — otherwise a user could grant themselves access
they didn't have.

**What must hold:**
- Every write (source create/update, registry create/update, publish, delete published
  entry, claim update) validates that the resource's claims are a subset of the caller's
  JWT.
- Super-admin is exempt from subset checks — but not from role gates.
- Registry creation additionally requires the caller to cover each referenced source's
  claims (otherwise a user could "see" entries they cannot manage directly).

**Breaks if violated:** users escalate their reach by creating permissive resources;
compliance stories around "least privilege" become unenforceable.

---

## 4. API surface

### 4.1 Consumer API is upstream-spec-compliant; admin API is ours

**Why:** The whole reason this server exists is to be a drop-in MCP Registry for consumers.
Breaking spec compatibility means clients built against the upstream spec silently fail.
Admin endpoints are our concern and don't need to match anything.

**What must hold:**
- Consumer API lives under `/registry/{name}/v0.1/...` and matches the upstream MCP
  Registry v0.1 schema exactly (request params, response shapes, pagination cursor, error
  codes).
- Admin API lives under `/v1/...` — separate namespace, separate evolution, separate
  OpenAPI spec.
- Extensions to the consumer API go under `/x/{vendor}/...` (currently `/x/dev.toolhive/`).
  The `/x/` prefix signals "non-spec."
- The `{registryName}` path segment is required even if only one registry exists. The
  `/registry/` prefix avoids path collisions (e.g., a registry literally named "v1").

**Breaks if violated:** existing MCP clients break; we cannot upgrade to future upstream
spec versions without a breaking change; custom extensions become indistinguishable from
spec features.

### 4.2 Publish is a single global endpoint under /v1/, not per-registry

**Why:** An entry's visibility is determined by its claims, not by which URL published it.
A per-registry publish endpoint would imply the opposite — that publishing "into"
registry X makes the entry X's — and would break cross-registry visibility via claims.

**What must hold:**
- `POST /v1/entries` is the only publish endpoint. The payload carries a top-level `claims`
  field plus mutually-exclusive `server` or `skill` fields.
- The `server` / `skill` payload is the upstream spec shape, unchanged. Claims are
  **metadata about the entry**, not part of the entry itself — do not fold them into the
  payload.
- Published entries all land in the single managed source. They appear in any registry
  whose source list includes it, subject to claims.

**Breaks if violated:** publish becomes registry-scoped; cross-registry visibility
regresses; the upstream `server` JSON shape gets polluted.

### 4.3 Response content is personalized; pagination is per-user

**Why:** Per-user filtering means the same URL returns different data for different users.
Clients and infrastructure must not assume otherwise.

**What must hold:**
- Cursors are only valid for the same user that received them. Do not cache cursors
  cross-user or cross-request.
- Total counts in response metadata reflect only the caller's visible set.
- CDN caching of consumer responses requires `Vary: Authorization` (effectively: no cache).
  Do not add aggressive caching that ignores the caller.

**Breaks if violated:** users see entries that belong to other tenants; pagination returns
inconsistent pages; a CDN layer turns personalization into a data leak.

### 4.4 Admin endpoints always wrapped in audit middleware

**Why:** Every admin operation is security-relevant. Audit trails are a compliance
requirement, and losing an event on a new endpoint is a gap that is hard to spot.

**What must hold:**
- All `/v1/` handlers are wrapped with `auditmw.Audited` (or a variant). The event type,
  resource type, and URL-param name are declared at route registration.
- Adding a new admin endpoint without an audit wrapper should be treated as a bug.
- Audit events have stable names (e.g., `source.create`, `entry.publish`). Renaming them is
  a breaking change for downstream log consumers.

**Breaks if violated:** compliance evidence gaps; incident response cannot reconstruct
"who did what."

### 4.5 Dedup is at the `(type, name)` level, with source priority

**Why:** Mixed version histories from different sources (v1.0 from source A, v1.5 from
source B, v2.0 from source A) are confusing and expose consumers to inconsistent metadata
(descriptions, URLs, claims). One source owns a name; all of its versions win.

**What must hold:**
- When multiple sources in a registry provide the same `(type, name)`, the highest-priority
  source (earliest in the registry's source list) wins **entirely**. Versions from
  lower-priority sources for that name are invisible in that registry.
- Claims filtering runs **before** dedup — if the highest-priority source's entry fails the
  caller's claims check, the next source is promoted.
- Admin endpoints (`/v1/sources/{name}/entries`, `/v1/registries/{name}/entries`) show
  unshadowed data so operators can see what dedup hid.

**Breaks if violated:** consumers see entries whose versions straddle sources; claims-based
filtering becomes incoherent (the caller sees v1.0 from A and v1.5 from B but not v1.1
from A).

---

## 5. Sync pipeline

### 5.1 Sync runs in a serializable transaction; failure preserves previous data

**Why:** A half-applied sync (partial entries, dangling versions) is worse than a stale
registry. Atomicity + rollback-on-error means the registry is always internally consistent.

**What must hold:**
- Sync writes use `pgx.Serializable` isolation. Temp tables + `COPY` feed a single
  transaction that commits or rolls back as a unit.
- On failure (fetch error, validation error, DB error), the transaction rolls back and the
  previous good data remains visible. The `registry_sync` row records the failure.
- No partial orphan cleanup: if orphan deletion fails, the whole sync fails — do not commit
  the inserts and skip the deletes.

**Breaks if violated:** consumers observe a registry with half-imported servers or with
versions that reference deleted entries; sync failure becomes an outage rather than a
retry.

### 5.2 Non-synced source types skip the sync loop entirely

**Why:** `managed` and `kubernetes` sources don't fetch from external data — managed is an
API-written container; K8s is a controller-driven watch. Running the hash-compare / fetch
path on them is pure overhead and would require synthetic sync policies.

**What must hold:**
- `SourceConfig.IsNonSyncedSource()` returns true for `managed` and `kubernetes`. The sync
  coordinator skips these sources entirely.
- Non-synced sources do not require `syncPolicy` or `filter` in config; if present, they
  are silently ignored.
- K8s source ingestion happens via the controller-runtime reconciler in
  `internal/kubernetes/**`, not the sync coordinator.

**Breaks if violated:** empty fetches burn CPU; config validation grows an unnecessary
syncPolicy requirement; the K8s watch path forks from the managed-write path.

### 5.3 K8s source claims don't inherit; entries get claims from an annotation

**Why:** A single K8s source typically covers a whole namespace containing CRDs owned by
different teams with different visibility requirements. Inheriting source claims would
force every team to use a dedicated source (and thus a dedicated namespace). Per-CRD claims
via annotation let one source serve many claim sets.

**What must hold:**
- Source claims on `kubernetes`-type sources are used *only* for `manageSources` role
  scoping, never merged into entry claims.
- Entry claims come exclusively from the `toolhive.stacklok.dev/authz-claims` JSON
  annotation on the CRD.
- CRDs opt in to registry export via `toolhive.stacklok.dev/registry-export=true`. Without
  that annotation, the resource is ignored.
- Malformed claims JSON causes the specific CRD to be skipped with a warning — it is not
  fatal for the source.

**Breaks if violated:** either per-team claim diversity regresses (forcing more sources), or
source-level claims silently override per-CRD intent.

---

## 6. Implementation details worth preserving

### 6.1 Per-user filtering is application-level, not SQL-level

**Why:** For current data volumes (hundreds to low thousands of distinct names), streaming
rows and filtering in Go is simpler than JSONB `@>` + GIN indexes and keeps all auth logic
in one place. Swap later if volumes demand it.

**What must hold:**
- No JSONB operators (`@>`, `->>`) in production SQL queries. Claims come out as raw bytes.
- Filtering happens in `RecordFilter` chains applied during row streaming; the database
  returns candidates, Go code decides visibility.
- If you're tempted to add a JSONB operator for speed, that's a migration — not a drop-in
  change. The authz tests assume app-level semantics.

**Breaks if violated:** filtering semantics subtly diverge between code paths; index
changes become coupled to code changes; debugging "why can't user X see entry Y" becomes a
SQL `EXPLAIN` problem instead of a Go unit test.

### 6.2 Builder + option pattern for app assembly; factories for testability

**Why:** Dependency injection via functional options lets tests replace storage, sync,
auth, and handler factories without mocking the whole world. Production code uses
sensible defaults.

**What must hold:**
- `NewRegistryApp(ctx, opts...)` is the canonical assembly. `With*` options replace
  components; tests lean on `WithStorageFactory`, `WithSyncManager`,
  `WithRegistryHandlerFactory`, `WithCoordinatorOptions`.
- Factories (`RegistryHandlerFactory`, `storage.Factory`) are interfaces so tests inject
  fakes. Concrete implementations live beside them.
- When adding a new component, prefer a factory interface over a direct dependency —
  future tests will thank you.

**Breaks if violated:** tests grow real DB / real K8s / real HTTP dependencies; PRs that
refactor internals can't be reviewed in isolation.

### 6.3 Secrets via file paths, not env vars; symlinks resolved

**Why:** Env-var secrets leak into `ps`, into shell history, into process dumps. File-based
secrets integrate cleanly with K8s secrets, Docker secrets, and systemd credentials.
Resolving symlinks closes a class of traversal attack.

**What must hold:**
- Git auth password, OAuth client secret, database password (optionally): all loaded via a
  file path (`passwordFile`, `clientSecretFile`, etc.).
- Secret files are required to be **absolute paths**, and `filepath.EvalSymlinks` is
  called before reading.
- `THV_REGISTRY_DATABASE_PASSWORD` is an escape hatch for environments that can't mount
  files, but the file path is the preferred interface.
- `DatabaseConfig.LogValue()` strips passwords from logs — do not log the raw struct.

**Breaks if violated:** secrets show up in logs, dumps, or error paths; rotation requires
a restart; a symlink in a config directory becomes an arbitrary-file-read primitive.

### 6.4 HTTPS required for OAuth issuer URLs

**Why:** JWKS fetched over HTTP is trivially MITM'd, which defeats JWT validation. Allowing
HTTP as a deployment choice makes the "did we validate this token properly?" question
unanswerable.

**What must hold:**
- OAuth `issuerUrl` must be HTTPS, with two exceptions: `localhost` / `127.0.0.1` / `::1`,
  or `THV_REGISTRY_INSECURE_URL=true`.
- The env-var escape hatch is **env-only**, never loaded from YAML. This prevents someone
  from committing an insecure-allow flag to a repo.
- The flag propagates from config to `AuthConfig.InsecureAllowHTTP` to the token validator.

**Breaks if violated:** production deployments accept forged JWTs; the insecure-allow flag
becomes casually-committed config.

---

## 7. Where this list may be wrong

- **Storage-agnosticism.** The `storage.Factory` abstraction suggests plans for non-Postgres
  backends. Today, the only implementation is Postgres, and some SQL lives in service code.
  Before promising "swap the DB," actually audit.
- **K8s CRD-as-source-config.** Today, sources and registries are config-file or API only.
  If CRDs become a third creation path, the `creation_type` enum grows and §2.5 needs an
  update.
- **Filter expressiveness.** `FilterConfig` supports name+tag patterns only, no
  entry-type filter, no claim-based filter. If filters become claim-aware, §2.4 constraints
  on claim shape may need to relax.
- **Caching.** Nothing here assumes a cache exists. If a per-user or per-registry cache
  lands, §4.3 invariants around personalized responses must be designed into it.

Treat items in this section as "known holes." Update them as the code changes.