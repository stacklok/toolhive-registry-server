# Configuration Reference

Complete configuration reference for the ToolHive Registry API server.

## Table of Contents

- [Overview](#overview)
- [Configuration File Structure](#configuration-file-structure)
- [Registry Configuration](#registry-configuration)
- [Data Sources](#data-sources)
- [Sync Policy](#sync-policy)
- [Filtering](#filtering)
- [Settings Normalized for v1.5.4 Compatibility](#settings-normalized-for-v154-compatibility)
- [Conflicts with API-Created Sources and Registries](#conflicts-with-api-created-sources-and-registries)
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
| `--auth-mode` | Override auth mode (anonymous or oauth) | No | - |

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
| `claims` | map | No | Key-value pairs for authorization purposes |

\* Exactly one data source must be configured per source entry
† Required for synced sources (git, api, file); not applicable for managed or kubernetes sources

### Registry Entry Fields

The `registries:` block aggregates sources into named registry views. At least one registry must be configured.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Unique name for this registry |
| `sources` | array | Yes | Ordered list of source names that feed this registry. A source listed more than once keeps the position of its last listing, with a warning (see [normalization](#settings-normalized-for-v154-compatibility)). |
| `claims` | map | No | Key-value pairs for authorization purposes |

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
| `commit` | string | No | Commit SHA (mutually exclusive with branch/tag). A source that sets more than one of `branch`, `tag` and `commit` starts, but every sync of it fails, as in v1.5.4 (see [normalization](#settings-normalized-for-v154-compatibility)). |
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
| `timeout` | string | No | Per-request HTTP timeout as a Go duration (e.g. `30s`, `1m`). Defaults to `10s`; must be greater than `0` and at most `5m`. Raise this for public or occasionally-slow upstreams where individual requests can take longer. The timeout applies to each request (the probe and every page) separately; a paginated sync as a whole has no time limit. |

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
| `path` | string | No* | Absolute path to registry JSON file |
| `url` | string | No* | HTTP(S) URL of the registry JSON file |
| `timeout` | string | No | Per-request HTTP timeout for `url` (Go duration). Defaults to `30s`. Ignored, with a warning, on a `path` source; `0s` means `10s` and a negative value means the default (see [normalization](#settings-normalized-for-v154-compatibility)). |

\* Exactly one of `path` and `url`.

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
| `namespaces` | array | No | Kubernetes namespaces to watch (empty = uses `THV_REGISTRY_WATCH_NAMESPACE` env var; neither = all namespaces). Each must be a valid namespace name |

The same applies to Kubernetes sources created or updated through the admin API
(`PUT /v1/sources/{name}`): no `namespaces` means `THV_REGISTRY_WATCH_NAMESPACE`, an
invalid namespace name is rejected with 400, and the server must be able to list the
toolhive resources in the scope, or the request is rejected with 400 naming the kind,
namespace and Kubernetes reason.

If the server cannot list a config file's Kubernetes source at startup (missing RBAC
permission, CRDs not installed, API server unreachable), it logs the source, kind,
namespace and reason and starts anyway, serving every other source; the Kubernetes
source keeps retrying in the background with backoff, logging each failure. One source's
failure (including a stored definition the server cannot use, which is recorded as a
failed sync of that source) never stops sync for the other sources.

**Per-entry claims:** CRDs can carry per-entry authorization claims via the
`toolhive.stacklok.dev/authz-claims` JSON annotation. The annotation value uses the same
`map[string]any` format as source claims (strings or arrays of strings). Source claims are
**not** merged: an entry with the annotation gets exactly the claims from the annotation,
including `{}` or `null` as written. An entry without the annotation gets the source's
claims. A malformed annotation skips that resource with a warning.

```yaml
# Example CRD annotation for per-entry claims:
metadata:
  annotations:
    toolhive.stacklok.dev/authz-claims: '{"team": "platform"}'
```

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

**Package URL:** a network package (any transport other than `stdio`) without a URL gets
the `toolhive.stacklok.dev/registry-url` annotation's value as its `transport.url`, because
the upstream server schema requires one. v1.5.4 left `transport.url` out of these packages.
A cluster status address is never used.

**Features:**
- Watches exported resources and syncs when one changes; re-lists every 5 minutes
- Reports its real sync status and server count (see [Registry Sync](registry-sync.md#kubernetes-sources))
- Requires in-cluster service account or kubeconfig

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
| `interval` | string | Yes | - | Sync interval (e.g., "30m", "1h", "24h"). Zero or negative means `2m`; below `1s` means `1s` (see [normalization](#settings-normalized-for-v154-compatibility)). |

The source is fetched once per interval. A failed sync is retried after two minutes, or after
the interval if that is shorter. See [Registry Sync](registry-sync.md#when-sync-is-triggered).

**Not applicable for** (ignored with a warning if set):
- Managed registries (no sync)
- Kubernetes registries (sync on watch events)
- Inline `file.data` sources (sync when their data changes)

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
- An invalid pattern does not stop startup; it selects what it selected in v1.5.4, which
  usually means the source is emptied (see [normalization](#settings-normalized-for-v154-compatibility)).

**Not applicable for:**
- Managed registries (controlled via API)
- Kubernetes registries (use labelSelector instead)

## Settings Normalized for v1.5.4 Compatibility

v1.5.4 accepted some source settings that the current sync runtime cannot use as written. A
config that started on v1.5.4 still starts: each such setting is rewritten to what v1.5.4
effectively did (or the nearest value the runtime supports) and a warning naming the source is
logged. Settings sent to the admin API (`PUT /v1/sources/{name}`) that v1.5.4 accepted are
rewritten the same way before they are stored, so `GET /v1/sources` shows the effective value.
Settings that v1.5.4's admin API rejected are still rejected there.

| Setting | v1.5.4 behavior | Now | Log level | Applies to |
|---------|-----------------|-----|-----------|------------|
| `syncPolicy.interval` zero or negative | Synced on every coordinator tick (every 2 minutes) | Interval `2m` | WARN | Config, admin API |
| `syncPolicy.interval` above zero and below `1s` | Synced every 2 minutes at most | Interval `1s`, the runtime's polling granularity | WARN | Config, admin API |
| `syncPolicy.interval` with sub-microsecond precision | Truncated by the database interval column | Truncated to microseconds | WARN | Config, admin API |
| `file.timeout` on a `file.path` source | Ignored | Ignored (dropped) | WARN | Config (the admin API rejects it, as v1.5.4 did) |
| `file.timeout: 0s` on a `file.url` source | Its HTTP client replaced 0 with its 10-second default | Timeout `10s` | WARN | Config, admin API |
| Negative `file.timeout` on a `file.url` source | No request timeout | The default timeout (`30s`); a timeout can no longer be disabled | WARN | Config, admin API |
| Git source with more than one of `branch`, `tag`, `commit` | Started, but every sync failed with `only one of branch, tag, or commit may be specified` | Same: the source is stored (keeping one ref, in the order commit, branch, tag) and every sync fails with that error, keeping previously synced data, until only one ref is set | WARN | Config (the admin API rejects it, as v1.5.4 did) |
| Registry listing a source more than once | Each later listing overwrote the earlier position | Each source keeps the position of its last listing: `[a, b, a]` becomes `[b, a]` | WARN | Config (the admin API rejects it, as v1.5.4 did) |
| Invalid `filter.names` glob pattern | Patterns were tried in order and an invalid one counted as a non-match that ended the search | An invalid `exclude` pattern excludes every entry. An invalid `include` pattern keeps only the include patterns before it; when it is the first, every entry is excluded | ERROR | Config, admin API |

Fix the setting to silence the warning. The invalid-glob case is logged as an error because it
usually empties the source.

Sources that v1.5.4's admin API already stored in the database get the same rewrites once, at
the first start after the upgrade. After config sources are applied, the server looks for
API-created sources that cannot be synced because of a zero or negative `syncPolicy.interval`, a
`file.timeout` the runtime refuses, or an invalid `filter.names` pattern. It rewrites those settings
as in the table above and stores the result, keeping the source's claims and its other settings.
The source then syncs again with the new settings. For each rewritten source it logs a WARN,
`Normalized stored settings of an API-created source that v1.5.4 accepted but cannot be synced as
stored`, with the source name and the changes. A rewritten source is valid, so later starts leave
it alone, and `GET /v1/sources` shows the new values. A stored source that is unusable for any
other reason, or still unusable after the rewrite, is left unchanged. The sync runtime logs and
skips it, as before (`Skipping source with an invalid definition`). Update or recreate it through
the admin API.

## Conflicts with API-Created Sources and Registries

The config file and the admin API own separate sources and registries. When the config
conflicts with what the admin API created, startup does not fail. The conflicts are:

- a config source with the same name as an API-created source;
- a config registry with the same name as an API-created registry;
- a config that removes a source that an API-created registry still uses;
- a config managed source while an API-created managed source exists;
- a config source that changes to or from `managed`.

The server logs an error naming the sources and registries involved (`Config sources and
registries were not applied because they conflict with the database; ...`) and applies none of
the config's sources and registries: it keeps serving, and keeps syncing, the definitions already
stored. Config claims are applied only to config sources the database already holds with the same
type, and to config-owned registries; they are never written to an API-created source or
registry. Readiness is not affected. Resolve the conflict (rename or remove the config item, or
delete the API-created one) and restart to apply the config.

v1.5.4 also kept serving after such a conflict, but stopped syncing every source.

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
| `CONFIG_FILE` | Override config file path |
| `THV_REGISTRY_INSECURE_URL` | Allow HTTP URLs for development (default: false) |

## Examples

### Development (Local File)

```yaml
sources:
  - name: local
    file:
      path: ./examples/registry-sample.json

auth:
  mode: anonymous

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
- **Invalid filter patterns**: Startup continues; see [normalization](#settings-normalized-for-v154-compatibility)
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