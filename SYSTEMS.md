# Dispatch: systems

## Architecture

Three parts: a Go server, a Bun CLI, a SvelteKit web client.

- **Server** verifies deliveries, resolves the pull request, runs one task at a time per job, launches Hermes profiles, verifies conditions, and keeps the record in SQLite.
- **API** is JSON over a Unix socket.
- **CLI** holds no state.
- **Web client** is a SvelteKit app whose server code reads the socket; the browser talks only to it.

A job runs one flow's tasks for one pull request; a task's actions run in order — a prompt, a script, an engine action — and the record keeps each output, condition, tool call and evidence. A merged or closed pull request ends the job.

GitHub posts deliveries; the server reads pull requests and reviews, and writes only labels. The agent writes with its own identity and may comment on the pull request.

### Deployment

Each part is a systemd unit on the operator's host. The client runs as the engine's user, because the socket is `0600`, and listens on loopback. The proxy sends `/dispatch*` to it; the webhook's longer path still wins.

## Dependencies

- Go 1.26, its standard library and a SQLite driver.
- The Hermes binary.
- A GitHub App with pull-request read, issue write and a webhook secret.
- Bun and Node for the clients.
- A TLS proxy.

## Failure modes

- Wrong signature or bad body: 401 or 400, nothing parsed.
- Duplicate: 202, no second task.
- No pull request: 202, nothing starts.
- Agent fails: the condition decides; a false report is recorded.
- A delivery during a task: another flow starts.
- Missed delivery: it is not replayed; the record keeps what arrived.
- Crash: unfinished tasks are re-verified.
- Deadline: the task is stopped and the job stops.
- Key or port missing: the process stops at boot.
- No socket: the client says so.
