# 017 - A Dismissed Review Wakes the Author

## Context

`012` wakes the author when an approval lands nothing, because an approval is the merge and an unarmed change lands nothing. An approval can also stop existing: a maintainer dismisses it, or GitHub dismisses a stale approval when new commits arrive on the branch — which is what a rebase that resolves conflicts produces. The change then cannot land, the author is not told, and the next wake is an unrelated one.

The payload answers it without an API call: `pull_request_review` with action `dismissed` carries `review.state`, the pull request as `simple-pull-request` — its `user`, `state`, `draft` and `merged_at` — and the `sender` who dismissed it (`octokit/webhooks`, `payload-schemas/api.github.com/pull_request_review/dismissed.schema.json` and `common/simple-pull-request.schema.json`, read 2026-10-21).

## Decision

- **One delivery acts: `pull_request_review` with action `dismissed`.**
- **The wake target is `pull_request.user.login`**, matched against the configured agents like the author in `006` and `012`.
- **It acts only on a pull request that could have landed**: `state` open, `draft` false, `merged_at` null. A dismissal on a draft, or on a closed or merged pull request, wakes nobody.
- **The delivery's own sender is never woken.**
- **The reason is identifiers**: `Dispatch: approval dismissed on <repository>#<number>`.
- **No round and neither cap**: a dismissal is not a review, so it neither advances nor consults the ten ([`007`](007-the-review-cap.md)) or the five ([`010`](010-a-failing-check-wakes-the-pusher.md)).
- **No API, no credential, no durable state, no new subscription.**

## What this overrides

[002](002-github-webhook-ingestion.md)'s "One event acts" set is widened by one action, as every record since `006` has widened it. Nothing else falls: `012`'s conditions on an *approval* stand and are reused unchanged, `006`'s "an approval spends no round" holds for a dismissal, and the actor rule, the roster match and the no-API consequence all still hold.

## Consequences

- The author learns at the moment the change loses its approval, which after a rebase is the moment they still have the branch in hand.
- **One push that dismisses two approvals wakes the author twice.** The delivery id is the unit of dedupe; the review is the unit of a dismissal.
- The payload does not say whether a person dismissed the review or GitHub dismissed it as stale, so the reason says the approval is gone and not why.
- A dismissal on a draft wakes nobody, by the same reasoning as an approval on one: a draft could not have landed.
- An armed pull request is woken too: its auto-merge now has no approval to wait on, so the change will not land until a reviewer approves again. `012` excludes armed pull requests because there the approval had already done its work; here it no longer exists.
- An author already working on a fix is woken to re-read a change they are mid-way through; the wake is a reason and costs one run.
