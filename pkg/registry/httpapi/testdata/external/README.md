# Replace ConsumerPages without a bundled backend

This independent Go module contains a complete, synthetic replacement application
in [`pages.go`](pages.go), runnable HTTP [`Example`](pages_test.go) and focused tests.
It is distinct from the [mTLS PostgreSQL tutorial](../../../../../examples/embedding/README.md):
there is no database, source/view setup, ingestion, standalone configuration, Cedar
or bundled authentication implementation here.

The application implements only `httpapi.ConsumerPages` (`List`, `Get`,
`ListVersions`). Public persistence **value types and classified errors** appear
because that is the ConsumerPages signature; it implements no persistence interface.
The host mounts `ConsumerServerRouter` under `/registry`, with exactly the three
server routes, `StrictQueries`, `PaginateHistory`, `AllowExactName`, host admission
and optional application-error mapping. Zero-option legacy behavior is tested in
the adapter suite, not changed by this example.

## Run

From the repository root, generate before tests:

```sh
task gen
(cd pkg/registry/httpapi/testdata/external && GOWORK=off go test -race -mod=readonly ./...)
go test -race -count=1 ./pkg/registry/httpapi ./pkg/registry/httpapi/conformance/pages
```

The parent `TestExternalHTTPAssemblyAndImportGraph` executes the separate module
and checks its complete test import graph, including direct conformance. PostgreSQL,
database/internal app/config, ingestion, reconciliation, operator, backend conformance,
Cedar, JWT, Viper and Kubernetes dependencies are forbidden. No DB/container test
is needed for this fixture.

## What the host owns

- Fixed, **synthetic fixture contexts**, attached by test wiring, never identity
  headers. These are not verified authentication evidence. Use your own credential
  verifier in production; the PostgreSQL tutorial demonstrates actual mTLS.
- Current admission and whole-name visibility in all three operations, including
  latest and history. Both are reevaluated between pages. An issued cursor never
  grants access, even when replayed under another synthetic context. Changed-query
  and cross-fixture rejection demonstrate cursor integrity, not authorization.
- Deliberately descending raw-version order and opaque continuations with spaces
  and punctuation. HTTP does not reorder, filter, truncate or refill host pages.
  The same coherent dataset has 1003 versions; list/search and history both traverse
  every expected record. Details and metadata are checked using independent wire
  structs, including the exact JSON number `9007199254740993`.
- `Cache-Control: private, no-store` on success and error, including admission
  denials. Missing and inaccessible names have identical generic HTTP 404 bodies.
  Default errors are sanitized; an optional mapper returns a safe dependency 503,
  and does not intercept query-validation or admission errors.
- Listener startup/shutdown and request cancellation. Construction calls no
  application methods. `httptest` lifecycle coverage starts all three operations,
  cancels their host base context, stops the listener and joins handlers; no pool
  or background production framework is introduced.

## Reuse direct conformance

[`httpapi/conformance/pages.Run`](../../conformance/pages/pages.go) accepts a setup
callback returning a ready application, synthetic context, ordered expected records,
a known hidden name, a missing name, a search term, and host change callbacks. See
`TestDirectConsumerPagesConformance` for a concrete invocation. Each scenario gets
fresh state. Records identify the expected latest detail explicitly, independently
of host order. The fixture needs two versions of the first name to exercise history
continuation, and all expectations are server-only.

This entry point imports no backend conformance package and needs no definitions,
snapshots, ingestion or jobs. The sibling `httpapi/conformance.RunConsumer` remains
available unchanged for backend/default-catalog conformance. The older `http_test.go`
contains a small list pass-through/admission test double, not the complete replacement
example. See the [embedding guide](../../../../../docs/registry-embedding-validation.md)
for other compositions and ownership contracts.
