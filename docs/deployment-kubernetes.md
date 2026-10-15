# Kubernetes deployment

Use the [repository Helm chart](../deploy/charts/toolhive-registry-server/README.md).
It deploys the standalone catalog API, not a sidecar, and does **not** provision
PostgreSQL. The following is the authoritative install/upgrade path; commands are
for an operator to run in a deliberately selected cluster context.

## Prepare database, image and credentials

Prerequisites: Kubernetes 1.25+, Helm 3.10+, kubectl, a PostgreSQL 16+ endpoint,
and a trusted HTTPS OAuth issuer/audience. Use the chart from this checkout and
an image built from this branch, or a pinned release verified to implement its
catalog startup/receipt enforcement. Published binary images live at
`ghcr.io/stacklok/thv-registry-api`; do not assume released `latest` includes this
runtime. See [source image builds](deployment-docker.md#images-and-standalone-containers).
A local image must be loaded into the selected cluster's nodes or made available
in an operator-managed registry; building locally alone does not make it pullable.

1. Provision PostgreSQL and the **two non-superuser roles** in
   [database setup](database.md#setup-guide): `thv_user` for runtime,
   `thv_migrator` for migrations. For an existing DB preserve schema ownership
   and use its existing credentials. This guide uses
   `postgres.database.svc.cluster.local:5432`, database `toolhive_registry`;
   replace that host consistently in values and pgpass.
2. Prepare a private pgpass file with those two role passwords using
   [the credential format](database.md#pgpass-file). Do not put passwords in
   Helm values: `config` is rendered into a ConfigMap.
3. In your selected context, create the namespace and Secret from the private
   file (set `PGPASS_PATH` to its absolute path). Do not print its contents:

   ```sh
   kubectl create namespace toolhive-system
   kubectl -n toolhive-system create secret generic registry-db-pgpass \
     --from-file="pgpass=${PGPASS_PATH:?Set private pgpass file path}"
   ```

## Prepare operator-values.yaml

Save the following as an operator-owned `operator-values.yaml`. Replace the image
placeholder with your exact current-runtime image and customize the DB and OAuth
provider. Built-in Cedar needs no group/scope mappings. If a custom policy uses
attributes, add mappings whose issuer exactly matches the trusted provider.
Keep admin off unless deliberately granting every authenticated user all enabled
admin operations with the default policy.

```yaml
image:
  registryServerUrl: REPLACE_WITH_CURRENT_RUNTIME_IMAGE
  pullPolicy: IfNotPresent
initializeEmptyCatalog: false
service:
  type: ClusterIP
  exposeInternalPort: false # Probes still reach the pod's 8081 listener.
podSecurityContext:
  fsGroup: 65535
config:
  admin:
    enabled: false
  authorization:
    allowAnonymousCatalog: false
    identityMappings: []
  auth:
    mode: oauth
    oauth:
      resourceUrl: https://registry.example.com
      providers:
        - name: company
          issuerUrl: https://id.example.com
          audience: registry-api
  database:
    host: postgres.database.svc.cluster.local
    port: 5432
    database: toolhive_registry
    user: thv_user
    migrationUser: thv_migrator
    sslMode: verify-full
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
extraEnv:
  - name: PGPASSFILE
    value: /run/registry-credentials/pgpass
initContainers:
  - name: prepare-pgpass
    image: cgr.dev/chainguard/busybox:latest # Pin an approved digest for production.
    command:
      - sh
      - -c
      - cp /credential-input/pgpass /credential-output/pgpass && chmod 600 /credential-output/pgpass
    securityContext:
      runAsNonRoot: true
      runAsUser: 65535
      runAsGroup: 65535
      allowPrivilegeEscalation: false
      readOnlyRootFilesystem: true
      capabilities:
        drop: [ALL]
      seccompProfile:
        type: RuntimeDefault
    volumeMounts:
      - name: pgpass-secret
        mountPath: /credential-input
        readOnly: true
      - name: pgpass-prepared
        mountPath: /credential-output
extraVolumes:
  - name: pgpass-secret
    secret:
      secretName: registry-db-pgpass
      defaultMode: 0440
  - name: pgpass-prepared
    emptyDir:
      medium: Memory
extraVolumeMounts:
  - name: pgpass-prepared
    mountPath: /run/registry-credentials
    readOnly: true
```

The init container runs as the chart's application UID 65535. `fsGroup` permits
reading the root-owned Secret input and writing the emptyDir; the copied file is
owned by UID 65535 with mode **0600**. Mounting a root-owned 0600 Secret directly
is unreadable by that UID. Making the final pgpass group-readable (0640) is not a
solution: PostgreSQL pgpass requires no group/other access. No root init container
or broad pod capabilities are needed. Secret changes require recreating pods to
copy new credentials; the prepared file is not a live Secret projection.

For `verify-full`, PostgreSQL must present a certificate valid for the configured
host and trusted by the client. Mount the provider's CA file and set `PGSSLROOTCERT`
via `extraEnv` if it is not already trusted. Do not disable TLS for production.

### Optional custom admission and Kubernetes sources

Create an external ConfigMap containing a reviewed `policies.cedar` key, then set
`policy.existingConfigMap` in values. The chart mounts it and selects the absolute
policy path. A specified missing/invalid file fails startup; it replaces the
built-in allow-all policy entirely. Follow the
[Cedar guide](registry-cedar-authorization.md#write-a-policy). Roll out every replica
after external policy edits; drain old replicas for revocations.

Kubernetes sources watch opted-in MCPServer, VirtualMCPServer and MCPRemoteProxy
resources and periodically reconcile full-scope snapshots through DB leases.
They do not query the cluster on each consumer request. Follow the
[operator configuration](configuration.md#kubernetes) and chart RBAC values for
namespace scope; the configured scope must be readable for all three kinds.
Mounting config/Secrets alone does not require granting API access to Secrets.

If accepting Kubernetes service-account OAuth tokens, use the cluster's actual
issuer/audience and CA. For an issuer that resolves to the private cluster API,
explicitly set **that provider's** `allowPrivateIP: true`, for example:

```yaml
# Additional auth.oauth.providers entry; use the actual cluster issuer/audience.
- name: kubernetes
  issuerUrl: https://kubernetes.default.svc
  audience: https://kubernetes.default.svc
  caCertPath: /var/run/secrets/kubernetes.io/serviceaccount/ca.crt
  allowPrivateIP: true
```

This opt-in does not disable TLS verification and should not be enabled globally
for unrelated external providers.

## New, entirely empty database

Set `initializeEmptyCatalog: true` in the prepared values **only** for a genuinely
new empty catalog. It authorizes empty-install preparation, not consumer access;
it never approves existing data. The chart supplies `serve --initialize-empty`.
It is safe to retain on same-target restarts once the receipt is consumed.

Render and inspect before installing (these first two commands are local only):

```sh
helm lint deploy/charts/toolhive-registry-server -f operator-values.yaml
helm template toolhive-registry-server deploy/charts/toolhive-registry-server \
  -n toolhive-system -f operator-values.yaml > rendered-registry.yaml
helm upgrade --install toolhive-registry-server deploy/charts/toolhive-registry-server \
  -n toolhive-system -f operator-values.yaml --wait
```

The Service exposes only 8080 with these values. Provide trusted TLS ingress to
that port for clients; do not forward 8081 through public ingress/load balancers.

## Existing database and upgrades

An existing current-runtime catalog with a consumed same-target receipt and clean
schema 30 can use the same install command with `initializeEmptyCatalog: false`.
For a historical catalog, do **not** install a new serving replica or run a generic
migration Job alongside old writers:

1. Stop/drain every reader, writer, controller, sync worker and embedding host,
   including replicas outside this Helm release. Prevent automatic restarts.
2. Take/verify a full DB/roles/config/policy backup. Prepare the final effective
   config and frozen policy, including operator exports and credential mounts.
3. Run the [offline exposure procedure](registry-exposure-migration.md#maintenance-procedure)
   with this binary and that exact config/DB host: stop at schema 29, inventory,
   explicitly acknowledge, retire claims, **finalize at 29**, then migrate to 30.
   Use an operator-controlled offline CLI environment with the same network,
   credential files, policy and Kubernetes scope as the deployment. The chart
   does not implement these review commands or infer consent.
4. Install the final chart/image with initialization false. Never restart an old
   binary against schema 30; `migrate down` cannot undo this transition. Recovery
   requires the verified full backup while everything remains offline.

Do not promise zero-downtime upgrades across this boundary. RollingUpdate,
`maxUnavailable: 0`, and Helm rollback do not make mixed old/new writers safe.
For later compatible updates, assess schema/config compatibility before selecting
an ordinary rolling strategy; a chart rollback does not roll back DB state.

## Verify and operate

```sh
kubectl -n toolhive-system rollout status deployment/toolhive-registry-server
kubectl -n toolhive-system logs deployment/toolhive-registry-server
kubectl -n toolhive-system port-forward deployment/toolhive-registry-server \
  --address 127.0.0.1 8080:8080 8081:8081
```

In another terminal, use a private mode-0600 header file containing
`Authorization: Bearer <access-token>` from the configured IdP:

```sh
curl --fail http://localhost:8081/health
curl --fail http://localhost:8081/readiness
curl --fail-with-body --header "@${AUTH_HEADER_FILE:?Set private header file path}" \
  http://localhost:8080/registry/default/v0.1/servers
```

Probes and enabled `/metrics` exist only on internal 8081 and have no auth/audit or
HTTP telemetry middleware. The chart's liveness/readiness probes already target
`internal-http`. Enable telemetry as in [observability](observability.md); configure
a trusted scraper to reach pod port 8081, or deliberately enable the internal
ClusterIP port and restrict it with the
[chart NetworkPolicy example](../deploy/charts/toolhive-registry-server/README.md#internal-port-exposure).

NetworkPolicy ingress isolation applies to **all ports** of selected pods: allow
public-client/ingress-controller traffic to 8080 separately from trusted monitoring
to 8081. With egress isolation, allow DNS (UDP/TCP), the exact DB endpoint/port,
OAuth discovery/JWKS/introspection, configured Git/API/file URLs, the Kubernetes
API for operator sources, and OTLP if enabled. `namespaceSelector: {}` selects
cluster pods, **not external HTTPS services**. Standard NetworkPolicy has no FQDN
rules: use approved destination CIDRs, an egress gateway/proxy, or CNI-supported
FQDN policy accounting for DNS/address changes. Do not blindly allow all internet
egress to compensate for an incorrect pod selector.

On failures, inspect startup/probe and init-container logs, config validity, file
ownership and exact credential tuples without dumping secrets or environments.
A healthy probe does not prove a token or Cedar policy admits API requests. For
dirty schema/receipt failures remain offline and follow the recovery procedure,
never auto-force a version or retry empty initialization against existing data.
