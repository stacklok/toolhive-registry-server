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
| affinity | object | `{}` | Node and pod affinity rules for scheduling Registry Server pods. |
| config | object | `{"auth":{"mode":"anonymous"},"database":{"database":"toolhive_registry","host":"","port":5432,"sslMode":"require","user":"thv_user"},"registries":[{"name":"default","sources":["toolhive"]}],"sources":[{"git":{"branch":"main","path":"pkg/catalog/toolhive/data/registry-upstream.json","repository":"https://github.com/stacklok/toolhive-catalog.git"},"name":"toolhive","syncPolicy":{"interval":"30m"}}]}` | Registry Server application configuration rendered as `config.yaml` in a Kubernetes ConfigMap. Accepts any valid Registry Server configuration field. See [Configuration](https://github.com/stacklok/toolhive-registry-server/blob/main/docs/configuration.md). Supply database passwords through Secret-backed environment variables in `extraEnv`, a PostgreSQL password file, or dynamic authentication. |
| extraEnv | list | `[]` | Additional environment variables for the Registry Server container. Use `valueFrom.secretKeyRef` to supply passwords from Kubernetes Secrets. |
| extraEnvFrom | list | `[]` | ConfigMap or Secret references whose keys become environment variables in the Registry Server container. |
| extraVolumeMounts | list | `[]` | Additional volume mounts for the Registry Server container, referencing volumes declared in `extraVolumes`. |
| extraVolumes | list | `[]` | Additional Kubernetes volumes available to the Registry Server pod. Mount them with `extraVolumeMounts` or an init container's `volumeMounts`. |
| fullnameOverride | string | `""` | Override the generated name of the Deployment, Service, and related resources. |
| image.pullPolicy | string | `"IfNotPresent"` | Image pull policy for the Registry Server container. |
| image.registryServerUrl | string | `"ghcr.io/stacklok/thv-registry-api:v1.5.2"` | Registry Server container image reference, including the tag or digest. |
| imagePullSecrets | list | `[]` | References to Secrets in the release namespace used to pull private container images. |
| initContainers | list | `[]` | Init container specifications run before the Registry Server container. Each init container defines its own volume mounts and can mount volumes declared in `extraVolumes`, for example to prepare a PostgreSQL password file. |
| livenessProbe | object | `{"httpGet":{"path":"/health","port":"internal-http"},"initialDelaySeconds":30,"periodSeconds":10}` | Kubernetes liveness probe for the Registry Server container. |
| nameOverride | string | `""` | Override the chart name used in resource labels and generated names. |
| nodeSelector | object | `{}` | Node labels that Registry Server pods require for scheduling. |
| podAnnotations | object | `{}` | Additional annotations on Registry Server pods. |
| podLabels | object | `{}` | Additional labels on Registry Server pods. |
| podSecurityContext | object | `{}` | Kubernetes security context applied to Registry Server pods. |
| rbac | object | `{"allowedNamespaces":[],"scope":"cluster"}` | Kubernetes role-based access control (RBAC) settings for watching ToolHive resources and Services. |
| rbac.allowedNamespaces | list | `[]` | Namespaces to watch when `rbac.scope` is `namespace`. This list must be nonempty for namespace scope and empty for cluster scope. |
| rbac.scope | string | `"cluster"` | Scope of permissions to watch ToolHive resources and Services. `cluster` creates a ClusterRoleBinding for all namespaces. `namespace` creates a RoleBinding in each namespace listed in `rbac.allowedNamespaces` and restricts the server's watches to those namespaces. |
| readinessProbe | object | `{"httpGet":{"path":"/readiness","port":"internal-http"},"initialDelaySeconds":5,"periodSeconds":5}` | Kubernetes readiness probe for the Registry Server container. |
| replicaCount | int | `1` | Number of Registry Server pod replicas. |
| resources | object | `{"limits":{"cpu":"500m","memory":"512Mi"},"requests":{"cpu":"100m","memory":"128Mi"}}` | CPU and memory requests and limits for the Registry Server container. |
| securityContext | object | `{"allowPrivilegeEscalation":false,"capabilities":{"drop":["ALL"]},"readOnlyRootFilesystem":true,"runAsNonRoot":true,"runAsUser":65535,"seccompProfile":{"type":"RuntimeDefault"}}` | Kubernetes security context applied to the Registry Server container. |
| service.annotations | object | `{}` | Annotations on the Registry Server Service. |
| service.exposeInternalPort | bool | `true` | Publish the internal listener through the Service. This listener serves health checks and metrics without authentication, audit logging, or rate limiting. Set to false to omit it from the Service; Kubernetes probes still reach the container directly. With an externally accessible Service type, enabling this setting also exposes the internal listener externally. |
| service.internalPort | int | `8081` | Service port forwarding to the internal listener on container port 8081. Applies when `service.exposeInternalPort` is true. Changing this value changes the Service port; the container port stays at 8081 with the default listener settings. |
| service.port | int | `8080` | Service port for the Registry Server API, forwarding to container port 8080. |
| service.type | string | `"ClusterIP"` | Kubernetes Service type. External Service types also expose the internal port when `service.exposeInternalPort` is true. |
| serviceAccount.annotations | object | `{}` | Annotations on the ServiceAccount created by this chart. |
| serviceAccount.create | bool | `true` | Create the ServiceAccount named by `serviceAccount.name`. Set to false to use an existing ServiceAccount. |
| serviceAccount.name | string | `"toolhive-registry-server"` | ServiceAccount name used by the pods and RBAC bindings. When `serviceAccount.create` is false, this account must already exist in the release namespace. |
| tolerations | list | `[]` | Tolerations that allow Registry Server pods to be scheduled on tainted nodes. |

