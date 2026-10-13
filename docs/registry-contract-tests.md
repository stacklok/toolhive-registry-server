# Registry contract tests

The suites exercise public components, backend behavior and the standalone
composition. Use [embedding guidance](registry-embedding-validation.md) for runnable
external examples and [component architecture](component-architecture.md) for the
boundaries those examples exercise.

## Suites by contract

Shared backend suites below live in `pkg/registry/persistence/conformance`.

| Contract | Suites |
| --- | --- |
| Payload validation, serialization and exact version ordering | `pkg/registry/model` and `pkg/registry/formats`, including the separate-module format fixture. |
| Definition provenance, managed singleton and atomic config reconciliation | `pkg/registry/persistence/conformance/suite.go`, run against independent memory and PostgreSQL; `pkg/registry/postgres/definitions_test.go` adds historical-field and schema refusal cases. |
| All-kind atomic snapshots, source-local publication/deletion and latest | Shared `entries_suite.go` and `publication_suite.go`; `pkg/registry/postgres` adapter tests; `internal/sync/writer` tests exercise COPY/upsert, related metadata, orphan cleanup and rollback in caller-owned transactions. |
| Lease expiry, stale generations/incarnations and atomic commit/acknowledgement | Shared `jobs_suite.go`, PostgreSQL jobs/reconciliation tests and `pkg/registry/reconcile` coordinator/shutdown tests. Equivalent schedule durations retain leases/baselines; changed durations fence work. |
| Whole-name winners, filters, versions and bound cursors | Shared `view_suite.go`, `pkg/registry/catalog/reader_test.go`, PostgreSQL view tests and the shared consumer HTTP suite described below. |
| Consumer/admin wire shapes, validation and host replacement | `pkg/registry/httpapi` tests and separate-module fixtures; PostgreSQL `consumer_http_test.go` and `admin_http_test.go` combine HTTP, catalog and real storage. |
| Import validation, transport boundaries and failure-last-good behavior | `pkg/registry/ingest` fetcher/upstream/shutdown tests, including private Git credentials, malformed/empty imports and partial API pagination. |
| Standalone preparation, ownership and shutdown | `internal/app/catalog*_test.go`, storage tests and CLI `serve_catalog*_test.go`: final config/frozen policy, receipt barriers, inline/background joins, borrowed/owned resources and listener/worker errors. |
| Verified authentication, Cedar and protected routes | `internal/auth`, `internal/authorization`, `internal/api/admission_test.go` and CLI/app HTTP tests: JWT/introspection, anonymous opt-in, admin-off, fixed-action evidence, evaluator failures, protected 410 routes and safe attribution. |
| Offline upgrades and retained evidence | CLI `migrate_exposure_test.go`, `internal/db/exposure*_test.go`, config presence tests and `database` migration tests: inventory/acknowledge/retire/finalize, target continuity, archives/receipts, rollback on failure and schema-30 refusal/cleanup. See the [operational migration guide](registry-exposure-migration.md). |

Expected HTTP bodies and route inventories are fixed independently of production
DTOs, serializers and route discovery. Do not generate expected responses by calling
the formatter under test. `httpapi/conformance.RunConsumer` runs unchanged against
independent memory selection (`TestConsumerHTTPMemory`) and PostgreSQL
(`TestConsumerHTTPPostgres`). It checks all three kinds, literal search, whole-name
winners before filters, query-bound page walks, reordered sources/view incarnations,
and histories below/at/above 1000 servers and 30 skills/plugins. Server histories
have no continuation; extension histories emit cursor metadata but ignore input
pagination. Unsupported extension wire filters are distinct from backend query tests.

The separate-module fixtures execute with `GOWORK=off -mod=readonly`; parent tests
also inspect actual transitive imports. The complete PostgreSQL fixture exercises
public definitions/jobs, ingestion, catalog and both HTTP routers. Adapter dependencies
are intentional: import checks distinguish portable packages from PostgreSQL and
Kubernetes adapters rather than banning those dependencies from the entire module.

## Kubernetes test boundaries

- `pkg/registry/operator` unit tests use fake clients for export opt-in, all three
  CRD kinds, full namespace scope, metadata validation, empty snapshots, readiness
  and list/watch/cancellation failures. Naming tests and
  `TestHistoricalCrossKindNameIdentityNotRewritten` cover same-name cross-kind exports.
- `internal/kubernetes/standalone_test.go` checks retired-annotation presence before
  conversion and rejects incomplete or failed full-scope listings.
- `internal/app/catalog_ingestion_test.go` contains
  `TestCatalogOperatorPostgresHTTPOwnershipAndScopeChange`: **fake Kubernetes API,
  real PostgreSQL**, the actual supervisor/runner and bound OAuth HTTP. It exercises
  all kinds over two namespaces, managed publication/removal and persisted scope shrink.
- `pkg/registry/operator/manager_integration_test.go` contains
  `TestRegisteredOperator`: **real isolated envtest API and leader-elected manager,
  generated mock Jobs**, not PostgreSQL. It installs all three CRDs, observes watches
  and nonempty/changed/empty startup snapshots, and injects list/watch failures.
  These suites do not constitute one live-Kubernetes-plus-PostgreSQL assembly.

## Run with isolated dependencies

Run from the repository root with the Go version in `go.mod`, Task, mockgen and sqlc.
Always run `task gen` before tests. Database helpers start embedded PostgreSQL 16
and create isolated databases, normally at schema 30; historical tests explicitly
select their schema. They do not use a deployment DSN or require Docker. The first
run needs network access to cache PostgreSQL binaries; subsequent runs can reuse them.

```sh
task gen
# Public contracts and adapters, including separate-module and PostgreSQL fixtures:
GOWORK=off go test -race -count=1 ./pkg/registry/...
# The same HTTP and job contracts on memory and PostgreSQL:
GOWORK=off go test -race -count=1 ./pkg/registry/persistence/conformance ./pkg/registry/postgres \
  -run 'TestConsumerHTTP(Memory|Postgres)$|Test(MemoryJobs|JobsConformance)$'
# Standalone HTTP/authentication/Cedar, preparation and lifecycle:
GOWORK=off go test -race -count=1 ./cmd/thv-registry-api/app ./internal/app ./internal/api ./internal/auth ./internal/authorization
```

For manager tests, set `KUBEBUILDER_ASSETS` to installed Kubernetes **1.33.0** envtest
assets (`etcd` and `kube-apiserver`). The pinned tool and release index are in
[the test workflow](../.github/workflows/test.yml). Keep existing-cluster mode disabled;
`TestRegisteredOperator` also disables it in the fixture. Never target a user cluster.

```sh
export KUBEBUILDER_ASSETS=/path/to/1.33.0-linux-amd64
export USE_EXISTING_CLUSTER=false
# Registered operator only:
GOWORK=off go test -p 2 -race -tags integration -count=1 ./pkg/registry/operator -run '^TestRegisteredOperator$'
# Full suite, including integration-tagged tests:
GOWORK=off go test -p 2 -race -tags integration -count=1 ./...
```

These commands exercise local test assemblies, not a deployed image or Helm installation.
