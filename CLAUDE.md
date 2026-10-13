# CLAUDE.md

Guidance for the ToolHive Registry API Server codebase.

## Design Rules

Read the matching files in [`.claude/rules/`](.claude/rules/) before changing their
scoped paths. They cover layering, data ownership, verified authentication/Cedar,
HTTP, ingestion/fencing and secrets. Cite the file and section when reviewing a
conflict. The rules now describe the active public-component architecture, not
the removed resource-claims/RBAC runtime.

## Specialized Agents

When delegation is available and authorized, use the agents in `.claude/agents/`:
`golang-code-writer` for Go implementation, `unit-test-writer` for tests,
`code-reviewer` for architecture/security/conventions, `tech-lead-orchestrator` for
complex coordination, and `security-advisor` for security decisions. Explicit
operator instructions control delegation and publication.

## Project Overview

See [README.md](README.md) and [standalone assembly](docs/registry-standalone-assembly.md).
The standalone binary serves a nonpersonalized MCP/skills/plugins catalog using the
same public components available to embedders. OAuth verifies identity; operator-
authored Cedar admits whole-instance actions. Named registries are ordered views,
not tenant/privacy boundaries. Shared registry contracts contain no identity or
resource claims. Hosts own personalized selection and admission when embedding.

## Architecture

```text
cmd/thv-registry-api/          CLI, migration commands and standalone wiring
pkg/registry/
  model/ formats/             Identity/storage-independent payloads and validation
  persistence/                Definitions, atomic entries, diagnostics, jobs/fencing
  catalog/                    Small reader/admin/publisher/diagnostic capabilities
  httpapi/                    Host-selected consumer/admin routers and serializers
  ingest/ reconcile/          Git/API/file fetching and leased reconciliation
  operator/                   Optional policy-free Kubernetes conversion/runner
  postgres/                   Explicit PostgreSQL implementation adapter
internal/
  app/ storage/               NewCatalogApp preparation, ownership and DI
  api/                        Cedar classifier, audit/tombstones, internal probes/OpenAPI
  auth/ authorization/        Verified JWT/introspection, identity projection and Cedar
  config/                     YAML/env, secrets, retired-presence/offline compatibility
  db/                         SQL plumbing, latest repair, exposure inventory/receipts
  sync/standalone/            Leased worker supervision and lifecycle
  sync/writer/                Caller-owned transactional payload COPY/upsert machinery
  kubernetes/                 Standalone client and retired-export preflight
  audit/ telemetry/           Safe attribution and observability
  git/ httpclient/ filtering/ Low-level shared ingestion helpers
 database/migrations/        Historical numbered SQL migrations
```

`app.NewCatalogApp(ctx, CatalogOptions{...})` enforces final config, frozen policy,
open, read-only historical/source/operator preflight, schema-29 exposure consumption,
then atomic definition reconciliation/latest repair and guarded HTTP. Constructors
start no application work; Start/Wait/Stop own listeners/workers and join inline
work before closing owned resources. Inject CatalogFactory, operator, fetcher and
reconciliation capabilities rather than recreate a monolithic RegistryService.
Public (`:8080`) and internal (`:8081`) servers stay separate. Admin is absent by
default; enabled admin requires OAuth. Internal probes/metrics have no auth/audit.

### Compatibility boundary

The old app, service, routers, claims matcher/roles and unfenced sync/controller
runtime are removed. Private config structures/raw history and version-1 fingerprint
JSON remain unchanged because schema-29 pending receipts bind them. Claims columns,
archives, receipts and mutation guards remain until the final forward schema slice.
Never rewrite historical migrations, infer consent from empty data, silently clear
claims or let old/new binaries co-own sources. See the
[offline migration guide](docs/registry-exposure-migration.md).

## Development and Tests

- `task build`: build the binary.
- `task gen`: regenerate mocks and sqlc; **always run before tests**.
- `task lint-fix`: preferred linting with fixes (not `task lint`).
- `task test`: tests; direct `go test -race -tags integration ./...` also exercises
  integration-tagged acceptance with isolated envtest assets and
  `USE_EXISTING_CLUSTER=false`. Never target a user cluster.
- Table-driven tests live beside source; mock external dependencies using
  `go.uber.org/mock` / `//go:generate mockgen`, never hand-write generated mocks.
- Public external-module fixtures prove importability and forbidden dependency
  graphs. Expected HTTP wire fixtures and route inventories must be independent of
  production DTOs/serializers. See [coverage mapping](docs/registry-contract-tests.md).
- Current real HTTP/Cedar/security acceptance lives in CLI/app/API/auth/authorization
  tests, not the removed `internal/authz` claims suites.
- Add queries to `database/queries/*.sql`, regenerate sqlc with `task gen`, and
  consume privately from the explicit PostgreSQL adapter.
- OpenAPI is generated from active public HTTP adapters and standalone metadata/
  tombstones. Use the pinned Swag revision in Taskfile and compare generated schema
  parity. Do not run recursive-deletion documentation commands for workspace cleanup.

## Code Style

Public methods before private methods. Interfaces where consumed, implementations
beside them. Keep API → catalog capabilities → persistence contracts direction.
Validate inputs at trust boundaries, preserve exact payload/version metadata,
propagate classified errors without leaking credentials or raw backend causes.
Standalone SourceConfig.GetType and public SourceDefinition.Kind infer exactly one
source block. Whole-name source selection precedes search/version/latest/pagination.

## Commit Message Style

No conventional commit prefixes. Imperative subject, max 50 characters, backtick-
quote identifiers/code names. Blank line, then paragraph body explaining what/why.
End with an issue reference when applicable (`Fixes #123`, `Improves on #444`).
Do not commit or publish without operator authorization.
