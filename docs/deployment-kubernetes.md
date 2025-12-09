# Kubernetes Deployment

This guide covers deploying the ToolHive Registry API server to Kubernetes.

## Table of Contents

- [Overview](#overview)
- [Prerequisites](#prerequisites)
- [Basic Deployment](#basic-deployment)
- [Database Configuration](#database-configuration)
- [Authentication Setup](#authentication-setup)
- [Production Considerations](#production-considerations)
- [Complete Examples](#complete-examples)

## Overview

The Registry API is designed to run as a sidecar container alongside the ToolHive Operator's MCPRegistry controller, but it can also run standalone.

### Key Features

- Automatic database migrations on startup
- Health and readiness probes
- Graceful shutdown (30-second timeout)
- ConfigMap-based configuration
- Secret-based credential management

## Prerequisites

- Kubernetes cluster (1.24+)
- `kubectl` configured to access your cluster
- PostgreSQL database (if using database backend)
- Container image: `ghcr.io/stacklok/toolhive/thv-registry-api:latest`

## Basic Deployment

### Minimal Example (File-based Registry)

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: registry-api-config
  namespace: toolhive
data:
  config.yaml: |
    registryName: my-registry
    registries:
      - name: toolhive
        format: toolhive
        git:
          repository: https://github.com/stacklok/toolhive.git
          branch: main
          path: pkg/registry/data/registry.json
        syncPolicy:
          interval: "15m"
    auth:
      mode: anonymous  # For development only!
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: registry-api
  namespace: toolhive
spec:
  replicas: 1
  selector:
    matchLabels:
      app: registry-api
  template:
    metadata:
      labels:
        app: registry-api
    spec:
      containers:
      - name: registry-api
        image: ghcr.io/stacklok/toolhive/thv-registry-api:latest
        args:
        - serve
        - --config=/etc/registry/config.yaml
        ports:
        - name: http
          containerPort: 8080
          protocol: TCP
        livenessProbe:
          httpGet:
            path: /health
            port: http
          initialDelaySeconds: 10
          periodSeconds: 10
        readinessProbe:
          httpGet:
            path: /readiness
            port: http
          initialDelaySeconds: 5
          periodSeconds: 5
        volumeMounts:
        - name: config
          mountPath: /etc/registry
          readOnly: true
        resources:
          requests:
            cpu: 100m
            memory: 128Mi
          limits:
            cpu: 500m
            memory: 512Mi
      volumes:
      - name: config
        configMap:
          name: registry-api-config
---
apiVersion: v1
kind: Service
metadata:
  name: registry-api
  namespace: toolhive
spec:
  selector:
    app: registry-api
  ports:
  - name: http
    port: 80
    targetPort: http
    protocol: TCP
  type: ClusterIP
```

Apply the manifests:

```bash
kubectl create namespace toolhive
kubectl apply -f deployment.yaml
```

Verify the deployment:

```bash
kubectl -n toolhive get pods
kubectl -n toolhive logs -l app=registry-api
```

## Database Configuration

### Database Secret (pgpass file)

The Registry Server uses PostgreSQL's pgpass file for database authentication. Create a secret containing the pgpass file:

**Create pgpass file:**

```bash
# Create pgpass file with credentials for both users
cat > pgpass <<EOF
postgres-service.database.svc.cluster.local:5432:toolhive_registry:db_app:your-secure-app-password
postgres-service.database.svc.cluster.local:5432:toolhive_registry:db_migrator:your-secure-migration-password
EOF

chmod 600 pgpass
```

**Create Kubernetes secret:**

```bash
kubectl -n toolhive create secret generic registry-db-pgpass \
  --from-file=pgpass=pgpass

# Clean up local file
rm pgpass
```

Or using YAML:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: registry-db-pgpass
  namespace: toolhive
type: Opaque
data:
  pgpass: <base64-encoded-pgpass-content>
```

### ConfigMap with Database

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: registry-api-config
  namespace: toolhive
data:
  config.yaml: |
    registryName: my-registry
    registries:
      - name: toolhive
        format: toolhive
        git:
          repository: https://github.com/stacklok/toolhive.git
          branch: main
          path: pkg/registry/data/registry.json
        syncPolicy:
          interval: "15m"
    auth:
      mode: oauth
      oauth:
        resourceUrl: https://registry.example.com
        providers:
          - name: kubernetes
            issuerUrl: https://kubernetes.default.svc
            audience: https://kubernetes.default.svc
            caCertPath: /var/run/secrets/kubernetes.io/serviceaccount/ca.crt
    database:
      host: postgres.database.svc.cluster.local
      port: 5432
      user: db_app
      migrationUser: db_migrator
      database: toolhive_registry
      sslMode: require
      maxOpenConns: 25
      maxIdleConns: 5
      connMaxLifetime: "5m"
```

**Note**: Database passwords are provided via the pgpass file mounted from the secret, not in the config.

### Deployment with Database

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: registry-api
  namespace: toolhive
spec:
  replicas: 2  # Can scale horizontally with database backend
  selector:
    matchLabels:
      app: registry-api
  template:
    metadata:
      labels:
        app: registry-api
    spec:
      serviceAccountName: registry-api
      containers:
      - name: registry-api
        image: ghcr.io/stacklok/toolhive/thv-registry-api:latest
        args:
        - serve
        - --config=/etc/registry/config.yaml
        ports:
        - name: http
          containerPort: 8080
          protocol: TCP
        env:
        - name: PGPASSFILE
          value: /secrets/pgpass
        livenessProbe:
          httpGet:
            path: /health
            port: http
          initialDelaySeconds: 10
          periodSeconds: 10
          timeoutSeconds: 5
          failureThreshold: 3
        readinessProbe:
          httpGet:
            path: /readiness
            port: http
          initialDelaySeconds: 5
          periodSeconds: 5
          timeoutSeconds: 3
          failureThreshold: 3
        volumeMounts:
        - name: config
          mountPath: /etc/registry
          readOnly: true
        - name: pgpass
          mountPath: /secrets
          readOnly: true
        resources:
          requests:
            cpu: 100m
            memory: 256Mi
          limits:
            cpu: 1000m
            memory: 1Gi
      volumes:
      - name: config
        configMap:
          name: registry-api-config
      - name: pgpass
        secret:
          secretName: registry-db-pgpass
          defaultMode: 0600
```

### Optional: Pre-Migration Job

Run migrations as a separate Job before deployment:

```yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: registry-migrate
  namespace: toolhive
spec:
  backoffLimit: 3
  template:
    spec:
      restartPolicy: OnFailure
      containers:
      - name: migrate
        image: ghcr.io/stacklok/toolhive/thv-registry-api:latest
        args:
        - migrate
        - up
        - --config=/etc/registry/config.yaml
        - --yes
        env:
        - name: PGPASSFILE
          value: /secrets/pgpass
        volumeMounts:
        - name: config
          mountPath: /etc/registry
          readOnly: true
        - name: pgpass
          mountPath: /secrets
          readOnly: true
      volumes:
      - name: config
        configMap:
          name: registry-api-config
      - name: pgpass
        secret:
          secretName: registry-db-pgpass
          defaultMode: 0600
```

## Authentication Setup

### Service Account

Create a service account for the registry API:

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: registry-api
  namespace: toolhive
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: registry-api
  namespace: toolhive
rules:
- apiGroups: [""]
  resources: ["configmaps", "secrets"]
  verbs: ["get", "list"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: registry-api
  namespace: toolhive
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: registry-api
subjects:
- kind: ServiceAccount
  name: registry-api
  namespace: toolhive
```

### OAuth with Kubernetes Service Accounts

Configuration for accepting both external OAuth and Kubernetes service account tokens:

```yaml
auth:
  mode: oauth
  oauth:
    resourceUrl: https://registry.example.com
    providers:
      # External OAuth provider
      - name: company-sso
        issuerUrl: https://auth.company.com
        audience: api://toolhive-registry

      # Kubernetes service accounts
      - name: kubernetes
        issuerUrl: https://kubernetes.default.svc
        audience: https://kubernetes.default.svc
        caCertPath: /var/run/secrets/kubernetes.io/serviceaccount/ca.crt
```

## Production Considerations

### High Availability

```yaml
spec:
  replicas: 3  # Multiple replicas for HA
  strategy:
    type: RollingUpdate
    rollingUpdate:
      maxSurge: 1
      maxUnavailable: 0
```

### Pod Disruption Budget

```yaml
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: registry-api-pdb
  namespace: toolhive
spec:
  minAvailable: 1
  selector:
    matchLabels:
      app: registry-api
```

### Horizontal Pod Autoscaler

```yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: registry-api-hpa
  namespace: toolhive
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: registry-api
  minReplicas: 2
  maxReplicas: 10
  metrics:
  - type: Resource
    resource:
      name: cpu
      target:
        type: Utilization
        averageUtilization: 70
  - type: Resource
    resource:
      name: memory
      target:
        type: Utilization
        averageUtilization: 80
```

### Network Policy

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: registry-api-netpol
  namespace: toolhive
spec:
  podSelector:
    matchLabels:
      app: registry-api
  policyTypes:
  - Ingress
  - Egress
  ingress:
  - from:
    - namespaceSelector:
        matchLabels:
          name: toolhive
    ports:
    - protocol: TCP
      port: 8080
  egress:
  # Allow DNS
  - to:
    - namespaceSelector:
        matchLabels:
          name: kube-system
    ports:
    - protocol: UDP
      port: 53
  # Allow database
  - to:
    - namespaceSelector:
        matchLabels:
          name: database
    ports:
    - protocol: TCP
      port: 5432
  # Allow HTTPS (for Git sync, OAuth, etc.)
  - to:
    - namespaceSelector: {}
    ports:
    - protocol: TCP
      port: 443
```

### Resource Limits

Recommended resource requests and limits:

```yaml
resources:
  requests:
    cpu: 100m      # Minimum required
    memory: 256Mi  # With database backend
  limits:
    cpu: 1000m     # Burst capacity
    memory: 1Gi    # Maximum memory
```

Adjust based on:
- Number of registries configured
- Sync frequency
- Database connection pool size
- Expected request rate

### Monitoring

Add Prometheus annotations:

```yaml
template:
  metadata:
    annotations:
      prometheus.io/scrape: "true"
      prometheus.io/port: "8080"
      prometheus.io/path: "/metrics"  # If metrics endpoint is added
```

## Complete Examples

### Production-Ready Deployment

See [examples/kubernetes/](../examples/kubernetes/) directory for complete, production-ready manifests including:

- Namespace setup
- ConfigMaps and Secrets
- Deployment with HA configuration
- Service and Ingress
- RBAC configuration
- Network policies
- Monitoring setup

### Helm Chart

A Helm chart is planned for future releases. Track progress in [issue #XXX](https://github.com/stacklok/toolhive-registry-server/issues).

## Troubleshooting

### Pod Not Starting

Check logs:
```bash
kubectl -n toolhive logs -l app=registry-api
```

Common issues:
- ConfigMap not found or malformed
- Secret not found or missing keys
- Database connection failure
- Invalid authentication configuration

### Database Connection Issues

Verify:
```bash
# Check if database is reachable
kubectl -n toolhive exec -it deployment/registry-api -- \
  nc -zv postgres.database.svc.cluster.local 5432

# Check environment variables
kubectl -n toolhive exec -it deployment/registry-api -- env | grep -E 'THV_REGISTRY_|PGPASSFILE'
```

### Migration Failures

View migration job logs:
```bash
kubectl -n toolhive logs job/registry-migrate
```

Manually run migrations:
```bash
kubectl -n toolhive exec -it deployment/registry-api -- \
  thv-registry-api migrate up --config /etc/registry/config.yaml
```

## See Also

- [Configuration Guide](configuration.md) - Complete configuration reference
- [Database Setup](database.md) - Database configuration and migrations
- [Authentication](authentication.md) - OAuth and security configuration
- [Docker Deployment](deployment-docker.md) - Docker and Docker Compose