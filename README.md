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

**A standards-compliant MCP Registry API server for ToolHive**

The ToolHive Registry API (`thv-registry-api`) implements the official [Model Context Protocol (MCP) Registry API specification](https://modelcontextprotocol.io/development/roadmap#registry). It provides a standardized REST API for discovering and accessing MCP servers from multiple backend sources.

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

- **Standards-compliant**: Implements the official MCP Registry API specification
- **Multiple data sources**: Git repositories, API endpoints, local files, managed registries, and Kubernetes discovery
- **Automatic synchronization**: Background sync with configurable intervals and retry logic
- **OAuth 2.0/OIDC authentication**: Secure by default with multi-provider support
- **PostgreSQL backend**: Optional database storage with automatic migrations
- **Container-ready**: Designed for deployment in Kubernetes clusters
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

The server supports five registry types, each suited for different use cases:

| Type | Description | Use Case | Sync |
|------|-------------|----------|------|
| **Git** | Clone from Git repositories | Version-controlled registries | ✅ Auto |
| **API** | Fetch from upstream MCP Registry APIs | Federation and aggregation | ✅ Auto |
| **File** | Read from local filesystem | Development and testing | ✅ Auto |
| **Managed** | Managed via API calls | Dynamic, API-controlled registries | ❌ On-demand |
| **Kubernetes** | Discover from K8s deployments | Cluster-local service discovery | ❌ On-demand |

**Configuration example:**

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

### Registry API (v0.1)

Standard MCP Registry API endpoints:

- `GET /registry/v0.1/servers` - List all available MCP servers
- `GET /registry/v0.1/servers/{name}/versions` - List all versions of a server
- `GET /registry/v0.1/servers/{name}/versions/{version}` - Get a specific server version
- `DELETE /registry/{registryName}/v0.1/servers/{name}/versions/{version}` - Delete a server version (managed registries only)
- `POST /registry/v0.1/publish` - Publish a server (not yet implemented)

### Extension API (v0)

ToolHive-specific extensions:

- `GET /extension/v0/registries` - List all registries (not yet implemented)
- `GET /extension/v0/registries/{name}` - Get registry details (not yet implemented)
- `PUT /extension/v0/registries/{name}` - Create or update a registry (not yet implemented)
- `DELETE /extension/v0/registries/{name}` - Delete a registry (not yet implemented)

### Operational Endpoints

- `GET /health` - Health check
- `GET /readiness` - Readiness check
- `GET /version` - Version information
- `GET /.well-known/oauth-protected-resource` - OAuth discovery (RFC 9728)

See the [MCP Registry API specification](https://github.com/modelcontextprotocol/registry/blob/main/docs/reference/api/openapi.yaml) for full API details.

## Configuration

All configuration is done via YAML files. The server requires a `--config` flag.

### Basic Example

```yaml
registryName: my-registry

registries:
  - name: toolhive
    format: toolhive
    git:
      repository: https://github.com/stacklok/toolhive.git
      branch: main
      path: pkg/registry/data/registry.json
    syncPolicy:
      interval: "30m"

auth:
  mode: anonymous  # Use "oauth" for production

database:  # Optional
  host: localhost
  port: 5432
  user: registry
  database: registry
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

The Registry API server works seamlessly with the ToolHive ecosystem:

- **ToolHive Operator**: Automatically deployed as part of MCPRegistry resources
- **ToolHive CLI**: Discover and install MCP servers from registries

See the [ToolHive documentation](https://docs.stacklok.com/toolhive/) for more details.

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