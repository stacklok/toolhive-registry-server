# Database Migrations

This document describes how to use the database migration tooling for the ToolHive Registry API Server.

## Overview

The project uses [golang-migrate](https://github.com/golang-migrate/migrate) for managing database schema migrations. Migrations are embedded into the binary at build time and can be executed via CLI commands or task commands.

## Migration Files

Migration files are located in `database/migrations/` and follow the naming convention:

```
000001_init.up.sql        # Apply migration
000001_init.down.sql      # Revert migration
```

Each migration consists of an "up" file (to apply changes) and a "down" file (to revert changes).

## CLI Commands

### Migrate Up

Apply pending migrations to the database:

```bash
# Migrate to latest version
./bin/thv-registry-api migrate up --config config.yaml --yes

# Migrate up by N steps
./bin/thv-registry-api migrate up --config config.yaml --num-steps 2 --yes

# Interactive mode (prompts for confirmation)
./bin/thv-registry-api migrate up --config config.yaml
```

### Migrate Down

Revert migrations (use with caution - may result in data loss):

```bash
# Migrate down by 1 step
./bin/thv-registry-api migrate down --config config.yaml --num-steps 1 --yes

# Interactive mode (prompts for confirmation)
./bin/thv-registry-api migrate down --config config.yaml --num-steps 1
```

**Note:** For safety, the down migration requires the `--num-steps` flag. To migrate all the way down (removing all schema), use `--num-steps 0`.

## Task Commands

For convenience, you can use task commands:

```bash
# Migrate up to latest version
task migrate-up CONFIG=config.yaml

# Migrate down by N steps
task migrate-down CONFIG=config.yaml NUM_STEPS=1
```

## Configuration

Migrations require a configuration file with database connection details:

```yaml
database:
  host: localhost
  port: 5432
  user: postgres
  password: secret
  database: toolhive_registry
  sslmode: disable  # or "require" for production
```

## Migration Tracking

The migration tool automatically creates a `schema_migrations` table in your database to track which migrations have been applied. This table contains:

- `version`: The migration version number
- `dirty`: Whether the migration is in a dirty state (manual intervention required)

## Creating New Migrations

To create a new migration:

1. Create two new files in `database/migrations/`:
   - `NNNNNN_description.up.sql` - SQL statements to apply the change
   - `NNNNNN_description.down.sql` - SQL statements to revert the change

2. Use sequential numbering (e.g., 000002, 000003, etc.)

3. After creating migration files, run `task gen` to regenerate sqlc code if schema changes affect queries

Example:

```sql
-- 000002_add_user_table.up.sql
CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPZ DEFAULT NOW()
);

-- 000002_add_user_table.down.sql
DROP TABLE IF EXISTS users;
```

## Best Practices

1. **Always test down migrations** - Ensure they properly revert changes
2. **Never edit applied migrations** - Create new migrations instead
3. **Use transactions** - Wrap DDL statements in transactions when possible
4. **Backup before down migrations** - Data loss is permanent
5. **Run migrations in CI/CD** - Automate migration testing

## Troubleshooting

### Dirty State

If a migration fails partway through, the database may be in a "dirty" state. To resolve:

1. Manually fix the database schema
2. Update the `schema_migrations` table to mark the version as clean
3. Or, revert to a previous known-good state

### Connection Issues

Ensure your configuration file has correct database credentials and the database server is accessible.

### Permission Errors

The database user must have sufficient privileges to:
- Create/drop tables
- Create/drop types
- Create/drop indexes
- Modify the schema
