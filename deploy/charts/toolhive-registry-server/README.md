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

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| affinity | object | `{}` | Affinity rules for pod scheduling |
| config.auth.mode | string | `"anonymous"` |  |
| config.fileStorage.baseDir | string | `"/data"` |  |
| config.registries[0].format | string | `"toolhive"` |  |
| config.registries[0].git.branch | string | `"main"` |  |
| config.registries[0].git.path | string | `"pkg/registry/data/registry.json"` |  |
| config.registries[0].git.repository | string | `"https://github.com/stacklok/toolhive.git"` |  |
| config.registries[0].name | string | `"toolhive"` |  |
| config.registries[0].syncPolicy.interval | string | `"30m"` |  |
| config.registryName | string | `"default"` |  |
| config.telemetry.enabled | bool | `true` |  |
| config.telemetry.endpoint | string | `"otel-collector-opentelemetry-collector.monitoring.svc.cluster.local:4318"` |  |
| config.telemetry.insecure | bool | `true` |  |
| config.telemetry.metrics.enabled | bool | `true` |  |
| config.telemetry.serviceName | string | `"thv-registry-api"` |  |
| config.telemetry.serviceVersion | string | `""` |  |
| config.telemetry.tracing.enabled | bool | `false` |  |
| extraEnv | list | `[]` | Additional environment variables to add to the container Use this for secrets, feature flags, or runtime configuration |
| extraEnvFrom | list | `[]` | Additional environment variables from ConfigMap or Secret references |
| extraVolumeMounts | list | `[]` | Additional volume mounts to add to the container |
| extraVolumes | list | `[]` | Additional volumes to add to the pod |
| fullnameOverride | string | `""` | Override the full name of the chart |
| image.pullPolicy | string | `"IfNotPresent"` | Image pull policy |
| image.registryServerUrl | string | `"ghcr.io/stacklok/thv-registry-api:v0.4.9"` | URL of the registry server image |
| imagePullSecrets | list | `[]` | Image pull secrets for private registries |
| initContainers | list | `[]` | Init containers to run before the main container Use this for setup tasks like preparing pgpass files, waiting for dependencies, etc. Init containers share the same volumes as the main container (extraVolumes) |
| livenessProbe | object | `{"httpGet":{"path":"/health","port":"http"},"initialDelaySeconds":30,"periodSeconds":10}` | Liveness probe configuration |
| nameOverride | string | `""` | Override the name of the chart |
| nodeSelector | object | `{}` | Node selector for pod scheduling |
| podAnnotations | object | `{}` | Annotations to add to the pod |
| podLabels | object | `{}` | Labels to add to the pod |
| podSecurityContext | object | `{}` | Pod security context |
| readinessProbe | object | `{"httpGet":{"path":"/readiness","port":"http"},"initialDelaySeconds":5,"periodSeconds":5}` | Readiness probe configuration |
| replicaCount | int | `1` | Number of replicas |
| resources | object | `{"limits":{"cpu":"500m","memory":"512Mi"},"requests":{"cpu":"100m","memory":"128Mi"}}` | Resource requests and limits (matching operator defaults) |
| securityContext | object | `{"allowPrivilegeEscalation":false,"capabilities":{"drop":["ALL"]},"readOnlyRootFilesystem":true,"runAsNonRoot":true,"runAsUser":65535,"seccompProfile":{"type":"RuntimeDefault"}}` | Container security context |
| service.annotations | object | `{}` | Service annotations |
| service.port | int | `8080` | Service port |
| service.type | string | `"ClusterIP"` | Service type |
| serviceAccount.annotations | object | `{}` | Annotations to add to the service account |
| serviceAccount.create | bool | `true` | Specifies whether a service account should be created |
| serviceAccount.name | string | `""` | The name of the service account to use. If not set and create is true, a name is generated using the fullname template |
| tolerations | list | `[]` | Tolerations for pod scheduling |

