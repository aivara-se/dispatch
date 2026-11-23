# 001 - The engine proves each task on its pull request

## Context

Work arrives as deliveries about pull requests, and the change and its proof live there. Without one root, tasks and verdicts scatter across runs; without a proof, a report of success is all there is and no reader can tell it from work that happened.

The engine cannot trust an agent's word, and it must not become a second author of the change. Every advance needs an effect it can read on GitHub, and every read needs to survive in the record.

## Decision

- The pull request is the root, `repo#n`. A job runs a flow's tasks; a task runs its actions in order.
- The engine advances only when it reads a condition on GitHub, and keeps that read as the task's evidence.
- The agent holds its credentials and performs every write to the change: commits, reviews, merges and comments. The engine reads, decides each step, and writes only the `blocked` label.
- One task runs per job at a time. A job stops at the limit its flow sets.
- A merged pull request ends its jobs `done`; a closed, unmerged one ends them `blocked`; a task still running ends `interrupted`.
- A prompt action's output is kept in the record; the agent comments on the pull request only when it sees fit.

## Consequences

Every task needs an effect the engine can read, so work is limited to what GitHub can show. The engine can label but not write code, review or merge, so a missing permission is a bug and not an accident. Parallel tasks need a later decision, and a job that stops cannot resume from its own word.
