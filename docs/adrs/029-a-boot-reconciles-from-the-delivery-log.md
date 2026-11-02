# 029 - A Boot Reconciles From the Delivery Log

## Context

The service answers `202` before it acts, and nothing retries a wake it could not start. [`005`](005-the-audit-record.md) records `start_failed` and moves on, and a crash between the answer and the run leaves no line at all. [`021`](021-one-run-per-pull-request-per-agent.md) holds a delivery behind a live run, and a crash loses the held one with it. The product document says the service never drops a wake; on these paths it does.

[`028`](028-the-service-is-a-github-app.md) gives the service a way to ask. GitHub keeps the app's deliveries for three days, and a line about a delivery already carries its id. The two records together name every delivery that arrived and every one the service acted on.

## Decision

**At boot, and only at boot, the service decides again every delivery in the log that its audit does not account for.**

- **The window** starts where the last reconcile ended, or at GitHub's three-day edge when there is none, and ends now.
- **Accounted for** is a line carrying that delivery id with the decision `started`, `ignored`, `duplicate` or `recovered`. A `held` line counts only when a later line carries the same id and the decision `started`, which is the follow-up [`021`](021-one-run-per-pull-request-per-agent.md) starts when the run ends. A `start_failed` line is not accounted for, and a delivery with no line is not.
- **The payload comes from `GET /app/hook/deliveries/{id}`** and the decision runs in process. The redelivery endpoint is not used: a replay re-enters the receiver, so it writes a delivery line and not a `recovered` one, it cannot name the earlier outcome, and it needs the listener to answer while the boot is still deciding. The fetch reads the payload and leaves GitHub's log alone.
- **The new line says `recovered`**, carries the id, names the event, the repository and the pull request the fetched payload carries, and names the earlier outcome when one exists. It carries no answer: no sender waits for one.
- **The reconcile appends one line of its own**, carrying the window it covered and how many deliveries it recovered, so the next boot resumes from there.
- **Neither line carries a decision.** A recovered delivery was counted when its request decided, or its request never decided and never entered the counter, and a recovery is not timed ([`026`](026-the-metrics-count-a-delivery-once.md)); the reconcile is the boot's act and not a delivery. `026` therefore stands as written: five decision values, one increment per decision line.
- **Nothing is stored.** The list is GitHub's, the audit is this service's, and the reconciliation adds no file.

## What this overrides

[`015`](015-the-audit-records-a-runs-exit.md) and [`021`](021-one-run-per-pull-request-per-agent.md) each close with "nothing reconciled at boot". That clause stops, and this reconcile replaces it. `021`'s consequence "A crash loses a held delivery" stops with it: the next boot finds the `held` line with no `started` after it and decides again. `021`'s "Still no queue and no durable state" narrows — the reconcile reads the audit and writes to it, and stores nothing else.

[`018`](018-the-services-boot-reaches-the-audit.md) fixes three kinds of line and says "a fourth kind would need its own reason to exist". The reason is that the record's question is whether a wake happened, and a delivery lost to a crash is the one answer no other line gives. `recovered` and the reconcile line are the fourth and the fifth kind, so `018`'s "one line at boot" becomes two, both appended before the first delivery is decided; its "no heartbeat, no periodic line, no shutdown line" stands.

[`005`](005-the-audit-record.md)'s "One JSON Lines line per delivery" and its answer field are extended, as `015` extended them: a recovered line has no sender and so no answer. Its "Only a delivery that verified is logged" holds for the receiver; a recovered line is a delivery GitHub logged and this boot re-read, and the JWT is what vouched for it.

## Consequences

- **A crash costs one list and one call per unaccounted delivery**, not a lost wake.
- **Three days is the floor.** An outage longer than that is not recoverable, and the reconcile line says how many deliveries fell outside the window.
- **The audit is now load-bearing.** A line lost to a full disk is a wake the next boot repeats. That is the reverse failure, and it is the cheaper one.
- **A run that dies after it started is not recovered.** Its line says `started` and the reconcile trusts it. A wake is a reason the agent re-reads, so that gap stays as it is.
- **A boot that cannot reach GitHub still serves.** It skips the reconcile, and the next one reaches back to the last window that closed.
