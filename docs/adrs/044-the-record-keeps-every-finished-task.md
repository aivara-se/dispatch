# 044 The record keeps every finished task

## Context

Measured on the production record after its first day: 1.29 MB, of which `tasks` is 1.13 MB, `actions` 100 KB, and `jobs` and `deliveries` 12 KB each. `tasks.tools` — the tool-call JSON — decides the size: 40 of the 60 tasks carry one at 28 KB each, and all 60 together average 18,852 bytes. A delivery is 228 bytes.

The rate follows the work: 60 tasks in 21.7 hours is 66.4 a day, so 1.25 MB a day and 0.46 GB a year. Deliveries add 13 KB a day.

The record is the answer to a question about the past — a job's state, the condition that decided a task, the evidence behind it, and what the run did inside it. A pruning rule trades that answer for disk, and the trade is the wrong way round at half a gigabyte a year.

`tools` is that answer's only witness to a false report: an agent that says it ran the tests and made no call is caught by what it did not call. No surface shows it today — `web/src/lib/types.ts` declares the field and no page renders it, and the CLI prints jobs and not calls — so 85% of the file is kept for a reader that does not exist yet.

## Decision

- No age and no size deletes a row. A job, its tasks and their actions stay for the life of the record.
- Growth is a function of work and not of retention. `deliveries` is not the table that matters; a task costs 85 times a delivery.
- `tools` stays. It is evidence, and the missing surface is a defect in the client, not a licence to drop the field.
- If the rate ever needs a bound, the lever is the payload and not the rows: compress `tasks.tools` on write and leave every row in place. It is taken when a copy of the record no longer fits the free space beside the live file, which at this rate is years away.
- The record's own file stays one file: `journal_mode` is `delete`, so a stopped service leaves nothing beside it, and its swap copy is a `cp`.

## Consequences

- The file grows for as long as work continues, at 0.46 GB a year for 66 tasks a day, and a swap holds a copy of it for the length of the swap.
- Every reader is unchanged: the client, the CLI and the API read the same shape, and `tools` stays JSON.
- A third of the tasks record no tool call, so the field costs nothing on those rows and the file's size follows the runs that do call tools.
- Compressing `tools` is the recorded lever and it is not taken here. When it is, `tools` stops reading with a plain `sqlite3` and reads through the API and the CLI.
- The client showing a task's tool calls is owed, and it is a client change: until it lands, the record keeps a field no page displays.
- The four indexes the unique constraints create stay the only ones; nothing is added for retention.
