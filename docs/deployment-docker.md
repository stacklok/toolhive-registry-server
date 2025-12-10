# Docker Deployment

This guide covers deploying the ToolHive Registry API server using Docker and Docker Compose.

## Table of Contents

- [Docker](#docker)
- [Docker Compose](#docker-compose)
- [Production Considerations](#production-considerations)
- [Troubleshooting](#troubleshooting)

## Docker

### Building the Image

Build from source:

```bash
# Using Task
task build-image

# Or using Docker directly
docker build -t ghcr.io/stacklok/toolhive/thv-registry-api:latest .
```

### Running with Docker

#### Git Source

```bash
docker run -d \
  --name registry-api \
  -p 8080:8080 \
  -v $(pwd)/examples:/config:ro \
  ghcr.io/stacklok/toolhive/thv-registry-api:latest \
  serve --config /config/config-git.yaml
```

#### File Source

```bash
docker run -d \
  --name registry-api \
  -p 8080:8080 \
  -v $(pwd)/examples:/config:ro \
  -v $(pwd)/data/registry.json:/data/registry.json:ro \
  ghcr.io/stacklok/toolhive/thv-registry-api:latest \
  serve --config /config/config-file.yaml
```

#### With Database

```bash
# Create pgpass file with database credentials
cat > ~/.pgpass <<EOF
postgres:5432:registry:db_app:app_password
EOF
chmod 600 ~/.pgpass

docker run -d \
  --name registry-api \
  -p 8080:8080 \
  -v $(pwd)/examples:/config:ro \
  -v ~/.pgpass:/root/.pgpass:ro \
  -e PGPASSFILE=/root/.pgpass \
  ghcr.io/stacklok/toolhive/thv-registry-api:latest \
  serve --config /config/config-database-dev.yaml
```

### Docker Run Options

Common options:

```bash
docker run \
  -d                                    # Run in background
  --name registry-api                   # Container name
  -p 8080:8080                         # Port mapping
  -v $(pwd)/config:/config:ro          # Config volume (read-only)
  -v ~/.pgpass:/root/.pgpass:ro        # Mount pgpass file
  -e PGPASSFILE=/root/.pgpass          # Point to pgpass file
  --restart unless-stopped              # Restart policy
  --health-cmd='curl -f http://localhost:8080/health || exit 1' \
  --health-interval=30s                 # Health check
  --health-timeout=3s \
  --health-retries=3 \
  ghcr.io/stacklok/toolhive/thv-registry-api:latest \
  serve --config /config/config.yaml
```

## Docker Compose

A complete Docker Compose setup is provided that includes PostgreSQL and the API server with automatic migrations.

### Quick Start

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

### Architecture

The `docker-compose.yaml` includes two services:

1. **postgres** - PostgreSQL 18 database server
2. **registry-api** - Main API server (runs migrations automatically on startup)

**Service startup flow:**
```
postgres (healthy) → registry-api (runs migrations, then starts)
```

### Configuration

Default setup uses:
- **Config file**: `examples/config-docker.yaml`
- **Sample data**: `examples/registry-sample.json`
- **Database passwords**: Managed via pgpass file (`docker/pgpass`) mounted into the container
  - `PGPASSFILE` environment variable points to the mounted pgpass file
  - Contains credentials for both application and migration users

### Docker Compose File

The complete `docker-compose.yaml` in the repository root demonstrates:

- Database-backed registry storage
- Separate users for migrations and operations
- Automatic schema migrations on startup
- Limited database privileges (principle of least privilege)
- File-based data source (for demo purposes)
- Proper service dependencies and health checks

### Accessing the API

Once running, the API is available at http://localhost:8080

```bash
# List all servers
curl http://localhost:8080/registry/v0.1/servers

# Get specific server version
curl http://localhost:8080/registry/v0.1/servers/example%2Ffilesystem/versions/latest

# Health check
curl http://localhost:8080/health
```

### Customization

#### Using Your Own Registry Data

1. Edit `examples/registry-sample.json` with your MCP servers
2. Or change the source configuration in `examples/config-docker.yaml`
3. Restart: `docker-compose restart registry-api`

#### Using Git Source

Update `examples/config-docker.yaml`:

```yaml
registries:
  - name: toolhive
    format: toolhive
    git:
      repository: https://github.com/your-org/your-repo.git
      branch: main
      path: path/to/registry.json
    syncPolicy:
      interval: "30m"
```

Restart:
```bash
docker-compose restart registry-api
```

#### Changing Database Credentials

1. Update the pgpass file (`docker/pgpass`) with new credentials:

```bash
# Edit docker/pgpass
postgres:5432:registry:db_app:new-app-password
postgres:5432:registry:db_migrator:new-migration-password
```

2. Update PostgreSQL superuser password in `docker-compose.yaml`:

```yaml
postgres:
  environment:
    - POSTGRES_PASSWORD=new-postgres-password
```

3. Recreate services:
```bash
docker-compose down
docker-compose up -d
```

### Database Access

The Docker Compose setup creates three database users:

- `registry`: Superuser (for administration)
- `db_migrator`: Migration user with schema modification privileges
- `db_app`: Application user with limited data access privileges

#### Connecting to PostgreSQL

```bash
# As superuser (for administration)
docker exec -it toolhive-registry-postgres psql -U registry -d registry

# As application user
docker exec -it toolhive-registry-postgres psql -U db_app -d registry

# From host machine
PGPASSWORD=registry_password psql -h localhost -U registry -d registry
PGPASSWORD=app_password psql -h localhost -U db_app -d registry
```

#### Database Backup

```bash
# Backup database
docker exec toolhive-registry-postgres pg_dump -U registry registry > backup.sql

# Restore database
docker exec -i toolhive-registry-postgres psql -U registry registry < backup.sql
```

### Advanced Docker Compose

#### Custom docker-compose.override.yaml

Create `docker-compose.override.yaml` for local customizations:

```yaml
version: '3.8'

services:
  registry-api:
    environment:
      - DEBUG=true
    ports:
      - "9090:8080"  # Different port
    volumes:
      - ./my-config.yaml:/config/config.yaml:ro

  postgres:
    ports:
      - "5433:5432"  # Expose on different port
```

Docker Compose automatically merges override files.

#### Production-like Setup

Create `docker-compose.prod.yaml`:

```yaml
version: '3.8'

services:
  registry-api:
    restart: always
    deploy:
      replicas: 2
      resources:
        limits:
          cpus: '1'
          memory: 1G
        reservations:
          cpus: '0.5'
          memory: 512M
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8080/health"]
      interval: 30s
      timeout: 3s
      retries: 3
      start_period: 40s

  postgres:
    restart: always
    deploy:
      resources:
        limits:
          cpus: '2'
          memory: 2G
    volumes:
      - postgres-data:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U registry"]
      interval: 10s
      timeout: 5s
      retries: 5

volumes:
  postgres-data:
    driver: local
```

Run with:
```bash
docker-compose -f docker-compose.yaml -f docker-compose.prod.yaml up -d
```

#### With Authentication

Update `examples/config-docker.yaml`:

```yaml
auth:
  mode: oauth
  oauth:
    resourceUrl: https://registry.localhost
    providers:
      - name: my-idp
        issuerUrl: https://auth.example.com
        audience: api://registry
```

Add OAuth provider service to `docker-compose.yaml` if needed.

## Production Considerations

### Security

1. **Don't use default passwords** in production
2. **Use secrets management** (Docker Swarm secrets, external secret store)
3. **Enable TLS** (use reverse proxy like Nginx or Traefik)
4. **Enable authentication** (OAuth, not anonymous mode)
5. **Limit network exposure** (don't expose database port)
6. **Use read-only volumes** where possible

### Persistence

Important volumes to persist:

```yaml
volumes:
  # Database data (critical!)
  postgres-data:
    driver: local
    driver_opts:
      type: none
      o: bind
      device: /data/postgres

  # Sync state (optional but recommended)
  registry-data:
    driver: local
```

### Logging

Configure logging drivers:

```yaml
services:
  registry-api:
    logging:
      driver: "json-file"
      options:
        max-size: "10m"
        max-file: "3"
```

Or use centralized logging:

```yaml
logging:
  driver: "syslog"
  options:
    syslog-address: "tcp://logs.example.com:514"
    tag: "registry-api"
```

### Monitoring

Add monitoring services:

```yaml
services:
  prometheus:
    image: prom/prometheus:latest
    volumes:
      - ./prometheus.yml:/etc/prometheus/prometheus.yml
    ports:
      - "9090:9090"

  grafana:
    image: grafana/grafana:latest
    ports:
      - "3000:3000"
    environment:
      - GF_SECURITY_ADMIN_PASSWORD=admin
```

### Reverse Proxy

Use Nginx or Traefik for TLS termination:

```yaml
services:
  nginx:
    image: nginx:alpine
    ports:
      - "443:443"
    volumes:
      - ./nginx.conf:/etc/nginx/nginx.conf:ro
      - ./certs:/etc/nginx/certs:ro
    depends_on:
      - registry-api
```

## Troubleshooting

### Container Won't Start

Check logs:
```bash
docker-compose logs registry-api
docker logs registry-api
```

Common issues:
- Configuration file not found or malformed
- Environment variables not set
- Database connection failure
- Port already in use

### Database Connection Failed

Verify database is running:
```bash
docker-compose ps postgres
docker-compose logs postgres
```

Test connection:
```bash
docker exec toolhive-registry-postgres pg_isready -U registry
```

### Slow Startup

The first startup may be slow due to:
- Database migrations running
- Initial Git clone (if using Git source)
- Docker image download

Monitor progress:
```bash
docker-compose logs -f registry-api
```

### Port Already in Use

Change port mapping in `docker-compose.yaml`:
```yaml
ports:
  - "8081:8080"  # Use 8081 instead of 8080
```

Or in docker run:
```bash
docker run -p 8081:8080 ...
```

### Cannot Access from Host

Check:
1. Container is running: `docker ps`
2. Port is exposed: `docker port registry-api`
3. Firewall allows connection
4. Using correct URL: `http://localhost:8080` (not `http://127.0.0.1:8080` on some systems)

### Migrations Failing

Run migrations manually:
```bash
docker-compose exec registry-api \
  thv-registry-api migrate up --config /etc/registry/config.yaml
```

Check migration status:
```bash
docker-compose exec postgres \
  psql -U registry -d registry -c "SELECT * FROM schema_migrations;"
```

## See Also

- [Configuration Guide](configuration.md) - Complete configuration reference
- [Database Setup](database.md) - Database configuration and migrations
- [Authentication](authentication.md) - OAuth and security configuration
- [Kubernetes Deployment](deployment-kubernetes.md) - Deploy on Kubernetes