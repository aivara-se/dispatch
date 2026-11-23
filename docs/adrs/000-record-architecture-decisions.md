# 000 - Record Architecture Decisions

## Context

A repository created from this template starts with its documents and no history. The first decision every repository makes is how it records the rest, and that record must outlive the agent that wrote it: the choice, what it cost, and what a later change overrode.

## Decision

We record one significant architectural decision per file in `docs/adrs`, as Markdown. A decision is **Accepted** when its file is merged into `main`, and **In Review** while its pull request is open. An accepted record is not edited: a decision that contradicts it is a new record that names the clauses it overrides, and the old file stays as the account of what was believed then.

An ADR is written before the code that depends on it, and it says what the choice costs, not only what it buys.

## Consequences

- The why behind a choice is searchable and outlives the agent who made it.
- A reader can tell which decisions are live and which are history.
- A decision that inverts costs a new file rather than a rewrite, so the record keeps its history.
