# 021 - A Review's Mention Is Trusted by Its Author

## Context

`016` reads a mention in a submitted review's body and in an inline review comment, under `011`'s guard: only an `author_association` of `OWNER`, `MEMBER` or `COLLABORATOR` may summon an agent.

On those two surfaces GitHub reports that field differently from an issue comment. For accounts whose `issue_comment` payload says `MEMBER`, the `pull_request_review` and `pull_request_review_comment` payloads say `CONTRIBUTOR`, and in one case `NONE` — dumped from an Actions run, while `gh api orgs/aivara-se/memberships/<login>` says `member`, `active`. The guard is therefore always refused there, and a mention in a review body or in an inline comment has never woken anyone. v1.11 is inert on the surface it added.

The payload carries the identities to judge instead: `review.user` and `comment.user` (the text's author) and `pull_request.user` (the pull request's author) (`common/pull-request-review.schema.json`, `common/pull-request-review-comment.schema.json` and `common/simple-pull-request.schema.json`, read 2026-10-21). No API call is needed.

`pull_request.requested_reviewers` does not hold one of them. GitHub drops a reviewer from that set the moment they submit, so a submitted review's author is never in it: on `aivara-se/dispatch#143`, the review by `thani-sh-mama` stands and `reviewRequests` is `thani-sh` alone (docs.github.com/en/rest/pulls/review-requests, live 2026-10-21). The one set that survives a submission is the logins the service is configured to run, which it already holds.

## Decision

- **On `pull_request_review` submitted and `pull_request_review_comment` created, a mention is accepted when the text's author**
  - has an `author_association` of `OWNER`, `MEMBER` or `COLLABORATOR`, as before, **or**
  - is the pull request's author, **or**
  - is one of the logins the service is configured to run — the fleet itself.
- **An `issue_comment` keeps `011`'s guard alone**: its association is the fact that rule was written for.
- **Nothing else changes**: the bounded `@login` match, the reason `Dispatch: mentioned on <repository>#<number>`, the self-actor rule, one run per agent per delivery, and the review's own targets beside the mentions.
- **No count, no API, no credential, no state, no new subscription.** Both identities are in hand at the decode: the author from the review or the comment, the configured logins from the configuration.

## What this overrides

`016`'s "the association guard ... unchanged" — on the two surfaces it added, and only by widening. Its matcher, its reason, its two-targets-one-line rule and its no-state consequence stand. `011` stands for comments, unchanged.

## Consequences

- The fleet's own review conversation — the finding on a line, the verdict in the body — now reaches the agent it names, which is the case `016` exists for.
- The association stops being the only fact the guard tests on those surfaces; it is one of three.
- **A stranger can summon an agent by opening a pull request and mentioning it in a review of their own change**, because the author branch admits them, and the repositories are public. The payload carries `pull_request.author_association`, which would refuse that case; that clause is not taken here.
- A configured agent can summon a peer from its own review, which is the fleet's own conversation and the case `016` exists for. A login dropped from the configuration stops summoning from those surfaces, since the configuration is the fact the branch tests.
- The reason and the log are unchanged, so an audit line still says an agent was woken and never why the author was trusted.
