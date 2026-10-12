# 009 - The Reason Carries No Text a Third Party Wrote

## Context

A wake is a reason: `hermes --profile <bot> chat -q "<reason>"`. That reason is an argument to a program started on the operator's host, holding the tools, credentials and sandbox the profile gives it (observed on the host, 2026-10-12). Everything that would make a reason vivid — an issue title, a comment body, a pull request body, a branch name — is text a third party wrote, and anyone who can open an issue in the organisation is a third party. [002](002-github-webhook-ingestion.md) fixes the rule for the request path — no field becomes a path, a command, a URL or a query — and stops at the point where the reason is composed, leaving the one place payload text reaches an agent unruled.

## Decision

**The reason is identifiers and the service's own words.** It carries the repository, the card or pull request number, the event and the moment, in sentences the service wrote. It never carries a payload field: not a title, not a body, not a comment, not a branch name, not a filename.

## Consequences

- A wake cannot quote the sentence that caused it. An agent that wants the text re-reads the card, which every wake already requires of it.
- The reason has a fixed shape per rule, so its wording is reviewed in this repository rather than assembled per delivery.
- A title or a comment that is hostile, enormous or misleading costs the service nothing, because none of it reaches an argv.
- The rule is checkable: a test runs the router over a fixture whose every text field is a sentinel value and asserts that no reason contains it.

## Rejected

- **Quoting the text that caused the wake.** Rejected: it is the one place third-party text would reach an agent with tools and credentials, and it buys only a sentence the agent can read for itself.
- **Escaping and length-limiting the text instead.** Rejected: escaping is one more filter to get wrong, and it does not change what the argv holds.
- **Leaving it to the reviewer.** Rejected: nothing in the documents forbade it, so nothing looked for it.
