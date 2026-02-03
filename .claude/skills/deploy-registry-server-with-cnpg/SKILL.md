---
name: deploy-registry-server-with-cnpg
description: Deploy the ToolHive Registry Server to a Kind cluster with CloudNativePG (CNPG) PostgreSQL database. Use when you need a production-like PostgreSQL setup with the CNPG operator.
allowed-tools: Bash, Read
argument-hint: "[local|latest]"
---

# Deploy Registry Server with CloudNativePG

Deploy the ToolHive Registry Server to a Kind cluster with CloudNativePG (CNPG) as the PostgreSQL backend. CNPG provides a production-grade PostgreSQL operator with features like managed roles, automated backups, and high availability.

## Arguments

- `local` - Build the image locally using ko and deploy (default)
- `latest` - Use the latest published image from GitHub

## Prerequisites

Before running this skill, ensure:
- Kind cluster exists (run `task kind-setup` first to create it)
- `kconfig.yaml` file exists in the project root

## Steps

### 1. Verify Prerequisites

```bash
echo "Checking prerequisites..."

# Check required tools
command -v kind >/dev/null 2>&1 || { echo "ERROR: kind is not installed"; exit 1; }
command -v helm >/dev/null 2>&1 || { echo "ERROR: helm is not installed"; exit 1; }
command -v kubectl >/dev/null 2>&1 || { echo "ERROR: kubectl is not installed"; exit 1; }

# Check kconfig.yaml exists
if [ ! -f kconfig.yaml ]; then
  echo "ERROR: kconfig.yaml not found. Run `task kind-setup` first to create the Kind cluster."
  exit 1
fi

# Check kind cluster is accessible
if ! kubectl cluster-info --kubeconfig kconfig.yaml >/dev/null 2>&1; then
  echo "ERROR: Cannot connect to Kind cluster. Run `task kind-setup` first."
  exit 1
fi

echo "Prerequisites verified."
```

### 2. Create Namespace

```bash
echo "Creating toolhive-system namespace..."
kubectl create namespace toolhive-system --kubeconfig kconfig.yaml
```

### 3. Install CNPG Operator

```bash
echo "Installing CloudNativePG operator..."

# Check if CNPG operator is already installed
if kubectl get deployment -n cnpg-system cnpg-controller-manager --kubeconfig kconfig.yaml >/dev/null 2>&1; then
  echo "CNPG operator already installed."
else
  echo "Deploying CNPG operator..."
  kubectl apply --server-side -f https://raw.githubusercontent.com/cloudnative-pg/cloudnative-pg/release-1.25/releases/cnpg-1.25.1.yaml --kubeconfig kconfig.yaml

  echo "Waiting for CNPG operator to be ready..."
  kubectl wait --for=condition=available deployment/cnpg-controller-manager -n cnpg-system --kubeconfig kconfig.yaml --timeout=3m
fi

echo "CNPG operator is ready."
```

### 4. Deploy CNPG PostgreSQL Cluster

```bash
echo "Deploying CloudNativePG PostgreSQL cluster..."

# Deploy the CNPG cluster from postgres.yaml in the project root
kubectl apply -f examples/cnpg/postgres.yaml --kubeconfig kconfig.yaml

echo "Waiting for CNPG cluster to be ready..."
# Wait for the cluster to be ready (this waits for the primary pod to be running)
kubectl wait --for=condition=Ready cluster/registry-db -n toolhive-system --kubeconfig kconfig.yaml --timeout=5m

echo "CNPG PostgreSQL cluster is ready."
```

### 5. Deploy pgpass Secret

```bash
echo "Deploying pgpass secret for registry server..."
kubectl apply -f examples/cnpg/pgpass-secret.yaml --kubeconfig kconfig.yaml
```

### 6. Deploy Registry Server

Based on the argument, either build locally or use the latest image.

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

  # Deploy with helm using CNPG values file
  echo "Deploying registry server with CNPG PostgreSQL..."
  helm upgrade --install toolhive-registry-server deploy/charts/toolhive-registry-server \
    --namespace toolhive-system \
    --kubeconfig kconfig.yaml \
    -f examples/cnpg/helm-values.yaml \
    --set image.registryServerUrl="$REGISTRY_SERVER_IMAGE" \
    --set image.pullPolicy=Never

elif [ "$DEPLOY_MODE" = "latest" ]; then
  echo "Deploying latest registry server image from GitHub..."
  helm upgrade --install toolhive-registry-server deploy/charts/toolhive-registry-server \
    --namespace toolhive-system \
    --kubeconfig kconfig.yaml \
    -f examples/cnpg/helm-values.yaml
else
  echo "ERROR: Unknown deploy mode '$DEPLOY_MODE'. Use 'local' or 'latest'."
  exit 1
fi
```

### 7. Wait for Deployment

```bash
echo "Waiting for registry server to be ready..."
kubectl rollout status deployment/toolhive-registry-server -n toolhive-system --kubeconfig kconfig.yaml --timeout=3m
```

### 8. Verify Deployment

```bash
echo "Verifying deployment..."
echo ""
echo "=== Pods ==="
kubectl get pods -n toolhive-system --kubeconfig kconfig.yaml
echo ""
echo "=== CNPG Cluster Status ==="
kubectl get cluster registry-db -n toolhive-system --kubeconfig kconfig.yaml
```

### 9. Show Configuration

```bash
echo ""
echo "=== Database Configuration ==="
kubectl get configmap toolhive-registry-server-config -n toolhive-system -o jsonpath='{.data.config\.yaml}' --kubeconfig kconfig.yaml | grep -A 10 "database:"
```

### 10. Display Access Instructions

```bash
cat <<'EOF'

=== Registry Server with CNPG Deployment Complete ===

Components deployed:
  - CloudNativePG Operator (cnpg-system namespace)
  - CNPG PostgreSQL Cluster (registry-db)
  - Registry Server

CNPG Services created:
  - registry-db-rw: Read-write service (used by registry server)
  - registry-db-r: Read-only service
  - registry-db-ro: Read-only replica service

To access the registry server API:
  kubectl port-forward -n toolhive-system svc/toolhive-registry-server 8080:8080 --kubeconfig kconfig.yaml

Then test with:
  curl http://localhost:8080/health
  curl http://localhost:8080/registry/toolhive/v0.1/servers?limit=3

To connect to PostgreSQL directly:
  kubectl port-forward -n toolhive-system svc/registry-db-rw 5432:5432 --kubeconfig kconfig.yaml
  # Then: psql -h localhost -U db_app -d registry

To view CNPG cluster status:
  kubectl get cluster registry-db -n toolhive-system --kubeconfig kconfig.yaml -o yaml

EOF
```

## Architecture

This deployment includes:

| Component | Description |
|-----------|-------------|
| CNPG Operator | CloudNativePG operator managing PostgreSQL clusters |
| registry-db | CNPG PostgreSQL cluster with managed roles |
| Registry Server | API server connected to CNPG PostgreSQL |

## CNPG Features

CloudNativePG provides:

| Feature | Description |
|---------|-------------|
| Managed Roles | Automatic user/password management from Secrets |
| SCRAM-SHA-256 | Secure password authentication |
| Declarative Config | PostgreSQL config managed via Kubernetes CRDs |
| Connection Pooling | Built-in PgBouncer support (optional) |
| Backup/Restore | Native backup to object storage (optional) |

## Database Users

| User | Purpose | Secret |
|------|---------|--------|
| db_app | Application user (limited privileges) | registry-db-app-credentials |
| db_migrator | Migration user (elevated privileges) | registry-db-migrator-credentials |

## Cleanup

To uninstall everything:

```bash
# Remove registry server
helm uninstall toolhive-registry-server --namespace toolhive-system --kubeconfig kconfig.yaml

# Remove CNPG cluster
kubectl delete -f examples/cnpg/postgres.yaml --kubeconfig kconfig.yaml

# Remove pgpass secret
kubectl delete -f examples/cnpg/pgpass-secret.yaml --kubeconfig kconfig.yaml

# Optionally remove CNPG operator
kubectl delete -f https://raw.githubusercontent.com/cloudnative-pg/cloudnative-pg/release-1.25/releases/cnpg-1.25.1.yaml --kubeconfig kconfig.yaml
```

Or to remove everything including the Kind cluster:

```bash
task kind-destroy
```
