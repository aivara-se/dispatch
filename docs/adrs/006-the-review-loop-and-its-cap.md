# 006 - The Review Loop and Its Cap

## Context

v1 wakes a reviewer when a pull request asks for a review, and what becomes of the review is nobody's business. v1.1 closes the loop: a review that requests changes wakes the author to update the pull request, and the author's re-request wakes the reviewer again. The loop has no natural end when the two disagree, and each round costs two runs, so it needs a stopping rule that does not depend on an agent's goodwill.

Two of the answers need what the service did not have: a count, and a write to GitHub. [`002`](002-github-webhook-ingestion.md) and [`003`](003-starting-a-run.md) are accepted and stay as they are. This record overrides `002`'s one-event bullet and its no-API bullet, and narrows `003`'s promise that a review request always starts a run: at the fifth round no run starts, and below it the only thing added is one bounded read. Everything else in those records stands.

Counting rounds from the reviews API was not taken: a paginated call on every review, on top of the label read. A GitHub App was not taken: a second credential system for one label.

## Decision

- **A review that requests changes wakes the author.** `pull_request_review` with action `submitted` and state `changes_requested`; the login woken is the pull request's author. An approval or a comment wakes nobody. Only a configured agent can be woken, and the delivery's actor never is.
- **Rounds are counted per pull request, in memory.** A round is a review that requests changes. The count skips duplicates and blocked pull requests, lives beside the delivery-id cache, and dies with the process.
- **The cap is five.** From the fifth round on, a delivery that would wake someone adds the `blocked` label instead, wakes nobody, and clears the count when the write succeeds — so removing the label gives the loop five more rounds.
- **`blocked` is one fixed name**, added by the service through the issues labels endpoint, never created and never removed by it. The operator removes it to resume.
- **The label is also the guard.** Before any wake the service reads the pull request's labels; `blocked` among them ends the decision. That read is what makes the block outlast a restart, and what covers a pull request the operator blocked by hand.
- **Two calls, one token.** `GET` and `POST /repos/{owner}/{repo}/issues/{number}/labels`. The token is fine-grained with `Issues: write` on the organisation's repositories, read from `DISPATCH_GITHUB_TOKEN` at boot; the configuration does not name it, because only the value differs per host.
- **A failed read does not withhold a wake; a failed write wakes nobody.** The read is bounded and fails open, recorded on the line. A write failure is recorded, wakes nobody, and is retried only by the next review.

## Consequences

- **The service holds a credential worth more than it uses.** `Issues: write` is label, comment, assign and close on every issue, where the service uses one endpoint of it. It is rotated like the webhook secret.
- **The count dies with the process.** A restart mid-loop gives the loop a fresh five; the label does not die, so a runaway needs a restart before the fifth round and no label yet. Accepted, as v1 accepted the dedup cache's loss: restarts are rare and a wake is repeatable.
- **Only a changes-requested review is a round.** Comment reviews and approvals never reach the cap: the budget measures the loop, not the pull request's traffic.
- **The wake path now waits on GitHub.** One bounded read stands between a delivery and its run, and a GitHub outage fails open. The label, not the counter, is what the guard rests on.
- **The label must exist in every repository the flow runs on.** A missing label is a failed write on the delivery that needed it; one operator action per repository is cheaper than a second endpoint and permission.
