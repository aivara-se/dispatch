# 016 - A Mention Reads the Review's Own Surfaces

## Context

`011` reads one event: `issue_comment` with action `created`. The fleet's review conversation is written elsewhere — a submitted review's body carries the findings and the verdict, and an inline review comment carries the finding on the line it is about. A mention in either wakes nobody, so the one place an agent is asked a direct question during a review is the one place the mention cannot reach.

Both payloads answer everything the matcher needs without an API call: `review.body` with `review.author_association`, and `comment.body` with `comment.author_association` (`octokit/webhooks`, `payload-schemas/api.github.com/common/pull-request-review.schema.json` and `common/pull-request-review-comment.schema.json`, read 2026-10-21).

## Decision

- **Three deliveries feed the mention path**: `issue_comment` created, as now; `pull_request_review` submitted, whatever its state; and `pull_request_review_comment` created.
- **The matcher, the bounded `@login` match, the association guard, the self-actor rule and the reason are unchanged**, read from the review's or the comment's own `author_association` and `body`. Only `OWNER`, `MEMBER` and `COLLABORATOR` may summon an agent, and the reason stays `Dispatch: mentioned on <repository>#<number>`.
- **A review that requests changes has two targets in one delivery**: the author (`006`), and any configured login its body names. Both are recorded on one line, as several mentions already are.
- **No count, no API, no credential, no state.** The organisation webhook already carries `pull_request_review` and gains `pull_request_review_comment`.

## What this overrides

`011`'s "One event acts: `issue_comment` with action `created`", and, when the code lands, PRODUCT's edge bullet that a mention in a review's body wakes nobody. Its matcher, its guard, its reason and its no-state consequence stand unchanged, and so does its rule that no third-party text reaches the reason or the log.

## Consequences

- A review's body and each of its inline comments are separate deliveries, so one review naming an agent in both starts two runs. The delivery id is the unit of dedupe and a comment is the unit of a mention.
- A review carrying several inline comments that each name the same agent starts a run per comment, as three comments on an issue already do.
- Only `created` and `submitted` act: an edited review or comment wakes nobody, and a deleted one cannot recall a run.
- The match stays a substring comparison, so an inline comment quoting a sentence about an agent still matches.
