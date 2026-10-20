# 005 - The Audit Record

## Context

PRODUCT's acceptance turns on two things being answerable after the fact: for any delivery, whether an agent was started and, when not, why — and whether the `blocked` label was written. Nothing else in the service remembers a delivery — the dedup cache is in memory and dies with the process ([002](002-github-webhook-ingestion.md)), the review count goes with it ([006](006-the-review-loop-and-its-cap.md)), and the run itself is not tracked ([003](003-starting-a-run.md)) — so the record is the only durable thing the service writes. What needs recording is its shape.

## Decision

**One JSON Lines line per delivery, appended in arrival order, never rewritten, size-rotated.** A line carries the delivery's identity and the service's decision, and never a secret value or a payload body:

- the **delivery id** (`X-GitHub-Delivery`) and the **event** (`X-GitHub-Event`);
- the **repository and the pull request number**;
- the **decision** — the profile whose run was started, or that nobody was woken and why;
- the **label**, when the delivery touched it: that `blocked` was written, or why the write failed; a delivery that read the label and found the pull request blocked says so in its decision;
- the **answer** the sender was given.

The reason a wake carried is in the line, because it is the service's own words and not a third party's ([002](002-github-webhook-ingestion.md)); no other field of the payload is logged.

## Consequences

- The record answers the acceptance exactly: whether an agent was started, whether the label was written, and when not, why — and a redelivered delivery leaves its line like any other.
- It is the delivery's record and the run's is not: a line says a run was started and never what became of it, which is the run's own channel's business ([003](003-starting-a-run.md)).
- Only a delivery that verified is logged. A request refused before that — a missing signature, an oversized body — is answered and nothing is appended, so the record holds what the service was asked to act on and not what it turned away.
