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

**Discover, govern, and control access to MCP servers and skills across your organization**

The ToolHive Registry Server aggregates MCP servers and skills from Git repos, Kubernetes clusters, upstream registries, and internal APIs into named catalogs that your teams and AI clients can query. Each catalog has its own access control and audit trail, so you decide which entries are visible to which users -- and have a record of who accessed what.

It implements the official [Model Context Protocol (MCP) Registry API specification](https://modelcontextprotocol.io/development/roadmap#registry). If you're not familiar with MCP: it's an open protocol that lets AI assistants connect to external tools and data sources. This server is the governance layer that sits between your MCP infrastructure and the teams consuming it.

**Why ToolHive Registry Server?** Most MCP setups start with a flat list of servers and no access control. As adoption grows, you need to govern who sees what, aggregate servers from multiple sources, and audit access -- without locking into a proprietary registry. The ToolHive Registry Server is open source, implements the official MCP Registry API spec, and plugs into your existing identity provider, combining multi-tenant claim-based authorization, multi-source aggregation, and SIEM-compliant audit logging in a single service.

[Learn more about the ToolHive platform →](https://stacklok.com/platform)

---

## Table of contents

- [Features](#features)
- [Quickstart](#quickstart)
- [Core Concepts](#core-concepts)
- [API Endpoints](#api-endpoints)
- [Configuration](#configuration)
- [The ToolHive Platform](#the-toolhive-platform)
- [Documentation](#documentation)
- [Contributing](#contributing)

## Features

### Governance and access control

- **JWT claim-based visibility**: Control which MCP servers and skills each user or team can see, per registry. A "production" registry can expose only vetted entries while a "dev-team" registry shows everything.
- **OAuth 2.0/OIDC authentication**: Plug in your existing identity provider (Okta, Auth0, Azure AD). OAuth is the default; anonymous mode is available for development.
- **Role-based administration**: Separate roles for managing sources, registries, and entries. Scope each role to specific JWT claims so teams manage their own resources.
- **SIEM-compliant audit logging**: Structured log covering all API operations with NIST SP 800-53 AU-3 compliant fields, dedicated file output, and configurable event filtering.

### Aggregate from anywhere

- **Five source types**: Pull entries from Git repos, upstream MCP registries, local files, Kubernetes clusters, or publish them via the Admin API.
- **Compose catalogs from multiple sources**: Each registry aggregates one or more sources, listed in order of precedence. One source can feed multiple registries, so the same internal catalog can serve different teams with different visibility rules.
- **Background sync**: Sources are polled on configurable intervals with retry logic. Registries stay current without manual intervention.

### Kubernetes and observability

- **Kubernetes-native discovery**: A built-in reconciler watches for MCP server CRDs across namespaces and syncs them into the registry automatically. Per-entry access control via CRD annotations, multi-namespace support, and leader election for HA.
- **OpenTelemetry**: Distributed tracing and metrics out of the box.
- **Standards-compliant**: Implements the official MCP Registry API specification. Clients that speak the spec work out of the box.
- **PostgreSQL backend**: Database storage with automatic migrations.

## Quickstart

### Prerequisites

- Go 1.26 or later (for building from source)
- [Task](https://taskfile.dev) for build automation
- PostgreSQL 16+

### Build and run

```bash
# Build the binary
task build

# Run with Git source
thv-registry-api serve --config examples/config-git.yaml

# Run with local file
thv-registry-api serve --config examples/config-file.yaml
```

The server starts on `http://localhost:8080` by default.

### Docker quickstart

```bash
# Using Task (recommended - ensures fresh state)
task docker-up

# Or detached mode
task docker-up-detached

# Access the API
curl http://localhost:8080/registry/default/v0.1/servers

# Stop and clean up
task docker-down
```

> **Note:** The `task docker-up` command ensures a fresh start by rebuilding the image and clearing all volumes (database + registry data). This prevents stale state issues.

## Core concepts

### Sources and registries

Most organizations have MCP servers and skills in more than one place -- a public catalog, an internal Git repo, a Kubernetes cluster running live instances. The Registry Server models this with two primitives: **sources** (where entries come from) and **registries** (what consumers query).

A **source** is a connection to where MCP server and skill entries live. It tells the server "go look here for entries." Sources come in five types:

| Type           | What it does                        | Example                                                      | Sync         |
| -------------- | ----------------------------------- | ------------------------------------------------------------ | ------------ |
| **API**        | Pulls from an upstream registry API | The official MCP Registry at registry.modelcontextprotocol.io | Auto         |
| **Git**        | Clones entries from a Git repo      | A version-controlled internal catalog                        | Auto         |
| **File**       | Reads from the local filesystem     | A curated `registry.json` on disk                            | Auto         |
| **Managed**    | Entries published via the Admin API | Dynamically registered internal servers                      | On-demand    |
| **Kubernetes** | Discovers deployed MCP servers      | Servers running in your K8s clusters                         | On-demand    |

A **registry** is a named catalog that aggregates one or more sources into a single consumer-facing endpoint. Each registry can pull from different sources and enforce its own access control via JWT claims. Sources are listed in order -- when the same entry appears in multiple sources, the first source in the list wins.

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
    format: upstream
    api:
      endpoint: https://registry.modelcontextprotocol.io
    syncPolicy:
      interval: "1h"

  - name: internal-tools
    format: toolhive
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

**Source management** (requires `manageSources` role):

- `GET /v1/sources` - List all configured sources
- `GET /v1/sources/{name}` - Get a source by name
- `PUT /v1/sources/{name}` - Create or update a source
- `DELETE /v1/sources/{name}` - Delete a source
- `GET /v1/sources/{name}/entries` - List entries for a source

**Registry management** (reads: authenticated; writes require `manageRegistries` role):

- `GET /v1/registries` - List all configured registries with status
- `GET /v1/registries/{name}` - Get registry details and sync status
- `GET /v1/registries/{name}/entries` - List entries for a registry (requires `manageRegistries` role)
- `PUT /v1/registries/{name}` - Create or update a registry
- `DELETE /v1/registries/{name}` - Delete a registry

**Entry management** (requires `manageEntries` role):

- `POST /v1/entries` - Publish a server or skill entry
- `DELETE /v1/entries/{type}/{name}/versions/{version}` - Delete a published entry
- `PUT /v1/entries/{type}/{name}/claims` - Update entry claims

### Skills extension API (ToolHive-specific)

Read-only endpoints for discovering skills within a registry:

- `GET /registry/{registryName}/v0.1/x/dev.toolhive/skills` - List skills (paginated)
- `GET /registry/{registryName}/v0.1/x/dev.toolhive/skills/{namespace}/{name}` - Get latest version of a skill
- `GET /registry/{registryName}/v0.1/x/dev.toolhive/skills/{namespace}/{name}/versions` - List all versions of a skill
- `GET /registry/{registryName}/v0.1/x/dev.toolhive/skills/{namespace}/{name}/versions/{version}` - Get a specific skill version

### Operational endpoints

- `GET /health` - Health check
- `GET /readiness` - Readiness check
- `GET /version` - Version information
- `GET /v1/me` - Returns the caller's identity and roles
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
    format: toolhive
    file:
      path: /data/registry.json

registries:
  - name: my-registry
    sources:
      - local

auth:
  mode: anonymous # Use "oauth" for production

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

- **Registry** (this project) -- Discovery and governance. Knows what MCP servers and skills exist, who can access them, and tracks all operations.
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
