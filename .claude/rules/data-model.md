---
paths:
  - "internal/config/**/*.go"
  - "internal/service/**/*.go"
  - "internal/sources/**/*.go"
  - "database/**/*.sql"
  - "database/**/*.go"
---

# Data Model

Applies to config parsing, service-layer logic, source ingestion, and SQL migrations.
These rules protect the entry-pool data model from drift that would compromise claims
filtering performance, source-type dispatch, and the boundary between config- and
API-managed resources.

## 1. Claims Live on Entry Names, Not Versions

`registry_entry.claims` (the `(type, name, source)` row) is the single source of truth for
an entry's claims. `entry_version` has no claims column. All versions of a given name
within a source share one claim set.

**Detect**: migrations adding a `claims` column to `entry_version`; per-version claim
writes; filtering logic that loads claims from `entry_version.*`.

**Instead**: keep claim reads/writes on `registry_entry`. Claim changes apply to every
version at once (that is the intent of `PUT /v1/entries/{type}/{name}/claims`).

**Why**: per-user filtering on version rows would scale with total row count; name-level
claims let us filter on the (lower-cardinality) distinct-name set without JSONB indexes.

## 2. Source Type Is Inferred From the Config Block

`SourceConfig` has exactly one non-nil type block (`git`, `api`, `file`, `managed`,
`kubernetes`). There is no `type:` field — the block shape *is* the type.

**Detect**: code reading a `Type` / `type:` field on `SourceConfig`; re-implementing
source-type dispatch outside `SourceConfig.GetType()`; adding a source type without updating
`validateSourceTypeCount` and `validateSourceSpecificConfig`.

**Instead**: call `SourceConfig.GetType()` — it is the single arbiter at runtime. Adding a
new source type requires, at minimum: `SourceType` constants, a new config struct,
`SourceConfig` field, `GetType()`, `validateSourceTypeCount`,
`validateSourceSpecificConfig`, `sources.CreateHandler`, and `IsNonSyncedSource()` if
applicable.

## 3. At Most One Managed Source Per Instance

The managed source is the single container for API-published entries. Publishing is
disabled when it is absent; allowing multiple would fork the publish path.

**What must hold:**
- Config-load validation rejects more than one `managed: {}` block (the `managedCount > 1`
  check in `config.go`).
- Runtime `getManagedSource()` errors if the DB contains more than one — defense in depth.
- Publishing returns `ErrNoManagedSource` (500) when none is configured.

**Breaks if violated:** publish destination becomes non-deterministic; migrations or API
writes that create a second managed source silently succeed and diverge.

## 4. Claim Values Are Flat: Scalar or Flat Array of Scalars

Nested objects inside claim values are unsupported. The whole "AND across keys, OR within
arrays" matching contract assumes flat values.

**Detect**: `ValidateClaimValues` changes that allow nested maps; callers that pass
`map[string]any{"k": map[string]any{...}}` as claims; YAML examples with nested claim
structures.

**Instead**: anywhere a map value is consumed as a claim, treat it as `string | []string`
only. `ValidateClaimValues` must reject nested values at every write boundary (config
load, source/registry create-update, entry publish).

## 5. CONFIG and API Resources Are Mutually Exclusive

The `creation_type` column (`CONFIG` | `API`) is set at create time and never changes.
Two writers for the same row is an operational trap; YAML is the operator's source of
truth, the API is the dynamic surface.

**Detect**: code that flips `creation_type` on an existing row; config-load reconciliation
that upserts over rows with `creation_type = 'API'`; API handlers that don't return
`ErrConfigSource` / `ErrConfigRegistry` (403) on writes to `CONFIG` rows.

**Instead**: config-load reconciliation only touches `CONFIG` rows. API writes to a `CONFIG`
resource return 403. A resource is never "converted" between the two — it is deleted and
recreated.

## 6. Source Names Must Be Valid DNS Subdomains (RFC 1123)

Source names are used as suffixes for Kubernetes leader-election lease names. Invalid DNS
labels fail lease creation opaquely.

**What must hold:** `IsValidDNSSubdomain` gates every source name at write time (config +
API). Max 63 chars, lowercase alphanumeric + hyphens, no leading/trailing hyphen.

**Breaks if violated:** leader election silently fails; operators see "no leader" instead
of a validation error.

## 7. No JSONB Operators in Production SQL

Claims come out of the DB as raw bytes and are filtered in Go. No `@>`, `->>`, or GIN
indexes on claim columns.

**Detect**: new SQL queries using `@>`, `->>`, `jsonb_path_query`; migrations creating GIN
indexes on `claims` columns; service code that expects SQL to do containment filtering.

**Instead**: filtering happens in `RecordFilter` chains in `internal/service/db/` during
row streaming. The DB returns candidates; Go code decides visibility. Moving to SQL-level
filtering is a migration, not a drop-in change — authz tests assume app-level semantics.

**Why**: for current data volumes (hundreds to low thousands of distinct names), streaming
+ Go filtering is simpler than JSONB + GIN and keeps all auth logic in one place.
