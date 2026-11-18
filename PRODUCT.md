# Dispatch: product

## Problem

One person runs a fleet of agents on GitHub. A review request, a failing check or a mention reaches an agent only because that person relays it, and nothing proves the work happened. The operator is router and verifier; a silent stop looks like an idle day.

Dispatch is the process engine for that fleet. It decides the next task, prompts the agent that owns it, and proves the step on GitHub before it moves on. Agents do the work; one operator runs the engine.

## Scope

**In** — a job per flow per pull request; Hermes profiles; a GitHub App that labels; a record of every task; a server, a CLI and a web client.

**Not this version** — repository overrides; push alerts; budgets in currency; other backends.

**Never** — the engine writes code, a review or a merge; it writes only the labels the flow names; it advances on an agent's word; it carries third-party text as instruction.

## Behaviour

- A pull request is the unit; a job runs one flow's tasks.
- Flows are user configuration; the engine ships none.
- A delivery that names no pull request starts nothing.
- A task runs its actions in order; every action must exit zero.
- The engine advances only when it reads a condition on GitHub.
- A report of success without the effect is a false report; the job stops.
- A job stops at the limit its configuration sets.
- One task runs per job at a time; a delivery that arrives during one waits.
- A prompt action's output stays in the record; an agent comments only when it sees fit.
- A merge is an agent task; the engine never merges.
- A merged or closed pull request ends the job.
- The engine labels a stopped job `blocked`.
- The record keeps each action's output, the verdict, the tool calls and the evidence.
- The CLI and the web client read the record; the client shows each task's actions and outputs.

## Autonomy

An agent decides how it works: model, tools, sandbox, skills and the code inside the task. A new flow, task, condition or dependency needs a decision first.

## Vision

One engine for the fleet, one record per pull request, agents free to work. It must not become the writer, the planner or a second GitHub client.

## Acceptance

- A job ends in `_done_` or `blocked`.
- A task runs its actions in order; a non-zero exit stops the job.
- A false report stops the job.
- A job stops at its limit.
- The engine writes only `blocked`.
- Every task shows its actions and outputs.
