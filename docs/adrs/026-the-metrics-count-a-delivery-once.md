# 026 - The Metrics Count a Delivery Once

## Context

[`020`](020-telemetry-leaves-through-a-collector.md) fixes the endpoint and the instruments: Prometheus text on `127.0.0.1:9464`, no dependency, `dispatch_deliveries_total{event,decision}` and a `dispatch_delivery_duration_seconds` histogram carrying [`005`](005-the-audit-record.md)'s decisions. Two records landed after it and moved the ground beneath it. [`021`](021-one-run-per-pull-request-per-agent.md) added a fifth decision, `held`, and made one delivery write two audit lines — the held one, and the follow-up its run's exit starts, carrying the same delivery id. [`024`](024-the-dedup-key-is-the-signed-body.md) bound the event header to the signed body, so the header a label reads is the one the signature vouches for.

Counted naively, those facts break the instrument. A held delivery increments `held` and then `started`, so a sum over decisions exceeds the deliveries that arrived and the operator's first question — how many landed — answers with two. A follow-up is also not a request: no event header, no request start, nothing to time.

The host runs Grafana Alloy rather than the `otelcol` `020` names; it scrapes the same text from the same loopback port.

## Decision

**One increment per delivery, at the decision its request produced.**

- Five decision values: `started`, `held`, `ignored`, `duplicate`, `start_failed`. `held` is `021`'s addition, not a replacement of one.
- A follow-up is its own counter, `dispatch_followups_total{decision}`, carrying `started` or `start_failed`. It has no event and is not timed, so it enters neither the delivery counter nor the histogram.
- The histogram times the handler, entry to decision. Its `_count` is deliveries decided, never lines written.
- The `event` label is the header verbatim. GitHub maintains the set, `024` makes it signed, and nothing that fails verification or parsing reaches an instrument, so a stranger adds no series.
- One increment per audit decision line and one line per increment: a run's exit line and a boot line carry no decision and count nothing.

## What this overrides

`020`'s "two instruments" — there are three — and its "`005`'s four" decisions — there are five. Its named collector gives way to the host's Alloy, which reads the same endpoint. The rest stands: the port, `internal/metrics`, the second loopback listener, standard library only, no spans, no configuration field, no audit change.

## Consequences

- **Deliveries are not wakes.** `sum(dispatch_deliveries_total)` counts what arrived; `started` plus `followups_total` counts what woke someone. A held burst reads as many deliveries and one follow-up.
- **A held delivery's sample times the wait's first half**, the decide and not the queue. The queue is the gap between its two audit lines, which is where an operator reads it.
- **A `401` or a `400` is in neither record.** It shows only as a delta against GitHub's own delivery count.
- **`event` has no allowlist.** An event this service has never seen adds a series and reads `ignored`; only a signed sender can grow it.
- **Two records, one decision.** A line without an increment, or the reverse, is a lost write rather than a metrics quirk, because both are taken at the same point.
