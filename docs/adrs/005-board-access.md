# 005 - Board Access

## Context

`SYSTEMS.md` (*The board*, *GitHub access*) fixes the surfaces: a card is an issue, its column is the project's `Status` field, the project's items are read with GraphQL one page at a time, one card is read with the issue endpoint, and one read-mostly token writes one field. What is unrecorded is why the payload answers what it can, and what the API is spent on.

## Decision

- **A column is only ever a project item.** The project's items are read with GraphQL's project-items query, one page at a time, with the `Status` field by name. That read answers which column a card is in — for the whole board, where a decision turns on which developer holds a card, and for one card.
- **One card is read with the REST issue endpoint**: its assignee, and its state. The endpoint answers an issue and a pull request alike, which a project read cannot — GraphQL's `issue()` field refuses a pull request's number with `NOT_FOUND` and no node.
- **One token**, built once at boot: `read:project` for the board, and write access to issues for the assignee. The minimum that does the job, and the only credential the service holds.
- **The write is one field** — the card's assignee, through the issue endpoint, which is the field the project shows. The service moves no card, writes no comment, closes nothing, and keeps no second source of truth.
- **A read or a write that fails is an error**, never a silent *nobody to wake*: the delivery is refused, and GitHub sends it again.

## Consequences

- The payload first. A decision the payload can answer spends no call, so the board is read only where the payload is genuinely silent, and a burst of comments on one card does not become a burst of queries.
- Two APIs in one client: a GraphQL query with a cursor and a page loop, and the issue endpoint. The cost is two response shapes, two rate-limit budgets and two ways to be wrong; the benefit is that each read is the cheapest one that answers its question.
- An empty answer and a failed read are different facts, and conflating them is the failure this decision exists to prevent: a read that fails refuses the delivery, and an empty page is a board on which no such item exists.
- The token is the service's outbound security surface: read-mostly by construction, so a leak is bounded by its scopes rather than by the code. Nothing logs it, at any verbosity.
- The write happens before the run, so the board shows the claim even when the run then fails to start. The board is ahead of reality by one run, which is the safe direction and the one a restart reconciles.

## Rejected

- **Reading one card's column from the issue endpoint.** Rejected because it is not there: that answer carries no project field, so a column a decision turns on would read as absent — a failure wearing the shape of an empty answer. The column comes from a project read, and the issue endpoint is read for the assignee.
- **Caching the board in memory.** Rejected: the board is the durable state, and a cache is a second source of truth; a restart could then serve a column that has already moved.
- **A payload-only router**, with no board read at all. Rejected: the payload names the card and the action and is silent about the column and the assignee, so choosing a developer for a `Refined` card, and honouring one card per developer, would be unanswerable.
- **A GitHub App** instead of a token. Rejected for v1: per-installation tokens are the better long-term shape and the obvious upgrade if the token's blast radius ever matters, but it is a second credential to create, store and rotate for a service with one installation.
- **Writing more than the assignee** — a comment to announce a wake, a label, a card move. Rejected: each would be a second writer of a fact the board, the audit log or the wake itself already carries.
