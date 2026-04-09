# Registry Server v2 — Final Design

v2 replaces the v1 model with a three-layer architecture: **sources** (data ingestion),
**entry pool** (shared database), and **registries** (consumer-facing views). Entry **names**
carry **claims** — key-value pairs inherited from their source or set explicitly at publish
time (all versions of a name share the same claims). Within a registry, each user sees only
the entries whose claims their JWT satisfies (**per-user filtering**). Claims use JWT claim
paths directly (e.g., `"https://myapp.com/org"`) — there is no `claimKeys` indirection layer.

---

## 1. Core Architecture

v2 decouples data ingestion from consumer-facing presentation. The v1 model — where each
registry is both a data source and an API endpoint — is replaced by a three-layer
architecture:

```
External data                          Consumers
─────────────                          ─────────
                ┌──────────┐
  Git repo ───→ │          │
  API ────────→ │ Sources  │──→ ┌────────────┐    ┌──────────────┐
  File ───────→ │ (ingest) │    │            │    │              │
  K8s ────────→ │          │──→ │  Entry     │──→ │  Registries  │──→ Clients
  Internal ──→  │          │    │  Pool (DB) │    │ (views)      │
                └──────────┘    │            │──→ │              │
                                └────────────┘    └──────────────┘
```

### Sources

A source is a named data ingestion pipeline. Sources pull entries from an external origin
into a shared entry pool. They are an admin/operator concern — never exposed to consumers.

| Type | Description |
|------|-------------|
| `git` | Syncs from a Git repository |
| `api` | Syncs from a remote API endpoint |
| `file` | Reads from local file or inline data |
| `kubernetes` | Discovers deployed MCP server instances via CRD watch |
| `internal` | Single global source for published entries (config type: `managed`, only one allowed) |

The v1 `managed` source type becomes the `internal` source for published entries. In config
files, it is declared with a `managed:` configuration block; the document refers to it as "internal" throughout.

> **Note:** The source type is inferred from which configuration block is present (`git:`, `api:`, `file:`, `managed:`, `kubernetes:`). There is no explicit `type:` field — since exactly one block must be set, the type is unambiguous.

**Filtering** stays at the source level, applied at ingestion time — same as v1. Entries that
don't match the filter are not stored. Filter config supports entry type, name include
patterns, and name exclude patterns:

```json
{
  "filters": {
    "types": ["server", "skill"],
    "include": ["com.example/*"],
    "exclude": ["com.example/internal-*"]
  }
}
```

Each source carries **claims** (key-value pairs, e.g.,
`{ "https://myapp.com/org": "acme", "https://myapp.com/team": "platform" }`).
Source claims are used for **role scoping**: only users with the `manageSources` role whose JWT
covers the source's claims can manage it.

### Entries

The database uses a **two-level model** that separates entry names from entry versions:

```
registry_entry:  (type, name, source)          ← claims live here
entry_version:   (type, name, version, source) ← server/skill payload lives here

type    = "server" | "skill"
name    = reverse-DNS name (e.g., "com.example/my-server")
version = semver string (e.g., "1.0.0")
source  = FK to source table
```

**Claims are tied to server/skill names, not individual versions.** All versions of the same
`(type, name)` within a source share the same claims. This dramatically reduces the amount
of data that must be filtered per query — claim matching operates on the (lower-cardinality)
name level, not on every version row.

How claims are populated depends on the source type:
- **Synced sources (git, api, file)**: entries inherit the source's claims at ingestion time.
  All entry names from a source share the same claims.
- **Published entries (internal source)**: claims are set explicitly on first publish for a
  given `(type, name)`. Subsequent versions must use the same claims (see Section 2.6).
- **K8s sources**: claims come from the `toolhive.stacklok.dev/authz-claims` CRD annotation (see Section 2.7). Source claims are not inherited — they are for `manageSources` role scoping only.

Multiple sources can ingest the same `(type, name, version)`. All copies are stored. Which
one surfaces to a consumer is determined by the registry's source priority configuration.

### Registries

A registry is a named, consumer-facing view over the entry pool. Registries select entries
from one or more sources and expose them via the consumer API. They are the UX boundary —
the endpoint consumers query.

- Registries reference an ordered list of sources (priority for dedup)
- Registries are the API endpoint consumers interact with
- Registries have a `claims` field that acts as an access gate (Section 2.2)

Out of current scope:
- Registries have filters for additional narrowing (type, include/exclude patterns)

### Deduplication

Resolution happens at the **entry level** (`type, name`), NOT at the version level.

When a registry includes multiple sources that both provide `server-x`:
- The source with higher priority (earlier in the list) **owns** `server-x` entirely
- All versions of `server-x` come from that winning source
- The lower-priority source's versions are invisible in that registry

This prevents mixed version histories from different sources. Example:
- Source A: `server-x` v1.0, v1.1, v2.0
- Source B: `server-x` v1.0, v1.5
- Registry sources: `["source-a", "source-b"]`
- Consumer sees: v1.0, v1.1, v2.0 (all from source A). v1.5 from source B is hidden.

### Sync lifecycle

Sources that sync from external origins follow incremental upsert + orphan deletion in a
serializable transaction. Failed syncs retain the previous data. This is carried forward
from v1 unchanged.

### Config vs API ownership

Sources and registries are either **config-managed** (defined in YAML, immutable via API) or
**API-managed** (created via PUT endpoints). The boundary is strict — a resource is never
both.

| Aspect | Config-managed | API-managed |
|--------|---------------|-------------|
| Created by | Server config file (YAML) | `PUT` API call |
| Mutable via API | No (read-only, returns 403) | Yes |
| Deleted via API | No | Yes |
| Use case | Operator defaults, shared infra | Dynamic, team-managed |

### API structure

Two API surfaces with different path namespaces:

- **Admin APIs** — under `/v1/`. Our own endpoints for managing sources, registries, and
  publishing.
- **Consumer APIs** — under `/registry/{reg}/v0.1/`. Follow the upstream MCP Registry
  specification. The `/registry/` prefix avoids path collisions (e.g., a registry named `v1`)
  and matches the current v1 implementation. The `v0.1` segment is the spec version.

The v1 aggregated endpoint (`/registry/v0.1/`) and the extension API (`/extension/v0/`) are
removed.

**Admin APIs:**

| Method | Path | Role | Description | Phase |
|--------|------|------|-------------|-------|
| `GET` | `/v1/sources` | manageSources | List sources | 1 |
| `GET/PUT/DELETE` | `/v1/sources/{name}` | manageSources | Source CRUD | 1 |
| `GET` | `/v1/sources/{name}/entries` | manageSources | List entries in a source (unshadowed, bypasses dedup) | 2 |
| `GET` | `/v1/registries` | authenticated | List registries | 1 |
| `GET` | `/v1/registries/{name}` | authenticated | Get registry | 1 |
| `PUT/DELETE` | `/v1/registries/{name}` | manageRegistries | Registry CRUD | 1 |
| `GET` | `/v1/registries/{name}/entries` | manageRegistries | List entries in a registry (unshadowed, names + versions only) | 2 |
| `POST` | `/v1/entries` | manageEntries | Publish an entry (global, servers and skills) | 1 |
| `DELETE` | `/v1/entries/{type}/{name}/versions/{ver}` | manageEntries | Delete a published entry | 1 |
| `PUT` | `/v1/entries/{type}/{name}/claims` | manageEntries | Update entry's claims | 2 |
| `GET` | `/v1/labels` | manageSources or manageRegistries | List distinct claim values across entries | Deferred |

**Consumer read APIs (upstream spec):**

| Method | Path | Role |
|--------|------|------|
| `GET` | `/registry/{reg}/v0.1/servers` | authenticated |
| `GET` | `/registry/{reg}/v0.1/servers/{name}/versions` | authenticated |
| `GET` | `/registry/{reg}/v0.1/servers/{name}/versions/{ver}` | authenticated |
| `GET` | `/registry/{reg}/v0.1/x/dev.toolhive/skills` | authenticated |
| `GET` | `/registry/{reg}/v0.1/x/dev.toolhive/skills/{ns}/{name}/versions` | authenticated |
| `GET` | `/registry/{reg}/v0.1/x/dev.toolhive/skills/{ns}/{name}/versions/{ver}` | authenticated |

**Labels API**: An operator-only labels endpoint (requires `manageSources` or
`manageRegistries`) lets operators discover available claim values (e.g., list all
`"https://myapp.com/org"` values or all `"https://myapp.com/team"` values) for auditing and
operational visibility. Aggregates claim values across entries, scoped by the user's claim
boundary. The exact endpoint shape is an open design item (see Section 6).

### Roles (Pure IdP)

Roles come from the identity provider — no local storage, no role management API.
Authorization config lives under `auth.authz`, separated from authentication config
(`auth.mode`, `auth.oauth`):

| Role | Scope | Description |
|------|-------|-------------|
| **superAdmin** | global | Full system access, bypasses all claim checks |
| **manageSources** | claim-scoped | CRUD on sources within the user's claim boundary |
| **manageRegistries** | claim-scoped | CRUD on registries within the user's claim boundary |
| **manageEntries** | claim-scoped | Publish and delete entries via the global publish endpoint |

The v2 design splits the previous single "admin" role into **manageSources** and
**manageRegistries** for finer-grained control. **manageEntries** replaces the previous
"writer" role.

Each role is a list of claim maps — the same syntax used in claim matching everywhere else:

- **Within a map**: multiple keys are **AND'd** (all must match)
- **Across maps**: the list is **OR'd** (any map matching grants the role)
- **Array values**: within a key, array values are **OR'd** (any value matches)

```yaml
auth:
  # Authentication — "who are you?"
  mode: oauth
  oauth:
    providers:
      - name: "keycloak"
        issuerUrl: "https://auth.acme.com/realms/main"
        audience: "mcp-registry"

  # Authorization — "what can you do?"
  authz:
    roles:
      # Full system access — bypasses all claim checks
      superAdmin:
        - "https://myapp.com/role": "super-admin"

      # Source management — OR across maps (user matches EITHER map)
      manageSources:
        # AND: both must be present
        - "https://myapp.com/org": "acme"
          "https://myapp.com/role": "admin"
        # OR within a value
        - "https://myapp.com/role": ["org-admin", "platform-admin"]

      # Registry management
      manageRegistries:
        - "https://myapp.com/org": "acme"
          "https://myapp.com/role": "admin"
        - "https://myapp.com/role": ["org-admin", "platform-admin"]

      # Entry publishing and deletion
      manageEntries:
        - "https://myapp.com/role": "writer"
```

### Deployment behavior

Three deployment configurations:

| Configuration | Behavior | Use case |
|---------------|----------|----------|
| No auth | Open — unrestricted | Local dev, evaluation |
| Auth, no claims on resources | Authenticated — JWT required, no claim scoping | Small team |
| Auth with claims | Full — JWT + claims + roles | Enterprise |

> **Warning: anonymous → auth transition.** In anonymous mode, claims on sources and
> registries are stored but not validated (there is no JWT to validate against). If an
> operator creates resources with claims that don't make logical sense while in anonymous
> mode, those resources may become inaccessible when auth is enabled. Switching between
> anonymous and authenticated modes has lasting effects on resource accessibility.

---

## 2. Authorization Model

### 2.1 Claims on entries

Claims are stored on the **entry name** level — on the `(type, name, source)` row, not on
individual versions. All versions of the same entry share the same claims. How claims are
populated depends on the source:

**Synced sources (git, api, file)**: all entry names from a source inherit the source's
claims at ingestion time. When a source re-syncs, entry claims are updated to match the
source's current claims.

```
Source "acme-catalog" (claims: { "https://myapp.com/org": "acme" })
  └── "com.acme/my-tool"  → claims: { "https://myapp.com/org": "acme" }
      ├── v1.0.0           (no claims — inherits from name)
      └── v1.1.0           (no claims — inherits from name)
  └── "com.acme/other"    → claims: { "https://myapp.com/org": "acme" }
      └── v2.0.0           (no claims — inherits from name)
```

**Published entries (internal source)**: the publisher specifies claims on the **first
publish** for a given `(type, name)`. The name is allocated on a first-come, first-served
basis. Subsequent versions must carry the same claims — the server validates this and rejects
mismatches. Claims are validated as a subset of the publisher's JWT (see Section 2.5).

```
POST /v1/entries/
{
  "server": { "name": "com.acme/my-tool", ... },
  "claims": { "https://myapp.com/org": "acme", "https://myapp.com/team": "platform" }
}
```

On first publish, the name `com.acme/my-tool` is allocated with these claims. Future versions
must use the same claims — changing claims requires a dedicated update operation (super-admin
or claim owner).

**K8s sources**: entry names get claims exclusively from the
`toolhive.stacklok.dev/authz-claims` JSON annotation on the CRD (see Section 2.7).
Source claims are not inherited — they serve only for `manageSources` role scoping.
Since K8s entries always have a single version, the name-vs-version distinction has no
practical impact.

### 2.2 Registry access gate

Each registry has a single `claims` field. A user's JWT must satisfy the registry's claims to
query it at all. If the JWT doesn't match, the server returns **403 Forbidden**.

```yaml
registries:
  - name: "acme-platform"
    claims:
      "https://myapp.com/org": "acme"
    sources: ["git-catalog", "k8s-prod", "internal"]
```

A user with JWT `{ "https://myapp.com/org": "acme" }` passes the access gate.
A user with JWT `{ "https://myapp.com/org": "contoso" }` gets 403.

The access gate is a coarse filter — it determines who can query the registry. It does **not**
determine which entries the user sees within the registry (that's per-user filtering,
Section 2.3).

A registry with an empty or absent `claims` field is open to all authenticated users (or all
users in no-auth mode).

### 2.3 Per-user entry filtering

This is the key behavior that distinguishes this design. Within a registry, each entry
**name's** claims are matched against the requesting user's JWT. Different users see different
entries from the same registry. Because claims live at the name level, the filtering operates
on the lower-cardinality set of distinct names — not on every version row — which is
significantly more efficient (see Section 4.1).

```
Registry "acme-all" (claims: { "https://myapp.com/org": "acme" })
  Sources: ["git-catalog", "k8s-prod", "internal"]

  entry-1 claims: { "https://myapp.com/org": "acme", "https://myapp.com/team": "platform" }
  entry-2 claims: { "https://myapp.com/org": "acme", "https://myapp.com/team": "data" }
  entry-3 claims: { "https://myapp.com/org": "acme" }

User A (JWT: { ..., "https://myapp.com/org": "acme", "https://myapp.com/team": "platform" })
  → passes access gate ✓
  → sees entry-1 (team matches) ✓
  → sees entry-2? NO — user has team "platform", entry requires team "data"
  → sees entry-3 (no team claim on entry — open) ✓
  → Result: [entry-1, entry-3]

User B (JWT: { ..., "https://myapp.com/org": "acme", "https://myapp.com/team": "data" })
  → passes access gate ✓
  → sees entry-1? NO — user has team "data", entry requires team "platform"
  → sees entry-2 (team matches) ✓
  → sees entry-3 (no team claim on entry — open) ✓
  → Result: [entry-2, entry-3]
```

The filtering direction is: **user JWT must cover entry claims**. For each claim key on the
entry, the user's JWT must have a matching value. If the entry has a claim key that the user's
JWT lacks entirely, the entry is **not visible** to that user.

The same rule applies at both levels — the user's JWT must cover the resource's claims. The
mental model:

1. **Registry access gate**: "Does the user belong here?" — user JWT ⊇ registry claims
2. **Entry filtering**: "Can the user see this entry?" — user JWT ⊇ entry claims

### 2.4 Claim matching rules

The same matching rules apply everywhere claims are compared (access gate, entry filtering,
role resolution, write-path scoping):

**AND across keys**: all claim keys must match.

```yaml
# User must have BOTH org=acme AND team=platform
claims:
  "https://myapp.com/org": "acme"
  "https://myapp.com/team": "platform"
```

**OR within arrays**: any value in an array matches.

```yaml
# User must have team=eng OR team=data
claims:
  "https://myapp.com/team": ["eng", "data"]
```

**Absent key = open**: if a resource (entry, registry) does not have a particular claim key,
that key is not checked. An entry with claims `{ "https://myapp.com/org": "acme" }` is
visible to any user whose JWT has `"https://myapp.com/org": "acme"`, regardless of what other
claims the user has or doesn't have.

**Super-admin bypass**: users with the super-admin role bypass all claim checks. They see all
entries in all registries.

### 2.5 Claim subset validation

On write operations (creating sources, registries, publishing entries), the resource's claims
must be a **subset** of the creator's JWT claims. This prevents privilege escalation — a user
cannot create resources with broader visibility than their own identity allows.

Examples:
- User with JWT `{ "https://myapp.com/org": "acme", "https://myapp.com/team": "platform" }`
  can create a source with claims `{ "https://myapp.com/org": "acme", "https://myapp.com/team": "platform" }` ✓
- Same user cannot create a source with claims `{ "https://myapp.com/org": "acme", "https://myapp.com/team": "finance" }` ✗
  (team "finance" not in their JWT)
- Same user can create a source with claims `{ "https://myapp.com/org": "acme" }` ✓
  (subset — fewer keys is fine)

Super-admins are exempt from subset validation.

### 2.6 Publishing

Publishing uses a single **global endpoint** under the admin API namespace. The endpoint
accepts both servers and skills via a single path — the payload distinguishes the type. This
endpoint is **not upstream-compliant** (it lives under `/v1/`, not under a registry path).

```
POST /v1/entries/
{
  "claims": { "https://myapp.com/org": "acme", "https://myapp.com/team": "platform" },
  "server": { "name": "com.acme/my-tool", "version": "1.0.0", ... }
}
```

The `claims` field is **separate** from the server/skill JSON payload. The server JSON
maintains the exact same structure as the upstream MCP Registry spec — claims are metadata
about the entry, not part of the entry itself. This also means the same server JSON
description can be published with different claims for different use cases.

The payload contains either a `server` field or a `skill` field — exactly one, mutually
exclusive.

**Name allocation and claim consistency:**

Server/skill names are allocated on a **first-come, first-served basis**. The first publish
for a given `(type, name)` sets the claims for that name. All subsequent versions must carry
the same claims:

1. First publish of `com.acme/my-tool` with claims `{ org: "acme", team: "platform" }` →
   name allocated, claims recorded
2. Second publish of `com.acme/my-tool` v2.0.0 with claims `{ org: "acme", team: "platform" }`
   → ✓ matches, version created
3. Attempt to publish `com.acme/my-tool` v3.0.0 with claims `{ org: "acme", team: "data" }`
   → ✗ rejected, claims don't match the allocated name

To change claims on an existing name, a dedicated update operation is required (super-admin
or original claim owner). This is an open design item (see Section 6).

> **Limitation**: the system cannot prevent two different teams from publishing different
> versions of the same server name. Name ownership is enforced by claims — whoever allocates
> the name first owns it. This mirrors how the upstream MCP registry works with reverse-DNS
> names (ownership is asserted on the name, not on versions).

**Requirements:**
- The publisher must have the **manageEntries** role (config-mapped, see Section 1)
- The `claims` field is **required** — the publisher must choose the entry's visibility scope
- Claims are validated as a subset of the publisher's JWT (Section 2.5)
- The entry is stored in the **internal source** (a single system-managed source)

**Internal source configuration**: publishing is disabled unless the `internal` source is
configured. Only one `internal` (managed) type source is allowed per registry server instance.
It is defined in the config file alongside other sources:

```yaml
sources:
  - name: "internal"
    managed: {}
    # No additional config needed — the internal source is a container for published entries
```

**Cross-registry visibility**: publish once, visible in any registry that includes `"internal"`
in its sources list — but only if the user querying that registry has JWT claims that cover
the entry's claims (per-user filtering). The entry's claims determine who can see it across
all registries.

```yaml
# Registry config — must explicitly include "internal" to see published entries
registries:
  - name: "acme-all"
    claims:
      "https://myapp.com/org": "acme"
    sources: ["git-catalog", "internal"]   # ← includes published entries
```

**Dedup with synced entries**: when a published entry has the same `(type, name)` as a synced
entry, source priority applies. The `internal` source position in the sources list controls
which wins:

```yaml
# Synced entries take priority — published entry hidden on collision
sources: ["git-catalog", "internal"]

# Published entries take priority — synced entry hidden on collision
sources: ["internal", "git-catalog"]
```

**Delete endpoint**: `DELETE /v1/entries/{type}/{name}/versions/{ver}` — the deleting user
must have the `manageEntries` role and their JWT must cover the entry's claims (subset
validation).

### 2.7 K8s per-entry claims

Kubernetes sources watch CRDs (MCPServer, VirtualMCPServer, MCPRemoteProxy) in configured
namespaces. Resources opt in via `toolhive.stacklok.dev/registry-export=true`.

A single K8s source per namespace can serve entries with **different claims** via the
`toolhive.stacklok.dev/authz-claims` annotation. The annotation value is a JSON object
using the same `map[string]any` format as the publish endpoint — full parity, no mapping
indirection. Source claims are **not** merged into entry claims — they serve only for
`manageSources` role scoping, consistent with how managed sources work.

```yaml
sources:
  - name: "k8s-prod"
    claims:
      "https://myapp.com/org": "acme"   # for manageSources scoping only, NOT inherited by entries
    kubernetes:
      namespaces: ["mcp-prod"]
```

A developer deploys a CRD with per-entry claims:

```yaml
apiVersion: toolhive.stacklok.dev/v1alpha1
kind: MCPServer
metadata:
  name: deploy-helper
  namespace: mcp-prod
  annotations:
    toolhive.stacklok.dev/registry-export: "true"
    toolhive.stacklok.dev/registry-description: "Deploy helper"
    toolhive.stacklok.dev/registry-url: "https://deploy-helper.mcp-prod.svc"
    toolhive.stacklok.dev/authz-claims: '{"https://myapp.com/org": "acme", "https://myapp.com/team": "platform"}'
```

The resulting entry gets exactly the claims from the annotation:

```json
{
  "https://myapp.com/org": "acme",
  "https://myapp.com/team": "platform"
}
```

**Annotation value format**: same JSON `map[string]any` as source claims and publish claims.
Supports scalar strings and arrays (e.g., `{"team": ["eng", "data"]}`).

**No merge with source claims**: entry claims come exclusively from the annotation. Source
claims on K8s sources serve only for `manageSources` role scoping (who can manage the source
via the admin API), not for entry-level visibility. This is consistent with managed sources,
where entry claims come from the publish payload.

**Missing annotation**: entry has no claims. In anonymous mode, the entry is visible to
everyone. When authz is configured, the entry is invisible (default-deny). This follows
standard ABAC/RBAC patterns where unlabeled resources are inaccessible.

**Invalid JSON**: the CRD is skipped with a warning log. Malformed claims are a developer
error — the entry is not synced until the annotation is fixed.

The developer controls their entry's claims by setting annotations on the CRD.

**1 source → entries with different claims → per-user filtering determines visibility.**

> **Note**: The original design used `claimMapping` (a config-level mapping from K8s label
> keys to JWT claim paths). This was replaced by the direct annotation approach because:
> (1) it uses the same JSON format as claims everywhere else — no new concept to learn,
> (2) it removes the indirection layer — no operator config needed for the mapping,
> (3) annotations are consistent with how the registry already reads CRD metadata.

---

## 3. Practical Examples

### 3.1 Full config example

```yaml
auth:
  mode: oauth
  oauth:
    resourceUrl: "https://registry.acme.com"
    providers:
      - name: "keycloak"
        issuerUrl: "https://auth.acme.com/realms/main"
        audience: "mcp-registry"

  authz:
    roles:
      superAdmin:
        - "https://myapp.com/role": "super-admin"

      manageSources:
        - "https://myapp.com/org": "acme"
          "https://myapp.com/role": "admin"

      manageRegistries:
        - "https://myapp.com/org": "acme"
          "https://myapp.com/role": "admin"

      manageEntries:
        - "https://myapp.com/role": "writer"

sources:
  - name: "git-shared-catalog"
    claims:
      "https://myapp.com/org": "acme"
    git:
      repository: https://github.com/acme/mcp-catalog.git
      branch: main
      path: registry.json
    syncPolicy:
      interval: "30m"

  - name: "k8s-prod"
    claims:
      "https://myapp.com/org": "acme"
    kubernetes:
      namespaces: ["mcp-prod"]
      # Per-entry claims set via toolhive.stacklok.dev/authz-claims annotation on CRDs

  # Internal source for published entries — only one allowed per instance.
  # Publishing is disabled unless this source is configured.
  - name: "internal"
    managed: {}

registries:
  - name: "acme-all"
    claims:
      "https://myapp.com/org": "acme"
    sources: ["git-shared-catalog", "k8s-prod", "internal"]

  - name: "acme-platform"
    claims:
      "https://myapp.com/org": "acme"
      "https://myapp.com/team": "platform"
    sources: ["git-shared-catalog", "k8s-prod", "internal"]

  - name: "acme-data"
    claims:
      "https://myapp.com/org": "acme"
      "https://myapp.com/team": "data"
    sources: ["git-shared-catalog", "k8s-prod", "internal"]

database:
  host: localhost
  port: 5432
  user: thv_user
  database: toolhive_registry
```

### 3.2 Simple deployment (SMB)

Small and medium-sized companies don't need custom JWT claims or a sophisticated IdP. Standard
claims that every OAuth provider already issues — `email`, `sub`, `groups`, `hd` (Google's
hosted domain) — work directly in role mappings and resource claims.

```yaml
auth:
  mode: oauth
  oauth:
    providers:
      - name: "google"
        issuerUrl: "https://accounts.google.com"
        audience: "my-registry"

  authz:
    roles:
      superAdmin:
        - "email": "alice@startup.com"

      manageSources:
        - "email": ["alice@startup.com", "bob@startup.com"]

      manageRegistries:
        - "email": ["alice@startup.com", "bob@startup.com"]

      # Any employee can publish
      manageEntries:
        - "hd": "startup.com"

sources:
  - name: "git-catalog"
    # No claims — open to any manageSources user
    git:
      repository: https://github.com/startup/mcp-tools.git
      branch: main
      path: registry.json

  - name: "internal"
    managed: {}

registries:
  - name: "default"
    # No claims — open to all authenticated users
    sources: ["git-catalog", "internal"]
```

This gives the team: authenticated access (JWT required), two people who can manage
sources and registries, and any employee can publish tools. No custom claims, no per-team
scoping — just a single open registry. Published entries also carry no team-level claims,
so everyone sees everything.

If later the company grows and needs per-team visibility, they add claims to sources and
entries without changing the architecture.

### 3.3 Git source — inherited claims

The `git-shared-catalog` source has claims `{ "https://myapp.com/org": "acme" }`. When it
syncs, every entry inherits these claims:

```
Sync: git-shared-catalog
  └── com.acme/code-analyzer v1.0.0 → claims: { "https://myapp.com/org": "acme" }
  └── com.acme/doc-generator v2.1.0 → claims: { "https://myapp.com/org": "acme" }
  └── com.acme/test-runner v1.5.0   → claims: { "https://myapp.com/org": "acme" }
```

These entries have only the `org` claim — no `team` restriction. Since absent key = open
(Section 2.4), they are visible to **any user** whose JWT includes
`"https://myapp.com/org": "acme"`, regardless of team. The shared catalog entries therefore
appear in all team-scoped registries (`acme-platform`, `acme-data`, etc.).

### 3.4 K8s source — per-entry claims via annotation

The `k8s-prod` source watches `mcp-prod` namespace. A platform engineer deploys:

```yaml
apiVersion: toolhive.stacklok.dev/v1alpha1
kind: MCPServer
metadata:
  name: deploy-helper
  namespace: mcp-prod
  annotations:
    toolhive.stacklok.dev/registry-export: "true"
    toolhive.stacklok.dev/registry-description: "Deploy helper for platform team"
    toolhive.stacklok.dev/registry-url: "https://deploy-helper.mcp-prod.svc"
    toolhive.stacklok.dev/authz-claims: '{"https://myapp.com/org": "acme", "https://myapp.com/team": "platform"}'
spec:
  image: ghcr.io/acme/deploy-helper:latest
```

A data engineer deploys:

```yaml
apiVersion: toolhive.stacklok.dev/v1alpha1
kind: MCPServer
metadata:
  name: data-pipeline
  namespace: mcp-prod
  annotations:
    toolhive.stacklok.dev/registry-export: "true"
    toolhive.stacklok.dev/registry-description: "Data pipeline tools"
    toolhive.stacklok.dev/registry-url: "https://data-pipeline.mcp-prod.svc"
    toolhive.stacklok.dev/authz-claims: '{"https://myapp.com/org": "acme", "https://myapp.com/team": "data"}'
spec:
  image: ghcr.io/acme/data-pipeline:latest
```

Both entries come from the same source `k8s-prod`, but with different claims (each CRD
carries its own claims via the `authz-claims` annotation — source claims are not inherited):

```
k8s-prod source:
  └── deploy-helper  → claims: { "https://myapp.com/org": "acme", "https://myapp.com/team": "platform" }
  └── data-pipeline  → claims: { "https://myapp.com/org": "acme", "https://myapp.com/team": "data" }
```

In registry `acme-all` (claims: `{ org: "acme" }`):
- User with JWT `{ org: "acme", team: "platform" }` sees: deploy-helper ✓, data-pipeline ✗
- User with JWT `{ org: "acme", team: "data" }` sees: deploy-helper ✗, data-pipeline ✓
- Super-admin sees both ✓

### 3.5 Publishing

A platform engineer with JWT containing:
```json
{
  "sub": "alice@acme.com",
  "https://myapp.com/org": "acme",
  "https://myapp.com/team": "platform",
  "https://myapp.com/role": "writer"
}
```

Publishes a new tool (first version — allocates the name):

```
POST /v1/entries/
Authorization: Bearer <alice-jwt>
{
  "claims": {
    "https://myapp.com/org": "acme",
    "https://myapp.com/team": "platform"
  },
  "server": {
    "name": "com.acme/custom-linter",
    "version": "1.0.0",
    "description": "Custom linting tool for platform team"
  }
}
```

Note: `claims` is a top-level field, separate from the `server` JSON payload. The `server`
object maintains upstream MCP Registry spec compatibility.

Validation:
1. Alice has `manageEntries` role (her JWT has `role: "writer"`, which matches the config) ✓
2. Name `com.acme/custom-linter` not yet allocated → allocate with these claims
3. Claims `{ org: "acme", team: "platform" }` ⊆ Alice's JWT ✓
4. Entry name stored in internal source with claims `{ org: "acme", team: "platform" }`

Publishing a second version:

```
POST /v1/entries/
Authorization: Bearer <alice-jwt>
{
  "claims": {
    "https://myapp.com/org": "acme",
    "https://myapp.com/team": "platform"
  },
  "server": {
    "name": "com.acme/custom-linter",
    "version": "2.0.0",
    "description": "Custom linting tool v2"
  }
}
```

Validation:
1. `manageEntries` role ✓
2. Name `com.acme/custom-linter` already allocated with claims `{ org: "acme", team: "platform" }`
3. Provided claims match allocated claims ✓ → version created

If Bob (from the data team) tries to publish v3.0.0 with claims
`{ org: "acme", team: "data" }`, the server rejects — the name is already allocated with
different claims.

Where the entry appears:
- Registry `acme-all` (sources include `internal`) — visible to users with `team: "platform"` ✓
- Registry `acme-platform` (sources include `internal`) — visible to platform team ✓
- Registry `acme-data` — visible to data team? NO — entry has `team: "platform"` ✗

Alice **cannot** publish with claims `{ "https://myapp.com/org": "acme", "https://myapp.com/team": "finance" }`
because `team: "finance"` is not in her JWT. The server returns 403.

### 3.6 Per-user visibility

This is the key example. Same registry, three different users, three different views.

**Registry**: `acme-all`
- claims: `{ "https://myapp.com/org": "acme" }`
- sources: `["git-shared-catalog", "k8s-prod", "internal"]`

**Entries in the pool** (from all sources, after dedup):

| Entry | Source | Claims |
|-------|--------|--------|
| com.acme/code-analyzer | git-shared-catalog | `{ org: "acme" }` |
| com.acme/doc-generator | git-shared-catalog | `{ org: "acme" }` |
| deploy-helper | k8s-prod | `{ org: "acme", team: "platform" }` |
| data-pipeline | k8s-prod | `{ org: "acme", team: "data" }` |
| com.acme/custom-linter | internal | `{ org: "acme", team: "platform" }` |

*Note: `org` and `team` are shorthand for their full JWT paths in this table.*

**User 1** — Platform engineer:
JWT: `{ org: "acme", team: "platform" }`
- Access gate: org=acme ✓
- code-analyzer (org: acme) → ✓ (no team claim on entry — open)
- doc-generator (org: acme) → ✓ (no team claim on entry — open)
- deploy-helper (org: acme, team: platform) → ✓
- data-pipeline (org: acme, team: data) → ✗ (user has team=platform, not data)
- custom-linter (org: acme, team: platform) → ✓
- **Sees 4 entries**

**User 2** — Data engineer:
JWT: `{ org: "acme", team: "data" }`
- Access gate: org=acme ✓
- code-analyzer (org: acme) → ✓
- doc-generator (org: acme) → ✓
- deploy-helper (org: acme, team: platform) → ✗ (user has team=data, not platform)
- data-pipeline (org: acme, team: data) → ✓
- custom-linter (org: acme, team: platform) → ✗
- **Sees 3 entries**

**User 3** — Org admin (super-admin):
JWT: `{ org: "acme", role: "super-admin" }`
- Access gate: bypassed (super-admin)
- All entries: bypassed (super-admin)
- **Sees all 5 entries**

### 3.7 New team onboarding

A new team (`data-science`) deploys their first MCP server to the shared `mcp-prod`
namespace:

```yaml
apiVersion: toolhive.stacklok.dev/v1alpha1
kind: MCPServer
metadata:
  name: ml-pipeline
  namespace: mcp-prod
  annotations:
    toolhive.stacklok.dev/registry-export: "true"
    toolhive.stacklok.dev/registry-description: "ML pipeline tools"
    toolhive.stacklok.dev/registry-url: "https://ml-pipeline.mcp-prod.svc"
    toolhive.stacklok.dev/authz-claims: '{"https://myapp.com/org": "acme", "https://myapp.com/team": "data-science"}'
spec:
  image: ghcr.io/acme/ml-pipeline:latest
```

**What happens** — zero admin action needed:

1. The existing `k8s-prod` source picks up the CR automatically
2. The `authz-claims` annotation is parsed and validated
3. Entry gets claims `{ "https://myapp.com/org": "acme", "https://myapp.com/team": "data-science" }`
4. In registry `acme-all`, users with JWT `{ org: "acme", team: "data-science" }` see the
   entry immediately
5. The shared catalog entries (no team claim) are also visible to data-science users

If a dedicated registry is desired, a user with `manageRegistries` can create one:

```yaml
registries:
  - name: "acme-data-science"
    claims:
      "https://myapp.com/org": "acme"
      "https://myapp.com/team": "data-science"
    sources: ["git-shared-catalog", "k8s-prod", "internal"]
```

But the entry is already visible and correctly claim-tagged before the registry exists. The
developer self-served by deploying a labeled CRD.

### 3.8 Multiple registries

Even with per-user filtering, multiple registries remain useful for:

**Source scoping**: different registries reference different sources.

```yaml
registries:
  # Public catalog — only curated git sources
  - name: "acme-public"
    claims:
      "https://myapp.com/org": "acme"
    sources: ["git-shared-catalog"]

  # Internal — includes K8s deployments and published entries
  - name: "acme-internal"
    claims:
      "https://myapp.com/org": "acme"
    sources: ["git-shared-catalog", "k8s-prod", "internal"]
```

**Private team registry**: a team can have a registry that combines curated sources,
K8s-deployed tools, and published entries — but is completely invisible to anyone outside
the team via the access gate.

```yaml
sources:
  - name: "git-shared-catalog"
    claims:
      "https://myapp.com/org": "acme"
    git:
      repository: https://github.com/acme/mcp-catalog.git
      branch: main
      path: registry.json

  - name: "git-security-tools"
    claims:
      "https://myapp.com/org": "acme"
      "https://myapp.com/team": "security"
    git:
      repository: https://github.com/acme/security-mcp-tools.git
      branch: main
      path: registry.json

  - name: "k8s-security"
    claims:
      "https://myapp.com/org": "acme"
      "https://myapp.com/team": "security"
    kubernetes:
      namespaces: ["mcp-security"]
      # Source claims are for manageSources role scoping only.
      # CRDs set their own claims via the authz-claims annotation.

  - name: "internal"
    managed: {}

registries:
  # Broad org access — everyone in acme
  - name: "acme-all"
    claims:
      "https://myapp.com/org": "acme"
    sources: ["git-shared-catalog", "k8s-prod", "internal"]

  # Security team ONLY — other teams get 403 on this registry
  - name: "acme-security"
    claims:
      "https://myapp.com/org": "acme"
      "https://myapp.com/team": "security"
    sources: ["git-shared-catalog", "git-security-tools", "k8s-security", "internal"]
```

A user with JWT `{ org: "acme", team: "platform" }` querying `acme-security` gets **403** —
they fail the access gate (`team: "security"` required). The security team's curated tools
(`git-security-tools`), K8s-deployed tools (`k8s-security`), and published entries are all
invisible outside the team. Within the registry, per-user filtering further scopes what each
security team member sees based on entry-level claims.

Meanwhile, the security team can also query `acme-all` — they pass the access gate
(`org: "acme"`) and see entries from the shared catalog, plus any `k8s-prod` or published
entries whose claims they satisfy.

**Dedup priority**: different registries can resolve collisions differently.

```yaml
registries:
  # Published entries override synced (for testing)
  - name: "acme-staging"
    claims:
      "https://myapp.com/org": "acme"
    sources: ["internal", "git-shared-catalog"]

  # Synced entries win (production — published entries supplement)
  - name: "acme-prod"
    claims:
      "https://myapp.com/org": "acme"
    sources: ["git-shared-catalog", "internal"]
```

---

## 4. Trade-offs and Considerations

### 4.1 Caching and performance

Per-user filtering means query results vary by user. Unlike a registry-level filter where all
readers see the same entries (cacheable per registry), per-user filtering requires evaluating
each user's JWT against entry claims.

**Name-level claims reduce query cost significantly.** Because claims live on entry names
(not versions), the JSONB containment check operates on the distinct set of `(type, name)`
rows — which is typically 1-2 orders of magnitude smaller than the total version count.
For the upstream MCP registry (~8,000 versions), the distinct server count is far lower.
Filtering on names first, then joining versions, keeps the expensive JSONB work minimal.

**Approach**: start without caching. Per-user queries go directly to the database. For
expected data volumes (hundreds to low thousands of distinct entry names), PostgreSQL JSONB
containment with GIN indexes is fast enough — tested to work in milliseconds up to 100,000
entries. Defer caching strategies to Phase 3.

If caching becomes necessary:
- Cache per `(registry, sorted_user_claims_hash)` — many users share the same effective claims
- Invalidate on entry upsert/delete, registry config change, or source re-sync
- Consider materialized views for hot registries

### 4.2 Non-deterministic responses

The same URL (`/registry/acme-all/v0.1/servers`) returns different results for different
users. This has implications:

- **CDN caching**: cannot cache consumer API responses at the CDN level without
  `Vary: Authorization` (which effectively disables caching)
- **Debugging**: "I see X entries" vs "I see Y entries" requires knowing the user's JWT to
  reproduce
- **Documentation**: API docs should note that results are personalized
- **Client behavior**: clients should not assume responses are stable across users or that
  counts are global

### 4.3 Pagination

With per-user filtering, pagination totals and cursors are user-specific:

- Total count reflects only entries visible to the requesting user
- Cursor values are valid only for the same user (different users have different result sets)
- Page size is applied after filtering, so a page of 20 always returns up to 20 visible
  entries (not 20 entries minus filtered-out ones)

### 4.4 Discoverability

The registry list endpoint (`GET /v1/registries/`) should ideally only show registries where
the user can see at least one entry. This requires a subquery: for each registry the user can
access (passes the access gate), check if any entry's claims match the user's JWT.

**Options:**
- **Simple**: list all registries the user can access (passes access gate), even if they'd see
  zero entries. Simpler to implement, but users may see empty registries.
- **Filtered**: run the entry visibility subquery. More useful but adds query cost.

Defer to implementation — start with the simple approach.

### 4.5 Debugging visibility

When a user can't see an entry they expect, the check has three layers:

1. **Access gate**: does the user's JWT satisfy the registry's claims?
2. **Source membership**: is the entry's source included in the registry's sources list?
3. **Claim match**: does the user's JWT cover the entry's claims?

Admin diagnostic tools (Phase 3) should expose this three-layer check for troubleshooting.
For example, an admin endpoint that takes a user identifier and returns which entries are
visible and why, showing which layer filtered each entry.

### 4.6 Admin visibility of shadowed entries

Deduplication hides entries from lower-priority sources in consumer-facing registry endpoints.
From an admin perspective, this means some entries are invisible through the normal API — for
example, if four sources all provide the GitHub MCP server, a registry shows only one copy.

Admin endpoints provide unshadowed access:
- `GET /v1/sources/{name}/entries` — lists all entries in a source, regardless of dedup
- `GET /v1/registries/{name}/entries` — lists all entries visible in a registry before dedup
  (names and versions only, no full details)

These are essential for troubleshooting ("which source owns this entry?") and for
understanding the full data in the system beyond what consumers see.

### 4.7 Claim structure constraints

Claims must use a **flat structure**: each claim key maps to either a scalar value or a flat
array of scalar values. Nested objects within claim values are not supported.

```yaml
# Supported
"https://myapp.com/org": "acme"                    # scalar
"https://myapp.com/team": ["eng", "data"]          # flat array

# NOT supported
"https://myapp.com/org": { "name": "acme", ... }   # nested object
```

This constraint keeps the JSONB matching queries expressible as simple Boolean statements
(`value = X` or `value IN (X, Y)`) rather than recursive structure traversals. It also
aligns with the requirements from design partners — only flat name-value pairs are expected
in JWT claims for authorization purposes.

---

## 5. Codebase State

> **Grounded in current code as of writing:**
>
> - **Strict 1:1 model**: `registry_entry.reg_id` → `registry.id` (direct FK, unique on
    >   `(reg_id, name, version)`). No source table, no junction table. v2's N:M requires a
    >   fundamental schema restructure.
> - **JSONB storage but no JSONB querying**: ~11 JSONB columns across tables (`upstream_meta`,
    >   `server_meta`, `source_config`, `filter_config`, `env_vars`, `transport_headers`,
    >   `repository`, `icons`, `metadata`, `extension_meta`), but **none are queried** with
    >   operators like `@>` or `->>` at runtime. All are stored and returned as blobs for
    >   client-side unmarshaling. JSONB containment predicates in SQL would be new for this
    >   codebase.
> - **Publishing is per-registry**: `POST /{registryName}/v0.1/publish` — needs replacement
    >   with the global `POST /v1/entries/` endpoint.
> - **K8s config is empty**: `KubernetesConfig struct{}` — no label selectors, no namespace
    >   config, no claim mapping. K8s config is built from scratch.
> - **Auth TODO exists**: `middleware.go` line 146 — claims validated but not stored in
    >   request context. Both JWT extraction and context propagation need implementation.
> - **Sync is per-registry**: coordinator polls for registries due for sync. v2's per-source
    >   sync model is a rewrite regardless.
> - **`managed` source type exists**: marker type meaning "writable via API" — but it's a
    >   registry attribute (`ManagedConfig`), not a separate source entity. Replaced by the
    >   `internal` source concept.
> - **No name/version split**: entries are stored as flat rows with `(reg_id, name, version)`
    >   as the unique key. v2 requires splitting into `registry_entry` (with claims) and
    >   `entry_version`. This is a fundamental schema restructure.

---

## 6. Open Items

1. **Anchor labels / minimum required claims on publish.** Should the publish endpoint enforce
   that certain claim keys are always present? Without this, a publisher could set
   `claims: { "https://myapp.com/org": "acme" }` with no team claim, making the entry
   visible to the entire org. An anchor label policy (e.g., "every published entry must have
   a `team` claim") would prevent overly-broad publishing. This prevents users from
   *omitting* required claim keys and making entries visible too broadly — the anchor claims
   are claims that must always be present and cannot be omitted. Define as a config option
   or defer.

2. **Registry discoverability with per-user filtering.** Should `GET /v1/registries/` hide
   registries where the user would see zero entries? Requires a subquery per registry. Defer
   or implement in Phase 1? (See Section 4.4.)

3. **Published entry claim updates.** Claims are set on first publish and subsequent versions
   must match. An API for updating claims on an existing entry name is needed for cases where
   org structure changes. Who can update — only the original claim owner? Super-admin only?
   What happens to visibility in registries when claims change?

4. **Entry deletion authorization.** The current design requires the deleting user to have the
   `manageEntries` role and JWT claims covering the entry's claims. Should super-admins be the
   only ones who can delete entries from other users? Or should any user with `manageEntries`
   who can "see" the entry be allowed to delete it?

5. **Labels API shape.** Endpoint design for `GET /v1/labels/`. Options:
    - `GET /v1/labels/?key=https://myapp.com/team` → list distinct values for a key
    - `GET /v1/labels/` → list all keys with their distinct values
    - Scoped by the user's claim boundary (operator only sees labels within their scope)

6. ~~**K8s claimMapping spec details.**~~ **Resolved.** Replaced `claimMapping` with the
   `toolhive.stacklok.dev/authz-claims` JSON annotation. Developers set claims directly
   on CRDs using the same JSON format as publish claims. Source claims are not inherited —
   they are for `manageSources` role scoping only. Missing annotation = no claims
   (default-deny when authz is on). Invalid JSON = skip with warning. Array values supported.

7. **Name squatting on publish.** The system cannot prevent team A from publishing
   `my-mcp-server` v1 and another team publishing `my-mcp-server` v2 if the first team
   hasn't published yet. Names are first-come, first-served with no reservation mechanism.
   This mirrors how upstream MCP registry works with reverse-DNS naming (ownership is on the
   domain, not enforced by the registry). Clarify with design partners whether this is
   acceptable or if additional protections are needed.

8. **Sync failure handling.** Failed syncs retain previous data (carried from v1). Do we need
   alerting, retry backoff, or a health endpoint for sync status?

9. **Pagination.** Consumer read APIs need cursor-based pagination. Cursor values are
   user-specific with per-user filtering (see Section 4.3).

10. **Rate limiting / abuse prevention.** Per-registry or global? Based on JWT identity or IP?

11. **Observability.** Metrics for sync operations, claim matching performance, API latency.
    OpenTelemetry integration points.

12. **K8s CRDs for sources and registries.** Should sources and registries be representable as
    Kubernetes custom resources (similar to how MCPServer CRDs work)? This would allow
    operators to manage sources and registries via `kubectl` instead of the config file,
    simplifying the operator experience. The config file would then only contain the `auth`
    block. Not needed initially but would simplify K8s-native deployments.

13. **Auth representation alignment.** The auth config block (roles, claim mappings) solves
    the same problem as ToolHive's OAuth CRD. The company should agree on a single way to
    represent auth configuration across ToolHive and the Registry Server.

---

## 7. Implementation Plan

### Phase 1: Foundation + Core APIs + Publishing

Restructure the data architecture, stand up all CRUD and publishing APIs with role-based
auth. By the end of Phase 1 the system is functional end-to-end: sources sync entries,
registries expose them, and users can publish — all behind JWT role checks.

**Step 1 — Schema + data layer** (sequential, everything else depends on this):

| Component | Description |
|-----------|-------------|
| Source table | New `source` table with name, type, claims JSONB, source_config JSONB, filter_config JSONB. Migration. |
| Registry-to-sources junction | Junction table with registry FK, source FK, and priority ordering. Migration. |
| Entry name / version split | Split current entry table into two: **`registry_entry`** `(type, name, source)` with `claims` JSONB column, and **`entry_version`** `(type, name, version, source)` with server/skill payload. Claims live on `registry_entry` only. Migration. |
| Shared entry pool | Refactor entry tables to use source FK instead of registry FK. Migration. |

**Step 2 — Auth + core logic** (can be parallelized after Step 1):

| Component | Description | Can parallelize with |
|-----------|-------------|---------------------|
| JWT middleware | Extract claims from JWT, store in request context (TODO already exists at `middleware.go:146`). | Sync engine, Dedup logic |
| Role resolution | Roles (`superAdmin`, `manageSources`, `manageRegistries`, `manageEntries`) resolved from JWT via config-mapped claim maps using JWT paths directly. | Sync engine, Dedup logic |
| Deployment modes | No auth / auth with roles. Gating all admin and publish endpoints behind the appropriate role. | JWT middleware (sequential) |
| Sync engine adaptation | Sync becomes per-source. Existing fetch handlers carry over. Sync writer adapted for source-scoped transactions. | JWT middleware, Source CRUD APIs, Dedup logic |
| Source CRUD APIs | `GET/PUT/DELETE /v1/sources/{name}`, `GET /v1/sources/`. Requires `manageSources` role. Config vs API ownership. | Sync engine, Dedup logic |
| Deduplication logic | Entry-level (type, name) dedup with source priority resolution on read queries. | Sync engine, Source CRUD APIs |

**Step 3 — API layer** (depends on Step 2):

| Component | Description | Can parallelize with |
|-----------|-------------|---------------------|
| Registry CRUD APIs | `PUT/DELETE /v1/registries/{name}` requires `manageRegistries`. `GET /v1/registries/` and `GET /v1/registries/{name}` require `authenticated`. Registries reference ordered sources list. Config vs API ownership. | Consumer read APIs, Publishing |
| Consumer read APIs | `/registry/{reg}/v0.1/servers`, versions, skills — reading from new multi-source model with dedup. Cursor-based pagination. | Registry CRUD APIs, Publishing |
| Global publish endpoint | `POST /v1/entries` accepting both servers and skills (mutually exclusive in payload). Requires `manageEntries` role. Claims field separate from server/skill JSON. Name allocation on first publish, claim consistency enforced on subsequent versions. Internal source must be configured in config file. | Registry CRUD APIs, Consumer read APIs |
| Delete published entries | `DELETE /v1/entries/{type}/{name}/versions/{ver}`. Requires `manageEntries` role. | Global publish endpoint (sequential) |

**Outcome**: A working multi-source, multi-registry system with role-gated CRUD, publishing,
and consumer reads. Auth enforces who can manage sources, registries, and entries. Claims
exist on entry names but are not yet used for per-user filtering.

### Phase 2: Claim-Based Visibility + Advanced Entries

Add per-user claim filtering, claim inheritance during sync, K8s claim mapping, and the
remaining admin entry-listing and claims-update endpoints.

**Claim visibility track** (sequential within the track):

| Step | Component | Description |
|------|-----------|-------------|
| 1 | Claim inheritance during sync | Populate `registry_entry.claims` column by copying source claims to entries on ingest. All versions of a name share the same claims. |
| 2 | Per-user entry filtering | JSONB containment predicate in read queries on `registry_entry`: `registry_entry.claims <@ user_jwt_claims`. **First use of JSONB operators in this codebase.** GIN index on `registry_entry.claims` column. Filter at name level, then join versions — reduces JSONB work to the lower-cardinality name set. |
| 3 | Registry access gate | Single `claims` JSONB column on registry table. Access gate check in read path — 403 if user's JWT doesn't satisfy registry claims. |
| 4 | Write-path scoping | Claim subset validation on source and registry write operations (`manageSources`, `manageRegistries`). Also enforced on publish to validate publisher claims cover entry claims. |
| 5 | K8s per-entry claims | Per-entry claims via `toolhive.stacklok.dev/authz-claims` JSON annotation on CRDs. Source claims are not inherited — annotation is the sole source of entry claims. Replaces the original `claimMapping` design. |

**Additional endpoints** (can run in parallel with claim visibility track):

| Component | Description |
|-----------|-------------|
| Source entry listing | `GET /v1/sources/{name}/entries` — unshadowed view of entries in a source, bypasses dedup. Requires `manageSources` role. |
| Registry entry listing | `GET /v1/registries/{name}/entries` — unshadowed view of entries in a registry (names + versions only). Requires `manageRegistries` role. |
| Claims update endpoint | `PUT /v1/entries/{type}/{name}/claims` — update claims on an existing published entry name. Requires `manageEntries` role + claim subset validation. |
| Labels API | **Deferred to a follow-up phase.** Operator-only endpoint at `GET /v1/labels` (requires `manageSources` or `manageRegistries`) aggregating distinct claim values from entries, scoped by the user's claim boundary. New SQLC query with JSONB aggregation. |

**Outcome**: Full v2 system — multi-source registries with JWT-based authorization,
per-user claim-scoped visibility, claim inheritance, and complete admin tooling.
Production-ready for enterprise use.

### Phase 3: Operational Polish

Deferrable improvements for scale, operational quality, and diagnostics. All items are
independent and can be done in any order.

| Component | Description | Depends on |
|-----------|-------------|------------|
| Cache exploration | Investigate caching strategies for per-user queries (see Section 4.1). Per `(registry, claims_hash)` or materialized views. | Phase 2 per-user filtering |
| Registry discoverability | Optionally hide registries where user sees zero entries (see Section 4.4). | Phase 2 access gate + entry filtering |
| Observability | Metrics for sync operations, claim matching performance, API latency. OpenTelemetry integration. | Phase 1 (can start early) |
| Rate limiting | Per-registry or global, based on JWT identity or IP. | Phase 1 JWT middleware |
| Admin diagnostic tools | Endpoint to debug visibility: given a user identity, show which entries are visible and which layer (access gate / source membership / claim match) filtered each one. | Phase 2 full auth model |

**Outcome**: Production-hardened system with caching for scale, operational visibility, and
self-service debugging. The system is fully functional after Phase 2 — Phase 3 adds
operational quality.

---

## 8. Requirements Traceability

This section maps the design decisions in this document to the authorization requirements
captured in [`authz-req.md`](authz-req.md). That document was written from the perspective
of the full ToolHive platform (runtime + registry); this design covers the **registry server**
only. Requirements that belong to the ToolHive runtime are called out as out of scope.

### 8.1 Fully covered

| Requirement (authz-req.md) | Design coverage |
|---|---|
| **JWT-based, no user sync** | Section 1 Roles — roles resolved from JWT claims, no local user storage |
| **IdP as source of truth** | Section 1 Roles — `auth.authz.roles` maps IdP-issued claim values to registry roles |
| **Visibility labels on servers** | Section 2.1 — called "claims" in this design; stored on entry names as JSONB key-value pairs |
| **AND logic across label keys** | Section 2.4 — AND across keys, same semantics |
| **OR logic within array values** | Section 2.4 — OR within arrays, same semantics |
| **Cross-label creation restrictions** | Section 2.5 — claim subset validation; users can only create resources with claims that are a subset of their JWT |
| **Super Admin full control** | Section 2.4 — super-admin bypasses all claim checks |
| **Self-service oriented** | Section 3.7 — zero admin action for new team onboarding via K8s CRD labels |
| **Virtual registries as named views** | Section 1 Registries — named, consumer-facing views over the entry pool with claims-based access gates and ordered source lists |
| **Admin scoping via virtual registries** | `manageSources` / `manageRegistries` roles are claim-scoped; registry access gate controls who can query |
| **Per-user server visibility** | Section 2.3 — per-user entry filtering; different users see different entries from the same registry |
| **Label discovery API** | Section 1 Labels API — operator-only endpoint for discovering distinct claim values |
| **Claim inheritance from creator** | Section 2.1 — synced entries inherit source claims; published entries carry explicit claims validated as subset of publisher's JWT |
| **API-first** | All operations are API-based; no UI layer |

### 8.2 Partially covered

| Requirement | What's covered | Gap |
|---|---|---|
| **ANY/ALL visibility modes** | AND-across-keys (ALL mode) is fully implemented in Section 2.4. | **No OR-across-keys (ANY) mode.** The req doc says ANY is the default — a server is visible if the user matches *any one* of its labels. v2-final only supports ALL (user must match *every* claim key on the entry). Adding ANY mode would require a different JSONB query strategy (intersection vs containment) and a per-entry mode flag. This is a **significant functional gap** if the ANY mode is required for the registry server. |
| **Anchor labels** | Open Item #1 acknowledges the need — minimum required claims on publish to prevent overly-broad visibility. | No design for anchor labels on synced or K8s entries. No mechanism for preventing removal of anchor labels on updates. The req doc's full anchor label model (Admin-protected, per-registry scoping, drift protection) is not yet designed. |
| **Claim-to-label mapping (configurable)** | K8s sources have `claimMapping` (Section 2.7) that maps CRD labels to JWT claim paths. Published entries require explicit claims. | No system-level config for "only inherit claims matching `team:*` and `project:*`." The req doc wants a global claim-to-label filter that controls which JWT claims are eligible for automatic inheritance. v2-final leaves this to the source config (K8s) or explicit publisher choice (publish endpoint). |
| **Admin role = per-virtual-registry** | `manageRegistries` is claim-scoped — users can only manage registries whose claims are a subset of their JWT. | No explicit per-registry admin *assignment*. The req doc models Admin as a per-registry role (assigned to specific registries). v2-final's model is claim-based: if your JWT covers the registry's claims, you can manage it. This is more flexible but less explicit than named assignment. |
| **Registry discoverability** | Section 4.4 discusses hiding registries where a user sees zero entries. Open Item #2. | Not yet decided — deferred to implementation. The req doc requires hiding empty registries and always showing registries to their Admins. |
| **Selector drift / audit events** | Sync lifecycle (Section 1) re-evaluates entry membership on re-sync. | No audit events when entries enter or leave a registry's scope. The req doc requires notifications to affected Admins on membership changes. |

### 8.3 Out of scope (ToolHive runtime, not registry server)

These requirements from `authz-req.md` apply to the ToolHive runtime (deployment, execution,
tool invocation) rather than the registry server (catalog, discovery, publishing):

| Requirement | Why out of scope |
|---|---|
| **Layer 3: Tool-level authorization** (read-only / read-write / destructive tiers) | Tool invocation is a runtime concern. The registry stores server/skill metadata but does not execute tools or enforce tool-level permissions. |
| **Owner always retains visibility** | The req doc says Owners can always see their servers regardless of label matching. v2-final is purely claim-based with no ownership-based visibility bypass. Ownership tracking (who deployed a server) is a runtime concept. For *published* entries, the publisher's identity could be stored, but no ownership-based visibility override is designed. |
| **Per-server Owner role** | The req doc has a strong ownership model: per-server, inheritable, with special edit/delete/visibility rights. v2-final has no per-entry ownership — authorization is claim-based. Published entry "ownership" is implicit (first-come name allocation + claim consistency), not a modeled role. |
| **Secondary owners and groups** | Runtime concept — assigning co-owners or group access to a deployed server. |
| **OAuth connection config per server** | Runtime deployment concern. |
| **Server relaunch / image refresh** | Runtime deployment concern. |
| **Health check before publish** | The req doc requires deployment validation before a server becomes visible. The registry publish endpoint stores metadata; it does not deploy containers. |
| **Deletion requires explicit confirmation** | UX/client concern, not a server-side design decision. |
| **Access groups per server** | Runtime access control concept, not modeled in the registry. |
| **Custom metadata (e.g., IAPM tracking number)** | Could be stored in the server JSON payload (which is opaque to the registry), but not explicitly modeled. |
| **Visibility mode per server (ANY vs ALL)** | If ANY mode is needed only at the runtime level (ToolHive deciding which deployed servers a user can invoke), it is out of scope. If it is needed at the *registry discovery* level, it is a gap (see Section 8.2). |

### 8.4 Key architectural differences

The req doc and this design share the same principles (JWT-based, label/claim matching,
named registries as views, self-service) but differ in two structural ways:

1. **Terminology**: The req doc uses "visibility labels" and "virtual registries." This design
   uses "claims" and "registries." The concepts are equivalent — labels/claims are key-value
   pairs on entries, registries are named views with access gates.

2. **Ownership model**: The req doc has explicit per-server ownership (Owner role, always-visible,
   inheritable). This design has no ownership concept — all authorization is claim-based.
   Whether ownership is needed at the registry level (vs only at the runtime level) is an
   open question. If a publisher should always see their published entries regardless of claim
   changes, ownership tracking would need to be added.

3. **ANY vs ALL**: The req doc assumes two matching modes as a core feature. This design
   implements only ALL (AND-across-keys). If ANY mode is required at the registry discovery
   level, it would need a per-`registry_entry` `visibility_mode` column and a different query path
   for entries in ANY mode (JSONB key intersection rather than containment).

---

## 9. Alternatives Considered

### 9.1 Approach A: Claims on entries, registry-level filtering

In the original Approach A, entries carry claims and registries filter entries by matching
entry claims against the registry's single `claims` field. All readers of a registry see the
**same entries** — the filter is registry-vs-entry, not user-vs-entry.

```
Registry "acme-platform" (claims: { org: "acme", team: "platform" })
  ├── entry-1 (claims: { org: "acme", team: "platform" })  → matches → visible
  ├── entry-2 (claims: { org: "acme", team: "data" })      → no match → hidden
  └── entry-3 (claims: { org: "acme" })                    → matches (subset) → visible

All readers see: entry-1, entry-3 (same view for everyone)
```

**Strengths:**
- Cross-registry publishing — publish once, visible in matching registries
- K8s `claimMapping` — single source per namespace, automatic onboarding
- Cacheable per registry — all readers see the same entries
- Simple mental model for operators — registry claims define the visibility boundary

**Weaknesses:**
- No per-user visibility within a registry — two users on different teams see the same
  entries if they can both access the registry
- No per-registry write control — `manageEntries` role is global
- To serve different entries to different teams, must create separate registries (one per
  team per visibility boundary)

**Why not chosen**: per-user visibility within a registry is important for reducing registry
proliferation and giving users a personalized view without requiring operators to pre-create
a registry for every team combination.

### 9.2 Approach B: No claims on entries, registry claims

In Approach B, entries carry **no claims**. Registries have two claim fields —
`readerClaims` (who can query) and `writerClaims` (who can publish). Visibility is entirely
at the registry level.

```
Registry "acme-platform" (readerClaims: { org: "acme", team: "platform" })
  ├── entry-1 (from platform source)
  ├── entry-2 (from platform source)
  └── entry-3 (from shared source)

All readers see: all 3 entries (no per-entry filtering)
```

**Strengths:**
- Simple queries — no per-row JSONB matching, scalar predicates only
- Per-registry write control via `writerClaims`
- Closer to existing codebase patterns (per-registry publish, managed source concept)

**Weaknesses:**
- No cross-registry publishing — published entries are private to one registry
- Manual wiring at scale — every source→registry relationship is manually configured
- Source proliferation in shared K8s namespaces (one source per team per namespace)
- No path to per-entry features without schema redesign
- New team onboarding requires admin action (create source + registry)

**Why not chosen**: too limited for multi-tenant enterprise use. No cross-registry
publishing, manual wiring at scale, and no foundation for per-entry features make it
a dead end if requirements grow.

### 9.3 Why this approach

The final design takes Approach A's core architecture — entries carry claims, single internal
source, K8s `claimMapping`, global publish endpoint — and adds **per-user filtering** within
registries. This is a superset of Approach A.

Two additional refinements distinguish this from the original Approach A:

1. **Claims at the name level, not version level.** The original Approach A put claims on
   every entry (version). This design moves claims to the entry name — all versions of the
   same server/skill share claims. This dramatically reduces the cardinality of claim
   matching (filter on names first, then join versions) and enables name-level ownership
   on publish (first-come, first-served allocation). This also mirrors how the upstream MCP
   registry works — ownership is on the name (reverse-DNS), not on individual versions.

2. **No `claimKeys` indirection.** Claims use JWT claim paths directly everywhere (config,
   source claims, entry claims, role definitions). This simplifies the system by removing a
   mapping layer — at the cost of slightly more verbose config when JWT paths are long URIs.

**Key trade-off**: per-user filtering loses the per-registry cacheability that original
Approach A offered. In Approach A, all readers see the same entries per registry, so results
can be cached per registry. With per-user filtering, results vary by user, requiring either
no caching or per-user-claims caching (see Section 4.1).

This is an acceptable trade-off given:
- Expected data volumes (hundreds to low thousands of distinct entry names) are well within
  PostgreSQL's uncached query performance with GIN indexes — tested to millisecond response
  times up to 100,000 entries
- Name-level claims mean the JSONB matching operates on the lower-cardinality name set,
  not on every version row
- Many users share the same effective claims, so a `(registry, claims_hash)` cache has good
  hit rates if caching is needed later
- The flexibility of per-user visibility eliminates the need to create a separate registry
  for every team combination

### Comparison table

| Aspect | Approach A (original) | Approach B | Final design |
|--------|----------------------|------------|--------------|
| Claims on entries | Yes (per version) | No | **Yes (per name, not version)** |
| Visibility granularity | Per registry | Per registry | **Per user** |
| Cross-registry publish | Yes | No | Yes |
| K8s claimMapping | Yes | No (label selectors) | Yes |
| New team onboarding | Automatic | Manual | Automatic |
| Cacheability | Per registry | Per registry | Per user-claims |
| Per-registry write control | No | Yes (writerClaims) | No |
| claimKeys indirection | Yes | Yes | **No (direct JWT paths)** |
| Query complexity | Per-row JSONB (registry vs entry) | Scalar only | **Per-name JSONB (lower cardinality)** |
| Name ownership | Not specified | Not specified | **First-come, first-served** |
| Publish endpoint | `/v1/publish/servers/` | Per-registry | **`/v1/entries/` (servers + skills)** |
| Future extensibility | Entry claims column exists | Would need redesign | Entry name claims column exists |
