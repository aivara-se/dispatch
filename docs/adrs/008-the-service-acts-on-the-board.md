# 008 - The Service Acts on the Board's State

## Context

`PRODUCT.md` was first written as a table of events: a card is added, a card moves, a review is requested. The service would therefore act only when GitHub posted, and hold nothing else — the design was one delivery in, one decision out.

Running the flow by hand for a day showed what an event table cannot do. Thirty-two cards sat on the board untouched because none of them generated an event. Two cards were refined and never reached a developer, because what assigns a developer is the *move* to `Refined` and nothing looks at a card that is already there. A wake whose target was busy was dropped, and the card it concerned was never looked at again. None of these is a bug in a rule; they are the shape of a service that only knows what it is told.

## Decision

**The service is driven by the board's state.** A delivery is one of the ways it learns the state changed.

- **The question is per card, not per event**: what moves this card forward, who does it, and is it already happening. `PRODUCT.md` (*The rule*) is that list.
- **It looks at three moments**: a delivery arrives, a run it started ends, and once at boot. Nothing else fires it — there is still no clock.
- **"Is it already happening" is the service's own run table**, held in memory and rebuilt at boot from the cards in `Started` and the processes it finds. The board is the durable state, but it is not the record of a run.
- **The pass writes one field and repeats a wake only to repair a claim.** It remembers, per card, the action it last started a run for, and starts that action again only when the card has changed since, or when the earlier run never started — a wake that started nothing repairs nothing. A card its agent has been given and has not moved is otherwise the agent's decision, not a wake to repeat.

## Consequences

- The bound costs a card's patience. An agent that has been given a card and has not moved it is not woken again for it, so a card can wait on its agent's judgement for as long as that agent chooses; the service's answer is that the agent holds it, and the operator's is the board.
- A card that sits still is a card the service has an answer for. That is what makes a card predating the service, a card an agent created for itself, and a card whose wake was dropped, reachable at all.
- Two new triggers cost one board read each. The full project read is the service's expensive call, so it happens on those three moments and never on a payload's say-so alone.
- The run table and the per-card record are memory, so a restart forgets which runs were in flight and which cards it had already woken. The boot pass rebuilds the run table from `Started` and starts one run per agent for the cards it finds unsettled — one run per agent is the runner's rule, so a pass that finds twenty-eight unsettled cards for one agent starts one run, not twenty-eight; the agent re-reads the board when it runs, which is what keeps the extra run harmless.
- The board does not have to carry a mark for every run, so a column keeps its single meaning: a stage of the work, never a fact about the service.
- The service can act on a card no delivery named, so the audit line records the trigger as well as the card, and *boot* is one of the triggers.

## Rejected

- **The event table alone.** Rejected: it cannot move a card that generates no event, it drops a wake whose target is busy with nothing to replay it, and every card that predates the service is invisible to it.
- **A clock** — a poll every few minutes. Rejected for now: a trigger that fires while nothing has changed buys latency the three moments do not, and each of them is a moment the state actually moved.
- **A durable run store** — a file, a row, a marker. Rejected: the board is the durable state and the run table is rebuilt at boot; the cost of rebuilding is at most one extra run per agent.
- **The board's `Started` column as the record of a run.** Rejected: it is a developer's column, it is moved by whoever holds the card, and GitHub's own project automation sets it when a pull request opens (observed on the live board, 2026-10-12).
