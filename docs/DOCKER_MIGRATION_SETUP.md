# Docker Compose Migration Setup

This document describes the Docker Compose setup with automated database migrations.

## Overview

The docker-compose configuration has been updated to use the `migrate` CLI command to manage database schema instead of mounting raw SQL files. This provides:

- **Version control**: Proper migration versioning and tracking
- **Rollback capability**: Ability to migrate down when needed
- **Embedded migrations**: No need to mount migration files
- **Production-ready**: Same migration flow in development and production

## Architecture

### Service Flow

```
┌──────────┐
│ postgres │ (starts, becomes healthy)
└────┬─────┘
     │
     ▼
┌──────────┐
│ migrate  │ (runs migrations, exits)
└────┬─────┘
     │
     ▼
┌──────────┐
│registry- │ (starts serving)
│   api    │
└──────────┘
```

### Services

1. **postgres** - PostgreSQL 16 database
   - Port: 5432
   - Healthcheck: `pg_isready` every 5 seconds
   - Volume: `postgres_data` for persistence

2. **migrate** - One-time migration service
   - Runs: `migrate up --config /config.yaml --yes`
   - Depends on: postgres (healthy)
   - Restart: `on-failure` (retries if database isn't ready)
   - Exits: After successful migration

3. **registry-api** - Main API server
   - Port: 8080
   - Depends on: postgres (healthy), migrate (completed successfully)
   - Command: `serve --config /config.yaml --address :8080`
   - Restart: `unless-stopped`

## Changes Made

### 1. docker-compose.yml

**Before:**
```yaml
postgres:
  volumes:
    - ./database/migrations/000001_init.up.sql:/docker-entrypoint-initdb.d/01-schema.sql:ro

registry-api:
  depends_on:
    postgres:
      condition: service_healthy
```

**After:**
```yaml
postgres:
  # No migration files mounted

migrate:
  command: ["migrate", "up", "--config", "/config.yaml", "--yes"]
  depends_on:
    postgres:
      condition: service_healthy
  restart: on-failure

registry-api:
  depends_on:
    postgres:
      condition: service_healthy
    migrate:
      condition: service_completed_successfully
```

### 2. Migration Files

All SQL migration files in `database/migrations/` are now:
- Embedded into the binary via `//go:embed` directive
- Automatically included in Docker image
- No volume mounts required

### 3. Documentation

Added/updated:
- `docs/MIGRATIONS.md` - Migration usage guide
- `docs/DOCKER.md` - Comprehensive Docker deployment guide
- `docs/DOCKER_MIGRATION_SETUP.md` - This document
- `README.md` - Updated Docker section with migration info

### 4. Files Added

- `database/migrations.go` - Embeds migrations and provides migrator interface
- `cmd/thv-registry-api/app/migrate.go` - Base migrate command
- `cmd/thv-registry-api/app/migrate_up.go` - Migrate up command
- `cmd/thv-registry-api/app/migrate_down.go` - Migrate down command
- `.dockerignore` - Optimized Docker build context

## Usage

### Starting the Stack

```bash
# Start all services (postgres → migrate → api)
docker-compose up -d

# View logs
docker-compose logs -f

# Check migration status
docker-compose ps migrate  # Should show "Exit 0"

# Check API is running
curl http://localhost:8080/api/v0/servers
```

### Manual Migrations

```bash
# Apply all pending migrations
docker-compose run --rm migrate migrate up --config /config.yaml --yes

# Revert last migration
docker-compose run --rm migrate migrate down --config /config.yaml --num-steps 1 --yes

# Check current version
docker-compose exec postgres psql -U registry -d registry -c "SELECT * FROM schema_migrations;"
```

### Troubleshooting

**Migration service fails:**
```bash
# View migration logs
docker-compose logs migrate

# Common issues:
# - Database not ready: Wait a few seconds and run: docker-compose up migrate
# - Config error: Check examples/config-docker.yaml
# - Permission error: Check database user permissions
```

**API won't start:**
```bash
# Check migrate service status
docker-compose ps migrate

# If Exit 1, fix migration and retry:
docker-compose up migrate
```

**Reset everything:**
```bash
# Stop and remove all data
docker-compose down -v

# Start fresh
docker-compose up -d
```

## Benefits

### Compared to Previous Setup

| Aspect | Before | After |
|--------|--------|-------|
| Migration tracking | None | schema_migrations table |
| Rollback | Manual SQL | `migrate down` command |
| Version control | Single file | Versioned migrations |
| Production parity | Different approach | Same binary, same flow |
| Embedded | Files mounted | Embedded in binary |
| Testing | Hard to test | Easy with task commands |

### Production Benefits

1. **Consistency**: Same migration binary in dev, staging, and production
2. **Auditability**: Migration history tracked in `schema_migrations` table
3. **Automation**: Easy to integrate into CI/CD pipelines
4. **Safety**: Built-in dirty state detection and warnings
5. **Flexibility**: Can run migrations separately from application

## CI/CD Integration

### GitHub Actions Example

```yaml
- name: Run migrations
  run: |
    docker-compose up -d postgres
    docker-compose run --rm migrate migrate up --config /config.yaml --yes

- name: Start API
  run: docker-compose up -d registry-api
```

### Kubernetes Integration

For production Kubernetes deployments, use an init container:

```yaml
initContainers:
  - name: migrate
    image: ghcr.io/stacklok/toolhive-registry-api:latest
    command: ["migrate", "up", "--config", "/config/config.yaml", "--yes"]
    volumeMounts:
      - name: config
        mountPath: /config
```

## Migration Best Practices

1. **Always test locally first**: Use docker-compose to test migrations
2. **Backup before production**: Always backup before running migrations
3. **Test down migrations**: Ensure they properly revert changes
4. **Keep migrations small**: One logical change per migration
5. **Never edit applied migrations**: Create new migrations instead

## Related Documentation

- [MIGRATIONS.md](MIGRATIONS.md) - Full migration guide
- [DOCKER.md](DOCKER.md) - Complete Docker deployment guide
- [README.md](../README.md) - Project overview and quick start
