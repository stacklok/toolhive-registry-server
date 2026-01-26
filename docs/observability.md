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

All metrics are prefixed with `thv_reg_srv_` to distinguish them from other metrics in the system.

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `thv_reg_srv_http_request_duration_seconds` | Histogram | `method`, `route`, `status_code` | Duration of HTTP requests |
| `thv_reg_srv_http_requests_total` | Counter | `method`, `route`, `status_code` | Total number of HTTP requests |
| `thv_reg_srv_http_active_requests` | UpDownCounter | - | Number of in-flight requests |
| `thv_reg_srv_servers_total` | Gauge | `registry` | Number of servers in each registry |
| `thv_reg_srv_sync_duration_seconds` | Histogram | `registry`, `success` | Duration of sync operations |

### Histogram Buckets

- **HTTP metrics:** 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10 seconds
- **Sync metrics:** 0.1, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300 seconds

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
