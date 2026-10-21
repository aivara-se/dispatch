# 017 - A Dismissed Review Wakes the Author

## Context

`012` wakes the author when an approval lands nothing, because an approval is the merge and an unarmed change lands nothing. An approval can also stop existing: a maintainer dismisses it, or GitHub dismisses a stale approval when new commits arrive on the branch — which is what a rebase that resolves conflicts produces. The change then cannot land, the author is not told, and the next wake is an unrelated one. A changes-requested review is dismissed through the same action, and the delivery does not separate the two: `review.state` reads `dismissed` whatever verdict was removed.

The payload answers it without an API call: `pull_request_review` with action `dismissed` carries `review.state`, the pull request as `simple-pull-request` — its `user`, `state`, `draft` and `merged_at` — and the `sender` who dismissed it (`octokit/webhooks`, `payload-schemas/api.github.com/pull_request_review/dismissed.schema.json` and `common/simple-pull-request.schema.json`, read 2026-10-21).

## Decision

- **One delivery acts: `pull_request_review` with action `dismissed`.**
- **The wake target is `pull_request.user.login`**, matched against the configured agents like the author in `006` and `012`.
- **It acts only on a pull request that could have landed**: `state` open, `draft` false, `merged_at` null. A dismissal on a draft, or on a closed or merged pull request, wakes nobody.
- **The actor rule is overridden for this event, as [`010`](010-a-failing-check-wakes-the-pusher.md) overrides it for a failing check.** The push that dismisses a stale approval is the author's own — [`010`](010-a-failing-check-wakes-the-pusher.md) records that the author is the only account that pushes to a pull request's own branch — so excluding the sender would silence the case this record exists for. A push does not tell the pusher that the approval is gone; the delivery does.
- **The reason is identifiers**: `Dispatch: review dismissed on <repository>#<number>`.
- **No round and neither cap**: a dismissal is not a review, so it neither advances nor consults the ten ([`007`](007-the-review-cap.md)) or the five ([`010`](010-a-failing-check-wakes-the-pusher.md)).
- **No API, no credential, no durable state, no new subscription.**

## What this overrides

[002](002-github-webhook-ingestion.md)'s "One event acts" set is widened by one action, as every record since `006` has widened it, and its "the delivery's own actor is never woken" falls for this event as it falls in [`010`](010-a-failing-check-wakes-the-pusher.md). `012`'s conditions on an *approval* stand and are reused unchanged, `006`'s "an approval spends no round" holds for a dismissal, the roster match stands, and the no-API consequence still holds.

## Consequences

- The author learns at the moment the change loses its review, which after a rebase is the moment they still have the branch in hand — and it is the only notice: the push that caused it returns no error.
- **Every dismissal wakes the author, wherever it came from**: their own rebase, a maintainer clearing a stale review, or GitHub's staleness rule. The payload says neither which verdict was removed nor who removed it, so a dismissed *rejection* wakes the author too — a run spent on a change that was not waiting on an approval — and the reason says only that a review was dismissed.
- **One push that dismisses two approvals wakes the author twice.** The delivery id is the unit of dedupe; the review is the unit of a dismissal.
- A dismissal on a draft wakes nobody, by the same reasoning as an approval on one: a draft could not have landed.
- An armed pull request is woken too: its auto-merge now has no approval to wait on, so the change will not land until a reviewer approves again. `012` excludes armed pull requests because there the approval had already done its work; here it no longer exists.
- An author already working on a fix is woken to re-read a change they are mid-way through; the wake is a reason and costs one run.
