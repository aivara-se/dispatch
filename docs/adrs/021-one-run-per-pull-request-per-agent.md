# 021 - One Run per Pull Request per Agent

## Context

`003` starts a run for every wake and keeps no record of one in flight: a wake is a reason the agent re-reads, so a repeated run reviews again and is harmless, and a repeated review costs tokens rather than correctness. On `aivara-se/.website#43` that premise failed. Two `review_requested` deliveries 8 minutes apart started two runs over one pull request, the second while the first was still live; each posted `CHANGES_REQUESTED` on head `de9ee74` 40 s apart, carrying half the findings each, and neither saw the other's review. The author was woken three times in seven minutes and four of the ten rounds were spent before either review was answered.

The premise holds for a re-read and not for a posted artifact. A review is published: an agent that answers a wake by submitting a review cannot merge its answer with a run it cannot see, so two concurrent runs over one pull request produce two partial verdicts where one complete one belongs. No rule in the reviewing profile closes that — the second run reads its own earlier review at start, before the first has posted it, so the check and the submission are 40 s apart at best and 8 minutes apart at worst.

The tree is what `003` says: `wakeTargets` starts a run per profile with nothing keyed by the object (`internal/receiver/receiver.go`), and `Runner.Start` returns once the child is launched (`internal/run/run.go`).

## Decision

- **One run in flight per object and profile.** The key is `<repository>#<number>` for a pull request or an issue and `<repository>@<branch>` for a default-branch run, paired with the local profile.
- **A delivery that lands while its key's run is live is held, not started.** When that run exits, one follow-up run starts, carrying the most recent held delivery's reason. A later held delivery replaces an earlier one, so a burst becomes one follow-up and the reason it carries is the last why-now.
- **The exit is the release.** The service already waits on every child and reads its status once ([`015`](015-the-audit-records-a-runs-exit.md)); the key is set when the start succeeds and cleared when the exit arrives or the follow-up fails to start.
- **Different keys still run concurrently.** Two pull requests of one agent do not wait on each other, and two agents reviewing one pull request do not wait on each other: the overlap that cost correctness is two runs of one agent over one object, and nothing wider is serialised.
- **A held delivery says so on its own line**: decision `held` and the key it waits on. The follow-up appends a line of its own when it starts, carrying the same delivery id, the profile and the reason, so the reader pairs the two.
- **A superseded delivery appends nothing further.** Its line says a later delivery for the same key replaced it, and the follow-up's line names the delivery it came from.
- **Still no queue and no durable state**: at most one held delivery per key, no ordering beyond replacement, nothing on disk and nothing reconciled at boot.
- **Neither cap changes.** Both are consulted at the delivery and advance at the moment they advance today, so a held delivery spends a round exactly as a started one does.

## What this overrides

`003`'s "No serialisation" bullet, and with it its Consequences' "a burst of deliveries for one agent starts a burst of runs" and the claim that a repeated review "costs tokens and not correctness". `003`'s "The run is not tracked" is narrowed, not withdrawn: an exit is now read for the gate as well as for `015`'s line, no run is recorded, and nothing about one outlives its key.

`015`'s "Still no ... run table" is narrowed the same way — one key per live run, and no record of what the run was. `005`'s "One JSON Lines line per delivery" is extended as `015` already extended it: a held delivery's follow-up writes a line of its own, and a reader pairs them by the delivery id.

When the code lands this retires PRODUCT's "Never holds a wake back or queues one" bullet, its edge bullet "An agent already running", and SYSTEMS' "No serialisation" sentence. Everything else in `003` stands, the argv and the absolute path included, and `002`'s no-API rule is untouched: the gate reads no GitHub state.

## Consequences

- **A review request waits for the review already running.** In the observed case that is up to 8 minutes, and the review it then produces covers the whole head instead of half of it. Latency grows by at most one run.
- **Two runs can no longer publish two partial reviews of one head.** That is the correctness the wait buys.
- **A run that never exits holds its key for the life of the process**, so that object's wakes for that profile stop. The held delivery's line is the trace — a delivery that reads `held` and no follow-up after it is a run that has not ended — and only a restart releases the key. Nothing bounds or kills a child, as before.
- **A crash loses a held delivery.** Its line says it was held, so the record shows a wake that did not happen rather than one that ran.
- **The gate sees only the runs this service started.** A run the operator starts by hand on the same object is invisible to it, and the overlap it prevents can still happen there.
- **Dedupe still runs first.** A redelivered delivery inside the dedup window is seen and appends nothing, so a held key cannot turn one delivery into two runs.
- **One follow-up can carry a reason that is not the one the review was waiting for.** An agent mentioned in the same pull request while reviewing it gets one run whose reason is the mention; it re-reads either way, and the review is what the run finds.
