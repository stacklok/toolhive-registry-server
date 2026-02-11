# ToolHive Registry API - Configuration Examples

This directory contains sample configuration files demonstrating automatic registry synchronization from different data sources.

## Quick Start

**Choose your data source:**

| Source | Use Case | Config File | Sync Interval |
|--------|----------|-------------|---------------|
| **Git** | Official registries, version control | [config-git.yaml](config-git.yaml) | 30m |
| **API** | Upstream aggregation, federation | [config-api.yaml](config-api.yaml) | 1h |
| **File** | Local development, testing | [config-file.yaml](config-file.yaml) | 5m |

**Start the server with sync:**

```bash
# Git source (recommended for getting started)
thv-registry-api serve --config examples/config-git.yaml

# API source (upstream MCP registry)
thv-registry-api serve --config examples/config-api.yaml

# File source (local development)
thv-registry-api serve --config examples/config-file.yaml
```

**Verify sync is working:**

```bash
# Query the API
curl http://localhost:8080/registry/v0.1/servers | jq

# Check health
curl http://localhost:8080/health
```

---

## Configuration Files

### 1. Git Repository Source

**File:** [config-git.yaml](config-git.yaml)

Syncs from the official ToolHive Git repository.

**Configuration:**
```yaml
registryName: toolhive

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

**What happens when you start:**
1. Background sync coordinator starts immediately
2. Clones `https://github.com/stacklok/toolhive.git` (shallow, depth=1)
3. Extracts `pkg/registry/data/registry.json` from the `main` branch
4. Stores synced data in the PostgreSQL database
5. Repeats every 30 minutes

**Best for:**
- Using official ToolHive registry data
- Version-controlled registry sources
- Multi-environment deployments (use different branches)
- Pinning to specific tags/commits for stability

**Options:**
- Use `tag: v1.0.0` instead of `branch` to pin to a release
- Use `commit: abc123` to pin to exact commit
- Change `interval` to control sync frequency

---

### 2. API Endpoint Source

**File:** [config-api.yaml](config-api.yaml)

Syncs from another MCP Registry API endpoint (like the official upstream registry).

**Configuration:**
```yaml
registryName: mcp-registry

registries:
  - name: mcp-upstream
    format: upstream
    api:
      endpoint: https://registry.modelcontextprotocol.io
    syncPolicy:
      interval: "1h"
```

**What happens when you start:**
1. Makes HTTP GET to `https://registry.modelcontextprotocol.io/registry/v0.1/servers`
2. Converts from upstream MCP format to ToolHive format
3. Stores synced data in the PostgreSQL database
4. Repeats every hour (less frequent to be respectful of external APIs)

**Best for:**
- Aggregating multiple registry sources
- Consuming official MCP registry data
- Creating curated/filtered subsets
- Registry federation scenarios

**Format Structure:**
The upstream format uses a wrapper structure with `version`, `meta`, and `data` sections (see `examples/upstream-registry.json`).
Each server follows the MCP 2025-10-17 schema with `$schema`, `packages[]`, and `_meta` extensions for ToolHive-specific metadata (tier, status, tools, etc.).

---

### 3. File Source

**File:** [config-file.yaml](config-file.yaml)

Reads registry data from a local file on the filesystem.

**Configuration:**
```yaml
registryName: toolhive

registries:
  - name: local-file
    format: toolhive
    file:
      path: ./data/registry.json
    syncPolicy:
      interval: "5m"
```

**What happens when you start:**
1. Reads registry data from the specified file path
2. Validates the JSON data structure
3. Stores synced data in the PostgreSQL database
4. Repeats every 5 minutes to detect file changes

**Best for:**
- Local development and testing
- Reading from mounted volumes in containers
- Using pre-generated registry files
- Quick prototyping without external dependencies

**Note:** The sync manager detects if the file has changed by comparing content hashes, so unchanged files won't trigger a database write.

---

### 4. Complete Reference

**File:** [config-complete.yaml](config-complete.yaml)

Comprehensive example showing **all** available configuration options with detailed comments.

Use this as a reference when you need to:
- Understand all available options
- Configure advanced filtering
- See examples of every source type
- Learn about data formats

---

## Configuration Structure

All config files follow this structure:

```yaml
# Registry name/identifier (optional, defaults to "default")
registryName: <name>

# Registries configuration (can have multiple registries)
registries:
  - name: <registry-name>
    # Data format: toolhive (native) or upstream (MCP registry format)
    format: <toolhive|upstream>

    # Source-specific config (one of: git, api, file, managed)
    git:
      repository: <url>
      branch: <name>      # OR tag: <name> OR commit: <sha>
      path: <file-path>

    api:
      endpoint: <base-url>

    file:
      path: <file-path>

    managed: {}  # For API-managed registries (no sync)

    # Per-registry sync policy (required except for managed registries)
    syncPolicy:
      interval: <duration>  # e.g., "30m", "1h", "24h"

    # Optional: Per-registry filter
    filter:
      names:
        include: [<glob-patterns>]
        exclude: [<glob-patterns>]
      tags:
        include: [<tag-names>]
        exclude: [<tag-names>]
```

---

## Customization Guide

### Sync Frequency

Choose based on your source and needs:

```yaml
syncPolicy:
  interval: "5m"   # Development/testing - very frequent
  interval: "30m"  # Git sources - balance freshness vs load
  interval: "1h"   # API sources - respectful rate limiting
  interval: "6h"   # Stable sources - infrequent updates
```

### Filtering Servers

Include/exclude specific servers:

```yaml
filter:
  # Name-based filtering (glob patterns)
  names:
    include:
      - "official/*"      # Only official namespace
      - "myorg/*"         # Your organization
    exclude:
      - "*/deprecated"    # Skip deprecated
      - "*/internal"      # Skip internal-only
      - "*/test"          # Skip test servers

  # Tag-based filtering (exact matches)
  tags:
    include:
      - "production"      # Only production-ready
      - "verified"        # Only verified servers
    exclude:
      - "experimental"    # Skip experimental
      - "beta"            # Skip beta versions
```

**Filter logic:**
1. Name include patterns (empty = include all)
2. Name exclude patterns
3. Tag include (empty = include all)
4. Tag exclude

### Data Format Examples

See the example files for format reference:
- **ToolHive format** (`examples/toolhive-registry.json`): Flat structure with servers as an object/map
- **Upstream format** (`examples/upstream-registry.json`): Wrapper structure with meta/data sections and MCP 2025-10-17 schema

### Git Version Pinning

```yaml
registries:
  - name: my-registry
    format: toolhive  # or "upstream"
    git:
      repository: https://github.com/stacklok/toolhive.git

      # Option 1: Track latest on a branch
      branch: main

      # Option 2: Pin to a release tag
      tag: v1.2.3

      # Option 3: Pin to exact commit
      commit: abc123def456

      path: pkg/registry/data/registry.json
    syncPolicy:
      interval: "30m"
```

---

## Monitoring & Troubleshooting

### Check Sync Status

Sync status is stored in the PostgreSQL database and exposed via server logs.

**Status phases:**
- `Syncing`: Sync operation in progress
- `Complete`: Last sync successful
- `Failed`: Last sync failed (will auto-retry at next sync interval)

### Server Logs

Look for these log messages:

```bash
# Successful initialization
"Initializing sync manager for automatic registry synchronization"
"Loaded sync status: last sync at 2024-11-05T12:00:00Z, 42 servers"
"Starting background sync coordinator"

# Successful sync
"Starting sync operation (attempt 1)"
"Registry data fetched successfully from source"
"Sync completed successfully: 42 servers, hash=abc123de"

# Sync failures
"Sync failed: Fetch failed: ..."
```

### Common Issues

#### Sync Not Starting

**Symptom:** Health endpoint returns unhealthy

**Solution:**
1. Verify `--config` flag is provided:
   ```bash
   thv-registry-api serve --config examples/config-git.yaml
   ```
2. Check logs for "Loaded configuration from..."
3. Ensure `syncPolicy` is defined in config
4. Ensure `database` is configured and reachable

#### Git Clone Failed

**Symptom:** Status shows `Failed` with git error

**Solutions:**
- Check repository URL is accessible: `git ls-remote <url>`
- For private repos, configure git credentials
- Verify branch/tag/commit exists
- Check network connectivity

#### API Endpoint Unreachable

**Symptom:** Status shows `Failed` with connection error

**Solutions:**
- Verify endpoint URL: `curl <endpoint>/v0.1/servers`
- Check network connectivity
- Look for rate limiting (increase interval)
- Verify API is MCP-compatible

---

## Storage

All synced data is stored in PostgreSQL. Database configuration is required in the config file.

---

## Advanced Usage

### Development Setup

Fast updates for local development:

```yaml
registries:
  - name: dev-registry
    format: toolhive
    git:
      repository: https://github.com/stacklok/toolhive.git
      branch: develop  # Use dev branch
      path: pkg/registry/data/registry.json
    syncPolicy:
      interval: "1m"  # Very frequent for testing
```

### Production Setup

Conservative config with filtering:

```yaml
registries:
  - name: prod-registry
    format: toolhive
    git:
      repository: https://github.com/your-org/registry.git
      branch: production
      path: registry.json
    syncPolicy:
      interval: "30m"
    filter:
      tags:
        include: ["production", "stable"]
        exclude: ["experimental", "deprecated"]
```

### Multi-Source Aggregation

Run multiple instances and aggregate at the application level:

```bash
# Instance 1: Official ToolHive (port 8081)
thv-registry-api serve \
  --config examples/config-git.yaml \
  --address :8081 &

# Instance 2: Upstream MCP (port 8082)
thv-registry-api serve \
  --config examples/config-api.yaml \
  --address :8082 &

# Instance 3: Local file (port 8083)
thv-registry-api serve \
  --config examples/config-file.yaml \
  --address :8083 &
```

**Note:** Each instance should be configured with its own database or registry name to avoid conflicts.

---

## Command Reference

```bash
# Start with Git sync
thv-registry-api serve --config examples/config-git.yaml

# Start with API sync
thv-registry-api serve --config examples/config-api.yaml

# Start with File sync (local development)
thv-registry-api serve --config examples/config-file.yaml

# Start with custom address
thv-registry-api serve --config examples/config-git.yaml --address :9090

# Test API endpoint
curl http://localhost:8080/registry/v0.1/servers | jq

# Check health
curl http://localhost:8080/health

# Watch logs
tail -f /var/log/thv-registry-api.log | grep -i sync

# Note: Manual sync triggering is not currently supported
# Sync happens automatically based on configured intervals
```

---

## See Also

- [Main README](../README.md) - Full project documentation
- [Architecture](../README.md#architecture) - System design
- [API Reference](../README.md#api-endpoints) - REST endpoints
- [CLAUDE.md](../CLAUDE.md) - Development guide
