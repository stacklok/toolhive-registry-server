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
- **Multiple data sources**: Git repositories, API endpoints, and local files
- **Automatic synchronization**: Background sync with configurable intervals and retry logic
- **Container-ready**: Designed for deployment in Kubernetes clusters
- **Flexible deployment**: Works standalone or as part of ToolHive infrastructure
- **Production-ready**: Built-in health checks, graceful shutdown, and sync status persistence

## Quick Start

### Prerequisites

- Go 1.23 or later
- [Task](https://taskfile.dev) for build automation

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

**With local file:**
```bash
thv-registry-api serve --config examples/config-file.yaml
```

**With API endpoint:**
```bash
thv-registry-api serve --config examples/config-api.yaml
```

The server starts on port 8080 by default. Use `--address :PORT` to customize.

**What happens when the server starts:**
1. Loads configuration from the specified YAML file
2. Runs database migrations automatically (if database is configured)
3. Immediately fetches registry data from the configured source
4. Starts background sync coordinator for automatic updates
5. Serves MCP Registry API endpoints on the configured address

For detailed configuration options and examples, see the [examples/README.md](examples/README.md).

### Available Commands

The `thv-registry-api` CLI provides the following commands:

```bash
# Start the API server
thv-registry-api serve --config config.yaml [--address :8080]

# Manually run database migrations
thv-registry-api migrate up --config config.yaml [--yes]
thv-registry-api migrate down --config config.yaml --num-steps N [--yes]

# Display version information
thv-registry-api version [--format json]

# Show help
thv-registry-api --help
thv-registry-api <command> --help
```

See the [Database Migrations](#database-migrations) section for more details on using migration commands.

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
  # Source type: git, api, or file
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

# Optional: Database configuration
database:
  host: localhost
  port: 5432
  user: registry
  passwordFile: /secrets/db-password  # Recommended for production
  database: registry
  sslMode: require
  maxOpenConns: 25
  maxIdleConns: 5
  connMaxLifetime: "5m"
```

### Command-line Flags

| Flag | Description | Required | Default |
|------|-------------|----------|---------|
| `--config` | Path to YAML configuration file | Yes | - |
| `--address` | Server listen address | No | `:8080` |

### Data Sources

The server supports three data source types:

1. **Git Repository** - Clone and sync from Git repositories
   - Supports branch, tag, or commit pinning
   - Ideal for version-controlled registries
   - Example: [config-git.yaml](examples/config-git.yaml)

2. **API Endpoint** - Sync from upstream MCP Registry APIs
   - Supports federation and aggregation scenarios
   - Format conversion from upstream to ToolHive format
   - Example: [config-api.yaml](examples/config-api.yaml)

3. **Local File** - Read from filesystem
   - Ideal for local development and testing
   - Supports mounted volumes in containers
   - Example: [config-file.yaml](examples/config-file.yaml)

For complete configuration examples and advanced options, see [examples/README.md](examples/README.md).

### Database Configuration

The server optionally supports PostgreSQL database connectivity for storing registry state and metadata.

#### Configuration Fields

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `host` | string | Yes | - | Database server hostname or IP address |
| `port` | int | Yes | - | Database server port |
| `user` | string | Yes | - | Database username for normal operations |
| `passwordFile` | string | No* | - | Path to file containing the database password |
| `migrationUser` | string | No | `user` | Database username for running migrations (should have elevated privileges) |
| `migrationPasswordFile` | string | No | `passwordFile` | Path to file containing the migration user's password |
| `database` | string | Yes | - | Database name |
| `sslMode` | string | No | `require` | SSL mode (`disable`, `require`, `verify-ca`, `verify-full`) |
| `maxOpenConns` | int | No | `25` | Maximum number of open connections to the database |
| `maxIdleConns` | int | No | `5` | Maximum number of idle connections in the pool |
| `connMaxLifetime` | string | No | `5m` | Maximum lifetime of a connection (e.g., "1h", "30m") |

\* Password configuration is required but has multiple sources (see Password Security below)

#### Password Security

The server supports secure password management with separate credentials for normal operations and migrations.

**Normal Operations Password (for `user`):**

1. **Password File** (Recommended for production):
   - Set `passwordFile` to the path of a file containing only the password
   - The file content will have leading/trailing whitespace trimmed
   - Ideal for Kubernetes secrets mounted as files
   - Example:
     ```yaml
     database:
       passwordFile: /secrets/db-password
     ```

2. **Environment Variable**:
   - Set `THV_DATABASE_PASSWORD` environment variable
   - Used if `passwordFile` is not specified
   - Example:
     ```bash
     export THV_DATABASE_PASSWORD="your-secure-password"
     thv-registry-api serve --config config.yaml
     ```

**Migration User Password (for `migrationUser`):**

1. **Migration Password File**:
   - Set `migrationPasswordFile` to the path of a file containing the migration user's password
   - Falls back to `passwordFile` if not specified
   - Example:
     ```yaml
     database:
       migrationUser: db_migrator
       migrationPasswordFile: /secrets/db-migration-password
     ```

2. **Environment Variable**:
   - Set `THV_DATABASE_MIGRATION_PASSWORD` environment variable
   - Falls back to `THV_DATABASE_PASSWORD` if not specified
   - Example:
     ```bash
     export THV_DATABASE_MIGRATION_PASSWORD="migration-user-password"
     thv-registry-api serve --config config.yaml
     ```

**Security Best Practices:**
- Use separate users for migrations (with elevated privileges) and normal operations (read-only or limited)
- Never commit passwords directly in configuration files
- Use password files with restricted permissions (e.g., `chmod 400`)
- In Kubernetes, mount passwords from Secrets
- Rotate passwords regularly

#### Connection Pooling

The server uses connection pooling for efficient database resource management:

- **MaxOpenConns**: Limits concurrent database connections to prevent overwhelming the database
- **MaxIdleConns**: Maintains idle connections for faster query execution
- **ConnMaxLifetime**: Automatically closes and recreates connections to prevent connection leaks

Tune these values based on your workload:
- High-traffic scenarios: Increase `maxOpenConns` and `maxIdleConns`
- Resource-constrained environments: Decrease pool sizes
- Long-running services: Set shorter `connMaxLifetime` (e.g., "1h")

#### Database Migrations

The server includes built-in database migration support to manage the database schema.

**Automatic migrations on startup:**

When you start the server with `serve`, database migrations run automatically if database configuration is present in your config file. This ensures your database schema is always up to date.

The only thing necessary is granting the role `toolhive_registry_server` to
the database user you want, for example

```sql
BEGIN;

DO $$
DECLARE
  username TEXT := 'thvr_user';
  password TEXT := 'custom-password';
BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'toolhive_registry_server') THEN
    CREATE ROLE toolhive_registry_server;
    RAISE NOTICE 'Role toolhive_registry_server created';
  END IF;

  IF NOT EXISTS (SELECT FROM pg_user WHERE usename = username) THEN
    EXECUTE format('CREATE USER %I WITH PASSWORD %L', username, password);
    RAISE NOTICE 'User % created', username;
  END IF;

  EXECUTE format('GRANT toolhive_registry_server TO %I', username);
  RAISE NOTICE 'Role toolhive_registry_server granted to %', username;
END
$$;

COMMIT;
```

To help with that, you can run the `prime-db` subcommand to configure a user with
the correct role as follows.

```sh
thv-registry-api prime-db --config examples/config-database-dev.yaml
...
```

By default, the password is read from input, but it is also possible to pipe
it via shell via `echo "mypassword" | thv-registry-api prime-db --config ...`.

Alternatively, the SQL script that would be executed can be printed to standard
output without touching the database.

```sh
thv-registry-api prime-db --config examples/config-database-dev.yaml --dry-run
```

Once done, you start the server as follows

```bash
# Migrations run automatically when database is configured
thv-registry-api serve --config examples/config-database-dev.yaml
```

**Manual migration commands (optional):**

You can also run migrations manually using the CLI commands:

```bash
# Apply all pending migrations
thv-registry-api migrate up --config examples/config-database-dev.yaml

# Apply migrations non-interactively (useful for CI/CD)
thv-registry-api migrate up --config config.yaml --yes

# Revert last migration (requires --num-steps for safety)
thv-registry-api migrate down --config config.yaml --num-steps 1

# View migration help
thv-registry-api migrate --help
```

**Running migrations with Task:**

```bash
# Apply migrations (development)
export THV_DATABASE_PASSWORD="devpassword"
task migrate-up CONFIG=examples/config-database-dev.yaml

# Revert migrations (specify number of steps for safety)
task migrate-down CONFIG=examples/config-database-dev.yaml NUM_STEPS=1
```

**Migration workflow:**

1. **Configure database**: Create a config file with database settings (see [examples/config-database-dev.yaml](examples/config-database-dev.yaml))
2. **Set password**: Either set `THV_DATABASE_PASSWORD` env var or use `passwordFile` in config
3. **Start server**: Run `serve` command - migrations will run automatically

**Example: Local development setup**

```bash
# 1. Start PostgreSQL (example with Docker)
docker run -d --name postgres \
  -e POSTGRES_USER=thv_user \
  -e POSTGRES_PASSWORD=devpassword \
  -e POSTGRES_DB=toolhive_registry \
  -p 5432:5432 \
  postgres:16

# 2. Set password environment variable
export THV_DATABASE_PASSWORD="devpassword"

# 3. Start the server (migrations run automatically)
thv-registry-api serve --config examples/config-database-dev.yaml
```

**Example: Production deployment**

```bash
# 1. Create password file
echo "your-secure-password" > /run/secrets/db_password
chmod 400 /run/secrets/db_password

# 2. Start the server (migrations run automatically)
thv-registry-api serve --config examples/config-database-prod.yaml
```

**Safety features:**

- `migrate down` requires `--num-steps` flag to prevent accidental full rollback
- Interactive confirmation prompts (bypass with `--yes` flag)
- Strong warnings displayed for destructive operations
- Configuration validation before connecting to database

For complete examples, see:
- [examples/config-database-dev.yaml](examples/config-database-dev.yaml) - Development configuration
- [examples/config-database-prod.yaml](examples/config-database-prod.yaml) - Production configuration

#### Example Kubernetes Deployment with Database

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: registry-db-password
type: Opaque
stringData:
  password: your-secure-password
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: registry-api-config
data:
  config.yaml: |
    registryName: my-registry
    source:
      type: git
      format: toolhive
      git:
        repository: https://github.com/stacklok/toolhive.git
        branch: main
        path: pkg/registry/data/registry.json
    syncPolicy:
      interval: "15m"
    database:
      host: postgres.default.svc.cluster.local
      port: 5432
      user: registry
      passwordFile: /secrets/db-password
      database: registry
      sslMode: require
      maxOpenConns: 25
      maxIdleConns: 5
      connMaxLifetime: "5m"
---
# Run migrations as a Kubernetes Job before deploying the server
apiVersion: batch/v1
kind: Job
metadata:
  name: registry-migrate
spec:
  template:
    spec:
      restartPolicy: OnFailure
      containers:
      - name: migrate
        image: ghcr.io/stacklok/toolhive/thv-registry-api:latest
        args:
        - migrate
        - up
        - --config=/etc/registry/config.yaml
        - --yes
        volumeMounts:
        - name: config
          mountPath: /etc/registry
        - name: db-password
          mountPath: /secrets
          readOnly: true
      volumes:
      - name: config
        configMap:
          name: registry-api-config
      - name: db-password
        secret:
          secretName: registry-db-password
          items:
          - key: password
            path: db-password
---
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
        volumeMounts:
        - name: config
          mountPath: /etc/registry
        - name: db-password
          mountPath: /secrets
          readOnly: true
      volumes:
      - name: config
        configMap:
          name: registry-api-config
      - name: db-password
        secret:
          secretName: registry-db-password
          items:
          - key: password
            path: db-password
```

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

# Database migrations
task migrate-up CONFIG=examples/config-database-dev.yaml
task migrate-down CONFIG=examples/config-database-dev.yaml NUM_STEPS=1
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
│   ├── api.go               # API endpoint source
│   ├── file.go              # File system source
│   ├── factory.go           # Registry handler factory
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
4. **Registry Handler Layer** (`pkg/sources`): Pluggable data source implementations
   - `GitRegistryHandler`: Clones Git repositories and extracts registry files
   - `APIRegistryHandler`: Fetches from upstream MCP Registry APIs
   - `FileRegistryHandler`: Reads from local filesystem
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
      type: git
      format: toolhive
      git:
        repository: https://github.com/stacklok/toolhive.git
        branch: main
        path: pkg/registry/data/registry.json
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

# Run with database password from environment variable
docker run -v $(pwd)/examples:/config \
  -e THV_DATABASE_PASSWORD=your-password \
  ghcr.io/stacklok/toolhive/thv-registry-api:latest \
  serve --config /config/config-database-dev.yaml
```

### Docker Compose

A complete Docker Compose setup is provided in the repository root that includes PostgreSQL and the API server with automatic migrations.

**Quick start:**

```bash
# Start all services (PostgreSQL + API with automatic migrations)
docker-compose up

# Run in detached mode
docker-compose up -d

# View logs
docker-compose logs -f registry-api

# Stop all services
docker-compose down

# Stop and remove volumes (WARNING: deletes database data)
docker-compose down -v
```

**Architecture:**

The docker-compose.yaml includes two services:
1. **postgres** - PostgreSQL 18 database server
2. **registry-api** - Main API server (runs migrations automatically on startup)

**Service startup flow:**
```
postgres (healthy) → registry-api (runs migrations, then starts)
```

**Configuration:**

- Config file: `examples/config-docker.yaml`
- Sample data: `examples/registry-sample.json`
- Database passwords: Set via environment variables in docker-compose.yaml
  - `THV_DATABASE_PASSWORD`: Application user password
  - `THV_DATABASE_MIGRATION_PASSWORD`: Migration user password

The setup demonstrates:
- Database-backed registry storage with separate users for migrations and operations
- Automatic schema migrations on startup using elevated privileges
- Normal operations using limited database privileges (principle of least privilege)
- File-based data source (for demo purposes)
- Proper service dependencies and health checks

**Accessing the API:**

Once running, the API is available at http://localhost:8080

```bash
# List all servers
curl http://localhost:8080/api/v0/servers

# Get specific server
curl http://localhost:8080/api/v0/servers/example%2Ffilesystem
```

**Customization:**

To use your own registry data:
1. Edit `examples/registry-sample.json` with your MCP servers
2. Or change the source configuration in `examples/config-docker.yaml`
3. Restart: `docker-compose restart registry-api`

**Database access:**

The Docker Compose setup creates three database users:
- `registry`: Superuser (for administration)
- `db_migrator`: Migration user with schema modification privileges
- `db_app`: Application user with limited data access privileges

To connect to the PostgreSQL database directly:

```bash
# As superuser (for administration)
docker exec -it toolhive-registry-postgres psql -U registry -d registry

# As application user
docker exec -it toolhive-registry-postgres psql -U db_app -d registry

# From host machine
PGPASSWORD=registry_password psql -h localhost -U registry -d registry
PGPASSWORD=app_password psql -h localhost -U db_app -d registry
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