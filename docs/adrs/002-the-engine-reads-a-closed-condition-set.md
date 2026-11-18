# 002 The engine reads a closed condition set

## Context

A flow names what must hold before the job advances. If a flow could name any expression, the engine would either evaluate something it cannot read or trust the agent. The set must be small, readable on GitHub and shared by every flow.

## Decision

- A `next` key is one of five conditions:
  - `pr.created` — the delivery opens a pull request.
  - `pr.approved` — the last review submitted on the current head is an approval.
  - `pr.rejected` — the last review submitted on the current head requests changes.
  - `pr.updated` — the head is not the head the task started on.
  - `pr.merged` — the pull request is merged.
- `_start_` reads its keys on each delivery; every other task reads them after it ends.
- The keys are tried in the order written; the first that holds wins.
- A key that names no condition fails the configuration load.
- Reaching `_done_` is success; any other stop fails the job.
- A closed pull request ends the job; unmerged, it is blocked and failed.
- The read that decided a condition is stored as the task's evidence.

## Consequences

Adding a condition is a decision, not a configuration change. A flow cannot express a condition the engine does not implement. The engine stays the only reader of outcomes.
