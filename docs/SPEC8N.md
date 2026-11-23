# Specification

## Direction

Dispatch is the process engine for the `aivara-se` agent fleet. One operator runs several agents on GitHub, and a review request, an approval or a failing check reaches an agent only because that person relays it; nothing proves the work happened. Dispatch decides the next task in a job, prompts the agent that owns it, and proves the step on GitHub before it advances.

The operator sets the flows and the limits. Deferred by decision: repository overrides, push alerts, budgets in currency, and other backends.

## Requirements

The pull request is the unit. A job runs one flow's tasks for one pull request and one agent; a task runs its actions in order. Flows are user configuration, and the engine ships none.

A delivery that names no pull request starts nothing. The engine advances only on a condition it reads on GitHub. A report of success without the effect is a false report, and the job stops. One task runs per job at a time; a delivery that arrives during one waits. A job stops at the limit its flow sets and takes the `blocked` label. A prompt action's output stays in the record; the agent comments on the pull request only when it sees fit. A merged pull request ends its jobs `done`; a closed one `blocked`; a task still running is `interrupted`. The record keeps each action's output and exit, the verdict, the tool calls and the evidence.

An agent decides how it works: model, tools, sandbox, skills and the code inside the task. A new flow, task, condition or dependency needs a decision first.

Never break: no engine write of code, review or merge, or of any label but `blocked`; no advance on an agent's word; no third-party text carried as instruction.

## Architecture

One Go server verifies deliveries, resolves the pull request, runs one task at a time per job, launches Hermes profiles, reads conditions on GitHub, and keeps the record in SQLite. A read-only JSON API over a Unix socket answers the flows, a flow's jobs, one job, a stream and a health route. A Bun CLI and a SvelteKit client read that socket; the client renders on its server, and the browser talks only to it.

The server and client are systemd units on the operator's host. The client runs as the server's user, listens on loopback, and answers at the host's root behind a TLS proxy.

It depends on Go 1.26 with its standard library and a SQLite driver, the Hermes binary by absolute path, a GitHub App with pull-request read, issue write and a webhook secret, Bun and Node for the clients, and a TLS proxy. The engine is Go and the tooling is Bun and TypeScript; this split is the repository's stated exception.

Checks: Go format, vet, build and test; `bun fmt`, the type check and the CLI tests; the web build, check and tests; and the organisation's entry-file, document and settings checks.
