# 011 - A Mention Wakes the Agent It Names

## Context

Three flows now start from a GitHub gesture: a review request, an assignment, a failing check. The channel left is prose — a comment asking an agent for something — which today wakes nobody unless a review is requested or the issue is reassigned.

`issue_comment` with action `created` carries the comment's body and its author, the author's association with the repository, and the issue it belongs to, which is a pull request where `issue.pull_request` is present (`octokit/webhooks`, `payload-schemas/api.github.com/issue_comment/created.schema.json` and `common/issue-comment.schema.json`, read 2026-10-20).

The repositories are public, so a mention is a wake any GitHub account could aim at an agent that holds tools, credentials and a sandbox. The payload answers that without an API call: `comment.author_association`.

## Decision

- **One event acts: `issue_comment` with action `created`**, on an issue or on a pull request. `edited` and `deleted` do not act.
- **The wake set is the exact `@login` of each configured agent found in the body.** The match is case-insensitive and bounded: the `@` is not preceded by a login character, and the login is not followed by one (`A-Za-z0-9-`), so `@thani-sh-mama.` matches and `@thani-sh-mama-2` does not.
- **Only the owner, a member or a collaborator may summon an agent**: `comment.author_association` must be `OWNER`, `MEMBER` or `COLLABORATOR`. Any other comment wakes nobody, whatever it says, and the audit line says why. `CONTRIBUTOR` means a past contributor to a repository, not a member of the organisation, and is not enough.
- **The actor is never woken**: an agent that mentions itself starts nothing.
- **Each configured agent named in one comment is woken once**, however often the body names it.
- **No count, no API, no credential, no state.** The organisation webhook gains one subscription, `issue_comment`.
- **The reason is identifiers**: `Dispatch: mentioned on <repository>#<number>`.

## What this overrides

[002](002-github-webhook-ingestion.md)'s "One event acts … so is every other event, action and repository" is widened here, as `006`, `008` and `010` widened it. Nothing else in `002` falls: the body is read only against a known set — the configured logins — which is the rule `002` already sets for every field a decision uses, and no text from it reaches the reason or the log.

## Consequences

- **A stranger's mention is inert**, and costs a string comparison: the association is read from the payload, not looked up.
- **The body is read only to find a configured login.** No other text reaches the reason and none is logged ([002](002-github-webhook-ingestion.md), [005](005-the-audit-record.md)).
- **A mention inside a code block, a quotation or a sentence about an agent still matches.** The service does not parse Markdown; a false wake it accepts is smaller than a parser it would own.
- **An edited comment does not act**, because only `created` is read, and a comment deleted after it woke an agent cannot recall the run.
- **A comment naming three agents starts three runs**, and nothing holds one back.
- **A member's ordinary sentence is a wake only when it names a login**, so conversation about an agent, without the `@`, still reaches nobody.
