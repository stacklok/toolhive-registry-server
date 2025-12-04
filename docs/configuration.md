# Configuration Reference

Complete configuration reference for the ToolHive Registry API server.

## Table of Contents

- [Overview](#overview)
- [Configuration File Structure](#configuration-file-structure)
- [Registry Configuration](#registry-configuration)
- [Data Sources](#data-sources)
- [Sync Policy](#sync-policy)
- [Filtering](#filtering)
- [Authentication](#authentication)
- [Database](#database)
- [File Storage](#file-storage)
- [Environment Variables](#environment-variables)
- [Examples](#examples)

## Overview

All configuration is done via YAML files. The server requires a `--config` flag pointing to a configuration file.

```bash
thv-registry-api serve --config config.yaml
```

### Command-line Flags

| Flag | Description | Required | Default |
|------|-------------|----------|---------|
| `--config` | Path to YAML configuration file | Yes | - |
| `--address` | Server listen address | No | `:8080` |
| `--auth-mode` | Override auth mode (anonymous or oauth) | No | - |

## Configuration File Structure

### Minimal Configuration

```yaml
registryName: my-registry

registries:
  - name: default
    format: toolhive
    file:
      path: /data/registry.json
```

### Complete Configuration

```yaml
# Registry name/identifier (optional, defaults to "default")
registryName: my-registry

# Registries configuration - multiple registries can be configured
registries:
  - name: toolhive
    format: toolhive
    git:
      repository: https://github.com/stacklok/toolhive.git
      branch: main
      path: pkg/registry/data/registry.json
    syncPolicy:
      interval: "30m"
    filter:
      names:
        include: ["official/*"]
        exclude: ["*/deprecated"]
      tags:
        include: ["production"]
        exclude: ["experimental"]

# Authentication configuration (optional, defaults to OAuth)
auth:
  mode: oauth
  oauth:
    resourceUrl: https://registry.example.com
    providers:
      - name: my-idp
        issuerUrl: https://idp.example.com
        audience: api://registry

# Database configuration (optional)
database:
  host: localhost
  port: 5432
  user: registry
  database: registry
  sslMode: require
  maxOpenConns: 25
  maxIdleConns: 5
  connMaxLifetime: "5m"

# File storage configuration (optional)
fileStorage:
  baseDir: ./data
```

## Registry Configuration

Multiple registries can be configured, each with its own data source and settings.

### Top-Level Fields

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `registryName` | string | No | `default` | Global registry name identifier |
| `registries` | array | Yes | - | List of registry configurations |

### Registry Entry Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Unique name for this registry |
| `format` | string | Yes | Data format: `toolhive` or `upstream` |
| `git` | object | No* | Git repository configuration |
| `api` | object | No* | API endpoint configuration |
| `file` | object | No* | Local file configuration |
| `managed` | object | No* | Managed registry configuration |
| `kubernetes` | object | No* | Kubernetes resource configuration |
| `syncPolicy` | object | No | Sync policy configuration |
| `filter` | object | No | Server filtering rules |

\* Exactly one data source must be configured per registry

## Data Sources

### Git Repository

Clone and sync from Git repositories. Ideal for version-controlled registries.

```yaml
git:
  repository: https://github.com/stacklok/toolhive.git
  branch: main                    # Optional: defaults to default branch
  tag: v1.0.0                     # Optional: use specific tag
  commit: abc123                  # Optional: pin to specific commit
  path: pkg/registry/data/registry.json
  auth:                           # Optional: for private repos
    username: user
    passwordFile: /secrets/git-password
```

**Fields:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `repository` | string | Yes | Git repository URL (HTTPS or SSH) |
| `branch` | string | No | Branch name (default: default branch) |
| `tag` | string | No | Tag name (mutually exclusive with branch/commit) |
| `commit` | string | No | Commit SHA (mutually exclusive with branch/tag) |
| `path` | string | Yes | Path to registry JSON file within repo |
| `auth.username` | string | No | Git username for private repos |
| `auth.passwordFile` | string | No | Path to file containing Git password/token |

**Supports:**
- Automatic background synchronization
- Per-registry filtering
- Branch, tag, or commit pinning

### API Endpoint

Sync from upstream MCP Registry APIs. Ideal for federation scenarios.

```yaml
api:
  url: https://registry.example.com/v0.1
  headers:                        # Optional: custom headers
    Authorization: Bearer token
  timeout: 30s                    # Optional: request timeout
```

**Fields:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `url` | string | Yes | API endpoint URL |
| `headers` | map | No | Custom HTTP headers |
| `timeout` | string | No | Request timeout (default: "30s") |

**Supports:**
- Automatic background synchronization
- Per-registry filtering
- Format conversion (upstream → toolhive)

### Local File

Read from local filesystem. Ideal for development and testing.

```yaml
file:
  path: /data/registry.json
```

**Fields:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `path` | string | Yes | Absolute path to registry JSON file |

**Supports:**
- Automatic background synchronization (monitors file changes)
- Per-registry filtering

### Managed

Directly managed via API. No external data source.

```yaml
managed: {}
```

**Features:**
- Create, update, and delete servers through API calls
- No background synchronization
- Requires database backend
- Ideal for custom, dynamically-managed registries

**Does NOT support:**
- Sync policy configuration
- Filtering configuration

### Kubernetes

Discover MCP servers from Kubernetes deployments.

```yaml
kubernetes:
  namespace: default             # Optional: specific namespace
  labelSelector:                 # Optional: filter by labels
    app: mcp-server
```

**Fields:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `namespace` | string | No | Kubernetes namespace (empty = all namespaces) |
| `labelSelector` | map | No | Label selector for filtering pods |

**Features:**
- Queries running Kubernetes resources
- No background synchronization (on-demand only)
- Requires in-cluster service account or kubeconfig

**Does NOT support:**
- Sync policy configuration
- Filtering configuration

## Sync Policy

Controls automatic background synchronization for Git, API, and File registries.

```yaml
syncPolicy:
  interval: "30m"                # Sync interval
  retryInterval: "5m"            # Retry interval on failure
  retryLimit: 3                  # Maximum retry attempts
```

**Fields:**

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `interval` | string | No | `30m` | Sync interval (e.g., "30m", "1h", "24h") |
| `retryInterval` | string | No | `5m` | Retry interval on sync failure |
| `retryLimit` | int | No | `3` | Maximum number of retry attempts |

**Not applicable for:**
- Managed registries (no sync)
- Kubernetes registries (no sync)

## Filtering

Filter which servers are exposed from a registry.

```yaml
filter:
  names:
    include: ["official/*", "approved/*"]
    exclude: ["*/deprecated", "*/test"]
  tags:
    include: ["production", "stable"]
    exclude: ["experimental", "beta"]
```

**Fields:**

| Field | Type | Description |
|-------|------|-------------|
| `names.include` | array | Include servers matching these patterns (glob) |
| `names.exclude` | array | Exclude servers matching these patterns (glob) |
| `tags.include` | array | Include servers with these tags |
| `tags.exclude` | array | Exclude servers with these tags |

**Behavior:**
1. If `include` is specified, only matching servers are included
2. `exclude` is then applied to remove servers
3. Both name patterns and tags are evaluated
4. Empty filter = no filtering (all servers included)

**Pattern matching:**
- Uses glob patterns (wildcards: `*`, `?`, `[...]`)
- Examples: `official/*`, `company/*/stable`, `*-prod`

**Not applicable for:**
- Managed registries (controlled via API)
- Kubernetes registries (use labelSelector instead)

## Authentication

See detailed [Authentication Guide](authentication.md).

### Quick Reference

```yaml
auth:
  mode: oauth                    # "oauth" (default) or "anonymous"
  publicPaths:                   # Optional: additional public paths
    - /metrics
  oauth:
    resourceUrl: https://registry.example.com
    realm: mcp-registry          # Optional
    scopesSupported:             # Optional
      - mcp-registry:read
      - mcp-registry:write
    providers:
      - name: my-idp
        issuerUrl: https://idp.example.com
        audience: api://registry
        clientId: client-id      # Optional
        clientSecretFile: /secrets/secret  # Optional
        caCertPath: /certs/ca.crt  # Optional
```

## Database

See detailed [Database Guide](database.md).

### Quick Reference

```yaml
database:
  host: localhost
  port: 5432
  user: registry_app
  passwordFile: /secrets/db-password          # Optional
  migrationUser: registry_migrator            # Optional
  migrationPasswordFile: /secrets/db-migration-password  # Optional
  database: toolhive_registry
  sslMode: require                            # disable, require, verify-ca, verify-full
  maxOpenConns: 25                            # Optional
  maxIdleConns: 5                             # Optional
  connMaxLifetime: "5m"                       # Optional
```

**Password priority:**
1. Environment variable (`THV_DATABASE_PASSWORD`)
2. pgpass file (`~/.pgpass` or `$PGPASSFILE`)
3. `passwordFile` in config

## File Storage

Configure local file storage for registry data.

```yaml
fileStorage:
  baseDir: ./data                # Base directory for file storage
```

**Fields:**

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `baseDir` | string | No | `./data` | Base directory for storing sync data |

## Environment Variables

Configuration values can be overridden or supplemented with environment variables.

### Database Passwords

| Variable | Description |
|----------|-------------|
| `THV_DATABASE_PASSWORD` | Database password for normal operations |
| `THV_DATABASE_MIGRATION_PASSWORD` | Database password for migrations |

### PostgreSQL Standard

| Variable | Description |
|----------|-------------|
| `PGPASSFILE` | Path to pgpass file (default: `~/.pgpass`) |
| `PGHOST` | Database host |
| `PGPORT` | Database port |
| `PGDATABASE` | Database name |
| `PGUSER` | Database user |

### Other

| Variable | Description |
|----------|-------------|
| `CONFIG_FILE` | Override config file path |

## Examples

### Development (Local File)

```yaml
registryName: dev-registry

registries:
  - name: local
    format: toolhive
    file:
      path: ./examples/registry-sample.json

auth:
  mode: anonymous

fileStorage:
  baseDir: ./data
```

### Production (Git + Database + OAuth)

```yaml
registryName: prod-registry

registries:
  - name: toolhive
    format: toolhive
    git:
      repository: https://github.com/stacklok/toolhive.git
      branch: main
      path: pkg/registry/data/registry.json
    syncPolicy:
      interval: "15m"
      retryLimit: 5
    filter:
      names:
        include: ["official/*"]
      tags:
        exclude: ["experimental"]

auth:
  mode: oauth
  oauth:
    resourceUrl: https://registry.company.com
    providers:
      - name: company-sso
        issuerUrl: https://auth.company.com
        audience: api://toolhive-registry

database:
  host: postgres.production.svc.cluster.local
  port: 5432
  user: registry_app
  migrationUser: registry_migrator
  database: toolhive_registry
  sslMode: verify-full
  maxOpenConns: 50
  maxIdleConns: 10
  connMaxLifetime: "1h"

fileStorage:
  baseDir: /var/lib/registry
```

### Multi-Registry Setup

```yaml
registryName: multi-registry

registries:
  # Official ToolHive registry
  - name: toolhive
    format: toolhive
    git:
      repository: https://github.com/stacklok/toolhive.git
      branch: main
      path: pkg/registry/data/registry.json
    syncPolicy:
      interval: "30m"

  # Company internal registry
  - name: internal
    format: upstream
    api:
      url: https://internal-registry.company.com/v0.1
      headers:
        Authorization: Bearer ${INTERNAL_REGISTRY_TOKEN}
    syncPolicy:
      interval: "1h"
    filter:
      tags:
        include: ["approved"]

  # Managed registry for custom servers
  - name: custom
    format: toolhive
    managed: {}

  # Kubernetes-deployed servers
  - name: k8s-deployed
    format: toolhive
    kubernetes:
      namespace: mcp-servers
      labelSelector:
        environment: production

auth:
  mode: oauth
  oauth:
    resourceUrl: https://registry.company.com
    providers:
      - name: company-sso
        issuerUrl: https://auth.company.com
        audience: api://registry
      - name: kubernetes
        issuerUrl: https://kubernetes.default.svc
        audience: https://kubernetes.default.svc

database:
  host: postgres
  port: 5432
  user: registry
  database: registry
```

## Validation

The configuration is validated on startup. Common validation errors:

- **Missing required fields**: Ensure all required fields are present
- **Invalid data source**: Exactly one data source must be configured per registry
- **Invalid duration format**: Use format like "30m", "1h", "24h"
- **Invalid auth mode**: Must be "anonymous" or "oauth"
- **Missing OAuth providers**: At least one provider required when mode is "oauth"
- **Invalid filter patterns**: Check glob pattern syntax
- **Database connection**: Verify connection parameters

Run with `--debug` flag for detailed validation output:

```bash
thv-registry-api serve --config config.yaml --debug
```

## See Also

- [Database Configuration](database.md) - Detailed database setup
- [Authentication](authentication.md) - OAuth and security
- [Kubernetes Deployment](deployment-kubernetes.md) - Kubernetes configuration examples
- [Docker Deployment](deployment-docker.md) - Docker configuration examples
- [examples/](../examples/) - Complete configuration examples