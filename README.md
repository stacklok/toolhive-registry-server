<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/images/toolhive-byline-white.svg">
  <img src="docs/images/toolhive-byline-black.svg" alt="ToolHive logo" width="500"/>
</picture>

<br>

[![Release][release-img]][release] [![Build status][ci-img]][ci]
[![Coverage Status][coveralls-img]][coveralls]
[![License: Apache 2.0][license-img]][license]
[![Star on GitHub][stars-img]][stars] [![Discord][discord-img]][discord]

# ToolHive Registry Server

**Discover, govern, and control access to MCP servers, skills, and plugins across your organization**

The ToolHive Registry Server aggregates MCP servers, skills, and plugins from Git repos, Kubernetes clusters, upstream registries, and internal APIs into named catalogs. OAuth verifies identity and Cedar grants whole-instance operations. Standalone evaluates built-in allow-all Cedar by default; an optional custom file replaces it entirely. Every verified identity can use every enabled operation, including admin if explicitly enabled (admin is off by default). Permitted readers of the same view receive the same catalog; views are not privacy boundaries. Use separate instances or host-owned personalized services when audiences must see different data.

It implements the official [Model Context Protocol (MCP) Registry API specification](https://modelcontextprotocol.io/development/roadmap#registry). If you're not familiar with MCP: it's an open protocol that lets AI assistants connect to external tools and data sources. This server is the governance layer that sits between your MCP infrastructure and the teams consuming it.

**Why ToolHive Registry Server?** Most MCP setups start with a flat list of servers and no access control. As adoption grows, you need to govern who sees what, aggregate servers from multiple sources, and audit access -- without locking into a proprietary registry. The ToolHive Registry Server is open source, implements the official MCP Registry API spec, and plugs into your existing identity provider, combining whole-instance Cedar authorization, multi-source aggregation, and SIEM-compliant audit logging in a single service.

[Learn more about the ToolHive platform →](https://stacklok.com/platform)

---

## Table of contents

- [Features](#features)
- [Quickstart](#quickstart)
- [Core Concepts](#core-concepts)
- [API Endpoints](#api-endpoints)
- [Configuration](#configuration)
- [The ToolHive Platform](#the-toolhive-platform)
- [Embedding](#embedding)
- [Documentation](#documentation)
- [Contributing](#contributing)

## Features

### Governance and access control

- **Cedar admission**: Evaluated built-in allow-all after authentication; optional custom policies replace it entirely to restrict consumer read, publication and administration. No per-entry/view claim filtering.
- **OAuth 2.0/OIDC authentication**: Plug in your existing identity provider (Okta, Auth0, Azure AD). OAuth is the default; anonymous mode is available for development.
- **Explicit administration**: Admin is disabled by default; enabled endpoints always require authenticated identity and a matching Cedar action permit.
- **SIEM-compliant audit logging**: Structured log covering all API operations with NIST SP 800-53 AU-3 compliant fields, dedicated file output, and configurable event filtering.

### Aggregate from anywhere

- **Five source types**: Pull entries from Git repos, upstream MCP registries, local files, Kubernetes clusters, or publish them via the Admin API.
- **Compose catalogs from multiple sources**: Each registry aggregates sources in order of precedence. Views are catalog compositions, not privacy or authorization boundaries.
- **Background sync**: Sources are polled on configurable intervals with retry logic. Registries stay current without manual intervention.

### Kubernetes and observability

- **Kubernetes-native discovery**: Export-opted-in CRDs are ingested through fenced leased writers over the configured namespace scope. Retired claims annotations block preflight.
- **OpenTelemetry**: Distributed tracing and metrics out of the box.
- **Standards-compliant**: Implements the official MCP Registry API specification. Clients that speak the spec work out of the box.
- **PostgreSQL backend**: Guarded forward migrations, with explicit offline review/finalization for existing catalogs.

## Quickstart

### Prerequisites

- Go 1.26 or later (for building from source)
- [Task](https://taskfile.dev) for build automation
- PostgreSQL 16+

### Build and run

```bash
# Build the binary
task build

# After configuring PostgreSQL and trusted OAuth (custom Cedar mount optional):
# Existing catalog: complete the linked offline procedure before serving.
./bin/thv-registry-api serve --config examples/config-git.yaml

# New, entirely empty database only (explicit opt-in):
./bin/thv-registry-api serve --config examples/config-file.yaml --initialize-empty
```

Configure trusted OAuth and PostgreSQL in the selected example first. Omit
`authorization.policyFile` for built-in allow-all Cedar, or explicitly mount a custom
restriction read-only (invalid specified files fail startup, never default fallback).
Anonymous catalog access requires explicit opt-in; invalid credentials never become
anonymous. Existing catalogs require the
[offline exposure procedure](docs/registry-exposure-migration.md); even unlabeled
data is not an empty install. Then start with `thv-registry-api serve --config config.yaml`.
Only for a genuinely new, entirely empty database, explicitly add `--initialize-empty`.
It grants no policy permission and never approves existing data.

The public listener defaults to `:8080`; probes/metrics use the separate `:8081`.

### Docker quickstart

Configure your OAuth issuer/audience in `examples/config-docker.yaml`. No external
policy mount is required by default; see [configuration](docs/configuration.md) for an
optional custom policy override. The base Compose configuration has no implicit
initialization or anonymous grant:

```bash
docker compose up --build
# New, entirely empty database only; explicit empty-install authorization:
docker compose -f docker-compose.yaml -f docker-compose.initialize-empty.yaml up --build
```

Neither invocation deletes existing volumes. Keep production policy/config mounts
read-only and use TLS or trusted TLS ingress.

## Embedding

Import only the public components you need from `pkg/registry`: models/formats,
persistence, catalog, ingestion/reconciliation, HTTP and optional PostgreSQL/operator
adapters. Hosts own admission, personalized selection, pools/clients, listeners and
shutdown. Hosts own the authorizer lifecycle; it is borrowed and shared across
requests, so it must be concurrency-safe and honor request-context cancellation.
Cedar is optional for libraries: `httpapi.Authorizer` takes host context and an existing
`RouteDescriptor`; use `httpapi.Authorize(authorizer)` as the existing `Decorate`
seam after host authentication `Middleware`. Nil, deny, errors and unknown or
mismatched routes fail closed before application calls. Undecorated low-level
routers are explicitly policy-free. The [real host-authentication/non-Cedar example](pkg/registry/httpapi/testdata/external/authorization_test.go)
covers consumer and admin without internal authentication or policy dependencies.
`internal/app` is the Cedar-only standalone composition, not a public host framework.

[Embedding guidance and executable examples](docs/registry-embedding-validation.md)
include a combined separate-module PostgreSQL → ingestion → catalog → HTTP workflow:

```sh
task gen
GOWORK=off go test -race -count=1 ./pkg/registry/postgres -run '^TestExternalCompleteAssembly$'
```

The test prepares a disposable schema-30 database; it does not use a deployment
DSN or target your cluster. See [consumer reads](docs/registry-consumer-reads.md) for
selection, version ordering and cursor contracts, and the
[offline migration guide](docs/registry-exposure-migration.md) for existing-data
review, backups and rollback restrictions.

## Core concepts

### Sources and registries

Most organizations have MCP servers, skills, and plugins in more than one place -- a public catalog, an internal Git repo, a Kubernetes cluster running live instances. The Registry Server models this with two primitives: **sources** (where entries come from) and **registries** (what consumers query).

A **source** is a connection to where MCP server, skill, and plugin entries live. It tells the server "go look here for entries." Sources come in five types:

| Type           | What it does                        | Example                                                      | Sync         |
| -------------- | ----------------------------------- | ------------------------------------------------------------ | ------------ |
| **API**        | Pulls from an upstream registry API | The official MCP Registry at registry.modelcontextprotocol.io | Auto         |
| **Git**        | Clones entries from a Git repo      | A version-controlled internal catalog                        | Auto         |
| **File**       | Reads from the local filesystem     | A curated `registry.json` on disk                            | Auto         |
| **Managed**    | Entries published via the Admin API | Dynamically registered internal servers                      | On-demand    |
| **Kubernetes** | Discovers deployed MCP servers      | Servers running in your K8s clusters                         | On-demand    |

A **registry** is a named catalog that aggregates one or more sources into a single consumer-facing endpoint. Each registry aggregates sources into an organizational view, not a separate access-control boundary. Sources are listed in order -- when the same entry appears in multiple sources, the first source in the list wins.

**Why they are separate**: Sources and registries have a many-to-many relationship. One source can feed multiple registries, and one registry can pull from multiple sources. This lets you compose different catalogs for different audiences from the same underlying data:

```text
Source: "official-catalog"  ──┐
Source: "internal-tools"    ──┼──> Registry: "production"  (curated, vetted)
                              │
Source: "internal-tools"    ──┼──> Registry: "dev-team"    (everything)
Source: "k8s-deployed"      ──┘
```

In this example, the "production" registry only exposes vetted entries from the official catalog and internal tools, while the "dev-team" registry includes everything plus live Kubernetes-discovered servers.

**Configuration example:**

```yaml
sources:
  - name: official-catalog
    api:
      endpoint: https://registry.modelcontextprotocol.io
    syncPolicy:
      interval: "1h"

  - name: internal-tools
    git:
      repository: https://github.com/myorg/mcp-catalog.git
      branch: main
      path: registry.json
    syncPolicy:
      interval: "30m"

registries:
  - name: production
    sources:
      - official-catalog
      - internal-tools

  - name: dev-team
    sources:
      - internal-tools
```

See [Configuration Guide](docs/configuration.md) for complete details.

## API endpoints

### Registry API v0.1 (read-only, standards-compliant)

Fully compatible with the upstream MCP Registry API specification:

- `GET /registry/{registryName}/v0.1/servers` - List servers from a specific registry
- `GET /registry/{registryName}/v0.1/servers/{name}/versions` - List all versions of a server
- `GET /registry/{registryName}/v0.1/servers/{name}/versions/{version}` - Get a specific server version

**Note:** Git, API, File, and Kubernetes sources are read-only through the registry API.

### Admin API v1

ToolHive-specific endpoints for managing sources, registries, and entries:

**Source management** (Cedar `sources.read/write/delete`; authenticated admin):

- `GET /v1/sources` - List all configured sources
- `GET /v1/sources/{name}` - Get a source by name
- `PUT /v1/sources/{name}` - Create or update a source
- `DELETE /v1/sources/{name}` - Delete a source
- `GET /v1/sources/{name}/entries` - List entries for a source

**Registry management** (Cedar `views.read/write/delete`; authenticated admin):

- `GET /v1/registries` - List all configured registries with status
- `GET /v1/registries/{name}` - Get registry details and sync status
- `GET /v1/registries/{name}/entries` - List unshadowed version diagnostics
- `PUT /v1/registries/{name}` - Create or update a registry
- `DELETE /v1/registries/{name}` - Delete a registry

**Entry management** (Cedar `entries.publish/delete`; authenticated admin):

- `POST /v1/entries` - Publish a server, skill, or plugin entry
- `DELETE /v1/entries/{type}/{name}/versions/{version}` - Delete a published entry
- `GET/PUT /v1/entries/{type}/{name}/claims` - Authenticated retired-endpoint tombstones (410)

### Skills extension API (ToolHive-specific)

Read-only endpoints for discovering skills within a registry:

- `GET /registry/{registryName}/v0.1/x/dev.toolhive/skills` - List skills (paginated)
- `GET /registry/{registryName}/v0.1/x/dev.toolhive/skills/{namespace}/{name}` - Get latest version of a skill
- `GET /registry/{registryName}/v0.1/x/dev.toolhive/skills/{namespace}/{name}/versions` - List all versions of a skill
- `GET /registry/{registryName}/v0.1/x/dev.toolhive/skills/{namespace}/{name}/versions/{version}` - Get a specific skill version

### Plugins extension API (ToolHive-specific)

Read-only endpoints for discovering plugins within a registry:

- `GET /registry/{registryName}/v0.1/x/dev.toolhive/plugins` - List plugins (paginated)
- `GET /registry/{registryName}/v0.1/x/dev.toolhive/plugins/{namespace}/{name}` - Get latest version of a plugin
- `GET /registry/{registryName}/v0.1/x/dev.toolhive/plugins/{namespace}/{name}/versions` - List all versions of a plugin
- `GET /registry/{registryName}/v0.1/x/dev.toolhive/plugins/{namespace}/{name}/versions/{version}` - Get a specific plugin version

### Operational endpoints

- `GET /health` - Health check
- `GET /readiness` - Readiness check
- `GET /version` - Version information
- `GET /v1/me` - Authenticated retired-endpoint tombstone (410), when admin is enabled
- `GET /.well-known/oauth-protected-resource` - OAuth discovery (RFC 9728)

### Use cases

- **Registry API**: Standards-compliant MCP discovery for clients and upstream integrations
- **Admin API**: Manage sources and registries, publish entries, query registry status

See the [MCP Registry API specification](https://github.com/modelcontextprotocol/registry/blob/main/docs/reference/api/openapi.yaml) for full API details.

## Configuration

All configuration is done via YAML files. The server requires a `--config` flag.

### Basic example

```yaml
sources:
  - name: local
    file:
      path: /data/registry.json

registries:
  - name: my-registry
    sources:
      - local

auth:
  mode: oauth
  oauth:
    providers:
      - name: company
        issuerUrl: https://id.example.org
        audience: registry-api
admin:
  enabled: false
authorization:
  # Omit policyFile to evaluate built-in allow-all Cedar after authentication.
  # For a custom restriction, mount the file read-only and set its absolute path:
  # policyFile: /etc/registry/policies.cedar
  allowAnonymousCatalog: false

database:
  host: localhost
  port: 5432
  user: registry
  database: registry
```

### Complete guides

- **[Configuration reference](docs/configuration.md)** - Complete configuration options
- **[Environment variables](docs/environment-variables.md)** - Using environment variables for configuration
- **[Database setup](docs/database.md)** - PostgreSQL configuration, migrations, and security
- **[Authentication](docs/authentication.md)** - OAuth/OIDC setup and security

## The ToolHive platform

The Registry Server can be deployed standalone or as part of the full [ToolHive](https://github.com/stacklok/toolhive) platform. Together, the platform components cover the full MCP lifecycle:

- **Registry** (this project) -- Discovery and governance. Knows what MCP servers, skills, and plugins exist, who can access them, and tracks all operations.
- **[Runtime](https://github.com/stacklok/toolhive)** -- Deployment and management. Deploys MCP servers on Kubernetes using registry metadata to drive lifecycle decisions. Servers deployed by the Runtime automatically appear in the Registry.
- **Gateway** -- Aggregation and access. Unifies multiple MCP servers into a single endpoint with centralized authentication, Cedar-based authorization, tool conflict resolution, and composite workflows.
- **Portal** -- Management UI. Catalog browsing, search, and administration backed by the Registry API.

See the [ToolHive documentation](https://docs.stacklok.com/toolhive/) for the complete platform architecture.

## Documentation

- **[Configuration reference](docs/configuration.md)** - All configuration options
- **[Environment variables](docs/environment-variables.md)** - Environment variable overrides
- **[Database setup](docs/database.md)** - PostgreSQL setup and migrations
- **[Authentication](docs/authentication.md)** - OAuth/OIDC security
- **[Observability](docs/observability.md)** - OpenTelemetry tracing and metrics
- **[Registry sync](docs/registry-sync.md)** - How background sync works
- **[Registry component architecture](docs/component-architecture.md)** - Public packages, contracts and ownership
- **[Registry models and formats](docs/registry-formats.md)** - Embed payload validation and response formatting without storage
- **[Reusable ingestion and operator export](docs/registry-ingestion.md)** - Source adapters, leased coordination, and optional Kubernetes lifecycle
- **[Definition persistence](docs/registry-persistence.md)** - Embeddable source and named-view definitions
- **[Kubernetes deployment](docs/deployment-kubernetes.md)** - K8s deployment and HA
- **[Docker deployment](docs/deployment-docker.md)** - Docker Compose setup
- **[API documentation](docs/thv-registry-api/)** - Auto-generated OpenAPI docs
- **[CLI reference](docs/cli/)** - Command-line interface docs
- **[Examples](examples/)** - Working configuration examples

## Contributing

We welcome contributions! See the **[Contributing guide](CONTRIBUTING.md)** to get started, including development setup, build commands, architecture overview, and project structure.

- **[Code of conduct](CODE_OF_CONDUCT.md)** - Community guidelines
- **[Security policy](SECURITY.md)** - Report security vulnerabilities

## License

This project is licensed under the [Apache 2.0 License](LICENSE).

---

**Part of the [ToolHive](https://github.com/stacklok/toolhive) project** - Simplify and secure MCP servers

<!-- Badge links -->
<!-- prettier-ignore-start -->
[release-img]: https://img.shields.io/github/v/release/stacklok/toolhive-registry-server?style=flat&label=Latest%20version
[release]: https://github.com/stacklok/toolhive-registry-server/releases/latest
[ci-img]: https://img.shields.io/github/actions/workflow/status/stacklok/toolhive-registry-server/run-on-main.yml?style=flat&logo=github&label=Build
[ci]: https://github.com/stacklok/toolhive-registry-server/actions/workflows/run-on-main.yml
[coveralls-img]: https://coveralls.io/repos/github/stacklok/toolhive-registry-server/badge.svg?branch=main
[coveralls]: https://coveralls.io/github/stacklok/toolhive-registry-server?branch=main
[license-img]: https://img.shields.io/badge/License-Apache2.0-blue.svg?style=flat
[license]: https://opensource.org/licenses/Apache-2.0
[stars-img]: https://img.shields.io/github/stars/stacklok/toolhive-registry-server.svg?style=flat&logo=github&label=Stars
[stars]: https://github.com/stacklok/toolhive-registry-server
[discord-img]: https://img.shields.io/discord/1184987096302239844?style=flat&logo=discord&logoColor=white&label=Discord
[discord]: https://discord.gg/stacklok
<!-- prettier-ignore-end -->

<!-- markdownlint-disable-file first-line-heading no-emphasis-as-heading no-inline-html -->
