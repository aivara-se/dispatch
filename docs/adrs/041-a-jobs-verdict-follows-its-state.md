# 041 A job's verdict follows its state

## Context

On the first production record, 12 of 18 ended jobs read `state done` with `verdict pending` — "finished, outcome unknown" on the site. Every one of the twelve sits on a merged pull request, so every one should read `passed`.

Two writers set the pair. A merged close wrote `done` with `pending` until `2f57c68`, and `_done_` writes `done` with `passed`. The close delivery lands within seconds of the merge, before the runner re-reads `pr.merged`, so the close path wins in practice and the task's own result is never used.

The repair for those rows is `207`. It does not remove the class: `verdict` holds a function of `state` in a second column, and every ending path must remember both halves. The state alone is total — `done` from `_done_` or a merged close, `blocked` from any other stop, `started` while running.

## Decision

- `verdict` is not stored. A job's verdict is its state's: `done` is `passed`, `blocked` is `failed`, `started` is `pending`.
- The record derives it on read, so the API's `verdict` field and every client stay as they are.
- A state the mapping does not know is an error, never a default. A new state is a decision about its verdict, in the record that adds it.

## Consequences

- The pair cannot disagree again, so no reader meets "finished, outcome unknown" for a job that ended.
- The `207` repair becomes a version step: its `UPDATE` is deleted with the column, and a record still at the earlier version is repaired once.
- A stored pair leaves the schema, so a record from an older version is rebuilt once and its rows keep the verdict its state gives them.
- A job that stops `blocked` on a pull request that later merges still reads `failed`: the record is the engine's decision at the time it stopped, not the pull request's later state. `036`'s "a merged close ends every started job done with verdict passed" holds, and the close now writes the state alone.
