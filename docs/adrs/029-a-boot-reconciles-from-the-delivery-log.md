# 029 - A Boot Reconciles From the Delivery Log

## Context

The service answers `202` before it acts, and nothing retries a wake it could not start. [`005`](005-the-audit-record.md) records `start_failed` and moves on, and a crash between the answer and the run leaves no line at all. [`021`](021-one-run-per-pull-request-per-agent.md) holds a delivery behind a live run, and a crash loses the held one with it. The product document says the service never drops a wake; on these paths it does.

[`028`](028-the-service-is-a-github-app.md) gives the service a way to ask. GitHub keeps the app's deliveries for three days, and a line about a delivery already carries its id. The two records together name every delivery that arrived and every one the service acted on.

## Decision

**At boot, and only at boot, the service decides again every delivery in the log that its audit does not account for.**

- **The window** starts where the last reconcile ended, or at GitHub's three-day edge when there is none, and ends now.
- **Accounted for** is a line carrying that delivery id with the decision `started`, `ignored`, `duplicate` or `recovered`. A `held` line counts only when a later line carries the same id and the decision `started`, which is the follow-up [`021`](021-one-run-per-pull-request-per-agent.md) starts when the run ends. A `start_failed` line is not accounted for, and a delivery with no line is not.
- **The payload comes from `GET /app/hook/deliveries/{id}`** and the decision runs in process. The redelivery endpoint is not used: it re-enters the receiver, and when the process did not crash the body is already in the dedup cache, so the replay would be dropped as a `duplicate`.
- **The new line says `recovered`**, carries the id, and names the earlier outcome when one exists.
- **The reconcile appends one line of its own**, carrying the window it covered and how many deliveries it recovered, so the next boot resumes from there.
- **Nothing is stored.** The list is GitHub's, the audit is this service's, and the reconciliation adds no file.

## Consequences

- **A crash costs one list and one call per unaccounted delivery**, not a lost wake.
- **Three days is the floor.** An outage longer than that is not recoverable, and the reconcile line says how many deliveries fell outside the window.
- **The audit is now load-bearing.** A line lost to a full disk is a wake the next boot repeats. That is the reverse failure, and it is the cheaper one.
- **A run that dies after it started is not recovered.** Its line says `started` and the reconcile trusts it. A wake is a reason the agent re-reads, so that gap stays as it is.
- **A boot that cannot reach GitHub still serves.** It skips the reconcile, and the next one reaches back to the last window that closed.
