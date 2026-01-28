---
name: deploy-registry-server-with-otel
description: Deploy the ToolHive Registry Server to a Kind cluster with telemetry enabled. Use when you need to deploy the registry server for testing with OTEL metrics and tracing.
allowed-tools: Bash, Read
argument-hint: "[local|latest]"
---

# Deploy Registry Server with Telemetry

Deploy the ToolHive Registry Server to a Kind cluster with OpenTelemetry telemetry enabled and PostgreSQL backend for full trace visibility.

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
if ! kubectl get daemonset -n monitoring otel-collector-opentelemetry-collector-agent --kubeconfig kconfig.yaml >/dev/null 2>&1; then
  echo "WARNING: OTEL Collector not found in monitoring namespace."
  echo "Telemetry data will not be collected. Run /deploy-otel first."
fi

echo "Prerequisites verified."
```

### 2. Create Namespace

```bash
echo "Creating toolhive-system namespace..."
kubectl create namespace toolhive-system --kubeconfig kconfig.yaml --dry-run=client -o yaml | kubectl apply -f - --kubeconfig kconfig.yaml
```

### 3. Deploy PostgreSQL

```bash
echo "Deploying PostgreSQL database..."
kubectl apply -f examples/otel/postgres-manifests.yaml --kubeconfig kconfig.yaml

echo "Waiting for PostgreSQL to be ready..."
kubectl wait --for=condition=available deployment/postgres -n toolhive-system --kubeconfig kconfig.yaml --timeout=2m

echo "PostgreSQL deployed successfully."
```

### 4. Deploy Registry Server

Based on the argument, either build locally or use the latest image. Uses PostgreSQL values file for full tracing support.

```bash
DEPLOY_MODE="${ARGUMENTS:-local}"

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

  # Deploy with helm using PostgreSQL values file
  echo "Deploying registry server with PostgreSQL and telemetry enabled..."
  helm upgrade --install toolhive-registry-server deploy/charts/toolhive-registry-server \
    --namespace toolhive-system \
    --kubeconfig kconfig.yaml \
    -f examples/otel/registry-server-postgres-values.yaml \
    --set image.registryServerUrl="$REGISTRY_SERVER_IMAGE" \
    --set image.pullPolicy=Never

elif [ "$DEPLOY_MODE" = "latest" ]; then
  echo "Deploying latest registry server image from GitHub..."
  helm upgrade --install toolhive-registry-server deploy/charts/toolhive-registry-server \
    --namespace toolhive-system \
    --kubeconfig kconfig.yaml \
    -f examples/otel/registry-server-postgres-values.yaml
else
  echo "ERROR: Unknown deploy mode '$DEPLOY_MODE'. Use 'local' or 'latest'."
  exit 1
fi
```

### 5. Wait for Deployment

```bash
echo "Waiting for registry server to be ready..."
kubectl rollout status deployment/toolhive-registry-server -n toolhive-system --kubeconfig kconfig.yaml --timeout=3m
```

### 6. Verify Deployment

```bash
echo "Verifying deployment..."
kubectl get pods -n toolhive-system --kubeconfig kconfig.yaml
```

### 7. Show Configuration

```bash
echo "Checking database and telemetry configuration..."
kubectl get configmap toolhive-registry-server-config -n toolhive-system -o jsonpath='{.data.config\.yaml}' --kubeconfig kconfig.yaml | grep -A 20 "database:\|telemetry:"
```

### 8. Display Access Instructions

```bash
cat <<'EOF'

=== Registry Server Deployment Complete ===

Components deployed:
  - PostgreSQL database (for full trace visibility)
  - Registry Server with telemetry enabled

To access the registry server API:
  kubectl port-forward -n toolhive-system svc/toolhive-registry-server 8080:8080 --kubeconfig kconfig.yaml

Then test with:
  curl http://localhost:8080/health
  curl http://localhost:8080/registry/toolhive/v0.1/servers?limit=3

View traces in Grafana:
  kubectl port-forward -n monitoring svc/kube-prometheus-stack-grafana 3000:3000 --kubeconfig kconfig.yaml
  Open http://localhost:3000 (admin / admin)
  Go to Explore -> Select Tempo -> Search for service "thv-registry-api"

With PostgreSQL backend, you should now see:
  - HTTP request spans (top level)
  - Database service spans (dbService.ListServers, etc.)
  - Full trace context through the application

EOF
```

## Architecture

This deployment includes:

| Component | Description |
|-----------|-------------|
| PostgreSQL | Database backend with tracing instrumentation |
| Registry Server | API server with OTEL telemetry enabled |
| OTEL Collector | Receives traces and exports to Tempo |
| Tempo | Distributed tracing backend |
| Grafana | Visualization (Tempo datasource) |

## Telemetry Configuration

The PostgreSQL values file configures:

| Setting | Value |
|---------|-------|
| `config.telemetry.enabled` | true |
| `config.telemetry.serviceName` | thv-registry-api |
| `config.telemetry.endpoint` | otel-collector-opentelemetry-collector.monitoring.svc.cluster.local:4318 |
| `config.telemetry.insecure` | true |
| `config.telemetry.metrics.enabled` | true |
| `config.telemetry.tracing.enabled` | true |
| `config.telemetry.tracing.sampling` | 1.0 |
| `config.database.host` | postgres.toolhive-system |

## Cleanup

To uninstall everything:

```bash
helm uninstall toolhive-registry-server --namespace toolhive-system --kubeconfig kconfig.yaml
kubectl delete -f examples/otel/postgres-manifests.yaml --kubeconfig kconfig.yaml
```
