# 013 - A Draft Marked Ready Wakes Its Reviewers

## Context

The fleet opens probes, spikes and anything that must not merge as drafts, and never arms them (`.agents`, pull request 18, `skills/coding/SKILL.md`, *The pull request*, read 2026-10-20). A draft that is ready becomes a pull request that may land, and that transition is its own delivery — `pull_request` with action `ready_for_review` — which nothing acts on.

The reviewers were requested when the draft opened, so the wake they got was for something they had been told not to merge. The wake they need is this one: the change is now a candidate.

The pull request object in the delivery names the reviewers still requested, as users and teams (`octokit/webhooks`, `payload-schemas/api.github.com/common/pull-request.schema.json`, read 2026-10-20).

## Decision

- **One delivery acts: `pull_request` with action `ready_for_review`.**
- **The wake set is every configured login in `pull_request.requested_reviewers`.** A team entry is not resolved — that would take an API call — and wakes nobody. The delivery's own actor is never woken.
- **The reason is identifiers**: `Dispatch: ready for review on <repository>#<number>`.
- **No count, no API, no credential, no state.**

## What this overrides

The event set [002](002-github-webhook-ingestion.md) opened is widened by one action, as every record since [`006`](006-the-review-loop.md) has widened it. Nothing else falls: the roster match, the actor rule and the no-API consequence all still hold.

## Consequences

- **A draft marked ready before anyone is requested wakes nobody**, which is right: requesting the review is the act that wakes, and it is one command.
- **A reviewer already woken when the draft opened is woken again.** The first wake said a draft asked for a review; this one says it is a candidate — the second is the one that matters.
- **A team reviewer stays silent**, as it does in every flow.
- **Toggling a pull request between draft and ready wakes the reviewers each time it becomes ready.** Only the delivery-id cache dedupes, and two toggles are two deliveries.
- **The wake does not say what changed in the pull request.** The agent re-reads it, as every wake requires.
