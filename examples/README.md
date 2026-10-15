# Configuration and Go examples

For Go library onboarding, start with the [runnable embedding tutorial](embedding/README.md).
It serves an existing PostgreSQL catalog with host-owned mTLS admission and shutdown.
The configurations below are for the standalone CLI.

Choose an example by source and deployment environment. These are templates:
configure a real PostgreSQL endpoint, credentials and trusted OAuth issuer/audience
before serving. Admin defaults off. Standalone evaluates built-in allow-all Cedar
after authentication; no group/scope mapping is needed for that policy. When changing
an issuer, remove unused `authorization.identityMappings` or update their issuer to
match the configured provider exactly. Mismatched mappings fail startup.

## Choose a configuration

| File | Purpose |
|---|---|
| [config-git.yaml](config-git.yaml) | Git catalog, 30-minute polling |
| [config-api.yaml](config-api.yaml) | Upstream MCP API, hourly polling |
| [config-file.yaml](config-file.yaml) | Local catalog file, 5-minute polling |
| [config-docker.yaml](config-docker.yaml) | Repository Compose DB and container paths |
| [config-docker-multiple.yaml](config-docker-multiple.yaml) | Multiple sources/views in Compose |
| [config-docker-with-managed.yaml](config-docker-with-managed.yaml) | External and managed sources |
| [config-database-dev.yaml](config-database-dev.yaml) | Local DB with separate runtime/migration logins |
| [config-database-prod.yaml](config-database-prod.yaml) | Production DB template |
| [config-database-passwordfile.yaml](config-database-passwordfile.yaml) | pgpass credential template |
| [config-oauth.yaml](config-oauth.yaml) | OAuth provider settings |
| [config-complete.yaml](config-complete.yaml) | Detailed configuration reference |
| [config-filter-test.yaml](config-filter-test.yaml) | Source filtering example |

Historical claims fixtures in [migration-fixtures](migration-fixtures/README.md)
are offline tooling fixtures, not deployment examples. Selected startup rejects
retired `auth.authz`, roles/publicPaths and resource claims by presence.

## Run a prepared configuration

Follow [database setup](../docs/database.md#setup-guide) first. For a genuinely new,
entirely empty catalog only:

```sh
thv-registry-api serve --config examples/config-git.yaml --initialize-empty
```

For an existing catalog, complete the
[offline exposure procedure](../docs/registry-exposure-migration.md#maintenance-procedure)
before using the new runtime. A current schema-30 catalog with a consumed same-target
receipt starts normally:

```sh
thv-registry-api serve --config examples/config-git.yaml
```

The public listener is 8080. Probes/metrics exist only on internal 8081. For direct
local binary use, bind operational traffic to loopback with
`--internal-address 127.0.0.1:8081`; do not expose it via a public proxy.

Store `Authorization: Bearer <access-token>` from the configured issuer/audience
in a private mode-0600 header file outside the repository. Set `AUTH_HEADER_FILE`
to its absolute path and use the example's `default` view:

```sh
curl --fail-with-body --header "@${AUTH_HEADER_FILE:?Set private header file path}" \
  http://localhost:8080/registry/default/v0.1/servers
curl --fail http://localhost:8081/health
curl --fail http://localhost:8081/readiness
```

Use HTTPS outside loopback development. Probe success does not prove OAuth/Cedar
admission or successful upstream refresh.

## Sources and views

Define fetch/publication settings under **`sources`** and ordered source names
under **`registries`**. The earliest source wins all versions of each `(kind,name)`
before consumer search/version/latest/pagination. Views are not privacy boundaries.
All following snippets are source/view fragments; use the linked full examples
for authentication and database settings.

### Git

From [config-git.yaml](config-git.yaml):

```yaml
sources:
  - name: toolhive
    git:
      repository: https://github.com/stacklok/toolhive-catalog.git
      branch: main
      path: pkg/catalog/toolhive/data/registry-upstream.json
    syncPolicy:
      interval: "30m"
registries:
  - name: default
    sources: [toolhive]
```

Use exactly one of `branch`, `tag` or `commit` to select a ref. To restrict content,
place `filter` alongside that source's `git`/`syncPolicy`, not inside a view.
Private Git credentials use an absolute `git.auth.passwordFile` mounted read-only.

### API

From [config-api.yaml](config-api.yaml):

```yaml
sources:
  - name: mcp-upstream
    api:
      endpoint: https://registry.modelcontextprotocol.io
    syncPolicy:
      interval: "1h"
registries:
  - name: default
    sources: [mcp-upstream]
```

The importer fetches the upstream `/v0.1/servers` API and completes pagination
before validating/committing a snapshot. This upstream URL is distinct from this
server's named consumer path `/registry/default/v0.1/servers`.

### File

From [config-file.yaml](config-file.yaml):

```yaml
sources:
  - name: local-file
    file:
      path: ./examples/upstream-registry.json
    syncPolicy:
      interval: "5m"
registries:
  - name: default
    sources: [local-file]
```

Paths resolve in the server process's filesystem; containers need their mounted
paths (Compose uses `/examples/upstream-registry.json`).
[upstream-registry.json](upstream-registry.json) demonstrates the import envelope;
see [formats](../docs/registry-formats.md) for exact payload contracts. File sources
also support URL or inline data as alternatives to `path`; inline data bypasses
scheduled polling.

### Managed and Kubernetes

`managed: {}` is the singleton API-publication source, not a poller. Publication
requires enabled authenticated admin routes and Cedar permission.
Kubernetes sources watch export-opted-in resources and periodically reconcile
full-scope fenced snapshots, not on-demand consumer queries. They do not take
`syncPolicy`; see [operator configuration](../docs/configuration.md#kubernetes).

For combined sources/views, use [config-complete.yaml](config-complete.yaml) or
[config-docker-multiple.yaml](config-docker-multiple.yaml), not separate competing
config owners against the same database.

## Admission and deployment

For an optional restriction, author a custom policy using
[write a policy](../docs/registry-cedar-authorization.md#write-a-policy), set an
absolute `authorization.policyFile`, and mount that file read-only. It replaces
the built-in policy entirely; missing/empty/invalid specified files fail startup.
Attribute mappings are needed only when the custom policy uses those attributes.
Admin stays off unless explicitly enabled; anonymous catalog access needs both
explicit opt-in and a Cedar permit (the built-in policy supplies that permit).
Invalid supplied credentials never become anonymous.

Use these authoritative deployment paths rather than independent manifests:

- [Compose: new and existing catalogs](../docs/deployment-docker.md): current-source
  development image, explicit new-empty override, volume-preserving lifecycle,
  optional custom mount and production credential limitations.
- [Helm/Kubernetes: new and existing catalogs](../docs/deployment-kubernetes.md):
  complete `operator-values.yaml`, image selection, database roles/Secret, readable
  mode-0600 pgpass, OAuth, optional custom policy and internal-only probes.

For local kind tasks, prepare `VALUES` using the Kubernetes guide, including its DB
and trusted OAuth settings. `registry-server-install-local` builds current source;
`registry-server-install-latest` requires an explicitly selected catalog-capable
`REGISTRY_SERVER_IMAGE`. Neither picks initialization consent implicitly:

```sh
task registry-server-install-local VALUES="$CATALOG_VALUES"
task registry-server-install-latest VALUES="$CATALOG_VALUES" REGISTRY_SERVER_IMAGE="$REGISTRY_SERVER_IMAGE"
```

Set `initializeEmptyCatalog: true` only for a genuinely new empty DB. These tasks
require the deliberately prepared local cluster/kubeconfig; do not run them against
a user cluster by accident. [cnpg](cnpg/) supplies additional database templates,
not a substitute for production secret provisioning or offline migration review.

## Monitoring and troubleshooting

[Observability](../docs/observability.md) documents actual public HTTP and ingestion
metrics/traces. Scheduled Git/API/file reconciliation uses database leases and
atomic fenced commits; failures retain the last good snapshot. Use
[publication diagnostics](../docs/registry-publication-diagnostics.md) when admin is
enabled to inspect source state. Do not rely on obsolete sync-manager log strings.

- **Startup refused:** check DB configuration, exact issuer mappings, frozen policy
  and exposure receipts. Empty initialization cannot bypass historical review.
- **Fetch failed:** verify the source URL/ref/path and server-side network access;
  private Git credentials must be readable at their configured absolute path.
- **401/403:** check token issuer/audience/expiry and custom Cedar permissions.
- **Credentials failed:** check file ownership/mode and exact DB tuple, never print
  credentials or environment dumps.

## See also

- [Project overview](../README.md#toolhive-registry-server)
- [Configuration reference](../docs/configuration.md)
- [Component architecture](../docs/component-architecture.md)
- [Consumer HTTP contract](../docs/registry-consumer-http.md)
- [Ingestion](../docs/registry-ingestion.md)
