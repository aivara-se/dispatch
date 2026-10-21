# 020 - Telemetry Leaves Through a Collector, Not Through the Binary

## Context

The record answers what happened to a delivery and, since `015`, what a run exited with; `018` marks each boot. All of it is history, written for one host: answering "how many deliveries this week", "how slow is a wake", "is it alive" means shelling in and reading a file. Nothing is measurable while the service runs.

The operator's requirement is provider-neutral telemetry: instrument once, point it at any backend, and use OpenTelemetry for that boundary. What needs deciding is where the standard sits — inside this binary or outside it.

Inside is expensive. Measured on 2026-10-21 with Go 1.27.2, adding the SDK and an OTLP/HTTP exporter to a program that already serves HTTP:

| The program | Binary | Over plain HTTP |
|---|---|---|
| `net/http` alone | 8,617,812 | — |
| OpenTelemetry API, no-op provider | 10,291,041 | +1.67 MB |
| Trace SDK, no exporter | 11,213,799 | +2.60 MB |
| Trace and metric SDKs, both OTLP/HTTP exporters | 23,588,543 | +14.97 MB |

The API is cheap; the exporter is the weight — gRPC, protobuf and `genproto`, 24 modules and 63 lines of `go.sum` for the last row. It buys a wire format this service would send at most a few times a minute.

It also buys little: GitHub sends no trace context and the child run is untracked ([003](003-starting-a-run.md)), so every span would be a root carrying the audit line's attributes and a duration — a log line with timing. The operator's questions are counts, rates and a duration distribution.

## Decision

**The service exposes metrics in Prometheus text; a stock OpenTelemetry Collector scrapes them and exports OTLP. No telemetry dependency enters this module.**

- **One new route, `GET /metrics`, on a second loopback listener at `127.0.0.1:9464`** — OpenTelemetry's conventional Prometheus port, a constant in the code and not a configuration field, because it does not differ per host and a collision should stop the process at boot rather than be silently reconfigured. Served by the standard library's `net/http`, in `internal/metrics`, with no dependency ([001](001-go-and-the-standard-library.md) stands unamended).
- **Two instruments, in Prometheus' own conventions:** `dispatch_deliveries_total{event,decision}` and `dispatch_delivery_duration_seconds{decision}` (a histogram). The decision values are [005](005-the-audit-record.md)'s four: `started`, `ignored`, `duplicate`, `start_failed`. No label carries a payload field, a title, a branch, a login or a secret — the same rule the audit line keeps ([002](002-github-webhook-ingestion.md), [005](005-the-audit-record.md)).
- **No spans.** With no inbound context and no child instrumentation a trace is a slow log line; when a second service exists that this one calls, that is the moment to reconsider traces, and it will be its own record.
- **The collector is stock, general-purpose and not this repository's.** `otelcol` from `opentelemetry-collector-releases` (38 MB compressed, whose core distribution carries the Prometheus receiver and the OTLP/HTTP exporter), configured with a scrape target and an exporter — two blocks, no dispatch-specific code, no build. Any OTLP-compatible agent substitutes.
- **The provider's endpoint and key live in the collector's configuration**, never in `dispatch.json` and never in the service's environment ([004](004-configuration.md) is untouched). Swapping backends is an edit and a restart of the collector, not a rebuild of the service.

## What this overrides

[002](002-github-webhook-ingestion.md)'s "GitHub is its only inbound client" no longer holds: a collector on the same host reads `/metrics` over loopback. Nothing else about ingestion changes — the webhook route still verifies before it parses, and the metrics route reads nothing from a sender.

[001](001-go-and-the-standard-library.md) is not amended: this is the shape its rule prefers, a standard-library endpoint where the alternative is a dependency tree.

[004](004-configuration.md) is not amended: the fixed port is the service's, like a fixed flag name, and the document holds what it held.

## Consequences

- **The binary does not grow and the module gains nothing.** The whole cost moves to one general-purpose process on the host, which is the point: the collector is reusable by every service there, not built for this one.
- **A second thing to run, upgrade and watch.** 38 MB on disk, its own unit and its own config; if it dies, metrics stop and the service does not notice — the audit file is still the record, and nothing in the delivery path depends on the scrape.
- **Metrics are pulled, so a scrape gap is visible as a gap** rather than as silence from a broken exporter; the collector's own health is the operator's to monitor.
- **The route is loopback-only by construction**; it is not on the public listener and the proxy does not know it.
- **A histogram holds the duration distribution in one instrument**, so percentiles are the backend's to compute; the service keeps no sliding window and no state.
- **Traces are absent until a second service is in the path.** A request that crosses two of them will want one, and that is the trigger to revisit this record.
