# 008 - The Service Acts on the Board's State

## Context

A service that acts only when GitHub posts knows only what it is told: a card that generates no event is invisible, a wake whose target is busy is dropped with nothing to replay it, and a card that predates the service is never looked at at all.

## Decision

**The service is driven by the board's state.** A delivery is one of the ways it learns the state changed.

- **The question is per card, not per event**: what moves this card forward, who does it, and is it already happening. `PRODUCT.md` (*The rule*) is that list.
- **It looks at three moments**: a delivery arrives, a run it started ends, and once at boot. Nothing else fires it — there is still no clock.
- **"Is it already happening" is the service's own run table**, held in memory: one entry per profile the runner has run — the run it started last for it, and whether that run is still going. Which of the cards and the runs becomes an entry is the runner's fact and never the board's: a card is never an entry, so a `Started` card whose process died does not hold its agent out of a new run; and a run the runner holds is an entry whether or not a card sits in `Started` behind it, so no live child is left out of the answer. A `Started` card with no entry behind it is unsettled work, and the boot look starts the run the assignment promised instead of pretending one is going. The board is the durable state, but it is not the record of a run.
- **The pass writes one field and repeats a wake only to repair a claim.** It remembers, per card, the action last attempted for it, keyed to the marker the card's own row of the read gave — its column and the login that holds it — and attempts that action again only when that marker has changed, or when the attempt never started a run. The login in the marker is the record's durable half, because it is the one field the pass writes ([005](005-board-access.md)); whether the run went is memory's, and memory's alone. A card its agent has been given and has not moved is otherwise the agent's decision, not a wake to repeat.

## Consequences

- The bound costs a card's patience. An agent that has been given a card and has not moved it is not woken again for it, so a card can wait on its agent's judgement for as long as that agent chooses; the service's answer is that the agent holds it, and the operator's is the board.
- A card that sits still is a card the service has an answer for. That is what makes a card predating the service, a card an agent created for itself, and a card whose wake was dropped, reachable at all.
- Two triggers cost one board read each: the full project read happens on those three moments.
- The run table is memory, so a restart forgets which runs were in flight; the per-card record is memory too, and of it what survives is the marker the board itself carries — the card's column and the login that holds it. The boot look therefore re-decides every card from the board alone, and writes no claim twice: a card the board shows a holder on is that agent's until the card changes, so a claim whose run never went is not repaired at boot — the miss is in the dead-letter file, and the wake waits for the card to change, where the per-look repair would have repeated it. A `Started` card is the one the board cannot answer for, a column being no run: with no live run behind it, the boot look starts the run the claim promised (`PRODUCT.md`, *The rule*: with no run started for it, its assignee). That is the whole cost of a restart — at most one extra run per agent per restart, which the agent re-reading the board when it runs keeps harmless. What the marker never guarantees is that the promised run went.
- The board does not have to carry a mark for every run, so a column keeps its single meaning: a stage of the work, never a fact about the service.
- The service can act on a card no delivery named, so the audit line records the trigger as well as the card, and *boot* is one of the triggers.
