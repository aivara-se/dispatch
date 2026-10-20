# 012 - An Approval That Lands Nothing Wakes the Author

## Context

The fleet arms every pull request for auto-merge as it opens, and the ruleset lands an armed pull request on the approving review (`.agents`, pull request 18, `skills/coding/SKILL.md`, *The pull request*, read 2026-10-20). The approval is the merge; there is no click after it.

An approval on a pull request that was never armed — or was disarmed and left open — is a change everyone believes is landing and that nothing will move. Nothing reports it, because nothing happened: no delivery fires, and the author has moved on to the next task.

`pull_request_review` with action `submitted` carries the whole pull request object, and it answers the question without an API call. `auto_merge` is the object when auto-merge is armed and `null` when it is not, and `state` and `draft` say whether the pull request is one the ruleset could land (`octokit/webhooks`, `payload-schemas/api.github.com/common/pull-request.schema.json`, read 2026-10-20).

## Decision

- **One delivery acts: `pull_request_review` with action `submitted`, review state `approved`, on a pull request that is `open`, not a draft, and carries `auto_merge` null.**
- **The wake target is the pull request's author**, `pull_request.user.login`, matched against the configured agents. The delivery's own actor is never woken.
- **An approval on an armed pull request wakes nobody**: the ruleset lands it, or it waits on a review or a check that has its own wake ([`006`](006-the-review-loop.md), [`010`](010-a-failing-check-wakes-the-pusher.md)).
- **The reason is identifiers**: `Dispatch: approved but not armed on <repository>#<number>`.
- **No count, no API, no credential, no state.**

## What this overrides

[006](006-the-review-loop.md)'s "A review that approves or only comments wakes nobody" is narrowed: an approval on an armed pull request still wakes nobody, and an approval on an unarmed one wakes the author. Everything else in `001`–`011` stands, including `006`'s loop, `007`'s cap and `002`'s no-API rule.

## Consequences

- **A deliberately parked pull request wakes its author when someone approves it.** The wake says approved-but-unarmed, which is true, and arming or closing it is one command; staying silent is the failure this record exists to catch.
- **An armed pull request that stalls still wakes nobody.** Its branch can be behind the default, a required check can be pending, and neither produces a delivery. Reaching that state needs a clock, and the service has none; this record does not close it.
- **An approval while a required check is still pending wakes nobody**, because the pull request is armed; if that check fails, [`010`](010-a-failing-check-wakes-the-pusher.md) wakes the pusher.
- **An approval of a draft wakes nobody**, because a draft is not a change the ruleset could land; arming it is refused too, so the two failures are the same one.
- **A stale payload cannot invent the wake**: `auto_merge` is the pull request's own field at the moment of the review.
