# Dispatch: systems

## Architecture

One Go server, one Bun CLI, one SvelteKit web client.

- **Server** verifies deliveries, resolves the pull request, runs one task at a time per job, launches Hermes profiles, reads conditions on GitHub, and keeps the record in SQLite.
- **API** is read-only JSON over a Unix socket: the flows, one flow's jobs, one job, a stream and a health route.
- **CLI and web client** read that socket; the client renders on its server, and the browser talks only to it.

The server and the client are systemd units on the operator's host. The client runs as the server's user, because the socket is `0600`, listens on loopback, and answers at the host's root; the proxy sends it every path but the webhook's, which reaches the engine by four spellings (`005`).

## Dependencies

- Go 1.26, its standard library and a SQLite driver.
- The Hermes binary, by absolute path.
- A GitHub App with pull-request read, issue write and a webhook secret.
- Bun and Node for the clients.
- A TLS proxy.

## Failure modes

- Wrong signature or a body past 32 MiB: 401 or 400, nothing parsed.
- No pull request, or an unserved repository: 202, no job.
- Agent fails or reports an effect it did not make: the condition decides.
- A delivery during a task: another flow starts.
- A missed delivery: not replayed; the record keeps what arrived.
- A crash: unfinished tasks resume.
- A deadline kills the action and stops the job.
- A key, port or socket missing: the process stops at boot.
