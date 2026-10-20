# Dispatch: systems

## Where it runs

One long-running process on the operator's host, beside the Hermes home, as a systemd user unit that restarts on failure. It binds loopback and is reached through the host's reverse proxy, which terminates TLS; GitHub is its only inbound client. The binary is static and the deploy is a new binary plus a unit restart — there is no deploy pipeline ([`001`](adrs/001-go-and-the-standard-library.md)).

The unit, the environment file and the proxy site are `README.md` (*Run it on the host*).

## A delivery becomes a run

1. GitHub posts to the public URL; the proxy forwards to the loopback address and path the configuration names ([`004`](adrs/004-configuration.md)).
2. The receiver reads the raw body up to 1 MiB, then verifies `X-Hub-Signature-256` against the configured secret. Nothing is parsed before it verifies ([`002`](adrs/002-github-webhook-ingestion.md)).
3. It takes the event and the delivery id from the headers, and the action, the repository, the pull request and — per event — the requested reviewer, the review state and the author from the body.
4. Two deliveries act: a `pull_request` delivery whose action is `review_requested`, which wakes the requested reviewer; and a `pull_request_review` delivery whose action is `submitted` and whose review state is `changes_requested`, which wakes the author ([`006`](adrs/006-the-review-loop.md)). Every other event, action or review state is accepted and ignored.
5. The delivery id is checked against the in-memory cache. A delivery already seen is accepted and starts nothing.
6. The login to wake is matched against the configured agents — the requested reviewer, or the pull request's author. A login the configuration does not carry wakes nobody, and the delivery's own actor is never woken.
7. The matched profile's agent is started, with the reason ([`003`](adrs/003-starting-a-run.md)).
8. One audit line is appended whatever happened ([`005`](adrs/005-the-audit-record.md)).

The answers are the service's own: `401` a missing or non-verifying signature, `400` a body it will not read, `202` every delivery that verifies — acted on or ignored. The audit line, not the status, tells those two apart.

## Starting a run

The requirement is that a review request is never delayed. The mechanism is a child process: `hermes --profile <profile> chat -q "<reason>"`, launched by the service and not tracked ([`003`](adrs/003-starting-a-run.md)).

- **No serialisation.** Every wake starts a run; nothing queues and nothing is held back, so an agent may have several runs at once.
- **No tracking.** The child is reaped so it does not linger; no run table, no exit bookkeeping, no lifetime bound, nothing reconciled at boot.
- **The binary is absolute**, because a user unit's `PATH` does not resolve the bare command ([`004`](adrs/004-configuration.md)).
- **When it cannot start**, the audit line records it and the delivery is still answered `202`. The service keeps no queue, so that wake is lost rather than retried.

The argv is a contract with another program — `--profile`/`-p` and `chat -q` are Hermes' own. A test can pin the argv the service builds; only a hand check on the host can show that Hermes still accepts it.

## The reason

A wake's only input is the reason, and the reason is identifiers and the service's own words — never a title, a body, a branch or a comment ([`002`](adrs/002-github-webhook-ingestion.md)). The two fixed sentences are:

```
Dispatch: review requested on aivara-se/dispatch#105
Dispatch: changes requested on aivara-se/dispatch#105
```

The service's name tells the agent this is a wake and not the operator speaking; the repository and number are the pointer the agent re-reads; the phrase is the why-now. GitHub sends `review_requested` for both a reviewer added and a review requested again, so that sentence does not claim to tell them apart.

## GitHub access

One organisation webhook, created by an owner of the organisation, subscribed to `pull_request` and `pull_request_review`, and set to content type `json` — a `form` webhook sends `payload=<urlencoded>`, which the JSON decode refuses. Its secret is the value the configuration names, held in the unit's environment and never in the tree.

The service calls no GitHub API and holds no GitHub credential: the payload and the configuration answer the whole decision ([`002`](adrs/002-github-webhook-ingestion.md)). The agents act on GitHub with their own credentials, not the service's.

## Storage

- **In memory, and only there:** the delivery-id cache for the TTL `002` fixes, and the configuration and secret resolved at boot.
- **On disk:** the audit file alone — one JSON Lines line per delivery, appended, size-rotated ([`005`](adrs/005-the-audit-record.md)).
- **Nothing else.** No database, no queue, no counter, no ordering of events, and no state reconciled after a restart.

## Failure modes

- **A wrong or missing signature** — `401`; nothing is parsed and nothing is logged.
- **A body that is not JSON, or past the 1 MiB bound** — refused, answered `400`.
- **A delivery that is not ours** — another event, action or review state: `202`, and nobody is woken.
- **A repeat** — `202`, and nobody is woken; the line shows it was seen.
- **A run that cannot start** — recorded on the line, answered `202`, not retried.
- **A crash or a restart** — out of scope: the cache and any run in flight are not reconciled, and a delivery redelivered inside the lost hour can start one extra run.

## Checks

```sh
gofmt -l .        # empty output is the pass
go vet ./...
go test ./...
```

The tests must cover: the signature refused before parsing, the event and review-state filter, the dedup, the configuration loader's refusals, and one signed POST per flow that starts a run. The reason tests use a fixture whose every text field is a sentinel — no sentinel reaches the argv — and the argv is a fixed sentence above. A fake `hermes` on the configured path captures the argv.

What no test can reach: that Hermes still accepts the argv, and a real delivery through GitHub to a real agent run. Both are checked by hand on the host.
