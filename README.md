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
- **Multiple backends**: Supports Kubernetes ConfigMaps and file-based registry data
- **Container-ready**: Designed for deployment in Kubernetes clusters
- **Flexible deployment**: Works standalone or as part of ToolHive infrastructure
- **Production-ready**: Built-in health checks, graceful shutdown, and basic observability

## Quick Start

### Prerequisites

- Go 1.23 or later
- [Task](https://taskfile.dev) for build automation
- Access to a Kubernetes cluster (for ConfigMap backend)

### Building the binary

```bash
# Build the binary
task registry-build
```

### Running the Server

**From a Kubernetes ConfigMap:**
```bash
thv-registry-api serve \
  --from-configmap my-registry-cm \
  --registry-name my-registry
```

**From a local file:**
```bash
thv-registry-api serve \
  --from-file /path/to/registry.json \
  --registry-name my-registry
```

The server starts on port 8080 by default. Use `--address :PORT` to customize.

## API Endpoints

The server implements the standard MCP Registry API:

- `GET /api/v0/servers` - List all available MCP servers
- `GET /api/v0/servers/{name}` - Get details for a specific server
- `GET /api/v0/deployed` - List deployed server instances (Kubernetes only)
- `GET /api/v0/deployed/{name}` - Get deployed instances of a specific server

See the [MCP Registry API specification](https://github.com/modelcontextprotocol/registry/blob/main/docs/reference/api/openapi.yaml) for full API details.
**Note**: The current implementation is not strictly compliant with the standard. The deviations will be fixed in the next iterations.

## Configuration

### Command-line Flags

The `thv-registry-api serve` command supports the following flags:

| Flag | Description | Required | Default |
|------|-------------|----------|---------|
| `--address` | Server listen address | No | `:8080` |
| `--from-configmap` | ConfigMap name containing registry data | Yes* | - |
| `--from-file` | Path to registry.json file | Yes* | - |
| `--registry-name` | Registry identifier | Yes | - |

*One of `--from-configmap` or `--from-file` must be specified (mutually exclusive)

### Backend Options

#### ConfigMap Backend
Fetches registry data from a Kubernetes ConfigMap. Requires:
- Kubernetes API access (in-cluster or via kubeconfig)
- ConfigMap with a `registry.json` key containing MCP registry data
- Appropriate RBAC permissions to read ConfigMaps

#### File Backend
Reads registry data from a local file. Useful for:
- Mounting ConfigMaps as volumes in Kubernetes
- Local development and testing
- Static registry deployments

## Development

### Build Commands

```bash
# Build the binary
task registry-build

# Run linting
task lint

# Fix linting issues automatically
task lint-fix

# Run tests
task registry-test

# Generate mocks
task gen

# Build container image
task registry-build-image
```

### Project Structure

```
cmd/thv-registry-api/
├── api/v1/              # REST API handlers and routes
├── app/                 # CLI commands and application setup
├── internal/service/    # Business logic and data providers
│   ├── file_provider.go     # File-based registry backend
│   ├── k8s_provider.go      # Kubernetes ConfigMap backend
│   ├── provider.go          # Provider interfaces
│   └── service.go           # Core service implementation
└── main.go              # Application entry point
```

### Architecture

The server follows a clean architecture pattern:

1. **API Layer** (`api/v1`): HTTP handlers implementing the MCP Registry API
2. **Service Layer** (`internal/service`): Business logic for registry operations
3. **Provider Layer**: Pluggable backends for registry data sources
   - `FileRegistryDataProvider`: Reads from local files
   - `K8sRegistryDataProvider`: Fetches from Kubernetes ConfigMaps
   - `K8sDeploymentProvider`: Queries deployed MCP server instances

### Testing

The project uses table-driven tests with mocks generated via `go.uber.org/mock`:

```bash
# Generate mocks before testing
task gen

# Run all tests
task registry-test
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
        - --from-configmap=my-registry
        - --registry-name=my-registry
        ports:
        - containerPort: 8080
```

### Docker

```bash
# Build the image
task registry-build-image

# Run with file backend
docker run -v /path/to/registry.json:/data/registry.json \
  ghcr.io/stacklok/toolhive/thv-registry-api:latest \
  serve --from-file /data/registry.json --registry-name my-registry
```

## Integration with ToolHive

The Registry API server works seamlessly with the ToolHive ecosystem:

- **ToolHive Operator**: Automatically deployed as part of MCPRegistry resources
- **ToolHive CLI**: Discovers and deploys servers from registry API endpoints
- **ToolHive Studio**: Provides UI access to available MCP servers

See the [ToolHive documentation](https://docs.stacklok.com/toolhive/) for more details.

## Contributing

We welcome contributions! Please see:

- [Contributing Guide](./CONTRIBUTING.md)
- [Code of Conduct](./CODE_OF_CONDUCT.md)
- [Security Policy](./SECURITY.md)

### Development Guidelines

- Run `task registry-lint-fix` before committing
- Ensure tests pass with `task registry-test`
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
