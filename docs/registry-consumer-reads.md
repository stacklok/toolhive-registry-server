# Consumer-view catalog reads

`pkg/registry/catalog` provides a replaceable `Reader` with `List`, `Get` and
`ListVersions`. `NewReader` accepts only `persistence.ViewEntryReader`, validates
without IO, and starts no work. No standalone `RegistryService`, HTTP, identity,
configuration, PostgreSQL or Kubernetes types are part of the core contract.

**Raw storage, not an authorization boundary:** hosts authorize calls and choose
which views to expose. Personalized selection requires a host-owned coherent
application service, not per-entry callbacks in this reader. Standalone selects these
components after mandatory [exposure preparation](registry-exposure-migration.md),
with verified identity and instance/action Cedar outside the public contracts.

```go
// backend implements persistence.ViewEntryReader (PostgreSQL or host storage).
reader, err := catalog.NewReader(backend)
if err != nil { return err }
page, err := reader.List(ctx, "public", catalog.Query{
    Kind: persistence.SkillKind, Search: "weather", Limit: 50,
})
if err != nil { return err }
version, err := reader.Get(ctx, "public", catalog.EntryName{
    Kind: persistence.SkillKind, Name: "weather", Namespace: "com.example",
}, catalog.VersionSelector{Latest: true})
history, err := reader.ListVersions(ctx, "public", catalog.EntryName{
    Kind: persistence.SkillKind, Name: "weather", Namespace: "com.example",
}, catalog.PageRequest{Limit: 30})
```

The PostgreSQL implementation is on `postgres.Entries`, borrowing the pool
already supplied to `NewEntries`; it never owns or closes that pool. The existing
constructor's explicit latest-pointer repair remains unchanged. Independent
memory conformance and an executable external-module fixture demonstrate
replacement persistence → catalog → public format serializers. The reader returns
existing `persistence.Entry`/`EntryPage` payloads and cursor metadata unchanged.
Use `formats.ServerPage`, `SkillPage` or `PluginPage` to serialize their payloads,
or mount the independently reusable [consumer HTTP router](registry-consumer-http.md)
with the reader or a replacement host application. Standalone composes the same
public constructors.

## Selection and bounds

For each `(kind,name)`, choose the first linked source incarnation that actually
has versions. **All versions belong to that winner.** Only then apply namespace,
name/search, updated-since, exact/latest, cursor and page bound. Namespace filters
skill/plugin payloads; it is not an ownership key and is invalid on servers.
An unmatched winner never promotes a lower source. If A has versions 2 and 3 and
B has 1, 2 and 9, exact 1 is absent, latest is A's 3, and search/namespace matches
found only in B do not produce this name. Kinds have independent ownership.

An empty selector lists all versions. `Exact: "latest"` means that literal stored
version, distinct from `Latest: true`. Latest uses the approved
`model.CompareVersions` order: accepted semver above non-semver, semantic
precedence, exact raw lexical ties, then lexical non-semver. Lists order by name
then exact raw version, ascending byte-lexically (PostgreSQL `COLLATE "C"`),
**not semantic version order**. Updated-since is exclusive on version update time.
Search is a case-insensitive **literal substring** of name/title/description;
`%`, `_` and backslash have no wildcard meaning. PostgreSQL uses `strpos`, so no
LIKE escaping is necessary. This intentionally corrects legacy wildcard search.

Every list/history request explicitly supplies a limit in **1..1000**; zero,
negative and excessive values are rejected. Existing source `ListEntries` keeps
its 100 cap. Missing views/Get misses are `errors.Is(err, persistence.ErrNotFound)`;
existing empty views and missing-name histories return empty pages. Invalid
kinds/selectors/queries/cursors are `ErrInvalid`; storage or unreadable payload
failures are `ErrUnavailable` without SQL details. Context cancellation/deadlines
remain classifiable.

## Cursor compatibility and consistency

Opaque, versioned backend tokens are at most 4096 bytes. They bind view name,
private view incarnation, ordered source incarnation IDs, **every query field
including limit**, fixed ordering and the last `(name,raw version)` position.
Changed filters, view/source recreation or link ordering invalidate continuation.
Large filters are hashed rather than embedded. An identity too large to encode
within the token bound fails with `ErrInvalid`; it is never truncated.
Legacy, source-list and other-backend tokens are not portable. Tokens carry no
principal/policy binding and are not credentials or an authorization mechanism.

PostgreSQL resolves the definition, links, binding and selected payload page in
one read-only repeatable-read transaction. The independent memory backend holds
its state lock and returns copied payloads. **There is no universal cross-backend
or cross-page snapshot guarantee.** Normal entry mutations do not invalidate a
token: each page recomputes live winners, so inserts/deletes/source emptying may
change or skip results before the continuation position. No frozen history is
promised.

The [consumer HTTP router](registry-consumer-http.md) preserves
these existing route-specific behaviors, rather than blindly exposing this
library's continuation: server lists default 30 and cap at 1000;
extension lists default 50 and reject above 100; server histories request 1000
with no wire continuation; extension histories request 30 and retain opaque
response cursor metadata but ignore input limit/cursor query parameters.
[Definition administration](registry-definition-administration.md) and [managed
publication/complete diagnostics](registry-publication-diagnostics.md) are also
implemented and mounted through [admin HTTP](registry-admin-http.md) in the guarded
standalone assembly.
