# Standalone Cedar authorization

## A private catalog for consumer readers, publishers and operators

Suppose only members of your IdP's `registry-readers` group may browse the
registry's consumer API. Members of `registry-publishers` may also publish and
delete entry versions. Members of `registry-operators` administer sources and
named catalog views. These are separate grants: publishers and operators who
need to browse must also belong to `registry-readers`; no role automatically
inherits consumer-read access.

Cedar applies to both the consumer and admin APIs. In this proposal,
`catalog.read` admits a caller to every consumer list, get, version-list and
latest-version endpoint in every named view. It is an endpoint admission
decision, not per-view access or per-entry filtering.

This is a product proposal for #912, not implemented or approved functionality.
All Cedar and new configuration examples below are PROPOSED and have not been
parsed, typechecked or executable-validated. Today's claims/RBAC still apply.

Today, operators configure fixed roles and attach claims to sources, views and
entries to control access and filter results. We propose replacing those rules
with an operator-owned Cedar policy file that grants operations on the whole
instance. Keep OAuth for identity; use Cedar to decide what that identity may do.
There is no policy editor, policy API or automatic sample allow-all policy.

Crucially, this is not per-entry privacy. Finance and engineering readers querying
the same view see the same catalog, including both teams' entries. Named views
organize data, but are not security partitions: read permission covers all views.
If finance must not see engineering entries, use host-owned personalized catalog
services or separate instances, not another Cedar group rule on this instance.

## What would I configure?

1. Retain or configure your trusted OAuth IdP and registry audience. Have it issue
   a `groups` claim as an array of strings in the token used for registry access.
2. Explicitly map that verified claim for that issuer, without guessing claim names.
   A missing group claim gives no group grant; a malformed mapped claim rejects
   authentication. Token claims cannot overwrite verified identity attributes.
3. Create `policies.cedar` with the consumer-read, publisher and operator grants
   below. Point your registry config at it and explicitly enable admin endpoints
   for publishing and administration.

This config fragment is PROPOSED, not runnable today. Keep existing database,
sources, views and operational settings. The `auth` fields exist; blocks marked
PROPOSED are new, with names/placement not finalized. Map only verified claims.

```yaml
# Existing authentication fields
auth:
  mode: oauth
  oauth:
    providers:
      - name: company
        issuerUrl: https://id.example.org
        audience: registry-api
# PROPOSED new blocks and all their fields
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

Plain Cedar is the proposed file format here; the earlier investigation recommends
a JSON bundle. Packaging is unsettled; neither is supported by today's server.

## What does the policy look like?

A principal is the caller, with identity attributes established by authentication.
An action is the requested operation, such as browsing or publishing.
The resource is the entire registry instance, always `Registry::Instance::"catalog"`,
not an entry, source or view. These names follow the proposed registry schema.

Put all three blocks in the proposed `policies.cedar`. Replace the issuer and group
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
After implementation, start with the existing invocation
`thv-registry-api serve --config /etc/registry/config.yaml`.
The command exists today, but the proposed config and policy support do not.

With admin explicitly enabled and the example policies, the expected results are:

| Caller | Browse catalog | Publish/delete entry | Administer sources/views |
| --- | --- | --- | --- |
| Authenticated reader, `registry-readers` only | Allowed | 403 | 403 |
| Publisher, `registry-readers` + `registry-publishers` | Allowed | Allowed | 403 |
| Operator, `registry-readers` + `registry-operators` | Allowed | 403 | Allowed |
| Publisher or operator without `registry-readers` | 403 | Its separate grant, if any | Its separate grant, if any |
| Anonymous, no token | 401 | 401 | 401 |

A 401 means authentication is missing or invalid; a 403 means a valid caller lacks
permission. Permitted calls use the existing APIs and return unchanged payloads,
subject to normal validation and catalog errors, not caller-specific filtering.

A missing, empty or invalid policy prevents startup from serving catalog/admin
traffic. No matching grant denies the operation without calling the
catalog service. Server-side evaluation failure returns a safe generic error
(503 for evaluator unavailability, 500 for wiring faults), never partial data.
Denials are audited with actor, action, decision and policy revision, not raw tokens,
claims or policy contents. Permission does not imply that a subsequent write succeeded.

Policy edits require a restart; drain old replicas when revoking access because
they retain the old policy. A JWT group change requires a fresh token to be visible;
an already-issued token retains its claims until replaced or expired.

Anonymous browsing is optional and off by default. It requires both
`authorization.allowAnonymousCatalog: true` and a Cedar `catalog.read` permit
for the anonymous principal; the private reader-group policy above does not
provide one. Invalid supplied tokens still get 401, never anonymous fallback.
`PublicPaths` or any other authentication bypass must never bypass Cedar: a
consumer route without verified or explicitly anonymous request evidence is
denied. Admin endpoints are disabled by default, and always require
authentication when enabled, even with anonymous browsing.

## Migration: a deliberate exposure decision

This is a breaking upgrade, not a policy-file swap. Before upgrading, back up and
inventory ALL old catalogs: sources, views, entries, retained snapshots and operator
exports, including unlabelled data. Unlabelled data was also default-denied when
legacy authorization was enabled, so "no claims" does not mean "safe to expose".

The operator decides whether the complete catalog may be shared with the new reader
population, or whether to move data to host-owned personalization or separate
instances. Staying on the legacy release remains an option. There is no automatic
translation from old resource claims to equivalent whole-instance permissions.

Stop/drain old readers and writers. The proposed offline migration requires an
explicit acknowledgement tied to the inventoried database/config revision before
new behavior starts, even if old claims are empty. Only that deliberate migration
may archive/remove obsolete claims; startup never silently strips them. Resolve
retired config/API claims and operator annotations before cutover. Reopen traffic
after migration and access checks, keeping the backup for an offline rollback.
The migration command and acknowledgement interface still need design.

## Product questions

- Does whole-catalog operation permission meet the intended use, or is per-entry privacy required?
- Is the first deployment private or publicly browsable, and who needs publisher versus operator access?
- Is an operator-managed file with restart activation acceptable for policy changes?
- Is an offline inventory, exposure decision and acknowledged migration an acceptable upgrade journey?

The [technical reference](registry-cedar-authorization-reference.md) has optional investigation, schema, route, validation, migration and draft issue details.
