# Cedar authorization

The registry server authorizes requests in one of two modes, chosen once per
deployment:

| | Claims mode (default) | Cedar mode |
| --- | --- | --- |
| Selected by | no `authorization` block | an `authorization` block |
| Who may call what | `auth.authz` roles (`superAdmin`, `manageSources`, …) | a Cedar policy |
| What a caller sees | entries whose claims match the caller's token claims | every entry of a registry the policy lets them read |
| Admin API (`/v1`) | always served, with claims fields, `/v1/me` and entry-claims endpoints | served only with `admin.enabled`; no claims, roles, `/v1/me` or entry-claims endpoints |

Claims mode is v1.5.4's model and is described in
[authentication.md](authentication.md). This page covers Cedar mode.

## One mode per deployment

The server records its mode in the database on its first start and refuses to
start with the other mode:

```
authorization mode mismatch: this database uses claims mode but the configuration
selects cedar mode; an existing deployment cannot switch modes, so start cedar mode
on a new database
```

Databases upgraded from v1.5.4 or earlier are recorded as claims mode. To move a
claims-mode deployment to Cedar, start a new instance on a new database: synced
sources fill themselves on the first sync, and only entries published through
`POST /v1/entries` need publishing again.

## What Cedar decides

Cedar admits or refuses each request at the route. It never filters entries:

- every consumer read (`/registry/{registryName}/v0.1/...`, including skills and
  plugins) needs `catalog.read` on `Registry::View::"<registryName>"`;
- `/openapi.json` needs `metadata.read`;
- admin routes need `sources.read|write|delete`, `views.read|write|delete`,
  `entries.publish` or `entries.delete` on `Registry::Instance::"catalog"`.

Every caller admitted to a registry sees the same entries. An entry reachable
through two registries is readable through either one the caller may read, so
keep registry composition consistent with who should see what, or use separate
instances for stronger isolation.

The schema is [`pkg/registry/cedar/schema.cedarschema`](../pkg/registry/cedar/schema.cedarschema).
The authorizer lives in the `pkg/registry/cedar` library package, so hosts that
embed the registry library can use the same policies.

## Configure Cedar mode

```yaml
auth:
  mode: oauth
  oauth:
    resourceUrl: https://registry.example.org
    providers:
      - name: company
        issuerUrl: https://id.example.org
        audience: registry-api
authorization:
  policyFile: /etc/registry/policies.cedar   # absolute path; omit for the built-in policy
  allowAnonymousCatalog: false
  identityMappings:
    - issuer: https://id.example.org
      groups:
        claim: groups                         # token claim holding the caller's groups
        format: string-array                  # or space-delimited
admin:
  enabled: true                               # default false; requires auth.mode oauth
```

In Cedar mode the server rejects claims-mode settings: `auth.authz`,
`auth.publicPaths`, and `claims` on sources or registries. Claims mode rejects
the `admin` block.

| Setting | Meaning |
| --- | --- |
| `authorization.policyFile` | Absolute path to a Cedar policy file (a regular file of at most 1 MiB). Without it the built-in policy permits every action to every caller that passes authentication, **including the admin API when it is enabled**. |
| `authorization.allowAnonymousCatalog` | Lets requests without a token through as the `anonymous` principal. A request that carries a token is always validated; an invalid token never falls back to anonymous. Required with `auth.mode: anonymous`. |
| `authorization.identityMappings` | Per OAuth issuer, the token claims that become the principal's `groups` and `scopes`. Formats: `string-array` (a JSON array of strings) or `space-delimited` (a string such as OAuth `scope`). At most 256 values, 4096 bytes per value and 16 KiB per attribute. A missing claim grants nothing; a malformed one rejects the request (401). |
| `admin.enabled` | Serves the admin API under `/v1`. Off by default, and needs `auth.mode: oauth`. Anonymous callers never reach admin routes. |

Environment overrides: `THV_REGISTRY_AUTHORIZATION_POLICYFILE`,
`THV_REGISTRY_AUTHORIZATION_ALLOWANONYMOUSCATALOG` and `THV_REGISTRY_ADMIN_ENABLED`.
Setting either authorization variable selects Cedar mode.

A policy that is missing, empty, unreadable, not valid Cedar or not valid against
the schema stops the server at startup; it never falls back to the built-in policy.
Policy edits take effect on restart.

## Principals

An authenticated caller is `Registry::Principal::"authenticated:<sha256>"`, an ID
derived from the token's issuer and subject, with attributes:

| Attribute | Value |
| --- | --- |
| `authenticated` | `true` (`false` for the anonymous principal) |
| `issuer`, `subject` | the token's `iss` and `sub` |
| `groups`, `scopes` | optional, from the issuer's identity mapping |

Anonymous callers are `Registry::Principal::"anonymous"` with `authenticated`
set to `false`.

## Write a policy

Permits grant access; anything not permitted is refused, and a `forbid` overrides
any permit. Check the issuer so a same-named group from another provider grants
nothing, and use `principal has groups` for tokens without the attribute.

```cedar
// Engineering may read the engineering registry, not finance.
permit (
    principal is Registry::Principal,
    action == Registry::Action::"catalog.read",
    resource == Registry::View::"engineering"
) when {
    principal.authenticated && principal.issuer == "https://id.example.org" &&
    principal has groups && principal.groups.contains("engineering")
};

// The API description is a separate grant.
permit (
    principal is Registry::Principal,
    action == Registry::Action::"metadata.read",
    resource == Registry::Instance::"catalog"
) when {
    principal.authenticated && principal.issuer == "https://id.example.org" &&
    principal has groups && principal.groups.contains("engineering")
};

// Publishers may publish and delete entry versions.
permit (
    principal is Registry::Principal,
    action in [Registry::Action::"entries.publish", Registry::Action::"entries.delete"],
    resource == Registry::Instance::"catalog"
) when {
    principal.authenticated && principal.issuer == "https://id.example.org" &&
    principal has groups && principal.groups.contains("registry-publishers")
};

// Operators manage sources and registries.
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

// Optional: anyone may read the public registry (needs allowAnonymousCatalog).
permit (
    principal,
    action == Registry::Action::"catalog.read",
    resource == Registry::View::"public"
);
```

Grants are independent: a publisher who should also browse needs a
`catalog.read` grant too.

## Responses

| Situation | Status | Body |
| --- | --- | --- |
| Permitted | the route's normal response | |
| No token where one is required, invalid token, malformed mapped claim | 401 | `{"error":"unauthorized"}` with a `WWW-Authenticate` challenge pointing at `/.well-known/oauth-protected-resource` |
| Valid caller without a permit | 403 | `{"error":"authorization denied or unavailable"}` |
| Policy evaluation failure | 503 | `{"error":"authorization denied or unavailable"}` |
| Admin route with `admin.enabled` off, `/v1/me`, `/v1/entries/{type}/{name}/claims` | 404 | |

Only `/.well-known/oauth-protected-resource` is served without authorization.
Health, readiness, version and metrics stay on the internal port.

## Audit

With audit logging enabled, every Cedar decision is recorded as an
`authorization.admission` event with the principal ID, action, resource, decision
(`allow`, `deny` or `error`) and the policy revision (`sha256:` of the policy).
The event never contains tokens, claims or policy text. Each request is also
audited with its usual event type (`server.list`, `source.create`, …), including
refused ones. `audit.includeRequestData` captures request bodies even for refused
requests; leave it off unless that is acceptable for your audit sink.
