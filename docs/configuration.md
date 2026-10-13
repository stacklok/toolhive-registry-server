# Configuration Reference

Complete configuration reference for the ToolHive Registry API server.

## Table of Contents

- [Overview](#overview)
- [Configuration File Structure](#configuration-file-structure)
- [Sources and views](#source-configuration)
- [Data Sources](#data-sources)
- [Sync Policy](#sync-policy)
- [Filtering](#filtering)
- [Authentication](#authentication)
- [Database](#database)
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
| `--internal-address` | Internal probes/metrics listen address | No | `:8081` |
| `--initialize-empty` | Explicit empty-install authorization only; never approve existing data | No | `false` |
| `--auth-mode` | Override auth mode (anonymous or oauth) | No | - |

## Standalone Cedar admission (required)

Every standalone deployment evaluates Cedar. Omit `authorization.policyFile` (or
leave the optional path empty) to load built-in allow-all Cedar: every verified
identity may use **every enabled operation**, including administration if explicitly
enabled. This is actual policy evaluation, not an auth bypass. Admin remains off by
default and requires verified OAuth when enabled; anonymous catalog access requires
explicit opt-in. Invalid credentials never become anonymous; unknown/mismatched
routes and evaluation errors/diagnostics deny.

An optional absolute `authorization.policyFile` replaces the built-in policy entirely,
never concatenates with it. Missing/unreadable/empty/comment-only/invalid explicitly
configured files fail startup with no fallback. The bounded plain Cedar policy is
parsed and strictly schema-validated with pinned cedar-go v1.8.0 before migrations,
writers or serving. Effective auth configuration is validated after CLI overrides.
Policy changes take effect only after restarting every replica.

```yaml
admin:
  enabled: false
authorization:
  # Optional custom restriction; omit for evaluated built-in allow-all Cedar.
  # policyFile: /etc/registry/policies.cedar
  allowAnonymousCatalog: false
  identityMappings:
    - issuer: https://id.example.org
      groups:
        claim: groups
        format: string-array
```

Configure matching OAuth issuer/audience. `serve` selects `NewCatalogApp`: Cedar
is whole-instance admission, not a per-entry or per-view privacy filter. Remove
`auth.authz`, `auth.roles`, `auth.publicPaths`, and source/view `claims`; their
presence (even empty/null or environment-injected values) is rejected. Admin is
off by default and requires OAuth when enabled. OpenAPI requires `metadata.read`;
only exact GET credential-bootstrap metadata is exempt. Internal probes remain
on their separate port.

Every pre-existing catalog, including unlabeled data, requires the explicit
[offline inventory/acknowledgement/retirement procedure](registry-exposure-migration.md).
For a new, entirely empty database only, `serve --initialize-empty --config config.yaml`
explicitly authorizes empty-install preparation after final config, frozen policy
and read-only source/operator preflight. This default-false flag does not grant
policy permissions or approve existing data; it is safe to retain for same-target
replicas/restarts without resetting or rebinding the receipt.

See the [Cedar guide](registry-cedar-authorization.md) for typechecked policies,
identity mapping/introspection constraints, retirement tombstones and transition
instructions. Snippets below illustrate particular settings and must include the
trusted authentication configuration and explicit anonymous opt-in when needed to serve traffic.

Compose no longer requires an external policy mount. To use a custom policy, add a
Compose override and set the optional path in the selected config (or use
`THV_REGISTRY_AUTHORIZATION_POLICYFILE`):

```yaml
services:
  registry-api:
    volumes:
      - /absolute/operator/policies.cedar:/etc/registry/policies.cedar:ro
    environment:
      THV_REGISTRY_AUTHORIZATION_POLICYFILE: /etc/registry/policies.cedar
```

Keep the override/path identical throughout offline exposure review and finalization.

## Configuration File Structure

### Minimal Configuration

```yaml
sources:
  - name: default
    file:
      path: /data/registry.json

registries:
  - name: default
    sources: ["default"]

database:
  host: localhost
  port: 5432
  user: registry
  database: registry
```

### Complete Configuration

```yaml
# Sources define where registry data comes from
sources:
  - name: toolhive
    git:
      repository: https://github.com/stacklok/toolhive-catalog.git
      branch: main
      path: pkg/catalog/toolhive/data/registry-upstream.json
    syncPolicy:
      interval: "30m"
    filter:
      names:
        include: ["official/*"]
        exclude: ["*/deprecated"]
      tags:
        include: ["production"]
        exclude: ["experimental"]

# Registries aggregate one or more sources into a named view (required)
registries:
  - name: default
    sources: ["toolhive"]

# Authentication configuration (required)
auth:
  mode: oauth
  oauth:
    resourceUrl: https://registry.example.com
    providers:
      - name: my-idp
        issuerUrl: https://idp.example.com
        audience: api://registry

# Database configuration (required)
database:
  host: localhost
  port: 5432
  user: registry
  database: registry
  sslMode: require
  maxOpenConns: 25
  maxIdleConns: 5
  connMaxLifetime: "5m"
```

## Source Configuration

Sources define where registry data comes from. Multiple sources can be configured, each with its own data provider, sync policy, and filters.

### Top-Level Fields

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `sources` | array | Yes | - | List of data source configurations |
| `registries` | array | Yes | - | List of registry aggregations (at least one required) |

### Source Entry Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Unique name for this source |
| `git` | object | No* | Git repository configuration |
| `api` | object | No* | API endpoint configuration |
| `file` | object | No* | Local file configuration |
| `managed` | object | No* | Managed registry configuration |
| `kubernetes` | object | No* | Kubernetes resource configuration |
| `syncPolicy` | object | Yes† | Sync policy configuration |
| `filter` | object | No | Server filtering rules |
| `claims` | retired | Rejected by presence, including null; use the offline exposure guide |

\* Exactly one data source must be configured per source entry
† Required for synced sources (git, api, file); not applicable for managed or kubernetes sources

### Registry Entry Fields

The `registries:` block aggregates sources into named registry views. At least one registry must be configured.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Unique name for this registry |
| `sources` | array | Yes | Ordered list of source names that feed this registry |
| `claims` | retired | Rejected by presence, including null; use the offline exposure guide |

## Data Sources

### Git Repository

Clone and sync from Git repositories. Ideal for version-controlled registries.

```yaml
git:
  repository: https://github.com/stacklok/toolhive-catalog.git
  branch: main                    # Optional: defaults to default branch
  tag: v1.0.0                     # Optional: use specific tag
  commit: abc123                  # Optional: pin to specific commit
  path: pkg/catalog/toolhive/data/registry-upstream.json
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
  endpoint: https://registry.example.com
  timeout: 30s
```

**Fields:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `endpoint` | string | Yes | Base API URL (without path); the server appends MCP Registry API v0.1 paths automatically |
| `timeout` | string | No | Per-request HTTP timeout as a Go duration (e.g. `30s`, `1m`). Defaults to `10s`; must be greater than `0` and at most `5m`. Raise this for public or occasionally-slow upstreams where individual requests can take longer. |

**Supports:**
- Automatic background synchronization
- Per-registry filtering

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
  namespaces:                    # Optional: specific namespaces to watch
    - default
```

**Fields:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `namespaces` | array | No | Kubernetes namespaces to watch (empty = uses `THV_REGISTRY_WATCH_NAMESPACE` env var) |

**Retired annotations:** an export-opted-in CRD carrying
`toolhive.stacklok.dev/authz-claims` is rejected during read-only startup preflight,
including empty/null/malformed values. Explicitly remediate live objects before
exposure review/cutover; the new operator never parses or propagates resource claims.
Export still requires exact `toolhive.stacklok.dev/registry-export: "true"`.

**Per-entry version:** the entry version is resolved in this order:

1. the `toolhive.stacklok.dev/registry-version` annotation, when it holds a usable version
2. the container image tag, when it holds a usable version (`MCPServer` only —
   `VirtualMCPServer` and `MCPRemoteProxy` run no image, so the annotation is the only route)
3. `1.0.0`

A version is usable when it is a three-part semantic version — `MAJOR.MINOR.PATCH` with an
optional prerelease, optionally `v`-prefixed — matching what the upstream MCP registry
treats as semantic. Anything else falls through to the next option, because the entry
version is part of an entry's identity, appears as a URL path segment
(`GET .../versions/{version}`), and decides which version is served as `latest`.

Values that fall through include `latest`, channel tags such as `stable` or `main`, two-part
tags such as `1.2` or `20`, version ranges such as `1.x`, versions carrying `+build`
metadata, and anything longer than 255 characters. An unusable *annotation* is logged as a
warning naming the resource; an unusable image tag is not, since untagged and `latest`
images are routine.

Only the tag is read, never the digest — a digest does not order and changes on every
rebuild. An image pinned as `db:1.4.2@sha256:...` therefore resolves to `1.4.2`, while an
image pinned by digest alone resolves to `1.0.0`. A colon in a registry host is not
mistaken for a tag, so `registry.internal:5000/db` is treated as untagged.

```yaml
# Example CRD annotation pinning the entry version:
metadata:
  annotations:
    toolhive.stacklok.dev/registry-version: '2.5.0'
```

**Features:**
- Watches export-opted-in MCPServer, VirtualMCPServer and MCPRemoteProxy resources
- Periodically reconciles full-scope snapshots through fenced database leases
- Requires in-cluster service account or kubeconfig; consumer reads use persisted data

**Does NOT support:**
- Sync policy configuration
- Filtering configuration

## Sync Policy

Controls automatic background synchronization for Git, API, and File registries.

```yaml
syncPolicy:
  interval: "30m"                # Sync interval
```

**Fields:**

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `interval` | string | Yes | - | Sync interval (e.g., "30m", "1h", "24h") |

**Not applicable for:**
- Managed registries (no sync)
- Kubernetes sources (watch plus periodic reconciliation, not configured by `syncPolicy`)

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
- Kubernetes sources (selection uses namespace scope and export opt-in annotations)

## Authentication

See detailed [Authentication Guide](authentication.md).

### Quick Reference

```yaml
auth:
  mode: oauth                    # "oauth" (default) or "anonymous"
  # publicPaths is rejected; metrics/probes use the internal listener.
  oauth:
    resourceUrl: https://registry.example.com
    realm: mcp-registry          # Optional
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
  migrationUser: registry_migrator            # Optional
  database: toolhive_registry
  sslMode: require                            # disable, require, verify-ca, verify-full
  maxOpenConns: 25                            # Optional
  maxIdleConns: 5                             # Optional
  connMaxLifetime: "5m"                       # Optional
```

**Password management:**
Passwords can be provided via `THV_REGISTRY_DATABASE_PASSWORD` / `THV_REGISTRY_DATABASE_MIGRATIONPASSWORD` environment variables, the `password` / `migrationPassword` config fields, or PostgreSQL's pgpass file (`~/.pgpass` or `$PGPASSFILE`). See [Database Configuration](database.md#password-security) for details.

## Environment Variables

Configuration values can be overridden using environment variables with the `THV_REGISTRY_` prefix. For complete documentation, see [Environment Variables Guide](environment-variables.md).

### Common Configuration Overrides

| Variable | Description |
|----------|-------------|
| `THV_REGISTRY_DATABASE_HOST` | Override database host |
| `THV_REGISTRY_DATABASE_PORT` | Override database port |
| `THV_REGISTRY_DATABASE_USER` | Override database user |
| `THV_REGISTRY_AUTH_MODE` | Override authentication mode (anonymous or oauth) |
| `THV_REGISTRY_LOG_LEVEL` | Set log level (debug, info, warn, error) |

### Database Passwords

| Variable | Description |
|----------|-------------|
| `THV_REGISTRY_DATABASE_PASSWORD` | Database password for the application user |
| `THV_REGISTRY_DATABASE_MIGRATIONPASSWORD` | Database password for the migration user |
| `PGPASSFILE` | Path to pgpass file (default: `~/.pgpass`) — used as fallback when password env vars are not set |

See [Database Configuration](database.md#password-security) for password management details.

### Other

| Variable | Description |
|----------|-------------|
| `CONFIG_FILE` | Compose-only filename under `/examples`; direct CLI uses `--config` |
| `THV_REGISTRY_INSECURE_URL` | Allow HTTP URLs for development (default: false) |

## Examples

### Development (Local File)

Save the following as `config-local.yaml`. Bind **both** standalone listeners to
loopback explicitly; the CLI defaults listen on all interfaces, and anonymous
mode does not restrict the bind address:

```sh
thv-registry-api serve --config config-local.yaml \
  --address 127.0.0.1:8080 --internal-address 127.0.0.1:8081
```

For a genuinely new empty database only, add `--initialize-empty`; existing data
requires the offline exposure procedure before serving.

```yaml
sources:
  - name: local
    file:
      path: ./examples/upstream-registry.json
    syncPolicy:
      interval: "5m"
registries:
  - name: default
    sources: [local]
admin:
  enabled: false
authorization:
  allowAnonymousCatalog: true
  identityMappings: []
auth:
  mode: anonymous # Loopback-only development, with built-in Cedar permit.

database:
  host: localhost
  port: 5432
  user: registry
  database: registry
  sslMode: disable
```

### Production (Git + Database + OAuth)

```yaml
sources:
  - name: toolhive
    git:
      repository: https://github.com/stacklok/toolhive-catalog.git
      branch: main
      path: pkg/catalog/toolhive/data/registry-upstream.json
    syncPolicy:
      interval: "15m"
    filter:
      names:
        include: ["official/*"]
      tags:
        exclude: ["experimental"]

registries:
  - name: default
    sources: ["toolhive"]

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
```

### Multi-Registry Setup

```yaml
sources:
  # Official ToolHive registry
  - name: toolhive
    git:
      repository: https://github.com/stacklok/toolhive-catalog.git
      branch: main
      path: pkg/catalog/toolhive/data/registry-upstream.json
    syncPolicy:
      interval: "30m"

  # Company internal registry
  - name: internal
    api:
      endpoint: https://internal-registry.company.com
    syncPolicy:
      interval: "1h"
    filter:
      tags:
        include: ["approved"]

  # Managed registry for custom servers
  - name: custom
    managed: {}

  # Kubernetes-deployed servers
  - name: k8s-deployed
    kubernetes:
      namespaces:
        - mcp-servers

registries:
  - name: default
    sources: ["toolhive", "internal", "custom", "k8s-deployed"]

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
        caCertPath: /var/run/secrets/kubernetes.io/serviceaccount/ca.crt
        allowPrivateIP: true # Explicit trust of this private cluster issuer.

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

Enable sanitized debug logging with `THV_REGISTRY_LOG_LEVEL=debug`:

```bash
THV_REGISTRY_LOG_LEVEL=debug thv-registry-api serve --config config.yaml
```

## See Also

- [Database Configuration](database.md) - Detailed database setup
- [Authentication](authentication.md) - OAuth and security
- [Kubernetes Deployment](deployment-kubernetes.md) - Kubernetes configuration examples
- [Docker Deployment](deployment-docker.md) - Docker configuration examples
- [examples/](../examples/) - Complete configuration examples