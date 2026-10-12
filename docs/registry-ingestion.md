# Reusable ingestion and operator export

`pkg/registry/ingest`, `pkg/registry/reconcile`, and optional
`pkg/registry/operator` implement source ingestion over the public
[persistence jobs contract](registry-persistence-jobs.md). They own registry
formats, filtering and reconciliation, not users, tenants, claims, authorization
or personalized queries. No constructor loads Viper, starts goroutines/listeners,
opens a pool, or migrates storage. Supplied clients, pools and managers remain
host-owned. Core ingestion does not import Kubernetes, PostgreSQL, auth or app
assembly. This is package isolation within the existing module, not a new module.

## Source adapters and host coordinators

`ingest.New(ingest.Options{})` supplies the existing bounded HTTP and in-memory
Git implementations. `Options.HTTP`, `Git`, `ReadFile` and `LoadSecret` let the
host replace I/O without replacing formats. All callbacks must honor cancellation;
ordinary OS file reads cannot interrupt a blocked filesystem syscall. Definitions
are trusted host configuration: do not expose this as an arbitrary URL/file fetch
endpoint. Hosts restrict network egress, redirects, paths and credential access
for their environment. Default HTTP retains the existing size/status checks,
redirect handling and request timeouts. Source timeouts also bound the overall
fetch; absent an override the overall bound is five minutes.

Git credentials remain absolute file **references**, not persisted plaintext.
Authenticated Git requires a host `LoadSecret(ctx, absolutePath)` callback that
validates and resolves the reference (including symlinks) before reading it.
No automatic environment or secret loading is performed. Do not log definitions,
inline data, credential values, or raw low-level transport errors. The high-level
fetcher and coordinator return sanitized fetch/validation errors; job failures
store only the persistence allowlisted categories.

`Fetcher.Fetch(ctx, definition)` returns `ingest.Result{Snapshot, Hash}`. A host
can use it without Jobs, or supply its own implementation of `reconcile.Fetcher`.
Git and file snapshots support servers, skills and plugins; inline `File.Data` is
literal registry JSON, not base64. Their hashes cover the original bytes. The
upstream MCP API adapter retains `/openapi.yaml` identification and paginated
`/v0.1/servers` fetching; that upstream protocol supplies **servers only**, not
ToolHive extension inventories. It fails on incomplete/repeated pagination, HTTP
errors and invalid data. API hashes cover snapshot `Data`, excluding the generated
timestamp. No adapter reports an error as an empty successful snapshot.

The existing nonempty-server import rule remains. The reusable adapter additionally
checks `persistence.ValidateSnapshot`, so every fetched entry must be acceptable
to the public fenced writer (including server schema URLs and unique version keys).
An invalid entry is not hidden by an ingestion filter. Operator snapshots may be
empty; a validated import may also become empty after filtering.

## Provided coordinator

Complete host assembly (the caller has already created the source and Jobs backend):

```go
package embedding

import (
    "context"
    "time"

    "github.com/stacklok/toolhive-registry-server/pkg/registry/ingest"
    "github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
    "github.com/stacklok/toolhive-registry-server/pkg/registry/reconcile"
)

func Sync(ctx context.Context, jobs persistence.Jobs, sourceName string) error {
    coordinator, err := reconcile.New(jobs, ingest.New(ingest.Options{}), reconcile.Options{
        PollInterval: time.Second,
        LeaseDuration: time.Minute,
    })
    if err != nil { return err }
    return coordinator.Sync(ctx, sourceName)
}
```

`Sync` acquires a lease **before** fetching its captured source definition. It
supports inline/manual sources; managed publication is never acquired. `Run(ctx)`
instead polls `Jobs.ClaimNext`, which excludes managed, Kubernetes and inline-file
sources. Both durations must be positive; lease duration has the Jobs precision
and 24-hour limits. There is one scheduled worker, and every attempt or empty poll
waits `PollInterval`. Storage controls due/retry scheduling. Different coordinators
or explicit calls can work on separate sources; Jobs serializes each source.

The coordinator validates the entire fetched snapshot, then applies the existing
name/tag filters: exclude wins, name globs cross slashes, server tags use exact
matching, skills/plugins use `namespace/name` name filters only. Invalid patterns
fail validation rather than accidentally clearing a catalog. `FilterHash` is SHA256
of the JSON filter definition (`null` when absent), independent of the fetch hash.
Filter order is preserved, so equivalent reordered definitions may cause a harmless
replacement. Counts come from the committed filtered set, never the original fetch.
These filters are ingestion selection, **not identity policy**.

A no-change acknowledgement requires a valid current baseline and both hashes;
`Complete` conflict falls back to committing the actual fetched snapshot. Initial
snapshots, definition changes, and direct-write invalidation therefore cannot be
acknowledged without applying data. Database lease expiry/generation/source ID is
authoritative, not the local clock or a source name reused after deletion.

Leases renew during fetch **and commit**. Renewal waits are bounded to one-third
of the lease: if a commit holds the source-row lock too long, renewal failure
cancels it and the backend rolls it back. Choose a lease comfortably larger than
the expected commit duration. Shutdown cancels I/O/renewal, joins the renewal worker,
and attempts `Fail` with an independent ten-second cleanup context. Stale tokens
never overwrite successor status. Injected Jobs and fetchers must honor contexts.

`Ready()` is false before `Run`, true after the first successful claim query, and
false after stop/fatal backend failure. It is not “every remote source succeeded”.
Fetch/validation failures are safely recorded and retried; infrastructure errors
return from `Run`. A source deleted or recreated during acquired work is a stale
attempt, not a fatal storage failure; polling continues with other sources.
Cancellation returns nil from `Run` and a context error from
an interrupted `Sync`. Reentrant `Run` returns `ErrBusy`.

The [separate-module executable fixture](../pkg/registry/ingest/testdata/external/consumer_test.go)
assembles a real inline fetcher with generated Jobs expectations, without a DB or
listener. Normal tests execute it and inspect its **test-inclusive** dependency
graph, rather than merely compiling exported declarations.

## Optional ToolHive operator

`operator.NewFetcher(reader, source)` takes a Kubernetes reader and a persisted
Kubernetes source definition with an incarnation ID. Use an authoritative API
reader; a custom cache must already be synced and cover the complete scope.
`Fetch` lists **all configured namespaces and all three CRD kinds** before returning:
`MCPServer`, `VirtualMCPServer`, `MCPRemoteProxy`. Empty namespaces means all namespaces.
A list error or continuation token cannot become a partial/empty snapshot.

Only exact `toolhive.stacklok.dev/registry-export=true` opts in. Explicit
`registry-url` and `registry-description` annotations remain required; no readiness
gate or cluster-internal status URL replaces them. Shared conversion preserves
version/image fallback, tools, title, icons, category and Kubernetes metadata.
The public fetcher fills network package transport URLs from the **same explicit
registry URL**, making legacy-converted packages acceptable to the fenced schema
validator. Malformed export metadata is skipped; duplicate name/version keys fail
the snapshot rather than choosing one CRD. No identity annotation is parsed,
propagated, or used to select entries, including malformed `authz-claims`.

For automatic delivery, construct a direct `client.WithWatch` and register the
runner with a **host-owned** manager:

```go
package embedding

import (
    "time"

    "github.com/stacklok/toolhive-registry-server/pkg/registry/operator"
    "github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
    "github.com/stacklok/toolhive-registry-server/pkg/registry/reconcile"
    "sigs.k8s.io/controller-runtime/pkg/client"
    "sigs.k8s.io/controller-runtime/pkg/manager"
)

func Register(mgr manager.Manager, jobs persistence.Jobs, source persistence.SourceDefinition) (*operator.Runner, error) {
    // The host must register ToolHive's v1beta1 types in mgr.GetScheme().
    api, err := client.NewWithWatch(mgr.GetConfig(), client.Options{Scheme: mgr.GetScheme()})
    if err != nil { return nil, err }
    runner, err := operator.NewRunner(api, jobs, source, reconcile.Options{
        PollInterval: 10 * time.Second,
        LeaseDuration: time.Minute,
    })
    if err != nil { return nil, err }
    if err := runner.Register(mgr); err != nil { return nil, err }
    return runner, nil // host calls mgr.Start(ctx), not runner.Run as well
}
```

Registration adds a leader-only runnable. The host configures manager leader
election/lease namespace and starts/stops the manager. Alternatively call
`runner.Run(ctx)` directly; it creates no Kubernetes leader lease, and persistence
leases serialize replicas. The runner owns only its watches and work goroutines.
It uses authoritative API lists, not the manager cache, eliminating an incomplete
cache startup barrier. Kubernetes RBAC must allow list/watch for all three kinds
in the entire configured scope (and manager leader-election access when enabled).

Watches start before a full startup reconciliation, **including zero resources**.
`Ready()` becomes true only after successful fenced reconciliation. Startup
list/access/commit failures and all watch errors/closures return from Run (or
manager Start), never log-only. The host decides restart/backoff. Later transient
refresh failures retry at PollInterval while preserving the last good catalog and
readiness. Periodic resync also protects against missed events. Opt-out and delete
remove only this source's entries; restart clears resources deleted during downtime.

A runner captures source ID and namespace scope. Changing either requires stopping
it and constructing a new runner from the current definition. Jobs generation
invalidates old fetched data; the old runner rejects the new scope/incarnation.
The new runner's startup replacement removes entries from removed namespaces.
No managed or unrelated source can be targeted by this adapter.

Real manager/envtest acceptance is included in CI with the existing pinned 1.33.0
assets; fake-client tests cover complete scope, opt-in/out, metadata, listing and
watch failures, incarnation/scope mismatch, and startup readiness.

## Temporary standalone compatibility boundary

The standalone app is **not rewired** to these unrestricted components. Its
claim-aware service, legacy coordinator/writer, and legacy operator claims parsing
remain active. Only policy-free machinery is shared: upstream API pagination,
existing transport/format/filter code, and operator metadata conversion. Filter
config aliases preserve standalone YAML/JSON behavior. Do not allow the legacy
writer/controller and leased coordinator/operator to co-own a source. Public
PostgreSQL writes continue rejecting claimed legacy data rather than erasing it.

Claim removal remains #912, consumer/application services #913, and complete
standalone reassembly #914. This does not solve named-view consumer queries,
personalized query selection, or host authorization.
