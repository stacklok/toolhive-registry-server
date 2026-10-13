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
inline data, credential values, or raw low-level transport errors. The provided
fetch adapters sanitize transport errors; ordinary coordinator fetch/validation
failures are sanitized too. Injected fetcher/backend errors retained during shutdown
must not be logged raw. Job failures store only the persistence allowlisted categories.

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
Cancellation/deadline-only shutdown returns nil from `Run`; an interrupted `Sync`
returns its context error. Wrapped or joined non-cancellation failures from active
work, renewal or independent `Fail` cleanup remain in the returned error, even if
shutdown races with them. Reentrant `Run` returns `ErrBusy`.

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

## Standalone lifecycle

The supervisor is selected by the default CLI/chart
[`NewCatalogApp` composition](registry-standalone-assembly.md) after its mandatory
preparation/receipt barrier. Verified OAuth and Cedar admission protect HTTP outside
these policy-free components; named-view consumer selection belongs to `catalog.Reader`.
Library admission and personalized selection belong to the embedding host. Never
run an old unfenced writer/controller concurrently against these sources.

`internal/sync/standalone.New` constructs an unstarted supervisor over the public
coordinator and injected operator factory. Construction performs no database access,
source listing, fetch, lease, watch, initialization or manager startup. `Preflight` reads
**all persisted sources**, CONFIG/API and linked/unlinked alike, validates public
definitions, and invokes the supplied read-only compatibility check. `Run` repeats
that startup barrier before starting scheduled polling and per-source workers.

Scheduled Git/API/file ingestion stays with `Coordinator.Run` and `Jobs.ClaimNext`;
there is no second source scheduler, hash implementation or CRUD path. Inline files
use the persisted `File.Data` captured by `Coordinator.Sync` **after acquisition**.
Startup and bounded baseline checks recover missed notifications/invalidated
generations; a current valid baseline is not repeatedly fetched or replaced.
`SyncInline(name)` accepts no raw payload and has no direct writer/status shortcut.
Managed sources are never leased. Supply `ingest.New` with the host's existing HTTP,
Git, file and secret-loader dependencies; authenticated Git still needs an explicit
validated file-reference loader, and scheduled filters use the public coordinator.

`Refresh` serializes ownership changes while `Run` is active. Discovery errors are
not deletion: an explicit failed refresh leaves existing workers intact; a failed
periodic backend query terminates the supervisor without writing an empty snapshot.
Source deletion, incarnation replacement and canonical namespace-scope changes
cancel and **join** old workers before starting replacements. Readiness is false
during replacement and until the new worker's startup succeeds. Terminal worker or
scheduled backend failures cancel and join siblings. Shutdown closes neither borrowed
clients nor pools. In-flight discovery is also canceled and joined before `Run`
returns; its completed backend failure is joined with any selected worker failure
or shutdown result. Pure cancellation/deadline shutdown is clean, but mixed errors
retain their non-cancellation causes. The host supplies restart/backoff and bounded
compatibility alerts.

`internal/kubernetes.NewStandaloneOperator` wraps an authoritative `client.WithWatch`
and constructs the **existing public runner**, never a Kubernetes manager. Its List
wrapper checks the actual returned CRD objects before metadata validation/conversion:
only exact `toolhive.stacklok.dev/registry-export=true` opts in; presence of
`toolhive.stacklok.dev/authz-claims`, including empty or malformed values, rejects the
**entire** snapshot. Missing URL/description cannot hide that annotation. Non-exported
resources with the annotation do **not** block ingestion. No annotation is parsed,
stripped, propagated, or converted into a policy callback in a public package.

`PreflightStandaloneOperator` performs read-only authoritative lists for every kind
in every persisted namespace; empty scope means all namespaces. Startup, event and
periodic snapshots all pass through that same checked List wrapper, not a separate
unchecked fetch after preflight. Incomplete/error lists fail safely. Later annotation
introduction emits only the fixed remediation diagnostic through the host callback,
records a categorized fetch failure and preserves the last good snapshot. The public
converter still owns explicit export URL/description handling and valid empty startup.

### Cutover requirements enforced by standalone

- Stop/drain legacy unfenced writers/controllers before selecting this owner. No
  constructor that starts a manager can participate in the cutover preflight barrier.
- Revalidate/consume the acknowledged offline exposure transition, final configuration
  and Cedar policy before any initialization, lease, fetch, watch or serving. Neither
  the supervisor itself nor its tests supply consent or clear historical claims.
- Validate/remediate historical definitions before initialization. Public `Kind`
  rejects schedules/filters on inline, managed and Kubernetes sources; standalone
  preflight rejects unsupported configuration by presence. If historical inline
  filters selected data, explicitly preserve that selection in the input catalog
  rather than silently dropping filters. Repeat exposure review when effective
  configuration changes.
- Persist the final effective Kubernetes namespace scope consistently. Empty public
  scope means all namespaces; any later global watch-namespace fallback must be
  resolved into the stored definition used by both fence and runner. Do not narrow
  only the wrapper while leaving stored scope broader. Namespace ordering alone does
  not change ownership; duplicates/invalid namespaces remain rejected by the runner.
