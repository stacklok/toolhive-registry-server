# Authentication

The ToolHive Registry API server supports OAuth 2.0/OIDC authentication to protect API endpoints.

## Table of Contents

- [Overview](#overview)
- [Authentication Modes](#authentication-modes)
- [Multi-Provider Support](#multi-provider-support)
- [Configuration](#configuration)
- [Default Public Paths](#default-public-paths)
- [Provider Configuration](#provider-configuration)
- [RFC 9728 Support](#rfc-9728-protected-resource-metadata)
- [Examples](#examples)

## Overview

The server defaults to **OAuth mode** for security-by-default. For development environments, you can explicitly set `mode: anonymous`.

### Key Features

- OAuth 2.0 / OIDC token validation
- Multiple provider support with sequential fallback
- Kubernetes service account integration
- RFC 9728 Protected Resource Metadata
- Per-endpoint public path configuration

## Authentication Modes

| Mode | Description | Use Case |
|------|-------------|----------|
| `oauth` | OAuth 2.0/OIDC token validation | Production (default) |
| `anonymous` | No authentication required | Local development |

### Secure by Default

If no `auth` configuration is provided, the server defaults to OAuth mode. This ensures production deployments are secure by default.

To disable authentication for local development:

```yaml
auth:
  mode: anonymous
```

## Multi-Provider Support

The server supports multiple OAuth providers with sequential fallback. This enables supporting both Kubernetes service accounts and external identity providers simultaneously.

### How It Works

When a token is received:

1. The token is validated against each provider in order
2. If validation succeeds with any provider, the request is authenticated
3. If all providers fail, the request is rejected with 401 Unauthorized

### Example: Kubernetes + External IDP

```yaml
auth:
  mode: oauth
  oauth:
    resourceUrl: https://registry.example.com
    providers:
      - name: my-company-idp
        issuerUrl: https://auth.company.com
        audience: api://registry

      - name: kubernetes
        issuerUrl: https://kubernetes.default.svc
        audience: https://kubernetes.default.svc
        caCertPath: /var/run/secrets/kubernetes.io/serviceaccount/ca.crt
```

This configuration allows both:
- Employee access via company SSO
- Service-to-service access via Kubernetes service accounts

## Configuration

### Basic OAuth Configuration

```yaml
auth:
  # Authentication mode: anonymous or oauth (default: oauth)
  mode: oauth

  # OAuth/OIDC configuration (required when mode is "oauth")
  oauth:
    # URL identifying this protected resource (RFC 9728)
    resourceUrl: https://registry.example.com

    # OAuth/OIDC providers (at least one required)
    providers:
      - name: my-idp
        issuerUrl: https://idp.example.com
        audience: api://registry
```

### Full Configuration Example

```yaml
auth:
  mode: oauth

  # Additional paths that bypass authentication (optional)
  # These extend the default public paths listed below
  publicPaths:
    - /custom/public
    - /metrics

  # OAuth/OIDC configuration
  oauth:
    # URL identifying this protected resource (RFC 9728)
    # Used in /.well-known/oauth-protected-resource endpoint
    resourceUrl: https://registry.example.com

    # Protection space identifier for WWW-Authenticate header (optional)
    # Defaults to "mcp-registry"
    realm: mcp-registry

    # OAuth scopes supported by this resource (optional)
    # Defaults to ["mcp-registry:read", "mcp-registry:write"]
    scopesSupported:
      - mcp-registry:read
      - mcp-registry:write
      - mcp-registry:admin

    # OAuth/OIDC providers (at least one required)
    providers:
      - name: primary-idp
        issuerUrl: https://idp.example.com
        audience: api://registry

      - name: kubernetes
        issuerUrl: https://kubernetes.default.svc
        audience: https://kubernetes.default.svc
        caCertPath: /var/run/secrets/kubernetes.io/serviceaccount/ca.crt
```

## Default Public Paths

The following endpoints are always accessible without authentication:

- `/health` - Health check endpoint
- `/readiness` - Readiness probe endpoint
- `/version` - Version information
- `/.well-known/*` - OAuth discovery endpoints (RFC 9728)

Additional public paths can be configured using the `publicPaths` option.

## Provider Configuration

### Configuration Fields

| Field | Required | Description |
|-------|----------|-------------|
| `name` | Yes | Unique identifier for this provider |
| `issuerUrl` | Yes | OIDC issuer URL (must be HTTPS in production) |
| `jwksUrl` | No | Direct JWKS URL (skips OIDC discovery if specified) |
| `audience` | Yes | Expected audience claim in the token |
| `clientId` | No | OAuth client ID for token introspection |
| `clientSecretFile` | No | Path to file containing client secret |
| `caCertPath` | No | Path to CA certificate for TLS verification |
| `authTokenFile` | No | Path to bearer token file for authenticating to OIDC/JWKS endpoints |
| `introspectionUrl` | No | Token introspection endpoint (RFC 7662) for opaque tokens |
| `allowPrivateIP` | No | Allow OIDC endpoints on private IP addresses (required for in-cluster Kubernetes) |

### Kubernetes Provider

For Kubernetes service account tokens:

```yaml
- name: kubernetes
  issuerUrl: https://kubernetes.default.svc
  jwksUrl: https://kubernetes.default.svc/openid/v1/jwks  # Skip OIDC discovery
  audience: https://kubernetes.default.svc
  caCertPath: /var/run/secrets/kubernetes.io/serviceaccount/ca.crt
  authTokenFile: /var/run/secrets/kubernetes.io/serviceaccount/token  # Optional: for authenticated endpoints
  allowPrivateIP: true  # Required for in-cluster Kubernetes API server
```

> **Note:** Using `jwksUrl` is useful in Kubernetes where the OIDC discovery endpoint
> requires authentication. The JWKS endpoint is typically unauthenticated, but you can
> use `authTokenFile` if authentication is required.

### External IDP Provider

For external identity providers (Okta, Auth0, Keycloak, etc.):

```yaml
- name: company-sso
  issuerUrl: https://auth.company.com
  audience: api://toolhive-registry
```

### Self-Signed Certificate Provider

For providers with self-signed certificates:

```yaml
- name: internal-idp
  issuerUrl: https://internal-auth.company.local
  audience: api://registry
  caCertPath: /etc/ssl/certs/internal-ca.crt
```

## RFC 9728 Protected Resource Metadata

When OAuth is enabled, the server exposes an RFC 9728 compliant discovery endpoint:

```
GET /.well-known/oauth-protected-resource
```

### Response Example

```json
{
  "resource": "https://registry.example.com",
  "authorization_servers": [
    "https://idp.example.com",
    "https://kubernetes.default.svc"
  ],
  "bearer_methods_supported": ["header"],
  "scopes_supported": [
    "mcp-registry:read",
    "mcp-registry:write"
  ]
}
```

This endpoint allows OAuth clients to automatically discover authentication requirements.

## Examples

### Local Development (No Auth)

```yaml
auth:
  mode: anonymous
```

### Production with Single IDP

```yaml
auth:
  mode: oauth
  oauth:
    resourceUrl: https://registry.company.com
    providers:
      - name: company-sso
        issuerUrl: https://auth.company.com
        audience: api://toolhive-registry
```

### Kubernetes Deployment

```yaml
auth:
  mode: oauth
  oauth:
    resourceUrl: https://registry.company.com
    providers:
      # Allow employee access via corporate SSO
      - name: company-sso
        issuerUrl: https://auth.company.com
        audience: api://toolhive-registry

      # Allow service-to-service access within cluster
      - name: kubernetes
        issuerUrl: https://kubernetes.default.svc
        audience: https://kubernetes.default.svc
        caCertPath: /var/run/secrets/kubernetes.io/serviceaccount/ca.crt
```

### Multi-Region Setup

```yaml
auth:
  mode: oauth
  oauth:
    resourceUrl: https://registry-us-west.company.com
    providers:
      - name: us-idp
        issuerUrl: https://auth-us.company.com
        audience: api://toolhive-registry

      - name: eu-idp
        issuerUrl: https://auth-eu.company.com
        audience: api://toolhive-registry
```

## Testing Authentication

### Using curl with Bearer Token

```bash
# Get token from your IDP (example)
TOKEN="your-jwt-token-here"

# Make authenticated request
curl -H "Authorization: Bearer $TOKEN" \
  https://registry.example.com/registry/v0.1/servers
```

### Using kubectl with Service Account

```bash
# Get service account token
TOKEN=$(kubectl get secret <service-account-token> -o jsonpath='{.data.token}' | base64 -d)

# Make authenticated request
curl -H "Authorization: Bearer $TOKEN" \
  https://registry.example.com/registry/v0.1/servers
```

## Security Best Practices

1. **Always use OAuth in production** - Anonymous mode is for development only
2. **Use HTTPS for issuer URLs** - Required in production environments
3. **Validate audience claims** - Prevent token reuse across services
4. **Rotate secrets regularly** - Use short-lived tokens when possible
5. **Limit scope to minimum required** - Follow principle of least privilege
6. **Monitor authentication failures** - Set up alerts for repeated failures
7. **Use separate providers** - Don't share auth configuration across environments

## Troubleshooting

### 401 Unauthorized Errors

Check:
1. Token is not expired
2. Token is in `Authorization: Bearer <token>` header format
3. Issuer URL matches provider configuration
4. Audience claim matches provider configuration
5. Provider is reachable from the server

### Provider Connection Issues

Check:
1. Network connectivity to issuer URL
2. CA certificate path is correct (for self-signed certs)
3. DNS resolution for issuer hostname
4. Firewall rules allow HTTPS traffic

### Token Validation Failures

Enable debug logging to see detailed error messages:

```bash
thv-registry-api serve --config config.yaml --debug
```

## See Also

- [Configuration Guide](configuration.md) - Complete configuration reference
- [Kubernetes Deployment](deployment-kubernetes.md) - Deploy with authentication
- [Database Configuration](database.md) - Database security considerations