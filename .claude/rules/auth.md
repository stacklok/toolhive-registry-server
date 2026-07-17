---
paths:
  - "internal/auth/**/*.go"
  - "internal/authz/**/*.go"
  - "internal/service/db/claims_filter.go"
  - "internal/service/db/*claims*.go"
  - "internal/api/v1/entries.go"
---

# Authentication, Authorization, and Claims

Applies to all code that handles JWTs, resolves roles, or compares claims. These rules keep
the authz story coherent: one matching algorithm, subset validation everywhere, default-deny
when claims don't cover.

## 1. Authentication and Authorization Are Separate Concerns

"Who are you?" (JWT validation) and "what can you do?" (role + claim matching) live in
separate config blocks and separate middleware stages. Conflating them produces a config
surface that is hard to secure.

**What must hold:**
- `auth.mode` controls authentication: `anonymous` or `oauth`. Default is `oauth`
  (secure-by-default).
- `auth.authz` is optional and separate; it controls authorization (roles and their claim
  maps).
- Without `auth.authz`, authenticated users implicitly get all roles (open mode). Role
  enforcement is opt-in.

**Detect**: a single `auth:` block combining identity and permissions; a default that
allows anonymous access; role checks inside JWT validation middleware.

## 2. Roles Come From the IdP — No Local Role Storage

Four fixed roles: `superAdmin`, `manageSources`, `manageRegistries`, `manageEntries`. Each
is a list of claim maps in config. No role table, no role CRUD API, no migration that adds
one.

**Detect**: a new role added at runtime via API; a database table storing role-to-user
mappings; middleware that reads "role" from anywhere other than the resolved context
populated by `ResolveRolesMiddleware`.

**Instead**: add roles in config under `auth.authz.roles`. Adding a new role name requires
a code change (new constant + handler wiring). `ResolveRolesMiddleware` runs after JWT
extraction and injects resolved roles into the request context.

## 3. Claim Matching Has Two Rules, Split by Direction: Visibility (OR) and Subset (AND)

Claim comparison splits by *which question is being asked*. Both share one key-level
matcher (`claimsMatch` in `claims_filter.go`: AND across keys, caller must hold each key,
empty-array vacuously satisfied, fail-closed on bad types) and, at the gate layer, the same
super-admin bypass and empty-resource default-deny (`validateClaimsWith`). They differ
**only** in the within-array direction:

- **Visibility / read** — "may this caller see or reach an *existing* resource?"
  **AND across keys, OR within array values, absent key = not checked.** A record tagged
  `team: [platform, data]` is an allow-list: a caller in *either* team matches. Used by the
  registry access gate, per-user entry filtering, single-resource reads, deletes, and
  referencing a source. Implemented by `claimsVisible` / `validateClaimsVisible` /
  `validateClaimsVisibleBytes`. Role resolution (`matchesClaimValue` in
  `internal/auth/roles.go`) uses the same AND-keys/OR-array shape, but is a *separate*
  matcher — it has no super-admin bypass or default-deny (roles are how super-admin is
  derived) and treats an empty required array as no-match, so don't assume gate parity.

- **Subset / write** — "may this caller stamp these *new* claims onto a resource?"
  **AND across keys, AND within array values (containment).** The caller must hold *every*
  value, or they could create a resource with broader visibility than their own identity —
  privilege escalation (§5). Used only on create/update/publish/update-claims, against the
  incoming request claims. Implemented by `claimsContain` / `validateClaimsSubset`.

**Detect**: a read/gate check calling `validateClaimsSubset` / `claimsContain` (should be
the `*Visible` variant); a create/update check on incoming claims calling
`validateClaimsVisible` (should be `validateClaimsSubset`); hand-rolled JSON comparison
loops over claim maps; a fifth matching algorithm.

**Instead**: pick the variant by direction — reads/gates use the visibility helpers, writes
of new claims use the subset helpers. The list filter and the single-resource gate MUST use
the same (visibility) rule, or a resource is visible via one endpoint and 403 via another
(§4). If you need a new matching semantic, change the shared matcher and audit every caller.

**Why**: "can I see it?" and "can I grant it?" are different questions. Collapsing them into
one AND-everywhere algorithm produced #843, where a caller in one of several allowed groups
was hidden from entries meant for them; the mirror mistake (OR on writes) is an escalation
hole.

## 4. Default-Deny When Authz Is Enabled

When authz is enabled, **unlabeled is not the same as public**. A resource with no claims
is treated as "no rule matches" → deny, not "open to all." This is the Saltzer–Schroeder
*fail-safe defaults* principle and matches how every Zero Trust framework (NIST SP
800-162, OWASP Access Control, AWS IAM "implicit deny") handles the absence of an
authorization rule.

**What must hold:**
- A row with claims the caller's JWT doesn't cover is invisible to that caller.
- A row with **empty/missing claims** is also invisible to claim-bearing callers — only
  super-admin (or anonymous mode / authz-off, where the gate is bypassed entirely) can
  reach it. "Public" must be expressed with an explicit positive claim that the role
  config maps to all authenticated users.
- Registry access gate returns **403** (not 404) when the caller fails it. 404 would leak
  registry existence.
- The list filter and the single-resource gate must agree on this rule. Inconsistency
  between list and get/put paths produces "visible via one endpoint, invisible via
  another" bugs.
- Publishing with empty claims is **forbidden** when authz is active (see §6) — this is
  the write-time enforcement that complements the read-time default-deny.

**Detect**: a gate that returns nil when the resource's claims are empty and the caller's
claims are non-empty; new "empty = open" carve-outs; 404 responses on registry access
denial; publish handlers that accept empty claims when `authzEnabled`.

**Implementation note**: the `validateClaims*` functions (`validateClaimsSubset` and the
`validateClaimsVisible` / `validateClaimsVisibleBytes` read variants) in
`internal/service/db/claims_filter.go` are package-level functions and intentionally
have no knowledge of `s.skipAuthz`. They short-circuit when `callerClaims == nil`, so
every callsite is responsible for passing `nil` when authz is off — the standard pattern
is `gateClaims := …; if s.skipAuthz { gateClaims = nil }`. Keeping the gate functions
pure preserves the layering: internal-state inspection lives at the callsite, not inside
the matching algorithm. Turning authz off disables the entire gate, including its
default-deny posture on empty claims.

**Upgrading from "empty = open" to default-deny**: deployments that ran with authz off,
or that ingest synced sources whose entries have no claims, will have rows in
`registry_entry`, `sources`, and `registries` with `claims IS NULL`. After turning authz
on (or upgrading to a release that includes default-deny), those rows are invisible to
every claim-bearing caller. Two recovery paths:

- **Per-entry**: a super-admin can read and re-tag each affected row via
  `PUT /v1/entries/{type}/{name}/claims` (or the equivalent source/registry endpoints).
- **Operator-managed sources**: tag the managed source itself with a tenant-wide claim
  (e.g. `{org: "acme"}`) in config so writers can reference it; otherwise no
  non-super-admin caller can publish to it (enforced by the publish source-claims gate —
  §5). This is the most common stumbling block when enabling authz on an existing
  deployment — forgetting to tag the managed source looks like a permissions bug at
  publish time.

There is intentionally no automatic "backfill claims from JWT" path — that would
re-introduce the "empty = the caller's identity" behavior that this rule rejects.

## 5. Subset Validation When a Write Introduces New Claims

When a write sets new claims on a resource, those claims must be a subset of the caller's
JWT claims (`validateClaimsSubset`, containment/AND — §3). Without this, a user can create a
source/registry/entry with broader visibility than their own identity allows — that's
privilege escalation.

This is distinct from *authorization to act on an existing resource* (read, delete, and the
"cover the current claims" half of an update), which is a **visibility** check
(`validateClaimsVisible*`, OR — §3), not a subset check. Consequence to accept consciously:
**anyone who can see a shared-claim resource can also delete it.** A caller matching one
value of a `team:[platform,data]` resource can read *and* delete it; that is intended so the
list, get, and delete paths agree (§4).

**Subset (AND) applies to the NEW claims introduced by:**

- Source create/update — `req.Claims`
- Registry create/update — `req.Claims`
- Publish, update entry claims — `options.Claims`

**Visibility (OR) applies to the existing-claims gate on:**

- Source/registry/entry read, delete
- The "cover current claims" check on update
- Covering each referenced source's claims when creating/updating a registry
- Publishing into the managed source — the caller must cover the *source's* claims
  (§4; #845), in addition to the entry-claims subset check above. An untagged managed
  source is therefore publishable only by super-admin (default-deny).

**Detect**: a create/update/publish handler that doesn't call `validateClaimsSubset` on its
incoming request claims; a read/delete/gate that calls `validateClaimsSubset` instead of a
`validateClaimsVisible*` variant (§3); a super-admin exemption applied to role gates
(super-admin is exempt from *subset* checks, not from *role* gates).

## 6. First Publish Owns the Name; Claims Immutable on Re-Publish

Entry names are allocated on a first-come, first-served basis. All versions of a name share
one claim set. Changing claims on an already-published name goes through
`PUT /v1/entries/{type}/{name}/claims`, not through re-publish.

**What must hold:**
- `POST /v1/entries` with empty/missing `claims` returns 400 when auth is active (caller
  has JWT claims in context).
- In anonymous mode, claims remain optional (the filter is bypassed).
- Re-publishing with claims that don't match the allocated set returns 409
  (`ErrClaimsMismatch`).

**Detect**: publish handlers that allow claim drift between versions; code paths that
accept re-publish with a different claim set and silently update the name's claims.

## 7. OAuth Issuer URLs Require HTTPS

JWKS fetched over HTTP is trivially MITM'd, which defeats JWT validation. Allowing HTTP is
an explicit dev-only escape hatch.

**What must hold:**
- OAuth `issuerUrl` must be HTTPS. Exceptions: `localhost` / `127.0.0.1` / `::1`, or the
  env-only flag `THV_REGISTRY_INSECURE_URL=true`.
- The insecure-allow flag is **env-only** — it must not be readable from YAML. This
  prevents committing it to a repo.

**Detect**: YAML schemas that accept `insecureAllowHTTP` as a field; token-validator code
that bypasses scheme checks; test fixtures that set `http://` issuer URLs without localhost.

**Instead**: propagate `insecureAllowHTTP` from `Config.insecureAllowHTTP` →
`AuthConfig.InsecureAllowHTTP` → the token validator, as currently wired.
