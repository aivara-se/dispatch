# 024 - The Dedup Key Is The Signed Body

## Context

`002` says a delivery seen before wakes nobody, and that the dedup key is `X-GitHub-Delivery`. The id is a header and the HMAC covers the body alone, so a body that was accepted once verifies as readily on its second posting — whatever id the poster chooses. The cache answered to the header, so a replay under a fresh id was a fresh delivery and started a second run. The event header steers `decide` the same way, and nothing binds it to the body either.

A probe posted one signed body three times under three delivery ids: three runs started. A body that was read once — by a proxy that logs payloads, by a dump on loopback, by a run that kept its own argv — was therefore an unlimited supply of wakes of that delivery's agent.

`002`'s rule holds for GitHub's own redelivery, which reuses the id it redelivers.

## Decision

- **The dedup key is the SHA-256 of the verified body.** A body already acted on is answered `202` with `duplicate` and starts nothing, whatever id and event headers it carries.
- **The delivery id stays, as the audit's identity.** It names the delivery on every line and pairs a held line with its follow-up's; it no longer decides whether anything acts.
- **The TTL is `002`'s**: an hour, in memory, gone with the process.

## What this overrides

`002`'s "A delivery seen before wakes nobody. The dedup key is `X-GitHub-Delivery`, held in memory for one hour (provisional; the first live week freezes the number)." The rule stands and the hour stands; what identifies a delivery as seen is now its bytes. Every other clause of `002` stands, GitHub's own redelivery answering `duplicate` included, because it carries the same bytes it carried the first time.

`013`, `016` and `017` each say the delivery id is the unit of dedupe, in a consequence about toggles, reviews and dismissals. Those consequences still hold — each of those events is its own body — and the sentence naming the id as the unit is superseded by this one.

## Consequences

- **One body under any number of ids is one delivery**, and one id with two bodies is two, which is what a redelivery carrying a corrected payload looks like. Bytes are the unit of dedupe; the id is a label.
- **Two real events with identical bytes would be one delivery.** GitHub's payloads differ per delivery — ids, timestamps, the state that changed — so this does not bite today, and the risk is named rather than guarded.
- **The cache holds digests rather than delivery ids**: same hour, same memory-only life, a wider key.
- **A restart still loses the cache**, so a replay after one is a wake again. `002` accepted that price for the redelivered id, and this decision changes the key rather than the durability.
- **The event header is still taken on trust.** Which branch of `decide` a delivery reaches is still steered by `X-GitHub-Event`, and binding it to the body is a separate decision, named in the pull request that lands this one.
- **A test that posts one fixture under many ids must vary the bytes**, because a second delivery now means a second body.
