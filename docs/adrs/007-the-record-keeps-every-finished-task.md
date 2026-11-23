# 007 - The record keeps every finished task

## Context

Measured on the production record after its first day: 1.29 MB, of which tasks are 1.13 MB, actions 100 KB, and jobs and deliveries 12 KB each. The tool-call payload decides the size: 40 of 60 tasks carry one at 28 KB each, and all 60 average 18,852 bytes. A delivery is 228 bytes.

The rate follows the work: 60 tasks in 21.7 hours is 66.4 a day, so 1.25 MB a day and 0.46 GB a year. Deliveries add 13 KB a day.

The record answers questions about the past — a job's state, the condition that decided a task, the evidence, and what the run did inside it. A pruning rule trades that answer for disk, and the trade is the wrong way round at half a gigabyte a year.

The tool-call payload is the only witness to a false report: an agent that says it ran the tests and made no call is caught by what it did not call.

## Decision

- No age and no size deletes a row. A job, its tasks and their actions stay for the life of the record.
- Growth is a function of work, not of retention. A delivery is not the table that matters; a task costs 83 times a delivery at the 18,852-byte mean.
- The tool-call payload stays. It is evidence, and a surface that hides a field is a defect in the surface, not a licence to drop the field.
- If the rate ever needs a bound, the lever is the payload, not the rows: compress the tool calls on write and leave every row in place. It is taken when a copy of the record no longer fits the free space beside the live file, which at this rate is years away.
- The record's own file stays one file, and its swap copy is a plain copy.

## Consequences

- The file grows for as long as work continues, at 0.46 GB a year for 66 tasks a day, and a swap holds a copy for the length of the swap.
- Every reader is unchanged: the client, the CLI and the API read the same shape.
- A third of the tasks record no tool call, so the field costs nothing on those rows.
- Compressing the payload is the recorded lever and it is not taken here. When it is, the payload stops reading with a plain SQLite client and reads through the API and the CLI.
- The four indexes the unique constraints create stay the only ones; nothing is added for retention.
