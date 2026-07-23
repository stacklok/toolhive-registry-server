# Lazy Tool Discovery

When a `MCPServer`, `VirtualMCPServer`, or `MCPRemoteProxy` CRD is exported to the
registry (via `toolhive.stacklok.dev/registry-export: "true"`) but carries no
`toolhive.stacklok.dev/tool-definitions` annotation, the registry-server controller
automatically connects to the proxy's Service URL and calls `tools/list` to discover the
tool set at reconcile time. The result is written directly to the database; no Kubernetes
patch is performed and no new RBAC is required.

---

## How it works

Discovery is triggered per-CRD during the normal reconcile loop:

1. The controller extracts the `registry-url` annotation as the connection target.
2. If `tool-definitions` is **absent** and `registry-url` is present, `discoverTools` is
   called with the configured timeout and optional OAuth2 `TokenSource`.
3. `discoverTools` attempts a Streamable-HTTP MCP connection, falls back to SSE, performs
   the MCP handshake, and calls `ListTools`.
4. Non-empty results are stored in `extensions.ToolDefinitions` and persisted via the sync
   writer. Any failure (connection error, empty list, token error) logs a warning and
   leaves `ToolDefinitions` empty — the entry is still registered, and discovery retries
   on the next reconcile.

### Decision table

| `tool-definitions` annotation | `registry-url` annotation | Source of `ToolDefinitions` |
|:---:|:---:|---|
| ✅ present | any | annotation value used directly — lazy discovery **not triggered** |
| ❌ absent | ✅ present | **lazy discovery** — `tools/list` called at reconcile time |
| ❌ absent | ❌ absent | empty — no HTTP connection |

The `tools` annotation (name-only list) is independent and unaffected by lazy discovery.

---

## OAuth2 authentication

Production MCP proxies typically enforce Cedar authorization. Anonymous `tools/list`
requests are rejected with 401. To authenticate, configure an OAuth2
`client_credentials` grant under the `kubernetes` source:

```yaml
sources:
  - name: k8s-source
    kubernetes:
      namespaces: ["toolhive-system"]
      discoverTimeout: "15s"
      discoverOAuth2:
        tokenUrl: https://keycloak.example.com/realms/toolhive/protocol/openid-connect/token
        clientId: registry-server-discovery
        clientSecretFile: /var/run/secrets/toolhive/discover-oauth2/client-secret
        scopes: ["openid"]
        # audience: registry-server       # optional (Auth0-style)
        # endpointParams:                 # optional extra token endpoint params
        #   resource: https://mcp.example
```

The `clientSecretFile` must be an **absolute path** to a file containing the secret
(symlinks are resolved via `filepath.EvalSymlinks`). Tokens are cached and reused until
expiry by `golang.org/x/oauth2/clientcredentials`.

When `discoverOAuth2` is omitted, discovery runs anonymously (no `Authorization` header).

### Keycloak setup (example)

1. Create a confidential client `registry-server-discovery` with `client_credentials`
   grant enabled.
2. Create a Protocol Mapper: **User Attribute → roles**, Token Claim Name **`roles`**
   (not `claim_roles` — the ToolHive proxy prefixes claims with `claim_`, so the Cedar
   attribute becomes `claim_roles`).
3. Assign the client a service account role that the Cedar policy allows for
   `tools/list` discovery.
4. Mount the client secret as a Kubernetes `Secret` volume at the path given in
   `clientSecretFile`.

> **Note on the `claim_X` convention**: The ToolHive proxy maps JWT claim `X` to Cedar
> entity attribute `claim_X`. Therefore the Protocol Mapper Token Claim Name must be the
> bare claim name (e.g. `roles`), not the prefixed form (e.g. `claim_roles`), to avoid
> a double-prefix (`claim_claim_roles`) that Cedar policies do not match.

---

## Configuration reference

### `discoverOAuth2` fields

| Field | Type | Required | Description |
|-------|------|:--------:|-------------|
| `tokenUrl` | string | **Yes** | OAuth2 token endpoint URL |
| `clientId` | string | **Yes** | OAuth2 client identifier |
| `clientSecretFile` | string | **Yes** | Absolute path to the file containing the client secret |
| `scopes` | []string | No | OAuth2 scopes to request |
| `audience` | string | No | `audience` parameter sent to the token endpoint (Auth0-style) |
| `endpointParams` | map[string]string | No | Additional token-endpoint parameters |

### `discoverTimeout`

Per-server timeout for the `tools/list` call (default `10s`). Set higher if proxies are
slow to initialize.

---

## Kubernetes deployment (Helm)

Mount the client secret as a volume and reference it via `clientSecretFile`:

```yaml
# values.yaml
config:
  sources:
    - name: k8s-source
      kubernetes:
        namespaces: ["toolhive-system"]
        discoverTimeout: "15s"
        discoverOAuth2:
          tokenUrl: https://keycloak.example.com/realms/toolhive/protocol/openid-connect/token
          clientId: registry-server-discovery
          clientSecretFile: /var/run/secrets/toolhive/discover-oauth2/client-secret
          scopes: ["openid"]

extraVolumes:
  - name: discover-oauth2
    secret:
      secretName: registry-discovery-oauth2
      items:
        - key: client-secret
          path: client-secret

extraVolumeMounts:
  - name: discover-oauth2
    mountPath: /var/run/secrets/toolhive/discover-oauth2
    readOnly: true
```

Create the Kubernetes Secret:

```bash
kubectl create secret generic registry-discovery-oauth2 \
  --from-literal=client-secret=<your-client-secret> \
  -n toolhive-system
```

If the proxy enforces an HTTP (non-TLS) issuer URL in development, set:

```yaml
extraEnv:
  - name: THV_REGISTRY_INSECURE_URL
    value: "true"
```

---

## Workload annotations reference

All three CRD types (`MCPServer`, `VirtualMCPServer`, `MCPRemoteProxy`) support:

| Annotation | Required | Description |
|------------|:--------:|-------------|
| `toolhive.stacklok.dev/registry-export` | **Yes** | Must be `"true"` to include in the registry |
| `toolhive.stacklok.dev/registry-url` | **Yes** | Proxy Service endpoint URL (also the lazy discovery target) |
| `toolhive.stacklok.dev/registry-description` | **Yes** | Description shown in the registry listing |
| `toolhive.stacklok.dev/registry-title` | No | Human-readable display name |
| `toolhive.stacklok.dev/registry-icon` | No | Icon URL |
| `toolhive.stacklok.dev/registry-category` | No | Category tag for grouping and filtering |
| `toolhive.stacklok.dev/tools` | No | JSON array of tool **names** — independent of lazy discovery |
| `toolhive.stacklok.dev/tool-definitions` | No | JSON array of full tool definitions (name, description, inputSchema, …) — if absent, lazy discovery is triggered |
| `toolhive.stacklok.dev/authz-claims` | No | JSON object of authorization claims controlling entry visibility |

### Tool definitions format

Each entry in `tool-definitions` follows the MCP spec:

```json
[
  {
    "name": "analyze_code",
    "description": "Analyze code for potential issues",
    "inputSchema": {
      "type": "object",
      "properties": { "file": { "type": "string" } },
      "required": ["file"]
    }
  }
]
```

Only JSON syntax is validated; schema semantics are not checked. Invalid JSON is logged
as a warning and the tool definitions are omitted, but the server entry is still
registered.

---

## Registry API response structure

Tool definitions appear in the `_meta` field, nested under the proxy URL from
`registry-url`:

```json
{
  "_meta": {
    "io.modelcontextprotocol.registry/publisher-provided": {
      "io.github.stacklok": {
        "https://mcp.example.com/servers/my-mcp-server": {
          "tool_definitions": [
            { "name": "analyze_code", "description": "..." }
          ]
        }
      }
    }
  }
}
```

To extract tool names with `jq`:

```bash
curl -s http://localhost:8080/registry/default/v0.1/servers | jq '
  [.servers[] | {
    name: .name,
    tools: (
      (.server._meta["io.modelcontextprotocol.registry/publisher-provided"] // {})
      | to_entries[] | .value | to_entries[] | .value.tool_definitions? // []
      | .[].name
    )
  }]'
```
