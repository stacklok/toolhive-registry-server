# Observability

Standalone exports OpenTelemetry traces/metrics via OTLP HTTP and, when metrics are
enabled, serves Prometheus metrics on **internal 8081 only**. Public 8080 serves
consumer/admin HTTP. Never proxy unauthenticated `/health`, `/readiness`, `/version`
or `/metrics` through the public endpoint.

## Configuration

```yaml
telemetry:
  enabled: true
  serviceName: thv-registry-api
  endpoint: otel-collector:4318
  insecure: false
  tracing:
    enabled: true
    sampling: 0.05
  metrics:
    enabled: true
```

The collector must be reachable from the server and accept OTLP HTTP. `insecure:
false` uses TLS; `true` is only for trusted local development. Traces and metrics
can be enabled independently, but both require top-level `enabled: true`.

| Setting | Default | Meaning |
|---|---|---|
| `enabled` | `false` | Initialize telemetry providers |
| `serviceName` | `thv-registry-api` | Resource service name |
| `serviceVersion` | `unknown` | Resource service version when omitted |
| `endpoint` | `localhost:4318` | Collector host:port; OTLP paths added automatically |
| `insecure` | `false` | Use HTTP rather than HTTPS for OTLP |
| `tracing.enabled` | `false` | Enable spans |
| `tracing.sampling` | `0.05` | Sampling ratio; when enabled must be >0 and <=1 |
| `metrics.enabled` | `false` | Enable OTLP and direct Prometheus readers |

OTLP metrics export every 60 seconds; traces use batch export. Disabling telemetry
uses no-op providers. To disable tracing, set its `enabled: false`, not sampling 0.

## Probes and scraping

For repository Compose, loopback 8081 is already mapped. For Kubernetes, the chart
probes pod port `internal-http` (8081); no public/internal Service exposure is
required for those probes. Use a pod scraper or an intentionally restricted
ClusterIP port for metrics. See [deployment networking](deployment-kubernetes.md#verify-and-operate)
and the [chart ingress policy](../deploy/charts/toolhive-registry-server/README.md#internal-port-exposure).

```sh
curl --fail http://localhost:8081/health
curl --fail http://localhost:8081/readiness
curl --fail http://localhost:8081/metrics
```

The last command requires telemetry and metrics enabled. These internal routes
have **no public HTTP metrics or tracing middleware**, authentication, audit or
rate limiting. Use the scraper's availability/latency metrics and probe results to
monitor them; `http_server_request_duration_seconds` does not measure probes.
A healthy process does not prove catalog access is authorized or every source is
fresh. Readiness checks database and runtime readiness, including initial operator
snapshot readiness. Failed refreshes retain last good snapshots.

For a Prometheus setup that discovers pods via annotations, chart values can add:

```yaml
podAnnotations:
  prometheus.io/scrape: "true"
  prometheus.io/port: "8081"
  prometheus.io/path: /metrics
```

Annotations do nothing without a scraper configured to honor them. Restrict 8081
to trusted scrapers; do not use public ingress for metrics. Alternatively scrape
the collector's configured Prometheus exporter endpoint after OTLP ingestion.

## Metrics reference

These are direct Prometheus series names; OTLP uses dotted instrument names.
HTTP metrics cover public HTTP, not internal probes. Counts are distinct names per
source, not per view/version, so overlapping sources can count the same name.

| Metric | Type | Labels | Meaning |
|---|---|---|---|
| `http_server_request_duration_seconds` | Histogram | `http_request_method`, `url_scheme`, `http_route`, `http_response_status_code` | Public request duration |
| `stacklok_registry_http_requests_total` | Counter | Same HTTP labels | Public request count |
| `http_server_active_requests` | UpDownCounter | `http_request_method`, `url_scheme` | In-flight public requests |
| `stacklok_registry_servers` | Gauge | `source` | Distinct server names |
| `stacklok_registry_skills` | Gauge | `source` | Distinct skill names |
| `stacklok_registry_plugins` | Gauge | `source` | Distinct plugin names |
| `stacklok_registry_sync_duration_seconds` | Histogram | `source`, `outcome` | Complete leased attempt duration, outcome `success` or `error` |
| `stacklok_registry_errors_total` | Counter | `error_type`, `area` | Classified HTTP/sync failures; supplementary to status/outcome |
| `stacklok_build_info_ratio` | Gauge | `component`, `version`, `commit` | Constant 1 with binary build identity |

Series include `stacklok_component="registry"` and `stacklok_product` resource
ownership labels. HTTP metric names derived from OpenTelemetry semantic conventions
are not product-prefixed. Route labels use templates rather than actual IDs.

- HTTP histogram bounds: 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10 seconds.
- Sync histogram bounds: 0.1, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 180, 300 seconds.

Sync instrumentation starts at successful lease acquisition and finishes on
successful commit/completion or failure. It measures fetch plus persistence, not
just remote IO; stale/unfinished attempts are not synthetic successful samples.
Counts are collected from the catalog via a cached DB aggregate (30-second success
TTL). On refresh failure, last successful counts can remain visible; monitor the
sanitized degradation/recovery logs, not merely a flat gauge, for freshness.

## Distributed tracing

The current standalone instrumentation emits:

| Span | Tracer | Attributes and scope |
|---|---|---|
| `{METHOD} {route-template}` | `github.com/stacklok/toolhive-registry-server/http` | Public HTTP server span; `http.request.method`, `http.route`, `url.path`, bounded `user_agent.original`, `http.response.status_code` |
| `catalog.fetch` | `catalog.ingestion` | Source fetch/validation span with `source`; generic error status `source fetch failed` |

Example route template: `GET /registry/{registryName}/v0.1/servers`.
Incoming W3C `traceparent` context is extracted for public HTTP. Background
scheduled ingestion has its own context; do not assume it is a child of a previous
consumer request. Logs carrying valid span context include `trace_id` and `span_id`.
HTTP 5xx marks a span error; 4xx leaves status unset rather than marking a server
failure. Fetch errors do not attach raw remote/backend causes to the span.

`catalog.fetch` covers fetching/validation, **not** the complete leased commit;
use the sync duration metric for complete attempts. No per-query/write spans are
promised. Internal probe/scrape requests do not emit HTTP spans or HTTP metrics.

## Troubleshooting

- **No metrics endpoint:** enable both `telemetry.enabled` and `metrics.enabled`,
  then reach internal 8081 through a trusted path, not public 8080.
- **Direct scrape works but OTLP is missing:** check collector host/port, TLS,
  receiver/exporter configuration and server egress. Allow at least one metrics
  export interval before diagnosing a gap.
- **No traces:** check both enable flags, sampling, collector trace pipeline and
  whether the operation is actually instrumented. Probes intentionally have none.
- **Stale counts:** inspect metric-count refresh degradation and recovery logs and
  DB availability. Last-good fallback is not proof of current catalog size.
- **Unexpected high cardinality:** use registered route templates; avoid dynamic
  IDs in custom metric labels. Treat raw URL paths/user agents in trace storage as
  potentially sensitive and restrict access/retention.

Structured logs use `THV_REGISTRY_LOG_LEVEL` (fallback `LOG_LEVEL`); audit is a
separate [configuration](configuration.md) concern. Do not dump tokens, credentials,
policy contents, raw claims or environment variables into debugging output.

## See also

- [Compose deployment](deployment-docker.md)
- [Kubernetes deployment](deployment-kubernetes.md)
- [Leased ingestion and reconciliation](registry-sync.md)
- [Persistence jobs and fencing](registry-persistence-jobs.md)
