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
- `latest` - Use an explicitly selected catalog-capable release image (no fallback)

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

Before either install mode, prepare an operator-owned admission values file outside
this repository and set `CATALOG_VALUES` to its path. Configure your actual OAuth
issuer/audience, resource URL and identity mappings as described in
[the Cedar guide](../../../docs/registry-cedar-authorization.md). For example,
customize these values (the example domains are not a working IdP):

```yaml
config:
  auth:
    mode: oauth
    oauth:
      resourceUrl: https://registry.example.com
      providers:
        - name: corporate
          issuerUrl: https://id.example.com
          audience: registry-api
  authorization:
    allowAnonymousCatalog: false
    identityMappings:
      - issuer: https://id.example.com
        groups: {claim: groups, format: string-array}
```

Optionally author and review a custom plain Cedar policy; set `POLICY_FILE` and
`POLICY_CONFIGMAP` only when replacing the built-in policy. With no custom file,
standalone evaluates built-in allow-all Cedar after verified OAuth: every verified
identity can use every enabled operation, including admin if explicitly enabled.
Admin stays disabled by default; anonymous catalog access requires explicit opt-in.
An invalid specified file fails startup, never falls back. A deny-all custom policy
is valid and keeps the API denied while operational readiness still works.

Only for a custom policy, create its external ConfigMap before Helm:

```bash
: "${POLICY_FILE:?Set the path to your reviewed Cedar policy}"
: "${POLICY_CONFIGMAP:?Choose the external policy ConfigMap name}"
test -f "$POLICY_FILE" || exit 1
kubectl create configmap "$POLICY_CONFIGMAP" --namespace toolhive-system \
  --from-file="policies.cedar=$POLICY_FILE" --kubeconfig kconfig.yaml
```

For an existing database, stop all old readers/writers and complete the manual
inventory, acknowledgement and (where required) claims-retirement receipt procedure
in [the upgrade guide](../../../docs/registry-exposure-migration.md) using the exact
final config and policy before serving. Set `CATALOG_INITIALIZATION=existing`.
Do not use fresh initialization to bypass existing-data review, even when old claims
are NULL. Only for a genuinely new, entirely empty database, explicitly choose
`CATALOG_INITIALIZATION=new-empty`. There is no initialization default.

For `latest`, set `REGISTRY_SERVER_IMAGE` to a release tag/digest verified to contain
catalog startup and receipt enforcement. Old pre-cutover images are not compatible.
`local` builds that image from the current source tree. Both modes pass the image,
optional custom policy and chosen initialization mode explicitly:

```bash
set -euo pipefail
DEPLOY_MODE="${ARGUMENTS:-local}"
: "${CATALOG_VALUES:?Set the operator-prepared OAuth/admission values file}"
test -f "$CATALOG_VALUES" || exit 1
case "${CATALOG_INITIALIZATION:?Choose existing or new-empty explicitly}" in
  existing) INITIALIZE_EMPTY=false ;;
  new-empty) INITIALIZE_EMPTY=true ;;
  *) echo "ERROR: Choose existing or new-empty"; exit 1 ;;
esac

if [ "$DEPLOY_MODE" = "local" ]; then
  command -v ko >/dev/null 2>&1 || { echo "ERROR: ko is not installed"; exit 1; }
  REGISTRY_SERVER_IMAGE=$(KO_DOCKER_REPO=kind.local ko build --local -B ./cmd/thv-registry-api | tail -n 1)
  PULL_POLICY=Never
elif [ "$DEPLOY_MODE" = "latest" ]; then
  : "${REGISTRY_SERVER_IMAGE:?Select a catalog-capable release image tag or digest}"
  PULL_POLICY=IfNotPresent
else
  echo "ERROR: Unknown deploy mode '$DEPLOY_MODE'. Use 'local' or 'latest'."
  exit 1
fi
case "$REGISTRY_SERVER_IMAGE" in
  ''|*[!a-zA-Z0-9./:@_-]*) echo "ERROR: Invalid image reference"; exit 1 ;;
esac
POLICY_ARGS=()
if [ -n "${POLICY_CONFIGMAP:-}" ]; then
  case "$POLICY_CONFIGMAP" in
    *[!a-z0-9.-]*) echo "ERROR: Invalid ConfigMap name"; exit 1 ;;
  esac
  POLICY_ARGS=(--set-string "policy.existingConfigMap=$POLICY_CONFIGMAP")
fi
if [ "$DEPLOY_MODE" = "local" ]; then
  kind load docker-image "$REGISTRY_SERVER_IMAGE" --name thv-registry
fi
helm upgrade --install toolhive-registry-server deploy/charts/toolhive-registry-server \
  --namespace toolhive-system --kubeconfig kconfig.yaml \
  -f examples/cnpg/helm-values.yaml -f "$CATALOG_VALUES" \
  --set-string "image.registryServerUrl=$REGISTRY_SERVER_IMAGE" \
  --set-string "image.pullPolicy=$PULL_POLICY" \
  "${POLICY_ARGS[@]}" \
  --set "initializeEmptyCatalog=$INITIALIZE_EMPTY"
```

Changing the external policy ConfigMap requires an explicit rollout of every
replica; the running evaluator uses frozen policy bytes. Drain old replicas for
revocations. Never reset a consumed receipt or reuse a fresh flag for an upgrade.

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

Then test with your IdP token stored in a private header file (no token in history):
  curl --fail-with-body --header "@${AUTH_HEADER_FILE:?Set your private header file path}" http://localhost:8080/registry/default/v0.1/servers?limit=3

For unauthenticated health checks on the internal port:
  kubectl port-forward -n toolhive-system svc/toolhive-registry-server 8081:8081 --kubeconfig kconfig.yaml
  curl http://localhost:8081/health

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
