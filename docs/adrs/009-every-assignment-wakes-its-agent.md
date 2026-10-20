# 009 - Every Assignment Wakes Its Agent

## Context

[008](008-issue-assignment.md) acts on `issues` with action `assigned`. [PRODUCT](../PRODUCT.md) (*Edge cases v1.3 leaves unhandled*) lists two assignment-shaped gestures it does not reach: an issue created with an assignee already on it, and a pull request assigned.

The first turns out not to need reaching. GitHub delivers `assigned` for the assignment that creation performs, after `opened`. A repository webhook subscribed to `issues` and `pull_request` was given an issue created with an assignee and a pull request created with an assignee; it recorded `issues` `opened` then `issues` `assigned`, and `pull_request` `opened` then `pull_request` `assigned` (observed 2026-10-20). The `opened` delivery is not the assignment, and acting on it would wake the agent for work the `assigned` delivery is about to announce anyway.

The second is real: assigning a pull request delivers `pull_request` with action `assigned`, which no record acts on. The same gesture wakes an agent on an issue and not on a pull request.

## Decision

- **One delivery more acts: `pull_request` with action `assigned`**, waking `assignee.login` — the field `issues`/`assigned` already reads. A pull request does not deliver on the `issues` event, so it needs its own branch.
- **`issues` with action `opened` stays ignored**, because the assignment it carries arrives as its own `assigned` delivery. Acting on both would wake the agent twice for one gesture.
- **Every other action stays ignored**: `edited`, `labeled`, `unassigned`, `closed`, `reopened`, and a pull request's other actions.
- **008's rules govern the wake**: the roster match, the actor exclusion, the identifiers-only reason, and no count, no API call, no credential and no new state.
- **The reason gains one sentence**: `Dispatch: pull request assigned on <repository>#<number>`, so a reason says which kind it points at. `Dispatch: issue assigned on <repository>#<number>` is unchanged and covers an issue created with its assignee, because that arrives as `assigned`.
- **No new subscription**: the organisation webhook already carries `pull_request`.

## What this overrides

[002](002-github-webhook-ingestion.md)'s "one event acts" is widened by one delivery, as `006`, `008` and `010` widen it. Nothing else falls: `008`'s "Every other `issues` action — `opened`, `edited`, `unassigned`, `labeled`, `closed` and the rest — is accepted and ignored" still holds, `opened` among them, and so do its actor rule, its roster match and its consequence that the flow adds no state.

## Consequences

- **An issue created with an assignee already wakes its agent**, and did before this record: the `assigned` delivery is the one that acts. The gesture's two deliveries are not a duplicate wake, and no code was needed to make them one.
- **A pull request created with an assignee wakes its agent once**, by the same route: `opened` is ignored and `assigned` acts.
- **Several assignees in one change remain unhandled**: only the payload's `assignee` is read, never its `assignees` array, and whether GitHub reports such a change as one delivery per assignee is unrecorded here.
- **Anyone who can assign can start a run** — triage permission or above — for a pull request as well as for an issue.
- **One wake per assignee**, and nothing is held back.
