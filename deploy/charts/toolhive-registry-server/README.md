# ToolHive Registry Server Helm Chart

A Helm chart for deploying the ToolHive Registry Server - the central metadata hub for enterprise MCP governance and discovery

**Homepage:** <https://github.com/stacklok/toolhive-registry-server>

## Source Code

* <https://github.com/stacklok/toolhive-registry-server>

---

## TL;DR

```console
helm upgrade -i toolhive-registry-server oci://ghcr.io/stacklok/toolhive-registry-server/toolhive-registry-server -n toolhive-system --create-namespace
```

Or for a custom values file:

```consoleCustom
helm upgrade -i toolhive-registry-server oci://ghcr.io/stacklok/toolhive-registry-server/toolhive-registry-server -n toolhive-system --create-namespace --values values-custom.yaml
```

## Prerequisites

- Kubernetes 1.25+
- Helm 3.10+ minimum, 3.14+ recommended

## Usage

### Installing from the Chart

Install one of the available versions:

```shell
helm upgrade -i <release_name> oci://ghcr.io/stacklok/toolhive-registry-server/toolhive-registry-server --version=<version> -n toolhive-system --create-namespace
```

> **Tip**: List all releases using `helm list`

### Uninstalling the Chart

To uninstall/delete the `toolhive-registry-server` deployment:

```console
helm uninstall <release_name>
```

The command removes all the Kubernetes components associated with the chart and deletes the release. You will have to delete the namespace manually if you used Helm to create it.

### Internal Port Exposure

The Service exposes `service.internalPort` (default `8081`: `/health`, `/readiness`,
`/version`, and `/metrics` when telemetry is enabled) alongside the public API port.
Unlike the public port, the internal port carries no authentication, no audit logging,
and no rate limiting by design — it is meant for Kubernetes probes and Prometheus
scrapers only.

This does not expose the internal port outside the cluster (the default Service `type`
is `ClusterIP`), but with the default Service type any pod elsewhere in the cluster can
reach it, not just the registry server's own pod. If your cluster does not already treat
all in-cluster workloads as equally trusted, restrict the internal port to your metrics
scraper and probe traffic with a NetworkPolicy, e.g.:

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: toolhive-registry-server-internal
spec:
  podSelector:
    matchLabels:
      app.kubernetes.io/name: toolhive-registry-server
  policyTypes:
    - Ingress
  ingress:
    - from:
        - namespaceSelector:
            matchLabels:
              kubernetes.io/metadata.name: monitoring
      ports:
        - port: 8081
          protocol: TCP
```

**Non-default `service.type`**: the internal port has no per-port guard, so changing
`service.type` to anything that publishes the Service externally (e.g. `LoadBalancer`,
or `NodePort` reachable from outside the cluster) publishes the unauthenticated internal
port externally too — there is no Ingress in this chart to preempt it. Set
`service.exposeInternalPort: false` if you change `service.type` and don't want that;
Kubernetes probes are unaffected, since kubelet reaches the container's port directly
rather than through the Service.

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| affinity | object | `{}` | Affinity rules for pod scheduling |
| config.auth.mode | string | `"anonymous"` |  |
| config.database.database | string | `"toolhive_registry"` |  |
| config.database.host | string | `""` |  |
| config.database.port | int | `5432` |  |
| config.database.sslMode | string | `"require"` |  |
| config.database.user | string | `"thv_user"` |  |
| config.registries[0].name | string | `"default"` |  |
| config.registries[0].sources[0] | string | `"toolhive"` |  |
| config.sources[0].git.branch | string | `"main"` |  |
| config.sources[0].git.path | string | `"pkg/catalog/toolhive/data/registry-upstream.json"` |  |
| config.sources[0].git.repository | string | `"https://github.com/stacklok/toolhive-catalog.git"` |  |
| config.sources[0].name | string | `"toolhive"` |  |
| config.sources[0].syncPolicy.interval | string | `"30m"` |  |
| extraEnv | list | `[]` | Additional environment variables to add to the container Use this for secrets, feature flags, or runtime configuration |
| extraEnvFrom | list | `[]` | Additional environment variables from ConfigMap or Secret references |
| extraVolumeMounts | list | `[]` | Additional volume mounts to add to the container |
| extraVolumes | list | `[]` | Additional volumes to add to the pod |
| fullnameOverride | string | `""` | Override the full name of the chart |
| image.pullPolicy | string | `"IfNotPresent"` | Image pull policy |
| image.registryServerUrl | string | `"ghcr.io/stacklok/thv-registry-api:v1.5.0"` | URL of the registry server image |
| imagePullSecrets | list | `[]` | Image pull secrets for private registries |
| initContainers | list | `[]` | Init containers to run before the main container Use this for setup tasks like preparing pgpass files, waiting for dependencies, etc. Init containers share the same volumes as the main container (extraVolumes) |
| livenessProbe | object | `{"httpGet":{"path":"/health","port":"internal-http"},"initialDelaySeconds":30,"periodSeconds":10}` | Liveness probe configuration |
| nameOverride | string | `""` | Override the name of the chart |
| nodeSelector | object | `{}` | Node selector for pod scheduling |
| podAnnotations | object | `{}` | Annotations to add to the pod |
| podLabels | object | `{}` | Labels to add to the pod |
| podSecurityContext | object | `{}` | Pod security context |
| rbac | object | `{"allowedNamespaces":[],"scope":"cluster"}` | RBAC configuration for the registry server |
| rbac.allowedNamespaces | list | `[]` | List of namespaces that the registry server is allowed to watch. Only used if scope is set to "namespace". |
| rbac.scope | string | `"cluster"` | Scope of the RBAC configuration. - cluster: The registry server will have cluster-wide permissions via ClusterRole and ClusterRoleBinding. - namespace: The registry server will have permissions to watch resources in the namespaces specified in `allowedNamespaces`.   The registry server will have a ClusterRole and RoleBinding for each namespace in `allowedNamespaces`. |
| readinessProbe | object | `{"httpGet":{"path":"/readiness","port":"internal-http"},"initialDelaySeconds":5,"periodSeconds":5}` | Readiness probe configuration |
| replicaCount | int | `1` | Number of replicas |
| resources | object | `{"limits":{"cpu":"500m","memory":"512Mi"},"requests":{"cpu":"100m","memory":"128Mi"}}` | Resource requests and limits (matching operator defaults) |
| securityContext | object | `{"allowPrivilegeEscalation":false,"capabilities":{"drop":["ALL"]},"readOnlyRootFilesystem":true,"runAsNonRoot":true,"runAsUser":65535,"seccompProfile":{"type":"RuntimeDefault"}}` | Container security context |
| service.annotations | object | `{}` | Service annotations |
| service.exposeInternalPort | bool | `true` | Whether the Service publishes service.internalPort at all. The internal port carries no authentication, audit logging, or rate limiting by design (see "Internal Port Exposure" below); if service.type is set to anything other than the default ClusterIP, that Service publishes the internal port externally too, since there is no per-port guard. Set this to false to keep the internal port pod-local (reachable only via port-forward) if you change service.type and don't want that. |
| service.internalPort | int | `8081` | Internal service port (health checks, metrics). The container always listens on 8081 regardless of this value — this only changes the port the Service publishes that listener on, the same as service.port above. Only takes effect when service.exposeInternalPort is true. |
| service.port | int | `8080` | Service port |
| service.type | string | `"ClusterIP"` | Service type |
| serviceAccount.annotations | object | `{}` | Annotations to add to the service account |
| serviceAccount.create | bool | `true` | Specifies whether a service account should be created |
| serviceAccount.name | string | `"toolhive-registry-server"` | The name of the service account to use |
| tolerations | list | `[]` | Tolerations for pod scheduling |

