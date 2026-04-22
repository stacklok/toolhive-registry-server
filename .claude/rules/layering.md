---
paths:
  - "internal/**/*.go"
  - "cmd/**/*.go"
---

# Layering and App Assembly

Applies to all Go code in the service. These rules govern how packages relate and how the
app is wired together. Violating them is how unit tests grow real-DB dependencies and
refactors become global.

## 1. Three-Layer Data Model: Sources → Entry Pool → Registries

Ingestion, storage, and consumer views are distinct layers. Sources are an admin concern,
registries are the consumer boundary, and the entry pool is the only shared state.

**Detect**: admin concepts (sync config, source filters, ingestion hashes) leaking into
`/registry/{name}/v0.1/...` request/response shapes; new per-registry tables; handlers under
`internal/api/registry/v01/` that reference `sources.*` types directly.

**Instead**: admin concerns belong under `/v1/` (`internal/api/v1/`). Consumer endpoints go
through `service.RegistryService` and see only `registry_entry` + `entry_version` data.

## 2. Layer Direction: API → Service → Storage/Sources

The dependency arrow goes one way. HTTP concerns stay out of business logic; Postgres and
K8s stay out of the service layer.

**Detect**: `internal/api/**` files importing `internal/service/db/**`, `database/**`, or
concrete `sources.*` types; `internal/service/**` importing `net/http`, `client-go`, or
controller-runtime; `client-go` imports anywhere outside `internal/kubernetes/**` and the
K8s sync path.

**Instead**: API handlers call `service.RegistryService`. Service depends on storage
interfaces (`storage.Factory`), not concrete drivers. Kubernetes code lives in
`internal/kubernetes/**` and is invoked from sync orchestration.

## 3. Two HTTP Servers: Public and Internal

Operational endpoints run on a separate port with no auth middleware. Consumer and admin
traffic run on the public port with full middleware.

**What must hold:**
- Public server (default `:8080`): all `/v1/`, `/registry/`, `/openapi.json`,
  `/.well-known/...` routes. Carries auth and audit middleware.
- Internal server (default `:8081`): `/health`, `/readiness`, `/version` only. No auth, no
  audit, no rate limiting.
- Internal endpoints are never reachable from the public port.

**Breaks if violated:** Kubernetes probes fail when auth is required; operational endpoints
become part of the customer-facing API contract; rate limiting on the public path throttles
probes.

## 4. Builder + Functional Options; Factory Interfaces for DI

The app is assembled with `app.NewRegistryApp(ctx, opts...)`. Every non-trivial component
is injectable for tests through a factory interface.

**Detect**: a new component wired with a direct concrete dependency that has no
corresponding `With*` option; tests that stand up real Postgres / real K8s / real HTTP
because there is no injection seam; a struct literal in `internal/app/builder.go` for a
component that doesn't go through its factory.

**Instead**: wire new components through `internal/app/builder.go` using a `With*` option
and a factory interface. Tests lean on `WithStorageFactory`, `WithSyncManager`,
`WithRegistryHandlerFactory`, `WithCoordinatorOptions`. Concrete implementations live
beside the interfaces.

## 5. No Cross-Layer Helpers

Don't create "common" packages that leak types across layer boundaries — e.g., a helper
package that depends on both `net/http` and the DB types. Shared helpers are the easy way
to launder layering violations.

**Detect**: a new package under `internal/` that imports from more than one of
{`internal/api/**`, `internal/service/**`, `internal/db/**`, `client-go`, `controller-runtime`};
utility files that accept both `http.Request` and `pgx.Tx`.

**Instead**: put the helper in the layer that owns it, or split the helper so each layer
gets its own narrow version. `internal/api/common` is intentionally scoped to
request/response helpers only — keep it that way.
