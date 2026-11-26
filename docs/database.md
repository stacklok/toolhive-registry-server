# Database Configuration

The ToolHive Registry API server requires PostgreSQL for storing registry state and metadata.

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
| `migrationUser` | string | No | `user` | Database username for running migrations (should have elevated privileges) |
| `password` | string | No | - | Database password for the application user. Alternative to pgpass. Mutually exclusive with `dynamicAuth`. |
| `migrationPassword` | string | No | - | Database password for the migration user. Defaults to `password` when `migrationUser` equals `user`. Mutually exclusive with `dynamicAuth`. |
| `database` | string | Yes | - | Database name |
| `sslMode` | string | No | `require` | SSL mode (`disable`, `require`, `verify-ca`, `verify-full`) |
| `maxOpenConns` | int | No | `25` | Maximum number of open connections to the database |
| `maxIdleConns` | int | No | `5` | Maximum number of idle connections in the pool |
| `connMaxLifetime` | string | No | `5m` | Maximum lifetime of a connection (e.g., "1h", "30m") |

## Password Security

The server supports three methods for providing database passwords, in order of precedence:

1. **Dynamic auth** (e.g., AWS RDS IAM) — short-lived tokens generated automatically via the `dynamicAuth` config block
2. **Config field / environment variable** — set `password` and `migrationPassword` in config YAML, or use `THV_REGISTRY_DATABASE_PASSWORD` and `THV_REGISTRY_DATABASE_MIGRATIONPASSWORD` environment variables
3. **pgpass file** — PostgreSQL's standard `~/.pgpass` or `$PGPASSFILE` mechanism (used when no password is configured via methods 1 or 2)

The `password`/`migrationPassword` fields and `dynamicAuth` are mutually exclusive — the server rejects configurations that set both.

### pgpass File

The pgpass file approach supports secure credential handling with separate passwords for normal operations and migrations.

### Password File Format

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

# Restrict access to the file owner
chmod 600 ~/.pgpass
```

Unlike `psql` and other libpq clients, the server's driver (pgx) reads the pgpass file
whatever its permissions, so nothing warns you about a world-readable file. Restrict it
yourself.

### Custom pgpass Location

By default, the pgx driver looks for `~/.pgpass`. To use a custom location, set the `PGPASSFILE` environment variable:

```bash
# Use PGPASSFILE environment variable for custom location
export PGPASSFILE=/run/secrets/pgpass
thv-registry-api serve --config config.yaml
```

### Two-User Security Model

The server supports separate database users for different privilege levels:

- **Application user** (`user` config field): Limited privileges for normal operations (SELECT, INSERT, UPDATE, DELETE)
- **Migration user** (`migrationUser` config field): Elevated privileges for schema changes (CREATE, ALTER, DROP)

Each user's password comes from the config, the environment or the pgpass file (one entry per user).

**The migration user** must:

- own the registry's tables, because migrations alter them. Run every migration as the same
  user (or as a member of the role that owns the tables); if you change it, transfer ownership
  first with `REASSIGN OWNED BY old_migrator TO new_migrator`.
- have `CREATE` on the `public` schema.
- on the first install, have `CREATEROLE`, because the first migration creates the
  `toolhive_registry_server` role. If the role already exists (for example, created by
  [`prime-db`](#option-1-using-prime-db-command-recommended) run as an administrator),
  `CREATEROLE` is not needed.

**The application user** must:

- be a member of `toolhive_registry_server`. The migrations grant that role `SELECT`,
  `INSERT`, `UPDATE` and `DELETE` on the registry's tables, including `schema_migrations`,
  which the server reads and locks to check the schema version before every write.
- have `TEMPORARY` on the database, because sync stages incoming entries in temporary
  tables. PostgreSQL grants `TEMPORARY` to `PUBLIC` by default, so this matters only if
  your database revokes it; without it every sync fails with `permission denied to create
  temporary tables`.
- have `CONNECT` on the database and `USAGE` on the `public` schema.

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

When you start the server with `serve`, database migrations run automatically on startup. This ensures your database schema is always up to date.

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
# Ensure ~/.pgpass file contains database credentials
task migrate-up CONFIG=examples/config-database-dev.yaml

# Revert migrations (specify number of steps for safety)
task migrate-down CONFIG=examples/config-database-dev.yaml NUM_STEPS=1
```

### Safety Features

- `migrate down` requires `--num-steps` flag to prevent accidental full rollback
- Interactive confirmation prompts (bypass with `--yes` flag)
- Strong warnings displayed for destructive operations
- Configuration validation before connecting to database
- Precondition checks before any migration runs (see below)

### Migration Preconditions

Before applying or reverting anything, `serve`, `migrate up` and `migrate down`
check that the database can take the migrations they are about to cross. If a
check fails, the command stops with an error that names the problem, and the
database is left exactly as it was: same schema version, not dirty. Fix the
problem and run the command again.

| Migration | Direction | Refused when |
|-----------|-----------|--------------|
| 24 | up | The database has more than one managed source. See [Repairing more than one managed source](#repairing-more-than-one-managed-source). |
| 26 | down | A sync job holds a live lease. Stop every server, wait for the leases to expire (at most the lease duration, one minute by default), then retry. |

The checks are not locks: stop every server before a manual `migrate down`, so
no new lease is taken between the check and the migration.

### Repairing More Than One Managed Source

This version allows one managed source (the source that API-published entries
go into). Databases created on v1.1.x or earlier can have more than one, and the
upgrade then stops with an error such as:

```
migration precondition failed: the database has 2 managed sources (legacy, published) ...
```

Repair it while the database is still on its old schema, using the old server
version or `psql` as the migration user:

1. List the managed sources, how many entries each holds and which registries use them:

   ```sql
   SELECT s.name, s.creation_type, count(DISTINCT e.id) AS entries,
          string_agg(DISTINCT r.name, ', ') AS registries
   FROM source s
   LEFT JOIN registry_entry e ON e.source_id = s.id
   LEFT JOIN registry_source rs ON rs.source_id = s.id
   LEFT JOIN registry r ON r.id = rs.registry_id
   WHERE s.source_type = 'managed'
   GROUP BY s.name, s.creation_type;
   ```

2. Pick the managed source to keep, normally the one your configuration names in
   its `managed: {}` block.
3. For each other managed source whose entries you still need, publish those
   entries again (`POST /v1/entries` on the old server) so they land in the
   source you keep. Entries in the extra sources are deleted in the next step.
4. Remove each extra managed source:
   - created from the configuration (`creation_type = 'CONFIG'`): remove it from
     the configuration file and its registries, then restart the old server, which
     deletes config sources that are no longer listed;
   - created through the API (`creation_type = 'API'`): remove it from every
     registry that lists it (`PUT /v1/registries/{name}`), then delete it
     (`DELETE /v1/sources/{name}`);
   - or, as a last resort, delete it directly. This deletes its entries and
     registry links too:

     ```sql
     DELETE FROM source WHERE source_type = 'managed' AND name = 'legacy';
     ```

5. Run the upgrade again.

### Recovering from a Dirty Schema Version

If a migration fails part-way (for example, because the connection was lost or a
statement failed), the schema version is recorded as dirty. Every later
migration, and the server's startup, then refuses to run until the operator
records which version the schema actually matches:

```bash
# Show the recorded version and dirty flag: every migrate command logs them,
# or query directly:
#   SELECT version, dirty FROM schema_migrations;

# Record VERSION as clean, without running any migration (-1 records none)
thv-registry-api migrate force VERSION --config config.yaml
```

> **Warning:** `migrate force` does not change the schema; it only rewrites the
> recorded version. First compare the schema with the migration files in
> `database/migrations/` and finish or undo the failed migration by hand, so the
> schema matches `VERSION` exactly. Forcing a version that the schema does not
> match makes later migrations fail or corrupt data. Back up the database first.

Usually `VERSION` is the version before the failed migration, after undoing its
partial changes, and then `migrate up` (or a server start) runs it again.

### One-Time Latest Version Rebuild

The first start after upgrading past migration 25 rebuilds the latest-version
pointer of every entry once, before the server starts serving, and logs
`Rebuilt latest version pointers after schema upgrade`. Fresh installs skip the
log line. The rebuild takes a few seconds even for catalogs of 150,000 names.

## Setup Guide

### Prerequisites

The server requires that the database user has the `toolhive_registry_server` role granted.

### Option 1: Using prime-db Command (Recommended)

The `prime-db` subcommand creates the `toolhive_registry_server` role and an application
user that is a member of it. It takes the new user's name as its argument, reads the new
user's password from standard input, and connects with the database settings in `--config`
(as the migration user), which needs `CREATEROLE`:

```bash
# Interactive mode (prompts for the new user's password)
thv-registry-api prime-db thvr_user --config examples/config-database-dev.yaml

# Pipe the password via stdin
echo "mypassword" | thv-registry-api prime-db thvr_user --config examples/config-database-dev.yaml

# Dry run (print SQL without executing)
thv-registry-api prime-db thvr_user --config examples/config-database-dev.yaml --dry-run
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
2. **Set up pgpass file**: Create `~/.pgpass` file with database credentials (see [Password Security](#password-security))
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
  postgres:18-alpine

# 2. Create pgpass file with credentials
cat > ~/.pgpass <<EOF
localhost:5432:toolhive_registry:thv_user:devpassword
EOF
chmod 600 ~/.pgpass

# 3. Start the server (migrations run automatically)
thv-registry-api serve --config examples/config-database-dev.yaml
```

### Production Deployment

```bash
# 1. Create pgpass file with secure credentials
cat > /run/secrets/pgpass <<EOF
db.example.com:5432:toolhive_registry:db_app:app_secure_password
db.example.com:5432:toolhive_registry:db_migrator:migration_secure_password
EOF
chmod 400 /run/secrets/pgpass

# 2. Export PGPASSFILE environment variable
export PGPASSFILE=/run/secrets/pgpass

# 3. Start the server (migrations run automatically)
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
