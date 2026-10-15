# 008 - The Service Acts on the Board's State

## Context

A service that acts only when GitHub posts knows only what it is told: a card that generates no event is invisible, a wake whose target is busy is dropped with nothing to replay it, and a card that predates the service is never looked at at all.

## Decision

**The service is driven by the board's state.** A delivery is one of the ways it learns the state changed.

- **The question is per card, not per event**: what moves this card forward, who does it, and is it already happening. `PRODUCT.md` (*The rule*) is that list.
- **It looks at three moments**: a delivery arrives, a run it started ends, and once at boot. Nothing else fires it — there is still no clock.
- **"Is it already happening" is the service's own run table**, held in memory and rebuilt at boot from the cards in `Started` and the processes it finds. The board is the durable state, but it is not the record of a run.
- **The pass writes one field and repeats a wake only to repair a claim.** It remembers, per card, the action last attempted for it, and attempts that action again only when the card has changed since, or when that attempt never started a run. A card its agent has been given and has not moved is otherwise the agent's decision, not a wake to repeat.

## Consequences

- The bound costs a card's patience. An agent that has been given a card and has not moved it is not woken again for it, so a card can wait on its agent's judgement for as long as that agent chooses; the service's answer is that the agent holds it, and the operator's is the board.
- A card that sits still is a card the service has an answer for. That is what makes a card predating the service, a card an agent created for itself, and a card whose wake was dropped, reachable at all.
- Two triggers cost one board read each: the full project read happens on those three moments.
- The run table and the per-card record are memory, so a restart forgets which runs were in flight and which cards it had already woken. The boot pass rebuilds the run table from `Started` and starts one run per agent for the cards it finds unsettled — one run per agent is the runner's rule, so one pass starts one run per agent, not one per unsettled card; the agent re-reads the board when it runs, which is what keeps the extra run harmless.
- The board does not have to carry a mark for every run, so a column keeps its single meaning: a stage of the work, never a fact about the service.
- The service can act on a card no delivery named, so the audit line records the trigger as well as the card, and *boot* is one of the triggers.
