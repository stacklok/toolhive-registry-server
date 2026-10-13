# Opt-in standalone public-component assembly

`internal/app.NewCatalogApp` is an available **opt-in Go entrypoint** for #914/#912.
It assembles the actual public PostgreSQL definitions/jobs/entries, catalog reader,
definition administration, publisher and diagnostics, public consumer/admin HTTP
adapters, leased supervisor and standalone operator boundary. It does not adapt the
old `RegistryService`, start an old coordinator/writer or start a Kubernetes manager.

**The production CLI and charts still select the legacy default.** This change does
not close #914: startup selection and chart activation belong to the next PR. Legacy
code/tests, configuration fields and historical migrations remain for that transition
and #915 cleanup. Never run the legacy CLI against a retired catalog, or co-own sources
with an old writer. Stop/drain all old readers/writers before offline review.

## Direct use

Within this repository, load the configuration, apply final overrides through
`CatalogOptions.Overrides`, and call the new entrypoint after the explicit offline
procedure below. `LoadCatalogConfig` and offline inspection use the same decoded
YAML/environment snapshot without the old loader's source/view validation; public
inline definitions are supported and final source/admission validation happens after
overrides in NewCatalogApp. The loader still validates database settings **before**
`CatalogOptions.Overrides`; even an alternate storage factory requires valid loader
DB settings when using `LoadCatalogConfig`. It is not a decode-only loader.
The CLI still uses LoadConfig's legacy validation. There is no consent
boolean, default policy or automatic migration:

```go
cfg, err := config.LoadCatalogConfig(config.WithConfigPath(configPath))
if err != nil { return err }
application, err := app.NewCatalogApp(ctx, app.CatalogOptions{
    Config: cfg,
    HTTP: []app.RegistryAppOptions{
        app.WithAddress(":8080"),
        app.WithInternalAddress(":8081"),
        app.WithMeterProvider(meterProvider),
        app.WithTracerProvider(tracerProvider),
        app.WithMetricsHandler(metricsHandler),
    },
})
if err != nil { return err }
if err := application.Start(); err != nil { return err }
// Wait reports a terminal worker/listener error and automatically drains/joins.
// On operator-requested shutdown call Stop; it also waits for the final result.
return application.Wait()
```

`Run` is `Start` followed by `Wait`. The supplied lifetime context cancels serving.
`Stop` is safe before Start and after termination. Instances are single-use: do not
restart a stopped instance. `Addresses` returns actual bound addresses after Start.
`SyncInline(ctx, persistedSourceName)` is the tracked manual ingestion seam; it cannot
be used before Start or after shutdown admission closes. No raw HTTP services/runtime
or unguarded router is exposed by the application.

`storage.CatalogPostgres{BorrowedPool: pool}` explicitly borrows a migrated pool;
application cleanup never closes it. With the default factory the application owns the
opened pool. Injected `CatalogFactory` implementations **must** return the preparation
interface: `Preflight`, `Consume`, `Initialize`, `Close`. There is no optional assertion
or legacy fallback. Returned storage transfers cleanup even alongside an error; when
returning nil storage, the factory cleans its own partially opened resources. Fake/host
resources remain borrowed only if their Close implementation declares that behavior.
Operator/fetcher and telemetry providers are borrowed; their hosts retain ownership.

## Mandatory preparation order

1. Copy the decoded config, apply final overrides, reject retired fields **by presence**,
   and validate admission/storage/public source definitions and lifecycle options.
   Retired fields include explicit null/empty/case/dotted YAML, merges/aliases and
   reserved environment prefixes (including empty values): `auth.authz`, `auth.roles`,
   `auth.publicPaths`, source/registry `claims`. Direct non-nil struct fields are rejected
   too. Raw-key/environment evidence is retained from loading, not reread at startup.
2. Load and strictly validate immutable Cedar. Bind `Config.ExposureReview` to the
   actual `Engine.Revision`, never reread the policy/config file during preparation.
3. Open storage without migration, definition initialization, latest repair or workers.
4. Read-only compatibility preflight over **all** historical definitions (CONFIG/API,
   linked/unlinked and CONFIG rows about to be replaced) and all final definitions.
   Unsupported historical fields/ingestion flags, invalid names, incompatible inline
   payloads and non-synced schedules/filters fail
   with explicit remediation instructions; none are silently discarded. Inline filters
   are not erased or translated: explicitly remediate the unsupported definition before
   review. Operator preflight authoritatively lists the entire namespace scope and all
   three CRD kinds. Exact export opt-in followed by retired-annotation presence rejects
   the whole snapshot, even when conversion would otherwise skip an object.
5. Serialize `ConsumeExposure` at clean schema **29**. Missing/stale/pending unretired
   evidence cannot be bypassed by empty/unlabeled data, authz-off or admin-off. A pending
   receipt checks final config/policy, target and full persisted catalog. Once consumed,
   only target continuity is a restart binding: legitimate catalog/config/policy edits
   do not require matching a historical catalog hash.
6. Atomically reconcile CONFIG definitions and repair latest pointers via `NewEntries`.
   Construct public services, mandatory fixed-action guarded capabilities and HTTP.
   Provider validation/refresh initialization is deferred until an authenticated request;
   preparing an application starts no application workers/listeners.
7. Only `Start` binds **two** servers and starts the exclusive leased runtime. Pool
   housekeeping is owned by the pool, not an ingestion worker. Terminal failures are
   returned by Wait/Run/Stop, not swallowed as log-only events.

Use the [offline exposure procedure](registry-exposure-migration.md) against an isolated
or explicitly authorized deployment: inventory, exact-digest acknowledgement and explicit
retirement for historical state; inventory and **initialize-empty** for a fresh database
before any definitions are reconciled. The existing offline commands alone archive/clear
claims. Neither `migrate up --yes`, an empty schema nor application construction supplies
consent. An error after successful consumption/initialization does not roll back the
recorded exposure transition: inspect the receipt and retry the same target, never clear
or rebind evidence automatically.

## Namespace and lifecycle ownership

Effective namespace precedence is explicit source scope, then global watch scope, then
all namespaces. CONFIG and new/updated API definitions store the canonical effective
scope (trimmed, sorted, deduplicated); preflight, acquired Jobs and operator runners use
that stored scope. Historical API sources with an empty scope and a nonempty global
fallback fail preflight until the intended scope is explicitly materialized in storage.
There is no client-only narrowing of an all-namespace Jobs definition.

Source CRUD persists first and synchronously invokes leased inline sync and refresh.
Failures return truthful **503 committed** responses; audit records the actual creation
or update event and `mutation_committed`, not a guessed event based on HTTP status.
Periodic rediscovery recovers missed refresh notifications. Every source origin is
included, independent of view links; managed publication never acquires a sync lease.
Shutdown closes admission, cancels requests/explicit inline calls, drains HTTP and joins
background and caller-owned work before closing owned storage/audit resources. HTTP
shutdown timeout is not permission to close a pool under an active operation. Source
counts, leased-attempt metrics, HTTP metrics/traces, request limits, transport timeouts,
secure Git secret-file resolution and OAuth resource metadata remain available.

## Admission and routes

Consumer mounting remains `/registry`; admin remains `/v1`. Each of the 13 consumer and
12 retained admin descriptors is checked against the closed action/audit manifest at
registration. Every guarded service method requires private verified identity and route
evidence matching its own fixed action; broad permits do not make omitted middleware,
an alternate handler or a mislabeled operation safe. No legacy roles/resource claims
participate. Permitted identities receive the same page/cursor. Anonymous catalog access
requires explicit opt-in **and** a Cedar read permit. Admin is absent by default and,
when enabled, always authenticated. Bearer challenges also protect metadata. `/v1/me`
and GET/PUT claims routes are authenticated static 410 tombstones, never legacy calls.
Operational probes/metrics are only on the separate internal server. The sole fixed
unauthenticated OAuth bootstrap is the protected-resource metadata GET.

Audit/admission logs never include raw token, claims, policy or callback error values.
The existing optional full request-body audit capture is a separate opt-in: publication
metadata and inline administrative configuration may contain secrets. Leave it off unless
its exposure and retention have been separately reviewed.

## Remaining activation work (next PR)

- Select NewCatalogApp from CLI startup, preserving final overrides and the mandatory
  barrier; do not run legacy automatic initialization/migration before it.
- Activate charts/config examples with Cedar and explicit administration defaults, the
  offline maintenance/receipt procedure, full-scope operator permissions and probe ports.
- Update the default OpenAPI schema/annotations to the public admin surface and retired
  claims fields. This PR deliberately does not regenerate the legacy-default Swagger
  artifact; `/openapi.json` currently serves that transitional artifact behind admission.
- Validate the selected startup/chart path and monitor its CI before considering #914
  complete. Delete legacy monolith/claims fields and physical columns only in #915.

Embedding applications continue to import the public packages and own their admission,
personalized selection (if needed) and dependencies. Existing separate-module executable
fixtures remain unchanged; `internal/app` is the standalone composition, not a new public
host framework.
