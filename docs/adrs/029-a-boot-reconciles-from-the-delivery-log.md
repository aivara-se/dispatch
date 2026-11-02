# 029 - A Boot Reconciles From the Delivery Log

## Context

The service answers `202` before it acts, and nothing retries a wake it could not start. [`005`](005-the-audit-record.md) records `start_failed` and moves on, and a crash between the answer and the run leaves no line at all. [`021`](021-one-run-per-pull-request-per-agent.md) holds a delivery behind a live run, and a crash loses the held one with it. The product document says the service never drops a wake; on these paths it does.

[`028`](028-the-service-is-a-github-app.md) gives the service a way to ask. GitHub keeps the app's deliveries for three days, and a line about a delivery already carries its id. The two records together name every delivery that arrived and every one the service acted on.

## Decision

**At boot, and only at boot, the service decides again every delivery in the log that its audit does not account for.**

- **The window** starts where the last reconcile ended, or at GitHub's three-day edge when there is none, and ends when the reconcile starts. The reconcile ends before the first delivery is served, so no delivery falls in two windows.
- **Accounted for** is a line carrying that delivery id with the decision `started`, `ignored`, `duplicate` or `recovered`. A `held` line counts only when a later line carries the same id and the decision `started`. That later line is the follow-up [`021`](021-one-run-per-pull-request-per-agent.md) starts at the run's exit. A `start_failed` line is not accounted for, and a delivery with no line is not.
- **The payload comes from `GET /app/hook/deliveries/{id}`**, and the decision runs in process. The redelivery endpoint is not used. A replay re-enters the receiver, so it writes a delivery line and not a `recovered` one. It cannot name the earlier outcome, and it needs the listener to answer while the boot is still deciding. The fetch reads the payload and leaves GitHub's log alone.
- **The new line says `recovered` and carries the id.** It carries the event, the repository and the pull request from the fetched payload, and the earlier outcome when one exists. It carries no answer, because no sender waits for it.
- **The reconcile appends one line of its own**: the window it covered and how many deliveries it recovered. The next boot resumes from there.
- **A recovered line and the reconcile line count nothing, and neither is timed.** A recovered delivery was counted at its request, or its request never decided and never entered the counter. Counting it again would make the total exceed what arrived ([`026`](026-the-metrics-count-a-delivery-once.md)).
- **Nothing is stored.** The list is GitHub's, the audit is this service's, and the reconciliation adds no file.

## What this overrides

[`015`](015-the-audit-records-a-runs-exit.md) and [`021`](021-one-run-per-pull-request-per-agent.md) each close with "nothing reconciled at boot". That clause stops, and this reconcile replaces it. `021`'s consequence "A crash loses a held delivery" stops with it. The next boot finds the `held` line with no `started` after it, and decides again. `021`'s "Still no queue and no durable state" narrows: the reconcile reads the audit and writes to it, and stores nothing else.

[`018`](018-the-services-boot-reaches-the-audit.md) fixes three kinds of line and says "a fourth kind would need its own reason to exist". The reason is that the record's question is whether a wake happened. A delivery lost to a crash is the one answer no other line gives. `recovered` and the reconcile line are the fourth and the fifth kind. `018`'s "one line at boot" becomes two, both appended before the first delivery is served. Its "no heartbeat, no periodic line, no shutdown line" stands.

[`026`](026-the-metrics-count-a-delivery-once.md)'s "One increment per audit decision line and one line per increment" stops for a recovered line: it carries a decision and no increment. Its consequence that a line without an increment is a lost write narrows to a delivery's own decision. Its five values, its histogram and its follow-up counter stand, and the value `recovered` never reaches them.

[`005`](005-the-audit-record.md)'s "One JSON Lines line per delivery" and its answer field are extended, as `015` extended them. A recovered line has no sender, so it has no answer. Its "Only a delivery that verified is logged" holds for the receiver. A recovered line is a delivery GitHub logged, and the JWT is what vouched for it.

## Consequences

- **A crash costs one list and one call per unaccounted delivery**, not a lost wake.
- **Three days is the floor.** An outage longer than that is not recoverable, and the reconcile line says how many deliveries fell outside the window.
- **The audit is now load-bearing.** A line lost to a full disk is a wake the next boot repeats. That is the reverse failure, and it is the cheaper one.
- **A run that dies after it started is not recovered.** Its line says `started` and the reconcile trusts it. A wake is a reason the agent re-reads, so that gap stays as it is.
- **A boot that cannot reach GitHub still serves.** It skips the reconcile, and the next one reaches back to the last window that closed.
