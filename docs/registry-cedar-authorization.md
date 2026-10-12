# Standalone Cedar authorization

## A private catalog for consumer readers, publishers and operators

Suppose only members of your IdP's `registry-readers` group may browse the
registry's consumer API. Members of `registry-publishers` may also publish and
delete entry versions. Members of `registry-operators` administer sources and
named catalog views. These are separate grants: publishers and operators who
need to browse must also belong to `registry-readers`; no role automatically
inherits consumer-read access.

Cedar applies to both the consumer and admin APIs. In this admission slice,
`catalog.read` admits a caller to every consumer list, get, version-list and
latest-version endpoint in every named view. It is an endpoint admission
decision, not per-view access or per-entry filtering.

**Status: user-authorized first runtime admission slice for #912.** Cedar is an
additional restriction, not a replacement for legacy authorization yet. Existing
`auth.authz`, role resolution, source/view/entry claims, personalized filtering,
service checks and PostgreSQL mutation guards remain in force. A Cedar permit
cannot bypass them, including the legacy super-admin rules. No data or claims
are automatically removed or exposed. Offline inventory, dormant revision-bound
acknowledgement and explicit offline archive-and-clear are implemented; see the
[migration guide](registry-exposure-migration.md). Metadata retirement is **not**
runtime activation: remain offline afterward until a runtime cutover release or
an offline backup restore. Runtime claims/RBAC retirement and marker consumption
remain a later slice; #912 is not complete. Services/HTTP
extraction (#913–#915) is deferred.

The examples below are parsed and strictly typechecked by tests against the
embedded application schema using cedar-go v1.8.0's experimental validator.
This verifies the Go implementation, not parity with the Rust reference validator.
Reference-validator fixtures and independent review remain release gates.

Today, operators configure fixed roles and attach claims to sources, views and
entries to control access and filter results. We propose replacing those rules
with an operator-owned Cedar policy file that grants operations on the whole
instance. Keep OAuth for identity; use Cedar to decide what that identity may do.
There is no policy editor, policy API or automatic sample allow-all policy.

The eventual claims-removal design is not per-entry privacy. After the separate,
explicitly acknowledged migration, finance and engineering readers querying the
same view will see the same catalog, including both teams' entries. That is **not**
the behavior of this admission-only slice: legacy claims still restrict responses.
Named views organize data, but Cedar does not grant per-view access. If finance
must not see engineering entries after migration, use host-owned personalized
catalog services or separate instances, not another Cedar group rule.

## What would I configure?

1. Retain or configure your trusted OAuth IdP and registry audience. Have it issue
   a `groups` claim as an array of strings in the token used for registry access.
2. Explicitly map that verified claim for that issuer, without guessing claim names.
   A missing group claim gives no group grant; a malformed mapped claim rejects
   authentication. Token claims cannot overwrite verified identity attributes.
3. Create `policies.cedar` with the consumer-read, publisher and operator grants
   below. Point your registry config at it and explicitly enable admin endpoints
   for publishing and administration.

Keep existing database, sources, views, operational settings and legacy
`auth.authz`/resource claims. The new blocks are mandatory standalone admission
configuration. Map only verified claims.

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

Plain Cedar is the file format for this slice and supersedes the earlier JSON
bundle recommendation. The absolute path must refer to a readable regular file
of at most 1 MiB. Missing, empty, comment-only, syntax-invalid or schema-invalid
files fail preflight before migrations in `serve`, and before storage/writer/operator
construction in the app builder. There is no implicit policy or allow-all fallback.

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

## What does the policy look like?

A principal is the caller, with identity attributes established by authentication.
An action is the requested operation, such as browsing or publishing.
The resource is the entire registry instance, always `Registry::Instance::"catalog"`,
not an entry, source or view. These names follow the proposed registry schema.

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
view selected by `{registryName}`. The singleton
`Registry::Instance::"catalog"` means this proposed policy cannot grant one
reader access to only one view. Once admitted, callers receive the ordinary
consumer response for that view; this proposal adds no personalized filtering
or per-view grants.

The issuer check prevents a same-named group from another provider granting access.
`principal has groups` safely handles tokens without that optional attribute.
Policy-file write access is security administration, separate from these API grants.

## How would I start it and check access?

Mount `config.yaml` and `policies.cedar` read-only at `/etc/registry/`, with write
access restricted to deployment administrators. Use TLS or a trusted TLS ingress.
After mounting policy and config, start with the existing invocation
`thv-registry-api serve --config /etc/registry/config.yaml`.
Configure legacy roles and resource claims as before: readers still need the
view/entry claims; publishers need the legacy `manageEntries` role and covered
managed-source/entry claims; operators need `manageSources`/`manageRegistries`
and their resource claims whenever `auth.authz` is enabled.

With admin explicitly enabled and the example policies, the expected results are:

| Caller | Browse catalog | Publish/delete entry | Administer sources/views |
| --- | --- | --- | --- |
| Authenticated reader, `registry-readers` only | Allowed | 403 | 403 |
| Publisher, `registry-readers` + `registry-publishers` | Allowed | Allowed | 403 |
| Operator, `registry-readers` + `registry-operators` | Allowed | 403 | Allowed |
| Publisher or operator without `registry-readers` | 403 | Its separate grant, if any | Its separate grant, if any |
| Anonymous, no token | 401 | 401 | 401 |

The table shows Cedar admission only. "Allowed" still requires all applicable
legacy roles and claims, normal payload validation, ownership and storage checks.
A 401 means authentication is missing or invalid; a 403 means a valid caller lacks
permission. Responses remain unchanged and may still be caller-specific under
legacy claims filtering. There is no new per-entry or per-view Cedar grant.

With admin enabled, `/v1/me` and both GET/PUT
`/v1/entries/{type}/{name}/claims` are authenticated static 410 tombstones, with
no service calls. Retagging via those endpoints is no longer available in this
slice; inventory/backup and resolve entry claims on the legacy release before
upgrading. Source/view configuration and their legacy claims remain supported.
With admin disabled the entire `/v1` surface is absent (404). Operational probes
stay on the separate internal port. Unknown public URLs return 404, unsupported
methods (including HEAD/OPTIONS for catalog routes) return 405 without calling
services. Only exact GET `/.well-known/oauth-protected-resource` is a credential
bootstrap exemption; OpenAPI requires `metadata.read`. `auth.publicPaths` is
rejected, including empty and null values, rather than interpreted as a bypass.

A missing, empty or invalid policy prevents startup from serving catalog/admin
traffic. No matching grant denies the operation without calling the
catalog service. Server-side evaluation failure returns a safe generic error
(503 for evaluator unavailability, 500 for wiring faults), never partial data.
Cedar admission decision events contain actor, action, decision and policy revision,
not raw tokens, claims or policy contents. This is not a guarantee about all logs:
the pre-existing opt-in `audit.includeRequestData` captures request bodies, including
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
**Transitional constraint:** anonymous catalog admission is rejected at startup
when legacy `auth.authz` is configured. Those legacy list filters interpret absent
caller claims as unfiltered; allowing anonymous requests there would bypass the
protection retained in this slice. Do not disable legacy authorization to make
restricted catalogs anonymous. Keep those deployments authenticated until the
separate acknowledged exposure migration/removal is implemented. Anonymous
admission works for explicitly non-claims-scoped deployments, with the config
opt-in and a policy permit; it is not a migration shortcut.
`PublicPaths` or any other authentication bypass must never bypass Cedar: a
consumer route without verified or explicitly anonymous request evidence is
denied. Admin endpoints are disabled by default, and always require
authentication when enabled, even with anonymous browsing.

## Migration: a deliberate exposure decision

Offline inventory, pending acknowledgement and explicit metadata archive-and-clear
are implemented in the [migration guide](registry-exposure-migration.md). This is
**not** completion of a runtime cutover: claims/RBAC/storage guard code remains
unchanged. Only the explicit `retire-claims` command clears historical metadata;
afterward stay offline. Ordinary migrations/startup/reconciliation never do this.

This is a breaking upgrade, not a policy-file swap. Before upgrading, back up and
inventory ALL old catalogs: sources, views, entries, retained snapshots and operator
exports, including unlabelled data. Unlabelled data was also default-denied when
legacy authorization was enabled, so "no claims" does not mean "safe to expose".

The operator decides whether the complete catalog may be shared with the new reader
population, or whether to move data to host-owned personalization or separate
instances. Staying on the legacy release remains an option. There is no automatic
translation from old resource claims to equivalent whole-instance permissions.

Stop/drain old readers and writers. Prepare the **final** config/policy without
retired fields before inventorying the historical database and acknowledging its
whole-catalog exposure decision. Changing config invalidates an old acknowledgement;
re-inventory and acknowledge, never silently rebind it. Then explicit offline
`retire-claims` may archive/clear metadata against the pending post-fence digest.
Fresh `initialize-empty` evidence needs no archive. Both markers remain dormant:
a later slice must consume/revalidate them before guard retirement. Startup never
silently clears metadata. Resolve retired config/API claims and operator annotations
before eventual cutover. Do not restart this slice after clearing; stay offline
until the runtime cutover release or restore the verified backup offline.

## Product questions

- Does whole-catalog operation permission meet the intended use, or is per-entry privacy required?
- Is the first deployment private or publicly browsable, and who needs publisher versus operator access?
- Is an operator-managed file with restart activation acceptable for policy changes?
- Is an offline inventory, exposure decision and acknowledged migration an acceptable upgrade journey?

The [technical reference](registry-cedar-authorization-reference.md) has optional investigation, schema, route, validation, migration and draft issue details.
