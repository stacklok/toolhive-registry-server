# Registry library (`pkg/registry`)

> **Experimental.** These packages live in the server's Go module and are
> versioned with its releases. Their API may change in minor releases until we
> declare it stable. Pin an exact version if you depend on them.

The packages let another Go program host the registry's catalog without running
the standalone server. They carry no identity, claims or policy engine. The host
decides who may read or change what.

| Package | Purpose |
|---|---|
| `model` | Entry versions and the total version order shared by every store |
| `formats` | Payload validation and serialization, independent of storage |
| `persistence` | Storage-neutral contracts: definitions, entries, view reads, sync jobs |
| `catalog` | Small reader, admin, publisher and diagnostics capabilities over `persistence` |
| `httpapi` | Consumer and optional admin HTTP routers; authorization is a host-supplied `Authorizer` |
| `ingest` | Git, API and file source fetching with full snapshot validation |
| `reconcile` | Leased, fenced source reconciliation over `persistence.Jobs` |
| `operator` | Optional Kubernetes adapter that exports ToolHive CRDs as a source |
| `postgres` | PostgreSQL implementation of the `persistence` contracts |

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

## Importability

`testdata/external` modules in each package build and test against the library
from outside this module, and fail if an assembly pulls in server-only
dependencies (configuration, authentication, Kubernetes or PostgreSQL where they
do not belong).
