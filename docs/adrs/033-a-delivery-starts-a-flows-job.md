# 033 A delivery starts a flow's job

## Context

GitHub posts deliveries. A flow names the condition that starts it in `_start_.next`. The engine must turn a delivery into jobs without asking an agent.

## Decision

- The App subscribes once to `pull_request` and `pull_request_review`; no flow names an event, and no config adds one.
- The engine verifies the signature, dedupes by delivery id, and resolves the pull request.
- A flow's `_start_.next` decides which deliveries act: `pr.created` from opened; `pr.approved` and `pr.rejected` from a submitted review; `pr.merged` from a closed, merged pull request.
- `pull_request.closed` is always handled: every job ends when its pull request closes.
- `pr.updated` is read after a task, never on a delivery; a start that names it starts nothing.
- For each flow, the engine evaluates `_start_.next`. Each condition that holds starts a job for `flow/repo/pr`, unless that flow already has a job there.
- A delivery that names no pull request, or matches no flow, starts nothing.
- A delivery for a flow that already has a job on the pull request starts no second job; the job's conditions are read after each task. Jobs of different flows run in parallel.
- A closed pull request ends every job for it and kills a running action.
- The engine does not poll; deliveries are the only trigger. State lives in the local SQLite record, so a restart resumes what is unfinished.

## Consequences

A new flow needs no new webhook event unless it starts on something other than a pull request. A delivery missed while the engine is down is not replayed; the record keeps what arrived.
