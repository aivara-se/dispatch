# 010 A delivery is kept before it is applied

## Context

GitHub waits a fixed time for a webhook reply, and a close calls GitHub. The handler therefore answers 202 once the delivery is queued, and one worker applies the queue.

The queue is memory. A delivery that fails after the 202 is logged and dropped: `Wait` returns nil, a stop reads clean, and GitHub holds its 202 so it does not resend. No row shows the delivery, and no later step can find it.

ADR 006:18 says state lives in the local SQLite record, so a restart resumes what is unfinished. An accepted delivery is state, and no row holds it.

## Decision

- The record keeps one row for every verified delivery, before the handler answers 202.
  - The row carries every field an apply reads, including whether the pull request merged and the state of the review it carried.
  - A write that fails answers 503, so GitHub retries the delivery.
- The dedupe reads the row, so a repeat is dropped while its first copy waits.
- Boot applies the rows that are not applied, in arrival order, before the runner resumes and before the listener opens.
- A failed apply keeps its error on the row and stays unapplied. `Wait` logs the count it could not apply, and a stop exits clean, because the row is the record.
- An apply is repeatable: a crash between the work and the applied mark repeats the work at the next start, and a merged close writes `done` and `passed` again.
- This amends ADR 006:22: a delivery that never reached the engine is still not replayed, and a delivery the engine kept before the 202 is applied at the next start.

## Consequences

A delivery that reached the engine survives a stop, and a failed one waits for the next start instead of vanishing. Replay is bounded by the count of unapplied rows; the table is not, because every verified delivery gains a row, including one that starts nothing, and no pruning is decided. Each apply can call GitHub, and the listener opens only after them.
