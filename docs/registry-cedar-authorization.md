# Standalone Cedar authorization

## A private catalog for consumer readers, publishers and operators

Suppose only members of your IdP's `registry-readers` group may browse the
registry's consumer API. Members of `registry-publishers` may also publish and
delete entry versions. Members of `registry-operators` administer sources and
named catalog views. These are separate grants: publishers and operators who
need to browse must also belong to `registry-readers`; no role automatically
inherits consumer-read access.

Cedar applies to both the consumer and admin APIs. `catalog.read` admits a caller
to every consumer list, get, version-list and latest-version endpoint in every named
view. It is an endpoint admission decision, not per-view access or per-entry filtering.

Permitted readers receive the same nonpersonalized catalog. Existing data requires
the explicit [offline exposure procedure](registry-exposure-migration.md); startup
never strips claims or automatically approves exposure. `--initialize-empty` is
explicit new-install authorization only, disabled by default. See
[standalone assembly](registry-standalone-assembly.md) for preparation and lifecycle.

Policies use the [embedded application schema](../internal/authorization/schema.cedarschema)
and cedar-go v1.8.0's **experimental** strict validator. The examples below are
checked by `TestOfficialReferenceParity` against versioned official Rust
`cedar-policy` 4.13.0 fixtures; see the
[pinned generator](../internal/authorization/testdata/reference/README.md).
That comparison covers the shipped schema and examples, not every Cedar feature.

OAuth establishes identity; standalone always evaluates Cedar. With no custom
`authorization.policyFile`, built-in allow-all Cedar admits every verified identity
to every enabled operation, **including admin if enabled**. Admin stays off by default
and requires verified OAuth. Anonymous catalog access still needs explicit opt-in;
invalid credentials never fall back to anonymous. Unknown/mismatched operations,
missing evidence and evaluation diagnostics/errors deny. There is no standalone
alternate engine/provider/plugin option and no policy editor or API.

For the group-restricted catalog described here, explicitly supply the custom policy
below: it replaces the default entirely, never unions with it. Whole-instance
admission provides no per-entry privacy: finance and engineering readers querying
the same view see the same catalog. If their data
must remain separate, use host-owned personalized services or separate instances,
not another Cedar group rule. See the [technical reference](registry-cedar-authorization-reference.md)
for the implemented route/action boundary, identity projection and evaluation failures.

## Configure authorization

1. Retain or configure your trusted OAuth IdP and registry audience. Have it issue
   a `groups` claim as an array of strings in the token used for registry access.
2. Explicitly map that verified claim for that issuer, without guessing claim names.
   A missing group claim gives no group grant; a malformed mapped claim rejects
   authentication. Token claims cannot overwrite verified identity attributes.
3. Create `policies.cedar` with the consumer-read, publisher and operator grants
   below. Point your registry config at it and explicitly enable admin endpoints
   for publishing and administration.

Keep database, supported sources/views and operational settings. Remove retired
`auth.authz`/roles/publicPaths and resource claims from the **final** config before
offline review. Map only verified identity claims; those are not resource claims.

```yaml
# Existing authentication fields
auth:
  mode: oauth
  oauth:
    providers:
      - name: company
        issuerUrl: https://id.example.org
        audience: registry-api
# Standalone admission blocks
admin:
  enabled: true                # Default false; no automatic permission grant
authorization:
  policyFile: /etc/registry/policies.cedar
  allowAnonymousCatalog: false
  identityMappings:
    - issuer: https://id.example.org
      groups:
        claim: groups
        format: string-array
```

Policy files contain plain Cedar. The absolute path must refer to a readable regular file
of at most 1 MiB. Missing, empty, comment-only, syntax-invalid or schema-invalid
files fail preflight before migrations in `serve`, and before storage/writer/operator
construction in the app builder. An invalid explicit file never falls back to the
built-in policy. Omission alone selects the default; neither policy grants exposure consent.

Authenticated principals use `Registry::Principal` with a deterministic ID derived
from a JSON tuple of the exact verified provider issuer and subject (SHA-256,
`authenticated:` prefix). Anonymous uses the disjoint `anonymous` ID. No token
attribute can overwrite `authenticated`, `issuer` or `subject`; spoofed reserved
attributes reject authentication. The bounded optional `groups` and `scopes`
attributes support explicit `string-array` or `space-delimited` mappings, with
at most 256 values, 4096 bytes per value and 16 KiB total per mapped attribute.
Missing mapped claims grant nothing; malformed mapped claims return 401.

**Opaque-token compatibility:** ToolHive v0.46.0 validates introspection issuer,
audience and expiration, but RFC7662 parsing preserves only `iss`, `sub`, `aud`,
`exp` and `scope`, not arbitrary groups. Use explicitly mapped `scope` with
`space-delimited` for such tokens, or signed JWTs for arbitrary group claims.
Introspection must provide the exact configured issuer and a nonempty subject;
responses without sufficient identity fail closed (401), never acquire a locally
invented issuer or anonymous fallback. Google tokeninfo uses ToolHive's
provider-established issuer; it still needs matching issuer and audience.
Duplicate provider names/issuers and duplicate/unconfigured identity mappings
are rejected. No provider attributes are inferred from unverified JWT parsing.

## Write a policy

A principal is the caller, with identity attributes established by authentication.
An action is the requested operation, such as browsing or publishing.
The resource is the entire registry instance, always `Registry::Instance::"catalog"`,
not an entry, source or view. These names are defined by the embedded registry schema.

Put all three blocks in `policies.cedar`. Replace the issuer and group
names with your IdP's exact values. Permits add grants; without a matching permit,
access is denied. An explicit Cedar `forbid` takes precedence over any permit.

```cedar
// Readers may use every consumer endpoint in every named view. This does not
// grant entry-write or admin access, and it is not a per-view or per-entry rule.
permit (
    principal is Registry::Principal,
    action in [Registry::Action::"catalog.read", Registry::Action::"metadata.read"],
    resource == Registry::Instance::"catalog"
) when {
    principal.authenticated && principal.issuer == "https://id.example.org" &&
    principal has groups && principal.groups.contains("registry-readers")
};

// Publishers may publish or delete any entry version in the managed source.
permit (
    principal is Registry::Principal,
    action in [Registry::Action::"entries.publish", Registry::Action::"entries.delete"],
    resource == Registry::Instance::"catalog"
) when {
    principal.authenticated && principal.issuer == "https://id.example.org" &&
    principal has groups && principal.groups.contains("registry-publishers")
};

// Operators manage sources and views, but receive no entry-write grant here.
permit (
    principal is Registry::Principal,
    action in [
        Registry::Action::"sources.read", Registry::Action::"sources.write",
        Registry::Action::"sources.delete", Registry::Action::"views.read",
        Registry::Action::"views.write", Registry::Action::"views.delete"
    ],
    resource == Registry::Instance::"catalog"
) when {
    principal.authenticated && principal.issuer == "https://id.example.org" &&
    principal has groups && principal.groups.contains("registry-operators")
};
```

`catalog.read` is checked before the following ordinary consumer GET requests:

| Consumer route family | GET operations covered by `catalog.read` |
| --- | --- |
| `/registry/{registryName}/v0.1/servers` | List servers, list a server's versions, fetch an exact version or `latest` |
| `/registry/{registryName}/v0.1/x/dev.toolhive/skills` | List skills, fetch a skill's latest version, list versions, fetch an exact version |
| `/registry/{registryName}/v0.1/x/dev.toolhive/plugins` | List plugins, fetch a plugin's latest version, list versions, fetch an exact version |

This includes each family's nested entry and version routes, for every named
view selected by `{registryName}`. The singleton `Registry::Instance::"catalog"`
cannot express grants for individual views or entries. Once admitted, callers
receive the same consumer response for that view.

The issuer check prevents a same-named group from another provider granting access.
`principal has groups` safely handles tokens without that optional attribute.
Policy-file write access is security administration, separate from these API grants.

## Start and verify access

Mount `config.yaml` and `policies.cedar` read-only at `/etc/registry/`, with write
access restricted to deployment administrators. Use TLS or a trusted TLS ingress.
After the [offline procedure](registry-exposure-migration.md), start with
`thv-registry-api serve --config /etc/registry/config.yaml`. For a genuinely empty
new database only, explicitly use `serve --initialize-empty --config /etc/registry/config.yaml`.
No legacy roles or resource claim filters participate in either selected path.

With admin explicitly enabled and the example policies, the expected results are:

| Caller | Browse catalog | Publish/delete entry | Administer sources/views |
| --- | --- | --- | --- |
| Authenticated reader, `registry-readers` only | Allowed | 403 | 403 |
| Publisher, `registry-readers` + `registry-publishers` | Allowed | Allowed | 403 |
| Operator, `registry-readers` + `registry-operators` | Allowed | 403 | Allowed |
| Publisher or operator without `registry-readers` | 403 | Its separate grant, if any | Its separate grant, if any |
| Anonymous, no token | 401 | 401 | 401 |

"Allowed" still requires normal payload validation, ownership and storage checks.
A 401 means authentication is missing or invalid; a 403 means a valid caller lacks
permission. Permitted readers receive the same pages/cursors; there is no per-entry
or per-view Cedar grant.

With admin enabled, `/v1/me` and both GET/PUT
`/v1/entries/{type}/{name}/claims` are authenticated static 410 tombstones, with
no service calls. They cannot retag entries; use the
[offline migration procedure](registry-exposure-migration.md#maintenance-procedure)
to review and retire historical claims. Source/view resource claims are rejected by presence.
With admin disabled the entire `/v1` surface is absent (404). Operational probes
stay on the separate internal port. Unknown public URLs return 404, unsupported
methods (including HEAD/OPTIONS for catalog routes) return 405 without calling
services. Only exact GET `/.well-known/oauth-protected-resource` is a credential
bootstrap exemption; OpenAPI requires `metadata.read`. `auth.publicPaths` is
rejected, including empty and null values, rather than interpreted as a bypass.

A missing, empty or invalid **explicitly configured policy file** prevents startup
from serving catalog/admin traffic. No matching grant denies the operation without
calling the catalog service. Server-side evaluation failure returns a safe generic error
(503 for evaluator unavailability, 500 for wiring faults), never partial data.
Cedar admission decision events contain actor, action, decision and policy revision,
not raw tokens, claims or policy contents. This is not a guarantee about all logs:
the opt-in `audit.includeRequestData` captures request bodies, including
resource claims and any sensitive payload values, even on Cedar denials. Leave it
disabled unless that exposure and the audit sink's access/retention are acceptable.
Permission does not imply that a subsequent write succeeded.

Policy edits require a restart; drain old replicas when revoking access because
they retain the old policy. A JWT group change requires a fresh token to be visible;
an already-issued token retains its claims until replaced or expired.

Anonymous browsing is optional and off by default. It requires both
`authorization.allowAnonymousCatalog: true` and a Cedar `catalog.read` permit
for the anonymous principal; the private reader-group policy above does not
provide one. Invalid supplied tokens still get 401, never anonymous fallback.
Retired `auth.authz` is rejected for every authentication mode. Anonymous admission
is not a migration shortcut: every pre-existing catalog still requires explicit
whole-catalog review and retirement, including unlabeled data.
`PublicPaths` or any other authentication bypass must never bypass Cedar: a
consumer route without verified or explicitly anonymous request evidence is
denied. Admin endpoints are disabled by default, and always require
authentication when enabled, even with anonymous browsing.

## Migration: a deliberate exposure decision

Follow the [offline migration procedure](registry-exposure-migration.md#maintenance-procedure)
for inventory, acknowledgement, explicit claims retirement and finalization at schema
29 before schema-30 cleanup. Only `retire-claims` archives and clears historical
metadata; startup, reconciliation and ordinary migrations cannot approve exposure.
Archives, receipts and version-1 fingerprint compatibility remain for offline tooling.

This is a breaking upgrade, not a policy-file swap. Stop/drain all old readers and
writers and verify a complete backup. Inventory every catalog, including unlabeled
data: it was also default-denied under legacy authorization. No claims does not mean
safe to expose. Decide whether the whole catalog can be shared with admitted readers
or requires host-owned personalization or separate instances; resource claims cannot
be automatically translated to equivalent whole-instance permissions.

Prepare final config/policy and remediate retired fields and exported operator
annotations before review. A changed pending review requires fresh inventory and
acknowledgement, never silent receipt rebinding. `initialize-empty` applies only to
an entirely empty new catalog. Do not restart old binaries after retirement; inspect
unresolved receipt outcomes offline. Schema-30 rollback requires restoring a verified
complete backup, not reconstructing claims from archives.

## Library hosts

Cedar is mandatory for standalone, not for reusable packages. Library hosts own
verified authentication, admission and personalized selection. They may supply
`httpapi.Authorizer` through `Middleware`/`Decorate` without any Cedar dependency;
undecorated routers are policy-free. See [embedding guidance](registry-embedding-validation.md)
for the public contracts and executable host-authentication example.
