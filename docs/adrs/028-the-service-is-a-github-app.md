# 028 - The Service Is a GitHub App

## Context

[`002`](002-github-webhook-ingestion.md) took one organisation webhook, one secret, and a payload-only decision: for the one flow then in scope, the payload named the reviewer, so the service needed no API call and held no credential. That trade holds for the decision. It does not hold for the identity. The organisation's secret is shared by every integration the organisation configures, so a leak names no one. The service's scope is "every repository in the organisation", which nobody chose and nobody can narrow. And the service cannot see what GitHub sent it once the request is answered.

An app changes those three facts and no others. Its permissions are declared, granted and enforced by GitHub. Its installation names the repositories in scope, as an act rather than an omission. Its webhook is its own, with its own secret, and its deliveries stay readable for three days. The price is a private key on the host and one code path that signs.

## Decision

**The service runs as a GitHub App installed on the organisation. It holds read permissions only, and it calls no endpoint outside this record's list.**

- **Permissions, all read:** pull requests, issues, actions, and the metadata every app carries. No write permission of any kind.
- **The webhook is the app's.** Subscriptions are the six events the flows already use, and the body contract does not move: raw bytes, `X-Hub-Signature-256`, 1 MiB, verified before anything is parsed.
- **No installation token.** The list holds two app-level endpoints, `GET /app/hook/deliveries` and `GET /app/hook/deliveries/{id}`, and a JWT reaches them. The JWT is signed per call from the private key, lives ten minutes, and is never stored.
- **The key is a file.** The configuration names its path. The service reads it at boot and it leaves the process only as a signature.
- **The scope is the installation.** `installation` and `installation_repositories` deliveries name the repositories, and a delivery from a repository outside that set is refused, not acted on.

## What this overrides

`002`'s "The service calls no API", "holds no GitHub credential", and "the receiver does not re-check the repository". The rest of `002` stands, including the signature, the bound, the dedup key and the reason.

## Consequences

- **The key is the new crown jewel.** It signs for the app, so a stolen key reads what the app can read, across the installed repositories, until the key is regenerated. The shared webhook secret it replaces could start runs; this cannot.
- **Read-only is GitHub's to enforce.** The grant lives on the organisation's side, so a bad merge cannot widen it.
- **Two acts belong to an owner of the organisation:** register the app and install it. The service can do neither.
- **The trust sentence changes.** "It holds no credential but the webhook's secret" becomes "three read scopes, no write, and one endpoint list".
- **The standard library still signs it.** `crypto/rsa`, `crypto/x509` and `encoding/pem` are enough, and no dependency enters.
