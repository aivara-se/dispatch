# 028 - The Service Is a GitHub App

## Context

[`002`](002-github-webhook-ingestion.md) took one organisation webhook, one secret, and a payload-only decision. For the one flow then in scope, the payload named the reviewer, so the service needed no API call and held no credential. That trade holds for the decision. It does not hold for the identity. The organisation's secret is shared by every integration the organisation configures, so a leak names no one. The service's scope is "every repository in the organisation", which nobody chose and nobody can narrow. And the service cannot see what GitHub sent it once the request is answered. A service that only reads cannot speak either: a wake it could not start is silent, and the person waiting on the thread is the last to know.

An app changes those facts. Its permissions are declared, granted and enforced by GitHub. Its installation names the repositories in scope, as an act rather than an omission. Its webhook is its own, with its own secret, and its deliveries stay readable for three days. The price is a private key on the host and one code path that signs.

## Decision

**The service runs as a GitHub App installed on the organisation. It reads the events it acts on, writes one kind of thing, and calls no endpoint outside this record's list.**

- **Read:** pull requests, issues, actions, and the metadata every app carries.
- **Write:** issues and pull requests, for one act — a comment on the thread where the service has gone quiet, because a wake could not start after the boot reconcile or because a cap is reached. A pull request's conversation is an issue to GitHub's API, so the one call serves both surfaces.
- **The comment is the service's own words and identifiers.** It names no login and carries no text a third party wrote, so it wakes nobody, and its own delivery returns to the receiver and is ignored.
- **The webhook is the app's.** Subscriptions are the six events the flows already use. The body contract does not move: raw bytes, `X-Hub-Signature-256`, 1 MiB, verified before anything is parsed. That rule is the webhook's alone. A body an API call returns carries no signature; the JWT or the installation token, over TLS, authenticates it.
- **Two credentials.** A JWT signed per call from the private key reaches the app-level endpoints and lives ten minutes. An installation token, minted from it and cached until shortly before its hour ends, reaches the installation's.
- **The list:** `GET /app/hook/deliveries`, `GET /app/hook/deliveries/{id}`, `POST /app/installations/{id}/access_tokens`, `POST /repos/{owner}/{repo}/issues/{number}/comments`.
- **The scope is the installation, and GitHub enforces it.** The app is installed on the organisation and on the repositories the organisation selects. GitHub delivers to the app for those alone, so a delivery that arrives belongs to an installed repository by construction. The service keeps no repository list.

## What this overrides

`002`'s "The service calls no API" and "holds no GitHub credential". The rest of `002` stands as written, "no allowlist of repositories" and "the receiver does not re-check the repository" included. The installation narrows the scope, and GitHub's delivery enforces it.

## Consequences

- **One sentence stops being true:** "never writes to GitHub: no label, no comment, no merge, no push". The new one is narrower and checkable: one comment, on the thread, when the service stops waking on it — and no label, no merge, no push, no review request, no dismissal.
- **GitHub's grant is wider than the use.** `issues: write` also permits a label, a close, an assignment and an edit. `pull_requests: write` also permits a merge, a close, a review, a dismissal and a review request. The code refuses every one of them; the grant does not. That is the cost of a comment.
- **The key now speaks.** It signs for the app, so a stolen key reads what the app can read and posts as it, across the installed repositories, until the key is regenerated. The write is visible on the thread. The shared webhook secret it replaces could start runs; this cannot.
- **Two credentials to keep fresh**, and a clock that matters: a JWT past its ten minutes is a failed call, and so is a cached token near its hour.
- **A comment that fails is logged and not retried.** The error goes to the process log, as a failed audit append does, and nothing enters the record: no line, no field, no decision value. The five kinds stay five.
- **Check runs stay out.** A check named `dispatch` can be made required and block every merge the day the service stops posting.
- **Two acts belong to an owner of the organisation:** register the app and install it. The service can do neither.
- **The standard library still signs it.** `crypto/rsa`, `crypto/x509` and `encoding/pem` are enough, and no dependency enters.
