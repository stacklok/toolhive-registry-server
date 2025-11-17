# Docker Deployment Guide

This guide explains how to run the ToolHive Registry API Server using Docker and Docker Compose.

## Overview

The Docker Compose setup includes three services:

1. **postgres** - PostgreSQL 16 database
2. **migrate** - One-time migration service that sets up the database schema
3. **registry-api** - The main API server

## Quick Start

```bash
# Start all services
docker-compose up -d

# View logs
docker-compose logs -f

# Stop all services
docker-compose down

# Stop and remove volumes (WARNING: deletes all data)
docker-compose down -v
```

## Service Architecture

### Migration Flow

```
postgres (healthy) → migrate (runs migrations) → registry-api (starts serving)
```

The `migrate` service:
- Waits for PostgreSQL to be healthy
- Runs `migrate up --config /config.yaml --yes` to apply all migrations
- Exits after successful migration
- Uses `restart: on-failure` to retry if database isn't ready

The `registry-api` service:
- Depends on both `postgres` (healthy) and `migrate` (completed successfully)
- Only starts after migrations are complete
- Serves the API on port 8080

### Embedded Migrations

All migration files are embedded into the binary at build time using Go's `embed` package. This means:
- No need to mount migration files as volumes
- Migrations are versioned with the application code
- The binary is self-contained and portable

## Configuration

The Docker setup uses `examples/config-docker.yaml` which includes:

- Database connection to the `postgres` service
- File-based registry source (`/data/registry.json`)
- Sync interval of 5 minutes
- SSL disabled for local Docker network

## Ports

- **8080** - Registry API HTTP endpoint
- **5432** - PostgreSQL database (exposed for development)

## Volumes

- `postgres_data` - Persistent storage for PostgreSQL data
- `registry_data` - Persistent storage for registry data

## Building

The Dockerfile uses a multi-stage build:

1. **Builder stage** - Compiles the Go binary with migrations embedded
2. **Runtime stage** - Minimal UBI image with just the binary

```bash
# Build manually
docker build -t toolhive-registry-api .

# Or let docker-compose build it
docker-compose build
```

## Database Management

### Running Migrations Manually

If you need to run migrations outside of docker-compose:

```bash
# Run migration up
docker-compose run --rm migrate migrate up --config /config.yaml --yes

# Run migration down (careful - data loss!)
docker-compose run --rm migrate migrate down --config /config.yaml --num-steps 1 --yes

# Check migration version
docker-compose run --rm migrate migrate version --config /config.yaml
```

### Accessing the Database

```bash
# Connect to PostgreSQL
docker-compose exec postgres psql -U registry -d registry

# View schema migrations table
docker-compose exec postgres psql -U registry -d registry -c "SELECT * FROM schema_migrations;"
```

### Resetting the Database

```bash
# Stop services and remove volumes
docker-compose down -v

# Start fresh
docker-compose up -d
```

## Development Workflow

### Hot Reload Setup

For development with hot reload:

1. Mount source code into the container:
```yaml
volumes:
  - .:/workspace
```

2. Use `go run` instead of the compiled binary:
```yaml
command: ["go", "run", "./cmd/thv-registry-api", "serve", "--config", "/config.yaml"]
```

### Testing Migrations

```bash
# Test migration up
docker-compose up postgres migrate

# View migration logs
docker-compose logs migrate

# Test migration down
docker-compose run --rm migrate migrate down --config /config.yaml --num-steps 1 --yes

# Test migration up again
docker-compose run --rm migrate migrate up --config /config.yaml --yes
```

## Troubleshooting

### Migration Service Fails

Check logs:
```bash
docker-compose logs migrate
```

Common issues:
- Database not ready: Increase healthcheck interval in postgres service
- Connection refused: Verify database credentials in config
- Migration already applied: Use `--num-steps` to apply specific migrations

### API Won't Start

Check migration status:
```bash
docker-compose ps
```

If `migrate` service shows `Exit 1`, check its logs and fix the migration issue.

### Database Connection Issues

Verify postgres is healthy:
```bash
docker-compose exec postgres pg_isready -U registry
```

Check network connectivity:
```bash
docker-compose exec registry-api ping postgres
```

## Production Considerations

### Security

1. **Change default credentials** in both `docker-compose.yml` and `config-docker.yaml`
2. **Enable SSL** for database connections:
   ```yaml
   database:
     sslMode: require
   ```
3. **Use secrets management** instead of environment variables
4. **Run as non-root user** (already configured: USER 1001)

### Monitoring

Add healthcheck to registry-api:
```yaml
healthcheck:
  test: ["CMD", "wget", "--quiet", "--tries=1", "--spider", "http://localhost:8080/health"]
  interval: 30s
  timeout: 10s
  retries: 3
```

### Backup

```bash
# Backup database
docker-compose exec postgres pg_dump -U registry registry > backup.sql

# Restore database
docker-compose exec -T postgres psql -U registry registry < backup.sql
```

## Environment Variables

Available environment variables:

- `LOG_LEVEL` - Logging verbosity (debug, info, warn, error)
- `POSTGRES_USER` - Database username
- `POSTGRES_PASSWORD` - Database password
- `POSTGRES_DB` - Database name

## Advanced Configuration

### Using Different Sources

The Docker setup can be configured for different data sources:

**Git Source:**
```yaml
source:
  type: git
  format: toolhive
  git:
    url: https://github.com/org/repo
    branch: main
    path: registry.json
```

**API Source:**
```yaml
source:
  type: api
  format: mcp
  api:
    url: https://registry.example.com/api
```

### Multiple Registries

To run multiple registry instances:

```bash
# Copy and modify docker-compose.yml
cp docker-compose.yml docker-compose.registry2.yml

# Change container names, ports, and config
# Then run:
docker-compose -f docker-compose.registry2.yml up -d
```

## Related Documentation

- [Migration Guide](MIGRATIONS.md) - Detailed migration documentation
- [Configuration Guide](../README.md#configuration) - Full configuration options
- [API Documentation](../docs/thv-registry-api/) - API endpoint reference
