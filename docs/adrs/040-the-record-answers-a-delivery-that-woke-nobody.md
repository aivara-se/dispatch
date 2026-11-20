# 040 The record answers a delivery that woke nobody

## Context

`deliveries` is written only when a delivery starts or ends a job (`engine.Deliver`'s `touched`). A delivery the service ignores therefore answers `202` and leaves nothing: a review requested from a login no agent holds, a review requested from a team, and an `edited` or `synchronize` that no flow starts on. Measured on the first production run: a signed delivery naming `thani-sh-root` answered 202 and wrote no row, and the operator could not tell it from a delivery that never arrived. `audit.jsonl` carries no line since 19 November, so the host's journal is not the answer either.

`038` names one of these silences as intended: "a login no agent holds ... appends no line". The intent stands, the place does not. The record is what the operator reads, and a silent stop that reads as an idle day is the problem the service exists to solve.

## Decision

- Every delivery the service parses for a served repository gets one row, with a `decision` of `started`, `ended` or `ignored`.
- `decision` names what the delivery changed; `reason` names what it could not do, from a closed set: `no-flow`, `unheld-login`, `team-review`, `already-started`. An `ignored` row always carries one. A `started` row carries `unheld-login` when a login it was asked for is no agent's, and carries none otherwise.
- `started` when it created at least one job, `ended` when it ended one; a delivery that reaches its flow and finds the job already there has changed nothing, so it is `ignored` with the reason `already-started`. Its job's own start time is not the column's answer.
- The row is written once, at the point the service stops considering that delivery, and it is the dedupe claim. So no row still means the service did not finish, and redelivering every failed delivery stays safe.
- A delivery that names no pull request, or a repository the service does not serve, keeps no row: the allowlist and GitHub's own delivery list answer those, and the App's install scope would otherwise fill the record with other repositories' traffic.
- A `started` row counts the jobs the delivery started, so one delivery that wakes two reviewers reads one row with `2` beside two job rows.
- The reason names the service's own finding, never an agent's text.

## Consequences

- The record grows with every delivery on a served repository's pull request, and `synchronize` is the bulk.
- Pruning is a later policy and needs no schema: `received_at` is its only input, and a pruned row un-claims its delivery. That is safe because a job is created once per flow, pull request and agent, so a re-driven wake of a pruned delivery creates nothing twice. The age must exceed the window in which GitHub still redelivers a failed delivery, and is the operator's number.
- An ignored delivery is claimed, so a redelivery of it is refused. When a configuration change would make it act, the operator requests the review again.
- `038`'s "a login no agent holds ... appends no line" is retired; its silence still wakes nobody, and it now says why.
- A delivery that names a held login and an unheld one is `started` with the reason `unheld-login`, so the row names the class and not the login: the pull request's reviewer list is where the login reads.
- An unserved repository reads only in GitHub's delivery list, which the service does not have a credential to read.
