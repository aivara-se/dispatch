# 042 A review with no verdict still ends its task

## Context

Two production jobs ended `blocked` and `failed` although the reviewer had submitted a review: momo's review on `dispatch#196` and on `.website#219` was `COMMENTED`. `029` says a comment is not a verdict and leaves the last one standing, so the review task's `next` — `pr.approved` and `pr.rejected` — held nowhere, and "any other stop fails the job" applied.

The flow asks the reviewer for a verdict, and a review that only comments is a legitimate outcome the model allows. It has no branch. `_done_` cannot be reached on an agent's word, so the branch has to be a condition read on GitHub.

## Decision

- A condition `pr.reviewed` joins the set: the head carries at least one submitted review — approved, changes requested or commented — and a pending or a dismissed review does not count.
- It is read after a task, never on a delivery, so it starts no job.
- A review flow names it as the branch for a review the two verdicts do not cover.

## Consequences

- A reviewer who comments ends the task `done` instead of failing the job, and a merge flow still waits for `pr.approved`.
- The condition is pull request-level: a job can end on another reviewer's review of the same head, which is how `pr.approved` already behaves (`038`). The job's evidence names the head and the review states, not the reviewer.
- Two of the three blocked rows on the first production record would read `done` under this condition.
- `029`'s closed set gains a key, and `035`'s list of start conditions does not: `pr.reviewed` is read after a task, so a `_start_.next` naming it fails the load.
