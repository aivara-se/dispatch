# 007 The CLI reads the record

## Context

The operator needs a terminal view. The web client covers the same record visually, so a TUI would duplicate it. The CLI stays plain and scriptable.

## Decision

- One Bun CLI, `dispatch`, a client of the Unix socket; it holds no state.
- Commands:
  - `dispatch flows` — each flow and its job count.
  - `dispatch jobs [--flow F] [--state S]` — one line per job: key, state, verdict, age.
  - `dispatch show <flow>/<owner>/<repo>/<key>` — the job's tasks and actions with outputs; `--json` prints the API object.
  - `dispatch watch` — follow the stream and print each change.
- `--socket <path>` on every command; default `~/.dispatch/dispatch.sock`.
- Read-only; no command writes.
- Exit codes: 0 on success, 1 when the flow or job is not found, 2 when the socket is missing or the reply is bad.

## Consequences

The CLI is safe to run against production; it cannot change a job. Scripts depend on `--json`, which mirrors the API. There is no TUI in this version.
