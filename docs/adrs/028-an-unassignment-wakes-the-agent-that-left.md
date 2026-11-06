# 028 - An Unassignment Wakes the Agent That Left

## Context

[008](008-issue-assignment.md) acts on `issues` with action `assigned`, and it names `unassigned` among the actions it accepts and ignores; [009](009-every-assignment-wakes-its-agent.md) repeats that list. So a person who removes an agent from an issue tells it nothing, and the agent keeps working on work that is no longer its own.

The event costs nothing to reach. GitHub documents `issues` with action `unassigned`, "a user was unassigned from an issue", and its payload carries `assignee`, the login that left (*Webhook events and payloads*, docs.github.com, read 2026-11-06) — the field `assigned` already reads. The wake needs no API call, no credential and no state: it is the `assigned` path with the other sign.

## Decision

- **One delivery more acts: `issues` with action `unassigned`**, waking `assignee.login`.
- **008's rules govern it**: the payload's `assignee` and never its `assignees` array, the roster match against the configured agents, the actor exclusion, the identifiers-only reason, and no count, API call, credential or state.
- **The reason gains one sentence**: `Dispatch: issue unassigned on <repository>#<number>`.
- **Every other action stays ignored**, `pull_request` with action `unassigned` among them.

## What this overrides

[008](008-issue-assignment.md) says "Every other `issues` action — `opened`, `edited`, `unassigned`, `labeled`, `closed` and the rest — is accepted and ignored", and [009](009-every-assignment-wakes-its-agent.md) lists `unassigned` again among the actions that stay ignored. Each clause stops covering `unassigned`, and the other actions it names stay ignored. Everything else in `002`–`009` stands.

## Consequences

- **The agent that leaves is told once, by the delivery.** It reads nothing to learn why, and the audit line names the profile and the reason, as the assignment's line does.
- **Anyone who can assign can wake an agent by removing it.** Triage permission or above reaches both gestures, and the login that leaves is the one woken.
- **The actor rule silences the agent that unassigns itself.** It made the delivery, so no run starts, and the audit line says why.
- **A reassignment wakes both logins.** Where GitHub reports one as an `unassigned` and an `assigned`, the agent that left and the agent that took the issue each get one run, and nothing joins the two.
- **A pull request's unassignment stays silent.** The gesture wakes an agent on an issue and not on a pull request; `009` closed that gap for the assignment and this record does not close it here. The gap is named, not repaired.
- **One condition and one sentence.** The delivery set grows by one, and no stored state, dependency or subscription is added.
