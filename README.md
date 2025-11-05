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

## Features

- **Standards-compliant**: Implements the official MCP Registry API specification
- **Multiple data sources**: Git repositories, Kubernetes ConfigMaps, API endpoints, and local files
- **Automatic synchronization**: Background sync with configurable intervals and retry logic
- **Container-ready**: Designed for deployment in Kubernetes clusters
- **Flexible deployment**: Works standalone or as part of ToolHive infrastructure
- **Production-ready**: Built-in health checks, graceful shutdown, and sync status persistence

## Quick Start

### Prerequisites

- Go 1.23 or later
- [Task](https://taskfile.dev) for build automation
- Access to a Kubernetes cluster (for ConfigMap data source)

### Building the binary

```bash
# Build the binary
task build
```

### Running the Server

All configuration is done via YAML configuration files. See the [examples/](examples/) directory for sample configurations.

**Quick start with Git source:**
```bash
thv-registry-api serve --config examples/config-git.yaml
```

**With Kubernetes ConfigMap:**
```bash
thv-registry-api serve --config examples/config-configmap.yaml
```

**With local file:**
```bash
thv-registry-api serve --config examples/config-file.yaml
```

The server starts on port 8080 by default. Use `--address :PORT` to customize.

**What happens when the server starts:**
1. Loads configuration from the specified YAML file
2. Immediately fetches registry data from the configured source
3. Starts background sync coordinator for automatic updates
4. Serves MCP Registry API endpoints on the configured address

For detailed configuration options and examples, see the [examples/README.md](examples/README.md).

## API Endpoints

The server implements the standard MCP Registry API:

- `GET /api/v0/servers` - List all available MCP servers
- `GET /api/v0/servers/{name}` - Get details for a specific server
- `GET /api/v0/deployed` - List deployed server instances (Kubernetes only)
- `GET /api/v0/deployed/{name}` - Get deployed instances of a specific server

See the [MCP Registry API specification](https://github.com/modelcontextprotocol/registry/blob/main/docs/reference/api/openapi.yaml) for full API details.
**Note**: The current implementation is not strictly compliant with the standard. The deviations will be fixed in the next iterations.

## Configuration

All configuration is done via YAML files. The server requires a `--config` flag pointing to a YAML configuration file.

### Configuration File Structure

```yaml
# Registry name/identifier (optional, defaults to "default")
registryName: my-registry

# Data source configuration (required)
source:
  # Source type: git, configmap, api, or file
  type: git

  # Data format: toolhive (native) or upstream (MCP registry format)
  format: toolhive

  # Source-specific configuration
  git:
    repository: https://github.com/stacklok/toolhive.git
    branch: main
    path: pkg/registry/data/registry.json

# Automatic sync policy (required)
syncPolicy:
  # Sync interval (e.g., "30m", "1h", "24h")
  interval: "30m"

# Optional: Server filtering
filter:
  names:
    include: ["official/*"]
    exclude: ["*/deprecated"]
  tags:
    include: ["production"]
    exclude: ["experimental"]
```

### Command-line Flags

| Flag | Description | Required | Default |
|------|-------------|----------|---------|
| `--config` | Path to YAML configuration file | Yes | - |
| `--address` | Server listen address | No | `:8080` |

### Data Sources

The server supports four data source types:

1. **Git Repository** - Clone and sync from Git repositories
   - Supports branch, tag, or commit pinning
   - Ideal for version-controlled registries
   - Example: [config-git.yaml](examples/config-git.yaml)

2. **Kubernetes ConfigMap** - Read from ConfigMaps in the cluster
   - Requires Kubernetes API access and RBAC permissions
   - Ideal for Kubernetes-native deployments
   - Example: [config-configmap.yaml](examples/config-configmap.yaml)

3. **API Endpoint** - Sync from upstream MCP Registry APIs
   - Supports federation and aggregation scenarios
   - Format conversion from upstream to ToolHive format
   - Example: [config-api.yaml](examples/config-api.yaml)

4. **Local File** - Read from filesystem
   - Ideal for local development and testing
   - Supports mounted volumes in containers
   - Example: [config-file.yaml](examples/config-file.yaml)

For complete configuration examples and advanced options, see [examples/README.md](examples/README.md).

## Development

### Build Commands

```bash
# Build the binary
task build

# Run linting
task lint

# Fix linting issues automatically
task lint-fix

# Run tests
task test

# Generate mocks
task gen

# Build container image
task build-image
```

### Project Structure

```
cmd/thv-registry-api/
├── api/                 # REST API implementation
│   └── v1/              # API v1 handlers and routes
├── app/                 # CLI commands and application setup
├── internal/service/    # Legacy service layer (being refactored)
│   ├── file_provider.go     # File-based registry provider
│   ├── k8s_provider.go      # Kubernetes provider
│   └── service.go           # Core service implementation
└── main.go              # Application entry point

pkg/
├── config/              # Configuration loading and validation
├── sources/             # Data source handlers
│   ├── git.go               # Git repository source
│   ├── configmap.go         # Kubernetes ConfigMap source
│   ├── api.go               # API endpoint source
│   ├── file.go              # File system source
│   ├── factory.go           # Source handler factory
│   └── storage_manager.go   # Storage abstraction
├── sync/                # Sync manager and coordination
│   └── manager.go           # Background sync logic
└── status/              # Sync status tracking
    └── persistence.go       # Status file persistence

examples/                # Example configurations
```

### Architecture

The server follows a clean architecture pattern with the following layers:

1. **API Layer** (`cmd/thv-registry-api/api`): HTTP handlers implementing the MCP Registry API
2. **Service Layer** (`cmd/thv-registry-api/internal/service`): Legacy business logic (being refactored)
3. **Configuration Layer** (`pkg/config`): YAML configuration loading and validation
4. **Source Handler Layer** (`pkg/sources`): Pluggable data source implementations
   - `GitSourceHandler`: Clones Git repositories and extracts registry files
   - `ConfigMapSourceHandler`: Reads from Kubernetes ConfigMaps
   - `APISourceHandler`: Fetches from upstream MCP Registry APIs
   - `FileSourceHandler`: Reads from local filesystem
5. **Sync Manager** (`pkg/sync`): Coordinates automatic registry synchronization
6. **Storage Layer** (`pkg/sources`): Persists registry data to local storage
7. **Status Tracking** (`pkg/status`): Tracks and persists sync status

### Testing

The project uses table-driven tests with mocks generated via `go.uber.org/mock`:

```bash
# Generate mocks before testing
task gen

# Run all tests
task test
```

## Deployment

### Kubernetes

The Registry API is designed to run as a sidecar container alongside the ToolHive Operator's MCPRegistry controller. Example deployment:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: registry-api
spec:
  template:
    spec:
      containers:
      - name: registry-api
        image: ghcr.io/stacklok/toolhive/thv-registry-api:latest
        args:
        - serve
        - --config=/etc/registry/config.yaml
        ports:
        - containerPort: 8080
        volumeMounts:
        - name: config
          mountPath: /etc/registry
      volumes:
      - name: config
        configMap:
          name: registry-api-config
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: registry-api-config
data:
  config.yaml: |
    registryName: my-registry
    source:
      type: configmap
      format: toolhive
      configmap:
        namespace: toolhive-system
        name: my-registry-data
        key: registry.json
    syncPolicy:
      interval: "15m"
```

### Docker

```bash
# Build the image
task build-image

# Run with Git source
docker run -v $(pwd)/examples:/config \
  ghcr.io/stacklok/toolhive/thv-registry-api:latest \
  serve --config /config/config-git.yaml

# Run with file source (mount local registry file)
docker run -v $(pwd)/examples:/config \
  -v /path/to/registry.json:/data/registry.json \
  ghcr.io/stacklok/toolhive/thv-registry-api:latest \
  serve --config /config/config-file.yaml
```

## Integration with ToolHive

The Registry API server works seamlessly with the ToolHive ecosystem:

- **ToolHive Operator**: Automatically deployed as part of MCPRegistry resources

See the [ToolHive documentation](https://docs.stacklok.com/toolhive/) for more details.

## Contributing

We welcome contributions! Please see:

- [Contributing Guide](./CONTRIBUTING.md)
- [Code of Conduct](./CODE_OF_CONDUCT.md)
- [Security Policy](./SECURITY.md)

### Development Guidelines

- Run `task lint-fix` before committing
- Ensure tests pass with `task test`
- Follow Go standard project layout
- Use mockgen for test mocks, not hand-written mocks
- See [CLAUDE.md](./CLAUDE.md) for AI assistant guidance

## License

This project is licensed under the [Apache 2.0 License](./LICENSE).

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
