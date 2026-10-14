# Reusable consumer HTTP

`pkg/registry/httpapi` adapts **already selected** application pages to the MCP
Registry v0.1 and ToolHive skills/plugins wire formats. Standalone selects this
router through mandatory Cedar-guarded capabilities after offline exposure preparation/finalization and clean schema-30 migration.
[Admin HTTP](registry-admin-http.md) completes the reusable HTTP capability; see
[standalone assembly](registry-standalone-assembly.md) for listener, admission and lifecycle ownership.

## Host assembly

```go
// Import github.com/go-chi/chi/v5 and the public catalog/httpapi packages.
// backend implements persistence.ViewEntryReader; it is owned by the host.
reader, err := catalog.NewReader(backend)
if err != nil { return err }
consumer, err := httpapi.ConsumerRouter(reader, httpapi.ConsumerOptions{
    Middleware: []func(http.Handler) http.Handler{hostMiddleware},
    Decorate: func(route httpapi.RouteDescriptor, next http.Handler) http.Handler {
        // Map domain intent to YOUR admission/audit policy at registration.
        return hostAdmissionAndAudit(route, next)
    },
})
if err != nil { return err }
mux := chi.NewRouter()
mux.Mount("/registry", consumer)
// The host owns http.Server, listening, cancellation and shutdown.
```

Construction requires a non-nil initialized `ConsumerPages` (typed nil is rejected),
valid middleware and non-nil decorated handlers. It calls no application operation,
opens no backend, starts no goroutine and binds no listener. No admin, operational,
metadata, implicit HEAD or OPTIONS handlers are mounted. Chi returns 405 for
unsupported methods on known paths and 404 for unknown paths; neither calls the
application. A host can add its own routes or method handlers outside this router.

Admission is **entirely host-owned**. The low-level router is explicitly policy-free
when `Decorate` is omitted; do not mount it over formerly protected data without
explicit host policy. For a protected boundary, implement the narrow
`httpapi.Authorizer.Authorize(context.Context, RouteDescriptor) (bool, error)` and
set `Decorate: httpapi.Authorize(authorizer)` after host authentication `Middleware`.
Nil (including typed nil), deny, errors and unknown/mismatched descriptors deny with
403 before application calls; diagnostics never reach clients. Host identity lives
only in host context, with no Cedar/JWT/internal auth/user/tenant/persistence-policy
contract. This is HTTP/application-boundary admission, not exposure consent or a
standalone engine/provider/plugin framework. See the [external-module TLS authentication
and non-Cedar example](../pkg/registry/httpapi/testdata/external/authorization_test.go).
The router has no
identity, claims, JWT, Cedar schema, standalone configuration, PostgreSQL or
Kubernetes dependency. The default `catalog.Reader` is non-personalized.

`ConsumerRoutes()` returns a fresh complete inventory: 11 canonical GET templates
plus both extension-list trailing-slash aliases (13 registrations). Templates are
relative to `/registry`. Each descriptor includes method, template, entry kind,
domain operation, stable legacy audit event/resource type and resource/registry/
namespace parameter names. `Decorate` receives the descriptor and handler exactly
once per actual registration, not by URL guessing at request time. Hosts can use
ordinary middleware here. Standalone maps domain operations to fixed actions and
requires the **whole** manifest; this library
exports no authorization action, principal or entity schema.

`ConsumerServerRouter(pages, options)` uses the same implementation and host
contracts but registers only `ConsumerServerRoutes()`: server list, history and
version detail (exactly three descriptors). It excludes every skills/plugins route.
The full router still registers all 13 routes; standalone manifest guards and
OpenAPI remain unchanged.

## Replacement applications

Any host application can structurally implement `ConsumerPages.List`, `Get` and
`ListVersions` with the same public catalog/persistence value types. No default
reader or backend is constructed behind it. The request context passes through
unchanged, so a host's outer middleware and application can own personalized
selection. Supply coherent list, exact/latest and history operations.

Pages must contain at most the requested limit, with exactly the requested kind's
payload. The router preserves supplied ordering, payloads and route-supported
opaque cursor metadata, using `formats.ServerPage`, `SkillPage`, `PluginPage` and
their single-payload serializers. Counts describe only returned items. It never
sorts, filters, deduplicates, truncates, refills, recomputes latest or re-queries.
An invalid host result or unencodable payload gets a sanitized 500, not a partial
success. Cache headers, including personalized-cache restrictions, belong to the
host; the router adds none.

## Default compatibility at the HTTP edge

With zero-value `ConsumerOptions`, the adapter retains the upstream v1.8.1
history behavior and legacy ToolHive controls independently of the opt-ins below.

| Route | Request bound and selection | Response cursor |
| --- | --- | --- |
| `/{registryName}/v0.1/servers` | Default 30; positive integer limits capped at 1000. Search and cursor unchanged; `updated_since` parses RFC3339; optional exact/`latest` version selector. | Host cursor unchanged |
| `.../servers/{serverName}/versions` | Fixed 1000; **all query parameters ignored** | Always empty |
| `.../servers/{serverName}/versions/{version}` | Exact version or reserved `latest` | Single upstream envelope |
| `.../x/dev.toolhive/{skills,plugins}` (also `/`) | Default 50; limit outside 1..100 rejected. Search/cursor trimmed. | Host cursor unchanged |
| `.../{skills,plugins}/{namespace}/{name}` | Latest selector | Single extension payload |
| `.../{skills,plugins}/{namespace}/{name}/versions` | Fixed 30; **all query parameters ignored** | Host cursor unchanged, even though the route cannot consume continuation |
| `.../{skills,plugins}/{namespace}/{name}/versions/{version}` | Exact version or reserved `latest` | Single extension payload |

The actual legacy extension list handlers parsed `status` but never passed it to
the service. This adapter likewise ignores it, and ignores extension list
`version`/`updated_since`. Contrary to their Swagger descriptions, extension
lists do **not** request latest-only selection. Ordinary extension GET does.
The exact case-sensitive string `latest` is reserved on the wire; literal
`VersionSelector{Exact: "latest"}` remains expressible in the library only.

Chi's escaped server segment (`com.example%2Fitem`) is unescaped and checked with
`formats.ValidateServerName`; registry, namespace, name and version segments
retain the legacy nonempty/no-whitespace validation (no invented reverse-DNS
restriction on extension namespaces). Invalid UTF-8/NUL are rejected using the
public query contract. Path parameters are capped at 4096 bytes, list raw queries
at 16 KiB, and input cursors at `persistence.MaxCursorBytes` (4096). These are GET
routes: the adapter never reads a request body. Opaque cursor **interpretation**
belongs to the application, not to HTTP; malformed/mismatched backend cursors
must return `persistence.ErrInvalid`. There is no shared decoding policy that
would reject an otherwise valid personalized host token.

## Opt-in controls

Each option is independent and defaults to false:

- **`PaginateHistory`** accepts validated `limit` and opaque `cursor` on every
  history route and preserves the application's `NextCursor`, including servers.
  Defaults are server 1000 and extension 30; explicit limits must be decimal
  integers in server 1..1000 or extension 1..100. Empty, duplicate (including
  percent-decoded equivalent names), oversized or malformed controls fail before
  application calls, even when `StrictQueries` is false. Other parameters are
  ignored unless strict mode is enabled. This is **host-specific paginated
  history**, not upstream v1.8.1 history compatibility. HTTP does not sort, filter
  or refill these pages.
- **`StrictQueries`** accepts only the route-specific inventory below. It rejects
  unknown keys, duplicate decoded keys, invalid encoding, invalid UTF-8/NUL,
  oversized raw queries and empty present controls. Lists retain search and cursor
  exactly after URL decoding, without extension trimming. Server list limits above
  1000 fail rather than being capped. Nonempty whitespace in opaque search/cursor
  values is preserved; version and name selectors cannot contain whitespace.
- **`AllowExactName`** enables a **nonstandard** `name` list selector, forwarded
  unchanged to `Query.Name` after validation. Server names use
  `formats.ValidateServerName`; extension names use the nonempty, no-whitespace,
  UTF-8/NUL and 4096-byte name checks (no invented namespace syntax). This flag
  works with or without strict mode and never silently ignores a supplied name.
  Duplicate or invalid names fail in both modes. When disabled, permissive mode
  ignores `name` and strict mode rejects it.
- **`AllowUpdatedSince`** permits the existing RFC3339 `updated_since` filter on
  strict server lists. Without strict mode, legacy server forwarding and extension
  ignoring remain unchanged regardless of this flag. This filter is not full MCP
  synchronization: `include_deleted` is unsupported, there is no deletion-aware
  feed, and the adapter provides no cross-page snapshot guarantee.

### Complete query inventory

| Route family (all GET) | Permissive default | Strict mode |
| --- | --- | --- |
| Server list | `limit`, `cursor`, `search`, `version`, `updated_since`; other keys ignored | `limit`, `cursor`, `search`, `version`; `updated_since` only with `AllowUpdatedSince`, `name` only with `AllowExactName` |
| Skills/plugins lists, including both `/` aliases | `limit`, `cursor`, `search`; other keys ignored | `limit`, `cursor`, `search`; `name` only with `AllowExactName` |
| Server history | All query parameters ignored | No controls unless `PaginateHistory`, then only `limit`, `cursor` |
| Skills/plugins history | All query parameters ignored | No controls unless `PaginateHistory`, then only `limit`, `cursor` |
| Server version detail | All query parameters ignored | No query controls |
| Skills/plugins unversioned detail | All query parameters ignored | No query controls |
| Skills/plugins version detail | All query parameters ignored | No query controls |

In permissive mode, `PaginateHistory` enables only history `limit`/`cursor` and
`AllowExactName` enables only list `name`; neither changes unrelated legacy list
controls or detail behavior. Permissive list duplicates still use the first value
except for opted-in `name`. Strict mode rejects `include_deleted` for any value,
including `false`. Query and cursor size bounds apply to strict routes (including
otherwise query-free details) and opted-in history, not just legacy lists.

## Application error mapping

Errors retain the existing JSON shape (`{"error":"..."}`) by default. `ErrInvalid`, including
invalid cursors, maps to 400 (the deliberate correction from legacy cursor 500).
`ErrNotFound` maps to 404 `not found`: current narrow persistence errors cannot
distinguish a missing registry from a missing entry, so both use that generic
message rather than doing a hidden registry lookup. Other errors, including
cancellation/deadline errors while a response is still writable, retain the
legacy sanitized 500 `internal server error`. Host error text is neither returned
nor logged. A disconnected client may not receive a response; the supplied
request context is still canceled normally.

`ConsumerOptions.MapError` optionally receives the original request, actual
`RouteDescriptor` and error returned by `List`, `Get` or `ListVersions`. It returns
`httpapi.ErrorMapping{Status: ..., Body: ...}`, not a writer. Nil hook or exactly
`ErrorMapping{}` selects the sanitized default. Other mappings require status
400..599 and a non-nil, JSON-marshalable body; invalid status, typed-nil body or
encoding failure yields one sanitized 500 response, with no partial success.
The body is the complete JSON response and is not sanitized automatically: the
host must keep secrets and backend diagnostics out of custom bodies. For example,
a host can classify its own dependency error as 503 with a public error code.

Only **application-returned errors** reach this hook. Input validation, supplied
page/payload invariant failures and authorization denial never do; admission
still runs before the application and cannot be bypassed by an error mapper.
HTTP marshals before sending headers. Headers, cache policy, authentication,
admission and audit remain separate host middleware responsibilities. Applications
and their host-owned persistence own selection, opaque cursor interpretation,
stability across pages and any identity/cursor binding; the adapter adds none.

The default reader's winner-before-filter selection,
backend cursor format and literal-substring search semantics are unchanged; see
[consumer reads](registry-consumer-reads.md). HTTP does not retrofit legacy SQL
wildcard semantics or identity binding onto that reader.

Tests cover every registration/decorator, custom selected order and cursor with
one application call, strict and permissive query inventories, independent opt-ins,
exact traversal of more than 1000 host-ordered versions with page counts and opaque
continuations, sanitized/custom error mapping and authorization isolation. Existing
external-module hosts verify importability without DB/config/auth/Kubernetes
imports. Real HTTP over catalog/PostgreSQL covers whole-name source priority,
latest/history, cursors and below/at/above default route history bounds.
