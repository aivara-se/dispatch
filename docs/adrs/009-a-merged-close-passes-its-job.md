# 009 A merged close passes its job

## Context

ADR 008 ends a merged close `done` with verdict `pending`, because the flow did not reach `_done_` and ADR 003:12 writes `passed` only there. The record then answers a landed pull request as an undecided job: job 1 on `dispatch-test#1` read `state: done` beside `verdict: pending` after its merge.

A merge is the effect every flow works toward. The engine does not merge; an agent does. When the merge lands, no later delivery can advance the job (ADR 006:17), and the remaining tasks have nothing left to prove.

## Decision

- A merged close ends every started job `done` with verdict `passed`.
  - This overrides ADR 008:13-14, which end the same close `done` with verdict `pending`.
  - This overrides ADR 003:12, which says `passed` only when the flow reached `_done_`. A merge is a second proof: the flow's change landed.
- An unmerged close ends every started job `blocked` and `failed` (ADR 002:20).
- A close ends each task still `running` under the jobs it ends as `interrupted`, and records its end.
  - A done or blocked job never shows a task in progress, and a killed action keeps no exit code.

## Consequences

A landed pull request reads as a success, and the clients need no change. A job that a merge ended never writes `_done_`, so a flow that needs work after the merge still needs a new decision (ADR 008). A task killed by a close reads as interrupted, not as work still in flight.
