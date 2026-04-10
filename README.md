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

**A registry for MCP servers and skills -- so your organization knows what's available, who can use it, and where it came from**

The ToolHive Registry Server aggregates MCP servers and skills from Git repos, Kubernetes clusters, upstream registries, and internal APIs into named catalogs that your teams and AI clients can query. Each catalog has its own access control, so you decide which entries are visible to which users.

It implements the official [Model Context Protocol (MCP) Registry API specification](https://modelcontextprotocol.io/development/roadmap#registry). If you're not familiar with MCP: it's an open protocol that lets AI assistants connect to external tools and data sources. This server is the discovery and governance layer on top.

---

## Table of contents

- [Features](#features)
- [Quickstart](#quickstart)
- [Core Concepts](#core-concepts)
  - [Sources and Registries](#sources-and-registries)
  - [Architecture](#architecture)
- [API Endpoints](#api-endpoints)
- [Configuration](#configuration)
- [Deployment](#deployment)
- [Development](#development)
- [Documentation](#documentation)
- [Contributing](#contributing)

## Features

### Access control and audit

- **JWT claim-based visibility**: Control which MCP servers and skills each user or team can see, per registry. A "production" registry can expose only vetted entries while a "dev-team" registry shows everything.
- **OAuth 2.0/OIDC authentication**: Plug in your existing identity provider (Okta, Auth0, Azure AD). OAuth is the default; anonymous mode is available for development.
- **SIEM-compliant audit logging**: Structured log covering all API operations with NIST SP 800-53 AU-3 compliant fields, dedicated file output, and configurable event filtering.

### Aggregate from anywhere

- **Five source types**: Pull entries from Git repos, upstream MCP registries, local files, Kubernetes clusters, or publish them via the Admin API.
- **Compose catalogs from multiple sources**: Each registry aggregates one or more sources with priority ordering. One source can feed multiple registries, so the same internal catalog can serve different teams with different visibility rules.
- **Background sync**: Sources are polled on configurable intervals with retry logic. Registries stay current without manual intervention.

### Production infrastructure

- **Standards-compliant**: Implements the official MCP Registry API specification. Clients that speak the spec work out of the box.
- **PostgreSQL backend**: Database storage with automatic migrations.
- **OpenTelemetry**: Distributed tracing and metrics for observability.

## Quickstart

### Prerequisites

- Go 1.26 or later (for building from source)
- [Task](https://taskfile.dev) for build automation
- PostgreSQL 16+ (optional, for database backend)

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
curl http://localhost:8080/registry/v0.1/servers

# Stop and clean up
task docker-down
```

> **Note:** The `task docker-up` command ensures a fresh start by rebuilding the image and clearing all volumes (database + registry data). This prevents stale state issues.

### What happens on startup

1. Loads configuration from YAML file
2. Runs database migrations automatically (if configured)
3. Immediately fetches registry data from configured sources
4. Starts background sync coordinator for automatic updates
5. Serves MCP Registry API endpoints

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

A **registry** is a named catalog that aggregates one or more sources into a single consumer-facing endpoint. Each registry can pull from different sources, apply its own filtering, set priority ordering, and enforce its own access control via JWT claims.

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

### Architecture

The server follows clean architecture with clear separation of concerns:

```text
┌─────────────────────────────────────────────┐
│  API Layer (Chi Router)                     │
│  ├─ Registry API v0.1                       │
│  ├─ Admin API v1                            │
│  └─ OAuth/OIDC Middleware                   │
└─────────────────────────────────────────────┘
                    │
┌─────────────────────────────────────────────┐
│  Service Layer (RegistryService)            │
│  └─ PostgreSQL backend                      │
└─────────────────────────────────────────────┘
                    │
┌─────────────────────────────────────────────┐
│  Data Source Layer                          │
│  ├─ Git Handler                             │
│  ├─ API Handler                             │
│  ├─ File Handler                            │
│  ├─ Managed Handler                         │
│  └─ Kubernetes Handler                      │
└─────────────────────────────────────────────┘
                    │
┌─────────────────────────────────────────────┐
│  Sync Layer (Background Coordinator)        │
└─────────────────────────────────────────────┘
```

**Key directories:**

- `internal/api/` - HTTP API handlers
- `internal/service/` - Business logic
- `internal/sources/` - Data source handlers
- `internal/sync/` - Background synchronization
- `internal/auth/` - OAuth/OIDC authentication
- `internal/db/` - Database access (sqlc generated)
- `internal/config/` - Configuration loading
- `internal/app/` - Application builder and startup
- `internal/telemetry/` - OpenTelemetry integration
- `database/` - SQL migrations and queries

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

# Optional: Add a Git-based source
# sources:
#   - name: toolhive
#     format: toolhive
#     git:
#       repository: https://github.com/stacklok/toolhive-catalog.git
#       branch: main
#       path: pkg/catalog/toolhive/data/registry-legacy.json
#     syncPolicy:
#       interval: "30m"
# registries:
#   - name: my-registry
#     sources:
#       - toolhive

# Optional: Database backend
# database:
#   host: localhost
#   port: 5432
#   user: registry
#   database: registry
```

### Complete guides

- **[Configuration reference](docs/configuration.md)** - Complete configuration options
- **[Environment variables](docs/environment-variables.md)** - Using environment variables for configuration
- **[Database setup](docs/database.md)** - PostgreSQL configuration, migrations, and security
- **[Authentication](docs/authentication.md)** - OAuth/OIDC setup and security

### Configuration examples

See [examples/](examples/) directory for complete working examples:

- `config-git.yaml` - Git repository source
- `config-api.yaml` - API endpoint source
- `config-file.yaml` - Local file source
- `config-database-dev.yaml` - With database (development)
- `config-database-prod.yaml` - With database (production)
- `config-docker.yaml` - For Docker Compose

## Deployment

### Docker Compose

Quick start with PostgreSQL (ensures fresh state):

```bash
# Start with fresh state (recommended)
task docker-up

# Or detached mode
task docker-up-detached

# View logs
task docker-logs           # All logs
FOLLOW=true task docker-logs  # Tail logs

# Stop and clean up
task docker-down
```

> **Note:** Always use `task docker-up` instead of `docker-compose up` directly. The task command ensures fresh state by rebuilding the image and clearing volumes.

See [Docker deployment guide](docs/deployment-docker.md) for details.

### Kubernetes

Basic deployment:

```bash
kubectl apply -f examples/kubernetes/
```

See [Kubernetes deployment guide](docs/deployment-kubernetes.md) for production setup.

### Standalone binary

```bash
# Linux/macOS
./bin/thv-registry-api serve --config config.yaml

# With environment variables
export THV_REGISTRY_DATABASE_HOST=postgres.example.com
export THV_REGISTRY_AUTH_MODE=anonymous
./bin/thv-registry-api serve --config config.yaml
```

### Deployment guides

- **[Docker & Docker Compose](docs/deployment-docker.md)** - Container deployment
- **[Kubernetes](docs/deployment-kubernetes.md)** - K8s deployment, HA, and production best practices

## Development

### Build commands

```bash
# Build the binary
task build

# Run linting (auto-fix)
task lint-fix

# Run tests
task test

# Generate mocks
task gen

# Build container image
task build-image

# Run database migrations
task migrate-up CONFIG=examples/config-database-dev.yaml

# Regenerate documentation
task docs

# All checks (lint + test + build)
task all
```

### Testing

```bash
# Generate mocks before testing
task gen

# Run all tests
task test

# Run specific test
go test ./internal/service/... -v
```

The project uses table-driven tests with mocks generated via `go.uber.org/mock`.

### Project structure

```text
cmd/thv-registry-api/    # Main application
├── app/                 # CLI commands (serve, migrate, prime-db, version)
└── main.go

internal/                # Internal packages
├── api/                 # HTTP API handlers
├── app/                 # Application builder and startup
├── audit/               # SIEM-compliant audit logging
├── auth/                # OAuth/OIDC authentication
├── authz/               # JWT claim-based authorization
├── config/              # Configuration loading
├── db/                  # Database access (sqlc generated)
├── filtering/           # Registry entry filtering
├── kubernetes/          # Kubernetes reconciler for K8s sources
├── registry/            # Registry data models
├── service/             # Business logic
├── sources/             # Data source handlers (Git, API, File, Managed)
├── sync/                # Background sync coordination
├── telemetry/           # OpenTelemetry integration
└── validators/          # Input validation

database/                # Database schema and queries
├── migrations/          # SQL migrations
└── queries/             # SQL queries for sqlc

examples/                # Example configurations
docs/                    # Documentation
```

## Documentation

### Complete documentation

- **[Configuration reference](docs/configuration.md)** - All configuration options
- **[Environment variables](docs/environment-variables.md)** - Using environment variables for configuration
- **[Database setup](docs/database.md)** - PostgreSQL setup and migrations
- **[Authentication](docs/authentication.md)** - OAuth/OIDC security
- **[Observability](docs/observability.md)** - OpenTelemetry tracing and metrics
- **[Registry sync](docs/registry-sync.md)** - How background sync works, status tracking, retry logic
- **[Kubernetes deployment](docs/deployment-kubernetes.md)** - K8s deployment guide
- **[Docker deployment](docs/deployment-docker.md)** - Docker & Docker Compose
- **[API documentation](docs/thv-registry-api/)** - Auto-generated OpenAPI docs
- **[CLI reference](docs/cli/)** - Command-line interface docs
- **[Examples](examples/)** - Working configuration examples
- **[CLAUDE.md](CLAUDE.md)** - AI assistant guidance for contributors

### Common Tasks

| I want to...          | See...                                            |
| --------------------- | ------------------------------------------------- |
| Get started quickly   | [Quickstart](#quickstart)                         |
| Configure the server  | [Configuration reference](docs/configuration.md)  |
| Set up PostgreSQL     | [Database setup](docs/database.md)                |
| Enable authentication | [Authentication guide](docs/authentication.md)    |
| Set up observability  | [Observability guide](docs/observability.md)      |
| Understand sync       | [Registry sync](docs/registry-sync.md)            |
| Deploy to Kubernetes  | [Kubernetes guide](docs/deployment-kubernetes.md) |
| Use Docker Compose    | [Docker guide](docs/deployment-docker.md)         |
| Contribute code       | [Contributing](#contributing)                     |

## Integration with ToolHive

The Registry API server is the central metadata engine of the ToolHive platform:

### Enterprise UI integration

- **Metadata source**: Provides MCP details (name, description, URL, version, branding)
- **Server status**: Reports sync status and availability for each registry
- **Discovery experience**: Powers the catalog browsing and search in the Enterprise UI
- **Authentication**: Enforces OAuth/OIDC access control from your identity provider

### ToolHive Operator integration

- **Deployment metadata**: Operator references registry data when deploying MCPs to Kubernetes
- **Automated discovery**: Kubernetes-deployed MCPs automatically appear in the registry
- **Custom Resource binding**: MCPRegistry CRDs are backed by Registry Server data
- **Lifecycle management**: Tracks deployed MCP versions and configurations

### Security and governance

- **Centralized control**: Single source of truth for approved MCP servers and skills
- **Identity integration**: Seamless integration with Okta, Auth0, Azure AD, and other providers
- **Kubernetes-native**: All MCP execution stays within your Kubernetes boundary
- **Audit logging**: SIEM-compliant structured audit log for all API operations

See the [ToolHive documentation](https://docs.stacklok.com/toolhive/) for the complete platform architecture.

## Contributing

We welcome contributions! Please see:

- **[Contributing guide](CONTRIBUTING.md)** - How to contribute
- **[Code of conduct](CODE_OF_CONDUCT.md)** - Community guidelines
- **[Security policy](SECURITY.md)** - Report security vulnerabilities
- **[CLAUDE.md](CLAUDE.md)** - AI assistant guidance for development

### Development guidelines

- Run `task lint-fix` before committing
- Ensure tests pass with `task test`
- Follow Go standard project layout
- Use mockgen for test mocks (never hand-written)
- Write table-driven tests
- Keep documentation up to date

### Quick start for contributors

```bash
# Clone the repository
git clone https://github.com/stacklok/toolhive-registry-server.git
cd toolhive-registry-server

# Install dependencies
go mod download

# Run all checks
task all

# Make your changes...

# Run tests
task gen
task test

# Submit PR
```

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
