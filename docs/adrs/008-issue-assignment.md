# 008 - An Issue's Assignment Wakes Its Agent

## Context

v1.2 wakes an agent only from a pull request: the reviewer a delivery asks for, and the author whose review requests changes ([006](006-the-review-loop.md), [007](007-the-review-cap.md)). Work that is not a pull request — an issue the operator hands to an agent — has no way to start a run. v1.3 adds that path and keeps it as small as the ones before it: the assignment is the trigger, the service still calls no API, keeps no count, and reads no board.

What needs recording is which delivery acts, where the wake target comes from, and what the choice leaves out.

## Decision

- **A third delivery acts: `issues` with action `assigned`.** Every other `issues` action — `opened`, `edited`, `unassigned`, `labeled`, `closed` and the rest — is accepted and ignored. GitHub documents the action and the payload's `assignee` object (*Webhook events and payloads*, *issues*, docs.github.com, read 2026-10-20).
- **The wake set is the delivery's `assignee`.** Its login is matched against the configured agents, each a GitHub login beside the local profile it runs as; the match names the profile to wake. The payload's `assignees` array is not read, and a login the configuration does not carry wakes nobody.
- **The delivery's own actor is never woken** — the rule [002](002-github-webhook-ingestion.md) fixed for the reviewer and [006](006-the-review-loop.md) kept for the author. An agent that assigns an issue to itself starts no run.
- **The reason is identifiers**: `Dispatch: issue assigned on <repository>#<number>`, in the service's own words — never a title, a body, a label or any other text a third party wrote ([002](002-github-webhook-ingestion.md)).
- **Nothing else changes.** The assignment adds no count, no label, no GitHub API call, no credential and no state of its own: the delivery-id cache the receiver already keeps is all of it, and a wake still costs one child process and no bookkeeping ([003](003-starting-a-run.md)).
- **The delivery is accepted and logged.** `202` whether or not an agent was woken, and the audit line says which profile started and, when not, why ([005](005-the-audit-record.md)).

## What this overrides

[002](002-github-webhook-ingestion.md) decided that one event acts — `pull_request` with action `review_requested` — and [006](006-the-review-loop.md) had already widened that set once, for the changes-requested review. This record widens it again: `002`'s "One event acts" no longer describes the service, and the "one flow" framing its successors inherited goes with it.

It also parts company with two smaller clauses: [005](005-the-audit-record.md)'s field list names the pull request number, where a line now carries the issue's number for an `issues` delivery, and [003](003-starting-a-run.md)'s Context says the service carries one flow. Everything else in `001`–`007` stands, including `002`'s "the service calls no API", `003`'s "no serialisation" and "no tracking", and `006`'s "Nothing else changes".

## Consequences

- **An assignment is a wake with nothing behind it.** The service does not know whether the issue is a card, what column it sits in, or whether the agent it wakes is the right one. Assignment is a deliberate act by a person or an agent, and the service takes it as the reason; judging the work is the board's and the agents' business, not this service's.
- **The cases left out are listed, not implied** ([PRODUCT](../PRODUCT.md), *Edge cases v1.3 leaves unhandled*): an issue created already assigned, several assignees in one change, a pull request's assignment, a reassignment away, an assignment made outside GitHub, a second run while the agent is busy, and a duplicate after a restart. Each is a wake that does not happen or a run that happens twice, and none of them is repaired.
- **The second flow costs one branch in the receiver, not a second mechanism.** The same verification, dedup, match, spawn and audit serve it; no state, dependency or credential is added.
- **The organisation webhook must subscribe to `issues`.** A webhook left on the v1.2 subscription receives no assignment, and the first live test is what shows it; the subscription is the operator's, not the service's ([SYSTEMS](../SYSTEMS.md), *GitHub access*).
- **The reason's third sentence is a new fixed string.** An agent that pattern-matches the two review reasons learns one more phrase; nothing about the review flow's wording changes.
