# 006 - Idempotency and the Answer Every Delivery Gets

## Context

`SYSTEMS.md` (*The request path*, *Failure modes*) fixes the answer set and the one-hour cache; [002](002-github-webhook-ingestion.md) fixes everything up to and including verification. What is unrecorded is what happens to a delivery after it verifies: what makes a repeat harmless, and what each answer commits the service to.

## Decision

- **The dedup key is the delivery id**, `X-GitHub-Delivery`, remembered for one hour in a bounded in-memory cache. GitHub keeps the id identical across redeliveries of the same delivery (*Best practices for using webhooks*, *Use the `X-GitHub-Delivery` header*, docs.github.com, read 2026-10-12), so a repeat is recognised by the thing GitHub itself calls the delivery's identity.
- **The board is the durable state.** Nothing about idempotency survives a restart, so a delivery redelivered inside the hour is acted on again: a restart costs at most one duplicate wake for such a delivery, and what the pass's own loss costs is [008](008-the-service-acts-on-the-board.md)'s.
- **The answer set is this service's own** (`SYSTEMS.md`, *The request path*), and it parts company with the gateway's in two places. The gateway answers `200` to a delivery it ignores or has already seen, where this service answers `202` to both, because a delivery it accepted is not a delivery it acted on; and the gateway's `403` — a route whose secret is missing or disabled — has no counterpart here, because a secret reference that resolves to nothing stops this process at boot. Every other code is the same code with the same meaning, read from the gateway's own handler (`NousResearch/hermes-agent`, `gateway/platforms/webhook.py`, read 2026-10-12); a Hermes upgrade can change it, and nothing in this repository's tests covers it.
- **An event that is not ours is `202`, never a `4xx`.** A `4xx` is for a request that was wrong, not for one that was ours to ignore.
- **A failed read, a failed write or a run that will not start fails the delivery**: it is answered `502` rather than `202` and forgotten, so a wake the service could not act on is never silently lost. The recovery is the next look (`SYSTEMS.md`, *Failure modes*).
- **A dead-lettered delivery drops its dedup entry**, so a re-dispatch after a fix is not swallowed as a duplicate.
- **One inbound limit**: sixty requests a minute from one source, answered `429` before anything is parsed and before any audit line is appended (provisional; the first live week freezes the number). An unauthenticated POST must cost a header check and nothing else, and a service with no limit in front of it pays a disk append per request.

## Consequences

- One hour is the TTL the gateway's own adapter uses (`_idempotency_ttl = 3600`, same file, same read), so the two hops forget at the same rate: a delivery cannot be remembered at one hop and forgotten at the other.
- In memory and bounded is the whole of the state idempotency costs: no file, no database, no row. The price is stated rather than hidden — a restart inside the hour can start one extra run, and the agent re-reads the board when it runs, which is what makes the extra run harmless.
- `202` covers two different outcomes, acted on and deliberately ignored, so the delivery list alone cannot tell them apart. The audit line is what does, and it carries the decision for every delivery answered after verification; a request refused before that — a signature, a body limit, a rate limit — is answered and not logged.
- Refusing a delivery does not lose it: GitHub records a `5xx` as a failed delivery and keeps it in the list (*Troubleshooting webhooks*, *Invalid HTTP response*, docs.github.com, read 2026-10-12), and the dead-letter file keeps the service's own copy, so the wake has two records to be found in. What the refusal costs is the wait — the next look starts it, not the sender — and a service that is up but cannot reach the API looks like a failing sender until then. That is where the operator looks anyway.
- Two deliveries carrying the same fact are two deliveries and are both acted on. What keeps a burst on one card to one run is the runner's one-run-per-agent rule ([004](004-starting-a-run.md)), not this cache; the cache is for the same delivery twice.
