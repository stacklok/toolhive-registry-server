# Migration Guide: Removing Plain Text Password Support

## Overview

As of this update, the ToolHive Registry API Server **no longer supports plain text passwords in configuration files**. This change improves security by preventing passwords from being accidentally committed to version control or exposed in config files.

## What Changed

### Before (Deprecated)
```yaml
database:
  host: localhost
  port: 5432
  user: registry
  password: my-password  # ❌ NO LONGER SUPPORTED
  database: registry
```

### After (Required)

**Option 1: Environment Variable (Recommended for Docker/Development)**
```yaml
database:
  host: localhost
  port: 5432
  user: registry
  # Password comes from THV_DATABASE_PASSWORD environment variable
  database: registry
```

Set the environment variable:
```bash
export THV_DATABASE_PASSWORD=my-password
```

**Option 2: Password File (Recommended for Kubernetes/Production)**
```yaml
database:
  host: localhost
  port: 5432
  user: registry
  passwordFile: /secrets/db/password
  database: registry
```

Create the password file:
```bash
echo -n "my-password" > /secrets/db/password
chmod 400 /secrets/db/password
```

## Migration Steps

### For Docker Compose Users

**1. Update docker-compose.yml**

Add environment variable to your services:

```yaml
services:
  migrate:
    environment:
      - THV_DATABASE_PASSWORD=your-password  # Add this line

  registry-api:
    environment:
      - THV_DATABASE_PASSWORD=your-password  # Add this line
```

**2. Update config.yaml**

Remove the `password` field:

```yaml
database:
  host: postgres
  port: 5432
  user: registry
  # password: registry_password  # Remove this line
  database: registry
```

**3. Test**

```bash
docker-compose down
docker-compose up -d
docker-compose logs registry-api  # Verify it starts successfully
```

### For Kubernetes Users

**1. Create a Secret**

```bash
kubectl create secret generic db-password \
  --from-literal=password='your-password'
```

**2. Update your Deployment/Pod**

Mount the secret:

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
        secretName: db-password
        items:
          - key: password
            path: password
```

**3. Update config.yaml**

```yaml
database:
  host: postgres.prod.svc.cluster.local
  port: 5432
  user: registry
  passwordFile: /secrets/db/password  # Add this
  # password: old-password  # Remove this
  database: registry
```

### For Local Development

**Option A: Use Environment Variable (Simplest)**

```bash
# Add to your shell profile (.bashrc, .zshrc, etc.)
export THV_DATABASE_PASSWORD=postgres

# Or create a .env file (don't commit this!)
echo "export THV_DATABASE_PASSWORD=postgres" > .env
source .env

# Run the application
./bin/thv-registry-api serve --config config.yaml
```

**Option B: Use Password File**

```bash
# Create password file
mkdir -p ~/.config/thv-registry
echo -n "postgres" > ~/.config/thv-registry/db-password
chmod 400 ~/.config/thv-registry/db-password

# Update config.yaml
database:
  passwordFile: ~/.config/thv-registry/db-password
```

### For CI/CD Pipelines

**GitHub Actions**:
```yaml
- name: Run migrations
  env:
    THV_DATABASE_PASSWORD: ${{ secrets.DB_PASSWORD }}
  run: |
    ./bin/thv-registry-api migrate up --config config.yaml --yes
```

**GitLab CI**:
```yaml
migrate:
  variables:
    THV_DATABASE_PASSWORD: $DB_PASSWORD
  script:
    - ./bin/thv-registry-api migrate up --config config.yaml --yes
```

## Troubleshooting

### Error: "no database password configured"

**Cause**: Neither `passwordFile` nor `THV_DATABASE_PASSWORD` environment variable is set.

**Solution**: Choose one of the two methods above and configure it.

### Error: "failed to read password from file"

**Cause**: The password file doesn't exist or has wrong permissions.

**Solution**:
```bash
# Check file exists
ls -la /secrets/db/password

# Fix permissions
chmod 400 /secrets/db/password

# Verify content
cat /secrets/db/password
```

### Connection still fails

**Check**:
1. Password is correct
2. Environment variable is actually set: `echo $THV_DATABASE_PASSWORD`
3. Password file has correct content (no extra whitespace)
4. Database host and port are correct
5. Database user has proper permissions

## Benefits of This Change

✅ **Improved Security**: Passwords can't be accidentally committed to version control
✅ **Best Practices**: Follows industry standards (12-factor app, OWASP)
✅ **Kubernetes Native**: Works seamlessly with K8s Secrets
✅ **Docker Friendly**: Easy integration with Docker Compose
✅ **CI/CD Ready**: Works well with secret management in pipelines

## FAQ

**Q: Can I still use the old config with `password` field?**
A: No, this is no longer supported. You must migrate to `passwordFile` or environment variable.

**Q: Which method should I use?**
A:
- **Kubernetes/Production**: Use `passwordFile` with mounted secrets
- **Docker Compose/Development**: Use `THV_DATABASE_PASSWORD` environment variable
- **CI/CD**: Use `THV_DATABASE_PASSWORD` from your CI secret management

**Q: Is this a breaking change?**
A: Yes. If you have `password` in your config file, you must migrate to one of the secure methods.

**Q: Will my old configs break?**
A: Yes, if they use the `password` field. Update them using this migration guide.

**Q: Can I use both passwordFile and environment variable?**
A: Yes, but `passwordFile` takes priority if both are set.

## Support

If you encounter issues during migration:
1. Check [DATABASE_SECURITY.md](DATABASE_SECURITY.md) for detailed security documentation
2. Review [examples/](../examples/) for working configurations
3. Open an issue on GitHub with your specific problem

## Timeline

- **Before**: Plain text passwords supported (insecure)
- **Now**: Only `passwordFile` and `THV_DATABASE_PASSWORD` supported
- **Future**: May add support for external secret managers (Vault, AWS Secrets Manager)
