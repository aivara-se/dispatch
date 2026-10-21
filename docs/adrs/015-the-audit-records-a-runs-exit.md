# 015 - The Audit Records a Run's Exit

## Context

`005` fixes the record as one line per delivery, and `003` fixes the run as untracked: the child is started and forgotten. Together they leave the service unable to tell a wake that ran from a wake whose child died at once — a renamed `hermes` binary, a profile that no longer exists, an expired credential. The line says a run was started, which is the last thing the service knows and not the thing the operator needs, and `003` itself names the argv as a contract no test here covers.

The operator's habit is to read this file when a wake is missed. A missed wake whose line reads green is the one failure the file cannot show.

## Decision

- **The service waits on each child it starts and reads its exit status once.** Nothing else about the run is observed, and nothing about it is stored.
- **A run that exits non-zero appends one line of its own**, carrying the delivery id, the profile and the exit status, and no other field — not the child's output, not its duration, not the reason.
- **A run that exits zero appends nothing.**
- **Still no retry, no queue, no run table, no lifetime bound and nothing reconciled at boot.** The wait is a goroutine per child, and it ends with the child.
- **A failed run is not retried and does not change the delivery's answer**, which was given before the child was started.

## What this overrides

`003`'s "The run is not tracked", and with it its "no exit-code bookkeeping": the exit status is read, and only to be written. What replaces the clause is exact — one line outlives the request, and no state does.

`005`'s "One JSON Lines line per delivery, appended in arrival order" and its consequence that "a line says a run was started and never what became of it". The file becomes one line per delivery plus one per failed run, appended when each happens, so its order is the order of writing and no longer of arrival. `005`'s field list, its never-a-secret rule and its "answers the acceptance exactly" hold for the delivery line. Everything else in `003` and `005` stands.

## Consequences

- A break in the contract with Hermes becomes visible in the file the operator already reads, at the run that failed rather than at the review that never came.
- A delivery whose run fails writes two lines, the second minutes later. A reader pairs them by the delivery id.
- The service keeps a goroutine per run until it exits. A child that hangs holds one, and nothing bounds or kills it, as before.
- An agent that answers and does the wrong thing still exits zero and leaves no line. The record says the run ran, not that it was right.
- The file grows with failed runs alone, so a healthy service's record is unchanged in size.
