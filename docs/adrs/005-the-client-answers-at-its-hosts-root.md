# 005 - The client answers at its host's root

## Context

The proxy used to send `/dispatch*` to the client, and the deployment does not. Measured on the host: `/flows/correct` and `/_app/version.json` answer at the root; `/dispatch/flows/correct` and `/dispatch/app` answer `404`. The code matches the deployment, so the sentence was the stale one.

The webhook does not answer only at the path the engine binds. All four spellings answer alike: `405` to a `GET`, the engine refusing a method, and `401` to an unsigned `POST`, the engine refusing a signature. The four spellings are the configured path and three rewrites onto it. Each is matched exactly; the same paths with a trailing slash answer `404` to both methods.

## Decision

- The client is served at the host's root. The build carries no base path, and `/dispatch/*` is not a surface of it.
- The TLS proxy sends every path but the webhook's to the client, with buffering off for the event stream.
- The webhook reaches the engine by four spellings, rewritten onto the configured path. The engine reads no path from the proxy.
- The match is exact, slash included: a trailing slash answers `404`. A stray slash loses the delivery and reports nothing.
- The proxy's map lives in one file on the host. An edit reloads the proxy without dropping the tunnel.
- Nothing in the client or the read-only API authenticates, so the proxy is the only place an access rule can be written, and it stands at the host's root.

## Consequences

- Moving the client to a path is a rebuild plus a proxy line; moving it within the root is a proxy line alone.
- Every path but the webhook's belongs to the client on that host name, so a second surface there is a proxy rule, not a client route.
- The map is one file on one host: a deployment that loses a spelling, or gains a slash, loses the delivery and reports nothing, and no diff in this repository shows it.
- The retired clause read: "the proxy sends `/dispatch*` to the client". Its unit, its user and its loopback stand.
