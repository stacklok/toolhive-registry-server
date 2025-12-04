# Database Configuration

The ToolHive Registry API server optionally supports PostgreSQL for storing registry state and metadata.

## Table of Contents

- [Configuration Fields](#configuration-fields)
- [Password Security](#password-security)
- [Connection Pooling](#connection-pooling)
- [Database Migrations](#database-migrations)
- [Setup Guide](#setup-guide)
- [Kubernetes Deployment](#kubernetes-deployment)

## Configuration Fields

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

\* Password configuration is required but has multiple sources (see [Password Security](#password-security))

## Password Security

The server supports secure password management with separate credentials for normal operations and migrations.

### Normal Operations Password (for `user`)

#### 1. Postgres Password File (Recommended for production)

The server supports PostgreSQL's standard pgpass file for password management.

**pgpass file format:**
```
hostname:port:database:username:password
```

**Example pgpass file with two users:**
```bash
# Create pgpass file with credentials for both users
cat > ~/.pgpass <<EOF
localhost:5432:toolhive_registry:db_app:app_password
localhost:5432:toolhive_registry:db_migrator:migration_password
EOF

# Set secure permissions (REQUIRED - pgx will ignore files with wrong permissions)
chmod 600 ~/.pgpass
```

**Custom pgpass location:**
```bash
# Use PGPASSFILE environment variable for custom location
export PGPASSFILE=/run/secrets/pgpass
thv-registry-api serve --config config.yaml
```

#### 2. Environment Variable

Set `THV_DATABASE_PASSWORD` environment variable. Used if `passwordFile` is not specified.

```bash
export THV_DATABASE_PASSWORD="your-secure-password"
thv-registry-api serve --config config.yaml
```

### Migration User Password (for `migrationUser`)

#### 1. Migration Password File

Set `migrationPasswordFile` to the path of a file containing the migration user's password. Falls back to `passwordFile` if not specified.

```yaml
database:
  migrationUser: db_migrator
  migrationPasswordFile: /secrets/db-migration-password
```

#### 2. Environment Variable

Set `THV_DATABASE_MIGRATION_PASSWORD` environment variable. Falls back to `THV_DATABASE_PASSWORD` if not specified.

```bash
export THV_DATABASE_MIGRATION_PASSWORD="migration-user-password"
thv-registry-api serve --config config.yaml
```

### Security Best Practices

- Use separate users for migrations (with elevated privileges) and normal operations (read-only or limited)
- Never commit passwords directly in configuration files
- Use password files with restricted permissions (e.g., `chmod 400`)
- In Kubernetes, mount passwords from Secrets
- Rotate passwords regularly

## Connection Pooling

The server uses connection pooling for efficient database resource management:

- **MaxOpenConns**: Limits concurrent database connections to prevent overwhelming the database
- **MaxIdleConns**: Maintains idle connections for faster query execution
- **ConnMaxLifetime**: Automatically closes and recreates connections to prevent connection leaks

### Tuning Guidelines

- **High-traffic scenarios**: Increase `maxOpenConns` and `maxIdleConns`
- **Resource-constrained environments**: Decrease pool sizes
- **Long-running services**: Set shorter `connMaxLifetime` (e.g., "1h")

## Database Migrations

The server includes built-in database migration support to manage the database schema.

### Automatic Migrations on Startup

When you start the server with `serve`, database migrations run automatically if database configuration is present in your config file. This ensures your database schema is always up to date.

### Manual Migration Commands

You can also run migrations manually using the CLI commands:

```bash
# Apply all pending migrations
thv-registry-api migrate up --config config.yaml

# Apply migrations non-interactively (useful for CI/CD)
thv-registry-api migrate up --config config.yaml --yes

# Revert last migration (requires --num-steps for safety)
thv-registry-api migrate down --config config.yaml --num-steps 1

# View migration help
thv-registry-api migrate --help
```

### Running Migrations with Task

```bash
# Apply migrations (development)
export THV_DATABASE_PASSWORD="devpassword"
task migrate-up CONFIG=examples/config-database-dev.yaml

# Revert migrations (specify number of steps for safety)
task migrate-down CONFIG=examples/config-database-dev.yaml NUM_STEPS=1
```

### Safety Features

- `migrate down` requires `--num-steps` flag to prevent accidental full rollback
- Interactive confirmation prompts (bypass with `--yes` flag)
- Strong warnings displayed for destructive operations
- Configuration validation before connecting to database

## Setup Guide

### Prerequisites

The server requires that the database user has the `toolhive_registry_server` role granted.

### Option 1: Using prime-db Command (Recommended)

The `prime-db` subcommand configures a user with the correct role:

```bash
# Interactive mode (prompts for password)
thv-registry-api prime-db --config examples/config-database-dev.yaml

# Pipe password via stdin
echo "mypassword" | thv-registry-api prime-db --config examples/config-database-dev.yaml

# Dry run (print SQL without executing)
thv-registry-api prime-db --config examples/config-database-dev.yaml --dry-run
```

### Option 2: Manual SQL Setup

Execute this SQL to create the role and user:

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

### Migration Workflow

1. **Configure database**: Create a config file with database settings (see `examples/config-database-dev.yaml`)
2. **Set password**: Either set `THV_DATABASE_PASSWORD` env var or use `passwordFile` in config
3. **Start server**: Run `serve` command - migrations will run automatically

## Examples

### Local Development Setup

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

### Production Deployment

```bash
# 1. Create password file
echo "your-secure-password" > /run/secrets/db_password
chmod 400 /run/secrets/db_password

# 2. Start the server (migrations run automatically)
thv-registry-api serve --config examples/config-database-prod.yaml
```

## Kubernetes Deployment

For complete Kubernetes deployment examples with database, see [Kubernetes Deployment Guide](deployment-kubernetes.md#database-configuration).

## Configuration Examples

For complete configuration examples, see:
- [examples/config-database-dev.yaml](../examples/config-database-dev.yaml) - Development configuration
- [examples/config-database-prod.yaml](../examples/config-database-prod.yaml) - Production configuration

## See Also

- [Configuration Guide](configuration.md) - Complete configuration reference
- [Kubernetes Deployment](deployment-kubernetes.md) - Deploy with database on Kubernetes
- [Docker Deployment](deployment-docker.md) - Docker Compose with PostgreSQL