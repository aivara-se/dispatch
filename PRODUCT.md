# Dispatch: product

## Problem

One person runs a fleet of agents on GitHub. A review request, an approval or a failing check reaches an agent only because that person relays it, and nothing proves the work happened. Dispatch decides the next task, prompts the agent that owns it, and proves the step on GitHub before it moves on.

## Scope

**In** — a job per flow per pull request per agent; Hermes profiles; a GitHub App that labels; a record of every task; a server, a CLI and a web client.

**Not this version** — repository overrides; push alerts; budgets in currency; other backends.

**Never** — the engine writes code, a review or a merge, or anything but the `blocked` label; it advances on an agent's word; it carries third-party text as instruction.

## Behaviour

- A pull request is the unit; a job runs one flow's tasks, and a task runs its actions in order.
- Flows are user configuration; the engine ships none.
- A delivery that names no pull request starts nothing.
- The engine advances only on a condition it reads on GitHub.
- A report of success without the effect is a false report; the job stops.
- One task runs per job at a time; a delivery that arrives during one waits.
- A job stops at the limit its flow sets, and takes the `blocked` label.
- A prompt action's output stays in the record; the agent comments on the pull request only when it sees fit.
- A merged pull request ends its jobs `done`; a closed one `blocked`; a task still running is `interrupted`.
- The record keeps each action's output and exit, the verdict, the tool calls and the evidence.

## Autonomy

An agent decides how it works: model, tools, sandbox, skills and the code inside the task. A new flow, task, condition or dependency needs a decision first.

## Vision

One engine for the fleet, one record per pull request, agents free to work.

## Acceptance

- A job ends `done` or `blocked`.
- A non-zero exit stops the job, and so does a false report.
- A job stops at its limit.
- The engine writes only `blocked`.
- Every task shows its actions, outputs and tool calls to the CLI and the web client.
