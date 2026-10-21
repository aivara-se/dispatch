# 020 - A Dismissal Wakes the Author Who Caused It

## Context

`017` decides that a dismissal wakes the pull request's author, and then excludes the delivery's own sender. In this organisation the sender and the author are the same account exactly where it matters: a push does not tell the pusher that a review has gone, and a rebase is what dismisses a stale approval — so the excluded delivery is the one the version exists for. [`010`](010-a-failing-check-wakes-the-pusher.md) already relies on the same fact from the other side: the author is the only account that pushes to a pull request's own branch.

The payload also does not carry the verdict that was removed. `pull_request_review` with action `dismissed` reads `review.state` as `dismissed` whether an approval or a changes-requested review was dismissed (`octokit/webhooks`, `payload-schemas/api.github.com/pull_request_review/dismissed.schema.json`, read 2026-10-21), so a reason that says *approval* names something the delivery does not state.

## Decision

- **`017`'s "The delivery's own sender is never woken" falls.** The event wakes the author even when the author is the sender.
- **The reason is `Dispatch: review dismissed on <repository>#<number>`**, naming the review and not a verdict.
- **Everything else in `017` stands**: the target is `pull_request.user.login`, the conditions are open and not a draft and not merged, and it spends no round, consults neither cap, calls no API and adds no subscription.

## What this overrides

`017`'s sender clause and its reason, and nothing else. [002](002-github-webhook-ingestion.md)'s "the delivery's own actor is never woken" falls for this event as it falls for a failing check in [`010`](010-a-failing-check-wakes-the-pusher.md), and for the reason `010` gives: the fact is news to the one agent that can act on it. `017`'s *What this overrides* says the actor rule still holds; that sentence falls with the clause it described. `001`–`019` otherwise stand.

## Consequences

- **The author learns at the moment the change loses its review**, which after a rebase is the moment they still have the branch in hand — and it is the only notice: the push that caused it returns no error.
- **A dismissal the author did not cause wakes them too**: a maintainer clearing a stale review, or GitHub's own staleness rule.
- **A dismissed rejection wakes a run that was not waiting on an approval.** The payload does not separate the two, so every dismissal reaches the author.
- **A dismissal by a peer wakes the author, not the peer**: the target is unchanged, and only the maker of the delivery stops being excluded.
- **One push that dismisses two reviews wakes the author twice.** The delivery id is the unit of dedupe; the review is the unit of a dismissal.
