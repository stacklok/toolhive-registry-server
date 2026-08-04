# Observability

The ToolHive Registry Server provides comprehensive observability through OpenTelemetry (OTEL), supporting both distributed tracing and metrics collection via OTLP exporters.

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────────────┐
│                     Registry Server                                  │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐                 │
│  │   HTTP      │  │   Sync      │  │  Registry   │                 │
│  │ Middleware  │  │  Metrics    │  │  Metrics    │                 │
│  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘                 │
│         │                │                │                         │
│         └────────────────┼────────────────┘                         │
│                          │                                          │
│                   ┌──────▼──────┐                                   │
│                   │  Telemetry  │                                   │
│                   │   Facade    │                                   │
│                   └──────┬──────┘                                   │
│                          │                                          │
│         ┌────────────────┼────────────────┐                         │
│         │                │                │                         │
│  ┌──────▼──────┐  ┌──────▼──────┐  ┌──────▼──────┐                 │
│  │   Tracer    │  │    Meter    │  │   Resource  │                 │
│  │  Provider   │  │  Provider   │  │  Attributes │                 │
│  └──────┬──────┘  └──────┬──────┘  └─────────────┘                 │
│         │                │                                          │
│         └────────┬───────┘                                          │
│                  │ OTLP HTTP                                        │
└──────────────────┼──────────────────────────────────────────────────┘
                   │
                   ▼
          ┌────────────────┐
          │      OTEL      │
          │   Collector    │
          └───────┬────────┘
                  │
        ┌─────────┼─────────┐
        │         │         │
        ▼         ▼         ▼
   ┌────────┐ ┌────────┐ ┌────────┐
   │ Jaeger │ │Promethe│ │ Grafana│
   │        │ │   us   │ │        │
   └────────┘ └────────┘ └────────┘
```

## Package Structure

The telemetry implementation is located in `internal/telemetry/`:

| File | Responsibility |
|------|----------------|
| `telemetry.go` | Main facade orchestrating tracer and meter providers |
| `config.go` | Configuration types with validation and defaults |
| `tracer.go` | TracerProvider setup with OTLP HTTP exporter |
| `meter.go` | MeterProvider setup with OTLP HTTP exporter |
| `metrics.go` | Application-specific metrics (registry and sync) |
| `middleware.go` | HTTP metrics middleware for Chi router |
| `tracing_middleware.go` | HTTP tracing middleware for distributed tracing |

## Configuration

Telemetry is configured via the main application config file:

```yaml
telemetry:
  enabled: true
  serviceName: "thv-registry-api"
  serviceVersion: "1.0.0"
  endpoint: "otel-collector:4318"
  insecure: true
  tracing:
    enabled: true
    sampling: 0.05  # 5% of traces sampled
  metrics:
    enabled: true
```

### Configuration Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enabled` | bool | `false` | Enable/disable all telemetry |
| `serviceName` | string | `"thv-registry-api"` | Service name in telemetry data |
| `serviceVersion` | string | `""` | Service version in telemetry data |
| `endpoint` | string | `"localhost:4318"` | OTLP HTTP endpoint |
| `insecure` | bool | `false` | Use insecure connection (no TLS) |
| `tracing.enabled` | bool | `false` | Enable distributed tracing |
| `tracing.sampling` | float64 | `0.05` | Trace sampling ratio (0.0-1.0) |
| `metrics.enabled` | bool | `false` | Enable metrics collection |

## Metrics Reference

Registry-specific metrics are prefixed with `stacklok_registry_` to distinguish them from
other metrics in the system. The two HTTP server metrics that have a direct OpenTelemetry
semantic-convention equivalent (`http.server.request.duration`, `http.server.active_requests`)
keep their unprefixed spec names instead, so they stay joinable with the same metric emitted
by any other semconv-instrumented service — see the metrics-standardization RFC's decision
rule: a metric whose name comes from an external semantic convention keeps the spec name
verbatim, with no `stacklok.*` prefix.

When `metrics.enabled` is `true`, the same metrics are also directly scrapable at
`/metrics` on the internal server (default port `8081`), independent of the OTLP export
path described above. That endpoint carries no authentication, consistent with the other
internal-server routes (`/health`, `/readiness`, `/version`) — keep port `8081` restricted
to trusted scrapers at the network level.

Every series additionally carries the constant labels `stacklok_component="registry"` and
`stacklok_product` (the D8 ownership labels), promoted from OTel resource attributes.

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `http_server_request_duration_seconds` | Histogram | `http_request_method`, `url_scheme`, `http_route`, `http_response_status_code` | Duration of HTTP requests (OTel semconv `http.server.request.duration`) |
| `stacklok_registry_http_requests_total` | Counter | `http_request_method`, `url_scheme`, `http_route`, `http_response_status_code` | Total number of HTTP requests |
| `http_server_active_requests` | UpDownCounter | `http_request_method`, `url_scheme` | Number of in-flight requests (OTel semconv `http.server.active_requests`) |
| `stacklok_registry_servers` | Gauge | `source` | Number of distinct servers in each source |
| `stacklok_registry_skills` | Gauge | `source` | Number of distinct skills in each source |
| `stacklok_registry_plugins` | Gauge | `source` | Number of distinct plugins in each source |
| `stacklok_registry_sync_duration_seconds` | Histogram | `source`, `outcome` | Duration of sync operations (`outcome` is `success` or `error`) |
| `stacklok_registry_errors_total` | Counter | `error_type`, `area` | Additive error-by-type classification for the sync (`area="sync"`) and HTTP (`area="http"`) paths — supplementary detail, not a replacement for the `outcome` label on `stacklok_registry_sync_duration_seconds` or `http_response_status_code` on `stacklok_registry_http_requests_total` |
| `stacklok_build_info_ratio` | Gauge | `component`, `version`, `commit` | Always `1`; build identity carried on labels. The OTel Prometheus exporter appends `_ratio` to gauges with unit `1`. Registered once per process and never unregistered — `RegistryMetrics.Unregister()` does not tear this gauge down, so it keeps observing for the life of the meter provider |

### Histogram Buckets

- **HTTP metrics:** 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10 seconds
- **Sync metrics:** 0.1, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 180, 300 seconds

## Distributed Tracing

The Registry Server implements distributed tracing across three layers: HTTP, Service, and Sync operations. Traces provide end-to-end visibility into request flows and background operations.

### Trace Hierarchy

```
┌─────────────────────────────────────────────────────────────────────┐
│ HTTP Request Span (root)                                            │
│ Name: "GET /registry/v0.1/servers"                                  │
│ ┌─────────────────────────────────────────────────────────────────┐ │
│ │ Service Span (child)                                            │ │
│ │ Name: "dbService.ListServers"                                   │ │
│ │  - Attributes: registry.name, pagination.limit, result.count    │ │
│ └─────────────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────┐
│ Background Sync Span                                                │
│ Name: "sync.performRegistrySync"                                    │
│  - Attributes: registry.name, registry.type, sync.success,         │
│                sync.duration_seconds, sync.server_count             │
└─────────────────────────────────────────────────────────────────────┘
```

### Span Reference

#### HTTP Layer Spans

| Span Name | Kind | Description |
|-----------|------|-------------|
| `{METHOD} {route}` | Server | Root span for all HTTP requests |

**Attributes:**
| Attribute | Type | Description |
|-----------|------|-------------|
| `http.request.method` | string | HTTP method (GET, POST, etc.) |
| `http.route` | string | Route pattern (e.g., `/registry/v0.1/servers/{serverName}`) |
| `url.path` | string | Actual URL path |
| `user_agent.original` | string | Client user agent |
| `http.response.status_code` | int | Response status code |

**Excluded Endpoints:**

The following endpoints are intentionally excluded from tracing:

| Endpoint | Reason for Exclusion |
|----------|---------------------|
| `/health` | Health check endpoint |
| `/readiness` | Readiness probe endpoint |

**Rationale for excluding health and readiness endpoints:**

1. **High frequency, low diagnostic value**: Health and readiness probes are typically called every 5-30 seconds by Kubernetes or load balancers. This generates a high volume of nearly identical spans that provide minimal insight into application behavior.

2. **Trace storage costs**: Each span consumes storage in your tracing backend (e.g., Jaeger, Tempo). Health check spans can account for 50-90% of total span volume while providing almost no diagnostic value, significantly increasing storage costs.

3. **Signal-to-noise ratio**: When investigating issues, health check spans clutter trace views and make it harder to find meaningful application traces. Excluding them improves the signal-to-noise ratio for debugging.

4. **Predictable behavior**: Health and readiness endpoints have simple, predictable behavior (return 200 OK or error). Unlike business logic endpoints, they rarely need trace-level debugging—HTTP metrics are sufficient for monitoring their behavior.

5. **Industry best practice**: Most observability frameworks and guidelines recommend excluding infrastructure endpoints from tracing. The OpenTelemetry community generally advises filtering out health checks at the instrumentation level.

If you need to debug health check issues, HTTP metrics (`http_server_request_duration_seconds`) still capture latency and error rates for these endpoints.

#### Service Layer Spans

| Span Name | Description |
|-----------|-------------|
| `dbService.ListServers` | List servers with optional filtering |
| `dbService.ListServerVersions` | List versions of a specific server |
| `dbService.GetServerVersion` | Get a specific server version |
| `dbService.PublishServerVersion` | Publish a new server version |
| `dbService.DeleteServerVersion` | Delete a server version |
| `dbService.ListRegistries` | List all registries |
| `dbService.GetRegistryByName` | Get a specific registry |
| `dbService.CreateRegistry` | Create a new registry |
| `dbService.UpdateRegistry` | Update an existing registry |
| `dbService.DeleteRegistry` | Delete a registry |

**Common Attributes:**
| Attribute | Type | Description |
|-----------|------|-------------|
| `registry.name` | string | Name of the registry |
| `server.name` | string | Name of the server |
| `server.version` | string | Version of the server |
| `pagination.limit` | int | Page size limit |
| `pagination.has_cursor` | bool | Whether pagination cursor is used |
| `result.count` | int | Number of results returned |
| `registry.type` | string | Type of registry (git, api, file, managed) |

#### Sync Operation Spans

| Span Name | Description |
|-----------|-------------|
| `sync.performRegistrySync` | Sync a registry from its source |

**Attributes:**
| Attribute | Type | Description |
|-----------|------|-------------|
| `registry.name` | string | Name of the registry being synced |
| `registry.type` | string | Type of registry source |
| `sync.success` | bool | Whether sync completed successfully |
| `sync.duration_seconds` | float64 | Duration of the sync operation |
| `sync.server_count` | int | Number of servers synced (on success) |

### Example Traces

#### Successful API Request

```
Trace ID: abc123def456...

[12ms] GET /registry/v0.1/servers
├── http.request.method: GET
├── http.route: /registry/v0.1/servers
├── http.response.status_code: 200
│
└── [10ms] dbService.ListServers
    ├── registry.name: upstream
    ├── pagination.limit: 50
    ├── pagination.has_cursor: false
    └── result.count: 25
```

#### Background Sync Operation

```
Trace ID: xyz789abc...

[30.5s] sync.performRegistrySync
├── registry.name: upstream
├── registry.type: git
├── sync.success: true
├── sync.duration_seconds: 30.5
└── sync.server_count: 42
```

#### Failed Request with Error

```
Trace ID: err456def...

[5ms] GET /registry/v0.1/servers/unknown/versions/1.0.0
├── http.request.method: GET
├── http.route: /registry/v0.1/servers/{serverName}/versions/{version}
├── http.response.status_code: 404
│
└── [3ms] dbService.GetServerVersion
    ├── server.name: unknown
    ├── server.version: 1.0.0
    ├── status: ERROR
    └── exception.message: server not found: unknown@1.0.0
```

### Tracer Names

Each component uses a unique tracer name for identification:

| Component | Tracer Name |
|-----------|-------------|
| HTTP Middleware | `github.com/stacklok/toolhive-registry-server/http` |
| Database Service | `github.com/stacklok/toolhive-registry-server/service/db` |
| Sync Coordinator | `github.com/stacklok/toolhive-registry-server/sync/coordinator` |

### Context Propagation

The Registry Server supports W3C Trace Context propagation. Incoming requests with `traceparent` headers will have their trace context extracted and used as the parent for all child spans. This enables distributed tracing across multiple services.

### Future Tracing Enhancements

The following components are not yet instrumented but are planned for future tracing coverage:

| Component | Description | Potential Value |
|-----------|-------------|-----------------|
| Sync Writer | Database write operations during sync | Diagnose write performance bottlenecks |
| State Service | Sync state tracking operations | Debug sync state management issues |

These additions would provide deeper visibility into sync operations, particularly useful for diagnosing performance issues in large-scale deployments.

## Implementation Details

### Graceful Degradation

The telemetry implementation handles disabled or missing components gracefully:

- When telemetry is disabled, no-op providers are used (zero overhead)
- Metrics and tracing can be independently enabled/disabled
- Nil provider checks prevent panics if metrics are not configured

### Route Pattern Extraction

The HTTP middleware extracts Chi route patterns (e.g., `/registry/v0.1/servers/{serverName}`) instead of actual URLs (e.g., `/registry/v0.1/servers/my-server`) to prevent metric cardinality explosion.

### Resource Attributes

All telemetry data includes these resource attributes:

| Attribute | Description |
|-----------|-------------|
| `service.name` | Service name from config |
| `service.version` | Service version from config |
| `host.name` | Hostname of the running instance |
| `telemetry.sdk.name` | "opentelemetry" |
| `telemetry.sdk.language` | "go" |
| `telemetry.sdk.version` | OTEL SDK version |

### OTLP Export

Both traces and metrics are exported via OTLP HTTP (port 4318 by default):

- Traces use batch processing for efficiency
- Metrics use a periodic reader with 60-second intervals

## Troubleshooting

### No Metrics Appearing

1. Verify telemetry is enabled in config:
   ```yaml
   telemetry:
     enabled: true
     metrics:
       enabled: true
   ```

2. Check the OTEL endpoint is reachable from the registry server

3. Verify the OTEL Collector is configured to receive OTLP and export to Prometheus

4. Check Prometheus is scraping the OTEL Collector's metrics endpoint (default port 8889)

### High Cardinality Warnings

If you see high cardinality warnings, check for:

- Custom routes not registered with Chi (will show as `unknown_route`)
- Dynamic path segments not using Chi parameters

### Missing Traces

1. Verify tracing is enabled:
   ```yaml
   telemetry:
     tracing:
       enabled: true
   ```

2. Check sampling rate - default is 5% (`sampling: 0.05`)

3. Verify Jaeger or trace backend is configured in OTEL Collector
