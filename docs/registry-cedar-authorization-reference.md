# Standalone Cedar authorization

[Back to the product proposal](registry-cedar-authorization.md).

This reference preserves the original investigation below as background.
The user authorized a first admission-only runtime slice using a plain Cedar
file and the pinned experimental validator. Plain Cedar supersedes the earlier
JSON-bundle recommendation. See the [main document](registry-cedar-authorization.md)
for the implemented configuration and transitional restrictions. Offline inventory,
dormant revision-bound acknowledgement and explicit offline archive-and-clear are
implemented; see the [migration guide](registry-exposure-migration.md). After metadata
retirement remain offline until a runtime cutover release or an offline backup
restore. Schema 29 and a dormant internal target-bound consumption primitive now
exist; no serving path selects it. Final non-personalized serving, startup marker
consumption, cutover and runtime claims/RBAC removal below remain future work,
not completed migration functionality.
Existing service, claims, role and PostgreSQL guard code remains in force.

## Recommendation and boundary

Use `github.com/cedar-policy/cedar-go` directly in the standalone application.
Keep a small internal policy loader, verified-identity mapper and operation gate.
Do not import ToolHive's MCP authorizer or build a generic authorizer registry.
Cedar answers **whether this principal may perform this operation on this
instance**. Once permitted, the OSS service returns the same non-personalized
catalog for every principal, given the same query and catalog state.

This does not authorize individual entries, sources or views. Named registries
remain catalog views, not tenants or security partitions. A deployment needing
personalized results must keep that selection, pagination and coherent
list/get/latest behavior in its embedding application's query/service layer.
Separate deployments are another option; a named view is not an isolation boundary.

Shared models, formats, catalog persistence, ingestion and reconciliation receive
no principal, JWT, group, tenant, Cedar entity, policy or authorized-scope contract.
They neither evaluate policies nor strip legacy claims to make data reusable.
Public storage remains raw storage, not an authorization boundary. The standalone
composition root owns the gate and audit attribution; hosts own their own boundary.
See [component architecture](component-architecture.md),
[formats](registry-formats.md), [persistence](registry-persistence.md),
[jobs](registry-persistence-jobs.md) and [ingestion](registry-ingestion.md).

## Evidence and version boundary

Investigation baseline: registry commit
[`9616f158f4b0bd03d15507e5d2fd3661570f38ef`](https://github.com/stacklok/toolhive-registry-server/tree/9616f158f4b0bd03d15507e5d2fd3661570f38ef).
Its [`go.mod`](https://github.com/stacklok/toolhive-registry-server/blob/9616f158f4b0bd03d15507e5d2fd3661570f38ef/go.mod#L24)
pins ToolHive **v0.46.0**, resolved through public GitHub to
`c6c425a924fac51c86cbade15d0e720e29a600ab` (P below).
Public ToolHive `main` resolved on 2026-10-12 to
`ec3d1fe6fe1a4d05ed34d7a2cc7a8c707979b905` (M below).
Both [P's go.mod](https://github.com/stacklok/toolhive/blob/c6c425a924fac51c86cbade15d0e720e29a600ab/go.mod)
and [M's go.mod](https://github.com/stacklok/toolhive/blob/ec3d1fe6fe1a4d05ed34d7a2cc7a8c707979b905/go.mod)
select **cedar-go v1.8.0**. No dependency update is needed merely to match the
investigated evaluator version; adding a direct requirement/import is future work.
The local sibling checkout was inaccessible under investigator authority. Its
version was **not verified** and is not a source for this design.

The following links are fixed-commit evidence, not references to a moving branch.

| Area | What the implementation establishes |
| --- | --- |
| Configuration | [Config loader, P and M unchanged](https://github.com/stacklok/toolhive/blob/ec3d1fe6fe1a4d05ed34d7a2cc7a8c707979b905/pkg/authz/authorizers/config.go#L84-L166) reads JSON/YAML, retains raw config, requires nonempty version/type and dispatches validation through registered factories. It is not a registry configuration contract. [Cedar factory, P](https://github.com/stacklok/toolhive/blob/c6c425a924fac51c86cbade15d0e720e29a600ab/pkg/authz/authorizers/cedar/core.go#L99-L137) checks the Cedar block and policy count; this is not policy typechecking. |
| Construction | [P constructor and updates](https://github.com/stacklok/toolhive/blob/c6c425a924fac51c86cbade15d0e720e29a600ab/pkg/authz/authorizers/cedar/core.go#L324-L427), [M constructor and updates](https://github.com/stacklok/toolhive/blob/ec3d1fe6fe1a4d05ed34d7a2cc7a8c707979b905/pkg/authz/authorizers/cedar/core.go#L374-L476): reject zero policies, parse each policy, assign positional `policyN` IDs, optionally decode entities. No schema validation is called. Empty entity input at construction leaves an empty map; entity updates parse JSON and accept `[]`. |
| Neutral-looking method | [P `IsAuthorized`](https://github.com/stacklok/toolhive/blob/c6c425a924fac51c86cbade15d0e720e29a600ab/pkg/authz/authorizers/cedar/core.go#L459-L553), [M `IsAuthorized`](https://github.com/stacklok/toolhive/blob/ec3d1fe6fe1a4d05ed34d7a2cc7a8c707979b905/pkg/authz/authorizers/cedar/core.go#L508-L602) take string principal/action/resource identifiers, context and optional entities. Request entities override configured entities. Any diagnostic error returns false/error, even if Cedar returned Allow. The method logs context and diagnostics at debug level; copying it would expose mapped identity attributes to logs. |
| Public authorizer interface | [P interface](https://github.com/stacklok/toolhive/blob/c6c425a924fac51c86cbade15d0e720e29a600ab/pkg/authz/authorizers/core.go), [M interface](https://github.com/stacklok/toolhive/blob/ec3d1fe6fe1a4d05ed34d7a2cc7a8c707979b905/pkg/authz/authorizers/core.go) expose only `AuthorizeWithJWTClaims(ctx, MCPFeature, MCPOperation, resourceID, arguments)`. P supports tools/prompts/resources; M adds skills. Neither exposes registry operations. The constructor returns this MCP interface, not a neutral evaluator interface. |
| Entities | [Entity factory, identical at P and M](https://github.com/stacklok/toolhive/blob/ec3d1fe6fe1a4d05ed34d7a2cc7a8c707979b905/pkg/authz/authorizers/cedar/entity.go#L43-L290) offers generic-looking entity builders, but request construction adds group parents and an `MCP` server parent, plus `operation`/`name` attributes. Conversion supports records/sets, caps nesting at ten, converts floating-point values to Cedar decimals and drops unsupported values. This is not strict registry identity validation. |
| Principal and context | [M mapping](https://github.com/stacklok/toolhive/blob/ec3d1fe6fe1a4d05ed34d7a2cc7a8c707979b905/pkg/authz/authorizers/cedar/core.go#L886-L1027), [M dispatch](https://github.com/stacklok/toolhive/blob/ec3d1fe6fe1a4d05ed34d7a2cc7a8c707979b905/pkg/authz/authorizers/cedar/core.go#L1273-L1389): reads ToolHive Identity from context; missing identity or subject errors. `sub` becomes `Client` ID, without issuer in the ID. Claims become `claim_*` attributes and context keys, configured multi-valued claims also get `claimset_*` sets, arguments use `arg_*`. Group/role extraction has well-known-name fallbacks. MCP dispatch constructs actions such as `call_tool`, not registry actions. |
| Claim provenance changes | [P resolution](https://github.com/stacklok/toolhive/blob/c6c425a924fac51c86cbade15d0e720e29a600ab/pkg/authz/authorizers/cedar/core.go#L555-L652), [M resolution](https://github.com/stacklok/toolhive/blob/ec3d1fe6fe1a4d05ed34d7a2cc7a8c707979b905/pkg/authz/authorizers/cedar/core.go#L604-L881): both support an upstream-provider token and narrowly supplement name/email from the same provider's stored ID token; opaque upstream tokens fall back to request claims, malformed JWT-shaped tokens fail. M adds validator-established sessionless-grant handling and the unspoofable `thv_claim_source` attribute distinguishing request/upstream/fallback provenance. Upstream JWT parsing itself is unverified and depends on ToolHive's session trust boundary. Do not transplant it into registry bearer authentication. |
| HTTP integration | [M configuration wrapper](https://github.com/stacklok/toolhive/blob/ec3d1fe6fe1a4d05ed34d7a2cc7a8c707979b905/pkg/authz/config.go) constructs an MCP HTTP middleware. [M middleware](https://github.com/stacklok/toolhive/blob/ec3d1fe6fe1a4d05ed34d7a2cc7a8c707979b905/pkg/authz/middleware.go#L114-L245) skips non-POST requests, requires parsed MCP messages for POST, has protocol exemptions, denies unknown MCP methods and filters capability lists. It is unsuitable for REST GET/PUT/DELETE authorization. |
| Actual vMCP boundary | [P server](https://github.com/stacklok/toolhive/blob/c6c425a924fac51c86cbade15d0e720e29a600ab/pkg/vmcp/server/server.go#L396-L479), [M server](https://github.com/stacklok/toolhive/blob/ec3d1fe6fe1a4d05ed34d7a2cc7a8c707979b905/pkg/vmcp/server/server.go#L444-L527): `New` composes core + Serve. `AuthzMiddleware` is vestigial; setting only it fails construction. `Config.Authz` feeds core admission. Authz plus optimizer is rejected there. Thus middleware still exists for other ToolHive paths, but recommending that vMCP hook would be ineffective. |
| Absent versus empty | [P admission](https://github.com/stacklok/toolhive/blob/c6c425a924fac51c86cbade15d0e720e29a600ab/pkg/vmcp/core/admission.go#L55-L106), [M admission](https://github.com/stacklok/toolhive/blob/ec3d1fe6fe1a4d05ed34d7a2cc7a8c707979b905/pkg/vmcp/core/admission.go#L55-L106) allow all when config is nil, but a non-nil empty Cedar config reaches the constructor and fails. Importantly, [M incoming factory](https://github.com/stacklok/toolhive/blob/ec3d1fe6fe1a4d05ed34d7a2cc7a8c707979b905/pkg/vmcp/auth/factory/incoming.go#L105-L203) suppresses middleware for empty policies and `BuildAuthzConfig` returns nil for absent/non-Cedar/empty input. A composition path can therefore erase the distinction. Registry must not copy this normalization. |
| Admission behavior | [M admission implementation](https://github.com/stacklok/toolhive/blob/ec3d1fe6fe1a4d05ed34d7a2cc7a8c707979b905/pkg/vmcp/core/admission.go#L147-L284) filters per capability; evaluation errors skip list items, while calls return denial/error. The useful lesson is consistent enforcement at the operation boundary, not per-item filtering for registry catalogs. |

### Decisions, tests and lifecycle

Cedar-go v1.8.0 resolves to
[`cda92d0d9345fce26b36366288afd3c909fc7bd8`](https://github.com/cedar-policy/cedar-go/tree/cda92d0d9345fce26b36366288afd3c909fc7bd8).
Its [`Authorize`](https://github.com/cedar-policy/cedar-go/blob/cda92d0d9345fce26b36366288afd3c909fc7bd8/authorize.go#L16-L62)
collects policy errors, skips those policies, gives matching forbids precedence,
and otherwise allows if a permit matches. No matching permit means deny.
**Allow plus diagnostics is possible.** Registry must inspect both, as ToolHive's
concrete evaluator does, and fail the request on any diagnostic error.

ToolHive's [constructor tests](https://github.com/stacklok/toolhive/blob/ec3d1fe6fe1a4d05ed34d7a2cc7a8c707979b905/pkg/authz/authorizers/cedar/core_test.go#L39-L147)
cover missing/invalid policies and malformed entities. Its
[update tests](https://github.com/stacklok/toolhive/blob/ec3d1fe6fe1a4d05ed34d7a2cc7a8c707979b905/pkg/authz/authorizers/cedar/core_test.go#L878-L987)
assert empty/invalid policy errors and entity-update parsing, not a transactional
policy-plus-entity reload. The implementation constructs replacements before
assignment under a mutex, preserving each old component on parse failure, but
`UpdatePolicies` and `UpdateEntities` are separate swaps. These methods and the
file middleware constructor do not establish an automatic file-watcher or an
atomic whole-configuration reload contract.

[Core admission tests](https://github.com/stacklok/toolhive/blob/ec3d1fe6fe1a4d05ed34d7a2cc7a8c707979b905/pkg/vmcp/core/admission_test.go#L455-L625)
exercise nil-config allow-all, construction errors and matching list/call/lookup
decisions. [JWT tests](https://github.com/stacklok/toolhive/blob/ec3d1fe6fe1a4d05ed34d7a2cc7a8c707979b905/pkg/authz/authorizers/cedar/core_test.go#L1336-L1676)
cover opaque fallback, missing credentials and sessionless provenance;
[provenance tests](https://github.com/stacklok/toolhive/blob/ec3d1fe6fe1a4d05ed34d7a2cc7a8c707979b905/pkg/authz/authorizers/cedar/core_test.go#L4508-L4625)
cover token attempts to spoof the claim-source attribute. These are inspected
source tests, not tests executed during this investigation.

ToolHive's [M anonymous middleware](https://github.com/stacklok/toolhive/blob/ec3d1fe6fe1a4d05ed34d7a2cc7a8c707979b905/pkg/auth/anonymous.go)
explicitly synthesizes an Identity with `sub: anonymous` and `iss: toolhive-local`;
Cedar then uses the same subject-based `Client` mapping. Missing identity is not
that anonymous principal. Registry should retain this distinction but use its own
reserved anonymous UID, disjoint from issuer-qualified authenticated IDs.

### Reuse decision

The low-level `IsAuthorized` could be reached by asserting the returned interface
to ToolHive's concrete Cedar authorizer. That saves little: it brings MCP entity
conventions, ToolHive identity/session imports, global factory registration,
string identifier parsing, permissive claim conversion and context logging.
It does not solve strict schema validation, issuer-qualified identity or a registry
boundary. Extracting a neutral ToolHive helper would require cross-project work
for functionality Cedar-go already provides.

Use Cedar-go's `Policy`, `PolicySet`, typed `EntityUID`, `EntityMap`, `Request` and
`Authorize` instead. Keep registry-specific configuration and mapping internal.
No ToolHive upgrade, authorizer plugin registry, remote policy service, per-entry
policy store or generic public `User`/`Tenant`/`VisibilityPolicy` type is proposed.

Cedar-go's [versioned README](https://github.com/cedar-policy/cedar-go/blob/cda92d0d9345fce26b36366288afd3c909fc7bd8/README.md#comparison-to-the-rust-implementation)
distinguishes parsing from its **experimental** schema validator.
[`x/exp/schema/validate.Policy`](https://github.com/cedar-policy/cedar-go/blob/cda92d0d9345fce26b36366288afd3c909fc7bd8/x/exp/schema/validate/policy.go#L29-L93)
does scope and expression typechecking. Recommend pinning that same version and
using strict validation behind an internal adapter, with reference-validator
fixtures in CI before shipping. This is an explicit experimental-dependency
choice for approval, not a claim that ToolHive already validates policy schemas.
If that choice is rejected, select and package a supported validator before
implementation; do not quietly substitute parsing for typechecking.

## Proposed standalone operation boundary

**Every identifier and configuration shape below is a registry proposal, not a
ToolHive API.** Keep public HTTP serialization reusable without Cedar dependencies.
The standalone assembler supplies only guarded application operations to it.

1. Declare method, route template, operation/action and audit event together at
   registration. Use a closed action enumeration shared by route descriptors and
   the standalone operation facade. Do not infer permissions from a URL prefix,
   query parameter, request body, entry kind, HTTP verb alone or an MCP method map.
2. The standalone route registrar is mandatory, not an optional `Use(authz)` call.
   It owns authentication and creates private request evidence: either a verified
   principal or an explicitly established anonymous principal, plus route action.
   Missing evidence is a wiring failure, **not anonymous**. Catalog handlers
   cannot manufacture evidence from headers or context maps.
3. Each internal guarded operation chooses its own fixed action, checks it equals
   the registered action, and evaluates Cedar immediately before calling the
   policy-free service. Do not expose the raw service to standalone HTTP handlers.
   This second boundary prevents an alternate route or an omitted wrapper from
   invoking reads or mutations with no check. A route mislabeled `catalog.read`
   cannot call `entries.publish`. No service method accepts an arbitrary action.
4. Constructor validation rejects nil gates, duplicate/conflicting route
   declarations, unknown actions, and missing route classifications. A router
   walk/conformance test compares every mounted method/template to the manifest.
   Metadata-only handlers are evaluated in the registrar; catalog operations are
   evaluated in the guarded facade, without a second divergent policy engine.
5. Unknown URLs return 404 and unsupported methods 405 without a service call.
   An unknown action is a configuration error at startup; if encountered at runtime
   it is denied as an internal failure even under a broad permit policy. No
   catch-all route dispatches operations. HEAD aliases, if supported, use the GET
   action; OPTIONS is body-free CORS negotiation, not a catalog read. Tests cover
   slash normalization, encoded paths, method overrides and alternate mounts.
   Retired-route tombstones are explicitly classified, static error handlers with
   no service dependency, not action fallbacks.
6. Preserve a separate operational listener for health/readiness/version/metrics;
   it cannot mount catalog/admin operations. Only fixed OAuth discovery metadata
   needed to obtain credentials is a bootstrap exemption on the public listener.
   OpenAPI is classified `metadata.read`, not an arbitrary public path exemption.
   Bootstrap/probe handlers cannot receive catalog services. Admin is opt-in;
   enabling it uses the same guarded boundary on every listener, with TLS or a
   reviewed trusted TLS ingress. No new file credential or loopback listener is
   assumed approved.

This is non-bypassable for requests through the supported standalone assembly,
not a sandbox against an embedder that deliberately calls raw library storage.
Hosts replacing HTTP/services own their enforcement. Constructors for shared
components must remain usable without any standalone identity machinery.

### Proposed route/action manifest

All actions target one `Registry::Instance::"catalog"`, fixed by assembly, never
selected by a caller. No source/view/name/claims attributes are on that resource.
Action names are stable security API once released; adding one requires a route,
schema, audit and deny-by-default test change.

| Existing route family | Proposed action | Extra admission requirement |
| --- | --- | --- |
| GET `/registry/{name}/v0.1/servers`, versions and exact/latest; skills/plugins extension list/get/versions/exact/latest | `catalog.read` | Verified identity, or explicit anonymous-catalog opt-in |
| GET `/v1/sources`, `/v1/sources/{name}`, `/v1/sources/{name}/entries` | `sources.read` | Admin enabled and authenticated |
| PUT `/v1/sources/{name}` (create or update) | `sources.write` | Admin enabled and authenticated |
| DELETE `/v1/sources/{name}` | `sources.delete` | Admin enabled and authenticated |
| GET `/v1/registries`, `/v1/registries/{name}`, `/v1/registries/{name}/entries` | `views.read` | Admin enabled and authenticated |
| PUT `/v1/registries/{name}` (create or update) | `views.write` | Admin enabled and authenticated |
| DELETE `/v1/registries/{name}` | `views.delete` | Admin enabled and authenticated |
| POST `/v1/entries` (all supported kinds) | `entries.publish` | Admin enabled and authenticated |
| DELETE `/v1/entries/{type}/{name}/versions/{version}` | `entries.delete` | Admin enabled and authenticated |
| GET `/openapi.json` | `metadata.read` | Explicit Cedar permit; anonymous identity may be established |

Retire `/v1/me` and **both GET and PUT** entry-claims routes. Do not map them to
another operation. Source/view diagnostics use the appropriate read action;
future sync-trigger or other endpoints need an explicit new action decision.
There is no `superAdmin` bypass and no action meaning “all future operations”.
Policies may deliberately use an unbounded action scope, but samples should list
operations so a future action does not inherit a grant unnoticed.

The current [route registration](https://github.com/stacklok/toolhive-registry-server/blob/9616f158f4b0bd03d15507e5d2fd3661570f38ef/internal/api/v1/routes.go#L44-L98)
is the inventory, not the proposed enforcement implementation. Current
[`WrapWithPublicPaths`](https://github.com/stacklok/toolhive-registry-server/blob/9616f158f4b0bd03d15507e5d2fd3661570f38ef/internal/auth/middleware.go#L236-L255)
bypasses authentication altogether. Retire configurable `publicPaths` for
catalog/admin traffic, replacing it with typed anonymous-discovery configuration.
Reject old bypass configuration clearly rather than ignoring it. Even an injected
legacy wrapper with `publicPaths: ["/"]` must not grant access: the facade sees
no trusted request evidence and denies. Fixed bootstrap exemptions do not skip
checks for other routes. This must be tested through the real standalone router.

## Proposed identity mapping

Authentication remains separate. Reuse the existing validator integration where
it already verifies signature, allowed algorithm, exact trusted issuer, audience
and token lifetime. Preserve HTTPS/JWKS restrictions. Extend the internal result
to retain the **validating provider's trusted issuer**, not just a claim map or
operator-chosen provider display name. Require nonempty string `iss` and `sub`,
with `iss` exactly matching that trusted issuer. Ambiguous duplicate issuer
configurations with different mappings must fail startup.

Construct the principal UID with Cedar's typed constructors, never by interpolating
untrusted text into Cedar policy source. Proposed authenticated ID:
`oidc:` followed by unpadded base64url of the UTF-8 JSON array `[issuer, subject]`.
The ordered two-string encoding is unambiguous, preserves exact values, and keeps
identical subjects from different issuers distinct. Do not lowercase, URL-normalize,
or use email, preferred username, groups or a provider nickname as the ID. Base64
is encoding, not redaction; the principal identifier is sensitive audit data.

`Registry::Principal::"anonymous"` is a different reserved identity; authenticated
IDs always use the `oidc:` prefix. It has `authenticated: false`, empty issuer and
subject, and no mapped groups/scopes. A missing bearer credential becomes this
principal only on a route whose configured authentication posture explicitly
allows it. A malformed/expired/wrong-issuer bearer credential returns 401 even
on an anonymous-enabled route; never downgrade invalid credentials to anonymous.
Omitted authentication middleware does not establish anonymous evidence.

Keep the first schema small: required `authenticated`, `issuer`, `subject`;
optional `groups` and `scopes`, each `Set<String>`. Group/scopes mapping is explicit
per trusted issuer, with an exact claim key and declared representation. Groups
accept arrays of strings; scopes accept either a declared string-array form or a
declared OAuth space-separated form. No automatic `roles`/`groups` fallback,
substring membership, nested claim traversal, arbitrary token-to-entity JSON,
ID-token supplementation or upstream-token parsing is proposed. An absent optional
claim remains absent; an empty array becomes an empty set; a present malformed
mapped claim rejects the identity instead of dropping elements or coercing values.
Enforce input size/count limits. Ignore unmapped claims for policy, without logging
them. New mapped attributes require an explicit schema/config review.

Only authentication writes reserved attributes. A JWT claim named `authenticated`,
`issuer`, `subject` or a Cedar entity type cannot overwrite them. Groups are
attributes, not global group entities, so policy grants must anchor their trusted
issuer. No principal entities or policies are persisted in catalog tables.
Request context is the empty record in v1: no raw headers, body, URL, forwarded
address, mutable clock or caller-selected resource metadata drives authorization.

## Proposed schema and operator policies

This is **proposed registry schema v1** in Cedar schema syntax. It is not the
ToolHive schema. The application owns the schema and the closed action list;
operators author policies against them, not replacement schemas/entities.

```cedarschema
namespace Registry {
    entity Principal = {
        authenticated: Bool,
        issuer: String,
        subject: String,
        groups?: Set<String>,
        scopes?: Set<String>
    };
    entity Instance;
    action "catalog.read", "metadata.read",
           "sources.read", "sources.write", "sources.delete",
           "views.read", "views.write", "views.delete",
           "entries.publish", "entries.delete"
        appliesTo {
            principal: [Principal],
            resource: [Instance],
            context: {}
        };
}
```

Example A: an explicitly public catalog, with named issuer/group administrators.
This requires the separate anonymous-catalog opt-in. The policy alone cannot
turn off authentication or enable the admin router. Replace the example issuer
and group with values controlled by the deployment's trusted IdP.

```cedar
// All admitted principals may read the same catalog and its API description.
permit (
    principal is Registry::Principal,
    action in [Registry::Action::"catalog.read", Registry::Action::"metadata.read"],
    resource == Registry::Instance::"catalog"
);

// This group grants instance-wide administration, not per-entry visibility.
permit (
    principal is Registry::Principal,
    action in [
        Registry::Action::"sources.read", Registry::Action::"sources.write",
        Registry::Action::"sources.delete", Registry::Action::"views.read",
        Registry::Action::"views.write", Registry::Action::"views.delete",
        Registry::Action::"entries.publish", Registry::Action::"entries.delete"
    ],
    resource == Registry::Instance::"catalog"
) when {
    principal.authenticated &&
    principal.issuer == "https://id.example.org" &&
    principal has groups && principal.groups.contains("registry-operators")
};
```

Example B: a private catalog. Use this **instead of** Example A's public read
permit; permits are additive. Retain or replace its admin permit deliberately.

```cedar
permit (
    principal is Registry::Principal,
    action in [Registry::Action::"catalog.read", Registry::Action::"metadata.read"],
    resource == Registry::Instance::"catalog"
) when {
    principal.authenticated &&
    principal.issuer == "https://id.example.org" &&
    principal has scopes && principal.scopes.contains("registry.read")
};
```

Example C: explicit deny overrides either example's grants. This blocks one
issuer-qualified subject, not every account with the same subject string.

```cedar
forbid (
    principal is Registry::Principal,
    action,
    resource == Registry::Instance::"catalog"
) when {
    principal.authenticated &&
    principal.issuer == "https://id.example.org" &&
    principal.subject == "suspended-account"
};
```

An intentional maintenance policy is `forbid(principal, action, resource);`.
It is a valid nonempty policy, not a missing configuration. No sample wildcard
admin permit is installed automatically. Typed request entities contain only
this principal, the selected declared action and the singleton instance; callers
cannot supply an entity map that overrides those facts.

## Proposed configuration, validation and failure behavior

Require a standalone `authorization.policyFile` pointing to an absolute,
operator-controlled local file, plus explicit `admin.enabled` (default false) and
`authorization.allowAnonymousCatalog` (default false). Names are proposals.
Authentication defaults to OAuth. Anonymous-only authentication is allowed only
with admin disabled and explicit anonymous-catalog configuration. No `authz: off`
or “missing means allow-all” compatibility mode is proposed.

Use one bounded, versioned JSON policy bundle containing schema version and an
array of unique policy IDs plus Cedar text. This makes an atomic file replacement
possible without racing a directory of unrelated files. Unknown versions/fields,
duplicate IDs, blank/comment-only policy entries and zero parsed policies are
configuration errors. Schema and identity mapping are not token-controlled.
Do not add a policy-management HTTP endpoint; policy file write access is instance
security administration, separate from catalog administration.

Before mounting any catalog/admin listener: load the complete bundle; parse every
policy; strictly typecheck against the application schema; validate the configured
identity mappings and route manifest; construct immutable policy state. Bound
bundle bytes, policy count and identity attributes. Resolve policy file references
under the existing file-loading conventions and require deployment-controlled
permissions; symlink resolution alone does not establish trust. Never log policy
text, tokens, full claims, entities or raw diagnostic messages.

**Recommend restart-only policy activation for the first slice.** No watcher and
no HTTP reload. The process uses one immutable policy/schema/mapping revision.
Changing a file has no effect on that process; activation requires a controlled
restart with preflight validation. During a security-tightening rollout, drain old
replicas rather than allowing them to keep serving the old grants. Publish the
active revision in protected operational diagnostics, not as policy contents.

| Condition | Proposed result |
| --- | --- |
| Missing authorization block/path, unreadable/deleted file at startup | Fail construction; no catalog/admin listener, no allow-all fallback |
| Missing/empty/null policy list; blank or comment-only entry | Fail construction; distinguish from deliberate deny-all |
| Invalid syntax, schema/type error, unknown action or entity type, unsupported schema version or malformed mapping | Fail construction with sanitized location/category and policy ID; not raw policy or claim values |
| Valid nonempty deny-all policy | Start successfully; protected operations denied, operational probes remain available |
| No matching permit | 403; service is not called |
| Matching forbid, even with permit | 403; service is not called |
| Missing bearer on authenticated route or any admin operation | 401 with appropriate challenge; a Cedar permit cannot waive authentication |
| No bearer on opted-in anonymous catalog route | Establish anonymous evidence, then evaluate Cedar; no implicit read permit |
| Invalid supplied bearer, missing issuer/subject or malformed mapped identity attribute | 401; never retry as anonymous |
| Admin disabled | Admin routes absent; enabling requires validated authentication and policy, not an automatic admin grant |
| Any Cedar diagnostic error, evaluator failure, invalid request entities or action mismatch | Fail closed, generic 503 for evaluation unavailability (500 for wiring invariant failures); no downstream call and no raw diagnostics in response |
| Gate/evidence missing, even under `publicPaths`, alternate mount or broad permit | Fail closed before service call; constructor error when detectable, runtime 500 otherwise |
| Policy file edited/deleted while running | No live reload in v1; existing immutable revision continues. Alerting/deployment tooling must not describe the edit as activated |
| Restart with invalid replacement | New process does not serve. Old process, if deliberately retained, still enforces its old revision; it has not applied a revocation |
| Future explicit reload, if separately approved | Parse/typecheck the whole candidate before a single atomic swap of policy/schema/mapping state. Failed reload retains last good revision and emits an alert; never partly swaps, installs empty state or allows all. Each in-flight request uses one captured revision |
| No valid initial revision, including a future reload failure before initialization | Remain unready and refuse protected traffic |

A diagnostic error is not ordinary policy denial. Return the same bounded generic
error independently of which secret attribute or policy caused it. Do not return a
partially filtered page on policy failure. Authorization happens before catalog
reads, response-cache lookup, counts or mutations; denied callers cannot retrieve
permitted users' cached responses. Non-personalized data does not make permission
checks cacheable across principals. Keep conservative cache headers initially.

## Audit is separate from policy and catalog state

Preserve existing mutation event names and actor attribution. Declare the audit
event beside the route action, but do not use audit resource names as Cedar
resources. Audit admission/denial separately from operation completion: permitted
does not mean a DB mutation committed. Put denial logging outside the gate so a
rejected request is still attributable; keep existing operation wrappers for
success/failure outcomes. Record a request correlation ID, issuer-qualified actor
identifier (or explicit anonymous), action, decision/error category, policy
revision and bounded policy IDs. Actor identifiers require controlled access and
retention. Do not log JWTs, raw claims, policy text, diagnostic values or secrets
in Cedar admission events. The implemented slice does not sanitize all existing
logs: opt-in `audit.includeRequestData` still captures request bodies (including
resource claims) on operation events, even when Cedar denies the request. Keep
that option disabled unless its data exposure and sink access/retention are accepted.

Trusted background ingestion records its source/job and process attribution,
not the last HTTP caller. Reconciliation and persistence receive no identity or
policy objects, and audit does not become a catalog column. Normal source
ownership, fencing, validation and Kubernetes service-account RBAC remain intact.

## Migration: a deliberate exposure decision

An instance-wide permit is **not equivalent** to legacy visibility filtering.
Under the current [auth rules §4](../.claude/rules/auth.md#4-default-deny-when-authz-is-enabled),
**unlabeled data is default-denied too** when legacy authorization is active.
Inventory both labeled and unlabeled source/view/entry-name rows, including null,
empty-object and malformed claims, plus config and operator annotations. “No
claims found” is not evidence that an upgrade has no exposure impact.

Proposed breaking-release procedure:

1. Keep legacy guards and claims-aware writers active while implementing and
   testing the new boundary. Do not dual-run leased and legacy writers for a
   source, and do not translate old RBAC into a broad Cedar permit automatically.
2. Provide a read-only offline inventory and backup procedure. Report definitions,
   view links, every source's entries and retained snapshots, retired config/API
   use, and Kubernetes exports across the full configured scope. Include previously
   default-denied **unlabeled** records. Do not log claim values by default.
3. The operator chooses explicitly: approve the complete non-personalized catalog
   for the proposed reader population; export/migrate selected data to a host with
   its own authorization; or remain on the legacy release. Restricted subsets
   cannot be reproduced by Cedar instance actions or by renaming views. Removing
   data is a separate confirmed operator action, not automatic cleanup.
4. Stop/drain all old readers and writers, back up, and validate new policies and
   retired-field handling. Require a migration acknowledgement tied to the
   inventoried database/config revision before starting new catalog listeners or
   policy-free standalone writers. Pre-existing databases lacking that marker are
   blocked even if all claims columns are null or previous authz posture is unknown.
   Fresh empty databases take an explicit new-install path. The acknowledgement is
   standalone migration state, not a shared visibility-policy contract.
5. Only an explicit offline migration, after that decision, may archive/remove
   obsolete claims metadata. No constructor, reconciliation pass, adapter write or
   policy loader automatically strips it. Keep current public-adapter conflict
   guards until the approved migration makes their retirement safe. Preserve
   payloads, CONFIG/API ownership, source incarnation/generation, export opt-in
   and the existing offline latest-pointer/fencing migration requirements.
6. Reopen traffic only after migration and authorization checks pass. Old binaries
   must not serve the new non-personalized state. Rollback requires the retained
   backup, old configuration and a maintenance window; dropping claims columns
   is not safely reversible by recreating empty columns.

Reject retired `auth.authz`/role definitions, source/view YAML `claims`, publish
and source/view API `claims` fields (including explicit null/empty forms), and
legacy authentication bypass settings with actionable errors. Reject only the
retired security fields deliberately; do not gratuitously tighten upstream MCP
payload compatibility. Retired claims endpoints and `/v1/me` return a documented
410 tombstone during the breaking migration window, then 404; they never mutate
or expose claims. Update OpenAPI, charts and examples in the implementing slices.

The standalone operator cutover needs its own preflight and ongoing compatibility
check for `toolhive.stacklok.dev/authz-claims`. Do not route existing annotated
exports straight through the public policy-free adapter, which intentionally
ignores that annotation. Block that source's standalone cutover until the operator
has resolved it. A newly detected retired annotation must fail the standalone
snapshot attempt and alert, preserving the last good snapshot, rather than silently
removing protection or committing a partial snapshot. This check belongs to the
standalone compatibility adapter, **not** public conversion/reconciliation. The
initial migration also covers unlabeled exported records and snapshots left from
downtime. Exact `toolhive.stacklok.dev/registry-export=true` remains required.

Physical claims-column removal belongs to #915 after all readers/writers have
stopped using them. Use a new forward migration, never rewrite old migrations.
There is **no automatic claim stripping and no silent allow-all migration for
labeled or unlabeled legacy data**.

## Staged implementation and acceptance gates

The current stack ends at #927; this design does not merge, retarget or publish
anything. After approval, #912 should be reviewable slices based on that stack:

1. **Agree the contract.** Approve the security choices below and reconcile #912's
   public scope. Freeze schema/action semantics and the migration exposure gate.
   Turn the policy examples and failure matrix into isolated fixtures first.
2. **Build the internal evaluator/mapper.** Direct Cedar-go, strict validation,
   immutable revision, typed entities, issuer+subject uniqueness and sanitized
   diagnostics. No production guard removal. Test permit/forbid/no-match and
   Allow-plus-error; wrong issuer with identical subject/group; absent/empty/bad
   mappings; anonymous versus missing evidence; spoofed reserved attributes.
3. **Install the standalone boundary before removing guards.** Closed route
   manifest, guarded operation facade, admin opt-in, mandatory policy load and
   typed anonymous configuration. During transition both old and new checks must
   pass; neither broadens access. Test every consumer read and admin read/mutation
   through the real router with zero service calls on denial. Exercise legacy
   `publicPaths: ["/"]`, direct/alternate handler mounts, missing middleware/gate,
   unknown actions/routes, HEAD/OPTIONS and path normalization. Assert audit events
   and no sensitive values in errors/logs. Test metadata/probe exemptions separately.
4. **Migrate and retire claims/RBAC.** Deliver the offline inventory and explicit
   acknowledgement, config/API tombstones and operator compatibility gate before
   activating policy-free standalone writes. Remove role resolution, `WithClaims`,
   source/view/name gates, subset/visibility matchers, per-entry filtering and
   claim allocation/mismatch paths. Keep behavior and compatibility tests that
   still matter; replace claims-specific tests with boundary/migration tests.
5. **Align artifacts.** Update generated APIs safely, charts, deployment/upgrade
   guidance, audit metadata and scoped rules alongside behavior. Validate a fresh
   private install, explicit anonymous discovery, deny-all maintenance and both
   labeled/unlabeled upgrades. No release until the real-service bypass suite passes.

[#913](https://github.com/stacklok/toolhive-registry-server/issues/913) supplies
replaceable services/HTTP and whole-name source selection before search/version/
latest/pagination. Keep route descriptors operation-oriented and identity-free;
standalone supplies guarded operations, hosts supply theirs. Do not export the
legacy service or insert Cedar into `ConsumerPages`. Test all read variants and
bounded pages, including the [known contract limitations](registry-contract-tests.md).

[#914](https://github.com/stacklok/toolhive-registry-server/issues/914) assembles
those same public components behind the internal boundary. Policy/migration
preflight must precede listeners and policy-free writers. Ensure failures
propagate, admin remains opt-in, probes stay separate and shutdown drains work.
Do not resurrect a separate file-credential listener implicitly.

[#915](https://github.com/stacklok/toolhive-registry-server/issues/915) proves
external-module format-only, reconciliation-only, custom-storage and custom-service
assemblies without identity dependencies; completes cross-backend/HTTP conformance,
release notes, offline rollback and forward physical cleanup. No portable query
engine or host-personalization contract is implied by the memory backend.

Active rules still describe today's code. Change them only with implementation:
`auth.md` §§1–6, `api.md` §§2–3 and §5, `data-model.md` §§1,4,7, and `sync.md` §3
need deliberate retirement/replacement. Preserve authentication separation,
`auth.md` §7 transport security, `api.md` §§1,4,6 protocol/audit/validation,
layering, secrets handling, ownership and atomicity rules. This proposal does not
waive those current requirements before cutover.

## Decisions requiring user approval

Recommended choices, none approved by this document:

1. **Product boundary:** standalone-only instance/action Cedar; all permitted
   callers get the same catalog. No per-entry/view policy or generic framework.
2. **Deployment defaults:** mandatory valid nonempty policy bundle; OAuth default;
   anonymous catalog only by explicit opt-in plus permit; admin disabled by default
   and always authenticated when enabled, even if policy would permit anonymous.
   Prefer the existing public listener with guarded routes and TLS ingress over
   adding the unapproved file-credential/loopback design. If a separate listener
   is required, decide that topology explicitly without weakening the same gate.
3. **Validation and activation:** direct Cedar-go v1.8.0, pinned experimental strict
   validator with reference fixtures, restart-only activation initially. A required
   hot-reload contract or refusal of experimental validation changes this scope.
4. **Migration:** breaking release, offline inventory/backup and revision-bound
   acknowledgement for **all** pre-existing catalogs, including unlabeled data;
   no automatic conversion. Choose the release window and operator-run migration
   workflow before deleting guards or columns.

These are approval gates, not questions about mechanical implementation details.

## Validation performed and limits

Read the handoff, `CLAUDE.md`, all six design-rule files and the listed component,
contract, formats, persistence, jobs and ingestion documents. Inspected public
ToolHive source/tests at P and M and Cedar-go at its fixed v1.8.0 commit. Fetched
current #912 before drafting Appendix A. The eight PRs were rechecked open and
unmerged with the stated base chain; #906–#915 remained open. #927's exact head
matched this checkout, with 14 successful checks and no submitted reviews. These
checks validate the existing stack, not this design or these policy examples.

No Cedar CLI was available on the investigator's PATH. No Go was executed, no
packages installed, and no module/dependency cache modified. The requested
`jaosorior-voice` and `toolbox-dev` skills were not exposed in the confined
workspace; external skill-directory access was denied and not retried. The prose
uses direct technical language, but this is not a claim that the unavailable
writing skill was activated. No project tests, mock generation or full lint were
needed for these documentation-only changes. `codespell` passed for both changed
Markdown files. Git whitespace checks passed for the tracked architecture diff
and the new design file (checked separately with `--no-index`).

**The schema and policies have not been parsed, typechecked or evaluated by a
Cedar executable in this task.** Source-level inspection is not syntax validation.
Before implementation approval is treated as a shipping gate, run them in isolation
with the pinned parser and strict validator and check the policy decision matrix,
including anonymous, wrong issuer, missing groups/scopes, forbidden subject and
Allow-plus-diagnostic cases. Keep evaluator and reference-validator fixtures
versioned; do not change shared caches to make validation pass.

## Appendix A — DRAFT full public #912 update

**Local draft only. Not posted.** Replace the current issue body only after user
approval. Suggested title: **Replace claims/RBAC with standalone Cedar checks**.
The fetched issue currently prohibits a generic replacement authorization
framework. This draft narrows that prohibition deliberately: standalone Cedar
operation checks are allowed; a generic or identity-aware shared framework is not.

---

### Goal

Remove the bespoke resource-claims/RBAC model as part of #906. Shared registry
components serve a non-personalized catalog and carry no users, tenants, JWT claims
or visibility-policy contracts. Establish operator-authored, instance-wide Cedar
authorization in the standalone server before removing existing guards.

This is a proposed scope update pending approval of the security defaults and
migration procedure in `docs/registry-cedar-authorization.md`. Cedar is the active
proposal; the earlier loopback/file-credential admin idea was not approved.
This issue does not remove authentication, make administration anonymously writable,
or remove Kubernetes service-account RBAC for discovery and leader election.

### Scope

Use a small internal adapter over Cedar-go for standalone operation authorization.
Declare actions at route registration and enforce them at the internal
application-operation boundary so authentication bypass configuration, alternate
mounts, unknown actions and omitted middleware cannot reach raw operations.
Policies target the instance, not individual sources, views or entries. Once a
read is permitted, every principal receives the same default catalog for the same
query/state. Personalized selection and pagination belong to embedding applications.

Use verified issuer-plus-subject identities and explicit anonymous behavior.
Keep authentication and authorization separate; do not parse unverified tokens or
reuse ToolHive's MCP-specific authorizer/middleware as a registry REST boundary.
Do not create a generic authorizer plugin framework, policy database, remote policy
service, or public User, Tenant, VisibilityPolicy or authorization-scope extension.
Standalone-only Cedar is the deliberate exception to the previous blanket
“no generic authorization framework replaces the removed model” wording.

After the replacement boundary and migration gate are ready, remove role resolution,
resource-claims route/service gates, result filtering, publish claim allocation and
mismatch rules, claims-update/read endpoints, and role-bearing `/v1/me` responses.
Remove claims propagation from configuration reconciliation, synced sources and
operator annotations without silently exposing existing exports. Retain source
provenance, deterministic whole-name priority, validation, atomicity/fencing and
exact registry-export opt-in.

Stop querying/writing obsolete claims fields and remove unused matchers and
claims-only tests. Update OpenAPI, config validation, charts/examples, deployment
and migration docs, audit attribution and scoped design rules with each behavior
change. Preserve stable mutation events, useful actor attribution and transport
security. Keep policy decisions and audit identity outside shared persistence,
formats, ingestion and reconciliation.

### Proposed security defaults — require approval

- Mandatory readable, nonempty, parsed and strictly schema-validated policy bundle.
  Missing/empty/invalid configuration prevents protected serving; no allow-all fallback.
- OAuth by default. Anonymous catalog access requires explicit configuration and a
  Cedar permit. Invalid supplied credentials never become anonymous.
- Admin routes disabled by default; when enabled every admin read and mutation
  requires authenticated identity plus Cedar permission. No super-admin bypass.
- Explicit forbid wins; no matching permit denies; any evaluation diagnostic/error
  fails the request without a service call or partial response.
- Restart-only policy activation initially. Policy file editing is not live activation.
  A future reload must validate a whole candidate and swap atomically, retaining the
  last good revision on failure and alerting rather than silently widening access.
- Direct Cedar-go with a pinned experimental strict validator is proposed; approve
  that dependency posture and validate against reference fixtures before shipping.

### Migration and compatibility

Treat this as a breaking exposure change for **both labeled and unlabeled data**.
Unlabeled resources were default-denied when legacy authorization was active.
Provide an offline read-only inventory, backup and explicit operator decision:
approve the complete non-personalized catalog for its reader population, migrate
selected data to host-owned authorization, or stay on the legacy release.

Block cutover of pre-existing catalogs without an acknowledgement tied to the
inventoried database/config revision, even when all claims columns are null.
Do not automatically strip claims, generate broad permits or infer that unlabeled
means public. Drain old readers/writers and retain a backup; mixed-version serving
and legacy/leased writer co-ownership are unsupported.

Reject retired `auth.authz`/roles, source/view/publish claims fields and configurable
authentication bypass settings with actionable errors. Document temporary 410
responses for retired claims routes and `/v1/me`, followed by removal. Standalone
operator compatibility checks must block unresolved retired claims annotations
before cutover and reject later affected snapshots without clearing good data;
public operator conversion remains identity-free. Preserve export opt-in and K8s RBAC.

Only an explicitly approved offline migration may archive/remove claims metadata.
Physical column removal is deferred to #915 after all readers/writers stop using
it. Add forward migrations; do not rewrite historical migrations. Document rollback
through backup restoration, not recreation of empty claims columns.

### Stages

1. Approve schema/actions, security defaults, validation posture and exposure migration.
2. Add isolated evaluator/identity/diagnostic tests without removing legacy guards.
3. Install the mandatory standalone route and operation boundary and prove bypass resistance.
4. Deliver migration tooling/gates, then retire claims/RBAC and claims-aware propagation.
5. Align generated APIs, configuration, charts, audits, rules and upgrade documentation.

### Acceptance criteria

- [ ] User approval records the instance-wide/non-personalized boundary, defaults,
  policy validation/activation and migration decisions before guard removal.
- [ ] No shared model, catalog, persistence, ingestion or reconciliation contract
  carries identity, resource claims, Cedar entities or visibility-policy semantics.
- [ ] Standalone Cedar uses a closed route/action manifest and a guarded operation
  facade; no generic authorization framework replaces the removed model.
- [ ] All consumer reads and admin reads/mutations are tested through the real
  router, including `publicPaths`, alternate mounts, omitted middleware, unknown
  actions/routes, normalized/encoded paths and HEAD/OPTIONS behavior.
- [ ] Identity tests cover identical subjects/groups at different issuers,
  malformed mapped claims, spoofed reserved attributes and explicit anonymous identity.
- [ ] Policy tests cover missing/empty/invalid config, strict schema validation,
  default deny, forbid precedence, Allow-plus-diagnostic failure and activation lifecycle.
- [ ] Both labeled and unlabeled legacy data, retired config/API fields/endpoints
  and operator exports have an explicit tested migration with no automatic claim stripping.
- [ ] Claims/RBAC are removed across services, routes, publication and ingestion
  only after the replacement and migration gates are operational.
- [ ] Existing audit event names and safe actor attribution remain; logs/errors
  do not disclose tokens, raw claims, policies or diagnostic values.
- [ ] Documentation, chart configuration, generated APIs and scoped rules match
  implemented behavior; Kubernetes RBAC and export opt-in remain functional.
- [ ] No dependency or policy examples are presented as validated without the
  pinned parser/validator and decision tests actually running.

### Dependencies and follow-up

Depends on #907 security/compatibility agreement and the #908–#911 component work;
#927 is the current next base while the stack remains open. Coordinate with #913
service/HTTP extraction: hosts own personalization and standalone supplies guarded
operations without adding identity to public service contracts. #914 assembles the
same public components behind the fail-closed boundary. #915 completes external
embedding/conformance, breaking-release guidance and physical forward cleanup.

### Starting points

`internal/auth/`, `internal/authz/`, `internal/api/v1/`, consumer/extension route
registration, `internal/service/options.go`, `internal/service/db/claims_filter.go`,
`internal/app/`, `internal/sync/state/`, `internal/sync/writer/`,
`internal/kubernetes/controller.go`, chart configuration and
`docs/registry-cedar-authorization.md`.
