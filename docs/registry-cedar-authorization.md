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

**Status:** `serve` selects `internal/app.NewCatalogApp` and the public services,
HTTP adapters and exclusively leased ingestion. Final retired-field validation,
frozen Cedar, read-only source/operator preflight and serialized receipt consumption
(offline finalization at 29, ordinary serving at clean 30) precede reconciliation/repair
and both listeners. Permitted readers
receive the same nonpersonalized catalog. Existing data requires the explicit
[offline exposure procedure](registry-exposure-migration.md); startup never strips
claims or automatically approves it. `--initialize-empty` is explicit new-install
authorization only, disabled by default. Schema 30 removes live claims columns only
after explicit historical finalization; archives and receipts remain.

The examples below are parsed and strictly typechecked by tests against the
embedded application schema using cedar-go v1.8.0's **experimental** validator.
`TestOfficialReferenceParity` compares validation and evaluation with versioned
fixtures actually executed using the official Rust `cedar-policy` **4.13.0** crate.
The [pinned generator and provenance](../internal/authorization/testdata/reference/README.md)
cover the shipped schema, examples and decision matrix, not every possible Cedar
policy or language feature. Passing fixtures do not make the Go validator stable
or eliminate independent security review.

OAuth establishes identity; an operator-owned Cedar policy file grants operations
on the whole instance. There is no policy editor, policy API or automatic allow-all
policy. Claims-removal is not per-entry privacy: after explicit migration, finance
and engineering readers querying the same view see the same catalog. If their data
must remain separate, use host-owned personalized services or separate instances,
not another Cedar group rule.

## What would I configure?

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
no service calls. Retagging via those endpoints is no longer available in this
slice; inventory/backup and resolve entry claims on the legacy release before
upgrading. Source/view resource claims are likewise retired and rejected by presence.
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
Retired `auth.authz` is rejected for every authentication mode. Anonymous admission
is not a migration shortcut: every pre-existing catalog still requires explicit
whole-catalog review and retirement, including unlabeled data.
`PublicPaths` or any other authentication bypass must never bypass Cedar: a
consumer route without verified or explicitly anonymous request evidence is
denied. Admin endpoints are disabled by default, and always require
authentication when enabled, even with anonymous browsing.

## Migration: a deliberate exposure decision

Offline inventory, acknowledgement and explicit metadata archive-and-clear are
implemented in the [migration guide](registry-exposure-migration.md). Only the
explicit `retire-claims` command clears historical metadata; ordinary migrations,
startup and reconciliation never do so. The new runtime consumes valid evidence
after preflight. The legacy runtime is removed; private offline compatibility,
historical migrations, archives and receipts remain. The same final binary finalizes
historical format-1 evidence at 29 before forward cleanup removes live columns at 30.

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
Fresh `initialize-empty` evidence needs no archive. The new startup consumes valid
markers after the mandatory final-config/frozen-policy/source/operator preparation
barrier. Startup never silently clears metadata. Resolve retired config/API claims
and live operator annotations before cutover; do not restart legacy binaries after
clearing. Inspect unresolved receipt outcomes offline or restore the verified backup.

## Product questions

These are deployment-owner checklist questions, not unresolved implementation choices
or automatic exposure approval. The separate operator same-name cross-kind identity
choice remains pending user agreement; see the [acceptance matrix](registry-embedding-validation.md).

- Does whole-catalog operation permission meet the intended use, or is per-entry privacy required?
- Is the first deployment private or publicly browsable, and who needs publisher versus operator access?
- Is an operator-managed file with restart activation acceptable for policy changes?
- Is an offline inventory, exposure decision and acknowledged migration an acceptable upgrade journey?

The [technical reference](registry-cedar-authorization-reference.md) has optional investigation, schema, route, validation, migration and draft issue details.
