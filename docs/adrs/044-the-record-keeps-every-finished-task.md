# 044 The record keeps every finished task

## Context

Measured on the production record after its first day: 1.29 MB, of which `tasks` is 1.13 MB, `actions` 100 KB, and `jobs` and `deliveries` 12 KB each. A task costs 19.3 KB and a delivery 228 bytes, and `tasks.tools` — the tool-call JSON — is the bulk of a task, averaging 28 KB with one at 76 KB.

The rate follows the work: 60 tasks in 21.7 hours is 66 a day, so 1.25 MB a day and 0.45 GB a year. Deliveries add 13 KB a day.

The record is the answer to a question about the past — a job's state, the condition that decided a task, the evidence behind it, and what the run did inside it. A pruning rule trades that answer for disk, and the trade is the wrong way round at a third of a gigabyte a year.

## Decision

- No age and no size deletes a row. A job, its tasks and their actions stay for the life of the record.
- Growth is a function of work and not of retention. `deliveries` is not the table that matters; a task costs 85 times a delivery.
- If the rate ever needs a bound, the lever is the payload and not the rows: compress `tasks.tools` on write and leave every row in place.
- The record's own file stays one file: `journal_mode` is `delete`, so a stopped service leaves nothing beside it, and its swap copy is a `cp`.

## Consequences

- The file grows for as long as work continues, at 0.45 GB a year for 66 tasks a day, and a swap holds a copy of it for the length of the swap.
- Every reader is unchanged: the client, the CLI and the API read the same shape, and `tools` stays JSON.
- Compressing `tools` is the recorded lever and it is not taken here. When it is, `tools` stops reading with a plain `sqlite3` and reads through the API and the CLI.
- The four indexes the unique constraints create stay the only ones; nothing is added for retention.
