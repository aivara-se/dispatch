# 002 The engine reads a closed condition set

## Context

A flow names what must hold before a job advances. A free expression would name something the engine cannot read, or ask it to trust the agent.

## Decision

- A `next` key is one of seven conditions: `pr.created`, `pr.review_requested`, `pr.assigned`, `pr.approved`, `pr.rejected`, `pr.updated`, `pr.merged`.
- A reviewer's verdict is their latest review on the head. A comment and a pending review leave the last verdict standing; a dismissed review leaves the reviewer with none.
- `_start_` reads its keys on each delivery, and names only what a delivery proves: `pr.created`, `pr.review_requested`, `pr.assigned`, `pr.approved` and `pr.rejected`.
- A start names the two login conditions or neither; the loader refuses a flow that names both kinds.
- Every other task reads its keys after it ends, in the order written, and the first that holds wins.
- A key that names no condition fails the configuration load.
- Reaching `_done_` is success; any other stop fails the job.

## Consequences

Adding a condition is a decision, not a configuration change. A flow cannot express a condition the engine does not implement, and the engine stays the only reader of outcomes.
