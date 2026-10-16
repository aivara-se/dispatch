# 005 - Board Access

## Context

`SYSTEMS.md` (*The board*, *GitHub access*) fixes the surfaces: a card is an issue, its column, its assignee and its repository are read with the project-items query, the assignee is written with the issue endpoint, and one read-mostly token does both. What is unrecorded is why the payload answers what it can, and what the API is spent on.

## Decision

- **A column is a project field, never an issue field.** The REST issue endpoint carries none (*Issues*, GitHub REST, docs.github.com, read 2026-10-12), so a `Status` read from it is necessarily an empty answer — the failure this record exists to prevent. The column is read with GraphQL's project-items query, one page at a time, with the `Status` field by name, for the whole board — which is what a decision that turns on who holds a card needs.
- **The payload names the change; the board names the state.** A `projects_v2_item` event carries the field that changed — `changes.field_value`, with `field_name`, `from` and `to` (*GraphQL and webhook support for project status updates and more!*, GitHub changelog, 2024-06-27, read 2026-10-12) — so the payload says a card's `Status` moved, and where to. What it does not say is where the card is when the delivery is acted on: GitHub delivers out of order (*Troubleshooting webhooks*, *Webhooks deliveries are out of order*, docs.github.com, read 2026-10-12), and this service never orders events, so the payload's copy is a move behind the moment the card moves again. The column a decision turns on is the one the board reports at the read.
- **A card's assignee is read from the project page, with the card, and written with the REST issue endpoint at the repository the page named.** One page carries a card's column, its holder and its repository, so the read the rule decides on is one read. The repository is read as its owner and name, never as a name under the configured owner: an issue's number is the issue's repository's own, the endpoint's URL is one path per owner and repository, and the project read is the only record of which one a card lives in. The endpoint answers an issue and a pull request alike (*Issues*, GitHub REST, docs.github.com, read 2026-10-12).
- **One token**, built once at boot: `read:project` for the board, and write access to issues for the assignee. The minimum that does the job, and the only credential the service holds.
- **The write is one field** — the card's assignee, through the issue endpoint, which is the field the project shows. The service moves no card, writes no comment, closes nothing, and keeps no second source of truth.
- **A read or a write that fails is an error**, never a silent *nobody to wake*: the delivery is refused rather than answered `202`. The recovery is the next look, not the sender (`SYSTEMS.md`, *Failure modes*).

## Consequences

- The payload names what changed and the action it concerns. It saves no call — every look reads the project in full ([008](008-the-service-acts-on-the-board.md)) — and what it carries is the event and its action, which a board read does not show: a review request lives on the pull request, not on the card.
- Two APIs in one client: a GraphQL query with a cursor and a page loop, and the issue endpoint. The cost is two response shapes, two rate-limit budgets and two ways to be wrong; the benefit is that each read is the cheapest one that answers its question.
- An empty answer and a failed read are different facts, and conflating them is the failure this decision exists to prevent: a read that fails refuses the delivery, and an empty page is a board on which no such item exists.
- The token is the service's outbound security surface: read-mostly by construction, so a leak is bounded by its scopes rather than by the code. Nothing logs it, at any verbosity.
- The write happens before the run, so the board shows the claim even when the run then fails to start. The board is ahead of reality by one run, which is the safe direction and the one a restart reconciles.
