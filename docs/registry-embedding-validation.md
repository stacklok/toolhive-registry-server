# Final embedding validation and release notes

This is the executable acceptance map for #906/#915 atop the schema-30 cleanup.
The CLI already uses public constructors; this slice adds validation/documentation,
not another runtime, authorization framework or production backend. The operator
same-name cross-kind identity choice remains **pending user decision**, including
different versions. No issue closure, human approval or merge is implied.

## Executable assemblies

From the repository root, with Go 1.26 and the existing development tools:

```sh
task gen
# Combined separate-module workflow; parent prepares disposable schema 30:
GOWORK=off go test -race -count=1 ./pkg/registry/postgres -run '^TestExternalCompleteAssembly$'
# Identical HTTP/backend suite, independent memory and PostgreSQL selection:
GOWORK=off go test -race -count=1 ./pkg/registry/persistence/conformance ./pkg/registry/postgres \
  -run 'TestConsumerHTTP(Memory|Postgres)$|Test(MemoryJobs|JobsConformance)$'
```

The [combined fixture](../pkg/registry/postgres/testdata/external/assembly_test.go)
runs outside this module's internal import boundary. Its parent alone prepares
migrations and passes the isolated DSN without printing credentials; the fixture
needs no registry-internal imports. It creates ordered source/view definitions,
ingests servers/skills/plugins through public Jobs/Coordinator with replacement IO,
then replaces the fetcher implementation. Catalog reader/admin/publisher/diagnostics
feed both host-mounted HTTP routers. It checks invalid snapshots and cancellation
preserve last-good history/latest/baseline, explicit valid-empty replacement clears
only its source, managed deletion retains other history, consumer winners precede
filters/history, diagnostics remain unshadowed, host middleware is applied, and a
replacement Reader's unusual order/opaque cursor survives unchanged. Constructors
start no ingestion; the host cancels/joins work and closes its servers/pool. Borrowed
pool/file IO remain usable after coordinator shutdown. Existing IO/transport/operator
ownership tests cover their own clients/managers rather than duplicating them here.

Partial external examples remain runnable independently:

```sh
(cd pkg/registry/formats/testdata/external && GOWORK=off go test -mod=readonly ./...)
(cd pkg/registry/persistence/testdata/external && GOWORK=off go test -mod=readonly ./...)
(cd pkg/registry/ingest/testdata/external && GOWORK=off go test -mod=readonly ./...)
(cd pkg/registry/catalog/testdata/external && GOWORK=off go test -race -mod=readonly ./...)
(cd pkg/registry/httpapi/testdata/external && GOWORK=off go test -race -mod=readonly ./...)
(cd pkg/registry/operator/testdata/external && GOWORK=off go test -mod=readonly ./...)
```

These use formats-only, independent backend/catalog, reconciliation-only and
replacement HTTP applications without standalone configuration or listeners unless
the host explicitly creates them. The combined fixture is invoked through its parent,
not by supplying an ambient or production DSN to the external module.

## #906 definition of done and scope

| Requirement | Executable evidence / artifact |
| --- | --- |
| External format/validation use without storage/server | `pkg/registry/formats` external-module tests and [format example](../pkg/registry/formats/testdata/external/consumer_test.go); `formats.DecodeUpstream`, public publication validators and all-kind serializers. |
| Reconciliation populates alternative storage without HTTP | `pkg/registry/ingest.TestExternalAssembly` → external `TestHostAssembly`; `pkg/registry/reconcile` coordinator tests over generated Jobs; independent `TestMemoryJobs` and shared `RunJobs` exercise actual destination/state semantics. |
| Every selected persistence capability replaceable | Public Sources/Views/Reconciler, EntryReader/SnapshotWriter/ManagedPublisher, ViewEntryReader, EntryDiagnostics and Jobs; `TestMemoryConformance`, `TestMemoryEntries`, `TestMemoryJobs`, `TestMemoryViewEntries`, diagnostic/publication conformance and corresponding PostgreSQL suites. |
| Host-provided HTTP service and already-selected pages | `TestExternalHTTPAssemblyAndImportGraph` → `TestHostMountsReplacementApplication`, `TestHostMountsIndependentAdminApplications`, `TestHostAdmissionAndAuditAssembly`; combined external `TestCompletePublicAssembly`. |
| Standalone uses the same public construction contracts | CLI `TestServeSelectsCatalogPreparationAndPropagatesListenerFailure`, `TestDefaultServeOAuthCatalogAndRetiredRoutes`; app `TestCatalogCompleteHTTPPostgresAndConsumedRestart`, `TestCatalogPreparationMandatoryPhases`. `internal/app` owns standalone preparation, not an alternate catalog implementation. |
| PostgreSQL manual publish/remove and operator discovery | Combined fixture; `TestAdminHTTPPostgres`, publication conformance; operator `TestRegisteredOperator` in isolated envtest and app `TestCatalogOperatorEmptyStartupAndAPIScopeCRUD`. |
| Ownership, atomic replacement, latest/version and pagination specified/tested | Shared entries/publication/jobs/view conformance; identical `httpapi/conformance.RunConsumer` in `TestConsumerHTTPMemory` and `TestConsumerHTTPPostgres`; [persistence](registry-persistence.md), [jobs](registry-persistence-jobs.md), [consumer reads](registry-consumer-reads.md). Duration-equivalent update/reconcile retains lease/baseline; real duration changes fence both backends. |
| Shared contracts free of user/tenant/claims/policy abstractions | Combined parent negative transitive import assertions for model/formats/persistence/catalog/httpapi, existing external graph tests, policy-free public contracts; standalone Cedar stays in `internal/authorization`. |
| Remove old claims/RBAC with explicit retired config/data handling | Removed service/factories/routers/unleased runtime; config presence tests, app `TestCatalogMissingAndStaleReceiptsLeaveDatabaseUntouched`, CLI `TestOneFinalBinaryOfflineUpgrade`, migrations 27–30 and database cleanup tests. No silent claims stripping/approval. |
| Examples, backend conformance, docs and release notes | Commands above, this matrix/release notes, [standalone assembly](registry-standalone-assembly.md), [migration procedure](registry-exposure-migration.md), public package documentation and scoped rules. |
| Preserve supported all-kind formats and export MCPServer/VirtualMCPServer/MCPRemoteProxy | All-kind format/entry/HTTP fixtures; operator conversion/fetcher tests and `TestRegisteredOperator`. Exact export opt-in and complete namespace snapshots remain. Identity collisions are characterized, **not approved/resolved**. |
| Both extensibility levels, not only read-only storage | Combined dependency injection then fetcher/Reader implementation replacement; independent admin replacement external fixture and generated coordinator/backend contracts. No universal SQL API, plugin framework or personalized selection in storage. |

The issue's earlier “no generic authorization framework” wording is implemented as
no policy engine in shared components. The separately selected standalone-only Cedar
instance/action boundary protects enabled administration; it is not resource RBAC or
an identity-aware persistence contract.

## #915 acceptance criteria

| Requirement | Evidence |
| --- | --- |
| External modules compile **and run** through public APIs in CI | Parent Go tests execute each separate module with `GOWORK=off -mod=readonly`; new `TestExternalCompleteAssembly` executes the combined workflow, not just nil constructor checks. Existing CI PostgreSQL/unit jobs run these parent packages. |
| Independent backend conformance and standalone compatibility | Same `RunConsumer` wire/query/history cases on independent memory and PG; shared definitions/entries/jobs/views/publication/diagnostics; current CLI/app HTTP/security acceptance. Memory selection does not call PostgreSQL/production selection code. |
| No accidental PG/Kubernetes/config imports in portable packages | Actual `go list -deps` negative assertions in the combined parent and separate core/HTTP fixtures. Optional `postgres` deliberately imports pgx, `internal/db` and transactional writer machinery; operator deliberately imports Kubernetes. No assertion that every adapter is dependency-free. |
| Ownership, startup, readiness, errors, cancellation and shutdown | Combined workflow plus ingestion/operator external examples, coordinator shutdown tests and app `TestCatalogShutdownJoinsCallerOwnedInlineBeforeCleanup`, `TestCatalogTerminalRuntimeErrorJoinsAndClosesExactlyOnce`; [assembly](registry-standalone-assembly.md). `NewEntries` explicitly performs latest repair; other constructors do not start application workers/listeners. |
| Forward cleanup after obsolete readers/writers removed | Immutable migration `000030_claims_schema_cleanup` and database `TestClaimsCleanupRefusalLeavesClean29`, `TestClaimsCleanupPreservesConsumedEvidence`, `TestSchema30CleanupCancellationPreservesEvidence`; historical migrations unchanged. |
| Retired security input cannot become unrestricted deployment | Config/API presence tests reject null/empty/aliases/environment and top-level claims; operator full-scope retired-annotation preflight; enabled admin requires verified OAuth plus Cedar. |
| Upgrade exposure, backups, API/config changes and rollback | [Offline guide](registry-exposure-migration.md): single final binary stops at 29, inventory/acknowledge/explicit retire/finalize, then upgrade to 30. CLI `TestOneFinalBinaryOfflineUpgrade`, target-continuity and migration refusal tests. No downgrade or automatic archive restoration at 30. |
| Superseded assembly and claims/RBAC remnants removed | Current CLI and public services/routers only; private version-1 fingerprint/raw-history tooling intentionally retained for immutable receipt compatibility. Archives/receipts are evidence, not live resource-authorization metadata. |
| Compatibility/versioning and module split decision | Release notes and measured import-graph decision below; public DTO contracts and independent wire fixtures. |
| Every #906 done item verified | Requirement-by-requirement table above; only the explicit operator identity decision remains unresolved technical/product scope here. This does not grant deployment exposure consent or replace release/security review. |

## One Go module: measured boundary, not efficiency promise

Retain one Go module. Core model/formats/persistence/catalog/httpapi transitive package
graphs exclude registry-internal code, standalone auth/config, PostgreSQL/pgx and
Kubernetes. Ingestion/reconciliation deliberately reuse internal low-level Git/HTTP/
filter/shutdown helpers without standalone admission/config/PG/Kubernetes. PostgreSQL
and operator pull their explicit adapter dependencies only when imported. External
fixtures prove Go's internal boundary and actual package compilation/execution, not
that the repository's entire `go.mod` is lightweight. Upstream model/format dependencies
remain; module resolution/download size is distinct from runtime import isolation.
There is no demonstrated independent release requirement warranting another module
or synchronized multi-module releases now. A future split requires measured need.

The independent memory backend proves behavior, not efficient implementability on
every production database. Atomicity, fencing, source selection and bound cursors are
contracts; the PostgreSQL query/transaction strategy is one implementation, not a
universal query engine or cross-page snapshot guarantee.

## Compatibility and release notes

This requires a **breaking release** for the removed configuration/API/runtime/schema,
not a silent minor upgrade. No new release number is selected here. The initial public
component API is the supported embedding seam; removed internal monoliths are not an
alternate supported API. Changes to stable exported contracts need normal release
compatibility review; dependency `x/exp` Cedar validation remains experimental.

- Consumer URL prefixes and server/skill/plugin response DTO shapes remain. Server
  envelopes use upstream metadata; extensions omit storage ID/latest/time metadata.
  Counts describe the supplied page. Independent fixed wire fixtures, not production
  serializers as expected-value oracles, check those contracts.
- Whole-name earliest-source selection precedes search, namespace, exclusive update
  time, exact/latest, history and pagination. A failed filter never promotes a lower
  source. This corrects old version-scoped shadowing. Kind/name ownership is separate.
- Search is a case-insensitive **literal substring**; `%`, `_`, backslash and brackets
  are not patterns. This intentionally changes legacy SQL-wildcard interpretation.
- Latest uses the approved Masterminds-parseable semver group, semantic precedence,
  raw lexical tie-break and lexical non-semver group. Preserve exact input spellings;
  startup repairs persisted latest before reads. This slice changes no version semantics.
- Legacy/source-list/other-backend cursors are invalid. New bounded opaque cursors bind
  kind, view incarnation, ordered source incarnations, all query fields including limit,
  ordering and position. Start a fresh page walk after upgrade/reconfiguration. Cursors
  are not credentials, and normal mutations do not promise cross-page snapshot isolation.
- Wire `latest` remains case-sensitive reserved selection; library `Exact: "latest"`
  accesses that literal identity. Managed deletion treats `latest` literally. Server
  histories cap at 1000 without continuation; extension histories cap at 30 with cursor
  metadata but ignore input query pagination. Extension list version/status/time inputs
  remain unsupported rather than newly acquiring selection behavior.
- Retired authz/roles/publicPaths and resource claims fail **by presence**, including
  null/empty values; do not replace them with ignored fields. Protected claims and `/me`
  endpoints are static authenticated 410 when admin is enabled, absent when disabled.
- Stop/drain **all** old readers/writers, verify a full pre-retirement backup, follow the
  offline procedure and never co-own sources with old binaries. Schema 30 preserves
  archives/receipts but cannot reconstruct old claims. Downgrade is refused; rollback
  means an offline verified complete backup restore, not claims rehydration or receipt
  rebinding. No automatic approval follows from unlabeled or empty data.

## Deployment and reference-validation evidence

Build with `task build`, then use `./bin/thv-registry-api`. Final configuration must
point `authorization.policyFile` at a readable absolute, read-only mounted Cedar file
and configure trusted OAuth; enabled admin additionally needs its action permits.
Select an image **containing this final binary** explicitly, not an old `latest` image.
The chart requires `image.registryServerUrl` and `policy.existingConfigMap`; it defaults
`initializeEmptyCatalog` to false. Compose requires `REGISTRY_POLICY_FILE`, mounts it
read-only, and builds from the selected checkout. Only a new **entirely empty** catalog
may opt into `serve --initialize-empty`, Helm `initializeEmptyCatalog: true`, or the
explicit `docker-compose.initialize-empty.yaml` override. Existing catalogs use the
manual 29 → finalize → 30 workflow first, with no empty-install shortcut. See the
[current migration commands](registry-exposure-migration.md#maintenance-procedure).

The official Rust reference actually executed `cedar-policy` 4.13.0 with existing
Rust/Cargo 1.94.0. [Pinned generator/provenance](../internal/authorization/testdata/reference/README.md)
and `TestOfficialReferenceParity` compare the shipped schema, guide policies and 17
cases, including raw Allow+diagnostics that the Go boundary denies. Routine CI uses
hash-bound goldens, not a Rust rebuild. This is bounded parity evidence, **not universal
proof** or stabilization of cedar-go's experimental validator. No runtime authorization
fix was needed for these cases.

Parent #942 at `4f9fa8bc0e6a827c0fa96f6615d8d1550058ef9d` had 14 successful checks,
including actual-image Helm fresh startup through schema 30. That is parent evidence,
not CI for this unpublished slice. Local validation uses disposable PostgreSQL and
pinned Kubernetes 1.33.0 envtest with `USE_EXISTING_CLUSTER=false`, never a user's DB
or cluster. Release publication/review and live deployment validation are separate.

**Remaining decision:** `TestHistoricalCrossKindNameIdentityNotRewritten` retains
rejection of duplicate exact `(kind,name,version)` snapshots and shared-name projection
for distinct exported CRD kinds with differing versions. It does not choose new IDs,
resolve those collisions, or approve that model. The pending user answer is required
before describing the whole epic as complete.

## Local validation of this slice

Completed against disposable PostgreSQL and installed Kubernetes 1.33.0 assets:

- `task gen`, `task lint-fix` (zero lint issues), `task build`, `go vet -p 2 ./...`.
- `USE_EXISTING_CLUSTER=false KUBEBUILDER_ASSETS=/path/to/1.33.0-linux-amd64 GOWORK=off go test -p 2 -race -tags integration -count=1 ./...`: passed, including real registered operator/envtest and all PostgreSQL/CLI migration acceptance.
- External modules: parent tests use `GOWORK=off -mod=readonly`; independent `go vet -mod=readonly ./...` also passed in all seven fixtures, including the combined module.
- Official pinned Rust generator: executed, then reproduced `results.json` byte-for-byte with `--locked`; Go parity passed for all 17 cases.
- CLI and OpenAPI: regenerated into verified-absent temporary paths using the pinned Swag revision; byte comparisons passed. OpenAPI's output directory retained the `thv-registry-api` basename to preserve the generated Go package name. No recursive-deletion docs task was run.
- Affected-document relative links, codespell, Helm lint and both Compose configurations (`config --quiet`) passed. No chart installation or live-cluster/database operation was performed locally.

The first full-suite invocation hit the tool's two-minute wall-clock limit; the
subsequent full run with a sufficient allowance completed successfully. This is
local evidence only; nothing was committed or published.
