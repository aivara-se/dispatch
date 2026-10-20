# 009 - Every Assignment Wakes Its Agent

## Context

[008](008-issue-assignment.md) acts on one delivery: `issues` with action `assigned`. Two assignment-shaped cases still wake nobody, and both are listed as deliberately unhandled in [PRODUCT](../PRODUCT.md) (*Edge cases v1.3 leaves unhandled*): an issue created with an assignee on it, whose delivery is `opened`, and a pull request assigned, which arrives as `pull_request` with action `assigned` rather than as `issues`. The second is the odder of the two, because the gesture is the same one — hand an agent a piece of work by assigning it — and it wakes the agent on an issue and not on a pull request.

GitHub documents the `assignees` array on an issue and the `assignee` object on both events (`Webhook events and payloads`, docs.github.com, read 2026-10-20).

## Decision

- **`issues` with action `opened` acts when the issue carries assignees.** Every configured login in `issue.assignees` is woken, and the delivery's actor is not.
- **`pull_request` with action `assigned` acts**, waking `assignee.login`, the same field `issues`/`assigned` reads. A pull request does not deliver on the `issues` event, so it needs its own branch.
- **Every other action stays ignored**: `edited`, `labeled`, `unassigned`, `closed`, `reopened`, and a pull request's `opened`.
- **008's rules still govern the wake.** The login is matched against the configured agents, the actor is never woken, the reason is identifiers and the service's own words, and there is no count, no API call, no credential and no new state.
- **The fixed sentences grow by one.** `Dispatch: issue assigned on <repository>#<number>` covers an issue that arrives assigned as well as one assigned later, and `Dispatch: pull request assigned on <repository>#<number>` covers the pull request, so a reason says which of the two it points at.
- **No new subscription.** The organisation webhook already carries `issues` and `pull_request`.

## What this overrides

[008](008-issue-assignment.md)'s "Every other `issues` action — `opened`, `edited`, `unassigned`, `labeled`, `closed` and the rest — is accepted and ignored" no longer holds: `opened` acts when the issue carries assignees. So does its "The payload's `assignees` array is not read", which was true of `assigned` alone. Everything else in `008` stands — the roster match, the actor rule, the reason's shape, and the absence of count, API and state — and so does `002`'s "the service calls no API".

## Consequences

- **An issue created with an assignee can wake the agent twice.** If GitHub delivers both `opened` — which carries the assignees — and `assigned` for the same act, the two deliveries have different ids and the dedup cache cannot see them as one; the agent runs twice over the same issue. That is the trade the service already makes for two review requests on one pull request — a wake is a reason, and a duplicate is harmless — and it is not repaired here.
- **Anyone who can assign can start a run**, at creation as well as after it. Assigning takes triage permission or above, so that was already true of `assigned`; `opened` widens it by one gesture and no further.
- **One wake per assignee.** A card handed to two agents starts two runs, one each, and neither is held back.
- **A pull request's assignment reads as a pull request in the reason**, so an agent that matches the wording is not told to look for an issue.
