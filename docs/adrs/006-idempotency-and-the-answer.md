# 006 - Idempotency and the Answer Every Delivery Gets

## Context

`SYSTEMS.md` (*The request path*, *Failure modes*) fixes the answer set and the one-hour cache; [002](002-github-webhook-ingestion.md) fixes everything up to and including verification. What is unrecorded is what happens to a delivery after it verifies: what makes a repeat harmless, and what each answer commits the service to.

## Decision

- **The dedup key is the delivery id**, `X-GitHub-Delivery`, remembered for one hour in a bounded in-memory cache. GitHub keeps the id identical across redeliveries of the same delivery, so a repeat is recognised by the thing GitHub itself calls the delivery's identity.
- **The board is the durable state.** Nothing about idempotency survives a restart, and a restart therefore costs at most one duplicate wake.
- **The answer set is the gateway's own**: `202` acted on or ignored, `400` a body that is not the JSON the signature covered, `401` a missing or non-verifying signature, `404` a path served by nobody, `413` the body limit, `429` the inbound rate limit, `503` no capacity.
- **An event that is not ours is `202`, never a `4xx`.** A `4xx` is for a request that was wrong, not for one that was ours to ignore.
- **A failed write fails the delivery**: the delivery is refused and GitHub sends it again, rather than being answered `202` and forgotten.
- **A dead-lettered delivery drops its dedup entry**, so an operator's re-dispatch after a fix is not swallowed as a duplicate.

## Consequences

- One hour is the TTL the gateway's own adapter uses, so the two hops forget at the same rate: a delivery cannot be remembered at one hop and forgotten at the other.
- In memory and bounded is the whole of the state idempotency costs: no file, no database, no row. The price is stated rather than hidden — a restart inside the hour can start one extra run, and the agent re-reads the board when it runs, which is what makes the extra run harmless.
- `202` covers two different outcomes, acted on and deliberately ignored, so the delivery list alone cannot tell them apart. The audit line is what does, and it carries the decision for every delivery whatever the answer.
- Refusing a failed delivery puts the retry in GitHub's hands, within GitHub's own window: the service keeps no queue, and a delivery it could not act on is not lost. The cost is that a service which is up but cannot reach the API looks like a failing sender in the delivery list, which is where the operator will look anyway.
- Two deliveries carrying the same fact are two deliveries and are both acted on. What keeps a burst on one card to one run is the runner's one-run-per-agent rule ([004](004-starting-a-run.md)), not this cache; the cache is for the same delivery twice.

## Rejected

- **A durable dedup store** — a file, or a marker on the board. Rejected: durable state of the service's own is the one thing the board exists to avoid, and the duplicate it would prevent costs one run that re-reads the board and changes nothing.
- **Dedup by a hash of the body.** Rejected: the identity is GitHub's delivery id; a body hash conflates a delivery with its content, and two genuine deliveries can carry the same body.
- **A `4xx` for an event we do not act on.** Rejected, for the reason [002](002-github-webhook-ingestion.md) rejects the same for a repository that is not on the allowlist: the delivery list stops distinguishing an excluded event from a broken one.
- **`200` for a delivery acted on.** Rejected: the gateway's own set uses `202`, and one mental model across the two hops is worth more than the distinction a `200` would draw.
- **A lock per card, held until its run ends.** Rejected: the board's `Started` column already says which agent is working, and a lock is state the service would have to survive a restart with.
