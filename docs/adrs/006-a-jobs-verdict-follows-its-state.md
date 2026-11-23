# 006 - A job's verdict follows its state

## Context

On the first production record, 12 of 18 ended jobs read `state done` with `verdict pending` — "finished, outcome unknown" on the site. Every one of the twelve sits on a merged pull request, so every one should read `passed`.

Two writers set the pair. A merged close wrote `done` with `pending`, and the success path wrote `done` with `passed`. The close delivery lands within seconds of the merge, before the runner re-reads the merge condition, so the close path won in practice and the task's own result was never used.

A repair for those rows does not remove the class: a stored verdict holds a function of state in a second column, and every ending path must remember both halves. The state alone is total.

## Decision

- A job's verdict is not stored. It is its state's: `done` is `passed`, `blocked` is `failed`, `started` is `pending`.
- The record derives it on read, so the API and every client keep the same shape.
- A state the mapping does not know is an error, never a default. A new state is a decision about its verdict.

## Consequences

- The pair cannot disagree again, so no reader meets "finished, outcome unknown" for a job that ended.
- The repair becomes a version step: a stored verdict leaves the schema, and a record from an older version is rebuilt once.
- A job that stops `blocked` on a pull request that later merges still reads `failed`: the record is the engine's decision at the time it stopped, not the pull request's later state. `001`'s "A merged pull request ends its jobs `done`" holds.
- A new state costs a mapping entry in the same change that adds it.
