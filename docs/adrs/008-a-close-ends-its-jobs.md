# 008 A close ends its jobs

## Context

A pull request closes once, merged or not, and the engine reads that delivery as the end of every started job (ADR 006:17). The same delivery can also be a start: ADR 006:11 names `pr.merged` as a condition a flow puts in `_start_.next`. ADR 002:19 decides the unmerged close only, and ADR 003:12 writes `passed` only when the flow reached `_done_`.

Two questions follow. What does a merged close record? Can a close start a job?

## Decision

- A closed pull request starts no job. The loader refuses `pr.merged` in `_start_.next`, as it refuses `pr.updated` there.
  - This overrides ADR 006:11, which names `pr.merged` as a start condition. A job started at a close has no later delivery to advance it, and ADR 006:17 ends every job for a closed pull request. A start that can only end is not a start.
- A merged close ends every started job `done` with verdict `pending`.
  - The state says how the job ended and the verdict says what the flow proved. The flow did not reach `_done_`, so `passed` stays there (ADR 003:12), and `failed` would name a failure that did not happen. `pending` is the verdict the record has when no flow decided one.
- An unmerged close ends every started job `blocked` and `failed` (ADR 002:19).
- A close touches jobs in `started` only. A job that already ended keeps its row.

## Consequences

A landed pull request reads as landed, not as abandoned work. The clients render a `done` job with a pending verdict. A flow that needs work after a merge needs a new decision, not a start condition.
