# 000 - Record Architecture Decisions

## Context

Dispatch is the process engine for an agent fleet. A few choices decide its shape and are expensive to reverse: what a job's root is, how a step is proved, which conditions the engine can read, what it may write, and how an agent is started. A later reader must find them, and their status must not depend on anyone's memory.

## Decision

We record one significant architectural decision per file in `docs/adrs`, as Markdown. A decision is **Accepted** when its file is merged into `main`, and **In Review** while its pull request is open. An accepted record is not edited: a decision that contradicts it is a new record that names the clauses it overrides, and the old file stays as the account of what was believed then.

An ADR is written before the code that depends on it, and it says what the choice costs, not only what it buys.

## Consequences

- The why behind a choice is searchable and outlives the agent who made it.
- A reader can tell which decisions are live and which are history.
- A decision that inverts costs a new file rather than a rewrite, so the record keeps its history.
