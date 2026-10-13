---
name: deploy-registry-server-with-otel
description: Deploy the ToolHive Registry Server to a Kind cluster with telemetry enabled. Use when you need to deploy the registry server for testing with OTEL metrics and tracing.
allowed-tools: Bash, Read
argument-hint: "[local|latest]"
---

# Deploy Registry Server with Telemetry

Deploy the ToolHive Registry Server to a Kind cluster with OpenTelemetry telemetry enabled.

## Arguments

- `local` - Build the image locally using ko and deploy (default)
- `latest` - Use an explicitly selected catalog-capable release image (no fallback)

## Prerequisites

Before running this skill, ensure:
- Kind cluster exists (run `/deploy-otel` first)
- `kconfig.yaml` file exists in the project root
- OTEL stack is deployed to the cluster

This skill deploys a minimal, single-user Postgres pod for the registry server's database —
it has no persistence or HA guarantees. Use `/deploy-registry-server-with-cnpg` instead if
you need a production-like database setup.

## Steps

### 1. Verify Prerequisites

```bash
echo "Checking prerequisites..."

# Check kconfig.yaml exists
if [ ! -f kconfig.yaml ]; then
  echo "ERROR: kconfig.yaml not found. Run /deploy-otel first to create the Kind cluster."
  exit 1
fi

# Check kind cluster is accessible
if ! kubectl cluster-info --kubeconfig kconfig.yaml >/dev/null 2>&1; then
  echo "ERROR: Cannot connect to Kind cluster. Run /deploy-otel first."
  exit 1
fi

# Check OTEL collector is deployed (the collector's DaemonSet name carries a
# chart-generated "-agent" suffix, so match by label instead of a fixed name)
if ! kubectl get daemonset -n monitoring -l app.kubernetes.io/name=opentelemetry-collector --kubeconfig kconfig.yaml 2>/dev/null | grep -q otel-collector; then
  echo "WARNING: OTEL Collector not found in monitoring namespace."
  echo "Telemetry data will not be collected. Run /deploy-otel first."
fi

echo "Prerequisites verified."
```

### 2. Deploy Postgres

The registry server requires a Postgres database. This step deploys the minimal
single-user Postgres pod defined in `examples/otel/postgres.yaml` into
`toolhive-system` if one isn't already running.

```bash
echo "Checking for existing Postgres deployment..."
kubectl create namespace toolhive-system --kubeconfig kconfig.yaml --dry-run=client -o yaml | kubectl apply -f - --kubeconfig kconfig.yaml

if kubectl get deployment postgres -n toolhive-system --kubeconfig kconfig.yaml >/dev/null 2>&1; then
  echo "Postgres already deployed."
else
  echo "Deploying Postgres..."
  kubectl apply -f examples/otel/postgres.yaml --kubeconfig kconfig.yaml

  echo "Waiting for Postgres to be ready..."
  kubectl rollout status deployment/postgres -n toolhive-system --kubeconfig kconfig.yaml --timeout=90s
fi
```

### 3. Deploy Registry Server

Based on the argument, either build locally or select a catalog-capable release
image explicitly. Before installing, prepare `CATALOG_VALUES` with your trusted
OAuth issuer/audience and identity mappings, author your own Cedar policy and
create an external ConfigMap with its `policies.cedar` key. Set `POLICY_CONFIGMAP`
to that name. Follow the [CNPG admission setup](../deploy-registry-server-with-cnpg/SKILL.md#6-deploy-registry-server)
for the values shape and explicit ConfigMap creation command; do not deploy CNPG
for this OTEL variant. No allow-all policy or anonymous default is supplied.

For an existing database, stop old readers/writers and complete
[offline inventory/acknowledgement/retirement](../../../docs/registry-exposure-migration.md)
using the final config and policy first; choose `CATALOG_INITIALIZATION=existing`.
Only for a genuinely new, entirely empty database choose
`CATALOG_INITIALIZATION=new-empty`. Reusing a Postgres deployment does not prove
its database is empty. Telemetry and database flags below are layered onto your
admission values, with admin disabled and anonymous catalog access disallowed.

Before running either deployment mode, **append/merge** these required database
entries for the OTEL deployment into the existing `extraEnv` list in your selected
`CATALOG_VALUES` file. Preserve all existing entries, including OAuth and proxy
secret references. If either name already exists, reconcile it explicitly rather
than adding a duplicate. This example shows only the entries to merge, not a
replacement values file:

```yaml
extraEnv:
  # Keep all existing entries here and append/merge these entries.
  - name: THV_REGISTRY_DATABASE_PASSWORD
    valueFrom:
      secretKeyRef:
        name: db-credentials
        key: password
  - name: THV_REGISTRY_DATABASE_MIGRATIONPASSWORD
    valueFrom:
      secretKeyRef:
        name: db-credentials
        key: migration-password
```

Keep the complete `extraEnv` list owned by that single values file. Helm replaces
lists rather than merging them; do not override `extraEnv` with another values
file or command-line flag.

```bash
set -euo pipefail
DEPLOY_MODE="${ARGUMENTS:-local}"
: "${CATALOG_VALUES:?Set the operator-prepared OAuth/admission values file}"
: "${POLICY_CONFIGMAP:?Create your external policy ConfigMap first}"
test -f "$CATALOG_VALUES" || exit 1
case "$POLICY_CONFIGMAP" in
  ''|*[!a-z0-9.-]*) echo "ERROR: Invalid ConfigMap name"; exit 1 ;;
esac
case "${CATALOG_INITIALIZATION:?Choose existing or new-empty explicitly}" in
  existing) INITIALIZE_EMPTY=false ;;
  new-empty) INITIALIZE_EMPTY=true ;;
  *) echo "ERROR: Choose existing or new-empty"; exit 1 ;;
esac
ADMISSION_FLAGS=(
  -f "$CATALOG_VALUES"
  --set-string "policy.existingConfigMap=$POLICY_CONFIGMAP"
  --set "initializeEmptyCatalog=$INITIALIZE_EMPTY"
  --set config.admin.enabled=false
  --set config.authorization.allowAnonymousCatalog=false
)

# Common telemetry configuration flags
TELEMETRY_FLAGS=(
  --set config.telemetry.enabled=true
  --set config.telemetry.serviceName=thv-registry-api
  --set config.telemetry.endpoint=otel-collector-opentelemetry-collector.monitoring.svc.cluster.local:4318
  --set config.telemetry.insecure=true
  --set config.telemetry.metrics.enabled=true
  --set config.telemetry.tracing.enabled=false
  --set config.telemetry.tracing.sampling=1.0
)

# Database configuration flags (points at the Postgres pod deployed in step 2)
DATABASE_FLAGS=(
  --set config.database.host=postgres.toolhive-system.svc.cluster.local
  --set config.database.user=thv_user
  --set config.database.database=toolhive_registry
  --set config.database.sslMode=disable
)

if [ "$DEPLOY_MODE" = "local" ] || [ -z "$DEPLOY_MODE" ]; then
  echo "Building registry server image locally with ko..."

  # Check ko is installed
  if ! command -v ko >/dev/null 2>&1; then
    echo "ERROR: ko is not installed. Install from https://ko.build/"
    exit 1
  fi

  # Build the image
  REGISTRY_SERVER_IMAGE=$(KO_DOCKER_REPO=kind.local ko build --local -B ./cmd/thv-registry-api | tail -n 1)
  case "$REGISTRY_SERVER_IMAGE" in
    ''|*[!a-zA-Z0-9./:@_-]*) echo "ERROR: Invalid image reference"; exit 1 ;;
  esac
  echo "Built image: $REGISTRY_SERVER_IMAGE"

  # Load into kind
  echo "Loading image into Kind cluster..."
  kind load docker-image "$REGISTRY_SERVER_IMAGE" --name thv-registry

  # Deploy with helm
  echo "Deploying registry server with telemetry enabled..."
  helm upgrade --install toolhive-registry-server deploy/charts/toolhive-registry-server \
    --namespace toolhive-system \
    --create-namespace \
    --kubeconfig kconfig.yaml \
    --set-string "image.registryServerUrl=$REGISTRY_SERVER_IMAGE" \
    --set image.pullPolicy=Never \
    "${ADMISSION_FLAGS[@]}" \
    "${TELEMETRY_FLAGS[@]}" \
    "${DATABASE_FLAGS[@]}"

elif [ "$DEPLOY_MODE" = "latest" ]; then
  : "${REGISTRY_SERVER_IMAGE:?Select a catalog-capable release image tag or digest}"
  case "$REGISTRY_SERVER_IMAGE" in
    ''|*[!a-zA-Z0-9./:@_-]*) echo "ERROR: Invalid image reference"; exit 1 ;;
  esac
  echo "Deploying selected catalog-capable release image..."
  helm upgrade --install toolhive-registry-server deploy/charts/toolhive-registry-server \
    --namespace toolhive-system \
    --create-namespace \
    --kubeconfig kconfig.yaml \
    --set-string "image.registryServerUrl=$REGISTRY_SERVER_IMAGE" \
    "${ADMISSION_FLAGS[@]}" \
    "${TELEMETRY_FLAGS[@]}" \
    "${DATABASE_FLAGS[@]}"
else
  echo "ERROR: Unknown deploy mode '$DEPLOY_MODE'. Use 'local' or 'latest'."
  exit 1
fi
```

### 4. Wait for Deployment

```bash
echo "Waiting for registry server to be ready..."
kubectl rollout status deployment/toolhive-registry-server -n toolhive-system --kubeconfig kconfig.yaml --timeout=2m
```

### 5. Verify Deployment

```bash
echo "Verifying deployment..."
kubectl get pods -n toolhive-system --kubeconfig kconfig.yaml
```

### 6. Show Telemetry Configuration

```bash
echo "Checking telemetry configuration..."
kubectl get configmap toolhive-registry-server-config -n toolhive-system -o jsonpath='{.data.config\.yaml}' --kubeconfig kconfig.yaml | grep -A 15 "telemetry:"
```

### 7. Provision the Grafana Dashboard

Grafana's sidecar auto-discovers dashboards from ConfigMaps labeled `grafana_dashboard=1`
in the `monitoring` namespace. Load the registry server dashboard this way:

```bash
echo "Provisioning the registry server Grafana dashboard..."
kubectl create configmap registry-server-dashboard \
  -n monitoring \
  --from-file=registry-server.json=examples/otel/grafana/dashboards/registry-server.json \
  --kubeconfig kconfig.yaml \
  --dry-run=client -o yaml | kubectl label -f - grafana_dashboard=1 --local -o yaml --kubeconfig kconfig.yaml | kubectl apply -f - --kubeconfig kconfig.yaml

echo "Dashboard provisioned. The Grafana sidecar picks it up within ~1 minute."
```

### 8. Display Access Instructions

```bash
cat <<'EOF'

=== Registry Server Deployment Complete ===

To access the registry server API:
  kubectl port-forward -n toolhive-system svc/toolhive-registry-server 8080:8080 --kubeconfig kconfig.yaml

Then test with your IdP token stored in a private header file:
  curl --fail-with-body --header "@${AUTH_HEADER_FILE:?Set your private header file path}" http://localhost:8080/registry/default/v0.1/servers

To check health and metrics (internal port, no auth):
  kubectl port-forward -n toolhive-system svc/toolhive-registry-server 8081:8081 --kubeconfig kconfig.yaml
  curl http://localhost:8081/health
  curl http://localhost:8081/metrics

View the dashboard in Grafana:
  kubectl port-forward -n monitoring svc/kube-prometheus-stack-grafana 3000:80 --kubeconfig kconfig.yaml
  Open http://localhost:3000/d/toolhive-registry/toolhive-registry-server (admin / admin)

EOF
```

## Telemetry Configuration

The skill dynamically configures telemetry via Helm `--set` flags:

| Setting | Value |
|---------|-------|
| `config.telemetry.enabled` | true |
| `config.telemetry.serviceName` | thv-registry-api |
| `config.telemetry.endpoint` | otel-collector-opentelemetry-collector.monitoring.svc.cluster.local:4318 |
| `config.telemetry.insecure` | true |
| `config.telemetry.metrics.enabled` | true |
| `config.telemetry.tracing.enabled` | false |
| `config.telemetry.tracing.sampling` | 1.0 |


## Cleanup

To uninstall the registry server and its Postgres pod:

```bash
helm uninstall toolhive-registry-server --namespace toolhive-system --kubeconfig kconfig.yaml
kubectl delete -f examples/otel/postgres.yaml --kubeconfig kconfig.yaml --ignore-not-found
kubectl delete configmap registry-server-dashboard -n monitoring --kubeconfig kconfig.yaml --ignore-not-found
```
