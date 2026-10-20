# 007 - The Label on the Audit Record

## Context

[005](005-the-audit-record.md) fixed the record's fields for a service whose decision was which agent to wake. v1.1 adds a second outcome the operator has to be able to answer for — whether the `blocked` label was written ([006](006-the-review-loop-and-its-cap.md)) — and a wake path that reads the label before it wakes. The product's acceptance now asks the log for that fact, and 005's field list has no place for it. 005 is accepted and stays as it is, so this record overrides its field list and the consequence that the line answers the acceptance exactly.

The failure modes are not one fact: a write that failed is a delivery that woke nobody, a read that failed is a wake that went ahead, and a pull request already carrying the label is neither. The line has to keep those apart.

## Decision

- **One more field: the label.** A line that touched the label carries its name and the outcome — `written`, or the reason the write failed. A line that did not touch it carries no label field.
- **A blocked read is the decision.** A delivery that read the label and found the pull request blocked wakes nobody; the decision says so and the label field stays absent, because nothing was written.
- **A failed read is a reason, not a decision.** When the read fails and the wake goes ahead, the decision is the run that started and the failure is the reason the line carries beside it.
- **The field set is 005's plus this one.** Nothing else changes: one JSON Lines line per delivery, appended in arrival order, never rewritten, size-rotated, with no payload body and no secret value.

## Consequences

- The record answers both halves of the acceptance: whether an agent was started, and whether the label was written. 005's "answers the acceptance exactly" still reads true; the acceptance grew by one question, and so did the line.
- The label's name is the service's own word, so logging it stays inside 005's rule that the line carries no third party's text.
- A failed write is visible without reading the pull request, which is the point: the operator learns from the log that a pull request is not marked blocked when the service meant it to be.
