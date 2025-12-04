<p float="left">
  <picture>
    <img src="docs/images/toolhive-icon-1024.png" alt="ToolHive Studio logo" height="100" align="middle" />
  </picture>
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/images/toolhive-wordmark-white.png">
    <img src="docs/images/toolhive-wordmark-black.png" alt="ToolHive wordmark" width="500" align="middle" hspace="20" />
  </picture>
  <picture>
    <img src="docs/images/toolhive.png" alt="ToolHive mascot" width="125" align="middle"/>
  </picture>
</p>

[![Release][release-img]][release] [![Build status][ci-img]][ci]
[![Coverage Status][coveralls-img]][coveralls]
[![License: Apache 2.0][license-img]][license]
[![Star on GitHub][stars-img]][stars] [![Discord][discord-img]][discord]

# ToolHive Registry API Server

**The central metadata hub for enterprise MCP governance and discovery**

The ToolHive Registry API (`thv-registry-api`) implements the official [Model Context Protocol (MCP) Registry API specification](https://modelcontextprotocol.io/development/roadmap#registry). It serves as the centralized metadata engine for the ToolHive platform, enabling enterprises to curate, discover, and govern MCP servers with security and auditability built-in.

---

## Table of Contents

- [Features](#features)
- [Quick Start](#quick-start)
- [Core Concepts](#core-concepts)
  - [Data Sources](#data-sources)
  - [Architecture](#architecture)
- [API Endpoints](#api-endpoints)
- [Configuration](#configuration)
- [Deployment](#deployment)
- [Development](#development)
- [Documentation](#documentation)
- [Contributing](#contributing)

## Features

### Enterprise Governance & Security
- **OAuth 2.0/OIDC authentication**: Integrate with enterprise identity providers (Okta, Auth0, Azure AD)
- **Multi-provider support**: Combine corporate SSO with Kubernetes service accounts
- **Secure by default**: OAuth mode enabled by default, with granular access control
- **Audit trail**: Track MCP discovery and access through centralized metadata

### Flexible Registry Sources
- **Curated registries**: Aggregate multiple sources into a unified catalog
- **Upstream verified**: Sync from public MCP registries implementing the standard API
- **File-based registries**: Support both ToolHive and upstream `server.json` formats
- **Internal custom MCPs**: Manage organization-specific MCP servers
- **Kubernetes discovery**: Automatically discover MCPs deployed in your clusters
- **Automatic synchronization**: Background sync with configurable intervals and retry logic

### Enterprise Integration
- **Central metadata hub**: Powers the ToolHive Enterprise UI with MCP metadata
- **ToolHive Operator integration**: Provides metadata for Kubernetes-native MCP deployment
- **PostgreSQL backend**: Scalable database storage with automatic migrations
- **Standards-compliant**: Implements the official MCP Registry API specification
- **Production-ready**: Built-in health checks, graceful shutdown, and observability

## Quick Start

### Prerequisites

- Go 1.23 or later (for building from source)
- [Task](https://taskfile.dev) for build automation
- PostgreSQL 16+ (optional, for database backend)

### Build and Run

```bash
# Build the binary
task build

# Run with Git source
thv-registry-api serve --config examples/config-git.yaml

# Run with local file
thv-registry-api serve --config examples/config-file.yaml
```

The server starts on `http://localhost:8080` by default.

### Docker Quick Start

```bash
# Using Docker Compose (includes PostgreSQL)
docker-compose up

# Access the API
curl http://localhost:8080/registry/v0.1/servers
```

### What Happens on Startup

1. Loads configuration from YAML file
2. Runs database migrations automatically (if configured)
3. Immediately fetches registry data from configured sources
4. Starts background sync coordinator for automatic updates
5. Serves MCP Registry API endpoints

## Core Concepts

### Data Sources

The Registry Server enables enterprises to curate MCP catalogs from multiple sources, creating a unified view for developers and knowledge workers:

| Type | Description | Enterprise Use Case | Sync |
|------|-------------|---------------------|------|
| **API** | Upstream MCP Registry APIs | Official MCP Registry (registry.modelcontextprotocol.io) or any registry implementing the upstream specification | ✅ Auto |
| **Git** | Clone from Git repositories | Version-controlled internal registries | ✅ Auto |
| **File** | Read from local filesystem | Simple curated lists in ToolHive or upstream format | ✅ Auto |
| **Managed** | API-managed registry | Internal custom MCPs (dynamically managed) | ❌ On-demand |
| **Kubernetes** | Discover from K8s deployments | Organization-deployed MCPs (live discovery) | ❌ On-demand |

**Key capability**: Configure multiple registries simultaneously to create a federated catalog that combines:
- Official MCP Registry (registry.modelcontextprotocol.io) or any registry implementing the upstream specification
- Internal organization-specific MCPs
- Kubernetes-deployed MCPs
- Custom curated collections

**Configuration example:**

```yaml
registries:
  - name: local
    format: toolhive
    file:
      path: /data/registry.json
```

For Git-based registries:
```yaml
registries:
  - name: toolhive
    format: toolhive
    git:
      repository: https://github.com/stacklok/toolhive.git
      branch: main
      path: pkg/registry/data/registry.json
    syncPolicy:
      interval: "30m"
```

See [Configuration Guide](docs/configuration.md) for complete details.

### Architecture

The server follows clean architecture with clear separation of concerns:

```
┌─────────────────────────────────────────────┐
│  API Layer (Chi Router)                     │
│  ├─ Registry API v0.1                       │
│  ├─ Extension API v0                        │
│  └─ OAuth/OIDC Middleware                   │
└─────────────────────────────────────────────┘
                    │
┌─────────────────────────────────────────────┐
│  Service Layer                              │
│  ├─ DB Service (PostgreSQL)                 │
│  └─ In-Memory Service                       │
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
- `database/` - SQL migrations and queries

## API Endpoints

The server provides two types of registry endpoints to support different use cases:

### 1. Aggregated Registry Endpoints (Read-Only)

**Unified view across all configured registries** - ideal for enterprise-wide discovery:

- `GET /registry/v0.1/servers` - List all MCP servers from all registries
- `GET /registry/v0.1/servers/{name}/versions` - List all versions of a server
- `GET /registry/v0.1/servers/{name}/versions/{version}` - Get a specific server version

### 2. Per-Registry Endpoints (Standards-Compliant)

**Individual registry access** - fully compatible with upstream MCP Registry API specification:

**Read operations (all registry types):**
- `GET /registry/{registryName}/v0.1/servers` - List servers from a specific registry
- `GET /registry/{registryName}/v0.1/servers/{name}/versions` - List versions from a specific registry
- `GET /registry/{registryName}/v0.1/servers/{name}/versions/{version}` - Get specific version from a specific registry

**Write operations (managed registries only):**
- `POST /registry/{registryName}/v0.1/publish` - Publish a server to a managed registry
- `DELETE /registry/{registryName}/v0.1/servers/{name}/versions/{version}` - Delete a server version from a managed registry

**Note:** Write operations (POST, DELETE) are only supported for `managed` registry types. Git, API, File, and Kubernetes registries are read-only through the API.

### Extension API (v0)

ToolHive-specific extensions for querying registry status:

- `GET /extension/v0/registries` - List all configured registries with status
- `GET /extension/v0/registries/{name}` - Get registry details and sync status

**Note:** Dynamic registry and server management endpoints (PUT/DELETE operations) are not yet implemented.

### Operational Endpoints

- `GET /health` - Health check
- `GET /readiness` - Readiness check
- `GET /version` - Version information
- `GET /.well-known/oauth-protected-resource` - OAuth discovery (RFC 9728)

### Use Cases

- **Aggregated endpoints**: Enterprise UI showing unified catalog of all MCPs
- **Per-registry endpoints**: Direct access to specific registries (e.g., only upstream verified MCPs)
- **Extension API**: Query registry status and configuration

See the [MCP Registry API specification](https://github.com/modelcontextprotocol/registry/blob/main/docs/reference/api/openapi.yaml) for full API details.

## Configuration

All configuration is done via YAML files. The server requires a `--config` flag.

### Basic Example

```yaml
registryName: my-registry

registries:
  - name: local
    format: toolhive
    file:
      path: /data/registry.json

auth:
  mode: anonymous  # Use "oauth" for production

# Optional: Add more registries
# - name: toolhive
#   format: toolhive
#   git:
#     repository: https://github.com/stacklok/toolhive.git
#     branch: main
#     path: pkg/registry/data/registry.json
#   syncPolicy:
#     interval: "30m"

# Optional: Database backend
# database:
#   host: localhost
#   port: 5432
#   user: registry
#   database: registry
```

### 📖 Complete Guides

- **[Configuration Reference](docs/configuration.md)** - Complete configuration options
- **[Database Setup](docs/database.md)** - PostgreSQL configuration, migrations, and security
- **[Authentication](docs/authentication.md)** - OAuth/OIDC setup and security

### Configuration Examples

See [examples/](examples/) directory for complete working examples:
- `config-git.yaml` - Git repository source
- `config-api.yaml` - API endpoint source
- `config-file.yaml` - Local file source
- `config-database-dev.yaml` - With database (development)
- `config-database-prod.yaml` - With database (production)
- `config-docker.yaml` - For Docker Compose

## Deployment

### Docker Compose

Quick start with PostgreSQL:

```bash
docker-compose up
```

See [Docker Deployment Guide](docs/deployment-docker.md) for details.

### Kubernetes

Basic deployment:

```bash
kubectl apply -f examples/kubernetes/
```

See [Kubernetes Deployment Guide](docs/deployment-kubernetes.md) for production setup.

### Standalone Binary

```bash
# Linux/macOS
./bin/thv-registry-api serve --config config.yaml

# With environment variables
export THV_DATABASE_PASSWORD="secure-password"
./bin/thv-registry-api serve --config config.yaml
```

### 📖 Deployment Guides

- **[Docker & Docker Compose](docs/deployment-docker.md)** - Container deployment
- **[Kubernetes](docs/deployment-kubernetes.md)** - K8s deployment, HA, and production best practices

## Development

### Build Commands

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

### Project Structure

```
cmd/thv-registry-api/    # Main application
├── app/                 # CLI commands (serve, migrate, prime-db, version)
└── main.go

internal/                # Internal packages
├── api/                 # HTTP API handlers
├── auth/                # OAuth/OIDC authentication
├── config/              # Configuration loading
├── db/                  # Database access (sqlc generated)
├── service/             # Business logic
├── sources/             # Data source handlers
├── sync/                # Background sync coordination
├── filtering/           # Registry entry filtering
├── git/                 # Git operations
├── kubernetes/          # Kubernetes discovery
└── registry/            # Registry data models

database/                # Database schema and queries
├── migrations/          # SQL migrations
└── queries/             # SQL queries for sqlc

examples/                # Example configurations
docs/                    # Documentation
```

## Documentation

### 📚 Complete Documentation

- **[Configuration Reference](docs/configuration.md)** - All configuration options
- **[Database Setup](docs/database.md)** - PostgreSQL setup and migrations
- **[Authentication](docs/authentication.md)** - OAuth/OIDC security
- **[Kubernetes Deployment](docs/deployment-kubernetes.md)** - K8s deployment guide
- **[Docker Deployment](docs/deployment-docker.md)** - Docker & Docker Compose
- **[API Documentation](docs/thv-registry-api/)** - Auto-generated OpenAPI docs
- **[CLI Reference](docs/cli/)** - Command-line interface docs
- **[Examples](examples/)** - Working configuration examples
- **[CLAUDE.md](CLAUDE.md)** - AI assistant guidance for contributors

### Common Tasks

| I want to... | See... |
|--------------|--------|
| Get started quickly | [Quick Start](#quick-start) |
| Configure the server | [Configuration Reference](docs/configuration.md) |
| Set up PostgreSQL | [Database Setup](docs/database.md) |
| Enable authentication | [Authentication Guide](docs/authentication.md) |
| Deploy to Kubernetes | [Kubernetes Guide](docs/deployment-kubernetes.md) |
| Use Docker Compose | [Docker Guide](docs/deployment-docker.md) |
| Contribute code | [Contributing](#contributing) |

## Integration with ToolHive

The Registry API server is the central metadata engine of the ToolHive platform:

### Enterprise UI Integration
- **Metadata source**: Provides MCP details (name, description, URL, version, branding)
- **Server status**: Reports sync status and availability for each registry
- **Discovery experience**: Powers the catalog browsing and search in the Enterprise UI
- **Authentication**: Enforces OAuth/OIDC access control from your identity provider

### ToolHive Operator Integration
- **Deployment metadata**: Operator references registry data when deploying MCPs to Kubernetes
- **Automated discovery**: Kubernetes-deployed MCPs automatically appear in the registry
- **Custom Resource binding**: MCPRegistry CRDs are backed by Registry Server data
- **Lifecycle management**: Tracks deployed MCP versions and configurations

### Security & Governance
- **Centralized control**: Single source of truth for approved MCPs
- **Identity integration**: Seamless integration with Okta, Auth0, Azure AD, and other providers
- **Kubernetes-native**: All MCP execution stays within your Kubernetes boundary
- **Audit trail**: Track MCP discovery and consumption patterns

See the [ToolHive documentation](https://docs.stacklok.com/toolhive/) for the complete platform architecture.

## Contributing

We welcome contributions! Please see:

- **[Contributing Guide](CONTRIBUTING.md)** - How to contribute
- **[Code of Conduct](CODE_OF_CONDUCT.md)** - Community guidelines
- **[Security Policy](SECURITY.md)** - Report security vulnerabilities
- **[CLAUDE.md](CLAUDE.md)** - AI assistant guidance for development

### Development Guidelines

- Run `task lint-fix` before committing
- Ensure tests pass with `task test`
- Follow Go standard project layout
- Use mockgen for test mocks (never hand-written)
- Write table-driven tests
- Keep documentation up to date

### Quick Start for Contributors

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

<!-- markdownlint-disable-file first-line-heading no-inline-html -->