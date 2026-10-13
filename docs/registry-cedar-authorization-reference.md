# Standalone Cedar authorization reference

[Back to the Cedar configuration guide](registry-cedar-authorization.md).

This reference defines the standalone admission boundary implemented by
`internal/authorization`. For policy-file configuration, examples, deployment and
migration guidance, use the configuration guide.

## Admission model

Cedar permits an operation on the singleton
`Registry::Instance::"catalog"`; it does not select or filter entries, sources,
or named views. A successful `catalog.read` admission therefore returns the same
nonpersonalized result for a given view, query and catalog state. Embedders own
any personalized selection and admission they require.

The standalone server registers every public route with a closed action and
requires verified route evidence before the guarded catalog capability calls a
service. An absent or inconsistent evidence record is an internal failure, not
an anonymous request. Admin routes additionally require enabled admin and an
authenticated identity. The only anonymous-eligible actions are `catalog.read`
and `metadata.read`; an invalid credential is never converted to anonymous.

## Route actions

The closed action set is:

| Action | Route family |
| --- | --- |
| `catalog.read` | Consumer server, skill and plugin GET routes under `/registry/{registryName}/v0.1` |
| `metadata.read` | `GET /openapi.json` |
| `sources.read`, `sources.write`, `sources.delete` | Admin source GET, PUT and DELETE routes under `/v1/sources` |
| `views.read`, `views.write`, `views.delete` | Admin registry-view GET, PUT and DELETE routes under `/v1/registries` |
| `entries.publish` | `POST /v1/entries` |
| `entries.delete` | `DELETE /v1/entries/{type}/{name}/versions/{version}` |

The [admission manifest](../internal/api/admission.go) is the exhaustive mapping,
including nested consumer and diagnostic routes, retired authenticated tombstones,
and audit classifications. The [action constants](../internal/authorization/policy.go)
and [embedded schema](../internal/authorization/schema.cedarschema) are the
source of truth for action names and Cedar types.

`GET /.well-known/oauth-protected-resource` is a fixed credential-bootstrap
endpoint, not a Cedar action. Internal probes and metrics are served separately
and do not receive catalog capabilities.

## Principal and request schema

Each evaluation has:

- a `Registry::Principal` principal;
- a `Registry::Action` from the closed action set;
- `Registry::Instance::"catalog"` as its resource; and
- an empty context record.

The principal has required `authenticated`, `issuer` and `subject` attributes;
optional `groups` and `scopes` are `Set<String>`. Authenticated principal IDs are
`authenticated:` plus the SHA-256 digest of the JSON tuple `[issuer, subject]`.
The anonymous principal ID is `anonymous`. These IDs distinguish identical
subjects from different issuers without exposing issuer or subject values in the
principal ID.

Authentication supplies issuer and subject. For authenticated callers, their
verified claims must contain identical `iss` and `sub` values, and claims named
`authenticated`, `issuer`, or `subject` reject the identity rather than
overwriting those attributes. Configured per-issuer mappings may project only
`groups` and `scopes`. `string-array` accepts an array of strings;
`space-delimited` accepts one space-separated string. Missing mapped claims add
no attribute; malformed or oversized mapped values reject authentication.
Unmapped claims, request headers, request bodies and route parameters are not
Cedar attributes.

## Evaluation and failures

The policy engine loads immutable policy bytes at startup. A custom file replaces
the built-in policy entirely. The parser and strict schema validator reject an
empty, syntactically invalid or schema-invalid policy before serving. The engine
uses cedar-go v1.8.0 and rejects an evaluation when Cedar returns any diagnostic,
even if its decision is Allow.

| Condition | HTTP result |
| --- | --- |
| Missing or invalid credentials where authentication is required | 401 |
| Invalid mapped identity attributes | 401 |
| No matching permit or a matching `forbid` | 403 |
| Evaluator diagnostic or unavailable evaluator | 503 |
| Missing, inconsistent or unknown route/action evidence | 500 |

No matching permit is deny-by-default, and a Cedar `forbid` overrides permits.
Admission failure occurs before the guarded catalog operation calls its backing
service. Policy revisions are SHA-256 identifiers of the loaded bytes; the
engine does not watch or reread policy files, so policy changes require restart.
