# 006 - The Review Loop and Its Cap

## Context

v1 wakes a reviewer when a pull request asks for a review, and what becomes of that review is nobody's business. v1.1 closes the loop: a review that requests changes must wake the author to update the pull request, and the author's update and re-request must wake the reviewer again. The loop has no natural end when reviewer and author disagree, and each round costs two runs, so the design needs a stopping rule that does not depend on an agent's goodwill.

Three questions had to be answered: which review wakes the author, how the service knows how many reviews a pull request has had, and what it does when the loop will not end. Two of the answers need what the service did not have — a count, and a write to GitHub. [`002`](002-github-webhook-ingestion.md) recorded that the service calls no API and holds no credential; that bullet and its consequence are amended in place, and nothing else in `002` changes.

Counting reviews by listing them from the API was not taken: it is a paginated call on every review and still needs a label read. A GitHub App was not taken: a second credential system for one label is more machinery than the flow carries.

## Decision

- **A review that requests changes wakes the author.** The event is `pull_request_review` with action `submitted` and review state `changes_requested`; the login woken is the pull request's `user.login`. A review that approves or only comments wakes nobody. The v1 rules hold unchanged: only a configured agent can be woken, and the delivery's actor never is.
- **Reviews are counted per pull request, in memory.** The count increments on every review delivery that is not a duplicate and arrives on a pull request that is not blocked, whatever the review's state, and lives beside the delivery-id cache. It is not written to disk and not reconciled at boot.
- **The cap is five.** From the fifth review on, a delivery that would wake someone does not: the service adds the `blocked` label instead, wakes nobody, and clears the count when the write succeeds. Below the cap the wake goes ahead.
- **`blocked` is one fixed name.** The service adds it by name through the issues labels endpoint, never creates it and never removes it. Because the count was cleared when the label was written, the operator's removal of the label gives the loop five more reviews, and the service needs no event to notice the removal.
- **The label is also the guard.** Before any wake the service reads the pull request's labels, and `blocked` among them ends the decision: nobody is woken, and the review that carried it is not counted. That read is what makes the block outlast a restart and cover a pull request the operator blocked by hand, and it is why the five reviews after a removal start from zero.
- **Two API calls, one token.** The whole of the service's GitHub access is `GET` and `POST /repos/{owner}/{repo}/issues/{number}/labels`. The token is fine-grained, with `Issues: write` on the organisation's repositories, and the service reads it from `DISPATCH_GITHUB_TOKEN` in its environment at boot. The variable's name is the service's, so the configuration has no field for it: only the value differs per host, and the environment file already carries that.
- **A failed read does not withhold a wake; a failed write wakes nobody.** The read is bounded by a timeout; a read that fails is recorded on the line and the wake goes ahead. A write that fails is recorded, wakes nobody, and is retried only by the next review on that pull request. Nothing is retried in place.

## Consequences

- **The service holds a credential worth more than it uses.** `Issues: write` on the organisation's repositories is label, comment, assign and close on every issue, where the service uses one endpoint of it. A leaked token is worth more than a leaked webhook secret and is rotated the same way.
- **The count dies with the process.** A restart mid-loop gives the loop a fresh five. The block does not die, because the label is on GitHub, so a runaway needs a restart before the fifth review and no label yet. That is accepted for the reason v1 accepted the dedup cache's loss: restarts are rare and a wake is repeatable.
- **Every review on an unblocked pull request counts, including one nobody is woken for.** A pull request that collects five comment reviews from humans is blocked. The rule is one number with no exceptions; the operator removes the label.
- **The wake path now waits on GitHub.** One bounded read stands between a review request and a run, and a GitHub outage fails open. The loop's guarantee is only as good as that read, which is why the label, not the counter, is what the guard rests on.
- **The label must exist in every repository the flow runs on.** The service adds it by name and does not create it, so a missing label is a failed write on the delivery that needed it. Creating labels would be a second endpoint and a second permission; one operator action per repository is cheaper.
- **The service still does not know what a conclusion is.** A review that does not request changes needs no wake, so "concluded" is the absence of a `changes_requested` review, and nothing has to be recorded about it.
