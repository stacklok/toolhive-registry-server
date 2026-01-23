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
- `latest` - Use the latest published image from GitHub

## Prerequisites

Before running this skill, ensure:
- Kind cluster exists (run `/deploy-otel` first)
- `kconfig.yaml` file exists in the project root
- OTEL stack is deployed to the cluster

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

# Check OTEL collector is deployed
if ! kubectl get daemonset -n monitoring otel-collector-opentelemetry-collector --kubeconfig kconfig.yaml >/dev/null 2>&1; then
  echo "WARNING: OTEL Collector not found in monitoring namespace."
  echo "Telemetry data will not be collected. Run /deploy-otel first."
fi

echo "Prerequisites verified."
```

### 2. Deploy Registry Server

Based on the argument, either build locally or use the latest image. Telemetry is configured dynamically via Helm `--set` flags.

```bash
DEPLOY_MODE="${ARGUMENTS:-local}"

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

if [ "$DEPLOY_MODE" = "local" ] || [ -z "$DEPLOY_MODE" ]; then
  echo "Building registry server image locally with ko..."

  # Check ko is installed
  if ! command -v ko >/dev/null 2>&1; then
    echo "ERROR: ko is not installed. Install from https://ko.build/"
    exit 1
  fi

  # Build the image
  REGISTRY_SERVER_IMAGE=$(KO_DOCKER_REPO=kind.local ko build --local -B ./cmd/thv-registry-api | tail -n 1)
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
    --set image.registryServerUrl="$REGISTRY_SERVER_IMAGE" \
    --set image.pullPolicy=Never \
    "${TELEMETRY_FLAGS[@]}"

elif [ "$DEPLOY_MODE" = "latest" ]; then
  echo "Deploying latest registry server image from GitHub..."
  helm upgrade --install toolhive-registry-server deploy/charts/toolhive-registry-server \
    --namespace toolhive-system \
    --create-namespace \
    --kubeconfig kconfig.yaml \
    "${TELEMETRY_FLAGS[@]}"
else
  echo "ERROR: Unknown deploy mode '$DEPLOY_MODE'. Use 'local' or 'latest'."
  exit 1
fi
```

### 3. Wait for Deployment

```bash
echo "Waiting for registry server to be ready..."
kubectl rollout status deployment/toolhive-registry-server -n toolhive-system --kubeconfig kconfig.yaml --timeout=2m
```

### 4. Verify Deployment

```bash
echo "Verifying deployment..."
kubectl get pods -n toolhive-system --kubeconfig kconfig.yaml
```

### 5. Show Telemetry Configuration

```bash
echo "Checking telemetry configuration..."
kubectl get configmap toolhive-registry-server-config -n toolhive-system -o jsonpath='{.data.config\.yaml}' --kubeconfig kconfig.yaml | grep -A 15 "telemetry:"
```

### 6. Display Access Instructions

```bash
cat <<'EOF'

=== Registry Server Deployment Complete ===

To access the registry server API:
  kubectl port-forward -n toolhive-system svc/toolhive-registry-server 8080:8080 --kubeconfig kconfig.yaml

Then test with:
  curl http://localhost:8080/health
  curl http://localhost:8080/registry/v0.1/servers

View metrics in Grafana:
  kubectl port-forward -n monitoring svc/kube-prometheus-stack-grafana 3000:3000 --kubeconfig kconfig.yaml
  Open http://localhost:3000 (admin / admin)

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

## Available Metrics

Once deployed, these metrics are exported to the OTEL collector:

- `http_server_request_duration_seconds` - HTTP request latency
- `http_server_requests_total` - Total HTTP requests
- `http_server_active_requests` - Currently in-flight requests
- `registry_servers_total` - Number of servers per registry
- `sync_operation_duration_seconds` - Sync operation duration

## Cleanup

To uninstall the registry server:

```bash
helm uninstall toolhive-registry-server --namespace toolhive-system --kubeconfig kconfig.yaml
```
