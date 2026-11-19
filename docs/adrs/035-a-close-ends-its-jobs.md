# 035 A close ends its jobs

## Context

A pull request closes once, merged or not, and the engine reads that delivery as the end of every started job (ADR 033:17). The same delivery can also be a start: ADR 033:11 names `pr.merged` as a condition a flow puts in `_start_.next`, and ADR 033:13 lets a start name `pr.updated` and start nothing. ADR 029:20 decides the unmerged close only, and ADR 030:12 writes `passed` only when the flow reached `_done_`.

Two questions follow. What does a merged close record? Can a close start a job?

## Decision

- A closed pull request starts no job. `_start_.next` may name only the conditions a delivery proves: `pr.created`, `pr.approved` and `pr.rejected`. The loader refuses `pr.updated` and `pr.merged` there.
  - This overrides ADR 033:11, which names `pr.merged` as a start condition, and ADR 033:13, which lets a start name `pr.updated` and start nothing. A start that no delivery can prove is dead configuration, and a job started at a close has no later delivery to advance it (ADR 033:17).
- A merged close ends every started job `done` with verdict `pending`.
  - The state says how the job ended and the verdict says what the flow proved. The flow did not reach `_done_`, so `passed` stays there (ADR 030:12), and `failed` would name a failure that did not happen. `pending` is the verdict the record has when no flow decided one.
- An unmerged close ends every started job `blocked` and `failed` (ADR 029:20).
- A close touches jobs in `started` only. A job that already ended keeps its row.

## Consequences

A landed pull request reads as landed, not as abandoned work. The clients render a `done` job with a pending verdict. A flow that needs work after a merge or after a push needs a new decision, not a start condition.
