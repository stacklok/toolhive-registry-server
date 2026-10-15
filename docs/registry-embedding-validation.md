# Embedding registry components

Import the components you need from
`github.com/stacklok/toolhive-registry-server/pkg/registry`. You do not need the
standalone configuration loader, listeners or application builder. The
[architecture overview](component-architecture.md) describes package boundaries;
the focused guides below describe each public contract.

Start with the [runnable mTLS catalog tutorial](../examples/embedding/README.md)
to mount the default reader over an existing schema-30 PostgreSQL catalog. It shows
host authentication/admission, request cancellation and pool/listener shutdown.
The separate-module [replacement ConsumerPages fixture](../pkg/registry/httpapi/testdata/external/README.md)
shows a host-selected application without PostgreSQL, configuration, ingestion or
bundled authentication. Its runnable test/example uses explicitly synthetic contexts;
it is not a production authentication recipe.

## Choose a composition

| Need | Components and executable example |
| --- | --- |
| Validate or serialize payloads only | [Models/formats](registry-formats.md); [external format example](../pkg/registry/formats/testdata/external/consumer_test.go). No backend or listener. |
| Use a different backend | Implement the [persistence capabilities](registry-persistence.md) you consume; see the [contract import example](../pkg/registry/persistence/testdata/external/consumer_test.go) and [independent backend/catalog example](../pkg/registry/catalog/testdata/external/consumer_test.go). Run the shared conformance suites against fresh isolated state. |
| Ingest without serving HTTP | [Fetcher and coordinator](registry-ingestion.md#source-adapters-and-host-coordinators); [external reconciliation example](../pkg/registry/ingest/testdata/external/consumer_test.go) uses generated Jobs expectations without a database. Replace I/O through `ingest.Options`, or replace `reconcile.Fetcher` itself. |
| Serve a host-selected catalog | [Consumer HTTP](registry-consumer-http.md) over `httpapi.ConsumerPages`; [replacement application example](../pkg/registry/httpapi/testdata/external/pages.go) and [HTTP tests](../pkg/registry/httpapi/testdata/external/pages_test.go). Supply coherent list, get/latest and version-history operations; no persistence implementation is required. |
| Add administration | [Admin HTTP](registry-admin-http.md) with separate source, view, publisher and diagnostic capabilities; [host assembly example](../pkg/registry/httpapi/testdata/external/assembly_test.go). Consumer mounting does not enable admin. |
| Export ToolHive Kubernetes resources | [Optional operator](registry-ingestion.md#optional-toolhive-operator); [external runner example](../pkg/registry/operator/testdata/external/consumer_test.go). Supply clients and, if used, a host-owned manager. |
| Combine PostgreSQL, ingestion, catalog and HTTP | [Complete external assembly](../pkg/registry/postgres/testdata/external/assembly_test.go) constructs public adapters and both routers, then substitutes fetcher and reader implementations. Run it through its parent test as shown below. |

The complete fixture's header gate demonstrates middleware wiring, **not production
authentication**. Its selected-reader replacement only exercises list pass-through;
a production personalized service must also select get, latest and history consistently.
For credential verification, see the [TLS authentication and non-Cedar authorizer example](../pkg/registry/httpapi/testdata/external/authorization_test.go).
Replace its fixture credentials with your host verifier in production.

## Host contracts

- **Authentication and admission belong to the host.** Libraries do not require
  Cedar. Run host authentication through `Middleware`, then use
  `Decorate: httpapi.Authorize(authorizer)` or a host decorator. `Authorizer` receives
  host context and a `RouteDescriptor`, not a shared identity model. The helper
  denies nil authorizers, errors, denials and unknown/mismatched routes before
  application calls. Omitting decoration leaves the low-level routers policy-free.
  Borrowed authorizers must be concurrency-safe and honor request cancellation.
- **Selection belongs to the application.** The default catalog is nonpersonalized;
  named views are not storage tenants; overlapping admitted views can expose the
  same entries. Standalone Cedar admits reads per requested view, without changing
  these public contracts. HTTP serializes bounded host-selected
  pages without sorting, filtering or refilling. Hosts own personalized cursors,
  cache restrictions and audit attribution. See [consumer HTTP](registry-consumer-http.md)
  for route-specific bounds and continuation limits.
- **Dependencies remain borrowed.** Hosts own pools, transports, Kubernetes clients,
  managers, secrets, listeners and telemetry. Constructors start no application
  workers/listeners. Cancel and join coordinators, runners and requests before
  closing their dependencies; callbacks must honor cancellation. PostgreSQL
  `NewEntries` performs latest repair on construction, but no adapter migrates or
  closes the supplied pool.
- **Storage behavior is part of the contract.** Alternative backends must preserve
  atomic source replacement, managed ownership, whole-name view selection and
  leased fencing for the capabilities they implement. The independent memory backend
  is test support, not a production storage recommendation. Source definitions are
  trusted administrative inputs: hosts constrain network/file/credential access,
  rather than exposing fetchers as arbitrary URL or file-read endpoints.

All components share one Go module. Portable model/formats/persistence/catalog/httpapi
package graphs exclude PostgreSQL, Kubernetes and registry-internal configuration/auth.
Ingestion/reconciliation reuse low-level internal helpers; the optional PostgreSQL and
operator adapters bring their respective driver/client dependencies. Package isolation
does not make the module's dependency download set lightweight.

## Replacement application conformance

[`httpapi/conformance/pages.Run`](../pkg/registry/httpapi/conformance/pages/pages.go)
takes a small setup callback returning a ready `ConsumerPages`, host context,
ordered expected server records and host-owned selection/admission change callbacks.
It directly checks all three methods, exact/latest, search, list/history continuation,
current selection and admission between pages, not-found classification and canceled
contexts. It does not require definitions, sources, views, snapshots, jobs, ingestion
or `catalog.NewReader`. Its separate package keeps backend test support out of a
replacement host's import graph. Existing `httpapi/conformance.RunConsumer` remains
the backend/default-reader suite.

The external fixture also checks real HTTP shapes independently of production DTOs,
1003-version history traversal, exact host order and opaque cursor preservation,
`json.Number` metadata fidelity for `9007199254740993`, generic missing/inaccessible
responses, sanitized defaults and custom dependency-503 errors. The host applies
`Cache-Control: private, no-store` to successes and errors, including denied requests.
Query/fixture cursor binding is integrity checking, **not authorization**; selection
and admission are reevaluated on continuations. A loopback listener test cancels and
joins in-flight List/Get/ListVersions before releasing host resources, without a DB.

## Run the examples

From the repository root, use the Go version in `go.mod` and installed development
tools. Generate mocks and sqlc before running tests:

```sh
task gen
(cd pkg/registry/formats/testdata/external && GOWORK=off go test -mod=readonly ./...)
(cd pkg/registry/persistence/testdata/external && GOWORK=off go test -mod=readonly ./...)
(cd pkg/registry/ingest/testdata/external && GOWORK=off go test -mod=readonly ./...)
(cd pkg/registry/catalog/testdata/external && GOWORK=off go test -race -mod=readonly ./...)
(cd pkg/registry/httpapi/testdata/external && GOWORK=off go test -race -mod=readonly ./...)
(cd pkg/registry/operator/testdata/external && GOWORK=off go test -mod=readonly ./...)
GOWORK=off go test -race -count=1 ./pkg/registry/postgres -run '^TestExternalCompleteAssembly$'
```

The last command's parent test prepares disposable PostgreSQL at schema 30 and
passes its DSN to the separate module. Do not supply an ambient or production DSN
to that fixture. PostgreSQL binaries must be cached or available for the test helper
to download. See [contract tests](registry-contract-tests.md) for backend conformance,
import-graph checks and isolated envtest commands.

## Existing data and standalone use

Public storage APIs provide no exposure-consent or authorization boundary. Before
using PostgreSQL adapters on an existing catalog, follow the
[offline migration procedure](registry-exposure-migration.md#maintenance-procedure):
stop/drain old readers and writers, verify a complete backup, review and finalize
at schema 29, then migrate to clean schema 30. Empty-install initialization cannot
approve existing data. Schema-30 rollback requires an offline verified complete
backup restore, not claims reconstruction or receipt rebinding.

For the packaged server rather than a library host, use
[standalone assembly](registry-standalone-assembly.md) and
[Cedar authorization](registry-cedar-authorization.md). Standalone's mandatory Cedar,
OAuth/admin gates and storage preparation are not optional library decorators.
