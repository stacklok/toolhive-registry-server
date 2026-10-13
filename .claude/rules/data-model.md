---
paths:
  - "internal/config/**/*.go"
  - "internal/db/**/*.go"
  - "database/**/*.sql"
  - "pkg/registry/persistence/**/*.go"
  - "pkg/registry/postgres/**/*.go"
  - "pkg/registry/catalog/**/*.go"
---

# Data Model

## 1. Entry Ownership and Historical Claims

A source owns `(kind,name,version)` payloads and related packages/remotes/icons.
Latest is selected per `(source,kind,name)` using the shared total version order.
Public entries and source/view definitions contain no resource claims. Schema 29 still
has historical claims columns and guarded mutation plumbing for offline transition;
retain them, archives, receipts and historical migrations until the final forward slice.
Do not infer exposure consent from empty/null claims or automatically strip claims.
Private config compatibility structures must keep version-1 exposure fingerprint JSON
byte semantics unchanged; retired-field presence/history classification is intentional.

## 2. Source Type From Exactly One Block

Standalone config uses `SourceConfig.GetType`; public definitions use `Kind()`.
Exactly one of git/api/file/managed/kubernetes is set. Validate at trust boundaries,
not by guessing a competing `type` field. Public source validation belongs to persistence;
YAML/Viper and secrets remain standalone concerns. Non-synced schedules/filters and
historical unsupported fields fail preflight rather than being silently discarded.

## 3. At Most One Managed Source

Managed publication has one destination. Enforce singleton both in contracts and the
PostgreSQL constraint; reject conflicting definitions atomically. Absence must report the
public typed no-managed-source error. No publication to arbitrary external sources.

## 4. Version and Payload Fidelity

Preserve exact version spellings and optional upstream/extension metadata. Masterminds-
parseable semver ranks above non-semver; compare precedence, then raw lexical spelling
for ties, and lexically within non-semver. Recompute latest on mutation and repair
historical pointers before reads. Source-scoped deletion cannot mutate another source.

## 5. CONFIG and API Ownership Is Immutable

Atomic config reconciliation touches only CONFIG-owned resources and links. API-owned
name collisions fail without partial writes; origin is never silently flipped. Source
incarnation/generation and leases fence reconfiguration/deletion/recreation.

## 6. Source Names and Views

Validate source DNS names at write boundaries. Views reference ordered source names and
are not authorization/privacy boundaries. Select the whole-name winning source before
search, version/latest selection or pagination; never merge different source histories.
Diagnostics deliberately show unshadowed source/view versions.

## 7. Selection Is Not Claims Filtering

There is no production resource-claim filter or JSONB containment authorization. Backend
queries implement nonpersonalized selection, ordering and bound cursors. Host services own
personalized selection. Offline history classification and claim-retirement SQL are private
compatibility machinery, not an authorization query engine.
