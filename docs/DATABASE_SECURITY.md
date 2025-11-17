# Database Security Best Practices

This document describes secure password handling for database connections.

## Overview

The ToolHive Registry API Server supports two secure methods for providing database passwords:

1. **Password File** (RECOMMENDED) - Read from a mounted secret file
2. **Environment Variable** - Set via `THV_DATABASE_PASSWORD`

**Note**: Plain text passwords in config files are NOT supported for security reasons.

## Priority Order

The password is resolved in this order:

```
1. passwordFile (if set) → read from file
2. THV_DATABASE_PASSWORD env var → if set
3. Error if neither is configured
```

## Method 1: Password File (RECOMMENDED for Production)

### Configuration

```yaml
database:
  host: postgres
  port: 5432
  user: registry
  passwordFile: /secrets/db/password  # Path to password file
  database: registry
  sslMode: require
```

### How It Works

- Reads password from the specified file
- File should contain only the password (trailing whitespace is trimmed)
- Uses `filepath.Clean()` to prevent path traversal attacks
- Ideal for Kubernetes Secrets and Docker Secrets

### Kubernetes Example

**Step 1: Create the secret**
```bash
kubectl create secret generic postgres-password \
  --from-literal=password='my-secure-password'
```

**Step 2: Mount the secret**
```yaml
apiVersion: v1
kind: Pod
metadata:
  name: registry-api
spec:
  containers:
    - name: api
      image: toolhive-registry-api:latest
      volumeMounts:
        - name: db-password
          mountPath: /secrets/db
          readOnly: true
  volumes:
    - name: db-password
      secret:
        secretName: postgres-password
        items:
          - key: password
            path: password
```

**Step 3: Reference in config**
```yaml
database:
  passwordFile: /secrets/db/password
```

### Docker Secrets Example

**Step 1: Create the secret**
```bash
echo "my-secure-password" | docker secret create postgres_password -
```

**Step 2: Use in docker-compose.yml**
```yaml
version: '3.8'
services:
  registry-api:
    image: toolhive-registry-api:latest
    secrets:
      - postgres_password
    volumes:
      - ./config.yaml:/config.yaml:ro
    command: ["serve", "--config", "/config.yaml"]

secrets:
  postgres_password:
    external: true
```

**Step 3: Reference in config**
```yaml
database:
  passwordFile: /run/secrets/postgres_password
```

## Method 2: Environment Variable

### Configuration

```yaml
database:
  host: postgres
  port: 5432
  user: registry
  # passwordFile not set
  # password not set - will use environment variable
  database: registry
```

### Set the environment variable

```bash
# Command line
export THV_DATABASE_PASSWORD=my-password
./thv-registry-api serve --config config.yaml

# Docker
docker run -e THV_DATABASE_PASSWORD=my-password ...

# Docker Compose
services:
  registry-api:
    environment:
      - THV_DATABASE_PASSWORD=my-secure-password

# Kubernetes
env:
  - name: THV_DATABASE_PASSWORD
    valueFrom:
      secretKeyRef:
        name: postgres-credentials
        key: password
```

### Use Cases

- CI/CD pipelines
- Development environments
- When file-based secrets aren't available
- Quick testing

### Security Considerations

- Environment variables can be visible in process listings
- May appear in logs or error messages
- Less secure than file-based secrets
- Better than plain text in config files

## Why No Plain Text Config Support?

For security reasons, this project does NOT support plain text passwords in configuration files.

**Reasons:**
- ❌ Config files often get committed to version control
- ❌ No encryption or protection
- ❌ Visible to anyone with file access
- ❌ Easy to accidentally expose in logs or backups
- ❌ Does not follow security best practices

**Use passwordFile or environment variables instead** - both provide better security with minimal complexity.

## Password Special Characters

The system properly handles passwords with special characters:

- Passwords from **files** are trimmed of whitespace
- Passwords in **connection strings** are URL-escaped
- Supports all characters including: `!@#$%^&*()+={}[]|:;"'<>,.?/~`

Example password: `P@ssw0rd!#123` works correctly in all methods.

## Security Best Practices

### DO ✅

1. **Use passwordFile in production**
2. **Enable SSL/TLS** (`sslMode: require`)
3. **Use strong passwords** (16+ characters, mixed case, numbers, symbols)
4. **Rotate passwords regularly** (every 90 days)
5. **Use Kubernetes/Docker Secrets** for production deployments
6. **Set restrictive file permissions** on password files (0400 or 0600)
7. **Monitor access logs** for unauthorized attempts
8. **Use connection pooling** to limit database connections

### DON'T ❌

1. **Never commit passwords to version control**
2. **Don't use default passwords** (postgres, admin, etc.)
3. **Don't share passwords** across environments
4. **Don't log passwords** in application logs
5. **Don't store passwords unencrypted** on disk
6. **Don't use weak passwords** (dictionary words, common patterns)
7. **Don't disable SSL** in production (`sslMode: disable`)

## Quick Start

### For Docker Compose (recommended)

Set environment variable in `docker-compose.yml`:
```yaml
services:
  registry-api:
    environment:
      - THV_DATABASE_PASSWORD=your-password
```

### For Kubernetes (recommended)

Create a secret and mount it:
```bash
kubectl create secret generic db-password --from-literal=password='your-password'
```

Mount in pod and reference in config:
```yaml
database:
  passwordFile: /secrets/db/password
```

### For Local Development

Set environment variable:
```bash
export THV_DATABASE_PASSWORD=postgres
./thv-registry-api serve --config config.yaml
```

## Troubleshooting

### Error: "failed to read password from file"

**Cause:** File doesn't exist or insufficient permissions

**Solution:**
```bash
# Check file exists
ls -la /secrets/db/password

# Check permissions
chmod 400 /secrets/db/password

# Check file content
cat /secrets/db/password
```

### Error: "no database password configured"

**Cause:** No password provided via any method

**Solution:** Set password using one of the three methods above.

### Error: "failed to connect to database"

**Cause:** Password is correct but connection fails

**Check:**
- Database host and port are correct
- User has proper permissions
- SSL mode is appropriate
- Firewall allows connections

### Password with special characters fails

**Cause:** Password contains characters that break parsing

**Solution:** The system handles this automatically via URL encoding. If still failing:
1. Verify password is correct (no extra whitespace)
2. Use passwordFile instead of environment variable
3. Test password directly with `psql`

## Examples

### Local Development (Environment Variable)
```bash
export THV_DATABASE_PASSWORD=postgres
```

```yaml
database:
  host: localhost
  port: 5432
  user: postgres
  # Password comes from THV_DATABASE_PASSWORD environment variable
  database: registry
  sslMode: disable
```

### Production (Kubernetes)
```yaml
database:
  host: postgres.production.svc.cluster.local
  port: 5432
  user: registry_user
  passwordFile: /secrets/db/password  # Mounted from K8s Secret
  database: registry_prod
  sslMode: require
```

### CI/CD (GitHub Actions)
```yaml
database:
  host: postgres
  port: 5432
  user: postgres
  # Use THV_DATABASE_PASSWORD environment variable
  database: test_db
  sslMode: disable
```

```yaml
# .github/workflows/test.yml
- name: Run tests
  env:
    THV_DATABASE_PASSWORD: ${{ secrets.POSTGRES_PASSWORD }}
  run: ./thv-registry-api serve --config config.yaml
```

## Related Documentation

- [Database Configuration](../README.md#database-configuration)
- [Docker Deployment](DOCKER.md)
- [Kubernetes Deployment](../deployment/kubernetes/README.md)
- [Migration Guide](MIGRATIONS.md)
