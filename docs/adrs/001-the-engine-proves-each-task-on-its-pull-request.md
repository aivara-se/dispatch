# 001 The engine proves each task on its pull request

## Context

The operator runs a fleet of agents that already hold identities, sessions and skills. The engine must own the sequence of work without taking execution away. Work arrives as deliveries about pull requests. The change and its proof live there. Without one root, tasks and verdicts scatter; without a proof, a report of success is all there is.

## Decision

- The pull request is the root, `repo#n`. A job runs a flow's tasks; a task runs its actions in order.
- A delivery that names no pull request starts nothing.
- A merged or closed pull request ends the job.
- The agent holds its credentials and performs every write to the change: commits, reviews, merges and comments. The engine reads, decides each step, advances only when it reads a condition on GitHub, and writes only the `blocked` label, through its `label` action.
- A job stops at its limit.
- A prompt action's output is kept in the record; the agent comments on the pull request only when it sees fit.
- One task runs per job at a time.

## Consequences

Every task needs an effect the engine can read, which limits work to what GitHub can show. The engine can label but not write code, review or merge, so a missing permission is a bug and not an accident. Parallel tasks wait for a later decision.
