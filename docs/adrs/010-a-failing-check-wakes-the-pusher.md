# 010 - A Failing Check Wakes the Pusher

## Context

A pull request with red checks is the commonest silent stall in the fleet: the author pushed, the run failed, and nothing wakes anyone until a reviewer happens to look. v1.3 leaves it out ([PRODUCT](../PRODUCT.md), *Edge cases v1.3 leaves unhandled*).

The target everyone names first is the pull request's author, and the payload does not carry it. `workflow_run.pull_requests[]` names each pull request by number and by head and base refs only, with no author (`octokit/webhooks`, `payload-schemas/api.github.com/common/workflow-run.schema.json`, read 2026-10-20). Reaching the author would take a GitHub API read and a credential, which [002](002-github-webhook-ingestion.md) rules out and which this service has never held.

The payload does carry the run's `actor`: the account that triggered it, which for a pushed commit is the account that pushed. In this organisation the author is the only one who pushes to a pull request's own branch — a reviewer never pushes to a peer's branch ([PRODUCT](../PRODUCT.md), *The flow: review and update*) — so the pusher is the author in everything but name.

## Decision

- **One event acts: `workflow_run` with action `completed`, conclusion `failure`, and at least one entry in `workflow_run.pull_requests`.** The first entry's number is the pointer the reason carries. `success`, `cancelled`, `timed_out`, `action_required`, `stale` and `skipped` wake nobody.
- **The wake target is `workflow_run.actor`**, matched against the configured agents like any other login. An actor the configuration does not carry wakes nobody.
- **The actor rule is overridden for this event, and only this event.** "No agent is woken for the event it made" rests on the maker already knowing what happened; a push does not tell the pusher that the checks failed, so the fact is news to the one agent that can act on it.
- **A cap of five wakes per pull request**, keyed by the repository and the number, held in memory, checked before the wake and advanced by each failing run that wakes someone (provisional; the first live week freezes the number). At five, a further failure wakes nobody and the audit line says the cap was reached. The shape is [007](007-the-review-cap.md)'s.
- **The reason is identifiers**: `Dispatch: checks failed on <repository>#<number>`.
- **No API, no credential, no durable state.** The organisation webhook gains one subscription, `workflow_run`.

## What this overrides

[002](002-github-webhook-ingestion.md)'s "the delivery's own actor is never woken" is overridden for this event alone, and for the reason above: a failed check is news to the pusher. Its "one event acts" is widened here as `006` and `008` widened it before. [007](007-the-review-cap.md)'s "One count per pull request" gains a second counter on the same key — a failed check is not a review round, and one budget for both would let CI spend the review cap — with its own limit. Everything else in `001`–`008` stands, including `002`'s "the service calls no API" and `003`'s "no tracking".

## Consequences

- **An agent that pushes a fix and fails again is woken again**, up to five times per pull request. The fifth failure is the last that starts a run, so a genuinely hard break stops reaching the agent and becomes the operator's.
- **A flaky test spends the budget** as surely as a real break, and the count does not know the difference.
- **The wake goes to the pusher, not to the author.** If the operator pushes a commit to an agent's branch, the actor is the operator, nobody is woken, and the agent is not told. That is the cost of doing it without an API call, and it is the case the alternative would have covered.
- **A pull request from a fork wakes nobody**: its runs are triggered by the forker, who is not a configured login.
- **A failing run that names several pull requests wakes once**, naming the first; one run is started, not one per pull request.
- **The count dies with the process**, as the review cap does: a restart grants a fresh five.
