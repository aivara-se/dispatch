# 010 The client is served under a path on its host

## Context

[003](003-the-clients-read-a-flow-scoped-api.md) fixes the client's boundary: its server code reads the socket, and the browser talks only to it. It says nothing about where that process runs or how a browser reaches it.

Two facts constrain the answer. The engine's port carries GitHub's deliveries and nothing else, and the socket is `0600` in the operator's home: a client that let a browser read either would publish the whole record, prompts and outputs included.

The lab's host serves several surfaces from one name. This one answers at `/dispatch`, and the webhook's path already ends there: `/dispatch/v1/webhook/github`.

## Decision

- The client is one Node process, `node build` from the SvelteKit build, in its own systemd unit on the operator's host, started after the engine's.
- It runs as the engine's user, because the socket it reads is `0600`; it keeps no copy of the record.
- It listens on loopback only. The host's TLS proxy sends `/dispatch*` to it and leaves the path in place. No other port opens.
- The webhook keeps its path. A client block and a webhook block both match `/dispatch...`, and the longer path wins by Caddy's own sort, whichever block is written first.
- The build carries its mount: `kit.paths.base` is `/dispatch`, and every link, redirect and the event stream is read from `$app/paths`' `base`. A client served at another path is rebuilt, not reconfigured.
- `DISPATCH_SOCKET` names the socket; it defaults to `~/.dispatch/dispatch.sock`.
- The client reads only: no write path, no credential, no state of its own.

## Consequences

- The record reaches a browser only through the client's server code, so the engine's port stays closed and the socket stays `0600`.
- The process needs the socket's owner, so a deployment that runs the engine as another user moves both units together.
- The build is tied to its path: moving the client is a rebuild and a proxy line, never an environment variable.
- Neither the client nor `internal/api` authenticates. The proxy is the only place an access rule can stand, and until one is chosen `/dispatch` publishes the whole record to anyone who reaches the host.
- A client that dies shows an error page and no record; the engine keeps serving deliveries.
- The path match was exercised on Caddy 2.10.2 with both blocks in either order: the webhook answered `/dispatch/v1/webhook/github`, the client answered `/dispatch/` and below.
