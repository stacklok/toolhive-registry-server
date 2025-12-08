# Environment Variables

This guide covers using environment variables to configure the ToolHive Registry Server. Environment variables provide a flexible way to override configuration values without modifying YAML files, making them ideal for container deployments, CI/CD pipelines, and environment-specific configurations.

## Overview

The ToolHive Registry Server uses [Viper](https://github.com/spf13/viper) for configuration management, which supports loading configuration from multiple sources with a clear precedence order.

### Configuration Precedence

Configuration values are resolved in this order (lowest to highest priority):

1. **Default values** - Hardcoded defaults in the application
2. **Configuration file** - Values from the YAML config file
3. **Environment variables** - Values from `THV_*` environment variables
4. **Command-line flags** - Values from CLI flags (e.g., `--address`)

Higher priority sources override lower ones. For example, if `database.host` is set to `localhost` in the YAML file but `THV_DATABASE_HOST=postgres.example.com` is set, the environment variable value (`postgres.example.com`) will be used.

## Naming Convention

All environment variables use the `THV_` prefix (ToolHive Registry) to avoid conflicts with other applications.

### Rules

1. **Prefix**: All variables start with `THV_`
2. **Nested keys**: Use underscores (`_`) to represent nested configuration
3. **Case**: Environment variables are case-insensitive (but uppercase is conventional)
4. **Dots to underscores**: Configuration key `database.host` becomes `THV_DATABASE_HOST`

### Examples

| Configuration Key | Environment Variable | Example Value |
|-------------------|---------------------|---------------|
| `registryName` | `THV_REGISTRYNAME` | `production-registry` |
| `database.host` | `THV_DATABASE_HOST` | `postgres.example.com` |
| `database.port` | `THV_DATABASE_PORT` | `5432` |
| `auth.mode` | `THV_AUTH_MODE` | `oauth` |
| `auth.oauth.realm` | `THV_AUTH_OAUTH_REALM` | `mcp-registry` |

## Common Configuration Examples

### Server Configuration

```bash
# Change the listening address (default: :8080)
export THV_ADDRESS=:9090

# Set the registry name
export THV_REGISTRYNAME=production-registry
```

### Database Configuration

```bash
# Database connection settings
export THV_DATABASE_HOST=postgres.example.com
export THV_DATABASE_PORT=5432
export THV_DATABASE_USER=registry_app
export THV_DATABASE_DATABASE=registry_prod
export THV_DATABASE_SSLMODE=verify-full
export THV_DATABASE_MIGRATIONUSER=registry_migrator

# Connection pool settings
export THV_DATABASE_MAXOPENCONNS=25
export THV_DATABASE_MAXIDLECONNS=5
export THV_DATABASE_CONNMAXLIFETIME=1h
```

**Note**: Database passwords are managed via PostgreSQL's [`.pgpass` file](https://www.postgresql.org/docs/current/libpq-pgpass.html) or the `PGPASSFILE` environment variable, not through `THV_*` variables.

### Authentication Configuration

```bash
# Set authentication mode
export THV_AUTH_MODE=oauth  # or "anonymous" for development

# OAuth/OIDC configuration
export THV_AUTH_OAUTH_RESOURCEURL=https://registry.example.com
export THV_AUTH_OAUTH_REALM=mcp-registry
```

**Note**: OAuth provider configuration (including issuer URLs, audiences, and client secrets) should be specified in the YAML configuration file for security and complexity reasons.

### Logging Configuration

```bash
# Set log level (debug, info, warn, error)
export THV_LOG_LEVEL=debug

# Or use the unprefixed version (backward compatible)
export LOG_LEVEL=debug
```

## Docker and Docker Compose

Environment variables integrate seamlessly with Docker and Docker Compose:

### Docker Run

```bash
docker run \
  -e THV_DATABASE_HOST=postgres \
  -e THV_DATABASE_PORT=5432 \
  -e THV_AUTH_MODE=anonymous \
  -v $(pwd)/config.yaml:/config.yaml \
  toolhive-registry-api serve --config /config.yaml
```

### Docker Compose

```yaml
version: '3.8'

services:
  registry:
    image: toolhive-registry-api
    environment:
      THV_DATABASE_HOST: postgres
      THV_DATABASE_PORT: 5432
      THV_DATABASE_USER: registry_app
      THV_DATABASE_DATABASE: registry
      THV_AUTH_MODE: anonymous
      THV_LOG_LEVEL: info
    volumes:
      - ./config.yaml:/config.yaml
    command: serve --config /config.yaml
```

## Kubernetes

Environment variables work well with Kubernetes ConfigMaps and Secrets:

### ConfigMap for Non-Sensitive Values

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: registry-config
data:
  THV_DATABASE_HOST: "postgres-service"
  THV_DATABASE_PORT: "5432"
  THV_AUTH_MODE: "oauth"
  THV_LOG_LEVEL: "info"
```

### Deployment

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: registry-server
spec:
  template:
    spec:
      containers:
      - name: registry
        image: toolhive-registry-api
        envFrom:
        - configMapRef:
            name: registry-config
        volumeMounts:
        - name: config
          mountPath: /config.yaml
          subPath: config.yaml
      volumes:
      - name: config
        configMap:
          name: registry-yaml-config
```

## Limitations and Best Practices

### Complex Nested Structures

Environment variables have limited support for complex nested arrays and structures. The following configurations **should be defined in the YAML file**:

- `registries[]` - Registry configurations (multiple registries with nested settings)
- `registries[].syncPolicy` - Sync policy per registry
- `registries[].filter` - Filter rules per registry
- `auth.oauth.providers[]` - OAuth provider configurations

**Why?** These structures involve arrays of objects with multiple nested fields, which don't map cleanly to environment variables.

### Recommended Approach

1. **Use YAML for complex structures**: Define registries, OAuth providers, and filter rules in the YAML configuration file
2. **Use environment variables for overrides**: Override simple values like database host, authentication mode, or log level
3. **Use command-line flags sparingly**: Reserve flags for runtime overrides (e.g., `--address` for port changes)

### Security Considerations

1. **Secrets in YAML files**: For sensitive values like OAuth client secrets, use file references (e.g., `clientSecretFile`) rather than inline values
2. **Environment variables in logs**: Be careful not to log environment variables that might contain sensitive information
3. **Kubernetes Secrets**: For Kubernetes deployments, use Secret objects for sensitive environment variables:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: registry-secrets
type: Opaque
stringData:
  THV_AUTH_MODE: "oauth"
---
apiVersion: apps/v1
kind: Deployment
spec:
  template:
    spec:
      containers:
      - name: registry
        envFrom:
        - secretRef:
            name: registry-secrets
```

## Examples by Use Case

### Development Environment

Override auth mode for local testing:

```bash
# Start with anonymous auth for development
THV_AUTH_MODE=anonymous ./thv-registry-api serve --config config-dev.yaml
```

### CI/CD Pipeline

Override database and logging for integration tests:

```bash
export THV_DATABASE_HOST=localhost
export THV_DATABASE_PORT=5432
export THV_DATABASE_USER=test_user
export THV_DATABASE_DATABASE=test_registry
export THV_AUTH_MODE=anonymous
export THV_LOG_LEVEL=debug

./thv-registry-api serve --config config-test.yaml
```

### Production Deployment

Minimal environment overrides with production config:

```bash
# Production uses YAML for most config, only override deployment-specific values
export THV_DATABASE_HOST=postgres-primary.prod.svc.cluster.local
export THV_LOG_LEVEL=info

./thv-registry-api serve --config config-production.yaml
```

## Troubleshooting

### Check Configuration Precedence

To understand which configuration source is being used:

1. Enable debug logging: `THV_LOG_LEVEL=debug`
2. Check server startup logs for configuration values
3. Verify environment variables are set: `env | grep THV_`

### Common Issues

**Environment variable not taking effect:**
- Check the variable name matches the configuration key (use underscores, not dots)
- Verify the `THV_` prefix is present
- Ensure the variable is exported: `export THV_DATABASE_HOST=...`
- Remember: YAML values are loaded first, then overridden by env vars

**Configuration validation errors:**
- Environment variables are type-coerced by Viper (strings to integers, etc.)
- Invalid values will fail validation after unmarshaling
- Check the error message for the specific configuration key that failed

## Reference

For a complete list of all configuration options, see:
- [Configuration Reference](configuration.md) - Complete YAML configuration options
- [Database Setup](database.md) - Database configuration and credentials
- [Authentication](authentication.md) - OAuth/OIDC setup

For the underlying technology:
- [Viper Documentation](https://github.com/spf13/viper) - Configuration library used by ToolHive