# 007 - A Claim About Another Program Names Its Source

## Context

Every record beside this one rests on behaviour of a program outside this tree: GitHub's webhook semantics, the Hermes CLI, the gateway's routes, signatures, answers and cache. No test here covers them, and `SYSTEMS.md` (*Checks*) says so: those are checked by hand on the host. A hand-check that does not name what it checked is re-derived from memory the next time the claim is touched — which is how a claim stays wrong, and how a wrong one is reused as the reason for a choice it cannot support.

## Decision

Where a record or a document in this repository rests on the behaviour of something outside it, the claim names what was read — the file and the repository, or the document, and the date — in the sentence that makes the claim. Where one file is the home of a fact, that file carries the source and everything else names the home. The check is repeated before the code that depends on the claim lands, and the name is what makes the repeat possible.

## Consequences

- A claim with a source can be re-checked by the next reader; a claim without one is a belief that reads like a fact, and is re-derived from memory instead of re-read.
- Each external claim costs one clause. Nothing else about a record changes: the decision, its consequences and its rejected alternatives are written as before.
- A name is enough. What is wanted is where to look, not a transcription of what is there.
- The record ages with its source: a Hermes or a GitHub upgrade can invalidate a claim, and the reader who can find the file is the one who can tell.

## Rejected

- **Leaving it to the reviewer.** It is how both wrong claims were caught, and it stops the moment the reviewer changes or is busy.
- **Naming a version or a commit** instead of the source and the date. Rejected: it dates the claim without saying where to look, and reads as current long after it is not.
- **No external claim in a record.** Rejected: the decisions that shape this service are mostly about how it relates to GitHub, the board and the Hermes runtime; a record that cannot state a dependency cannot record what it costs.
