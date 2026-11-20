# Registry library (`pkg/registry`)

> **Experimental.** These packages live in the server's Go module and are
> versioned with its releases. Their API may change in minor releases until we
> declare it stable. Pin an exact version if you depend on them.

The packages let another Go program host the registry's catalog without running
the standalone server. They carry no identity or claims, and only the optional
`cedar` package carries a policy engine. The host decides who may read or
change what.

| Package | Purpose |
|---|---|
| `model` | Entry versions and the total version order shared by every store |
| `formats` | Payload validation and serialization, independent of storage |
| `persistence` | Storage-neutral contracts: definitions, entries, view reads, sync jobs |
| `catalog` | Small reader, admin, publisher and diagnostics capabilities over `persistence` |
| `httpapi` | Consumer and optional admin HTTP routers; authorization is a host-supplied `Authorizer` |
| `ingest` | Git, API and file source fetching, each with its source type's acceptance policy |
| `reconcile` | Leased, fenced source reconciliation over `persistence.Jobs`; storage-safety check and sync logging |
| `operator` | Optional Kubernetes adapter that exports ToolHive CRDs as a source |
| `postgres` | PostgreSQL implementation of the `persistence` contracts |
| `cedar` | Optional reference Cedar authorizer for the `httpapi` routes |

## Database

`postgres` writes against the same schema as the standalone server. Apply the
registry migrations before constructing writers, either with the server's
`thv-registry-api migrate up` command or from Go with `database.MigrateUp`
(package `github.com/stacklok/toolhive-registry-server/database`, which embeds
the migration files and links no test dependencies). Writes refuse to run
unless the database is at `postgres.RequiredSchemaVersion`.

The first `postgres.NewEntries` after upgrading across migration 25 rebuilds
every latest-version pointer once, holding write locks on the entry tables
for the length of one scan. Later calls only read a marker row.

The standalone server is built on these packages. Running the server and a
library host against the same database at the same time is not supported: the
server keeps authorization claims in columns the library never writes.

## Keeping host data in step with writes

A host that stores its own per-entry data (the server stores authorization
claims) passes `postgres.WithEntryWriteHook` to `NewEntries` and `NewJobs`.
The hook's `BeforeEntryWrite` and `AfterEntryWrite` run inside the adapter's
serializable write transaction for every snapshot, publish and delete, so the
host's data commits or rolls back with the entries. A hook error aborts the
write and is returned unchanged; a serialization failure retries the whole
transaction, so a hook may run more than once and must act only through the
transaction it is given.

## Cedar authorization

`cedar` is an optional, route-level authorizer for the `httpapi` routers. It
does not filter entries: a request is either admitted to a route or rejected.

- Every consumer route is the action `catalog.read` on `Registry::View::"<name>"`,
  where the name comes from the `{registryName}` path segment. A policy can
  grant or deny whole views.
- Admin routes are instance-wide actions on `Registry::Instance::"catalog"`:
  `sources.read|write|delete`, `views.read|write|delete`, `entries.publish`
  and `entries.delete`. `Authorizer.Protect` guards host routes such as
  `/openapi.json` with `metadata.read` or another instance action.
- The host authenticates the request and hands the verified caller to
  `Options.Identity`. `Mapping` turns token claims into the principal's
  optional `groups` and `scopes` sets; check them with `ValidateMappings`.
- Responses: 401 for anonymous callers on authenticated-only actions or for
  malformed mapped claims, 403 on deny, 503 when evaluation fails, 500 on
  wiring faults (no identity, unknown route). `Options.OnDecision` receives
  each decision for audit logs.

```go
engine, err := cedar.Load(policyPath) // "" uses the permit-all default
authz, err := cedar.NewAuthorizer(engine, cedar.Options{Identity: identityFromRequest})
consumer, err := httpapi.ConsumerRouter(reader, httpapi.ConsumerOptions{Decorate: authz.Decorate})
admin, err := httpapi.AdminRouter(ops, httpapi.AdminOptions{Decorate: authz.Decorate})
```

**The default policy permits everything.** Without a policy file, any caller
that passes authentication may perform every action, so with the admin routes
mounted, every authenticated caller is a full admin. Supply a policy before
enabling admin routes. `NewAuthorizer` fails if the library adds a route it has
no action for, so an upgrade cannot silently expose a new route.
## Validation and logging

Publication (`persistence.ValidateSnapshot`, `Entry.Validate`) is strict. Synced
data is not: fetchers apply their source type's policy, and
`persistence.ValidateStorableSnapshot` checks only what storage and the read
routes need, on the filtered snapshot. See `docs/registry-sync.md` for both
rule sets. The library logs sync progress and failures with `log/slog` on the
default logger; returned errors and stored status carry only a category, and
`ingest.Cause` recovers a fetch failure's redacted cause.

## Importability

`testdata/external` modules in each package build and test against the library
from outside this module, and fail if an assembly pulls in server-only
dependencies (configuration, authentication, Kubernetes or PostgreSQL where they
do not belong).
