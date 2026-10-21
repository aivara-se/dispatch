# 020 - Observability Is OpenTelemetry over OTLP

## Context

The audit file answers what happened to a delivery and, since `015`, what a run exited with; `018` marks each boot. All of it is one file on the host, read by shelling in. Nothing about the service is measurable from anywhere else: how many deliveries arrive, how long a delivery takes to become a wake, how often a wake fails to start, whether the process is alive between the lines the record happens to carry.

The operator's requirement is provider-neutral telemetry: instrument once, point it at any backend. OpenTelemetry is that standard, and OTLP is the wire format every collector and hosted backend accepts.

The price is the first dependency in a module that has none. [001](001-go-and-the-standard-library.md) prefers the standard library wherever it can do the job. Measured on 2026-10-21 with Go 1.27.2: a program that sets up a tracer provider and an OTLP/HTTP exporter builds 13,374,808 bytes larger than the same program without it (21,992,652 against 8,617,844), writes 59 lines to `go.sum`, and pulls gRPC and protobuf into the tree.

## Decision

**OpenTelemetry, OTLP/HTTP over protobuf, confined to one package.**

- **`internal/telemetry` is the only place the SDK, the exporter and the semantic conventions appear.** It returns a provider the receiver holds the way it holds a `Runner`, and a no-op when nothing is configured.
- **Configuration is OpenTelemetry's own environment variables, not the configuration document:** `OTEL_EXPORTER_OTLP_ENDPOINT`, and `OTEL_EXPORTER_OTLP_HEADERS` when the provider wants a key, in the unit's environment file beside the webhook secret ([004](004-configuration.md)). An unset endpoint means a no-op provider and no export. The service name is `dispatch`, set in code as the resource default so a host that omits it still reports correctly.
- **One span per verified delivery:** `dispatch.delivery`, opened at the receiver's entry and ended at its answer. Its attributes are the audit line's own non-secret fields — `github.event`, `github.repository`, `github.number`, `dispatch.decision`, `dispatch.profile` when a run started — plus `http.response.status_code`. No payload body, no title, no branch, no login, no secret value ([002](002-github-webhook-ingestion.md), [005](005-the-audit-record.md)).
- **Two instruments:** `dispatch.deliveries`, a counter by `event` and `decision`, and `dispatch.delivery.duration`, a histogram in milliseconds by `decision`. The decision values are [005](005-the-audit-record.md)'s four: `started`, `ignored`, `duplicate`, `start_failed`.
- **Export is asynchronous and bounded:** a batch span processor, a periodic metric reader, a five-second export timeout and a flush on the shutdown path. No delivery is held, retried by the service, reordered or answered differently because the endpoint is slow or absent.
- **The child run is not instrumented.** The span ends when the run is started; the run is untracked ([003](003-starting-a-run.md)), and its exit is the audit's ([015](015-the-audit-records-a-runs-exit.md)).

## What this overrides

[001](001-go-and-the-standard-library.md)'s "a third-party dependency is added only when no standard-library equivalent exists", and its consequence "no dependency tree to update". There is no standard-library OTLP exporter; hand-writing the protobuf mapping would leave the service owning a wire format it did not design, and it would still not give the SDK's batching and provider-neutral resource handling. The dependency is admitted for the exporter, once, and the rule it expresses survives: no second dependency without the same bar.

Everything else in 001 stands — Go, the standard library wherever it still fits, one static binary, one deploy artefact.

[004](004-configuration.md) is untouched: the document holds what it held, and the endpoint lives in the environment because the key that travels with it must.

## Consequences

- **The binary grows by about 13 MB and the module gains a tree** — 59 lines of `go.sum`, gRPC and protobuf included — for a service that previously had none. That tree is now something to keep updated, and it is the whole of the cost this buys.
- **Whether telemetry is on is not answerable from `dispatch.json`.** The unit's environment file says so, and nothing validates it: a typo in the variable's name exports nothing and fails no check. The symptom is an empty backend, not a refusal at boot.
- **An unreachable endpoint fails quietly.** The exporter drops after its retries and logs; the delivery is still answered `202` and its audit line is still appended. The record remains the record, and telemetry is a convenience over it.
- **Repository names and numbers leave the host.** They already do in `audit.jsonl`, and the operator chooses the sink; an endpoint left unset sends nothing at all.
- **A provider's key joins the webhook secret in the environment file** — one file, mode `600`, neither value in the tree.
- **Every span is a root.** GitHub sends no trace context, so a delivery cannot be joined to the workflow run or the agent session it is about; the span measures the service, not the work.
- **No collector, dashboard or alert ships here.** This decides the exporter and the signal, not an observability stack.
