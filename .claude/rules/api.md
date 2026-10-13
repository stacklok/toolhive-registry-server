---
paths:
  - "internal/api/**/*.go"
  - "pkg/registry/httpapi/**/*.go"
---

# API Surface

## 1. Upstream Consumer, ToolHive Extensions, Optional Admin

Consumer routes remain `/registry/{registryName}/v0.1/servers` and version routes;
skills/plugins live under `/x/dev.toolhive/`. Preserve upstream/extension wire shapes,
named prefixes, route-specific version bounds and existing continuation behavior.
Public HTTP consumes host-selected capabilities, not standalone config/auth or sqlc.
Admin `/v1` uses small independent source/view/publisher/diagnostic operations.

## 2. Global Claims-Free Publication

`POST /v1/entries` is the global publish endpoint for exactly one server/skill/plugin
payload. Publish into the singleton managed source, never per-view or external storage.
Reject top-level claims by presence, including escaped/case-folded keys and null values;
ordinary nested payload extension keys named claims are not resource authorization.
Validate duplicate keys, body bounds, payload shape and names before service calls.

## 3. Nonpersonalized Pages

Standalone permitted readers receive the same view selection and cursor. HTTP must not
filter, deduplicate, reorder, truncate or refill the supplied page. Counts describe that
page, not a guessed global total. Personalized selection/caching belongs to a host service.

## 4. Mandatory Standalone Admission and Auditing

`api.NewCatalogServer` validates every actual descriptor against the closed method/template/
domain-operation/action/audit manifest. Every capability checks private verified identity
and matching fixed-action evidence. No old routers, monolithic facade or role middleware.
Admin is opt-in and authenticated. `/v1/me` and GET/PUT claims are audited authenticated
static 410 tombstones. Operational probes/metrics stay only on the internal server.
Committed callbacks determine truthful create/update audit events, including 503 committed
responses; never infer committed mutation solely from status. Keep stable audit event names,
hashed verified attribution, request logging and transport/security headers.

## 5. Whole-Name Source Priority

The earliest view source wins all versions of `(kind,name)` before search/version/latest/
pagination. No claims-based promotion of a lower source. Admin diagnostics retain
unshadowed source/version data rather than consumer deduplication.

## 6. Trust-Boundary Validation

Validate paths, queries, payloads and supported cursor/limits in public HTTP helpers.
Malformed input is 400 before backend calls. Unknown/mismatched cursors are rejected;
version history routes retain fixed bounds and ignore unsupported limit/cursor parameters.
Tests use fixed claims-free wire fixtures and explicit route inventories, not legacy DTOs
or production-generated expected-response oracles. OpenAPI comes from active public DTOs,
metadata handlers and protected tombstones; use the pinned Swagger generator safely.
