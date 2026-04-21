---
paths:
  - "internal/api/**/*.go"
---

# API Surface

Applies to all HTTP handlers and routing. These rules protect the distinction between our
admin API and the upstream MCP Registry spec surface, keep publishing coherent, and ensure
audit trails cover every admin operation.

## 1. Consumer API Is Upstream-Spec-Compliant; Admin API Is Ours

`/registry/{name}/v0.1/...` matches the upstream MCP Registry v0.1 schema exactly. `/v1/...`
is our admin surface and evolves independently. Extensions to the consumer surface go under
`/x/{vendor}/...`.

**What must hold:**
- Consumer endpoints under `internal/api/registry/v01/` must match upstream request params,
  response shapes, pagination cursor, and error codes. Types come from
  `github.com/modelcontextprotocol/registry/pkg/api/v0`.
- Admin endpoints under `internal/api/v1/` have no upstream parity requirement.
- Extensions live under `/x/dev.toolhive/...` (currently `internal/api/x/skills/`). The
  `/x/` prefix signals "non-spec."
- The `{registryName}` path segment is required even if only one registry exists. The
  `/registry/` prefix avoids path collisions (e.g., a registry literally named "v1").

**Detect**: custom fields added to consumer response structs; a consumer endpoint that
diverges from upstream; a non-spec endpoint mounted at `/registry/{name}/v0.1/` without
`/x/` prefix.

**Breaks if violated:** existing MCP clients break; we cannot follow future upstream spec
versions without a breaking change; custom extensions become indistinguishable from spec
features.

## 2. Publish Is a Single Global Endpoint Under /v1/

`POST /v1/entries` is the only publish endpoint. An entry's visibility is determined by its
claims, not by which URL published it. Per-registry publish would contradict that.

**What must hold:**
- One publish endpoint: `POST /v1/entries` with top-level `claims` plus mutually-exclusive
  `server` or `skill` fields.
- The `server` / `skill` payload is the upstream spec shape, **unchanged**. Claims are
  metadata about the entry, not part of the entry payload — do not fold them into the
  payload.
- Published entries all land in the single managed source; they appear in any registry
  whose source list includes it, subject to claims.

**Detect**: a new publish endpoint mounted on a registry path; claims fields appearing
inside `ServerJSON` or `Skill` payload types; publish logic that picks a destination source
other than the managed source.

## 3. Responses Are Personalized; Pagination Is Per-User

Same URL, different user → different data. Clients and infrastructure must treat responses
as personalized.

**What must hold:**
- Cursors are only valid for the same user that received them. Do not cache cross-user.
- Response-metadata counts reflect only the caller's visible set, not a global total.
- CDN caching of consumer responses requires `Vary: Authorization` (effectively: no cache).
  Do not add aggressive caching that ignores the caller.

**Detect**: new Cache-Control headers on consumer endpoints; cursor handling that assumes a
stable result set across users; global-count metadata.

## 4. Every Admin Handler Is Audit-Wrapped

All `/v1/` handlers are wrapped with `auditmw.Audited` (or a variant). Event type, resource
type, and URL-param name are declared at route registration.

**Detect**: a new handler in `internal/api/v1/` registered without an `auditmw.Audited*`
wrapper; a handler that rolls its own audit logging instead of using the middleware; an
event type renamed without checking downstream log consumers.

**Instead**: pick the right wrapper (`Audited`, `AuditedUpsert`, `AuditedEntry`,
`AuditedServer`) and declare it at the route. Audit event names
(`source.create`, `entry.publish`, etc.) are stable — renaming them is a breaking change.

## 5. Dedup at `(type, name)` Level With Source Priority

When multiple sources in a registry provide the same `(type, name)`, the highest-priority
source wins entirely. Mixed version histories across sources are not allowed in consumer
views.

**What must hold:**
- Dedup happens at the name level, not the version level. All versions of a name come from
  one source.
- Claims filtering runs **before** dedup — if the highest-priority source's entry fails the
  caller's claims check, the next source is promoted.
- Admin endpoints (`/v1/sources/{name}/entries`, `/v1/registries/{name}/entries`) show
  unshadowed data so operators can see what dedup hid.

**Detect**: consumer responses that list versions from multiple sources for one name; dedup
logic applied at version level; admin "entries" endpoints that apply dedup filters.

## 6. Path and Query Params Are Validated Before Service Calls

Use the helpers in `internal/api/common` (`GetAndValidateURLParam`,
`GetAndValidateServerNameParam`) at handler entry. The service layer assumes inputs have
been normalized and format-checked.

**Detect**: handlers passing raw `chi.URLParam(r, ...)` output to the service; handlers
that decode cursors or parse timestamps inline without returning 400 on parse errors;
service methods that re-implement input validation.

**Instead**: validate at the edge and return 400 for malformed input. Let the service layer
focus on business rules.
