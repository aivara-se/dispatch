# 007 - The Review Cap

## Context

`006` closed the review loop and left it unbounded: no round count stops it, because a stopping rule needs a count and a write. v1.2 adds that rule, so a disagreement cannot spend runs forever. The service is assumed to run without interruption, so the count is in memory and nothing survives or reconciles it.

`006`'s "no count, no state beyond the delivery-id cache" and "The loop is unbounded" are overridden here, and with them `002`'s "the dedup cache is the whole of the state ingestion keeps". Everything else in `002`, `003` and `006` stands.

## Decision

- **One count per pull request**, keyed by the repository's full name and the pull request number and held in memory for the life of the process. Nothing is written to disk.
- **A round is a review that requests changes.** Each `pull_request_review` delivery with action `submitted` and state `changes_requested` on a pull request below the cap adds one; an approval or a comment adds nothing. The count stops at ten.
- **The cap is ten rounds.** At ten, neither a `changes_requested` review nor a `review_requested` delivery for that pull request wakes anyone. The first ten rounds wake as before.
- **A capped delivery is accepted.** It is answered `202`, and its audit line says nobody was woken because the cap was reached, so the record still says whether a run started and why not.
- **No other state, no GitHub write.** No expiry, no reset, no per-agent or per-branch count, and nothing sent to GitHub.

## Consequences

- A pull request's loop ends after ten rounds even while the two agents still disagree: the tenth review is the last that wakes anyone.
- A count dies with the process, so a restart begins the pull request again at zero and can grant another ten rounds. That is accepted because uninterrupted running is assumed.
- The cap is blunt: a pull request that genuinely needs an eleventh round stops, and resolving it is the operator's act outside the service.
