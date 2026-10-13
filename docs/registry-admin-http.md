# Reusable admin HTTP (#913)

`pkg/registry/httpapi.AdminRouter` finishes the reusable HTTP capability alongside
[consumer HTTP](registry-consumer-http.md). Standalone now selects both behind
mandatory Cedar admission after [offline preparation/finalization and schema-30 migration](registry-exposure-migration.md).
Public routers start no listener and provide no implicit admission; see
[standalone assembly](registry-standalone-assembly.md) for preparation and ownership.

## Capabilities and lifecycle

Supply `AdminOperations` with four independently replaceable application capabilities:

| Field | Consumed operations | Default application |
| --- | --- | --- |
| `Sources` | Source create/get/list/update/delete | `catalog.NewDefinitionAdmin` |
| `Views` | Named view create/get/list/update/delete | `catalog.NewDefinitionAdmin` |
| `Publisher` | Publish and exact managed-version deletion | `catalog.NewPublisher` |
| `Diagnostics` | Complete source/view version projections and status-only lookup | `catalog.NewDiagnostics` |

A definition admin may share the source and view slots, or each can be replaced
separately. There is no hidden fallback backend, consumer reader, runtime manager,
readiness, global `RegistryService`, auth configuration or identity contract.
Constructors perform no IO, start no goroutines, bind no listeners, migrate nothing
and own/close no resources. All capabilities must be initialized: nil errors name
the missing slot; typed nil implementations/handlers are outside the caller contract.
No reflection-based dependency probing is performed by the admin constructor.

`AdminOptions` reuses `ConsumerOptions`: ordinary middleware and an optional
`Decorate(RouteDescriptor, http.Handler)` called once for every actual registration.
Templates are relative to the host's `/v1` mount. The router is policy-free, **not
self-authenticating or automatically audited**. This is an embedding boundary,
not an authorization omission to fill with library JWT/Cedar/claims logic.

## Explicit host mount, admission and audit

An executable version of this example, with independent application replacements
and admission/audit tests, is in
[`httpapi/testdata/external/assembly_test.go`](../pkg/registry/httpapi/testdata/external/assembly_test.go).
It compiles and runs in a separate Go module, without registry-internal imports.
Hosts supply real authentication, fail-closed route admission and safe audit sinks:

```go
func MountAdmin(
    ops httpapi.AdminOperations,
    authenticate func(http.Handler) http.Handler,
    admit func(context.Context, httpapi.RouteDescriptor) bool,
    audit func(context.Context, httpapi.RouteDescriptor, httpapi.MutationResult, int),
) (http.Handler, error) {
    if authenticate == nil || admit == nil || audit == nil {
        return nil, errors.New("host authentication, admission and audit wrappers required")
    }
    admin, err := httpapi.AdminRouter(ops, httpapi.AdminOptions{
        Middleware: []func(http.Handler) http.Handler{authenticate},
        Decorate: func(d httpapi.RouteDescriptor, next http.Handler) http.Handler {
            return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                rw := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
                var mutation httpapi.MutationResult
                defer func() { audit(r.Context(), d, mutation, rw.Status()) }()
                if !admit(r.Context(), d) {
                    http.Error(rw, "forbidden", http.StatusForbidden)
                    return
                }
                ctx := httpapi.WithMutationObserver(r.Context(), func(result httpapi.MutationResult) {
                    mutation = result
                })
                next.ServeHTTP(rw, r.WithContext(ctx))
            })
        },
    })
    if err != nil { return nil, err }
    mux := chi.NewRouter()
    mux.Mount("/v1", admin)
    return mux, nil
}
```

Imports: standard `context`, `errors`, `net/http`; `github.com/go-chi/chi/v5` and
its `middleware` package; public `pkg/registry/httpapi`. The returned handler is
unstarted. The host owns listener/TLS, identity verification, admission, audit
attribution, error handling, shutdown and dependencies. Never log request bodies,
source configs, payload metadata or callback causes. Source config/inline data are
administrative data and may be sensitive. Authorize by **method and registered
template/domain operation**, not fragile raw-URL matching. Fail closed on unknown
routes/actions. Authentication failures and unmatched routes need outer host audit
coverage too; the per-route example covers admitted and denied matched operations.

`WithMutationObserver` invokes a synchronous callback once for each known committed
mutation, before HTTP is written. It provides actual `create`/`update`/`delete`/
`publish` intent, the resolved stable audit event, and `Committed: true`, even for a
503 partial commit. Do not derive the PUT audit event exclusively from HTTP status,
or treat all non-2xx responses as no mutation. Callbacks must not panic or retry
mutations. There is no user identity or payload in the observer. Noncommitted failures
produce no mutation observation; the host still audits the route/status.

**Standalone retains mandatory authentication/admission and existing audit
coverage when mounting this router.** This example is not a standalone Cedar
implementation or permission to expose unprotected administration. `/v1/me` and
entry-claims endpoints are not mounted here; standalone retains its separately
protected static 410 tombstones. The consumer router never implicitly includes
admin, and neither router mounts health/readiness/version/metrics endpoints.

## Exact route inventory

`AdminRoutes()` is a fresh inventory, cross-checked in tests against the twelve
retained actual registrations using a fixed, explicit inventory, not legacy routers.
There are **no trailing-slash aliases**.
Source and view resources use `name`; exact deletion also identifies `KindParam:
"type"`. Publication has a body union, not a path kind discriminator.

| Method | Template (under `/v1`) | Domain operation | Stable audit event(s) |
| --- | --- | --- | --- |
| GET | `/sources` | list | `source.list` |
| GET | `/sources/{name}` | get | `source.read` |
| PUT | `/sources/{name}` | upsert | `source.create` / `source.update` |
| DELETE | `/sources/{name}` | delete | `source.delete` |
| GET | `/sources/{name}/entries` | list-entries | `source.entries.list` |
| GET | `/registries` | list | `registry.list` |
| GET | `/registries/{name}` | get | `registry.read` |
| PUT | `/registries/{name}` | upsert | `registry.create` / `registry.update` |
| DELETE | `/registries/{name}` | delete | `registry.delete` |
| GET | `/registries/{name}/entries` | list-entries | `registry.entries.list` |
| POST | `/entries` | publish | `entry.publish` |
| DELETE | `/entries/{type}/{name}/versions/{version}` | delete | `entry.delete` |

PUT descriptors expose `AuditCreateEvent` and `AuditUpdateEvent`, rather than
inventing a new generic upsert audit event. The observer resolves the actual event.

## Compatibility and deliberate corrections

- Source/view envelope keys, config blocks, source type inference, ordered `sources`,
  creation type and storage timestamps retain legacy shapes, **without resource
  claims or caller identity**. `Origin` maps to `creationType`; no source ID is
  returned. Actual source `type` and `sourceType` are lower-case (not stale comments
  suggesting `REMOTE`/`MANAGED`). Kubernetes config retains the actual legacy
  capitalized `Namespaces` JSON key. Timestamps use Go JSON time serialization and
  storage-assigned UTC metadata; historical NULLs remain zero.
- Source requests accept exactly one of `git/api/file/managed/kubernetes`, optional
  `syncPolicy.interval` and `filter`. DNS source names and referenced source names,
  source specs, durations, file alternatives and inline upstream catalogs,
  publication names/versions/payloads and URL-decoded path parameters are validated
  **before** calling applications. The existing public definition contract requires
  positive microsecond-resolution sync schedules and forbids schedules/filters on
  managed, Kubernetes and inline sources; HTTP returns 400 for those unsupported
  definitions rather than weakening that contract to legacy permissive behavior.
  Credential references remain `username`/absolute `passwordFile`; HTTP performs no
  secret-file access and does not accept/store a plaintext password field.
- Read-only definition `id`, `origin`, `creationType`, name and timestamps are ignored
  on input, matching the old request type's unknown-field behavior. The path sets
  the name; clients cannot select CONFIG ownership. HTTP does not pre-read before
  mutations: application/backend operations enforce immutable ownership, source
  kind/file-location kind, singleton and stale-incarnation constraints atomically.
- Top-level JSON `claims` is rejected **by presence**, case-insensitively, including
  escaped key spellings, null, empty objects/arrays and duplicate keys. All duplicate
  case-insensitive top-level keys are rejected. Dotted unknown keys such as
  `claims.team` are not the retired JSON field and remain ignored. Arbitrary nested
  server/skill/plugin metadata named `claims` remains valid. Unknown upstream
  extension fields are not rejected with `DisallowUnknownFields`.
- Bodies must be one JSON object, with no trailing JSON or garbage. Malformed/empty
  bodies return 400; bodies exceeding `MaxAdminBodyBytes` (4 MiB) return 413. Exactly
  one non-null server/skill/plugin payload is required. Publication retains the raw
  legacy public payload response (not the consumer server response envelope); skill
  and plugin publication retain their existing version metadata. Deletes map
  `server/skill/plugin` to `MCP/SKILL/PLUGIN` and always delete a literal exact version,
  **including `latest`**, never a floating latest selector.
- PUT returns 201 on create, 200 on update. **Only `ErrAlreadyExists`** triggers update.
  Unrefined conflicts (singleton, stale ID, legacy guard) return 409, never a fallback
  update. `ErrConfigOwned` is 403; `ErrKindChange`/`ErrInvalid` are 400; `ErrInUse` is
  409; not found is 404, except `ErrReferenceNotFound` (a missing referenced source on a view write)
  is 400. Replacement applications must preserve that refinement; it remains
  `errors.Is(err, ErrNotFound)` compatible, without HTTP pre-reads.
  `ErrNoManagedSource` precedes its `ErrNotFound` parent and preserves publication-
  disabled 500 semantics; a missing exact delete version is 404. Generic errors are
  sanitized 500; cancellation/deadline are sanitized 408/504.
- `catalog.PostCommitError` is handled **before ordinary `errors.Is` mappings**:
  HTTP 503, `{"error":"mutation committed; follow-up failed","committed":true,
  "resource":...}`. Create/update include the stored legacy resource projection;
  deletion has no resource. Callback errors/messages never escape. This is not a
  rollback, not an instruction to retry the mutation, and not an automatic retry.
  Rediscover persisted state before deciding how to retry sync/refresh. Mutation
  responses do not requery status, preventing post-write lookup errors from hiding
  a successful commit. Hosts must use the observer for accurate partial-commit audit.

## Diagnostics consistency and secrecy

Source diagnostics group every supplied version by `(kind,name)`, retaining the
supplied group/version order; view diagnostics retain every row, source name and
position, including shadowed collisions. Empty entry lists are JSON `[]`. No cap,
cursor, consumer dedup or personalized selection is applied. Diagnostic payloads
include the legacy name/version/title/description/timestamps, not entry payloads,
resource claims or source/lease identities.

Source read/list combines definitions with **separate** authoritative status calls.
Metadata and job state can change between calls; this is not a global atomic snapshot.
The source ID must match status's incarnation; mismatch returns sanitized 500,
lookup errors are surfaced (404 for a disappeared source), and no retry/requery
pairs unrelated observations. Status serializes only phase, attempt/completion
timestamps, attempts/counts and allowlisted failure categories in the existing
`syncStatus` schema. Hashes, baseline/lease metadata, raw failure messages and source
config are never serialized through job structs. Pending is explicitly `pending`;
unknown phases are `unknown`. This deliberately stops swallowing status failures
or exposing legacy free-form failure text. Counts are catalog counts, not per-user
scope. Each complete entry diagnostic call has its own storage snapshot contract.

## Verification and remaining work

Tests cover the full route/decorator manifest and host middleware, independent
capability replacements, fixed claims-free wire expectations, create/update/error and
partial-commit audit behavior, retired-field detection and body/path validation.
Real HTTP + default catalog + PostgreSQL tests exercise unshadowed collisions,
all-kind singleton publication, exact deletion/latest/history, ownership/location
constraints and no mutation for invalid bodies. An external module proves public
replacement/mounting and an import graph excluding registry-internal config/auth,
PostgreSQL and Kubernetes from the HTTP core.

The active [standalone assembly](registry-standalone-assembly.md) owns mandatory
admission, exposure preparation, exclusive leased lifecycle, protected tombstones and
listener/operational separation. Default OpenAPI is generated from active public DTOs,
metadata handlers and tombstones. Physical schema cleanup remains a separate final slice.
