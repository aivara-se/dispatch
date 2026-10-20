# 006 - The Review Loop

## Context

v1 wakes a reviewer when a pull request asks for a review, and what becomes of the review is nobody's business. v1.1 closes the loop: a review that requests changes wakes the author to update the pull request, and the author's re-request wakes the reviewer again. What needs recording is the second delivery and what it costs, because the loop can repeat as long as the two disagree.

`002` recorded that one event acts and that the service calls no API. The first is overridden here; the second stands, because v1.1 adds no GitHub access and no state. Everything else in `002` and `003` stands.

## Decision

- **A review that requests changes wakes the author.** The event is `pull_request_review` with action `submitted` and review state `changes_requested`; the login woken is the pull request's author. An approval or a comment wakes nobody. Only a configured agent can be woken, and the delivery's actor never is — the rules `002` fixed for the reviewer.
- **Nothing else changes.** No count, no label, no GitHub API call, no credential, no state beyond the delivery-id cache: the payload and the configuration still answer the whole decision, and a wake still costs one child process and no bookkeeping.
- **The loop is unbounded in v1.1.** No round count decides when to stop. It ends when a review approves, when the author stops requesting one, or when the operator acts on the pull request. The cap and the `blocked` label are the next version's decision and are not parked in this one.

## Consequences

- **A disagreement costs runs until someone stops it.** Two agents can exchange rounds indefinitely on one pull request, two runs a round. That is the price of the smallest change that closes the loop: a stopping rule needs a count and a write, which is the work this version leaves out.
- **The operator is the stop.** With no label, stopping the loop means removing the review request, closing the pull request, or telling an agent to stop — the platform's own controls, not the service's.
