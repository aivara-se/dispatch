# 005 - Board Access

## Context

`SYSTEMS.md` (*The board*, *GitHub access*) fixes the surfaces: a card is an issue, its column is the project's `Status` field read with the project-items query, one card's assignee is read with the issue endpoint, and one read-mostly token writes one field. What is unrecorded is why the payload answers what it can, and what the API is spent on.

## Decision

- **A column is a project field, never an issue field.** The REST issue endpoint carries none (*Issues*, GitHub REST, docs.github.com, read 2026-10-12), so a `Status` read from it is necessarily an empty answer — the failure this record exists to prevent. The column is read with GraphQL's project-items query, one page at a time, with the `Status` field by name, for the whole board — which is what a decision that turns on who holds a card needs.
- **The payload names the change; the board names the state.** A `projects_v2_item` event carries the field that changed — `changes.field_value`, with `field_name`, `from` and `to` (*GraphQL and webhook support for project status updates and more!*, GitHub changelog, 2024-06-27, read 2026-10-12) — so the payload says a card's `Status` moved, and where to. What it does not say is where the card is when the delivery is acted on: GitHub delivers out of order (*Troubleshooting webhooks*, *Webhooks deliveries are out of order*, docs.github.com, read 2026-10-12), and this service never orders events, so the payload's copy is a move behind the moment the card moves again. The column a decision turns on is the one the board reports at the read.
- **One card is read with the REST issue endpoint**: its assignee. The endpoint answers an issue and a pull request alike (*Issues*, GitHub REST, docs.github.com, read 2026-10-12), which a project read cannot — GraphQL's `issue()` field refuses a pull request's number with `NOT_FOUND` and no node (checked against the live board, 2026-10-12; the GraphQL reference does not state it).
- **One token**, built once at boot: `read:project` for the board, and write access to issues for the assignee. The minimum that does the job, and the only credential the service holds.
- **The write is one field** — the card's assignee, through the issue endpoint, which is the field the project shows. The service moves no card, writes no comment, closes nothing, and keeps no second source of truth.
- **A read or a write that fails is an error**, never a silent *nobody to wake*: the delivery is refused rather than answered `202`. The recovery is a person's, not the sender's (`SYSTEMS.md`, *Failure modes*).

## Consequences

- The payload names what changed and the action it concerns; the board answers the column and the assignee. Every look reads the project in full ([008](008-the-service-acts-on-the-board.md)), so the payload's saving is the search it removes rather than a call — and the column still comes from the board, because a delivery's copy is a move old by the time it is acted on.
- Two APIs in one client: a GraphQL query with a cursor and a page loop, and the issue endpoint. The cost is two response shapes, two rate-limit budgets and two ways to be wrong; the benefit is that each read is the cheapest one that answers its question.
- An empty answer and a failed read are different facts, and conflating them is the failure this decision exists to prevent: a read that fails refuses the delivery, and an empty page is a board on which no such item exists.
- The token is the service's outbound security surface: read-mostly by construction, so a leak is bounded by its scopes rather than by the code. Nothing logs it, at any verbosity.
- The write happens before the run, so the board shows the claim even when the run then fails to start. The board is ahead of reality by one run, which is the safe direction and the one a restart reconciles.

## Rejected

- **Reading one card's column from the issue endpoint.** Rejected because it is not there: that answer carries no project field, so a column a decision turns on would read as absent — a failure wearing the shape of an empty answer. The column comes from a project read, and the issue endpoint is read for the assignee.
- **The payload's column as the column.** Rejected for the same reason a cache is: it is a snapshot taken before the delivery was handled, and a card that moved twice before its second delivery was processed would be assigned a developer from the first move.
- **Caching the board in memory.** Rejected: the board is the durable state, and a cache is a second source of truth; a restart could then serve a column that has already moved.
- **A payload-only router**, with no board read at all. Rejected: the payload names the card and the action, and the column only when the column is the field that changed; choosing a developer for a `Refined` card, and honouring one card per developer, need the board's assignee, and the column as the board holds it now.
- **A GitHub App** instead of a token. Rejected for v1: per-installation tokens are the better long-term shape and the obvious upgrade if the token's blast radius ever matters, but it is a second credential to create, store and rotate for a service with one installation.
- **Writing more than the assignee** — a comment to announce a wake, a label, a card move. Rejected: each would be a second writer of a fact the board, the audit log or the wake itself already carries.
