# CLAUDE.md

This file provides AI assistant guidance for working with the ToolHive Registry API Server codebase.

## Design Rules

Scoped design rules live in [`.claude/rules/`](.claude/rules/). Each file has a `paths:` frontmatter glob and is auto-loaded when Claude Code is working on files that match. They cover layering, the data model, auth/claims, the API surface (admin vs upstream spec), the sync pipeline, and secrets handling. Read the rule file that matches the area you're changing, and cite it by section when reviewing a PR that conflicts with one (e.g. "this violates `auth.md` §3 — uniform claim matching").

## Available Subagents

Registry Server uses specialized AI subagents for different aspects of development. These agents are configured in `.claude/agents/` and MUST be invoked when you need to perform tasks that come under their expertise:

### Core Development Agents

- **golang-code-writer**: Expert Go developer for writing clean, idiomatic Go code. Use when creating new functions, structs, interfaces, or complete packages.

- **unit-test-writer**: Specialized in writing comprehensive unit tests for Go code. Use when you need thorough test coverage for functions, methods, or components.

- **code-reviewer**: Reviews code for Registry Server best practices, security patterns, Go conventions, and architectural consistency. Use after significant code changes. Should cross-check changes against the matching rule file under `.claude/rules/`.

- **tech-lead-orchestrator**: Provides architectural oversight, task delegation, and technical leadership for code development projects. Use for complex features or architectural decisions.

### Support Agents

- **security-advisor**: Provides security guidance for coding tasks, including code reviews, architecture decisions, and secure implementation patterns.

### When to Use Subagents

Invoke specialized agents when:
- You need expertise in their specific domain (e.g., writing code, reviewing code)
- Writing new code (use golang-code-writer)
- Creating tests (use unit-test-writer)
- Orchestrating the different tasks to other subagents that are required for completing work asked by the user (use tech-lead-orchestrator)
- Reviewing code that is written (use code-reviewer)

The agents work together - for example, tech-lead-orchestrator might delegate to golang-code-writer for implementation and then to code-reviewer for validation, then finally to security-advisor for a security check of any implementation details.

## Project Overview

See [README.md](README.md) for complete project documentation including features, API endpoints, configuration, and deployment.

**Quick Summary**: A standards-compliant MCP Registry API server that provides REST endpoints for discovering MCP servers and skills. Ingests data from Git, API, file, and Kubernetes sources into a shared entry pool, and exposes consumer views through claim-scoped registries.

## Essential Build Commands

See [README.md](README.md#development) for complete build and development documentation.

**Most commonly used:**
- `task build` - Build the binary
- `task lint-fix` - **Preferred** linting (auto-fixes issues)
- `task test` - Run tests
- `task gen` - Generate mocks (run before testing)
- `task all` - Lint, test, and build

## Code Architecture

High-level shape (read `.claude/rules/layering.md` for the rules that govern layer boundaries):

```
cmd/thv-registry-api/      # Entrypoint only: main.go, app wiring, generated OpenAPI docs
internal/
  api/                     # HTTP layer — Chi routers and handlers
    v1/                      # Admin API (/v1/sources, /v1/registries, /v1/entries, /v1/me)
    registry/v01/            # Upstream MCP Registry v0.1 consumer API (/registry/{name}/v0.1/...)
    x/skills/                # Consumer extension: /x/dev.toolhive/skills
    common/                  # Shared request/response helpers
  app/                     # Application assembly and lifecycle (builder + functional options)
    storage/                 # storage.Factory — DI seam for all DB-backed components
  service/                 # Business logic — RegistryService interface, claim checks, dedup, cursors
    db/                      # Postgres-backed service implementation and claims filter
  sources/                 # Ingestion handlers — RegistryHandler, RegistryHandlerFactory (git, api, file)
  sync/                    # Sync orchestration — Manager, Coordinator, writer (serializable txn)
  kubernetes/              # K8s controllers — MCPServer / VirtualMCPServer / MCPRemoteProxy CRDs
  auth/                    # Authentication (JWT middleware, validators, OAuth config)
  authz/                   # Authorization integration tests (role + claim enforcement)
  audit/                   # Audit logging middleware (wraps all /v1 handlers)
  config/                  # YAML config loader and validation (viper)
  db/                      # Low-level DB plumbing, migrations runner, sqlc-generated code
  filtering/               # Name/tag filter implementation (used during ingestion)
  telemetry/               # OpenTelemetry setup (metrics + tracing)
database/migrations/       # SQL migrations (numbered, up/down)
docs/                      # Developer documentation — configuration, deployment, etc.
.claude/rules/             # Scoped design rules, auto-loaded by Claude Code per paths glob
```

The app is assembled with a builder + functional options pattern (`app.NewRegistryApp(ctx, opts...)`) so tests can inject fakes for storage, sync manager, registry handler factory, and coordinator options. Two HTTP servers are started: public (default `:8080`) and internal (`:8081` — health/readiness/version only, no auth).

### Key Patterns for AI Development

- **Builder + functional options**: Wire new components through `internal/app/builder.go` using `With*` options. Factory interfaces preferred over direct dependencies — tests need injection points.
- **Layered flow**: API handlers call `service.RegistryService`, which calls storage and source interfaces. API must not call DB or `sources.*` directly. Service must not import `net/http` or `client-go`.
- **Source type inference**: `SourceConfig.GetType()` is the single source of truth for which source type a config represents. No `type:` field — exactly one of `git/api/file/managed/kubernetes` blocks must be set.
- **Record filters**: Claims filtering and dedup are composed as `RecordFilter` chains in `internal/service/db/`, applied during row streaming. No SQL-level JSONB operators — see `.claude/rules/data-model.md` §7.
- **Table-driven tests**: Standard pattern throughout. Mock HTTP clients, filesystem, Git operations.
- **Mock generation**: Use `go.uber.org/mock` via `//go:generate mockgen` directives. Run `task gen` to regenerate — never hand-write mocks.

## Testing Guidelines

- **Always run `task gen` before testing** to regenerate mocks
- Tests are located alongside source files (`*_test.go`)
- Use the table-driven test pattern for comprehensive coverage
- Mock external dependencies (HTTP clients, filesystem, Git operations, K8s client)
- Integration-style authz tests live in [internal/authz/](internal/authz/) and exercise the full stack with different auth postures (anonymous, oauth-no-authz, oauth+authz)
- Service-layer mocks: [internal/service/mocks/](internal/service/mocks/)
- Source handler mocks: [internal/sources/mocks/](internal/sources/mocks/)
- Sync mocks: [internal/sync/mocks/](internal/sync/mocks/) and [internal/sync/writer/mocks/](internal/sync/writer/mocks/)

## AI Assistant Best Practices

### Code Style
- **Public methods first, private methods last** within a Go file
- Interfaces defined where consumed, implementations beside them
- Respect layer direction: API → service → storage/sources (never the other way)

### Workflow
1. **Linting**: Always use `task lint-fix` (not `task lint`)
2. **Testing**: Run `task gen` before testing so mocks match interfaces
3. **Commits**: Follow the `## Commit Message Style` section below

### Common Tasks

**Adding a new admin API endpoint:**
1. Add handler in [internal/api/v1/](internal/api/v1/) (e.g., `routes.go` for wiring, plus a dedicated handler file)
2. Wrap with the appropriate `auditmw.Audited*` helper and put it inside the correct role group (`RequireRole(auth.RoleX, authzCfg)`) — `.claude/rules/api.md` §4
3. Add Swagger annotations on the handler
4. Run `task docs` to regenerate the OpenAPI spec
5. Add unit tests (handler-level) plus an authz integration test in [internal/authz/](internal/authz/) if the endpoint is claim-scoped

**Adding a new source type:**
1. Add the type constant in [internal/config/config.go](internal/config/config.go) and a new config struct
2. Update `SourceConfig`, `GetType()`, `validateSourceTypeCount`, `validateSourceSpecificConfig`, and `IsNonSyncedSource` if appropriate — `.claude/rules/data-model.md` §2
3. Implement `sources.RegistryHandler` (if synced) or wire a controller (if non-synced, like Kubernetes)
4. Register in [internal/sources/factory.go](internal/sources/factory.go)
5. Add a DB migration if new columns are needed
6. Run `task gen`, then add table-driven tests and a round-trip config-load test

**Adding a new DB query:**
1. Add the SQL in `database/queries/*.sql` with a `-- name:` comment for sqlc
2. Run `task gen` to regenerate sqlc bindings
3. Consume from `internal/service/db/` — do not reach into sqlc types from the API layer

### Key Dependencies
- **Web**: [go-chi/chi](https://github.com/go-chi/chi) router
- **CLI**: Cobra + Viper
- **Postgres**: `pgx/v5` (driver) + `sqlc` (typed queries) + `golang-migrate` (migrations)
- **Git**: `go-git` (public repos over HTTPS)
- **K8s**: `controller-runtime` + `client-go`, scoped to `internal/kubernetes/**`
- **Auth**: `go-jose` / OIDC JWKS for JWT validation
- **Testing**: `go.uber.org/mock`, `testcontainers-go` (real Postgres in integration tests)
- **Docs**: swaggo/swag generates OpenAPI from code annotations

## Commit Message Style

- No conventional commit prefixes (`feat:`, `fix:`, `chore:`, etc.)
- Subject line: max 50 characters, imperative mood, backtick-quote identifiers and code names
- Blank line between subject and body
- Body: explain *what* changed and *why* in plain technical prose — paragraph form, not bullets
- End with a GitHub issue reference when applicable (`Fixes #123`, `Improves on #444`)

Example:

```
Improve `GetServerVersion`/`GetSkillVersion` query performance

Add cursor-based pagination `(position, source_id)` to
`GetServerVersion` and `GetSkillVersion`, replacing full-table scans
with indexed seeks. Add two supporting indexes to eliminate nested-loop
seq scans.

Improves on #444
```
