---
paths:
  - "internal/**/*.go"
  - "cmd/**/*.go"
  - "pkg/registry/**/*.go"
---

# Layering and App Assembly

## 1. Sources → Entry Pool → Views

Sources own entry versions; named registries are ordered consumer views, not tenants.
Admin definitions, filters and jobs must not leak into upstream consumer payloads.
Public model/formats/persistence/catalog contracts contain no identity, resource claims,
visibility policy or authorization scope. Personalized selection belongs to a host service.

## 2. Dependency Direction

HTTP consumes small catalog capabilities; catalog and reconciliation consume persistence
contracts. Contracts never import PostgreSQL, standalone config/auth or application wiring.
`pkg/registry/httpapi` serializes the supplied page without filtering, sorting or refilling.
PostgreSQL may explicitly import `internal/db` and transactional `internal/sync/writer`;
these are implementation adapters, never exported types in portable contracts.
Kubernetes belongs in optional `pkg/registry/operator` and its standalone client boundary;
core ingestion/reconciliation must not import Kubernetes. The low-level Git/HTTP/filter
helpers remain shared implementation dependencies, not standalone policy.

## 3. Two HTTP Servers

The public server (default `:8080`) carries mandatory verified authentication, fixed-action
Cedar admission and audit. It mounts consumer routes, opt-in `/v1` administration,
OpenAPI and the fixed OAuth bootstrap GET. Operational `/health`, `/readiness`, `/version`
and optional `/metrics` belong only to the internal server (`:8081`), without auth/audit.
Do not introduce configurable public-path bypasses or mount operational routes publicly.

## 4. Catalog Assembly and Ownership

`app.NewCatalogApp` composes the same public components available to embedders.
`CatalogOptions` injects storage, operator, fetcher and reconciliation dependencies;
HTTP options retain shared audit, telemetry, middleware and timeout helpers.
Storage must implement all preparation phases, with no legacy fallback: final config,
frozen Cedar, open, read-only historical/source/operator preflight, receipt consumption
(offline finalization at 29, serving at clean 30), then atomic reconciliation/repair
and guarded services/HTTP.
Constructors start no application workers/listeners. Start/Wait/Stop must join all owned
and inline work before closing storage/audit resources. Borrowed resources stay borrowed.
Never recreate `RegistryService`, the old app/storage factories or unleased coordinator.

## 5. No Cross-Layer Helpers

No common package bridging HTTP, SQL, Kubernetes and identity. Interfaces are defined
where consumed; implementations live beside them. Separate-module tests must prove
public importability and the actual forbidden dependency graph, not just package names.
