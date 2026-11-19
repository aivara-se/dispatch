# 030 The clients read a flow-scoped API

## Context

Two clients read one record; the engine is the only writer. Flows come in kinds, and a job belongs to one flow. A flat job list would hide the flow and force each client to reconstruct it.

## Decision

- The API is read-only JSON over the Unix socket: `GET /v1/flows`, `GET /v1/flows/{flow}/jobs`, `GET /v1/flows/{flow}/jobs/{owner}/{repo}/{key}`, `GET /v1/stream` and `GET /v1/health`. No POST, PUT or DELETE.
- A job is identified by flow, owner, repo and key; the flow is a path segment.
- `/v1/flows` returns `{"flows":[{"name":"review","jobs":3}]}`. A jobs call returns the cards; one job adds each task's `n`, `short`, `at`, `actions`, `pc`, `evidence` and `tools`. An action carries `type`, `output` and `exit`; a prompt adds `agent` and `text`, a script `path` and `args`, an action `name` and `value`.
- `state` is `started`, `blocked` or `done`; `verdict` is `pending`, `passed` or `failed`, and `passed` only when the flow reached `_done_`. `at` is ISO 8601.
- In a task's `pc`, `expr` is the condition, `want` is always `true`, and `got` is what the engine read.
- The stream sends `{"flow","owner","repo","key"}`; the client refetches that job.
- The web client's server code reads the socket and renders on the server; the browser talks only to it.

## Consequences

The dashboard makes one call per flow. A new flow needs no new endpoint. The client reads the record and never derives a verdict.
