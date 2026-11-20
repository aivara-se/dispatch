# 005 The client answers at its host's root

## Context

`SYSTEMS.md` says the proxy sends `/dispatch*` to the client, and that is not the deployment. `web/svelte.config.js` sets no `paths.base`; `/flows/correct` and `/_app/version.json` answer at the root; `/dispatch/flows/correct` and `/dispatch/app` answer `404`. The code matches the deployment, so the sentence is the stale one.

The webhook does not answer only at the path the engine binds. Measured on the host, all four spellings answer alike: `405` to a `GET`, the engine refusing a method, and `401` to an unsigned `POST`, the engine refusing a signature. The proxy reaches the engine by four spellings — `/github`, the path `config/dispatch.json` names, and `/v1/webhook/github`, `/dispatch/v1/webhook/github` and `/_webhooks/github` rewritten onto it.

Each spelling is matched exactly. The same four paths with a trailing slash answer `404` to a `GET` and to a `POST`, on the canonical path as on the rewritten ones.

## Decision

- The client is served at the host's root. The build carries no `paths.base`, and `/dispatch/*` is not a surface of it. `SYSTEMS.md`'s `/dispatch*` clause stops here; its unit, its user and its loopback stand.
- The TLS proxy sends every path that is not the webhook's to the client, with buffering off for the event stream.
- The webhook reaches the engine by the four spellings, rewritten onto the path `dispatch.json` names. The engine reads no path from the proxy.
- The match is exact, slash included: `/github/` and `/v1/webhook/github/` answer `404`. A stray slash loses the delivery and reports nothing.
- The map lives in `/etc/caddy/Caddyfile` and nowhere else; the four `Caddyfile.bak-*` files beside it are its only history. An edit lands with `caddy reload --config /etc/caddy/Caddyfile --adapter caddyfile`, which keeps the tunnel up and the canonical path answering `401`.
- Nothing in the client or `internal/api` authenticates, so the proxy is the only place an access rule can be written, and it stands at the host's root.

## Consequences

- Moving the client to a path is a rebuild plus a proxy line, and moving it within the root is a proxy line alone — the inverse of what the old sentence assumed.
- Every path but the webhook's belongs to the client on that host name, so a second surface there is a proxy rule, not a client route.
- The map is one file on one host: a deployment that loses a spelling, or gains a slash, loses the delivery and reports nothing, and no diff in this repository shows it.
