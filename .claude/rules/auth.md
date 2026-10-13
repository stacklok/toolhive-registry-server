---
paths:
  - "internal/auth/**/*.go"
  - "internal/authorization/**/*.go"
  - "internal/config/**/*.go"
  - "internal/api/**/*.go"
---

# Authentication and Standalone Authorization

## 1. Verified Authentication Is Separate From Admission

OAuth JWT/JWKS or introspection establishes verified issuer/subject and attributes.
Anonymous catalog identity requires explicit opt-in. A context claim map, actor header,
role claim or missing middleware is not authentication evidence. Preserve OAuth challenges,
provider configuration, TLS and credential validation. Ordinary access/admission/audit logs
project only hashed verified identity, never raw tokens, claims, names or policy diagnostics.

## 2. Fixed-Action Cedar, Not RBAC

Standalone requires frozen, strictly validated nonempty Cedar and a closed action set.
Admin is absent by default and always requires OAuth when enabled. Anonymous catalog
access requires both explicit configuration and a Cedar permit. No implicit roles,
`superAdmin` bypass or automatic translation of old role rules into permits exists.
Evaluator errors fail closed; a permit with diagnostics is not successful authorization.
Public components remain identity-free and return nonpersonalized pages/cursors.

## 3. Evidence at Every Operation

The mandatory registrar checks actual method/template/domain-operation/action/audit
classification and every guarded capability checks its own private matching evidence.
A broad permit cannot authorize omitted middleware, alternate mounts or mislabeled calls.
`/v1/me` and GET/PUT entry-claims endpoints are authenticated audited static 410 tombstones,
not calls to a retained monolith. The sole unauthenticated public bootstrap is the fixed
OAuth protected-resource metadata GET. Never restore publicPaths matching/bypass code.

## 4. Historical Default-Deny and Exposure Compatibility

Legacy resource-claim matching, role enforcement and personalized service are retired.
Historically unlabeled data was also default-denied with authz enabled; null claims are
not proof of consent to nonpersonalized exposure. Keep schema-29 inventory, digest-bound
acknowledgement, archives, explicit retirement and serialized receipt consumption.
Missing/stale/pending unretired evidence blocks startup before reconciliation or repair.
`--initialize-empty` authorizes only a virgin entirely empty catalog after preflight;
it never approves existing data or resets/rebinds evidence.

Private config compatibility fields/types and raw retired-key/history classification
remain for offline tooling and version-1 fingerprint serialization. Their exact JSON shape
binds pending receipts: do not remove/rename fields or change serialization in runtime
cleanup. The current serve path rejects retired authz/roles/publicPaths and resource claims
by presence, including null/empty aliases/environment. Standalone operator preflight rejects
retired export annotations before conversion. No active matcher or claim-stamping returns.
Schema 30 removes live claims columns only after explicit offline finalization at 29;
ordinary writes require clean 30. Preserve historical migrations, archives and receipts;
never infer consent or rewrite version-1 fingerprint serialization.
See `docs/registry-exposure-migration.md` and `docs/registry-standalone-assembly.md`.

## 5. Frozen Preparation

Bind exposure review to the actual `Engine.Revision`, never reread policy bytes after
loading. Read-only historical sources and authoritative full-scope operator preflight
precede schema-29 consumption, writes or serving. Old and new binaries must not co-own
sources: stop/drain all historical readers/writers before offline review and cutover.

## 7. OAuth Issuer URLs Require HTTPS

JWKS over HTTP defeats token validation. Issuer URLs require HTTPS except localhost /
127.0.0.1 / ::1 or explicit env-only `THV_REGISTRY_INSECURE_URL=true` for development.
The insecure flag cannot be read from YAML. Preserve propagation to token validators.
