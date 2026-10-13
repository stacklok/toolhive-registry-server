# Reusable consumer HTTP

`pkg/registry/httpapi` adapts **already selected** application pages to the MCP
Registry v0.1 and ToolHive skills/plugins wire formats. Standalone selects this
router through mandatory Cedar-guarded capabilities after schema-29 exposure preparation.
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

Admission is **entirely host-owned**. The public router can be unprotected; do not
mount it over formerly protected data without explicit host policy. It has no
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

## Compatibility at the HTTP edge

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

Errors retain the existing JSON shape (`{"error":"..."}`). `ErrInvalid`, including
invalid cursors, maps to 400 (the deliberate correction from legacy cursor 500).
`ErrNotFound` maps to 404 `not found`: current narrow persistence errors cannot
distinguish a missing registry from a missing entry, so both use that generic
message rather than doing a hidden registry lookup. Other errors, including
cancellation/deadline errors while a response is still writable, retain the
legacy sanitized 500 `internal server error`. Host error text is neither returned
nor logged. A disconnected client may not receive a response; the supplied
request context is still canceled normally.

The default reader's previously documented winner-before-filter selection,
backend cursor format and literal-substring search semantics are unchanged; see
[consumer reads](registry-consumer-reads.md). HTTP does not retrofit legacy SQL
wildcard semantics or identity binding onto that reader.

Tests cover every registration/decorator, custom selected order and cursor with
one application call, request validation and errors, an external-module host
without DB/config/auth/Kubernetes imports, and real HTTP over catalog/PostgreSQL
with whole-name source priority, latest/history, cursors and below/at/above route
history bounds.
