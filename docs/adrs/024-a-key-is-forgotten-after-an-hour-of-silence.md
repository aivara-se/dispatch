# 024 - A Key Is Forgotten After an Hour of Silence

## Context

`SYSTEMS` *Storage* lists what the service keeps in memory: the delivery-id cache for the TTL `002` fixes, each pull request's round count ([`007`](007-the-review-cap.md)), its failed-check count ([`010`](010-a-failing-check-wakes-the-pusher.md)) and each repository and branch's ([`014`](014-a-red-main-wakes-its-pusher.md)), and one key per live run with the delivery held for it ([`021`](021-one-run-per-pull-request-per-agent.md)). Only the first of those forgets anything. Every other map keeps a key for the life of the process, and its size is chosen by whoever sends the delivery: one red-main delivery per branch, one review per pull request.

A probe at `57f0be3` posted 30000 acting deliveries: the round count held 20000 keys, the failed-check count 5000, and 5000 red-main deliveries naming 5000 distinct branches left 5000 gate keys behind. What is unbounded is not the runs — an exit releases its key — but the names: the delivery carries them, and a sender holding the webhook secret, or anyone who can push a branch to the organisation, decides how many.

The dedup cache is the one structure that prunes, and it pays for it on every add: a scan of every entry under the lock it already holds. 4000 posts cost 271 ms behind a 4000-entry cache and 1369 ms behind a 16000-entry one, so the per-delivery cost rises with the cache it is bounding.

## Decision

- **A key is forgotten after `DedupTTL` without a delivery reaching it.** The TTL is the one `002` fixes for the delivery-id cache, an hour. A round count, a failed-check count, a gate key and a seen delivery id all expire on it.
- **A reach is a touch.** Each map stamps the key on every delivery that consults it, not only on one that advances a count, so a cap still refuses at its limit and a key is still claimed while deliveries keep arriving. The TTL starts an hour of silence, which a live loop never reaches.
- **The sweep is off the delivery.** A counter and the gate take their sweep once in 256 touches, under their own lock. The dedup cache snapshots under the lock, scans the snapshot outside it, and then deletes the ids it showed expired, keeping one that was added again while the snapshot was being scanned.
- **A key with a delivery held for it is not swept.** That delivery is a wake and the gate is the only thing that records it; the run's exit is its release. Only a key with nothing held — a run that has not exited and that no delivery has reached for an hour — is freed.
- **Nothing becomes durable.** No file, no queue, nothing reconciled at boot: the same maps, each entry carrying the moment it was last reached.

## What this overrides

`007`'s "held in memory for the life of the process" and its "No expiry, no reset" are overridden for the review cap, and `010`'s and `014`'s "the count dies with the process" is widened: it dies an hour after its last delivery as well. Their limits, keys and spends are unchanged, and `014`'s second counter keeps its own key.

`021`'s "A run that never exits holds its key for the life of the process ... only a restart releases the key" is narrowed: an hour with nothing held for it is the other release, and everything else in `021` stands, the held delivery's own line included.

`002`'s TTL now bounds every other key as well. The cache keeps its semantics, and only the moment of its pruning moves.

## Consequences

- **A burst still costs its keys.** The sweep bounds what a burst leaves behind, not its peak: M distinct keys delivered inside one hour are M keys until that hour has passed. The bound is a rate — keys per TTL — rather than a total.
- **An abandoned key is freed, so a second run can start over the same object.** That is the overlap `021` fixed, and it comes back for a run that has neither exited nor heard a delivery for an hour. A run that long is one the operator has to look at anyway.
- **A held wake outlives the hour.** Nothing sweeps it, so an object's wakes still stop behind a run that never exits until a restart, as before; its cost is one entry per stalled object.
- **A cap is one hour of history.** A disagreement that pauses for an hour gets its ten rounds again, and a break that stops failing for an hour gets its five wakes again. A loop that keeps delivering never notices.
- **A forgotten key leaves no trace.** The audit says nothing about a count that was dropped, so the boot line and the held line are still the only markers a count was lost.
