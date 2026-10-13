# Managed publication and unshadowed diagnostics

`catalog.Publisher` and `catalog.Diagnostics` are independently replaceable
capabilities, not a generic admin service. Standalone selects them behind mandatory
Cedar admission after [offline preparation/finalization and schema-30 migration](registry-exposure-migration.md).
Public [admin HTTP](registry-admin-http.md) serializes their results without identity
or resource claims; the obsolete monolithic service/writer runtime is removed.

These are **raw host-owned operations, not a standalone authorization boundary**.
Hosts authorize every call and decide who can publish or inspect diagnostics.
There are no principals, caller identities, Cedar rules or claims in these
contracts. Public PostgreSQL writes require clean schema 30 after explicit offline
finalization; historical schemas fail closed. Never co-own a source with legacy writers.

## Independent construction and replacement

```go
// All capabilities are initialized and owned by the host. Constructors do no IO,
// start no work and own/close none of their dependencies.
publisher, err := catalog.NewPublisher(managedPublisher)
if err != nil { return err }
diagnostics, err := catalog.NewDiagnostics(entryDiagnostics, sourceStatusReader)
if err != nil { return err }

stored, err := publisher.Publish(ctx, persistence.Entry{
    Kind: persistence.SkillKind, Name: "item", Version: "1.0.0",
    Skill: &model.Skill{
        Namespace: "com.example", Name: "item", Version: "1.0.0",
        Description: "Example skill",
    },
})
if err != nil { return err }
versions, err := diagnostics.ListSourceVersions(ctx, "managed")
if err != nil { return err }
rawView, err := diagnostics.ListViewVersions(ctx, "public")
if err != nil { return err }
status, err := diagnostics.Status(ctx, "managed")
if err != nil { return err }
_ = stored
_ = versions
_ = rawView
_ = status
```

Inject `persistence.ManagedPublisher`, `EntryDiagnostics`, and
`SourceStatusReader` independently. PostgreSQL `Entries` supplies the first two;
`Jobs` already satisfies the status-only interface. No `Sources`/`Views` lookup
is needed: diagnostic listings resolve definition existence in their snapshot,
and status resolves its own source incarnation. Hosts can replace either
application implementation while retaining its consumers. Dependencies must be
initialized and non-nil; typed nil implementations are outside the contract.

The executable external-module fixture exercises default and replacement
application implementations over independent public-only storage, then formats
all three published payload kinds using public serializers. Its dependency-graph
test excludes internal, PostgreSQL, Viper and Kubernetes packages.

## Publication and deletion

`Publish` reuses `persistence.Entry` and its shared publication `Validate` gate:
exactly one payload matching `Kind`, with required matching name/version and
existing server/skill/plugin format validation. Input IDs, source destinations,
latest flags and skill/plugin storage timestamps are rejected. Catalog validates
at its input edge; storage still validates before writing. Metadata-size limits
remain backend-owned. No extra storage validation implementation or competing
payload schema is introduced.

Publication adds one exact `(kind,name,version)` to the existing singleton
managed source. It never accepts source/view destinations, creates a managed
source implicitly, or replaces a snapshot/history. Exact duplicates return
`ErrConflict`, even for identical payloads; they are **not** definition-name
`ErrAlreadyExists`. `DeleteManaged` takes an **exact literal version**: `"latest"`
is a stored spelling, never a selector. Both operations atomically maintain
related metadata and kind/name latest pointers under `model.CompareVersions`,
preserving other versions, names, kinds and sources. The existing PostgreSQL
serializable publish/delete implementation is reused.

`persistence.ErrNoManagedSource` refines `ErrNotFound`. PostgreSQL emits it only
at the authoritative managed-singleton lookup **inside the write transaction**,
not a raced service pre-read or parsed error message. A missing version with an
existing managed source remains ordinary `ErrNotFound`, distinguishable with
`errors.Is`. Invalid inputs are `ErrInvalid`; unavailable storage is
`ErrUnavailable`; context cancellation/deadlines propagate. Ownership errors
(`ErrConfigOwned`, `ErrKindChange`, `ErrInUse`) remain the definition operations'
separate contract; legacy claimed-data guards remain general `ErrConflict`.

There are no publication lifecycle callbacks, job mutations, readiness checks,
background workers or retries at the application layer. Definition CRUD and its
optional synchronous callbacks retain their previous
[post-commit semantics](registry-definition-administration.md#committed-follow-up-failures).

## Complete diagnostics, not consumer selection

`persistence.EntryDiagnostics` returns lightweight `DiagnosticVersion` rows for
sources and `ViewDiagnosticVersion` rows for views. Every stored version of all
three kinds is included, **including shadowed duplicate names and identical
versions from different sources**. It does not use `catalog.Reader` or
`ViewEntryReader`: those deliberately choose a whole-name winning source.
These diagnostic lists retain the old admin surface's **complete** semantics:
no silent cap, truncation, pagination, search or dedup. The host should account
for the memory/response cost before exposing a large raw catalog.

Rows contain kind, name, exact version, title, description and published/updated
storage timestamps (`CreatedAt`/`UpdatedAt`). View rows add source name, opaque
source incarnation ID and zero-based source order position. They contain no
payload, claims, source definition/configuration or freeform backend error.
A future HTTP adapter can group source rows by `(kind,name)` and preserve the
existing admin wire fields; this slice does not define new HTTP serialization.

Ordering is ascending **byte-lexical** `(name, version, kind)` with source position
as the view tie-breaker, then source name for equal historical positions (the
existing SQL schema permits those), not semantic version ranking. PostgreSQL
explicitly uses `COLLATE "C"`. Existing empty sources/views return a non-nil empty slice;
missing definitions return `ErrNotFound`. Stored timestamps are UTC metadata,
not CAS revisions; historical NULLs read as zero, not invented dates. Reads
neither touch timestamps nor alias stored values.

Each listing resolves existence, links and rows in **one coherent snapshot**.
PostgreSQL uses a read-only repeatable-read transaction and parameterized SQL
projections, without loading payload or claims columns. The independent memory
test backend holds its state lock and constructs detached rows, not a wrapper
around PostgreSQL. Coherence is per call only, not across calls or operations.
Unavailable reads return sanitized `ErrUnavailable`; context errors propagate.

## Sanitized source status

`SourceStatusReader.Status(ctx, name)` returns the existing `JobStatus` only.
There is no `Acquire`, `Renew`, `Fail`, configuration or lease-token capability.
Pending/no-attempt and managed sources remain pending without an applied
baseline. Phases and baseline validity are distinct: failed attempts retain
prior successful counts/hashes; a changed definition invalidates its baseline.
Only the existing allowlisted `FailureCategory` is returned; legacy arbitrary
error messages and source configuration are never exposed.

Status is authoritative for its own returned `SourceID`. No separate definition
lookup is combined with it, so source deletion/recreation cannot silently join
old definition metadata to new-incarnation status. **Separate calls are not an
atomic definition/status observation.** Hosts that combine status with previously
read definitions must compare incarnation IDs and handle a mismatch explicitly;
never imply cross-call atomicity. Storage failures are errors, not readiness or
job mutation. Replacement status backends must honor the same sanitization and
non-aliasing contract.

Shared publication/diagnostic conformance runs against PostgreSQL and independent
memory storage, including complete listings above consumer caps, kind/name
collisions, shadowed duplicates, lexical order, metadata, cancellation, literal
latest deletion and coherent link mutation. PostgreSQL-specific tests additionally
exercise claimed/NULL historical rows without payloads, error sanitization,
source reincarnation and a forced link mutation between definition and row reads.
